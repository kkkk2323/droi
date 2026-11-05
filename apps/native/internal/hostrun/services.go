package hostrun

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"slices"
	"sync"

	"github.com/kkkk2323/droi/apps/native/internal/automationwork"
	"github.com/kkkk2323/droi/apps/native/internal/hostsession"
	"github.com/kkkk2323/droi/apps/native/internal/hosttools"
	"github.com/kkkk2323/droi/apps/native/internal/keepawake"
	"github.com/kkkk2323/droi/apps/native/pkg/droidhook"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
)

// Services are the Host's own features beside Memory, which the app and
// `droi host` both run: the tool servers Droi carries for them, and the
// Sessions the Host starts and follows (ADR 0035, ADR 0036).
type Services struct {
	// Tools carries the Host features' tool servers; nil when it could not
	// listen. Register them before the Host starts its Daemon.
	Tools *hosttools.Bridge
	// Sessions starts and follows the Host's own Sessions.
	Sessions *hostsession.Runner
	// Automations runs the Host's Automations (ADR 0035); nil when another
	// Droi runs those of the same data folder.
	Automations *automationwork.Scheduler

	// gatewayAutomations answers the Gateway's Automation endpoints: nil
	// without the Automations, elsewhere when another Droi runs them.
	gatewayAutomations gateway.Automations

	mu        sync.Mutex
	hidden    []string
	changed   map[int]func()
	nextSub   int
	stop      context.CancelFunc
	stopAutos func()
}

// StartServices starts h's Services, after host.New and before h.Start;
// h's Config reads their Overlay and ClientHiddenTools. Stop them before
// h.Stop.
func StartServices(h *host.Host) *Services {
	s := &Services{}
	if b, err := hosttools.New(h.Executable()); err != nil {
		log.Println("droi: host tools:", err)
	} else {
		s.Tools = b
	}
	s.Sessions = hostsession.New(hostsession.Config{
		ResolveURL:           h.WaitForDaemonURL,
		Credential:           h.Credential,
		HasCredential:        h.HasCredential,
		SystemPromptAddition: h.SystemPromptAddition,
		OnChange:             h.OnChange,
		Log:                  func(m string) { log.Println("[host sessions]", m) },
	})
	// Factory's own Automation tools make an Automation that syncs to
	// Factory: Droi's Sessions use Droi's.
	s.HideFromClients(automationwork.FactoryAutomationTools...)
	ctx, cancel := context.WithCancel(context.Background())
	s.stop = cancel
	s.Sessions.Start(ctx)
	s.startAutomations(ctx, h, filepath.Join(h.UserData(), "automations"))
	return s
}

// runHiddenTools are what an Automation's run leaves out with a droid of
// version: none before 1.2.0, whose initialize_session does not take llm
// ids and MCP selectors in disabledToolIds. A version that cannot be read
// is taken for a newer one, as the Host does for its Clients.
func runHiddenTools(version string) []string {
	readable := droidhook.CompareVersions(version, "0.0.0") >= 0
	if readable && droidhook.CompareVersions(version, "1.2.0") < 0 {
		return nil
	}
	return automationwork.RunHiddenTools()
}

// startAutomations runs the Automations in dir, unless another Droi does;
// then it registers their tool server.
func (s *Services) startAutomations(ctx context.Context, h *host.Host, dir string) {
	release, err := automationwork.Lock(dir)
	if errors.Is(err, automationwork.ErrLocked) {
		s.gatewayAutomations = elsewhere{}
	}
	if err != nil {
		log.Println("droi: automations:", err)
		return
	}
	logf := func(m string) { log.Println("[automations]", m) }
	starter := automationwork.NewSessionStarter(s.Sessions, logf)
	sched := automationwork.New(automationwork.Config{
		Dir:     dir,
		Starter: starter,
		Hold:    keepawake.Hold,
		// DroidVersion reads the droid on disk, which the running Daemon
		// differs from only until it restarts.
		HiddenTools: func() []string { return runHiddenTools(h.DroidVersion()) },
		Changed:     s.automationsChanged,
		Log:         logf,
	})
	starter.Attach(sched)
	s.Automations = sched
	s.gatewayAutomations = &automationwork.GatewayHost{
		Scheduler: sched,
		Daemon: func() (automationwork.FactoryDaemon, error) {
			cl, err := s.Sessions.Client()
			if err != nil {
				return nil, err
			}
			return cl, nil
		},
	}
	if s.Tools != nil {
		s.Tools.Register(automationwork.ServerName, automationwork.ToolServer(sched, automationwork.LocalZone))
	}
	go sched.Run(ctx)
	s.stopAutos = func() {
		starter.Close()
		release()
	}
}

