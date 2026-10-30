package host

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	droid "github.com/kkkk2323/droid-sdk-go"

	"github.com/kkkk2323/droi/apps/native/pkg/brand"
	"github.com/kkkk2323/droi/apps/native/pkg/droidhook"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/sessiontimes"
)

const defaultFactoryAPIBaseURL = "https://api.factory.ai"

// Config is what the Host needs from the app around it.
type Config struct {
	// UserData is the app's data directory: settings.json and logs.
	UserData string
	Home     string
	Version  string
	Env      func(string) string
	// Executable runs the Memory Server and the hook; "" leaves Memory out.
	Executable string
	// FactoryRoot, optional, gives the Daemon a Factory home of its own:
	// it runs with FACTORY_HOME_OVERRIDE=FactoryRoot, and the sign-in, the
	// Sessions and the settings are in FactoryRoot/.factory instead of the
	// droid CLI's ~/.factory.
	FactoryRoot string
	// ScratchFolder is where Scratch Workspaces go unless Settings names a
	// folder; "" is the brand's, ~/.droi/chats for Droi.
	ScratchFolder string
	// KeyPoolUpstream replaces Factory's API for the Key Pool (for tests).
	KeyPoolUpstream string
	// DroidDownloads replaces Factory's droid releases for Droi's droid
	// (for tests); LATEST is read next to it (droidhook.LatestURLFor).
	DroidDownloads string
	// ScriptModels reports the models Settings lets use Script in addition
	// to Factory's list; nil adds none.
	ScriptModels func() []string
	// Overlay, optional, is what else the Runtime Overlay attaches, read each
	// time the Daemon starts: MCP servers, hooks, environment and models.
	// RestartDaemon applies a change.
	Overlay func() OverlayOptions
	// AppendSystemPrompt, optional, follows the System Prompt Addition of
	// Settings, read at every Session start.
	AppendSystemPrompt func() string
}

// Host starts and watches the Daemon and supplies the Factory credential.
type Host struct {
	cfg      Config
	Settings *SettingsStore
	Auth     *FactoryAuth
	Daemon   *Supervisor
	// Times are the Session Times (ADR 0032), which the Gateway writes and
	// serves.
	Times *sessiontimes.Store

	adding *FactoryAuth

	mu      sync.Mutex
	readers map[string]*CliLoginReader
	build   *DroidBuild
	// hookReady is the file the running Daemon's hook checks in at, "" for
	// a Daemon without the hook.
	hookReady string
	updating  DroidUpdate
	listeners map[int]func()
	next      int
	wasIn     bool

	usageMu sync.Mutex
	usage   map[string]*usageEntry
	client  droidClientInfo

	// magpieMu guards the catalog, magpieReadMu makes one read of the gateway
	// run at a time, magpieReading marks a background read queued or running,
	// and magpieGivenCatalog is what the running Daemon was started with.
	magpieMu           sync.Mutex
	magpieReadMu       sync.Mutex
	magpie             MagpieCatalog
	magpieReading      bool
	magpieGivenCatalog MagpieCatalog

	indexMu  sync.Mutex
	index    *sessionIndex
	indexRev uint64

	droiDroid droiDroidState
}

// New makes a Host; Start launches the Daemon.
func New(cfg Config) *Host {
	if cfg.Env == nil {
		cfg.Env = os.Getenv
	}
	h := &Host{cfg: cfg, listeners: map[int]func(){}, readers: map[string]*CliLoginReader{}, usage: map[string]*usageEntry{}}
	h.Settings = OpenSettings(filepath.Join(cfg.UserData, "settings.json"), cfg.Env)
	timesFile := ""
	if cfg.UserData != "" {
		timesFile = filepath.Join(cfg.UserData, "session-times.json")
	}
	h.Times = sessiontimes.Open(timesFile, nil)
	h.Auth = &FactoryAuth{
		Load: func() string { return deref(h.Settings.Get().Login) },
		Save: func(s string) {
			_ = h.Settings.Update(func(st *Settings) { st.Login = OptString(s) })
		},
		FactoryAPIBaseURL: h.FactoryAPIBaseURL(),
	}
	h.Auth.OnChange = func(s LoginState) {
		h.changed()
		// Signing in moves the Daemon off the API key; its env is read at spawn.
		in := s.Status == SignedIn
		h.mu.Lock()
		flip := in != h.wasIn
		h.wasIn = in
		h.mu.Unlock()
		if flip {
			go h.RestartDaemon()
		}
	}
	h.adding = &FactoryAuth{OnChange: func(LoginState) { h.changed() }, Handoff: h.addAccount, FactoryAPIBaseURL: h.FactoryAPIBaseURL()}
	writeCli := func(access, refresh string) error { return WriteCliLogin(h.FactoryHome(), access, refresh) }
	h.Auth.Handoff = func(access, refresh string) error {
		if err := writeCli(access, refresh); err != nil {
			return err
		}
		h.changed()
		go h.RestartDaemon()
		return nil
	}
	// A login an earlier Droi kept for itself moves to the CLI when that has
	// none: the Daemon cannot authenticate without one.
	if h.Auth.State().Status == SignedIn && h.cli(h.FactoryHome()).Read() == nil {
		h.Auth.handOver(writeCli)
	}
	h.wasIn = h.Auth.State().Status == SignedIn
	h.Daemon = &Supervisor{Spawn: h.spawn, Check: h.checkDaemon, OnState: func(DaemonState) { h.changed() }}
	return h
}

