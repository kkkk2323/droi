package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/smartwarm"
)

const agentRequestJSON = `{"model":"claude-opus-5-5","max_tokens":128000,"stream":false,` +
	`"thinking":{"type":"adaptive"},"output_config":{"effort":"high"},` +
	`"tools":[{"name":"Read","input_schema":{"type":"object"}}],` +
	`"system":[{"type":"text","text":"You are Droid <b>&</b>","cache_control":{"type":"ephemeral","ttl":"1h"}}],` +
	`"messages":[` +
	`{"role":"user","content":[{"type":"text","text":"context","cache_control":{"type":"ephemeral"}}]},` +
	`{"role":"assistant","content":[{"type":"text","text":"ok"}]},` +
	`{"role":"user","content":[{"type":"text","text":"go on","cache_control":{"type":"ephemeral"}}]}]}`

type fakeTimer struct {
	d       time.Duration
	f       func()
	stopped bool
}

func (t *fakeTimer) Stop() bool {
	was := !t.stopped
	t.stopped = true
	return was
}

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) timerHandle {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{d: d, f: f}
	c.timers = append(c.timers, t)
	return t
}

func (c *fakeClock) last(t *testing.T) *fakeTimer {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.timers) == 0 {
		t.Fatal("no timer scheduled")
	}
	return c.timers[len(c.timers)-1]
}

// fireLast advances the clock to the last timer and runs it synchronously.
func (c *fakeClock) fireLast(t *testing.T) {
	t.Helper()
	tm := c.last(t)
	if tm.stopped {
		t.Fatal("last timer was stopped")
	}
	c.mu.Lock()
	c.now = c.now.Add(tm.d)
	c.mu.Unlock()
	tm.f()
}

type fakeStates struct {
	sessions []OpenedSession
	err      error
}

func (s *fakeStates) ListOpenedSessions(context.Context) ([]OpenedSession, error) {
	return s.sessions, s.err
}

type fakeLock struct{ locked bool }

func (l fakeLock) Locked() (bool, bool) { return l.locked, true }

