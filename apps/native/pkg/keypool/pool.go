// Package keypool is the Key Pool (ADR 0027): the Factory API keys Droi owns
// for the Patched Droid's LLM requests, the key file the patch reads, and the
// LLM proxy that records those requests and moves droid to another key when
// one runs out. Everything lives in Droi's own data folder (Dir); nothing is
// shared with droid-proxy.
package keypool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/proxy"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/smartwarm"
)

// Options say where the Key Pool keeps its files and what it talks to.
type Options struct {
	// Dir holds keys.json, the key file and the logs; created mode 0700.
	Dir string
	// Upstream is the Factory API base URL the proxy forwards to.
	Upstream string
	// FactoryHome is the .factory folder whose Sessions the smart cache
	// warming learns from.
	FactoryHome string
	// Sessions lists the Daemon's open Sessions, for cache warming.
	Sessions proxy.SessionStates
	// OnChange hears every change of State.
	OnChange func()
	// ScriptModels reports the model IDs Settings lets use Script in
	// addition to Factory's list; nil adds none.
	ScriptModels func() []string
	// UsageURL replaces Factory's billing endpoint (for tests).
	UsageURL string
}

func (p *Pool) config() *config.Config {
	cfg := config.New()
	if p.opts.Upstream != "" {
		cfg.UpstreamBaseURL = strings.TrimRight(p.opts.Upstream, "/")
		// As the rest of Droi reads an account's usage: off the Factory API
		// base URL in force.
		cfg.UsageAPIURL = cfg.UpstreamBaseURL + "/api/billing/limits"
	}
	if p.opts.UsageURL != "" {
		cfg.UsageAPIURL, cfg.ChatUsageAPIURL = p.opts.UsageURL, ""
	}
	cfg.CacheWarmingModeFile = filepath.Join(p.opts.Dir, "cache-warming.json")
	return cfg
}

// Mode is how the Key Pool picks the key.
type Mode string

const (
	// Auto moves droid to another key when the current one runs out or the
	// pool prefers another for a model.
	Auto Mode = "auto"
	// Manual keeps the key the user chose; an exhausted key's errors reach
	// the Session.
	Manual Mode = "manual"
)

// Key is one key of the pool as the window shows it: never the key itself.
type Key struct {
	ID      string
	Masked  string
	Current bool
	AddedAt time.Time
	State   keys.KeyState
}

// State is what the Usage page and Settings show.
type State struct {
	Running   bool
	Mode      Mode
	Keys      []Key
	CurrentID string
	// Err is why the proxy is not running, or why the key file is not
	// written.
	Err          string
	CacheWarming *proxy.CacheWarmingStatus
	Events       []diag.Event
	// ScheduledOpening is whether Droi opens waiting keys' months on its
	// own; Opening is how today's openings stand, nil while not running.
	ScheduledOpening bool
	Opening          *keys.OpeningStatus
}

type storedKey struct {
	Key     string    `json:"key"`
	AddedAt time.Time `json:"addedAt"`
}

type storedPool struct {
	Mode             Mode        `json:"mode"`
	Current          string      `json:"current,omitempty"`
	ScheduledOpening bool        `json:"scheduledOpening,omitempty"`
	Keys             []storedKey `json:"keys"`
}

// Pool is safe for concurrent use.
type Pool struct {
	opts Options

	mu      sync.Mutex
	stored  storedPool
	rotator *keys.KeyRotator
	// idle reads the keys' quota while the proxy is not running; it is
	// never started, so it holds no goroutine.
	idle   *keys.KeyRotator
	px     *proxy.Proxy
	warmer *proxy.CacheWarmer
	sinks  []*diag.FileSink
	cancel context.CancelFunc
	err    string
}

// ErrNoKeys is why Start refuses an empty pool.
var ErrNoKeys = errors.New("the Key Pool has no keys")

// Open reads the pool from opts.Dir; a missing file is an empty pool.
func Open(opts Options) *Pool {
	p := &Pool{opts: opts, stored: storedPool{Mode: Auto}}
	if b, err := os.ReadFile(p.poolFile()); err == nil {
		_ = json.Unmarshal(b, &p.stored)
	}
	if p.stored.Mode != Manual {
		p.stored.Mode = Auto
	}
	return p
}

func (p *Pool) poolFile() string { return filepath.Join(p.opts.Dir, "keys.json") }

// KeyFile is the file the Patched Droid reads its LLM key from.
func (p *Pool) KeyFile() string { return filepath.Join(p.opts.Dir, "llm-key") }

func (p *Pool) logFile(name string) string { return filepath.Join(p.opts.Dir, "logs", name) }

// UsageLog is where the billing window snapshots go, for the spend history.
func (p *Pool) UsageLog() string { return p.logFile("usage.jsonl") }

