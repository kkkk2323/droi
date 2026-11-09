package hostrun

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/v2/internal/automationwork"
	"github.com/kkkk2323/droi/apps/native/v2/internal/hostsession"
	"github.com/kkkk2323/droi/apps/native/v2/internal/hosttools"
	"github.com/kkkk2323/droi/apps/native/v2/internal/keepawake"
	"github.com/kkkk2323/droi/apps/native/v2/internal/workstream"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/droidhook"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/host"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// Services are the Host's own features beside Memory, which the app and
// `droi host` both run: the tool servers Droi carries for them, the
// Sessions the Host starts and follows, and its Workstreams (ADR 0035, ADR
// 0036).
type Services struct {
	// Tools carries the Host features' tool servers; nil when it could not
	// listen. Register them before the Host starts its Daemon.
	Tools *hosttools.Bridge
	// Sessions starts and follows the Host's own Sessions.
	Sessions *hostsession.Runner
	// held answers the Gateway's Automation endpoints.
	held *heldAutomations
	// Workstreams runs the Host's Workstreams (ADR 0036).
	Workstreams *workstream.Coordinator

	// lockTried is whether takeAutomations ran before; it runs first at
	// start, then on the one goroutine that retries.
	lockTried bool

	mu        sync.Mutex
	sched     *automationwork.Scheduler
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
	s := &Services{held: &heldAutomations{err: gateway.ErrAutomationsElsewhere}}
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
		ToolMode:             func() protocol.ToolExecutionMode { return deviceToolMode(h.ToolMode()) },
		OnChange:             h.OnChange,
		Log:                  func(m string) { log.Println("[host sessions]", m) },
	})
	// Factory's own Automation tools make an Automation that syncs to
	// Factory: Droi's Sessions use Droi's.
	s.HideFromClients(automationwork.FactoryAutomationTools...)
	s.HideFromClients(hosttools.Selector(workstream.ServerName)...)
	ctx, cancel := context.WithCancel(context.Background())
	s.stop = cancel
	s.Sessions.Start(ctx)
	s.startAutomations(ctx, h, filepath.Join(h.UserData(), "automations"))
	s.Workstreams = workstream.New(workstream.Config{
		Dir:          filepath.Join(h.UserData(), "workstreams"),
		SessionsDir:  filepath.Join(h.SharedFactoryHome(), "sessions"),
		Sessions:     s.Sessions,
		ScriptModels: h.ScriptModelList,
		Log:          func(m string) { log.Println("[workstreams]", m) },
	})
	if s.Tools != nil {
		s.Tools.Register(workstream.ServerName, s.Workstreams.Server())
	}
	s.Workstreams.Start(ctx)
	return s
}

// deviceToolMode is the mode the Host's Sessions ask the Daemon for: only
// the user's choice of Direct. droid applies an asked mode to the models
// Factory lists only, where it picks Script by itself, so asking for Script
// would change nothing.
func deviceToolMode(mode string) protocol.ToolExecutionMode {
	if protocol.ToolExecutionMode(mode) == protocol.ToolExecutionModeDirectOnly {
		return protocol.ToolExecutionModeDirectOnly
	}
	return ""
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

// lockRetry is how often a Droi without the Automations' lock tries to
// take it again.
var lockRetry = time.Minute

// startAutomations registers the Automations' tool server, then runs the
// Automations in dir once this Droi holds their lock: at once, or when
// another Droi that held it quits.
func (s *Services) startAutomations(ctx context.Context, h *host.Host, dir string) {
	if s.Tools != nil {
		s.Tools.Register(automationwork.ServerName, s.automationTools())
	}
	if s.takeAutomations(ctx, h, dir) {
		return
	}
	every := lockRetry
	go func() {
		tick := time.NewTicker(every)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if s.takeAutomations(ctx, h, dir) {
					s.automationsChanged()
					return
				}
			}
		}
	}()
}

