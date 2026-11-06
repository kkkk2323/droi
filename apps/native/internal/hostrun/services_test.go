package hostrun

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/automationwork"
	"github.com/kkkk2323/droi/apps/native/internal/hosttools"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
)

func TestOnlyTheDeviceChoiceOfDirectIsAskedOfTheDaemon(t *testing.T) {
	for mode, want := range map[string]protocol.ToolExecutionMode{
		"direct_only": protocol.ToolExecutionModeDirectOnly,
		"script_only": "",
		"":            "",
		"nonsense":    "",
	} {
		if got := deviceToolMode(mode); got != want {
			t.Errorf("deviceToolMode(%q) = %q, want %q", mode, got, want)
		}
	}
	dir := t.TempDir()
	h := host.New(host.Config{UserData: dir, Home: dir, Env: func(string) string { return "" }, ToolMode: func() string { return "direct_only" }})
	if h.ToolMode() != "direct_only" || host.New(host.Config{UserData: dir, Home: dir}).ToolMode() != "" {
		t.Error("Host.ToolMode")
	}
}

// OnWorkstreamsChanged is the Coordinator's OnChange, whose calls the
// Coordinator's own tests cover; here it is only there, and safe without
// Services.
func TestServicesOfferTheWorkstreamsChangesAndEditing(t *testing.T) {
	var none *Services
	none.OnWorkstreamsChanged(func() { t.Error("called") })()

	dir := t.TempDir()
	h := host.New(host.Config{UserData: dir, Home: dir, Env: func(string) string { return "" }})
	s := StartServices(h)
	defer s.Stop()
	s.OnWorkstreamsChanged(func() {})()
	if _, ok := s.GatewayWorkstreams().(gateway.WorkstreamEditor); !ok {
		t.Fatal("the Gateway's Workstreams do not edit")
	}
}

func TestServicesCarryTheRegisteredToolServersAndTheHiddenTools(t *testing.T) {
	dir := t.TempDir()
	h := host.New(host.Config{UserData: dir, Home: dir, Executable: "/opt/droi/droi", Env: func(string) string { return "" }})
	s := StartServices(h)
	defer s.Stop()
	if s.Tools == nil || s.Sessions == nil {
		t.Fatalf("%+v", s)
	}
	// Droi's Automations and the Workstreams' tool server are there from the
	// start; the Workstreams' is hidden from every Client's Session.
	own := s.Overlay(host.OverlayOptions{})
	if len(own.MCPServers) != 2 || own.MCPServers[automationwork.ServerName].Autonomy != "medium" {
		t.Fatalf("the overlay of the Services' own servers: %+v", own)
	}
	if ws := own.MCPServers["droi-workstream"]; ws.Autonomy != "low" || !ws.Approved {
		t.Fatalf("droi-workstream %+v", ws)
	}
	if s.Workstreams == nil || s.GatewayWorkstreams() == nil {
		t.Fatal("no Workstreams")
	}
	s.Tools.Register("droi-test", hosttools.Server{Autonomy: "low"})
	// An extension's server cannot take the name of one of Droi's.
	ext := host.OverlayOptions{MCPServers: map[string]host.MCPServer{
		"droi-test": {Config: map[string]any{"type": "http", "url": "https://example.com/mcp"}},
		"corp":      {Config: map[string]any{"type": "http", "url": "https://corp.example.com/mcp"}},
	}}
	o := s.Overlay(ext)
	if got := o.MCPServers["droi-test"].Config["args"]; !slices.Equal(got.([]string), []string{hosttools.Command, "droi-test"}) {
		t.Fatalf("droi-test %v", o.MCPServers["droi-test"])
	}
	if o.MCPServers["corp"].Config["url"] != "https://corp.example.com/mcp" {
		t.Fatalf("the extension's own server is gone: %v", o.MCPServers)
	}

	s.HideFromClients("CreateAutomation", "mcp:droi-workstream")
	s.HideFromClients("CreateAutomation")
	got := s.ClientHiddenTools()
	if want := append(slices.Clone(automationwork.FactoryAutomationTools), "mcp:droi-workstream"); !slices.Equal(got, want) {
		t.Fatalf("hidden %v, want %v", got, want)
	}
	got[0] = "changed"
	if s.ClientHiddenTools()[0] != "CreateAutomation" {
		t.Fatal("the list given out is the Services' own")
	}
}

func TestOneDroiRunsTheAutomationsOfAFolder(t *testing.T) {
	dir := t.TempDir()
	h := host.New(host.Config{UserData: dir, Home: dir, Executable: "/opt/droi/droi", Env: func(string) string { return "" }})
	first := StartServices(h)
	defer first.Stop()
	if first.Scheduler() == nil || first.GatewayAutomations() == nil {
		t.Fatal("no Automations")
	}
	second := StartServices(h)
	if second.Scheduler() != nil {
		t.Fatal("a second Scheduler of the same folder")
	}
	second.Stop()
	first.Stop()
	third := StartServices(h)
	defer third.Stop()
	if third.Scheduler() == nil {
		t.Fatal("the folder stayed locked after Stop")
	}
}

