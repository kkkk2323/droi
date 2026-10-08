package host

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
)

const defaultFactoryAPIBaseURL = "https://api.factory.ai"

// Config is what the Host needs from the app around it.
type Config struct {
	// UserData is the app's data directory: settings.json and logs.
	UserData string
	Home     string
	Version  string
	// MoveToTrash moves a Scratch Workspace with files to the Trash.
	MoveToTrash func(path string) error
	Env         func(string) string
	// Executable runs the Memory Server and the hook; "" leaves Memory out.
	Executable string
}

// Host starts and watches the Daemon and supplies the Factory credential.
type Host struct {
	cfg      Config
	Settings *SettingsStore
	Auth     *FactoryAuth
	Daemon   *Supervisor
	Scratch  *Scratch

	adding *FactoryAuth

	mu        sync.Mutex
	readers   map[string]*CliLoginReader
	build     *DroidBuild
	listeners map[int]func()
	next      int
	wasIn     bool

	usageMu sync.Mutex
	usage   map[string]*usageEntry
	client  droidClientInfo
}

// New makes a Host; Start launches the Daemon.
func New(cfg Config) *Host {
	if cfg.Env == nil {
		cfg.Env = os.Getenv
	}
	h := &Host{cfg: cfg, listeners: map[int]func(){}, readers: map[string]*CliLoginReader{}, usage: map[string]*usageEntry{}}
	h.Settings = OpenSettings(filepath.Join(cfg.UserData, "settings.json"), cfg.Env)
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
	h.adding = &FactoryAuth{OnChange: func(LoginState) { h.changed() }, Handoff: h.addAccount}
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
	h.Scratch = &Scratch{Root: h.ScratchFolder, MoveToTrash: cfg.MoveToTrash}
	h.Daemon = &Supervisor{Spawn: h.spawn, OnState: func(DaemonState) { h.changed() }}
	return h
}

// Start launches the Daemon.
func (h *Host) Start() { h.Daemon.Start() }

// Stop ends the Daemon.
func (h *Host) Stop() { h.Daemon.Stop() }

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
	return filepath.Join(h.cfg.Home, ".droi", "chats")
}

// MemoryDir holds Memory's database, Markdown export and prompts (ADR
// 0010), the Electron Shell's folder.
func (h *Host) MemoryDir() string { return filepath.Join(h.cfg.UserData, "memory") }

// runtimeOverlay writes the Runtime Overlay when Memory is on and answers
// its path; "" starts the Daemon without one, and so without Memory.
func (h *Host) runtimeOverlay() string {
	if !h.Settings.Get().MemoryEnabled || h.cfg.Executable == "" {
		return ""
	}
	path := filepath.Join(h.cfg.UserData, "runtime-overlay.json")
	o := BuildRuntimeOverlay(MemoryAttachment{
		Executable: h.cfg.Executable,
		MemoryDir:  h.MemoryDir(),
		ApprovedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
	})
	if err := WriteRuntimeOverlay(path, o); err != nil {
		return ""
	}
	return path
}

// DaemonLogPath is where the Daemon's output goes.
func (h *Host) DaemonLogPath() string {
	return filepath.Join(h.cfg.UserData, "logs", "daemon.log")
}

// DroidPath is the droid the next Daemon starts from; "" when none.
func (h *Host) DroidPath() string {
	return LocateDroid(deref(h.Settings.Get().DroidPath), h.cfg.Env("PATH"), h.cfg.Home)
}

// DroidUpdated reports that droid changed on disk since the Daemon started.
func (h *Host) DroidUpdated() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.build.Replaced()
}

func (h *Host) spawn(port int) (*exec.Cmd, error) {
	path := h.DroidPath()
	if path == "" {
		return nil, errNoDroid
	}
	// Signed in: the Daemon runs as the account's login. Without one an API
	// key serves both sides.
	apiKey := ""
	if h.LoginState().Status != SignedIn {
		apiKey = h.Settings.APIKey()
	}
	override := ""
	if id := h.ActiveAccount(); id != "" {
		if err := shareFactoryHome(h.homeOf(id), h.SharedFactoryHome()); err != nil {
			return nil, err
		}
		override = h.accountRoot(id)
	}
	baseURL := deref(h.Settings.Get().FactoryAPIBaseURL)
	if baseURL == "" {
		baseURL = h.cfg.Env("FACTORY_API_BASE_URL")
	}
	h.mu.Lock()
	h.build = BuildOf(path)
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
	env := os.Environ()
	if apiKey != "" {
		env = append(env, "FACTORY_API_KEY="+apiKey)
	}
	if baseURL != "" {
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

const errNoDroid = hostError("droid executable not found")

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

// HasCredential reports that there is something to authenticate with.
func (h *Host) HasCredential() bool {
	return h.LoginState().Status == SignedIn || h.Settings.APIKey() != ""
}

// SystemPrompt is the Daemon's {type: "preset", preset: "droid", append}
// form of the System Prompt Addition, nil for none: it goes into every
// daemon.initialize_session the window makes.
func (h *Host) SystemPrompt() json.RawMessage {
	add := deref(h.Settings.Get().AppendSystemPrompt)
	if add == "" {
		return nil
	}
	b, _ := json.Marshal(map[string]string{"type": "preset", "preset": "droid", "append": add})
	return b
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
