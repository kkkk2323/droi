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
}

// Host starts and watches the Daemon and supplies the Factory credential.
type Host struct {
	cfg      Config
	Settings *SettingsStore
	Auth     *FactoryAuth
	Cli      *CliLoginReader
	Daemon   *Supervisor
	Scratch  *Scratch

	mu        sync.Mutex
	build     *DroidBuild
	listeners map[int]func()
	next      int
	wasIn     bool
}

// New makes a Host; Start launches the Daemon.
func New(cfg Config) *Host {
	if cfg.Env == nil {
		cfg.Env = os.Getenv
	}
	h := &Host{cfg: cfg, listeners: map[int]func(){}}
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
	h.wasIn = h.Auth.State().Status == SignedIn
	h.Cli = &CliLoginReader{FactoryHome: h.FactoryHome()}
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

// FactoryHome is the droid CLI's home, ~/.factory.
func (h *Host) FactoryHome() string {
	if d := h.cfg.Env("FACTORY_HOME_OVERRIDE"); d != "" {
		return d
	}
	return filepath.Join(h.cfg.Home, ".factory")
}

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
	// Signed in: the Daemon runs as the droid CLI's login and the Host
	// authenticates with the Droi login (ADR 0005). Without one an API key
	// serves both sides.
	apiKey := ""
	if h.Auth.State().Status != SignedIn {
		apiKey = h.Settings.APIKey()
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
	cmd := exec.Command(path, DaemonArgs(port, liveness, "")...)
	cmd.ExtraFiles = extra
	env := os.Environ()
	if apiKey != "" {
		env = append(env, "FACTORY_API_KEY="+apiKey)
	}
	if baseURL != "" {
		env = append(env, "FACTORY_API_BASE_URL="+baseURL)
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
	if cli := h.Cli.Read(); cli != nil {
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
	if cli := h.Cli.Read(); cli != nil {
		return &droid.Credential{Token: cli.AccessToken}, nil
	}
	if k := h.Settings.APIKey(); k != "" {
		return &droid.Credential{APIKey: k}, nil
	}
	return nil, nil
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
