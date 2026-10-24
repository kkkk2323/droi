package host

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/droidpatch"
	"github.com/kkkk2323/droi/apps/native/pkg/keypool"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/proxy"
)

// DroidPatched is Settings.DroidKind for the Patched Droid (ADR 0027).
const DroidPatched = "patched"

// PatchedDroid is where the Patched Droid stands, for Settings.
type PatchedDroid struct {
	// Enabled: the Daemon starts from the Patched Droid.
	Enabled bool
	// Version is the Patched Droid in use; Latest the newest one a recipe
	// exists for on this computer ("" when none).
	Version, Latest string
	// Supported: some recipe fits this computer's platform.
	Supported bool
	// Preparing: a download or a patch runs; Done/Total count the bytes.
	Preparing   bool
	Done, Total int64
	Err         string
	// Checking: a check for newer recipes runs (CheckPatchedDroid);
	// Checked: the last one read them; CheckErr says why it did not.
	Checking, Checked bool
	CheckErr          string
}

// UpdateAvailable reports a newer Patched Droid than the one in use.
func (p PatchedDroid) UpdateAvailable() bool {
	return p.Enabled && p.Latest != "" && droidpatch.CompareVersions(p.Latest, p.Version) > 0
}

type patchState struct {
	once      sync.Once
	store     *droidpatch.Store
	pool      *keypool.Pool
	mu        sync.Mutex
	preparing bool
	progress  droidpatch.Progress
	err       string
	refreshed time.Time
	checking  bool
	checked   bool
	checkErr  string
	// watch starts the hourly read of the published recipes once; stop
	// ends it.
	watch, stopped, stopMade sync.Once
	stop                     chan struct{}
}

func (h *Host) patches() *patchState {
	h.patch.once.Do(func() {
		h.patch.store = droidpatch.NewStore(filepath.Join(h.cfg.UserData, "droid"))
		h.patch.pool = keypool.Open(keypool.Options{
			Dir:      filepath.Join(h.cfg.UserData, "llm"),
			Upstream: h.keyPoolUpstream(),
			Sessions: daemonSessions{h},
			OnChange: h.changed,
		})
	})
	return &h.patch
}

// KeyPool is the Key Pool the Patched Droid's LLM requests use.
func (h *Host) KeyPool() *keypool.Pool { return h.patches().pool }

func (h *Host) usesPatchedDroid() bool { return h.Settings.Get().DroidKind == DroidPatched }

// PatchedDroidState is where the Patched Droid stands.
func (h *Host) PatchedDroidState() PatchedDroid {
	p := h.patches()
	st := h.Settings.Get()
	out := PatchedDroid{Enabled: st.DroidKind == DroidPatched, Version: st.PatchedDroidVersion}
	if rs := p.store.Recipes(); len(rs) > 0 {
		out.Supported, out.Latest = true, rs[0].Version
	}
	p.mu.Lock()
	out.Preparing, out.Done, out.Total, out.Err = p.preparing, p.progress.Done, p.progress.Total, p.err
	out.Checking, out.Checked, out.CheckErr = p.checking, p.checked, p.checkErr
	p.mu.Unlock()
	return out
}

func (h *Host) patchedDroidPath() string {
	v := h.Settings.Get().PatchedDroidVersion
	if v == "" || !h.patches().store.Ready(v) {
		return ""
	}
	return h.patches().store.Path(v)
}

func errNoPatchedDroid() error {
	return hostError(l10n.L("Droi's droid is missing. Turn the Key Pool off and on again in Settings → Advanced."))
}

// EnablePatchedDroid prepares the newest Patched Droid (downloading
// Factory's build when this computer's droid is another version), then
// starts the Daemon from it. With an empty Key Pool the Daemon waits for a
// key (AddPoolKey), since the patch never falls back to the signed-in
// account.
func (h *Host) EnablePatchedDroid() { go h.preparePatchedDroid(true) }

// UpdatePatchedDroid moves to the newest Patched Droid and restarts the
// Daemon.
func (h *Host) UpdatePatchedDroid() { go h.preparePatchedDroid(true) }

// DisablePatchedDroid goes back to the official droid.
func (h *Host) DisablePatchedDroid() {
	_ = h.Settings.Update(func(st *Settings) { st.DroidKind = "" })
	h.changed()
	go func() {
		h.RestartDaemon()
		h.KeyPool().Stop()
	}()
}

func (h *Host) setPatchErr(err string) {
	p := h.patches()
	p.mu.Lock()
	p.preparing, p.err = false, err
	p.mu.Unlock()
	h.changed()
}

