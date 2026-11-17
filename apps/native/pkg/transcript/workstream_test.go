package transcript

import (
	"encoding/json"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/protocol"
)

func wsCall(name string, input map[string]any, result string, failed bool) *ToolCall {
	c := &ToolCall{Use: protocol.ToolUse{Name: WorkstreamServer + "___" + name}, Input: input}
	if result != "" {
		raw, _ := json.Marshal(result)
		c.Result = &protocol.ToolResult{Content: raw, IsError: &failed}
	}
	return c
}

func TestWorkstreamToolsAreOnlyThoseOfItsServer(t *testing.T) {
	if tool, ok := WorkstreamTool(wsCall("spawn_agent", nil, "", false)); !ok || tool != ToolSpawnAgent {
		t.Fatal("spawn_agent is a Workstream tool")
	}
	other := &ToolCall{Use: protocol.ToolUse{Name: "memory___spawn_agent"}}
	if _, ok := WorkstreamTool(other); ok {
		t.Fatal("another server's tool of the same name")
	}
	if _, ok := WorkstreamTool(wsCall("rm_rf", nil, "", false)); ok {
		t.Fatal("an unknown tool")
	}
}

func TestAWorkstreamLineFollowsTheCall(t *testing.T) {
	spawn := wsCall("spawn_agent", map[string]any{"title": "Docs"}, "", false)
	if got := WorkstreamLine(spawn, ToolSpawnAgent, "Docs"); got != "Starting up" {
		t.Fatalf("running: %q", got)
	}
	spawn = wsCall("spawn_agent", map[string]any{"title": "Docs"}, `Started agent "Docs" (a1b2).`, false)
	if got := WorkstreamLine(spawn, ToolSpawnAgent, "Docs"); got != "Started" {
		t.Fatalf("done: %q", got)
	}
	send := wsCall("send_message", map[string]any{"agent_id": "a1b2"}, "The message did not reach the agent: gone", true)
	if got := WorkstreamLine(send, ToolSendMessage, "Docs"); got != "Failed to message Docs" {
		t.Fatalf("failed: %q", got)
	}
	if tool, ok := WorkstreamTool(wsCall("rename_agent", nil, "", false)); !ok || tool != ToolRenameAgent {
		t.Fatal("rename_agent is a Workstream tool")
	}
	renRun := wsCall("rename_agent", map[string]any{"agent_id": "a1b2", "title": "API"}, "", false)
	if got := WorkstreamLine(renRun, ToolRenameAgent, "API"); got != "Renaming API" {
		t.Fatalf("rename running: %q", got)
	}
	renDone := wsCall("rename_agent", map[string]any{"agent_id": "a1b2", "title": "API"}, `Renamed agent "API" (a1b2) (was "Docs").`, false)
	if got := WorkstreamLine(renDone, ToolRenameAgent, "API"); got != "Renamed API" {
		t.Fatalf("rename done: %q", got)
	}
	if id, title := AgentOf(renDone); id != "a1b2" || title != "API" {
		t.Fatalf("rename AgentOf: %q %q", id, title)
	}
	renFail := wsCall("rename_agent", map[string]any{"agent_id": "a1b2", "title": "API"}, `The user renamed agent "Docs" (a1b2), so its title stays.`, true)
	if got := WorkstreamLine(renFail, ToolRenameAgent, "Docs"); got != "Failed to rename Docs" {
		t.Fatalf("rename failed: %q", got)
	}
}

func TestTheAgentACallIsAbout(t *testing.T) {
	spawn := wsCall("spawn_agent", map[string]any{"title": "Docs"}, `Started agent "Docs" (a1b2). It works in the worktree /x.`, false)
	if id, title := AgentOf(spawn); id != "a1b2" || title != "Docs" {
		t.Fatalf("spawn: %q %q", id, title)
	}
	send := wsCall("send_message", map[string]any{"agent_id": "a1b2", "message": "go"}, `Sent to agent "Docs" (a1b2). Its report arrives.`, false)
	if id, title := AgentOf(send); id != "a1b2" || title != "Docs" {
		t.Fatalf("send: %q %q", id, title)
	}
	pending := wsCall("send_message", map[string]any{"agent_id": "a1b2"}, "", false)
	if id, title := AgentOf(pending); id != "a1b2" || title != "" {
		t.Fatalf("pending: %q %q", id, title)
	}
}

