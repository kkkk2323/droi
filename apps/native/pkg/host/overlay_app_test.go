package host

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// An app's own MCP servers sit beside Memory's: approved and at Low
// autonomy when it asks, Memory's name kept for Memory, hooks after
// Memory's and the environment merged.
func TestTheRuntimeOverlayCarriesAnAppsOwnServers(t *testing.T) {
	corp := map[string]any{"type": "http", "url": "https://mcp.example.com/db"}
	o := OverlayOptions{
		ApprovedAt: "2026-10-24T08:00:00.000Z",
		MCPServers: map[string]MCPServer{
			"corp-db":        {Config: corp, Approved: true, Autonomy: "low"},
			"corp-wiki":      {Config: map[string]any{"type": "stdio", "command": "/bin/wiki"}},
			MemoryServerName: {Config: map[string]any{"type": "stdio", "command": "/bin/other"}},
		},
		BlockOnMCPLoad: true,
		Hooks:          map[string][]any{"SessionStart": {map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "corp-hook"}}}}},
		Env:            map[string]string{"CORP_ENV": "prod", "DROI_MEMORY_DIR": "/elsewhere"},
	}
	got := roundTrip(t, BuildRuntimeOverlayWith(attachment, o)).(map[string]any)
	mcp := got["mcp"].(map[string]any)
	servers := mcp["mcpServers"].(map[string]any)
	if !reflect.DeepEqual(servers["corp-db"], corp) || servers["corp-wiki"] == nil {
		t.Errorf("servers %v", servers)
	}
	if servers[MemoryServerName].(map[string]any)["command"] != attachment.Executable {
		t.Errorf("Memory's server was replaced: %v", servers[MemoryServerName])
	}
	approved := mcp["persistentPermissions"].(map[string]any)["servers"].(map[string]any)
	if !reflect.DeepEqual(approved["corp-db"], map[string]any{"approvedAt": "2026-10-24T08:00:00.000Z", "impactLevel": "high"}) || approved["corp-wiki"] != nil || approved[MemoryServerName] == nil {
		t.Errorf("approvals %v", approved)
	}
	autonomy := got["mcpAutonomyOverrides"].(map[string]any)
	if !reflect.DeepEqual(autonomy["corp-db"], map[string]any{"defaultLevel": "low"}) || autonomy["corp-wiki"] != nil || autonomy[MemoryServerName] == nil {
		t.Errorf("autonomy %v", autonomy)
	}
	start := got["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(start) != 2 || start[1].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"] != "corp-hook" {
		t.Errorf("SessionStart hooks %v", start)
	}
	env := got["env"].(map[string]any)
	if env["CORP_ENV"] != "prod" || env["DROI_MEMORY_DIR"] != attachment.MemoryDir {
		t.Errorf("env %v", env)
	}
}

// Without Memory, an app's server alone makes an overlay, with no hooks.
func TestTheRuntimeOverlayCarriesAnAppsServersWithoutMemory(t *testing.T) {
	got := BuildRuntimeOverlayWith(MemoryAttachment{}, OverlayOptions{MCPServers: map[string]MCPServer{"corp-db": {Config: map[string]any{"type": "http", "url": "u"}, Approved: true}}})
	approved := got["mcp"].(map[string]any)["persistentPermissions"].(map[string]any)["servers"].(map[string]any)["corp-db"].(map[string]any)
	if approved["approvedAt"] == "" || got["hooks"] != nil || got["blockOnMcpLoad"] != nil {
		t.Errorf("overlay %v", got)
	}
}

func TestOverlayOptionsAddKeepsTheFirstName(t *testing.T) {
	a := OverlayOptions{CustomModels: []any{"m1"}, MCPServers: map[string]MCPServer{"x": {Autonomy: "low"}}, Hooks: map[string][]any{"SessionEnd": {"a"}}}
	b := OverlayOptions{CustomModels: []any{"m2"}, MCPServers: map[string]MCPServer{"x": {Autonomy: "high"}, "y": {}}, Hooks: map[string][]any{"SessionEnd": {"b"}}, Env: map[string]string{"K": "v"}, BlockOnMCPLoad: true}
	got := a.Add(b)
	if !reflect.DeepEqual(got.CustomModels, []any{"m1", "m2"}) || got.MCPServers["x"].Autonomy != "low" || len(got.MCPServers) != 2 ||
		!reflect.DeepEqual(got.Hooks["SessionEnd"], []any{"a", "b"}) || got.Env["K"] != "v" || !got.BlockOnMCPLoad {
		t.Errorf("added %+v", got)
	}
	if len(a.Hooks["SessionEnd"]) != 1 || len(a.CustomModels) != 1 {
		t.Errorf("Add changed its receiver: %+v", a)
	}
	if !(OverlayOptions{ApprovedAt: "t"}).empty() || got.empty() {
		t.Error("empty")
	}
}

// Config.Overlay alone makes the Host write an overlay; read again at each
// start.
func TestTheHostWritesConfigOverlay(t *testing.T) {
	dir := t.TempDir()
	url := "https://mcp.example.com/a"
	h := New(Config{UserData: dir, Home: dir, Env: func(string) string { return "" }, Overlay: func() OverlayOptions {
		return OverlayOptions{MCPServers: map[string]MCPServer{"corp": {Config: map[string]any{"type": "http", "url": url}}}}
	}})
	path := h.runtimeOverlay()
	if path == "" {
		t.Fatal("no overlay written")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		MCP struct {
			MCPServers map[string]map[string]string `json:"mcpServers"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(b, &got); err != nil || got.MCP.MCPServers["corp"]["url"] != url {
		t.Errorf("written %s", b)
	}
	url = "https://mcp.example.com/b"
	h.runtimeOverlay()
	b, _ = os.ReadFile(path)
	_ = json.Unmarshal(b, &got)
	if got.MCP.MCPServers["corp"]["url"] != url {
		t.Errorf("not read again: %s", b)
	}
}

func TestTheSystemPromptAdditionAddsTheApps(t *testing.T) {
	dir := t.TempDir()
	app := ""
	h := New(Config{UserData: dir, Home: dir, Env: func(string) string { return "" }, AppendSystemPrompt: func() string { return app }})
	prompt := h.GatewayOptions("1", nil).AppendSystemPrompt
	if got := prompt(); got != "" {
		t.Errorf("none: %q", got)
	}
	app = "Use corp-db for data."
	if got := prompt(); got != app {
		t.Errorf("the app's alone: %q", got)
	}
	if err := h.Settings.Update(func(s *Settings) { s.AppendSystemPrompt = OptString("Be brief") }); err != nil {
		t.Fatal(err)
	}
	if got := prompt(); got != "Be brief\n\nUse corp-db for data." {
		t.Errorf("both: %q", got)
	}
}