func newTestWarmer(mode string, states SessionStates) (*CacheWarmer, *fakeClock) {
	rotator := &keys.KeyRotator{}
	rotator.SetActiveKeyForTest("pool-key")
	clock := &fakeClock{now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	w := NewCacheWarmer(CacheWarmingConfig{Mode: mode, MinTokens: 50000, IdleMax: 30 * time.Minute, MaxSessions: 4, MaxTokens: 1},
		rotator, states, fakeLock{}, diag.NewRecorder(50))
	w.now = clock.Now
	w.afterFunc = clock.AfterFunc
	return w, clock
}

func testCapture(t *testing.T, upstream, encoding string, at time.Time) warmCapture {
	t.Helper()
	wire := []byte(agentRequestJSON)
	if encoding == "gzip" {
		var err error
		if wire, err = gzipBody(wire); err != nil {
			t.Fatal(err)
		}
	}
	h := http.Header{}
	h.Set(sessionIDHeader, "s1")
	h.Set(assistantMessageIDHeader, "orig-id")
	h.Set("Authorization", "Bearer real-key")
	h.Set("x-api-provider", "anthropic")
	if encoding != "" {
		h.Set("Content-Encoding", encoding)
	}
	return warmCapture{
		sessionID: "s1", upstream: upstream, path: "/api/llm/a/v1/messages", header: h, encoding: encoding,
		wire: wire, model: "claude-opus-5-5", provider: "anthropic", key: "real-key",
		usage:  diag.TokenUsage{Input: 100000, CacheRead: 99000, CacheWrite: 900, Output: 50},
		sentAt: at,
	}
}

func working() *fakeStates {
	return &fakeStates{sessions: []OpenedSession{{SessionID: "s1", WorkingState: "executing_tool"}}}
}

func usageServer(t *testing.T, usage string, got chan<- *http.Request, bodies chan<- []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if got != nil {
			got <- r
			bodies <- b
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"message","content":[],"usage":` + usage + `}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWarmableTTL(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    time.Duration
		wantErr bool
	}{
		{"five minute tail", agentRequestJSON, 5 * time.Minute, false},
		{"one hour tail", strings.Replace(agentRequestJSON, `"go on","cache_control":{"type":"ephemeral"}`, `"go on","cache_control":{"type":"ephemeral","ttl":"1h"}`, 1), time.Hour, false},
		{"string content still uses block breakpoints", strings.Replace(agentRequestJSON, `[{"type":"text","text":"ok"}]`, `"ok"`, 1), 5 * time.Minute, false},
		{"no tools", strings.Replace(agentRequestJSON, `"tools":[{"name":"Read","input_schema":{"type":"object"}}],`, "", 1), 0, true},
		{"no message breakpoint", strings.ReplaceAll(agentRequestJSON, `,"cache_control":{"type":"ephemeral"}`, ""), 0, true},
		{"budget thinking", strings.Replace(agentRequestJSON, `{"type":"adaptive"}`, `{"type":"enabled","budget_tokens":32000}`, 1), 0, true},
		{"not json", "nope", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := warmableTTL([]byte(tt.body))
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("warmableTTL = %v, %v; want %v, err=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestWarmReplayBodyKeepsOriginalBytes(t *testing.T) {
	in := `{"model":"m", "metadata":{"max_tokens":5}, "max_tokens" : 64000,"stream":true,"system":"<b>&</b>"}`
	want := `{"model":"m", "metadata":{"max_tokens":5}, "max_tokens" : 1,"stream":true,"system":"<b>&</b>"}`
	out, err := warmReplayBody([]byte(in), 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != want {
		t.Fatalf("replay body:\n%s\nwant:\n%s", out, want)
	}
	if _, err := warmReplayBody([]byte(`{"model":"m"}`), 1); err == nil {
		t.Fatal("body without max_tokens was accepted")
	}
}

func TestWarmReplayBodyOnlyChangesMaxTokens(t *testing.T) {
	out, err := warmReplayBody([]byte(agentRequestJSON), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte(`You are Droid <b>&</b>`)) {
		t.Fatalf("HTML characters were escaped: %s", out)
	}
	var before, after map[string]json.RawMessage
	_ = json.Unmarshal([]byte(agentRequestJSON), &before)
	if err := json.Unmarshal(out, &after); err != nil {
		t.Fatal(err)
	}
	if string(after["max_tokens"]) != "1" {
		t.Fatalf("max_tokens = %s", after["max_tokens"])
	}
	if len(after) != len(before) {
		t.Fatalf("field count changed: %d -> %d", len(before), len(after))
	}
	for k, v := range before {
		if k != "max_tokens" && string(after[k]) != string(v) {
			t.Errorf("field %s changed:\n%s\n%s", k, v, after[k])
		}
	}
}

func TestWarmDelay(t *testing.T) {
	if got := warmDelay(5 * time.Minute); got != 270*time.Second {
		t.Errorf("5m -> %v", got)
	}
	if got := warmDelay(time.Hour); got != 54*time.Minute {
		t.Errorf("1h -> %v", got)
	}
	if got := warmDelay(20 * time.Second); got != 10*time.Second {
		t.Errorf("20s -> %v", got)
	}
}

func TestDecide(t *testing.T) {
	base := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	entry := func() *warmEntry {
		return &warmEntry{sessionID: "s1", ttl: 5 * time.Minute, sentAt: base, lastRealAt: base}
	}
	session := func(state string) []OpenedSession {
		return []OpenedSession{{SessionID: "other", WorkingState: "idle"}, {SessionID: "s1", WorkingState: state}}
	}
	at := base.Add(270 * time.Second)
	tests := []struct {
		name     string
		mode     string
		mutate   func(*warmEntry)
		sessions []OpenedSession
		listErr  error
		locked   time.Time
		smart    *fakeAdvisor
		now      time.Time
		want     warmAction
		reason   string
	}{
		{name: "working session is refreshed", mode: CacheWarmingStreaming, sessions: session("executing_tool"), now: at, want: warmSend},
		{name: "waiting for approval is refreshed", mode: CacheWarmingStreaming, sessions: session("waiting_for_tool_confirmation"), now: at, want: warmSend},
		{name: "idle stops in streaming mode", mode: CacheWarmingStreaming, sessions: session("idle"), now: at, want: warmStop, reason: "idle_in_streaming_mode"},
		{name: "idle is refreshed in idle mode", mode: CacheWarmingIdle, sessions: session("idle"), now: at, want: warmSend},
		{name: "idle limit", mode: CacheWarmingIdle, sessions: session("idle"), now: base.Add(31 * time.Minute),
			mutate: func(e *warmEntry) { e.sentAt = base.Add(27 * time.Minute) }, want: warmStop, reason: "idle_limit"},
		{name: "working past an hour", mode: CacheWarmingIdle, sessions: session("executing_tool"), now: base.Add(61 * time.Minute),
			mutate: func(e *warmEntry) { e.sentAt = base.Add(57 * time.Minute) }, want: warmStop, reason: "max_age"},
		{name: "busy session retries", mode: CacheWarmingIdle, sessions: session("thinking"), now: at, want: warmRetry, reason: "session_busy"},
		{name: "closed session", mode: CacheWarmingIdle, sessions: session("idle")[:1], now: at, want: warmStop, reason: "session_closed"},
		{name: "unknown state counts as idle", mode: CacheWarmingIdle, listErr: errors.New("no daemon"), now: at, want: warmSend},
		{name: "unknown state stops streaming mode", mode: CacheWarmingStreaming, listErr: errors.New("no daemon"), now: at, want: warmStop, reason: "idle_in_streaming_mode"},
		{name: "recently locked screen is refreshed", mode: CacheWarmingStreaming, sessions: session("executing_tool"), locked: base, now: at, want: warmSend},
		{name: "screen locked past the idle limit", mode: CacheWarmingIdle, sessions: session("executing_tool"), locked: base.Add(10 * time.Minute), now: base.Add(45 * time.Minute),
			mutate: func(e *warmEntry) { e.sentAt = base.Add(41 * time.Minute) }, want: warmStop, reason: "screen_locked"},
		{name: "timer fired after the entry expired", mode: CacheWarmingIdle, sessions: session("executing_tool"), now: base.Add(5 * time.Minute), want: warmStop, reason: "deadline_missed"},
		{name: "dry run", mode: CacheWarmingDryRun, sessions: session("idle"), now: at, want: warmDryRun},
		{name: "smart refreshes when history says so", mode: CacheWarmingSmart, sessions: session("idle"), now: at,
			smart: &fakeAdvisor{worth: true, ok: true}, want: warmSend},
		{name: "smart stops when history says no", mode: CacheWarmingSmart, sessions: session("executing_tool"), now: at,
			smart: &fakeAdvisor{ok: true}, want: warmStop, reason: "smart_not_worth"},
		{name: "smart may go past an hour", mode: CacheWarmingSmart, sessions: session("executing_tool"), now: base.Add(61 * time.Minute),
			mutate: func(e *warmEntry) { e.sentAt = base.Add(57 * time.Minute) }, smart: &fakeAdvisor{worth: true, ok: true}, want: warmSend},
		{name: "smart without history uses the idle limit", mode: CacheWarmingSmart, sessions: session("idle"), now: base.Add(31 * time.Minute),
			mutate: func(e *warmEntry) { e.sentAt = base.Add(27 * time.Minute) }, smart: &fakeAdvisor{}, want: warmStop, reason: "idle_limit"},
		{name: "smart without history keeps the hour cap", mode: CacheWarmingSmart, sessions: session("executing_tool"), now: base.Add(61 * time.Minute),
			mutate: func(e *warmEntry) { e.sentAt = base.Add(57 * time.Minute) }, smart: &fakeAdvisor{}, want: warmStop, reason: "max_age"},
		{name: "smart without an advisor uses the hour cap", mode: CacheWarmingSmart, sessions: session("executing_tool"), now: base.Add(61 * time.Minute),
			mutate: func(e *warmEntry) { e.sentAt = base.Add(57 * time.Minute) }, want: warmStop, reason: "max_age"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := newTestWarmer(tt.mode, &fakeStates{})
			if tt.smart != nil {
				w.SetSmartAdvisor(tt.smart)
			}
			e := entry()
			if tt.mutate != nil {
				tt.mutate(e)
			}
			got, reason := w.decide(e, tt.sessions, tt.listErr, tt.locked, tt.now)
			if got != tt.want || reason != tt.reason {
				t.Fatalf("decide = %v %q, want %v %q", got, reason, tt.want, tt.reason)
			}
		})
	}
}

func TestCacheWarmerRefreshesWithOnlyMaxTokensChanged(t *testing.T) {
	got := make(chan *http.Request, 1)
	bodies := make(chan []byte, 1)
	srv := usageServer(t, `{"input_tokens":10,"cache_read_input_tokens":99990,"cache_creation_input_tokens":0,"output_tokens":1}`, got, bodies)
	w, clock := newTestWarmer(CacheWarmingStreaming, working())

	w.Observe(testCapture(t, srv.URL, "gzip", clock.Now()))
	if d := clock.last(t).d; d != 270*time.Second {
		t.Fatalf("first refresh scheduled after %v, want 270s", d)
	}
	clock.fireLast(t)

	req := <-got
	body := <-bodies
	if req.URL.Path != "/api/llm/a/v1/messages" {
		t.Errorf("path = %s", req.URL.Path)
	}
	if req.Header.Get("Content-Encoding") != "gzip" {
		t.Errorf("Content-Encoding = %q", req.Header.Get("Content-Encoding"))
	}
	if auth := req.Header.Get("Authorization"); auth != "Bearer pool-key" {
		t.Errorf("Authorization = %q, want the rotator's key", auth)
	}
	if id := req.Header.Get(assistantMessageIDHeader); id == "" || id == "orig-id" {
		t.Errorf("x-assistant-message-id = %q, want a fresh id", id)
	}
	if req.Header.Get(sessionIDHeader) != "s1" {
		t.Errorf("x-session-id = %q", req.Header.Get(sessionIDHeader))
	}
	plain, err := gunzipBody(body)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := warmReplayBody([]byte(agentRequestJSON), 1)
	if !bytes.Equal(plain, want) {
		t.Fatalf("replayed body:\n%s\nwant:\n%s", plain, want)
	}

	recs := w.recorder.Requests()
	if len(recs) != 1 || recs[0].Kind != "cache_warm" || recs[0].Usage == nil || recs[0].Usage.CacheRead != 99990 {
		t.Fatalf("recorded requests = %+v", recs)
	}
	st := w.Status()
	if len(st.Sessions) != 1 || st.Sessions[0].Refreshes != 1 || st.Totals.Refreshes != 1 {
		t.Fatalf("status = %+v", st)
	}
	if d := clock.last(t).d; d != 270*time.Second {
		t.Fatalf("next refresh scheduled after %v, want 270s", d)
	}
}

func TestCacheWarmerStopsWhenRefreshRewrites(t *testing.T) {
	srv := usageServer(t, `{"input_tokens":10,"cache_read_input_tokens":40000,"cache_creation_input_tokens":60000,"output_tokens":1}`, nil, nil)
	w, clock := newTestWarmer(CacheWarmingStreaming, working())
	w.Observe(testCapture(t, srv.URL, "", clock.Now()))
	clock.fireLast(t)
	if st := w.Status(); len(st.Sessions) != 0 || st.Totals.Failures != 1 {
		t.Fatalf("status = %+v", st)
	}
	if evs := w.recorder.Events(); len(evs) == 0 || evs[0].Kind != "cache_warm_rewrote" {
		t.Fatalf("events = %+v", evs)
	}
}

func TestCacheWarmerStopsOnUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()
	w, clock := newTestWarmer(CacheWarmingStreaming, working())
	w.Observe(testCapture(t, srv.URL, "", clock.Now()))
	clock.fireLast(t)
	if st := w.Status(); len(st.Sessions) != 0 {
		t.Fatalf("session kept after 402: %+v", st)
	}
	if evs := w.recorder.Events(); len(evs) == 0 || evs[0].Kind != "cache_warm_failed" || evs[0].Status != http.StatusPaymentRequired {
		t.Fatalf("events = %+v", evs)
	}
}

func TestRealRequestCancelsPendingRefresh(t *testing.T) {
	w, clock := newTestWarmer(CacheWarmingStreaming, working())
	w.Observe(testCapture(t, "http://unused", "", clock.Now()))
	timer := clock.last(t)

	background := http.Header{}
	background.Set(sessionIDHeader, "s1")
	background.Set(messageCallTypeHeader, sessionTitleCallType)
	w.RealRequestStarted(background)
	if timer.stopped || len(w.Status().Sessions) != 1 {
		t.Fatal("a background request cancelled the conversation's refresh")
	}

	main := http.Header{}
	main.Set(sessionIDHeader, "s1")
	w.RealRequestStarted(main)
	if !timer.stopped || len(w.Status().Sessions) != 0 {
		t.Fatal("the next real request did not cancel the refresh")
	}
}

func TestObserveSkipsWhatCannotBeWarmed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*warmCapture)
	}{
		{"small prompt", func(c *warmCapture) { c.usage.Input = 1000 }},
		{"no cache activity", func(c *warmCapture) { c.usage.CacheRead, c.usage.CacheWrite = 0, 0 }},
		{"background call type", func(c *warmCapture) { c.header.Set(messageCallTypeHeader, sessionTitleCallType) }},
		{"no session id", func(c *warmCapture) { c.header.Del(sessionIDHeader) }},
		{"unsupported encoding", func(c *warmCapture) { c.encoding = "br" }},
		{"no tools", func(c *warmCapture) {
			c.wire = []byte(strings.Replace(agentRequestJSON, `"tools":[{"name":"Read","input_schema":{"type":"object"}}],`, "", 1))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, clock := newTestWarmer(CacheWarmingIdle, working())
			c := testCapture(t, "http://unused", "", clock.Now())
			tt.mutate(&c)
			w.Observe(c)
			if n := len(w.Status().Sessions); n != 0 {
				t.Fatalf("captured %d sessions", n)
			}
		})
	}
}

