package host

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/droidhook"
	"github.com/kkkk2323/droi/apps/native/pkg/keypool"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/proxy"
)

// DroidDroi is Settings.DroidKind for Droi's droid (ADR 0033).
const DroidDroi = "droi"

// droidPatched is the kind of the Patched Droid (ADR 0027), which Droi's
// droid replaced: a Host that ran it runs Droi's droid.
const droidPatched = "patched"

// DroiDroid is where Droi's droid stands, for Settings: Factory's own droid,
// which the Daemon runs with Droi's LLM key hook while the Key Pool is on.
type DroiDroid struct {
	// Enabled: the Daemon starts from Droi's droid.
	Enabled bool
	// Version is Droi's droid in use; Latest the newest droid Factory
	// offers, "" until Droi has asked.
	Version, Latest string
	// Supported: Factory builds droid for this computer.
	Supported bool
	// Preparing: a download or a check of the hook runs; Done/Total count
	// the bytes.
	Preparing   bool
	Done, Total int64
	Err         string
	// Checking: a check for a newer droid runs (CheckDroiDroid); Checked:
	// the last one got an answer; CheckErr says why it did not.
	Checking, Checked bool
	CheckErr          string
}

// PatchedDroid is DroiDroid's old name.
//
// Deprecated: use DroiDroid.
type PatchedDroid = DroiDroid

// UpdateAvailable reports a newer droid than the one in use.
func (p DroiDroid) UpdateAvailable() bool {
	return p.Enabled && p.Latest != "" && droidhook.CompareVersions(p.Latest, p.Version) > 0
}

type droiDroidState struct {
	once      sync.Once
	store     *droidhook.Store
	pool      *keypool.Pool
	mu        sync.Mutex
	preparing bool
	progress  droidhook.Progress
	err       string
	latest    string
	refreshed time.Time
	checking  bool
	checked   bool
	checkErr  string
	// watch starts the hourly check for a newer droid once; stop ends it.
	watch, stopped, stopMade sync.Once
	stop                     chan struct{}
}

func (h *Host) droi() *droiDroidState {
	h.droiDroid.once.Do(func() {
		h.droiDroid.store = droidhook.NewStore(filepath.Join(h.cfg.UserData, "droid"))
		if h.cfg.DroidDownloads != "" {
			h.droiDroid.store.DownloadBase = h.cfg.DroidDownloads
			h.droiDroid.store.LatestURL = droidhook.LatestURLFor(h.cfg.DroidDownloads)
		}
		h.droiDroid.pool = keypool.Open(keypool.Options{
			Dir:          filepath.Join(h.cfg.UserData, "llm"),
			Upstream:     h.keyPoolUpstream(),
			Sessions:     daemonSessions{h},
			OnChange:     h.changed,
			ScriptModels: h.cfg.ScriptModels,
		})
	})
	return &h.droiDroid
}

// KeyPool is the Key Pool whose keys Droi's droid sends its LLM requests
// with.
func (h *Host) KeyPool() *keypool.Pool { return h.droi().pool }

func (h *Host) usesDroiDroid() bool {
	k := h.Settings.Get().DroidKind
	return k == DroidDroi || k == droidPatched
}

// DroiDroidState is where Droi's droid stands.
func (h *Host) DroiDroidState() DroiDroid {
	p := h.droi()
	out := DroiDroid{Enabled: h.usesDroiDroid(), Version: h.Settings.Get().DroiDroidVersion, Supported: droidhook.Supported(p.store.Platform)}
	p.mu.Lock()
	out.Preparing, out.Done, out.Total, out.Err = p.preparing, p.progress.Done, p.progress.Total, p.err
	out.Checking, out.Checked, out.CheckErr, out.Latest = p.checking, p.checked, p.checkErr, p.latest
	p.mu.Unlock()
	return out
}

// PatchedDroidState is DroiDroidState's old name.
//
// Deprecated: use DroiDroidState.
func (h *Host) PatchedDroidState() DroiDroid { return h.DroiDroidState() }

func (h *Host) droiDroidPath() string {
	v := h.Settings.Get().DroiDroidVersion
	if v == "" || !h.droi().store.Ready(v) {
		return ""
	}
	return h.droi().store.Path(v)
}

func (h *Host) errNoDroiDroid() error {
	if h.DroiDroidState().Preparing {
		return hostError(l10n.L("Droi is preparing its droid. The Daemon starts once it is ready."))
	}
	return hostError(l10n.L("Droi's droid is missing. Turn the Key Pool off and on again in Settings → Advanced."))
}

func errHookNotLoaded() error {
	return hostError(l10n.L("Droi's key hook did not load in droid, so the Daemon was stopped before a model request could go out without a Key Pool key. Check BUN_OPTIONS in Droi's environment, then restart the Daemon."))
}

// EnableDroiDroid prepares Factory's newest droid (copied from this
// computer's droid when that is the same build, else downloaded) and checks
// that the hook runs in it, then starts the Daemon from it. With an empty
// Key Pool the Daemon waits for a key (AddPoolKey), since the hook never
// falls back to the signed-in account.
func (h *Host) EnableDroiDroid() { go h.prepareDroiDroid(true) }