func (h *Host) preparePatchedDroid(restart bool) {
	p := h.patches()
	p.mu.Lock()
	if p.preparing {
		p.mu.Unlock()
		return
	}
	p.preparing, p.err, p.progress = true, "", droidpatch.Progress{}
	p.mu.Unlock()
	h.changed()

	if msg := h.baseURLConflict(); msg != "" {
		h.setPatchErr(msg)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	// A failed fetch keeps the recipes already known.
	_ = p.store.RefreshRecipes(ctx)
	rs := p.store.Recipes()
	if len(rs) == 0 {
		h.setPatchErr(l10n.L("The Key Pool is not available on this computer (%s) yet. It works on macOS and 64-bit Windows for now.", p.store.Platform))
		return
	}
	r := rs[0]
	if OlderThanMinDroid(r.Version) {
		h.setPatchErr(l10n.L("Droi's droid is only available as droid %s, but Droi needs %s or newer. Try again once a newer version is supported.", r.Version, MinDroid))
		return
	}
	local := LocateDroid(deref(h.Settings.Get().DroidPath), h.cfg.Env("PATH"), h.cfg.Home)
	_, err := p.store.Prepare(ctx, r, local, func(pr droidpatch.Progress) {
		p.mu.Lock()
		p.progress = pr
		p.mu.Unlock()
		h.changed()
	})
	if err != nil {
		h.setPatchErr(l10n.L("Droi's droid is not ready: %s", strings.TrimPrefix(err.Error(), "droidpatch: ")))
		return
	}
	old := h.Settings.Get().PatchedDroidVersion
	_ = h.Settings.Update(func(st *Settings) {
		st.DroidKind = DroidPatched
		st.PatchedDroidVersion = r.Version
	})
	h.setPatchErr("")
	if restart {
		h.RestartDaemon()
	}
	if old != "" && old != r.Version {
		p.store.Prune(r.Version)
	}
}

// AddPoolKey adds key to the Key Pool. The first key starts a Daemon that
// was waiting for one: the Patched Droid can be turned on with an empty
// pool, and its Daemon retries with a growing delay until then.
func (h *Host) AddPoolKey(ctx context.Context, key string) error {
	pool := h.KeyPool()
	first := pool.Len() == 0
	if err := pool.Add(ctx, key); err != nil {
		return err
	}
	if first && h.usesPatchedDroid() && h.Daemon.State().Status == DaemonRestarting {
		go h.RestartDaemon()
	}
	return nil
}

// RefreshPatchRecipes fetches the published recipes at most once an hour,
// so Settings can offer a newer Patched Droid.
func (h *Host) RefreshPatchRecipes() {
	p := h.patches()
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
		if p.store.RefreshRecipes(ctx) == nil {
			h.changed()
		}
	}()
}

// CheckPatchedDroid fetches the published recipes now, however recently
// they were read, so Settings can say whether a newer Patched Droid exists.
func (h *Host) CheckPatchedDroid() {
	p := h.patches()
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
		err := p.store.RefreshRecipes(ctx)
		p.mu.Lock()
		p.checking, p.checked = false, err == nil
		if err != nil {
			p.checkErr = l10n.L("Droi could not check for a newer droid: %s", strings.TrimPrefix(err.Error(), "droidpatch: "))
		}
		p.mu.Unlock()
		h.changed()
	}()
}

// watchPatchRecipes reads the published recipes every hour while the
// Patched Droid runs, so the window offers a newer one without Settings
// being open.
func (h *Host) watchPatchRecipes() {
	p := h.patches()
	stop := p.stopChan()
	p.watch.Do(func() {
		go func() {
			t := time.NewTicker(time.Hour)
			defer t.Stop()
			for {
				if h.usesPatchedDroid() {
					h.RefreshPatchRecipes()
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

func (h *Host) stopWatchingPatchRecipes() {
	p := &h.patch
	stop := p.stopChan()
	p.stopped.Do(func() { close(stop) })
}

func (p *patchState) stopChan() chan struct{} {
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

// patchedEnv is what the Daemon gets on top of its environment when it runs
// the Patched Droid: the Key Pool's proxy as the Factory API, the key file,
// and no self-update, which would replace the patched file with Factory's.
func (h *Host) patchedEnv() ([]string, error) {
	pool := h.KeyPool()
	if msg := h.baseURLConflict(); msg != "" {
		return nil, hostError(msg)
	}
	pool.Configure(h.keyPoolUpstream(), h.FactoryHome())
	base, err := pool.Start()
	if errors.Is(err, keypool.ErrNoKeys) {
		return nil, hostError(l10n.L("The Key Pool has no key yet. Add one in Settings → Usage, and the Daemon starts."))
	}
	if err != nil {
		return nil, err
	}
	return []string{
		"FACTORY_API_BASE_URL=" + base,
		droidpatch.KeyFileEnv + "=" + pool.KeyFile(),
		"FACTORY_DROID_AUTO_UPDATE_ENABLED=false",
	}, nil
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