func keyID(key string) string {
	s := sha256.Sum256([]byte(key))
	return hex.EncodeToString(s[:6])
}

func (p *Pool) changed() {
	if p.opts.OnChange != nil {
		p.opts.OnChange()
	}
}

// writePrivate writes b to path through a rename in the same folder, mode
// 0600, so a reader never sees half a file.
func writePrivate(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (p *Pool) saveLocked() error {
	b, err := json.MarshalIndent(p.stored, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(p.poolFile(), b)
}

func (p *Pool) keyByIDLocked(id string) string {
	for _, k := range p.stored.Keys {
		if keyID(k.Key) == id {
			return k.Key
		}
	}
	return ""
}

func (p *Pool) currentKeyLocked() string {
	if k := p.keyByIDLocked(p.stored.Current); k != "" {
		return k
	}
	if len(p.stored.Keys) > 0 {
		return p.stored.Keys[0].Key
	}
	return ""
}

// useLocked makes key the one the key file holds.
func (p *Pool) useLocked(key string) error {
	if err := writePrivate(p.KeyFile(), []byte(key)); err != nil {
		p.err = err.Error()
		return err
	}
	p.err = ""
	p.stored.Current = keyID(key)
	return p.saveLocked()
}

// Configure sets the upstream and the Factory home for the next Start. A
// running proxy with another upstream stops, so the next Start uses it.
func (p *Pool) Configure(upstream, factoryHome string) {
	p.mu.Lock()
	moved := p.px != nil && strings.TrimRight(upstream, "/") != strings.TrimRight(p.opts.Upstream, "/")
	p.opts.Upstream, p.opts.FactoryHome = upstream, factoryHome
	p.mu.Unlock()
	if moved {
		p.Stop()
	}
}

// Len is how many keys the pool has.
func (p *Pool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.stored.Keys)
}

// Start writes the key file and starts the proxy; it answers the base URL
// the Daemon's FACTORY_API_BASE_URL points at. Starting a running pool
// answers its URL.
func (p *Pool) Start() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.px != nil {
		return p.px.BaseURL(), nil
	}
	current := p.currentKeyLocked()
	if current == "" {
		p.err = l10n.L("The Key Pool has no key yet. It starts once you add one.")
		return "", ErrNoKeys
	}
	if err := p.useLocked(current); err != nil {
		return "", err
	}
	cfg := p.config()
	cfg.ApplyCacheWarmingModeFile()

	var list []string
	for _, k := range p.stored.Keys {
		list = append(list, k.Key)
	}
	rotator := keys.NewKeyRotator(cfg, list)
	p.idle = nil
	usageSink, err := diag.OpenFileSink(p.UsageLog(), 10<<20, -1)
	if err != nil {
		p.err = err.Error()
		return "", err
	}
	rotator.SetUsageLog(usageSink)
	p.sinks = []*diag.FileSink{usageSink}
	px := proxy.NewProxy(cfg.UpstreamBaseURL, rotator, p.switchKey)
	px.SetScriptModels(p.opts.ScriptModels)
	rec := px.Recorder()
	if sink, err := diag.OpenFileSink(p.RequestLog(), 10<<20, -1); err == nil {
		rec.PersistRequests(sink)
		p.sinks = append(p.sinks, sink)
	}
	if sink, err := diag.OpenFileSink(p.logFile("events.jsonl"), 10<<20, 3); err == nil {
		rec.PersistEvents(sink)
		p.sinks = append(p.sinks, sink)
	}
	base, err := px.Start("127.0.0.1", 0)
	if err != nil {
		p.err = err.Error()
		p.closeSinksLocked()
		return "", err
	}
	ctx, cancel := context.WithCancel(context.Background())
	sessions := p.opts.Sessions
	if sessions == nil {
		sessions = noSessions{}
	}
	warmer := proxy.NewCacheWarmer(proxy.CacheWarmingConfigFrom(cfg), rotator, sessions, proxy.MacScreenLock{}, rec)
	if p.opts.FactoryHome != "" {
		advisor := smartwarm.NewAdvisor(filepath.Join(p.opts.FactoryHome, "sessions"))
		warmer.SetSmartAdvisor(advisor)
		if cfg.CacheWarmingMode == proxy.CacheWarmingSmart {
			advisor.Prepare()
		}
	}
	px.SetCacheWarmer(warmer)
	px.SetKeyOpening(p.stored.ScheduledOpening)
	px.StartKeyOpener(ctx)
	p.rotator, p.px, p.warmer, p.cancel = rotator, px, warmer, cancel
	go func() {
		rotator.FetchAllUsages(ctx)
		rotator.StartRefreshLoop(ctx)
		p.afterUsage()
	}()
	return base, nil
}