func TestNoServicesAddNothing(t *testing.T) {
	var none *Services
	ext := host.OverlayOptions{Env: map[string]string{"A": "1"}}
	if none.ClientHiddenTools() != nil || none.Overlay(ext).Env["A"] != "1" || len(none.Overlay(ext).MCPServers) != 0 || none.GatewayWorkstreams() != nil {
		t.Fatal("nil Services add something")
	}
	none.Stop()
}

func TestAnotherDroisAutomationsAnswerThatTheyAreElsewhere(t *testing.T) {
	dir := t.TempDir()
	h := host.New(host.Config{UserData: dir, Home: dir, Executable: "/opt/droi/droi", Env: func(string) string { return "" }})
	first := StartServices(h)
	defer first.Stop()
	second := StartServices(h)
	defer second.Stop()
	g := second.GatewayAutomations()
	if g == nil {
		t.Fatal("no Gateway Automations")
	}
	if _, err := g.List(context.Background()); !errors.Is(err, gateway.ErrAutomationsElsewhere) {
		t.Fatalf("list: %v", err)
	}
	if _, err := g.Run(context.Background(), "x"); !errors.Is(err, gateway.ErrAutomationsElsewhere) {
		t.Fatalf("run: %v", err)
	}
}

func TestARunHidesToolsFromADroidThatTakesThem(t *testing.T) {
	for _, c := range []struct {
		version string
		hides   bool
	}{{"1.1.9", false}, {"0.240.0", false}, {"1.2.0", true}, {"1.4.1", true}, {"", true}} {
		got := runHiddenTools(c.version)
		if want := automationwork.RunHiddenTools(); c.hides && !slices.Equal(got, want) || !c.hides && got != nil {
			t.Errorf("%q: %v", c.version, got)
		}
	}
}

// A second Droi on the same data carries the Automation tools, which say
// another Droi runs them, and takes the Automations once the first quits.
func TestTheSecondDroiTakesTheAutomationsOnceTheFirstQuits(t *testing.T) {
	old := lockRetry
	lockRetry = 20 * time.Millisecond
	t.Cleanup(func() { lockRetry = old })
	dir := t.TempDir()
	h := host.New(host.Config{UserData: dir, Home: dir, Executable: "/opt/droi/droi", Env: func(string) string { return "" }})
	first := StartServices(h)
	second := StartServices(h)
	defer second.Stop()
	if _, ok := second.Overlay(host.OverlayOptions{}).MCPServers[automationwork.ServerName]; !ok {
		t.Fatal("the second does not carry the Automation tools")
	}
	if _, err := second.automationTools().Call(context.Background(), automationwork.ToolList, map[string]any{}, "s1"); err == nil || err.Error() != "Another Droi on this computer runs the automations." {
		t.Fatalf("a tool of the second: %v", err)
	}
	if _, err := second.GatewayAutomations().List(context.Background()); !errors.Is(err, gateway.ErrAutomationsElsewhere) {
		t.Fatalf("list of the second: %v", err)
	}
	changed := make(chan struct{}, 1)
	second.OnAutomationsChanged(func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	first.Stop()
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("the second never took the Automations")
	}
	if second.Scheduler() == nil {
		t.Fatal("no Scheduler")
	}
	if list, err := second.GatewayAutomations().List(context.Background()); err != nil || list == nil {
		t.Fatalf("list %v %v", list, err)
	}
	if _, err := second.automationTools().Call(context.Background(), automationwork.ToolList, map[string]any{}, "s1"); err != nil {
		t.Fatalf("a tool: %v", err)
	}
}

// A lock that fails for another reason than another Droi answers that
// reason, not that another Droi runs them.
func TestALockThatFailsOtherwiseSaysWhy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "automations"), []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := host.New(host.Config{UserData: dir, Home: dir, Executable: "/opt/droi/droi", Env: func(string) string { return "" }})
	s := StartServices(h)
	defer s.Stop()
	_, err := s.GatewayAutomations().List(context.Background())
	if err == nil || errors.Is(err, gateway.ErrAutomationsElsewhere) {
		t.Fatalf("list: %v", err)
	}
}

// The first time another Droi holds the Automations folder is logged.
func TestTheFirstLockedResultIsLogged(t *testing.T) {
	var buf safeBuffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	dir := t.TempDir()
	h := host.New(host.Config{UserData: dir, Home: dir, Executable: "/opt/droi/droi", Env: func(string) string { return "" }})
	first := StartServices(h)
	defer first.Stop()
	second := StartServices(h)
	defer second.Stop()
	if !strings.Contains(buf.String(), "droi: automations: another Droi holds the Automations folder "+filepath.Join(dir, "automations")) {
		t.Fatalf("logged %q", buf.String())
	}
}

type safeBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
