package app

import (
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
)

func wsUse(id, name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": "droi-workstream___" + name, "input": input}
}

func wsResult(id, text string, failed bool) fakedaemon.Message {
	return fakedaemon.Message{Role: "tool", Content: []map[string]any{{"type": "tool_result", "toolUseId": id, "content": text, "isError": failed}}}
}

// orchestratorTalks gives the orchestrator the history of one that started
// agents, asked after them, wrote to one, and heard back.
func orchestratorTalks() fakedaemon.Scenario {
	sc := workstreamsScenario()
	sc.Sessions[1].Messages = []fakedaemon.Message{
		{Role: "user", Text: "Go on"},
		{Role: "assistant", Content: []map[string]any{
			wsUse("t1", "spawn_agent", map[string]any{"title": "Docs sweep", "prompt": "Update the docs for the new API."}),
			wsUse("t2", "spawn_agent", map[string]any{"title": "Billing", "prompt": "Move billing."}),
			wsUse("t3", "spawn_agent", map[string]any{"title": "Too many", "prompt": "One more."}),
			wsUse("t4", "list_agents", map[string]any{}),
			wsUse("t5", "send_message", map[string]any{"agent_id": "agent-1", "message": "Please add **tests**."}),
		}},
		wsResult("t1", `Started agent "Docs sweep" (agent-9). It works in the workspace /Users/dev/pay.`, false),
		wsResult("t2", `Started agent "Billing" (agent-8). It works in the workspace /Users/dev/pay.`, false),
		wsResult("t3", "The agent was not started: no room", true),
		wsResult("t4", "[]", false),
		wsResult("t5", `Sent to agent "gRPC endpoints" (agent-1). Its report arrives as a message.`, false),
		{Role: "user", Text: "[From agent \"gRPC endpoints\" (agent-1)] done\nMoved the first endpoint.\n\nTests pass."},
	}
	return sc
}