// Start launches the Daemon, and the first time dates the Sessions by the
// messages sent before the Session Times were kept.
func (h *Host) Start() {
	h.Daemon.Start()
	h.watchDroiDroid()
	go func() { _ = h.Times.Backfill(filepath.Join(h.SharedFactoryHome(), "sessions")) }()
}

// Stop ends the Daemon and writes what the Session Times still hold.
func (h *Host) Stop() {
	h.stopWatchingDroiDroid()
	h.Daemon.Stop()
	h.Times.Flush()
}

// RestartDaemon starts a new Daemon from the droid on disk.
func (h *Host) RestartDaemon() {
	h.Daemon.Stop()
	h.mu.Lock()
	h.build = nil
	h.mu.Unlock()
	h.Daemon.Start()
}

// OnChange hears every change of what Snapshot reports.
func (h *Host) OnChange(fn func()) (unsubscribe func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := h.next
	h.next++
	h.listeners[id] = fn
	return func() {
		h.mu.Lock()
		delete(h.listeners, id)
		h.mu.Unlock()
	}
}

func (h *Host) changed() {
	h.mu.Lock()
	fns := make([]func(), 0, len(h.listeners))
	for _, fn := range h.listeners {
		fns = append(fns, fn)
	}
	h.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// Changed tells listeners the settings changed.
func (h *Host) Changed() { h.changed() }

// FactoryHome is the .factory folder of the account the Daemon runs as:
// ~/.factory for the droid CLI's own, the account's folder for another.
func (h *Host) FactoryHome() string { return h.homeOf(h.ActiveAccount()) }

// FactoryAPIBaseURL is the setting, else FACTORY_API_BASE_URL, else Factory's.
func (h *Host) FactoryAPIBaseURL() string {
	if u := deref(h.Settings.Get().FactoryAPIBaseURL); u != "" {
		return u
	}
	if u := h.cfg.Env("FACTORY_API_BASE_URL"); u != "" {
		return u
	}
	return defaultFactoryAPIBaseURL
}

// ScratchFolder is where Scratch Workspaces go (ADR 0008).
func (h *Host) ScratchFolder() string {
	if f := deref(h.Settings.Get().ScratchFolder); f != "" {
		return f
	}
	if h.cfg.ScratchFolder != "" {
		return h.cfg.ScratchFolder
	}
	return brand.ScratchFolder(h.cfg.Home)
}

// Executable is the app's own executable, which the Runtime Overlay runs as
// the Memory Server and the hook; "" under go run.
func (h *Host) Executable() string { return h.cfg.Executable }

// UserData is the app's data directory: settings.json and logs.
func (h *Host) UserData() string { return h.cfg.UserData }

// MemoryDir holds Memory's database, Markdown export and prompts (ADR
// 0010), the Electron Shell's folder.
func (h *Host) MemoryDir() string { return filepath.Join(h.cfg.UserData, "memory") }

// runtimeOverlay writes the Runtime Overlay when Memory or Magpie is on, or
// Config.Overlay adds something, and answers its path; "" starts the Daemon
// with none.
func (h *Host) runtimeOverlay() string {
	st := h.Settings.Get()
	approvedAt := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	var m MemoryAttachment
	if st.MemoryEnabled && h.cfg.Executable != "" {
		m = MemoryAttachment{
			Executable: h.cfg.Executable,
			MemoryDir:  h.MemoryDir(),
			ApprovedAt: approvedAt,
		}
	}
	var models []any
	given := MagpieCatalog{}
	if st.MagpieEnabled {
		given = h.magpieModels()
		models = MagpieCustomModels(given)
	}
	o := OverlayOptions{CustomModels: models, ApprovedAt: approvedAt}
	if h.cfg.Overlay != nil {
		o = o.Add(h.cfg.Overlay())
	}
	if m.Executable != "" || !o.empty() {
		path := filepath.Join(h.cfg.UserData, "runtime-overlay.json")
		if err := WriteRuntimeOverlay(path, BuildRuntimeOverlayWith(m, o)); err != nil {
			// Nothing reached the Daemon, models included, so a Refresh has
			// something to hand it.
			h.rememberMagpieGiven(MagpieCatalog{})
			return ""
		}
		h.rememberMagpieGiven(given)
		return path
	}
	// Nothing to hand over: the Daemon runs without an overlay and gets no
	// Magpie models. It gets them from this gateway though, as the record says,
	// so reading the same empty list again is not a change.
	h.rememberMagpieGiven(given)
	return ""
}

// DaemonLogPath is where the Daemon's output goes.
func (h *Host) DaemonLogPath() string {
	return filepath.Join(h.cfg.UserData, "logs", "daemon.log")
}

// DroidPath is the droid the next Daemon starts from; "" when none.
func (h *Host) DroidPath() string {
	if h.usesDroiDroid() {
		return h.droiDroidPath()
	}
	return LocateDroid(deref(h.Settings.Get().DroidPath), h.cfg.Env("PATH"), h.cfg.Home)
}

// DroidVersion is what `droid --version` prints for DroidPath; "" when
// it cannot be read.
func (h *Host) DroidVersion() string {
	v, _ := h.droidClient()
	return v
}

// DroidUpdate is where UpdateDroid stands. Err is why the last one left
// droid older than MinDroid.
type DroidUpdate struct {
	Running bool
	Err     string
}

// DroidUpdateState is the update UpdateDroid runs or last ran.
func (h *Host) DroidUpdateState() DroidUpdate {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.updating
}

// UpdateDroid runs `droid update` on DroidPath and, once droid is at least
// MinDroid, starts the Daemon from it.
func (h *Host) UpdateDroid() {
	// Droi's droid is replaced by preparing a newer one, never by droid's
	// own update.
	if h.usesDroiDroid() {
		h.UpdateDroiDroid()
		return
	}
	h.mu.Lock()
	if h.updating.Running {
		h.mu.Unlock()
		return
	}
	h.updating = DroidUpdate{Running: true}
	h.mu.Unlock()
	h.changed()

	path := h.DroidPath()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "update")
	hideConsole(cmd)
	// A Daemon gives its Sessions' commands FACTORY_DROID_AUTO_UPDATE_ENABLED=false,
	// and with it `droid update` refuses as if droid came from npm.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "FACTORY_DROID_AUTO_UPDATE_ENABLED=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	out, err := cmd.CombinedOutput()
	why := ""
	if v := h.DroidVersion(); OlderThanMinDroid(v) {
		why = lastLine(string(out))
		if why == "" && err != nil {
			why = err.Error()
		}
		if why == "" {
			why = l10n.L("droid is still %s.", v)
		}
	}
	h.mu.Lock()
	h.updating = DroidUpdate{Err: why}
	h.mu.Unlock()
	h.changed()
	if why == "" {
		h.RestartDaemon()
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// DroidUpdated reports that droid changed on disk since the Daemon started.
func (h *Host) DroidUpdated() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.build.Replaced()
}

func (h *Host) spawn(port int) (*exec.Cmd, error) {
	droi := h.usesDroiDroid()
	path := h.DroidPath()
	if path == "" {
		if droi {
			return nil, h.errNoDroiDroid()
		}
		return nil, errNoDroid
	}
	if v, _ := h.droidClient(); OlderThanMinDroid(v) {
		return nil, errDroidTooOld
	}
	var droiEnv []string
	ready := ""
	if droi {
		var err error
		if droiEnv, ready, err = h.droiDroidEnv(); err != nil {
			return nil, err
		}
	} else {
		h.KeyPool().Stop()
	}
	// Signed in: the Daemon runs as the account's login. Without one an API
	// key serves both sides.
	apiKey := ""
	if h.LoginState().Status != SignedIn {
		apiKey = h.Settings.APIKey()
	}
	// Only a setting droid may honor later: failing to write it must not
	// keep the Daemon from starting.
	_ = h.shareSessionVisibility()
	override := ""
	if id := h.ActiveAccount(); id != "" {
		if err := shareFactoryHome(h.homeOf(id), h.SharedFactoryHome()); err != nil {
			return nil, err
		}
		override = h.accountRoot(id)
	} else if h.cfg.FactoryRoot != "" {
		override = h.cfg.FactoryRoot
	}
	baseURL := deref(h.Settings.Get().FactoryAPIBaseURL)
	if baseURL == "" {
		baseURL = h.cfg.Env("FACTORY_API_BASE_URL")
	}
	h.mu.Lock()
	h.build = BuildOf(path)
	h.hookReady = ready
	h.mu.Unlock()

	var liveness []string
	var extra []*os.File
	var pipeW *os.File
	if runtime.GOOS == "windows" {
		liveness = []string{"--parent-pid", itoaPID()}
	} else {
		// The Daemon exits when the Host dies: fd 3 closes the moment this
		// process ends, which PID polling can mistake through PID reuse.
		r, w, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		extra = []*os.File{r}
		pipeW = w
		liveness = []string{"--liveness-fd", "3"}
	}
	cmd := exec.Command(path, DaemonArgs(port, liveness, h.runtimeOverlay())...)
	cmd.ExtraFiles = extra
	hideConsole(cmd)
	env := withoutEnv(os.Environ(), droidhook.KeyFileEnv, droidhook.ReadyEnv)
	if apiKey != "" {
		env = append(env, "FACTORY_API_KEY="+apiKey)
	}
	if droi {
		env = append(withoutEnv(env, "FACTORY_API_BASE_URL", "FACTORY_DROID_AUTO_UPDATE_ENABLED", "BUN_OPTIONS"), droiEnv...)
	} else if baseURL != "" {
		env = append(env, "FACTORY_API_BASE_URL="+baseURL)
	}
	if override != "" {
		env = append(env, "FACTORY_HOME_OVERRIDE="+override)
	}
	cmd.Env = env
	if log := OpenDaemonLog(h.DaemonLogPath()); log != nil {
		cmd.Stdout, cmd.Stderr = log, log
		defer log.Close()
	}
	if err := cmd.Start(); err != nil {
		if pipeW != nil {
			pipeW.Close()
		}
		for _, f := range extra {
			f.Close()
		}
		return nil, err
	}
	for _, f := range extra {
		f.Close()
	}
	if pipeW != nil {
		// Held for the life of the process; it closes when the Host exits.
		keepAlive(pipeW)
	}
	return cmd, nil
}

var (
	keptMu sync.Mutex
	kept   []*os.File
)

func keepAlive(f *os.File) {
	keptMu.Lock()
	defer keptMu.Unlock()
	kept = append(kept, f)
}

type hostError string

func (e hostError) Error() string { return string(e) }

const (
	errNoDroid     = hostError("droid executable not found")
	errDroidTooOld = hostError("droid is older than MinDroid")
)

func itoaPID() string {
	b, _ := json.Marshal(os.Getpid())
	return string(b)
}

// LoginState is Droi's own sign-in when there is one, else the CLI's.
func (h *Host) LoginState() LoginState {
	s := h.Auth.State()
	if s.Status != SignedOut {
		return s
	}
	if cli := h.cli(h.FactoryHome()).Read(); cli != nil {
		acc := cli.Account
		return LoginState{Status: SignedIn, Account: &acc, Source: "cli"}
	}
	return s
}

// Credential is what daemon.authenticate gets: a sign-in done in Droi,
// then the droid CLI's login, then an API key for automation.
func (h *Host) Credential(ctx context.Context) (*droid.Credential, error) {
	if tok, err := h.Auth.AccessToken(ctx); err == nil && tok != "" {
		return &droid.Credential{Token: tok}, nil
	}
	if cli := h.cli(h.FactoryHome()).Read(); cli != nil {
		return &droid.Credential{Token: cli.AccessToken}, nil
	}
	if k := h.Settings.APIKey(); k != "" {
		return &droid.Credential{APIKey: k}, nil
	}
	return nil, nil
}

// SignOut signs the account in use out, and so the droid CLI with it when
// that is the CLI's own; the Daemon restarts without a login.
func (h *Host) SignOut() error {
	err := RemoveCliLogin(h.FactoryHome())
	if h.Auth.State().Status == SignedIn {
		h.Auth.SignOut() // its OnChange restarts the Daemon
	} else {
		h.changed()
		go h.RestartDaemon()
	}
	return err
}

// SetAPIKey stores key, "" removing it, and starts the Daemon again on it
// unless a login is in use: an app that hands the Host its own key, from its
// own sign-in page, say.
func (h *Host) SetAPIKey(key string) error {
	if err := h.Settings.Update(func(st *Settings) { st.APIKey = OptString(key) }); err != nil {
		return err
	}
	h.changed()
	if h.LoginState().Status != SignedIn {
		go h.RestartDaemon()
	}
	return nil
}

// HasCredential reports that there is something to authenticate with.
func (h *Host) HasCredential() bool {
	return h.LoginState().Status == SignedIn || h.Settings.APIKey() != ""
}

// WaitForDaemonURL blocks until the Daemon runs, or ctx ends.
func (h *Host) WaitForDaemonURL(ctx context.Context) (string, error) {
	for {
		if u := h.Daemon.State().URL(); u != "" {
			return u, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