func TestDryRunSendsNothing(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	w, clock := newTestWarmer(CacheWarmingDryRun, working())
	w.Observe(testCapture(t, srv.URL, "", clock.Now()))
	clock.fireLast(t)
	if called {
		t.Fatal("dry-run sent a request")
	}
	if evs := w.recorder.Events(); len(evs) == 0 || evs[0].Kind != "cache_warm_dry_run" {
		t.Fatalf("events = %+v", evs)
	}
	if len(w.Status().Sessions) != 1 {
		t.Fatal("dry-run dropped the session")
	}
}

func TestMaxSessionsEvictsOldest(t *testing.T) {
	w, clock := newTestWarmer(CacheWarmingIdle, working())
	w.cfg.MaxSessions = 2
	for i, id := range []string{"a", "b", "c"} {
		c := testCapture(t, "http://unused", "", clock.Now().Add(time.Duration(i)*time.Second))
		c.sessionID = id
		c.header.Set(sessionIDHeader, id)
		w.Observe(c)
	}
	var ids []string
	for _, s := range w.Status().Sessions {
		ids = append(ids, s.SessionID)
	}
	if strings.Join(ids, ",") != "b,c" {
		t.Fatalf("sessions = %v, want b,c", ids)
	}
}

func TestSavedUnitsAfterRefreshedReturn(t *testing.T) {
	srv := usageServer(t, `{"input_tokens":10,"cache_read_input_tokens":99990,"cache_creation_input_tokens":0,"output_tokens":1}`, nil, nil)
	w, clock := newTestWarmer(CacheWarmingIdle, working())
	start := clock.Now()
	w.Observe(testCapture(t, srv.URL, "", start))
	clock.fireLast(t)

	next := testCapture(t, srv.URL, "", start.Add(8*time.Minute))
	w.RealRequestStarted(next.header)
	w.Observe(next)
	if saved := w.Status().Totals.SavedUnits; saved < 100000 {
		t.Fatalf("SavedUnits = %v, want about 1.2 x 100000", saved)
	}
}