// automationTools is the droi-automations server over the Scheduler, which
// says another Droi runs them while this one has none.
func (s *Services) automationTools() hosttools.Server {
	return automationwork.ToolServerOf(s.Scheduler, automationwork.LocalZone)
}

// takeAutomations takes the Automations' lock and starts their Scheduler,
// or notes why not; it reports whether it is done trying.
func (s *Services) takeAutomations(ctx context.Context, h *host.Host, dir string) bool {
	release, err := automationwork.Lock(dir)
	if err != nil {
		why := gateway.ErrAutomationsElsewhere
		if !errors.Is(err, automationwork.ErrLocked) {
			why = fmt.Errorf("Droi could not open its automations: %w", err)
		}
		// held starts as ErrAutomationsElsewhere: the first result is
		// logged whatever it is.
		if s.held.set(nil, why) || !s.lockTried {
			if errors.Is(err, automationwork.ErrLocked) {
				log.Println("droi: automations: another Droi holds the Automations folder", dir)
			} else {
				log.Println("droi: automations:", err)
			}
		}
		s.lockTried = true
		return false
	}
	logf := func(m string) { log.Println("[automations]", m) }
	var starter runStarter = automationwork.NewSessionStarter(s.Sessions, logf)
	if h.KernelEnabled() {
		starter = kernelStarter(h, logf)
	}
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
	s.mu.Lock()
	if ctx.Err() != nil {
		// Stopped while the lock was taken.
		s.mu.Unlock()
		starter.Close()
		release()
		return true
	}
	s.sched = sched
	s.stopAutos = func() {
		starter.Close()
		release()
	}
	s.mu.Unlock()
	s.held.set(&automationwork.GatewayHost{
		Scheduler: sched,
		Log:       logf,
		Daemon: func() (automationwork.FactoryDaemon, error) {
			cl, err := s.Sessions.Client()
			if err != nil {
				return nil, err
			}
			return cl, nil
		},
	}, nil)
	go sched.Run(ctx)
	return true
}

// runStarter is the Starter of a Host's Automations, the Daemon's or the Kernel's.
type runStarter interface {
	automationwork.Starter
	Attach(s *automationwork.Scheduler)
	Close()
}

// kernelRunStarter is a KernelStarter on a connection of its own, as the Host's.
type kernelRunStarter struct {
	*automationwork.KernelStarter
	link   *host.KernelLink
	unhear func()
}

func (k kernelRunStarter) Close() {
	k.KernelStarter.Close()
	k.unhear()
	k.link.Close()
}

// kernelStarter runs the Automations as Kernel conversations (M4 4L). While a run works its approval level is stored
// with the conversation policies the Host keeps, so a Kernel that starts again gets it back.
func kernelStarter(h *host.Host, logf func(string)) runStarter {
	link := host.NewKernelLink(host.KernelLinkOptions{
		Client: kernel.ClientHost,
		Token:  h.KernelToken,
		Log:    func(s string) { logf("kernel: " + s) },
	})
	ks := &automationwork.KernelStarter{
		Client: func() (automationwork.KernelRuns, error) {
			if c, _ := link.Current(); c != nil {
				return c, nil
			}
			return nil, errors.New("Droi is not connected to the Kernel")
		},
		KeepPolicy: func(id kernel.ID, autonomy string) error {
			return h.SetConversationPolicy(id, host.ConversationPolicy{Autonomy: autonomy})
		},
		Log: logf,
	}
	return kernelRunStarter{KernelStarter: ks, link: link, unhear: h.OnKernelState(link.Use)}
}

