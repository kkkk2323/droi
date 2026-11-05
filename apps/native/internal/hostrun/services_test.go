package hostrun

import (
	"slices"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/hosttools"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
)

func TestServicesCarryTheRegisteredToolServersAndTheHiddenTools(t *testing.T) {
	dir := t.TempDir()
	h := host.New(host.Config{UserData: dir, Home: dir, Executable: "/opt/droi/droi", Env: func(string) string { return "" }})
	s := StartServices(h)
	defer s.Stop()
	if s.Tools == nil || s.Sessions == nil {
		t.Fatalf("%+v", s)
	}
	if o := s.Overlay(host.OverlayOptions{}); len(o.MCPServers) != 0 {
		t.Fatalf("an overlay before any server: %+v", o)
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
	if !slices.Equal(got, []string{"CreateAutomation", "mcp:droi-workstream"}) {
		t.Fatalf("hidden %v", got)
	}
	got[0] = "changed"
	if s.ClientHiddenTools()[0] != "CreateAutomation" {
		t.Fatal("the list given out is the Services' own")
	}
}

func TestNoServicesAddNothing(t *testing.T) {
	var none *Services
	ext := host.OverlayOptions{Env: map[string]string{"A": "1"}}
	if none.ClientHiddenTools() != nil || none.Overlay(ext).Env["A"] != "1" || len(none.Overlay(ext).MCPServers) != 0 {
		t.Fatal("nil Services add something")
	}
	none.Stop()
}
