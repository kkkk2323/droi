package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/netproxy"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/smartwarm"
)

// Cache warming keeps a session's Anthropic prompt cache alive across idle
// gaps by replaying the session's last real request, unchanged except for a
// minimal max_tokens, shortly before the cache entry expires. A refresh is a
// cache read (0.05x input for claude-opus-5-5 in Factory billing); letting the
// entry expire costs a 1.25x rewrite of the whole prefix. See
// docs/plans/2026-10-02-cache-warming-design.md.

const (
	CacheWarmingOff       = "off"
	CacheWarmingDryRun    = "dry-run"
	CacheWarmingStreaming = "streaming"
	CacheWarmingIdle      = "idle"
	// CacheWarmingSmart decides each refresh from the user's own history (see
	// package smartwarm) and falls back to the idle rules while that history
	// is too thin.
	CacheWarmingSmart = "smart"
)

const (
	sessionIDHeader          = "x-session-id"
	assistantMessageIDHeader = "x-assistant-message-id"
	// Every real request gets a cache entry with the TTL of its breakpoints;
	// a refresh must land before it expires, keeping this margin.
	warmDeadlineMargin = 5 * time.Second
	// A session that is mid-turn will send its own request soon; look again
	// after this delay instead of refreshing.
	warmBusyRetry      = 15 * time.Second
	warmStateTimeout   = 5 * time.Second
	warmRequestTimeout = 60 * time.Second
	warmMaxAge         = 60 * time.Minute
	warmResponseLimit  = 64 << 20
	fiveMinuteCacheTTL = 5 * time.Minute
	oneHourCacheTTL    = time.Hour
)

// SessionStates lists the Droid sessions a Daemon has open.
type SessionStates interface {
	ListOpenedSessions(ctx context.Context) ([]OpenedSession, error)
}

// SmartAdvisor answers the smart mode from the user's history; see
// smartwarm.Advisor.
type SmartAdvisor interface {
	// Prepare builds or refreshes the model in the background.
	Prepare()
	Features(sessionID string, state smartwarm.State) smartwarm.Features
	Worth(q smartwarm.Query) (worth, ok bool)
	// Model is nil until the first build finished.
	Model() *smartwarm.Model
}

// CacheWarmingConfig holds the cache warming tunables.
type CacheWarmingConfig struct {
	Mode        string
	MinTokens   int64
	IdleMax     time.Duration
	MaxSessions int
	MaxTokens   int
	// ModeFile, when set, keeps a mode chosen through SetMode for the next start.
	ModeFile string
}

func CacheWarmingConfigFrom(cfg *config.Config) CacheWarmingConfig {
	return CacheWarmingConfig{
		Mode:        cfg.CacheWarmingMode,
		MinTokens:   int64(cfg.CacheWarmingMinTokens),
		IdleMax:     time.Duration(cfg.CacheWarmingIdleMaxMin) * time.Minute,
		MaxSessions: cfg.CacheWarmingMaxSessions,
		MaxTokens:   cfg.CacheWarmingMaxTokens,
		ModeFile:    cfg.CacheWarmingModeFile,
	}
}

type timerHandle interface{ Stop() bool }

// CacheWarmer is safe for concurrent use. The zero value is not usable; call
// NewCacheWarmer.
type CacheWarmer struct {
	cfg CacheWarmingConfig
	// mode starts as cfg.Mode; SetMode changes it while the daemon runs.
	mode     atomic.Value
	rotator  *keys.KeyRotator
	states   SessionStates
	lock     ScreenLock
	recorder *diag.Recorder
	client   *http.Client
	smart    SmartAdvisor

	now       func() time.Time
	afterFunc func(time.Duration, func()) timerHandle

	mu      sync.Mutex
	entries map[string]*warmEntry
	// credits remembers refreshed sessions until their next real request
	// shows whether the refresh avoided a rewrite.
	credits map[string]warmCredit
	totals  CacheWarmingTotals
	stopped bool
}