// OnAutomationsChanged calls fn, on a goroutine of the Scheduler's, when an
// Automation or a run changes; the returned function unsubscribes.
func (s *Services) OnAutomationsChanged(fn func()) (unsubscribe func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.changed == nil {
		s.changed = map[int]func(){}
	}
	id := s.nextSub
	s.nextSub++
	s.changed[id] = fn
	return func() {
		s.mu.Lock()
		delete(s.changed, id)
		s.mu.Unlock()
	}
}

func (s *Services) automationsChanged() {
	s.mu.Lock()
	fns := make([]func(), 0, len(s.changed))
	for _, fn := range s.changed {
		fns = append(fns, fn)
	}
	s.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// GatewayAutomations is gateway.Options.Automations: nil without the
// Automations; when another Droi runs them, one that answers
// gateway.ErrAutomationsElsewhere.
func (s *Services) GatewayAutomations() gateway.Automations {
	if s == nil {
		return nil
	}
	return s.gatewayAutomations
}

// elsewhere are the Automations of a Host whose Automations another Droi on
// its computer runs.
type elsewhere struct{}

func (elsewhere) List(context.Context) ([]gateway.Automation, error) {
	return nil, gateway.ErrAutomationsElsewhere
}

func (elsewhere) Runs(context.Context, string, int) ([]gateway.AutomationRun, error) {
	return nil, gateway.ErrAutomationsElsewhere
}

func (elsewhere) Save(context.Context, gateway.Automation) (gateway.Automation, error) {
	return gateway.Automation{}, gateway.ErrAutomationsElsewhere
}

func (elsewhere) SetPaused(context.Context, string, bool) error {
	return gateway.ErrAutomationsElsewhere
}

func (elsewhere) Run(context.Context, string) (gateway.AutomationRun, error) {
	return gateway.AutomationRun{}, gateway.ErrAutomationsElsewhere
}

func (elsewhere) Delete(context.Context, string) error { return gateway.ErrAutomationsElsewhere }

func (elsewhere) Factory(context.Context) ([]gateway.FactoryAutomation, error) {
	return nil, gateway.ErrAutomationsElsewhere
}

func (elsewhere) MoveFactory(context.Context, string) (gateway.AutomationMoved, error) {
	return gateway.AutomationMoved{}, gateway.ErrAutomationsElsewhere
}

// Overlay is the Runtime Overlay's additions: the Services' tool servers,
// then app's, the app's extensions'. Droi's servers come first, so that an
// extension's server of the same name cannot take their place.
func (s *Services) Overlay(app host.OverlayOptions) host.OverlayOptions {
	if s == nil || s.Tools == nil {
		return app
	}
	return s.Tools.Overlay().Add(app)
}

// HideFromClients leaves tools out of every Client's Session (from droid
// 1.2.0): tool ids, llm ids or MCP selectors (hosttools.Selector).
func (s *Services) HideFromClients(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		if !slices.Contains(s.hidden, id) {
			s.hidden = append(s.hidden, id)
		}
	}
}

// ClientHiddenTools is host.Config.ClientHiddenTools: what HideFromClients
// was given.
func (s *Services) ClientHiddenTools() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.hidden)
}

// Stop stops the Automations, disconnects the Runner and closes the
// Bridge.
func (s *Services) Stop() {
	if s == nil {
		return
	}
	s.stop()
	if s.stopAutos != nil {
		s.stopAutos()
		s.stopAutos = nil
	}
	if s.Tools != nil {
		_ = s.Tools.Close()
	}
}
