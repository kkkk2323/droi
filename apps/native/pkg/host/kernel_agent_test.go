package host

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/mcpserver"
)

// standInEnv makes this test binary a stand-in app's tool server (TestStandInToolServer).
const standInEnv = "DROI_STANDIN_TOOL_SERVER"

// TestStandInToolServer is the stand-in app's tool server when the Kernel starts this test binary as one: slow to
// start, as a server that loads something first, then a stdio MCP server with one tool. Otherwise it is skipped.
func TestStandInToolServer(t *testing.T) {
	if os.Getenv(standInEnv) == "" {
		t.Skip("run by the Kernel as an MCP server")
	}
	time.Sleep(time.Second)
	err := mcpserver.Serve(os.Stdin, os.Stdout, mcpserver.Server{
		Name: "standin", Version: "1.0.0",
		Tools: json.RawMessage(`[{"name":"hello","description":"Says hello from the stand-in app.","inputSchema":{"type":"object"}}]`),
		Call: func(string, map[string]any, string) (mcpserver.Result, error) {
			return mcpserver.Text("hello", false), nil
		},
	})
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// standInAgent is a downstream app's Agent, as extension.Agent reaches the Host: its own executable as a tool server
// that the first turn waits for, one tool that runs without asking, and a section.
func standInAgent(*Host) kernel.AppAgent {
	return kernel.AppAgent{
		MCPServers: map[string]kernel.AppMCPServer{"standin": {
			Command: os.Args[0], Args: []string{"-test.run=^TestStandInToolServer$"},
			Env: map[string]string{standInEnv: "1"}, BlockOnLoad: true,
		}},
		AlwaysAllow: []string{"standin___hello"},
		Section:     "Use the stand-in tools.",
	}
}

// The Kernel is given the app's Agent at launch: its servers, the tools that run unasked, and its section after
// Settings'. An app with no section of its own has its SystemPrompt there; one with both has the section once.
func TestTheKernelIsGivenTheAppsAgent(t *testing.T) {
	agentOf := func(cfg Config) kernel.AppAgent {
		h := New(cfg)
		t.Cleanup(h.KeyPool().Stop)
		prompt := "Settings says hi."
		if err := h.Settings.Update(func(s *Settings) { s.AppendSystemPrompt = &prompt }); err != nil {
			t.Fatal(err)
		}
		for _, kv := range h.kernelEnv() {
			if v, ok := strings.CutPrefix(kv, kernel.AppAgentEnv+"="); ok {
				var a kernel.AppAgent
				if err := json.Unmarshal([]byte(v), &a); err != nil {
					t.Fatal(err)
				}
				return a
			}
		}
		t.Fatal("no " + kernel.AppAgentEnv)
		return kernel.AppAgent{}
	}
	dir := t.TempDir()
	base := Config{UserData: dir, Home: dir, Env: func(string) string { return "" }}

	withAgent := base
	withAgent.KernelAgent = standInAgent
	withAgent.AppendSystemPrompt = func() string { return "The Daemon's copy of the stand-in text." }
	a := agentOf(withAgent)
	if s := a.MCPServers["standin"]; !s.BlockOnLoad || s.Command != os.Args[0] || len(a.AlwaysAllow) != 1 {
		t.Fatalf("agent %+v", a)
	}
	if a.Section != "Settings says hi.\n\nUse the stand-in tools." {
		t.Fatalf("section %q", a.Section)
	}

	legacy := base
	legacy.UserData = t.TempDir()
	legacy.AppendSystemPrompt = func() string { return "Use acme-db." }
	if got := agentOf(legacy).Section; got != "Settings says hi.\n\nUse acme-db." {
		t.Fatalf("an app with only a SystemPrompt gives the Kernel %q", got)
	}
}

// A stand-in downstream app on the Host's Kernel: the very first turn has the app's slow tool server and its section,
// and a change of Settings' prompt reaches the running Kernel's next turn.
func TestAStandInAppsAgentReachesTheKernelsFirstTurn(t *testing.T) {
	f := newFakeFactory(t)
	hk := startHostKernelWith(t, f, true, func(cfg *Config) { cfg.KernelAgent = standInAgent })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c := hk.next(t, ctx)
	first := hk.generation()
	chat, ok := offered(t, ctx, c, "factory-chat", "deepseek-v4.1-flash-1017")
	if !ok {
		t.Fatal("no chat model")
	}
	answer(t, ctx, c, chat)
	body := f.lastBody()
	for _, want := range []string{"standin___hello", "Use the stand-in tools."} {
		if !strings.Contains(body, want) {
			t.Fatalf("the first request lacks %q", want)
		}
	}

	prompt := "Answer in French."
	if err := hk.h.Settings.Update(func(s *Settings) { s.AppendSystemPrompt = &prompt }); err != nil {
		t.Fatal(err)
	}
	hk.h.Changed()
	harnessChanged(t, ctx, c)
	hk.stillRunning(t, first)
	answer(t, ctx, c, chat)
	if body := f.lastBody(); !strings.Contains(body, "Answer in French.") || !strings.Contains(body, "Use the stand-in tools.") {
		t.Fatal("the next turn does not have Settings' new prompt before the app's section")
	}
}