type warmEntry struct {
	sessionID    string
	upstream     string
	path         string
	rawQuery     string
	header       http.Header
	encoding     string
	plain        []byte
	model        string
	provider     string
	key          string
	promptTokens int64
	ttl          time.Duration
	sentAt       time.Time
	lastRealAt   time.Time
	// endedAt is when the real request's response finished: the start of the
	// wait the smart mode measures.
	endedAt    time.Time
	refreshes  int
	nextWarmAt time.Time
	// lockedSince is when a refresh check first saw the screen locked; zero
	// while unlocked.
	lockedSince time.Time
	state       string
	timer       timerHandle
	cancel      context.CancelFunc
}

type warmCredit struct {
	promptTokens int64
	model        string
	lastRealAt   time.Time
	ttl          time.Duration
	refreshes    int
}

// warmCapture is one finished real LLM request as handleRequest saw it.
type warmCapture struct {
	sessionID string
	upstream  string
	path      string
	rawQuery  string
	header    http.Header
	encoding  string
	wire      []byte
	model     string
	provider  string
	key       string
	usage     diag.TokenUsage
	sentAt    time.Time
}

// CacheWarmingStatus is the cacheWarming block of /debug/state.
type CacheWarmingStatus struct {
	Mode     string                `json:"mode"`
	Sessions []CacheWarmingSession `json:"sessions"`
	Totals   CacheWarmingTotals    `json:"totals"`
	// Smart describes the smart mode's model once it is built.
	Smart *CacheWarmingSmartModel `json:"smart,omitempty"`
}

type CacheWarmingSmartModel struct {
	Waits   int       `json:"waits"`
	BuiltAt time.Time `json:"builtAt"`
}

type CacheWarmingSession struct {
	SessionID    string    `json:"sessionId"`
	Model        string    `json:"model"`
	PromptTokens int64     `json:"promptTokens"`
	TTLSec       int       `json:"ttlSec"`
	Refreshes    int       `json:"refreshes"`
	NextWarmAt   time.Time `json:"nextWarmAt"`
	State        string    `json:"state"`
}

// CacheWarmingTotals counts since daemon start. Units are Factory billing
// tokens before the model's own multiplier: SpentUnits is what refreshes
// cost, SavedUnits the rewrites they are estimated to have avoided.
type CacheWarmingTotals struct {
	Refreshes        int64   `json:"refreshes"`
	Failures         int64   `json:"failures"`
	CacheReadTokens  int64   `json:"cacheReadTokens"`
	CacheWriteTokens int64   `json:"cacheWriteTokens"`
	SpentUnits       float64 `json:"spentUnits"`
	SavedUnits       float64 `json:"savedUnits"`
}

func NewCacheWarmer(cfg CacheWarmingConfig, rotator *keys.KeyRotator, states SessionStates, lock ScreenLock, recorder *diag.Recorder) *CacheWarmer {
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 4
	}
	if cfg.MaxTokens < 0 {
		cfg.MaxTokens = 1
	}
	w := &CacheWarmer{
		cfg:      cfg,
		rotator:  rotator,
		states:   states,
		lock:     lock,
		recorder: recorder,
		client:   &http.Client{Transport: netproxy.Transport()},
		now:      time.Now,
		afterFunc: func(d time.Duration, f func()) timerHandle {
			return time.AfterFunc(d, f)
		},
		entries: map[string]*warmEntry{},
		credits: map[string]warmCredit{},
	}
	w.mode.Store(cfg.Mode)
	return w
}

// isMainConversation is true for the agent loop's own requests. Background
// callers (session titles, classifiers, compaction helpers) set
// X-Message-Call-Type and may share the session id; they must neither be
// warmed nor cancel the conversation's warming.
func isMainConversation(h http.Header) bool {
	return strings.TrimSpace(h.Get(sessionIDHeader)) != "" && strings.TrimSpace(h.Get(messageCallTypeHeader)) == ""
}

// RealRequestStarted cancels any pending or in-flight refresh of the session:
// its next real request is about to refresh the cache itself.
func (w *CacheWarmer) RealRequestStarted(h http.Header) {
	if w == nil || !isMainConversation(h) {
		return
	}
	id := strings.TrimSpace(h.Get(sessionIDHeader))
	w.mu.Lock()
	defer w.mu.Unlock()
	e := w.entries[id]
	if e == nil {
		return
	}
	if e.refreshes > 0 {
		w.credits[id] = warmCredit{promptTokens: e.promptTokens, model: e.model, lastRealAt: e.lastRealAt, ttl: e.ttl, refreshes: e.refreshes}
	}
	w.dropLocked(id, e)
}