type fakeAdvisor struct {
	worth, ok bool
	prepared  int
	asked     []smartwarm.Query
}

func (a *fakeAdvisor) Prepare() { a.prepared++ }
func (a *fakeAdvisor) Features(_ string, s smartwarm.State) smartwarm.Features {
	return smartwarm.Features{State: s, LongReply: true}
}
func (a *fakeAdvisor) Worth(q smartwarm.Query) (bool, bool) {
	a.asked = append(a.asked, q)
	return a.worth, a.ok
}
func (a *fakeAdvisor) Model() *smartwarm.Model { return nil }

func TestSmartQueryMeasuresTheWait(t *testing.T) {
	w, _ := newTestWarmer(CacheWarmingSmart, working())
	adv := &fakeAdvisor{worth: true, ok: true}
	w.SetSmartAdvisor(adv)
	if adv.prepared != 1 {
		t.Fatalf("advisor prepared %d times, want 1", adv.prepared)
	}
	base := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	e := &warmEntry{sessionID: "s1", model: "claude-opus-5-5", ttl: 5 * time.Minute, sentAt: base, lastRealAt: base, endedAt: base.Add(20 * time.Second)}
	q := w.smartQuery(e, smartwarm.Idle, base.Add(270*time.Second))
	want := smartwarm.Query{
		Features: smartwarm.Features{State: smartwarm.Idle, LongReply: true},
		Elapsed:  250 * time.Second, CoveredUntil: 280 * time.Second,
		Interval: 270 * time.Second, TTL: 5 * time.Minute, Gain: 24,
	}
	if math.Abs(q.Gain-want.Gain) < 1e-9 {
		want.Gain = q.Gain
	}
	if q != want {
		t.Fatalf("query = %+v\nwant    %+v", q, want)
	}
	if err := w.SetMode(CacheWarmingIdle); err != nil {
		t.Fatal(err)
	}
	if err := w.SetMode(CacheWarmingSmart); err != nil || adv.prepared != 2 {
		t.Fatalf("switching to smart: err=%v prepared=%d", err, adv.prepared)
	}
}