// The orchestrator's tools read as what they did, not as MCP calls: agents
// started, listed, written to, and an agent's report as a card that opens.
func TestTheOrchestratorsToolCallsHaveTheirOwnRows(t *testing.T) {
	h := workstreamsHarnessOf(t, orchestratorTalks(), &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.until("the group", func() bool { return h.hasText("Started 2 of 3 agents") })
	for _, want := range []string{"Docs sweep", "Billing", "Too many", "Failed to start", "Listed available agents", "Messaged gRPC endpoints", "Received message from"} {
		if !h.hasText(want) {
			t.Errorf("no %q in %q", want, h.tt.Texts())
		}
	}
	if h.hasText("droi-workstream") || h.hasText("spawn_agent") {
		t.Errorf("a generic MCP row: %q", h.tt.Texts())
	}
	if h.hasText("Moved the first endpoint.") {
		t.Fatal("the report is open before a click")
	}
	h.shot("workstream-transcript")
	h.click("Received message from gRPC endpoints")
	h.until("the report", func() bool { return h.hasText("Moved the first endpoint.") })
	h.click("Docs sweep: Started")
	h.until("the prompt", func() bool { return h.hasText("Update the docs for the new API.") })
}

// The list_agents row opens the panel that lists them.
func TestTheListAgentsRowOpensThePanel(t *testing.T) {
	h := workstreamsHarnessOf(t, orchestratorTalks(), &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.until("the row", func() bool { return h.finds("Listed available agents") })
	h.click("Hide panel")
	h.until("the panel gone", func() bool { return h.finds("Show panel") })
	h.click("Listed available agents")
	h.until("the panel", func() bool { return h.finds("Workstream panel") })
}

// A Script that starts agents shows them as the same rows, and the group
// they make heads the cluster.
func TestAgentsStartedFromAScriptShowAsRows(t *testing.T) {
	sc := workstreamsScenario()
	nested := func(id, title string) map[string]any {
		use := wsUse(id, "spawn_agent", map[string]any{"title": title, "prompt": "Do " + title})
		use["scriptExecution"] = map[string]any{"runId": "s1", "outerToolUseId": "s1"}
		return use
	}
	sc.Sessions[1].Messages = []fakedaemon.Message{
		{Role: "user", Text: "Go on"},
		{Role: "assistant", Content: []map[string]any{
			{"type": "tool_use", "id": "s1", "name": "Script", "input": map[string]any{"script": "await tools.droi_workstream.spawn_agent({})"}},
			nested("n1", "Alpha"), nested("n2", "Beta"),
		}},
		wsResult("n1", `Started agent "Alpha" (agent-7). It works in the workspace /Users/dev/pay.`, false),
		wsResult("n2", `Started agent "Beta" (agent-6). It works in the workspace /Users/dev/pay.`, false),
		wsResult("s1", "{\"toolCallId\":\"s1\",\"status\":\"completed\"}\n[Script completed · 2 calls]", false),
	}
	h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.until("the rows", func() bool { return h.hasText("Alpha") && h.hasText("Beta") })
	if !h.hasText("Started 2 agents") {
		t.Fatalf("no group in %q", h.tt.Texts())
	}
	for _, no := range []string{"Used", "Script"} {
		if h.hasText(no) {
			t.Fatalf("a %q row over the agents: %q", no, h.tt.Texts())
		}
	}
}

// An agent's own report reads as one line with its kind, which opens to
// what it said.
func TestAnAgentsReportIsARow(t *testing.T) {
	sc := workstreamsScenario()
	sc.Sessions[2].Messages = []fakedaemon.Message{
		{Role: "user", Text: "Move the endpoint"},
		{Role: "assistant", Content: []map[string]any{wsUse("r1", "report", map[string]any{"kind": "blocked", "message": "Need the proto **schema**."})}},
		wsResult("r1", "The orchestrator has your report.", false),
	}
	h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.until("the row", func() bool { return h.finds("gRPC endpoints, Idle") })
	h.click("gRPC endpoints, Idle")
	h.until("the report", func() bool { return h.hasText("Sent report back to the orchestrator") && h.hasText("Blocked") })
	h.click("Sent report back to the orchestrator")
	h.until("the message", func() bool { return h.hasText("Need the proto") })
}

// The row names an agent as the window does, not as a result spells it:
// a title with a quote is cut in the result.
func TestARowNamesTheAgentByTheWindowsRecord(t *testing.T) {
	sc := workstreamsScenario()
	sc.Sessions[1].Messages = []fakedaemon.Message{
		{Role: "user", Text: "Go on"},
		{Role: "assistant", Content: []map[string]any{wsUse("t5", "send_message", map[string]any{"agent_id": "agent-1", "message": "hi"})}},
		wsResult("t5", `Sent to agent "gRPC \"endpoints" (agent-1). Its report arrives.`, false),
	}
	h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.until("the row", func() bool { return h.hasText("Messaged gRPC endpoints") })
}

// What a Script did besides start agents keeps the cluster over its calls,
// and a Script that failed keeps its row, though all its calls are the
// Workstream's.
func TestAScriptWithOtherCallsOrAFailureKeepsItsShell(t *testing.T) {
	nested := func(id, name string, input map[string]any) map[string]any {
		use := wsUse(id, name, input)
		if name == "Read" {
			use["name"] = "Read"
		}
		use["scriptExecution"] = map[string]any{"runId": "s1", "outerToolUseId": "s1"}
		return use
	}
	build := func(calls []map[string]any, results []fakedaemon.Message, status string) *harness {
		sc := workstreamsScenario()
		msgs := []fakedaemon.Message{{Role: "user", Text: "Go on"},
			{Role: "assistant", Content: append([]map[string]any{{"type": "tool_use", "id": "s1", "name": "Script", "input": map[string]any{"script": "x"}}}, calls...)}}
		msgs = append(msgs, results...)
		msgs = append(msgs, fakedaemon.Message{Role: "tool", Content: []map[string]any{{"type": "tool_result", "toolUseId": "s1", "content": []map[string]any{
			{"type": "text", "text": "[Script " + status + " · 2 calls]"},
			{"type": "text", "text": "{\"toolCallId\":\"s1\",\"status\":\"" + status + "\"}"},
		}}}})
		sc.Sessions[1].Messages = msgs
		h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
		h.openOrchestrator()
		return h
	}
	mixed := build([]map[string]any{nested("n1", "spawn_agent", map[string]any{"title": "Alpha", "prompt": "a"}), nested("n2", "Read", map[string]any{"file_path": "/x"})},
		[]fakedaemon.Message{wsResult("n1", `Started agent "Alpha" (agent-7).`, false), wsResult("n2", "text", false)}, "completed")
	mixed.until("the rows", func() bool { return mixed.hasText("Alpha") && mixed.hasText("Used 2 tools") })
	failed := build([]map[string]any{nested("n1", "spawn_agent", map[string]any{"title": "Alpha", "prompt": "a"})},
		[]fakedaemon.Message{wsResult("n1", `Started agent "Alpha" (agent-7).`, false)}, "failed")
	failed.until("the rows", func() bool { return failed.hasText("Alpha") && failed.hasText("Script") })
}