// Observe saves a finished real request for later refreshes.
func (w *CacheWarmer) Observe(c warmCapture) {
	if w == nil || w.Mode() == CacheWarmingOff || !isMainConversation(c.header) {
		return
	}
	w.settleCredit(c)
	if c.usage.CacheRead+c.usage.CacheWrite == 0 || c.usage.Input < w.cfg.MinTokens {
		return
	}
	plain, err := decodeWireBody(c.encoding, c.wire)
	if err != nil {
		return
	}
	ttl, err := warmableTTL(plain)
	if err != nil {
		return
	}
	header := c.header.Clone()
	header.Del("Authorization")
	e := &warmEntry{
		sessionID:    c.sessionID,
		upstream:     c.upstream,
		path:         c.path,
		rawQuery:     c.rawQuery,
		header:       header,
		encoding:     strings.ToLower(strings.TrimSpace(c.encoding)),
		plain:        plain,
		model:        c.model,
		provider:     c.provider,
		key:          c.key,
		promptTokens: c.usage.Input,
		ttl:          ttl,
		sentAt:       c.sentAt,
		lastRealAt:   c.sentAt,
		endedAt:      w.now(),
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	if old := w.entries[c.sessionID]; old != nil {
		w.dropLocked(c.sessionID, old)
	}
	for len(w.entries) >= w.cfg.MaxSessions {
		w.evictOldestLocked()
	}
	w.entries[c.sessionID] = e
	w.scheduleLocked(e, e.sentAt.Add(warmDelay(e.ttl)))
}

// settleCredit credits a refresh chain when the next real request, arriving
// after the original entry would have expired, still read the prefix from
// the cache.
func (w *CacheWarmer) settleCredit(c warmCapture) {
	w.mu.Lock()
	defer w.mu.Unlock()
	credit, ok := w.credits[c.sessionID]
	if !ok {
		return
	}
	delete(w.credits, c.sessionID)
	if c.sentAt.Sub(credit.lastRealAt) <= credit.ttl || c.usage.CacheRead*2 < credit.promptTokens {
		return
	}
	r := cacheReadMultiplier(credit.model)
	w.totals.SavedUnits += (1.25 - r) * float64(credit.promptTokens)
}

// warmDelay matches pi: refresh at 90% of the TTL, at least ten seconds early.
func warmDelay(ttl time.Duration) time.Duration {
	d := ttl * 9 / 10
	if alt := ttl - 10*time.Second; alt < d {
		d = alt
	}
	if d < 0 {
		return 0
	}
	return d
}

func (w *CacheWarmer) scheduleLocked(e *warmEntry, at time.Time) {
	if e.timer != nil {
		e.timer.Stop()
	}
	d := at.Sub(w.now())
	if d < 0 {
		d = 0
	}
	e.nextWarmAt = at
	e.state = "scheduled"
	e.timer = w.afterFunc(d, func() { w.fire(e) })
}

func (w *CacheWarmer) dropLocked(id string, e *warmEntry) {
	if e.timer != nil {
		e.timer.Stop()
	}
	if e.cancel != nil {
		e.cancel()
	}
	if w.entries[id] == e {
		delete(w.entries, id)
	}
}

func (w *CacheWarmer) evictOldestLocked() {
	var oldestID string
	var oldest *warmEntry
	for id, e := range w.entries {
		if oldest == nil || e.lastRealAt.Before(oldest.lastRealAt) {
			oldestID, oldest = id, e
		}
	}
	if oldest != nil {
		w.dropLocked(oldestID, oldest)
	}
}

func (w *CacheWarmer) current(e *warmEntry) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return !w.stopped && w.entries[e.sessionID] == e
}

type warmAction int

const (
	warmSend warmAction = iota
	warmDryRun
	warmRetry
	warmStop
)