func TestCacheWarmerKeepsRefreshingWhileLocked(t *testing.T) {
	srv := usageServer(t, `{"input_tokens":10,"cache_read_input_tokens":99990,"cache_creation_input_tokens":0,"output_tokens":1}`, nil, nil)
	w, clock := newTestWarmer(CacheWarmingStreaming, working())
	w.lock = fakeLock{locked: true}
	w.Observe(testCapture(t, srv.URL, "", clock.Now()))
	// The first check sees the lock at 4m30s; refreshes go on every 4m30s
	// until the lock is older than the 30 minute idle limit.
	for i := 0; i < 8; i++ {
		clock.fireLast(t)
	}
	st := w.Status()
	if st.Totals.Refreshes != 7 || len(st.Sessions) != 0 {
		t.Fatalf("refreshes = %d, sessions = %d; want 7 refreshes, then a stop", st.Totals.Refreshes, len(st.Sessions))
	}
}

func TestSetModeSwitchesAndSaves(t *testing.T) {
	srv := usageServer(t, `{"input_tokens":10,"cache_read_input_tokens":99990,"cache_creation_input_tokens":0,"output_tokens":1}`, nil, nil)
	w, clock := newTestWarmer(CacheWarmingIdle, working())
	w.cfg.ModeFile = filepath.Join(t.TempDir(), "cache-warming.json")
	w.Observe(testCapture(t, srv.URL, "", clock.Now()))
	if n := len(w.Status().Sessions); n != 1 {
		t.Fatalf("captured %d sessions, want 1", n)
	}

	if err := w.SetMode(" Off "); err != nil {
		t.Fatal(err)
	}
	if st := w.Status(); st.Mode != CacheWarmingOff || len(st.Sessions) != 0 {
		t.Fatalf("after off: %+v", st)
	}
	if data, _ := os.ReadFile(w.cfg.ModeFile); strings.TrimSpace(string(data)) != `{"mode":"off"}` {
		t.Fatalf("mode file = %q", data)
	}
	w.Observe(testCapture(t, srv.URL, "", clock.Now()))
	if n := len(w.Status().Sessions); n != 0 {
		t.Fatalf("captured %d sessions while off", n)
	}

	if err := w.SetMode("always"); !errors.Is(err, errUnknownWarmMode) {
		t.Fatalf("SetMode(always) = %v", err)
	}
	if err := w.SetMode(CacheWarmingDryRun); err != nil {
		t.Fatal(err)
	}
	w.Observe(testCapture(t, srv.URL, "", clock.Now()))
	if st := w.Status(); st.Mode != CacheWarmingDryRun || len(st.Sessions) != 1 {
		t.Fatalf("after dry-run: %+v", st)
	}
}