func TestCallsOfOneToolGroupWhileTheyFollowEachOther(t *testing.T) {
	a := wsCall("spawn_agent", map[string]any{"title": "A"}, `Started agent "A" (1).`, false)
	b := wsCall("spawn_agent", map[string]any{"title": "B"}, `Started agent "B" (2).`, false)
	c := wsCall("spawn_agent", map[string]any{"title": "C"}, "The agent was not started: no room", true)
	list := wsCall("list_agents", nil, "[]", false)
	d := wsCall("spawn_agent", map[string]any{"title": "D"}, "", false)
	runs := WorkstreamRuns([]*ToolCall{a, b, c, list, d})
	if len(runs) != 3 || len(runs[0]) != 3 || len(runs[1]) != 1 || len(runs[2]) != 1 {
		t.Fatalf("runs %v", runs)
	}
	if got, _ := WorkstreamGroup(runs[0]); got != "Started 2 of 3 agents" {
		t.Fatalf("partly failed: %q", got)
	}
	if got, ok := WorkstreamGroup(runs[1]); ok {
		t.Fatalf("a call alone is a group: %q", got)
	}
	if got, _ := WorkstreamGroup([]*ToolCall{a, b}); got != "Started 2 agents" {
		t.Fatalf("done: %q", got)
	}
	if got, _ := WorkstreamGroup([]*ToolCall{a, d}); got != "Starting 2 agents" {
		t.Fatalf("running: %q", got)
	}
	if got, _ := WorkstreamGroup([]*ToolCall{c, c}); got != "Failed to start 2 agents" {
		t.Fatalf("failed: %q", got)
	}
	r1 := wsCall("rename_agent", map[string]any{"agent_id": "1", "title": "One"}, `Renamed agent "One" (1) (was "A").`, false)
	r2 := wsCall("rename_agent", map[string]any{"agent_id": "2", "title": "Two"}, `Renamed agent "Two" (2) (was "B").`, false)
	rFail := wsCall("rename_agent", map[string]any{"agent_id": "3", "title": "Three"}, "locked", true)
	rWait := wsCall("rename_agent", map[string]any{"agent_id": "4", "title": "Four"}, "", false)
	if got, _ := WorkstreamGroup([]*ToolCall{r1, r2}); got != "Renamed 2 agents" {
		t.Fatalf("rename done group: %q", got)
	}
	if got, _ := WorkstreamGroup([]*ToolCall{r1, rFail}); got != "Renamed 1 of 2 agents" {
		t.Fatalf("rename partial group: %q", got)
	}
	if got, _ := WorkstreamGroup([]*ToolCall{r1, rWait}); got != "Renaming 2 agents" {
		t.Fatalf("rename running group: %q", got)
	}
	if got, _ := WorkstreamGroup([]*ToolCall{rFail, rFail}); got != "Failed to rename 2 agents" {
		t.Fatalf("rename failed group: %q", got)
	}
}

func TestReadingAnAgentsMessage(t *testing.T) {
	m, ok := ReadAgentMessage("[From agent \"Docs\" (a1b2)] done\nMoved it.\n\nTests pass.")
	if !ok || m.Title != "Docs" || m.ID != "a1b2" || m.Kind != "done" || m.Body != "Moved it.\n\nTests pass." {
		t.Fatalf("%+v", m)
	}
	m, ok = ReadAgentMessage("[From agent \"Docs\" (a1b2)] Its turn ended without a report (error).")
	if !ok || m.Kind != "" || m.Body != "Its turn ended without a report (error)." {
		t.Fatalf("%+v", m)
	}
	for _, no := range []string{"[From orchestrator]\nhi", "hello", "[From agent \"x\" ()] done"} {
		if _, ok := ReadAgentMessage(no); ok {
			t.Fatalf("%q read", no)
		}
	}
}
