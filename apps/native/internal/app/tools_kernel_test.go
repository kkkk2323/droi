package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// testMCPServerEnv makes this test binary a stdio MCP server instead of running tests: Kernel starts it as a
// repository's or the user's server, so a window test needs no other program.
const testMCPServerEnv = "DROI_TEST_MCP_SERVER"

func init() {
	if os.Getenv(testMCPServerEnv) == "" {
		return
	}
	serveTestMCP(os.Stdin, os.Stdout)
	os.Exit(0)
}

// serveTestMCP answers MCP's JSON-RPC, a message a line, with one read-only tool. With MARKER set it first
// appends its pid to that file, so a test can tell whether it was started.
func serveTestMCP(in io.Reader, out io.Writer) {
	if marker := os.Getenv("MARKER"); marker != "" {
		if f, err := os.OpenFile(marker, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
		}
	}
	enc := json.NewEncoder(out)
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scan.Scan() {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &msg) != nil || len(msg.ID) == 0 || msg.Method == "" {
			continue
		}
		var result any
		switch msg.Method {
		case "initialize":
			version, _ := msg.Params["protocolVersion"].(string)
			if version == "" {
				version = "2025-06-18"
			}
			result = map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": "test", "version": "1.0.0"}}
		case "ping":
			result = map[string]any{}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "echo", "description": "Say the text back.",
				"inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": true}}}}
		default:
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32601, "message": "method not found"}})
			continue
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
	}
}

// testMCPEntry is an mcp.json entry that runs this test binary as an MCP server.
func testMCPEntry(t *testing.T, marker string) map[string]any {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"command": exe, "args": []string{"-test.run=^$"},
		"env": map[string]string{testMCPServerEnv: "1", "MARKER": marker}}
}

func writeMCPFile(t *testing.T, path string, servers map[string]any) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func startedPids(marker string) int {
	raw, _ := os.ReadFile(marker)
	return len(strings.Fields(string(raw)))
}

// A repository's .factory/mcp.json runs nothing until the person allows it in the Session's dialog, which shows
// what it would run; the user's own servers connect at once, and their switch is Kernel's global file.
func TestAKernelRepositoryServerRunsOnlyOnceAllowed(t *testing.T) {
	h := kernelHarness(t)
	session := h.d.Sessions[1].SessionID
	conv, _ := h.kernel.id(session)

	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	repoMarker := filepath.Join(t.TempDir(), "repo-started")
	writeMCPFile(t, filepath.Join(repo, ".factory", "mcp.json"), map[string]any{"repo": testMCPEntry(t, repoMarker)})
	globalMarker := filepath.Join(t.TempDir(), "global-started")
	globalFile := filepath.Join(h.kernel.k.UserDir, "mcp.json")
	writeMCPFile(t, globalFile, map[string]any{"mine": testMCPEntry(t, globalMarker)})

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if _, err := h.testConn().Configure(ctx, kernel.ConfigureParams{ID: conv, Change: &kernel.AgentChange{Cwd: kernel.Some(repo)}}); err != nil {
		t.Fatal(err)
	}
	h.a.reloadList()
	h.until("the Session's repository", func() bool { return h.a.repoRootOf(session) == repo })

	h.openSession("Add dark mode toggle")
	h.until("the composer", func() bool { return h.hasButton("Skills and MCP servers") })
	h.click("Skills and MCP servers")
	h.click("MCP servers")
	h.until("the repository's server waiting", func() bool {
		return h.hasText("Waiting for your OK") && h.hasText("wants to run:") && h.hasText("-test.run=^$")
	})
	h.until("the user's server connected", func() bool { return h.hasText("Connected") })
	if n := startedPids(repoMarker); n != 0 {
		t.Fatalf("the repository's server ran %d times before it was allowed", n)
	}
	if n := startedPids(globalMarker); n != 1 {
		t.Fatalf("the user's server ran %d times", n)
	}

	h.click("Allow repo")
	h.until("the repository's server connected", func() bool {
		listed, err := h.testConn().MCP(ctx, kernel.MCPListParams{RepoRoot: repo})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range listed.Servers {
			if s.Status != "connected" {
				return false
			}
		}
		return len(listed.Servers) == 2 && !h.hasText("Waiting for your OK")
	})
	if n := startedPids(repoMarker); n != 1 {
		t.Fatalf("the repository's server ran %d times", n)
	}

	h.click("mine enabled")
	h.until("the user's server off", func() bool { return h.hasText("Disabled") })
	raw, err := os.ReadFile(globalFile)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if file.MCPServers["mine"]["disabled"] != true {
		t.Errorf("the global file does not switch it off: %s", raw)
	}
}