// decide applies the rules of the design doc, in order, for one due refresh.
func (w *CacheWarmer) decide(e *warmEntry, sessions []OpenedSession, listErr error, lockedSince time.Time, now time.Time) (warmAction, string) {
	if w.Mode() == CacheWarmingOff {
		return warmStop, "disabled"
	}
	if now.After(e.sentAt.Add(e.ttl - warmDeadlineMargin)) {
		return warmStop, "deadline_missed"
	}
	smart := w.Mode() == CacheWarmingSmart && w.smart != nil
	maxAge := warmMaxAge
	if smart {
		maxAge = smartwarm.Horizon
	}
	if now.Sub(e.lastRealAt) > maxAge {
		return warmStop, "max_age"
	}
	// A locked screen alone is no reason to stop: the user is often away for a
	// few minutes while the session waits for them, and one rewrite costs as
	// much as 12 to 25 refreshes. A lock longer than the idle limit is.
	if !lockedSince.IsZero() && w.cfg.IdleMax > 0 && now.Sub(lockedSince) > w.cfg.IdleMax {
		return warmStop, "screen_locked"
	}
	idle := true
	if listErr == nil {
		state, found := "", false
		for _, s := range sessions {
			if s.SessionID == e.sessionID {
				state, found = s.WorkingState, true
				break
			}
		}
		if !found {
			return warmStop, "session_closed"
		}
		switch state {
		case "executing_tool", "waiting_for_tool_confirmation":
			idle = false
		case "idle", "":
		default:
			return warmRetry, "session_busy"
		}
	}
	if smart {
		state := smartwarm.Waiting
		if idle {
			state = smartwarm.Idle
		}
		if worth, ok := w.smart.Worth(w.smartQuery(e, state, now)); ok {
			if !worth {
				return warmStop, "smart_not_worth"
			}
			return warmSend, ""
		}
		// Too little history: the idle mode's rules and its hour cap apply.
		if now.Sub(e.lastRealAt) > warmMaxAge {
			return warmStop, "max_age"
		}
	}
	if idle {
		if w.Mode() == CacheWarmingStreaming {
			return warmStop, "idle_in_streaming_mode"
		}
		if w.cfg.IdleMax > 0 && now.Sub(e.lastRealAt) > w.cfg.IdleMax {
			return warmStop, "idle_limit"
		}
	}
	if w.Mode() == CacheWarmingDryRun {
		return warmDryRun, ""
	}
	return warmSend, ""
}

func (w *CacheWarmer) fire(e *warmEntry) {
	if !w.current(e) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), warmStateTimeout)
	sessions, listErr := w.states.ListOpenedSessions(ctx)
	cancel()
	locked := false
	if w.lock != nil {
		locked, _ = w.lock.Locked()
	}
	now := w.now()
	w.mu.Lock()
	switch {
	case !locked:
		e.lockedSince = time.Time{}
	case e.lockedSince.IsZero():
		e.lockedSince = now
	}
	lockedSince := e.lockedSince
	w.mu.Unlock()
	action, reason := w.decide(e, sessions, listErr, lockedSince, now)

	w.mu.Lock()
	if w.stopped || w.entries[e.sessionID] != e {
		w.mu.Unlock()
		return
	}
	switch action {
	case warmStop:
		w.dropLocked(e.sessionID, e)
		w.mu.Unlock()
		w.event("cache_warm_stopped", e, 0, fmt.Sprintf("reason=%s refreshes=%d", reason, e.refreshes))
		return
	case warmRetry:
		at := now.Add(warmBusyRetry)
		if deadline := e.sentAt.Add(e.ttl - warmDeadlineMargin); at.After(deadline) {
			at = deadline
		}
		w.scheduleLocked(e, at)
		w.mu.Unlock()
		return
	case warmDryRun:
		e.refreshes++
		e.sentAt = now
		w.scheduleLocked(e, now.Add(warmDelay(e.ttl)))
		w.mu.Unlock()
		w.event("cache_warm_dry_run", e, 0, fmt.Sprintf("would refresh %d tokens", e.promptTokens))
		return
	}
	reqCtx, reqCancel := context.WithTimeout(context.Background(), warmRequestTimeout)
	e.cancel = reqCancel
	e.state = "refreshing"
	w.mu.Unlock()

	res := w.send(reqCtx, e, w.cfg.MaxTokens)
	reqCancel()
	w.finish(e, res)
}