func TestProxyCapturesOnlyMainConversation(t *testing.T) {
	upstream := usageServer(t, `{"input_tokens":10,"cache_read_input_tokens":99000,"cache_creation_input_tokens":990,"output_tokens":5}`, nil, nil)
	rotator := &keys.KeyRotator{}
	rotator.SetActiveKeyForTest("pool-key")
	px := NewProxy(upstream.URL, rotator, nil)
	w, _ := newTestWarmer(CacheWarmingIdle, working())
	px.SetCacheWarmer(w)
	baseURL, err := px.Start("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer px.Shutdown(context.Background())

	post := func(session, callType string) {
		req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/llm/a/v1/messages", strings.NewReader(agentRequestJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(sessionIDHeader, session)
		if callType != "" {
			req.Header.Set(messageCallTypeHeader, callType)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	post("main-session", "")
	post("title-session", sessionTitleCallType)

	sessions := w.Status().Sessions
	if len(sessions) != 1 || sessions[0].SessionID != "main-session" || sessions[0].PromptTokens != 100000 {
		t.Fatalf("sessions = %+v", sessions)
	}
	for _, rec := range px.Recorder().Requests() {
		if rec.Kind != "" {
			t.Fatalf("proxied request recorded with kind %q", rec.Kind)
		}
	}
}

func TestParseScreenLocked(t *testing.T) {
	if parseScreenLocked([]byte(`  | "IOConsoleLocked" = No`)) {
		t.Error("unlocked console reported as locked")
	}
	if !parseScreenLocked([]byte(`  | "IOConsoleUsers" = ({"CGSSessionScreenIsLocked"=Yes,"kCGSSessionOnConsoleKey"=Yes})`)) {
		t.Error("locked session not detected")
	}
	if !parseScreenLocked([]byte(`  | "IOConsoleLocked" = Yes`)) {
		t.Error("locked console not detected")
	}
}