// EnablePatchedDroid is EnableDroiDroid's old name.
//
// Deprecated: use EnableDroiDroid.
func (h *Host) EnablePatchedDroid() { h.EnableDroiDroid() }

// UpdateDroiDroid moves to Factory's newest droid and restarts the Daemon.
func (h *Host) UpdateDroiDroid() { go h.prepareDroiDroid(true) }

// DisableDroiDroid goes back to the computer's droid.
func (h *Host) DisableDroiDroid() {
	_ = h.Settings.Update(func(st *Settings) { st.DroidKind = "" })
	h.changed()
	go func() {
		h.RestartDaemon()
		h.KeyPool().Stop()
	}()
}

func (h *Host) setDroiErr(err string) {
	p := h.droi()
	p.mu.Lock()
	p.preparing, p.err = false, err
	p.mu.Unlock()
	h.changed()
}

func (h *Host) prepareDroiDroid(restart bool) {
	p := h.droi()
	p.mu.Lock()
	if p.preparing {
		p.mu.Unlock()
		return
	}
	p.preparing, p.err, p.progress = true, "", droidhook.Progress{}
	p.mu.Unlock()
	h.changed()

	if msg := h.baseURLConflict(); msg != "" {
		h.setDroiErr(msg)
		return
	}
	if !droidhook.Supported(p.store.Platform) {
		h.setDroiErr(l10n.L("The Key Pool is not available on this computer (%s): Factory builds droid for macOS, Linux and 64-bit Windows.", p.store.Platform))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	local := LocateDroid(deref(h.Settings.Get().DroidPath), h.cfg.Env("PATH"), h.cfg.Home)
	version, err := p.store.Latest(ctx)
	if err == nil {
		p.mu.Lock()
		p.latest = version
		p.mu.Unlock()
	} else if version = droidVersionAt(local); version == "" {
		h.setDroiErr(l10n.L("Droi could not find Factory's newest droid: %s", strings.TrimPrefix(err.Error(), "droidhook: ")))
		return
	}
	if OlderThanMinDroid(version) {
		h.setDroiErr(l10n.L("Factory's newest droid is %s, but Droi needs %s or newer.", version, MinDroid))
		return
	}
	_, err = p.store.Prepare(ctx, version, local, func(pr droidhook.Progress) {
		p.mu.Lock()
		p.progress = pr
		p.mu.Unlock()
		h.changed()
	})
	if err != nil {
		h.setDroiErr(l10n.L("Droi's droid is not ready: %s", strings.TrimPrefix(err.Error(), "droidhook: ")))
		return
	}
	_ = h.Settings.Update(func(st *Settings) {
		st.DroidKind = DroidDroi
		st.DroiDroidVersion = version
	})
	h.setDroiErr("")
	if restart {
		h.RestartDaemon()
	}
	p.store.Prune(version)
}

// droidVersionAt is what `droid --version` prints for path, "" when it
// cannot be read.
func droidVersionAt(path string) string {
	if path == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	hideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if _, ok := droidSemver(v); !ok {
		return ""
	}
	return v
}

// AddPoolKey adds key to the Key Pool. The first key starts a Daemon that
// was waiting for one: Droi's droid can be turned on with an empty pool,
// and its Daemon retries with a growing delay until then.
func (h *Host) AddPoolKey(ctx context.Context, key string) error {
	pool := h.KeyPool()
	first := pool.Len() == 0
	if err := pool.Add(ctx, key); err != nil {
		return err
	}
	if first && h.usesDroiDroid() && h.Daemon.State().Status == DaemonRestarting {
		go h.RestartDaemon()
	}
	return nil
}

// RefreshDroiDroid asks Factory for its newest droid at most once an hour,
// so Settings can offer it.
func (h *Host) RefreshDroiDroid() {
	p := h.droi()
	p.mu.Lock()
	if time.Since(p.refreshed) < time.Hour {
		p.mu.Unlock()
		return
	}
	p.refreshed = time.Now()
	p.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if v, err := p.store.Latest(ctx); err == nil {
			p.mu.Lock()
			p.latest = v
			p.mu.Unlock()
			h.changed()
		}
	}()
}

// CheckDroiDroid asks Factory for its newest droid now, however recently it
// asked, so Settings can say whether a newer one exists.
func (h *Host) CheckDroiDroid() {
	p := h.droi()
	p.mu.Lock()
	if p.checking {
		p.mu.Unlock()
		return
	}
	p.checking, p.checked, p.checkErr = true, false, ""
	p.refreshed = time.Now()
	p.mu.Unlock()
	h.changed()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		v, err := p.store.Latest(ctx)
		p.mu.Lock()
		p.checking, p.checked = false, err == nil
		if err != nil {
			p.checkErr = l10n.L("Droi could not check for a newer droid: %s", strings.TrimPrefix(err.Error(), "droidhook: "))
		} else {
			p.latest = v
		}
		p.mu.Unlock()
		h.changed()
	}()
}

