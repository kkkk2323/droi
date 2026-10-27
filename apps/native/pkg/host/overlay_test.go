package host

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

var attachment = MemoryAttachment{
	Executable: "/Applications/Droi.app/Contents/MacOS/Droi",
	MemoryDir:  "/Users/dev/Library/Application Support/droi's/memory",
	ApprovedAt: "2026-09-29T12:00:00.000Z",
}

const hookCommand = `DROI_MEMORY_DIR='/Users/dev/Library/Application Support/droi'\''s/memory' '/Applications/Droi.app/Contents/MacOS/Droi' memory-hook`

func roundTrip(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTheRuntimeOverlayAttachesMemory(t *testing.T) {
	command := `[{"type":"command","command":` + mustJSON(hookCommandLine(runtime.GOOS, attachment.MemoryDir, attachment.Executable)) + `,"timeout":10}]`
	start := `[{"matcher":"^(startup|clear|compact)$","hooks":` + command + `}]`
	want := `{
		"env": {"DROI_MEMORY_DIR": ` + mustJSON(attachment.MemoryDir) + `},
		"blockOnMcpLoad": true,
		"mcp": {
			"mcpServers": {"droi-memory": {"type": "stdio", "command": ` + mustJSON(attachment.Executable) + `, "args": ["memory-server"], "env": {"DROI_MEMORY_DIR": ` + mustJSON(attachment.MemoryDir) + `}}},
			"persistentPermissions": {"servers": {"droi-memory": {"approvedAt": "2026-09-29T12:00:00.000Z", "impactLevel": "high"}}}
		},
		"mcpAutonomyOverrides": {"droi-memory": {"defaultLevel": "low"}},
		"hooks": {"SessionStart": ` + start + `}
	}`
	var w any
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	if got := roundTrip(t, BuildRuntimeOverlay(attachment)); !reflect.DeepEqual(got, w) {
		t.Errorf("overlay\n got %v\nwant %v", got, w)
	}
	if _, ok := BuildRuntimeOverlay(attachment)["mcpServers"]; ok {
		t.Error("a top-level mcpServers, which the Daemon would ignore")
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestTheHookCommandLines(t *testing.T) {
	if got := hookCommandLine("darwin", attachment.MemoryDir, attachment.Executable); got != hookCommand {
		t.Errorf("sh: %s", got)
	}
	want := `set "DROI_MEMORY_DIR=C:\Users\A B\AppData\Roaming\Droi\memory"&& "C:\Program Files\Droi\Droi.exe" memory-hook`
	if got := hookCommandLine("windows", `C:\Users\A B\AppData\Roaming\Droi\memory`, `C:\Program Files\Droi\Droi.exe`); got != want {
		t.Errorf("cmd.exe: %s", got)
	}
}

func TestTheHookCommandIsQuotedForTheShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cmd.exe runs the hook on Windows: TestTheHookCommandRunsInCmd")
	}
	o := BuildRuntimeOverlay(MemoryAttachment{Executable: "/usr/bin/printenv", MemoryDir: attachment.MemoryDir})
	cmd := o["hooks"].(map[string]any)["SessionStart"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
	// printenv prints the variable its argument names: ask for the one the command sets.
	out, err := exec.Command("/bin/sh", "-c", strings.TrimSuffix(cmd, MemoryHookCommand)+"DROI_MEMORY_DIR").Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != attachment.MemoryDir+"\n" {
		t.Errorf("the shell saw %q", out)
	}
}

// A Host with Magpie on and Memory off writes the models alone: no Memory
// Server, no hooks, nothing for a Session to block on.
func TestTheRuntimeOverlayCarriesCustomModels(t *testing.T) {
	models := MagpieCustomModels(MagpieCatalog{BaseURL: DefaultMagpieBaseURL, Models: []MagpieModel{{ID: "workbuddy/glm-5.3"}}})
	got := BuildRuntimeOverlayWith(MemoryAttachment{}, OverlayOptions{CustomModels: models})
	if len(got) != 1 {
		t.Fatalf("keys %v", got)
	}
	list, ok := got["customModels"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("customModels %v", got["customModels"])
	}
	entry := list[0].(map[string]any)
	if entry["model"] != "workbuddy/glm-5.3" || entry["baseUrl"] != DefaultMagpieBaseURL+"/v1" {
		t.Errorf("entry %v", entry)
	}
}

// With both on, the overlay holds Memory's part and the models, and neither
// gets in the other's way.
func TestTheRuntimeOverlayCarriesMemoryAndCustomModels(t *testing.T) {
	models := MagpieCustomModels(MagpieCatalog{BaseURL: DefaultMagpieBaseURL, Models: []MagpieModel{{ID: "a/b"}}})
	got := BuildRuntimeOverlayWith(attachment, OverlayOptions{CustomModels: models})
	if _, ok := got["hooks"]; !ok {
		t.Errorf("Memory's hooks are gone: %v", got)
	}
	if _, ok := got["mcp"]; !ok {
		t.Errorf("Memory's MCP server is gone: %v", got)
	}
	if list, ok := got["customModels"].([]any); !ok || len(list) != 1 {
		t.Errorf("customModels %v", got["customModels"])
	}
}

func TestTheRuntimeOverlayIsWrittenForTheUserOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "runtime-overlay.json")
	if err := WriteRuntimeOverlay(path, BuildRuntimeOverlay(attachment)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var got any
	if err := json.Unmarshal(b, &got); err != nil || !reflect.DeepEqual(got, roundTrip(t, BuildRuntimeOverlay(attachment))) {
		t.Errorf("written %s", b)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
}
