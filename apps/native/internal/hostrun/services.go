package hostrun

import (
	"context"
	"log"
	"slices"
	"sync"

	"github.com/kkkk2323/droi/apps/native/internal/hostsession"
	"github.com/kkkk2323/droi/apps/native/internal/hosttools"
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

	mu     sync.Mutex
	hidden []string
	stop   context.CancelFunc
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
		SystemPromptAddition: h.SystemPromptAddition,
		OnChange:             h.OnChange,
		Log:                  func(m string) { log.Println("[host sessions]", m) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	s.stop = cancel
	s.Sessions.Start(ctx)
	return s
}

// Overlay is what the Services add to the Runtime Overlay: their tool
// servers.
func (s *Services) Overlay() host.OverlayOptions {
	if s == nil || s.Tools == nil {
		return host.OverlayOptions{}
	}
	return s.Tools.Overlay()
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

// Stop disconnects the Runner and closes the Bridge.
func (s *Services) Stop() {
	if s == nil {
		return
	}
	s.stop()
	if s.Tools != nil {
		_ = s.Tools.Close()
	}
}
