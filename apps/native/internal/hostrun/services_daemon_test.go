//go:build !windows

package hostrun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/hosttools"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
)

// TestTheDaemonGetsTheHostsToolServers starts a Host as Run does, on the
// stand-in droid, and reads the Runtime Overlay its Daemon was given.
func TestTheDaemonGetsTheHostsToolServers(t *testing.T) {
	b := newBox(t, "0.240.0")
	userData := t.TempDir()
	ext := host.OverlayOptions{MCPServers: map[string]host.MCPServer{
		"droi-test": {Config: map[string]any{"type": "http", "url": "https://example.com/mcp"}},
	}}
	var (
		h   *host.Host
		svc *Services
	)
	h = host.New(host.Config{
		UserData:   userData,
		Home:       b.home,
		Env:        func(k string) string { return b.env[k] },
		Executable: "/opt/droi/droi",
		Overlay:    func() host.OverlayOptions { return svc.Overlay(ext) },
	})
	svc = StartServices(h)
	defer svc.Stop()
	svc.Tools.Register("droi-test", hosttools.Server{Autonomy: "low"})
	h.Start()
	defer h.Stop()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if raw, err := os.ReadFile(b.pidFile); err == nil && len(raw) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the Daemon did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	raw, err := os.ReadFile(filepath.Join(userData, "runtime-overlay.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		MCP struct {
			MCPServers            map[string]map[string]any `json:"mcpServers"`
			PersistentPermissions struct {
				Servers map[string]any `json:"servers"`
			} `json:"persistentPermissions"`
		} `json:"mcp"`
		Autonomy map[string]map[string]any `json:"mcpAutonomyOverrides"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	s := got.MCP.MCPServers["droi-test"]
	env, _ := s["env"].(map[string]any)
	if s["type"] != "stdio" || s["command"] != "/opt/droi/droi" || !reflect.DeepEqual(s["args"], []any{hosttools.Command, "droi-test"}) ||
		env[hosttools.EnvURL] != svc.Tools.URL() || env[hosttools.EnvToken] == "" ||
		got.MCP.PersistentPermissions.Servers["droi-test"] == nil || got.Autonomy["droi-test"]["defaultLevel"] != "low" {
		t.Fatalf("runtime overlay %s", raw)
	}
}