type warmResult struct {
	start  time.Time
	status int
	usage  diag.TokenUsage
	found  bool
	err    error
}

// send replays the entry's request with only max_tokens changed. It bypasses
// handleRequest on purpose: a refresh is not LLM activity for the mission
// watchdog, must not be captured as a real request, and must not rotate keys
// on 402/429.
func (w *CacheWarmer) send(ctx context.Context, e *warmEntry, maxTokens int) warmResult {
	res := warmResult{start: w.now()}
	body, err := warmReplayBody(e.plain, maxTokens)
	if err == nil && e.encoding == "gzip" {
		body, err = gzipBody(body)
	}
	if err != nil {
		res.err = err
		return res
	}
	url := e.upstream + e.path
	if e.rawQuery != "" {
		url += "?" + e.rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		res.err = err
		return res
	}
	req.Header = e.header.Clone()
	if key, _ := w.rotator.SelectKeyForModel(e.key, e.model, nil); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if req.Header.Get(assistantMessageIDHeader) != "" {
		req.Header.Set(assistantMessageIDHeader, newRequestID())
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))

	resp, err := w.client.Do(req)
	if err != nil {
		res.err = err
		return res
	}
	defer resp.Body.Close()
	res.status = resp.StatusCode
	observer := diag.NewUsageObserver(resp.Header.Get("Content-Type"))
	warmBody, decoder, _ := decodeUpstreamBody(resp.Header.Get("Content-Encoding"), resp.Body)
	if decoder != nil {
		defer decoder.Close()
	}
	if _, err := io.Copy(observer, io.LimitReader(warmBody, warmResponseLimit)); err != nil {
		res.err = err
	}
	res.usage, res.found = observer.Usage()

	var rec diag.RequestRecord
	rec.Time = w.now()
	rec.Kind = "cache_warm"
	rec.Model = e.model
	rec.Provider = e.provider
	rec.Key = keys.MaskKey(strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer "))
	rec.Status = resp.StatusCode
	rec.Stream = observer.IsStream()
	rec.DurationMs = rec.Time.Sub(res.start).Milliseconds()
	rec.RequestBytes = int64(len(body))
	rec.Attempts = 1
	if res.found {
		u := res.usage
		rec.Usage = &u
	}
	if res.err != nil {
		rec.Error = "cache_warm_error"
	}
	w.recorder.RecordRequest(rec)
	return res
}

func (w *CacheWarmer) finish(e *warmEntry, res warmResult) {
	w.mu.Lock()
	if w.stopped || w.entries[e.sessionID] != e {
		w.mu.Unlock()
		return
	}
	e.cancel = nil
	kind, detail := "", ""
	switch {
	case res.err != nil:
		kind, detail = "cache_warm_failed", res.err.Error()
	case res.status != http.StatusOK:
		kind, detail = "cache_warm_failed", fmt.Sprintf("status=%d", res.status)
	case !res.found:
		kind, detail = "cache_warm_failed", "no usage in response"
	case float64(res.usage.CacheWrite) > 0.1*float64(e.promptTokens):
		kind, detail = "cache_warm_rewrote", fmt.Sprintf("cacheWrite=%d promptTokens=%d", res.usage.CacheWrite, e.promptTokens)
	case res.usage.CacheRead*2 < e.promptTokens:
		kind, detail = "cache_warm_stopped", fmt.Sprintf("reason=cache_lost cacheRead=%d promptTokens=%d", res.usage.CacheRead, e.promptTokens)
	}
	if res.found {
		w.totals.CacheReadTokens += res.usage.CacheRead
		w.totals.CacheWriteTokens += res.usage.CacheWrite
		r := cacheReadMultiplier(e.model)
		w.totals.SpentUnits += r*float64(res.usage.CacheRead) + 1.25*float64(res.usage.CacheWrite) + float64(res.usage.Input-res.usage.CacheRead-res.usage.CacheWrite)
	}
	if kind != "" {
		w.totals.Failures++
		w.dropLocked(e.sessionID, e)
		w.mu.Unlock()
		w.event(kind, e, res.status, detail)
		return
	}
	w.totals.Refreshes++
	e.refreshes++
	e.sentAt = res.start
	w.scheduleLocked(e, res.start.Add(warmDelay(e.ttl)))
	w.mu.Unlock()
	w.event("cache_warm_sent", e, res.status, fmt.Sprintf("cacheRead=%d cacheWrite=%d refreshes=%d", res.usage.CacheRead, res.usage.CacheWrite, e.refreshes))
}