// watchDroiDroid asks for a newer droid every hour while Droi's droid runs,
// so the window offers it without Settings being open. A Host whose droid
// is missing, such as one that ran the Patched Droid, prepares it first.
func (h *Host) watchDroiDroid() {
	p := h.droi()
	stop := p.stopChan()
	p.watch.Do(func() {
		go func() {
			if h.usesDroiDroid() && h.droiDroidPath() == "" {
				h.prepareDroiDroid(true)
			}
			t := time.NewTicker(time.Hour)
			defer t.Stop()
			for {
				if h.usesDroiDroid() {
					h.RefreshDroiDroid()
				}
				select {
				case <-t.C:
				case <-stop:
					return
				}
			}
		}()
	})
}

func (h *Host) stopWatchingDroiDroid() {
	p := &h.droiDroid
	stop := p.stopChan()
	p.stopped.Do(func() { close(stop) })
}

func (p *droiDroidState) stopChan() chan struct{} {
	p.stopMade.Do(func() { p.stop = make(chan struct{}) })
	return p.stop
}

// keyPoolUpstream is the Factory API the Key Pool forwards to and reads
// quota from: Factory's, never Settings' base URL (baseURLConflict).
func (h *Host) keyPoolUpstream() string {
	if h.cfg.KeyPoolUpstream != "" {
		return h.cfg.KeyPoolUpstream
	}
	return defaultFactoryAPIBaseURL
}

// baseURLConflict says why the Key Pool cannot run while a Factory API base
// URL is set: a droid-proxy there would put its own keys on the requests the
// Key Pool chose keys for, and its numbers would no longer be the keys'.
func (h *Host) baseURLConflict() string {
	same := func(u string) bool { return u == "" || strings.TrimRight(u, "/") == defaultFactoryAPIBaseURL }
	if u := deref(h.Settings.Get().FactoryAPIBaseURL); !same(u) {
		return l10n.L("The Key Pool cannot be used while Factory API base URL is set (%s). Clear it below, then turn the Key Pool on.", u)
	}
	if u := h.cfg.Env("FACTORY_API_BASE_URL"); !same(u) {
		return l10n.L("The Key Pool cannot be used while FACTORY_API_BASE_URL is set (%s). Open Droi without that variable, then turn the Key Pool on.", u)
	}
	return ""
}

// droiDroidEnv is what the Daemon gets on top of its environment when it
// runs Droi's droid: the Key Pool's proxy as the Factory API, the hook with
// its key file and ready file, and no self-update, which would replace the
// verified build. ready is the file the hook checks in at.
func (h *Host) droiDroidEnv() (env []string, ready string, err error) {
	pool := h.KeyPool()
	if msg := h.baseURLConflict(); msg != "" {
		return nil, "", hostError(msg)
	}
	hook, err := h.droi().store.Hook()
	if err != nil {
		return nil, "", err
	}
	pool.Configure(h.keyPoolUpstream(), h.FactoryHome())
	base, err := pool.Start()
	if errors.Is(err, keypool.ErrNoKeys) {
		return nil, "", hostError(l10n.L("The Key Pool has no key yet. Add one in Settings → Usage, and the Daemon starts."))
	}
	if err != nil {
		return nil, "", err
	}
	ready = filepath.Join(h.droi().store.Dir, "hook-ready.json")
	_ = os.Remove(ready)
	env = []string{"FACTORY_API_BASE_URL=" + base, "FACTORY_DROID_AUTO_UPDATE_ENABLED=false"}
	return append(env, droidhook.Env(hook, pool.KeyFile(), ready, os.Getenv("BUN_OPTIONS"))...), ready, nil
}

// checkDaemon runs once the Daemon answers its health check: Droi's droid
// must have run the hook.
func (h *Host) checkDaemon(pid int) error {
	h.mu.Lock()
	ready := h.hookReady
	h.mu.Unlock()
	if ready == "" {
		return nil
	}
	if droidhook.CheckReady(ready, pid) != nil {
		return errHookNotLoaded()
	}
	return nil
}

// withoutEnv drops the variables named in names from env.
func withoutEnv(env []string, names ...string) []string {
	out := env[:0:0]
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, n := range names {
			if name == n {
				drop = true
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// daemonSessions lists the Daemon's open Sessions for cache warming.
type daemonSessions struct{ h *Host }

func (d daemonSessions) ListOpenedSessions(ctx context.Context) ([]proxy.OpenedSession, error) {
	url := d.h.Daemon.State().URL()
	if url == "" {
		return nil, errors.New("the Daemon is not running")
	}
	cred, err := d.h.Credential(ctx)
	if err != nil {
		return nil, err
	}
	c, err := droid.Dial(ctx, droid.Options{URL: url, Credential: cred, Caller: "droi-native"})
	if err != nil {
		return nil, err
	}
	defer c.Close()
	res, err := c.ListOpenedSessions(ctx, protocol.ListOpenedSessionsParams{})
	if err != nil {
		return nil, err
	}
	out := make([]proxy.OpenedSession, 0, len(res.Sessions))
	for _, s := range res.Sessions {
		out = append(out, proxy.OpenedSession{SessionID: s.SessionID, UpdatedAt: int64(s.UpdatedAt), WorkingState: string(s.WorkingState), Cwd: s.Cwd})
	}
	return out, nil
}