// afterUsage moves an Auto pool off a current key that cannot serve at all
// once the usage is known.
func (p *Pool) afterUsage() {
	p.mu.Lock()
	r := p.rotator
	if r == nil || p.stored.Mode != Auto {
		p.mu.Unlock()
		p.changed()
		return
	}
	cur := p.currentKeyLocked()
	if want, _ := r.SelectKeyForModel(cur, "", nil); want != "" && want != cur {
		_ = p.useLocked(want)
		p.mu.Unlock()
		r.NoteKeyUsed(want)
		p.px.Recorder().RecordEvent(diag.Event{Time: time.Now(), Kind: "key_switched", Key: keys.MaskKey(want), Detail: "the current key cannot serve"})
	} else {
		p.mu.Unlock()
	}
	p.changed()
}

// switchKey is the proxy's KeySwitcher.
func (p *Pool) switchKey(key, _ string) bool {
	p.mu.Lock()
	if p.stored.Mode != Auto || p.keyByIDLocked(keyID(key)) == "" {
		p.mu.Unlock()
		return false
	}
	err := p.useLocked(key)
	p.mu.Unlock()
	p.changed()
	return err == nil
}

func (p *Pool) closeSinksLocked() {
	for _, s := range p.sinks {
		_ = s.Close()
	}
	p.sinks = nil
}

// Stop ends the proxy; the key file stays for a Daemon still running.
func (p *Pool) Stop() {
	p.mu.Lock()
	px, warmer, rotator, cancel := p.px, p.warmer, p.rotator, p.cancel
	p.px, p.warmer, p.rotator, p.cancel = nil, nil, nil, nil
	p.mu.Unlock()
	if px == nil {
		return
	}
	cancel()
	warmer.Stop()
	rotator.Stop()
	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	_ = px.Shutdown(ctx)
	done()
	// The recorder closes the request and event logs; the usage log is the
	// pool's.
	_ = px.Recorder().Close()
	p.mu.Lock()
	if len(p.sinks) > 0 {
		_ = p.sinks[0].Close()
	}
	p.sinks = nil
	p.mu.Unlock()
	p.changed()
}

// Add checks key against Factory's billing endpoint and adds it.
func (p *Pool) Add(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, "fk-") || strings.ContainsAny(key, " \t\r\n") {
		return errors.New(l10n.L("A Factory API key starts with fk- and has no spaces."))
	}
	p.mu.Lock()
	if p.keyByIDLocked(keyID(key)) != "" {
		p.mu.Unlock()
		return errors.New(l10n.L("This key is already in the Key Pool."))
	}
	r := p.readerLocked()
	p.mu.Unlock()
	if err := r.AddKey(ctx, key); err != nil {
		code := strings.TrimPrefix(err.Error(), "key probe failed: ")
		if keys.IsInvalidKeyError(code) {
			return errors.New(l10n.L("Factory does not accept this key. Check that you copied the whole key."))
		}
		return errors.New(l10n.L("Droi could not check this key's quota (%s). Check the key and try again.", code))
	}
	p.mu.Lock()
	p.stored.Keys = append(p.stored.Keys, storedKey{Key: key, AddedAt: time.Now().UTC()})
	err := p.saveLocked()
	p.mu.Unlock()
	p.changed()
	return err
}

// Remove takes the key out of the pool. Removing the current key moves the
// key file to another key; the last key cannot be removed while the proxy
// runs, since the Patched Droid would have none.
func (p *Pool) Remove(id string) error {
	p.mu.Lock()
	key := p.keyByIDLocked(id)
	if key == "" {
		p.mu.Unlock()
		return nil
	}
	if p.px != nil && len(p.stored.Keys) == 1 {
		p.mu.Unlock()
		return errors.New(l10n.L("The Key Pool needs at least one key while it is on. Add another key before removing this one."))
	}
	kept := p.stored.Keys[:0:0]
	for _, k := range p.stored.Keys {
		if k.Key != key {
			kept = append(kept, k)
		}
	}
	p.stored.Keys = kept
	if p.rotator != nil {
		p.rotator.RemoveKey(key)
	}
	if p.idle != nil {
		p.idle.RemoveKey(key)
	}
	var err error
	if p.stored.Current == id {
		p.stored.Current = ""
		next := p.currentKeyLocked()
		if p.rotator != nil {
			if want := p.rotator.GetActiveKey(); want != "" {
				next = want
			}
		}
		if next != "" && p.px != nil {
			err = p.useLocked(next)
		}
	}
	if err == nil {
		err = p.saveLocked()
	}
	p.mu.Unlock()
	p.changed()
	return err
}

// Use makes the key with id the current one at once.
func (p *Pool) Use(id string) error {
	p.mu.Lock()
	key := p.keyByIDLocked(id)
	if key == "" {
		p.mu.Unlock()
		return errors.New(l10n.L("This key is no longer in the Key Pool."))
	}
	var err error
	if p.px != nil {
		err = p.useLocked(key)
	} else {
		p.stored.Current = id
		err = p.saveLocked()
	}
	p.mu.Unlock()
	p.changed()
	return err
}