// Scheduler runs the Host's Automations (ADR 0035); nil while another Droi
// runs those of the same data folder.
func (s *Services) Scheduler() *automationwork.Scheduler {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sched
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

// OnWorkstreamsChanged calls fn, on a goroutine of the Workstreams', when
// what the Gateway lists of them changes; the returned function
// unsubscribes.
func (s *Services) OnWorkstreamsChanged(fn func()) (unsubscribe func()) {
	if s == nil || s.Workstreams == nil {
		return func() {}
	}
	return s.Workstreams.OnChange(fn)
}

// GatewayAutomations is gateway.Options.Automations: the Automations once
// this Droi runs them; until then, ErrAutomationsElsewhere while another
// Droi does, or why their lock could not be taken.
func (s *Services) GatewayAutomations() gateway.Automations {
	if s == nil {
		return nil
	}
	return s.held
}

// heldAutomations answers the Gateway's Automation endpoints with the
// Automations this Droi runs, or with why it runs none.
type heldAutomations struct {
	mu  sync.Mutex
	au  gateway.Automations
	err error
}

// set reports whether what it answers changed.
func (h *heldAutomations) set(au gateway.Automations, err error) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	same := h.au == au && (h.err == nil) == (err == nil) && (err == nil || h.err.Error() == err.Error())
	h.au, h.err = au, err
	return !same
}

func (h *heldAutomations) get() (gateway.Automations, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.au == nil {
		return nil, h.err
	}
	return h.au, nil
}

func (h *heldAutomations) List(ctx context.Context) ([]gateway.Automation, error) {
	au, err := h.get()
	if err != nil {
		return nil, err
	}
	return au.List(ctx)
}

func (h *heldAutomations) Runs(ctx context.Context, id string, limit int) ([]gateway.AutomationRun, error) {
	au, err := h.get()
	if err != nil {
		return nil, err
	}
	return au.Runs(ctx, id, limit)
}

func (h *heldAutomations) Save(ctx context.Context, a gateway.Automation) (gateway.Automation, error) {
	au, err := h.get()
	if err != nil {
		return gateway.Automation{}, err
	}
	return au.Save(ctx, a)
}

func (h *heldAutomations) SetPaused(ctx context.Context, id string, paused bool) error {
	au, err := h.get()
	if err != nil {
		return err
	}
	return au.SetPaused(ctx, id, paused)
}

func (h *heldAutomations) Run(ctx context.Context, id string) (gateway.AutomationRun, error) {
	au, err := h.get()
	if err != nil {
		return gateway.AutomationRun{}, err
	}
	return au.Run(ctx, id)
}

func (h *heldAutomations) Delete(ctx context.Context, id string) error {
	au, err := h.get()
	if err != nil {
		return err
	}
	return au.Delete(ctx, id)
}

func (h *heldAutomations) Factory(ctx context.Context) ([]gateway.FactoryAutomation, error) {
	au, err := h.get()
	if err != nil {
		return nil, err
	}
	return au.Factory(ctx)
}

func (h *heldAutomations) MoveFactory(ctx context.Context, id string) (gateway.AutomationMoved, error) {
	au, err := h.get()
	if err != nil {
		return gateway.AutomationMoved{}, err
	}
	return au.MoveFactory(ctx, id)
}

func (h *heldAutomations) Zone(ctx context.Context) (string, error) {
	au, err := h.get()
	if err != nil {
		return "", err
	}
	return au.Zone(ctx)
}

// GatewayWorkstreams is gateway.Options.Workstreams: nil without Services.
func (s *Services) GatewayWorkstreams() gateway.Workstreams {
	if s == nil || s.Workstreams == nil {
		return nil
	}
	return s.Workstreams
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

// Stop ends the Workstreams' work, stops the Automations, disconnects the
// Runner and closes the Bridge.
func (s *Services) Stop() {
	if s == nil {
		return
	}
	if s.Workstreams != nil {
		s.Workstreams.Close()
	}
	s.stop()
	s.mu.Lock()
	stopAutos := s.stopAutos
	s.stopAutos = nil
	s.mu.Unlock()
	if stopAutos != nil {
		stopAutos()
	}
	if s.Tools != nil {
		_ = s.Tools.Close()
	}
}