// ProbeResult is the answer of POST /debug/cache-warm/probe.
type ProbeResult struct {
	Status       int              `json:"status"`
	PromptTokens int64            `json:"promptTokens"`
	MaxTokens    int              `json:"maxTokens"`
	Usage        *diag.TokenUsage `json:"usage,omitempty"`
	Error        string           `json:"error,omitempty"`
}

var errNoWarmEntry = errors.New("no captured request for this session")

// Probe refreshes one captured session now, whatever the mode, to measure
// what a refresh costs with the given max_tokens. A successful probe counts
// as a refresh and restarts the session's timer.
func (w *CacheWarmer) Probe(ctx context.Context, sessionID string, maxTokens int) (ProbeResult, error) {
	w.mu.Lock()
	e := w.entries[sessionID]
	w.mu.Unlock()
	if e == nil {
		return ProbeResult{}, errNoWarmEntry
	}
	res := w.send(ctx, e, maxTokens)
	out := ProbeResult{Status: res.status, PromptTokens: e.promptTokens, MaxTokens: maxTokens}
	if res.found {
		u := res.usage
		out.Usage = &u
	}
	if res.err != nil {
		out.Error = res.err.Error()
	}
	w.finish(e, res)
	return out, nil
}

// Status reports the warmer for /debug/state.
func (w *CacheWarmer) Status() *CacheWarmingStatus {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	st := &CacheWarmingStatus{Mode: w.Mode(), Sessions: []CacheWarmingSession{}, Totals: w.totals}
	for _, e := range w.entries {
		st.Sessions = append(st.Sessions, CacheWarmingSession{
			SessionID:    e.sessionID,
			Model:        e.model,
			PromptTokens: e.promptTokens,
			TTLSec:       int(e.ttl / time.Second),
			Refreshes:    e.refreshes,
			NextWarmAt:   e.nextWarmAt,
			State:        e.state,
		})
	}
	sort.Slice(st.Sessions, func(i, j int) bool { return st.Sessions[i].NextWarmAt.Before(st.Sessions[j].NextWarmAt) })
	if w.smart != nil {
		if m := w.smart.Model(); m != nil {
			st.Smart = &CacheWarmingSmartModel{Waits: m.Waits, BuiltAt: m.BuiltAt}
		}
	}
	return st
}

// SetSmartAdvisor attaches the smart mode's advisor; call it before Start.
func (w *CacheWarmer) SetSmartAdvisor(a SmartAdvisor) {
	w.smart = a
	if w.Mode() == CacheWarmingSmart && a != nil {
		a.Prepare()
	}
}

// smartQuery describes a due refresh in the wait's own time frame. A return
// inside the cache saves a rewrite at the write price and pays a read instead;
// a refresh costs one read.
func (w *CacheWarmer) smartQuery(e *warmEntry, state smartwarm.State, now time.Time) smartwarm.Query {
	read := cacheReadMultiplier(e.model)
	write := 1.25
	if e.ttl >= oneHourCacheTTL {
		write = 2
	}
	ended := e.endedAt
	if ended.IsZero() {
		ended = e.lastRealAt
	}
	return smartwarm.Query{
		Features:     w.smart.Features(e.sessionID, state),
		Elapsed:      now.Sub(ended),
		CoveredUntil: e.sentAt.Add(e.ttl).Sub(ended),
		Interval:     warmDelay(e.ttl),
		TTL:          e.ttl,
		Gain:         (write - read) / read,
	}
}

// Mode is the current mode.
func (w *CacheWarmer) Mode() string {
	m, _ := w.mode.Load().(string)
	return m
}

var errUnknownWarmMode = errors.New("mode must be off, dry-run, streaming, idle or smart")