// SetMode switches between Auto and Manual.
func (p *Pool) SetMode(m Mode) error {
	if m != Manual {
		m = Auto
	}
	p.mu.Lock()
	p.stored.Mode = m
	err := p.saveLocked()
	p.mu.Unlock()
	p.changed()
	return err
}

// SetScheduledOpening turns the scheduled opening of waiting keys on or off
// and keeps it.
func (p *Pool) SetScheduledOpening(on bool) error {
	p.mu.Lock()
	p.stored.ScheduledOpening = on
	err := p.saveLocked()
	px := p.px
	p.mu.Unlock()
	if px != nil {
		px.SetKeyOpening(on)
	}
	p.changed()
	return err
}

// RefreshUsage reads every key's usage now.
func (p *Pool) RefreshUsage(ctx context.Context) {
	p.mu.Lock()
	r, running := p.readerLocked(), p.rotator != nil
	p.mu.Unlock()
	r.RefreshNow(ctx)
	if running {
		p.afterUsage()
		return
	}
	p.changed()
}

// readerLocked is the rotator that knows the keys' quota: the running one,
// else the idle one, made with the stored keys on first use.
func (p *Pool) readerLocked() *keys.KeyRotator {
	if p.rotator != nil {
		return p.rotator
	}
	if p.idle == nil {
		var list []string
		for _, k := range p.stored.Keys {
			list = append(list, k.Key)
		}
		p.idle = keys.NewKeyRotator(p.config(), list)
	}
	return p.idle
}

// SetCacheWarmingMode changes the cache warming mode and keeps it.
func (p *Pool) SetCacheWarmingMode(mode string) error {
	p.mu.Lock()
	w := p.warmer
	p.mu.Unlock()
	if w == nil {
		return errors.New(l10n.L("The Key Pool is not running"))
	}
	err := w.SetMode(mode)
	p.changed()
	return err
}

// RequestLog is the file the proxy logs every LLM request to, kept across
// restarts; it is read while the pool is off too.
func (p *Pool) RequestLog() string { return p.logFile("requests.jsonl") }

// Session is what the Key Pool has seen of Session id's model calls since
// it started, and the one under way for it (nil when none is); ok is false
// when it has seen none.
func (p *Pool) Session(id string) (calls diag.SessionCalls, live *diag.LiveCall, ok bool) {
	p.mu.Lock()
	px := p.px
	p.mu.Unlock()
	if px == nil || id == "" {
		return diag.SessionCalls{}, nil, false
	}
	calls, ok = px.Recorder().Session(id)
	for _, l := range px.Live() {
		// The newest, the conversation's own call over a side call (a title,
		// a compaction).
		if l.SessionID == id && (live == nil || l.CallType == "" || live.CallType != "") {
			live = &l
		}
	}
	return calls, live, ok || live != nil
}

// State is a snapshot for the window.
func (p *Pool) State() State {
	p.mu.Lock()
	st := State{Running: p.px != nil, Mode: p.stored.Mode, Err: p.err, ScheduledOpening: p.stored.ScheduledOpening}
	if cur := p.currentKeyLocked(); cur != "" {
		// The key the key file gets on the next Start, when none was chosen.
		st.CurrentID = keyID(cur)
	}
	stored := append([]storedKey(nil), p.stored.Keys...)
	r, px, w, rotator := p.readerLocked(), p.px, p.warmer, p.rotator
	p.mu.Unlock()
	byKey := map[string]keys.KeyState{}
	if r != nil {
		snap := r.Snapshot()
		for i, k := range r.Keys() {
			if i < len(snap) {
				byKey[k] = snap[i]
			}
		}
	}
	for i, k := range stored {
		ks, ok := byKey[k.Key]
		if !ok {
			ks = keys.KeyState{Index: i + 1, Key: keys.MaskKey(k.Key), Status: "no_usage_data"}
		}
		ks.Index = i + 1
		id := keyID(k.Key)
		ks.Active = id == st.CurrentID
		st.Keys = append(st.Keys, Key{ID: id, Masked: keys.MaskKey(k.Key), Current: ks.Active, AddedAt: k.AddedAt, State: ks})
	}
	if w != nil {
		st.CacheWarming = w.Status()
	}
	if px != nil {
		st.Events = px.Recorder().Events()
	}
	if rotator != nil {
		o := rotator.OpeningStatus()
		st.Opening = &o
	}
	return st
}

type noSessions struct{}

func (noSessions) ListOpenedSessions(context.Context) ([]proxy.OpenedSession, error) {
	return nil, errors.New("no Daemon")
}