// SetMode switches the mode while the daemon runs and saves it for the next
// start. Switching to off forgets every captured request.
func (w *CacheWarmer) SetMode(mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if !config.ValidCacheWarmingMode(mode) {
		return errUnknownWarmMode
	}
	if w.cfg.ModeFile != "" {
		if err := config.SaveCacheWarmingMode(w.cfg.ModeFile, mode); err != nil {
			return err
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Mode() == mode {
		return nil
	}
	w.mode.Store(mode)
	if mode == CacheWarmingSmart && w.smart != nil {
		w.smart.Prepare()
	}
	if mode == CacheWarmingOff {
		for id, e := range w.entries {
			w.dropLocked(id, e)
		}
		w.credits = map[string]warmCredit{}
	}
	log.Printf("cache warming mode=%s", mode)
	return nil
}

// Stop cancels every timer and in-flight refresh.
func (w *CacheWarmer) Stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	for id, e := range w.entries {
		w.dropLocked(id, e)
	}
}

func (w *CacheWarmer) event(kind string, e *warmEntry, status int, detail string) {
	w.recorder.RecordEvent(diag.Event{Time: w.now(), Kind: kind, Model: e.model, Status: status, Detail: "session=" + e.sessionID + " " + detail})
	log.Printf("%s model=%s session=%s %s", kind, e.model, e.sessionID, detail)
}

func decodeWireBody(encoding string, wire []byte) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "identity":
		return wire, nil
	case "gzip":
		return gunzipBody(wire)
	}
	return nil, fmt.Errorf("unsupported content encoding %q", encoding)
}

// warmableTTL accepts an Anthropic Messages request of the agent loop and
// returns the TTL of its last message breakpoint.
func warmableTTL(plain []byte) (time.Duration, error) {
	var req struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools    []json.RawMessage `json:"tools"`
		Thinking *struct {
			Type         string `json:"type"`
			BudgetTokens *int   `json:"budget_tokens"`
		} `json:"thinking"`
	}
	if err := json.Unmarshal(plain, &req); err != nil {
		return 0, err
	}
	if len(req.Messages) == 0 || len(req.Tools) == 0 {
		return 0, errors.New("not an agent conversation request")
	}
	// Budget-based thinking derives budget_tokens from max_tokens; shrinking
	// max_tokens changes the thinking config, which Anthropic keys the message
	// cache on, so such a replay would rewrite instead of refresh.
	if req.Thinking != nil && req.Thinking.BudgetTokens != nil {
		return 0, errors.New("budget-based thinking is not replayable")
	}
	ttl, found := time.Duration(0), false
	for _, m := range req.Messages {
		var blocks []struct {
			CacheControl *struct {
				TTL string `json:"ttl"`
			} `json:"cache_control"`
		}
		if json.Unmarshal(m.Content, &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			if b.CacheControl == nil {
				continue
			}
			found = true
			ttl = fiveMinuteCacheTTL
			if b.CacheControl.TTL == "1h" {
				ttl = oneHourCacheTTL
			}
		}
	}
	if !found {
		return 0, errors.New("no message cache breakpoint")
	}
	return ttl, nil
}

// warmReplayBody splices a new value over the top-level max_tokens and keeps
// every other byte, key order included: any change to tools, system, thinking
// or messages would move the cached prefix, and a re-encoded body would differ
// from the client's own requests on the wire.
func warmReplayBody(plain []byte, maxTokens int) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(plain))
	if tok, err := dec.Token(); err != nil {
		return nil, err
	} else if tok != json.Delim('{') {
		return nil, errors.New("request body is not a JSON object")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		if tok != "max_tokens" {
			continue
		}
		end := int(dec.InputOffset())
		start := end - len(value)
		out := make([]byte, 0, len(plain))
		out = append(out, plain[:start]...)
		out = strconv.AppendInt(out, int64(maxTokens), 10)
		return append(out, plain[end:]...), nil
	}
	return nil, errors.New("request body has no max_tokens")
}

// cacheReadMultiplier is Factory's cache-read price relative to input, used
// only for the savings estimate in /debug/state.
func cacheReadMultiplier(model string) float64 {
	switch {
	case strings.HasPrefix(model, "claude-opus-5-5"):
		return 0.05
	case strings.HasPrefix(model, "claude-fable-5.1"):
		return 0.025
	}
	return 0.1
}

func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
