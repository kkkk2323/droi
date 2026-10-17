package app

import (
	"encoding/json"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
)

// A running subagent's card shows what it does, from its own Session, and
// stops it; a TaskOutput call names it and opens it.
func TestARunningSubagentShowsItsLatestToolAndStops(t *testing.T) {
	sc := backgroundScenario(map[string]any{"sessionId": bgChild})
	sc.Sessions[0].Messages = append(sc.Sessions[0].Messages,
		fakedaemon.Message{Role: "assistant", Content: []map[string]any{{"type": "tool_use", "id": "toolu_out", "name": "TaskOutput",
			"input": map[string]any{"task_id": bgChild, "block": false}}}},
		fakedaemon.Message{Role: "tool", Content: []map[string]any{{"type": "tool_result", "toolUseId": "toolu_out", "content": "Status: running"}}},
	)
	sc.Sessions[1].Messages = append(sc.Sessions[1].Messages, fakedaemon.Message{Role: "assistant", Content: []map[string]any{
		{"type": "tool_use", "id": "toolu_read", "name": "Read", "input": map[string]any{"file_path": "src/strings.ts"}}}})
	h := newHarness(t, sc, "")
	h.openSession("Fix the login race")
	h.until("the Task card", func() bool { return h.hasText("Translate the strings") })
	h.announceChild()
	h.until("the latest tool and the count", func() bool { return h.hasText("src/strings.ts") && h.hasText("1 tool") })

	// The list the views read stays the same slice from frame to frame.
	_, listed, _, _, _, rev := h.a.snapshot()
	one, _ := h.a.knownSubagents(listed, rev)
	two, _ := h.a.knownSubagents(listed, rev)
	if len(one) == 0 || &one[0] != &two[0] {
		t.Error("knownSubagents built the list again for the same snapshot")
	}

	h.until("the TaskOutput row", func() bool { _, ok := h.tt.Find("Open Worker: Translate"); return ok })
	h.click("Stop subagent")
	req, err := h.d.WaitForRequest("daemon.interrupt_session", 1)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.SessionID != bgChild {
		t.Fatalf("interrupted %s", req.Params)
	}
	h.click("Open Worker: Translate")
	if r := h.a.Route(); r.SessionID != bgChild {
		t.Fatalf("route %+v", r)
	}
}

// A Task that resumes a subagent shows the run, with its tier; the call
// that started the subagent stays done.
func TestAResumedSubagentRunsOnTheCallThatResumedIt(t *testing.T) {
	sc := backgroundScenario(map[string]any{"sessionId": bgChild})
	sc.Sessions[0].Messages = append(sc.Sessions[0].Messages,
		fakedaemon.Message{Role: "assistant", Content: []map[string]any{{"type": "tool_use", "id": "toolu_again", "name": "Task",
			"input": map[string]any{"subagent_type": "worker", "description": "Translate the rest", "prompt": "And the rest",
				"resume": bgChild, "await": true, "complexity": "heavy"}}}})
	h := newHarness(t, sc, "")
	h.openSession("Fix the login race")
	h.until("the resuming card", func() bool {
		_, opens := h.tt.Find("Open subagent session")
		return h.hasText("Resume Worker") && h.hasText("Heavy task") && opens
	})
	if err := h.d.Notify(bgCaller, map[string]any{"type": "child_session_available", "childSessionId": bgChild, "toolUseId": "toolu_again",
		"subagentType": "worker", "description": "Translate the rest", "timestamp": 2}); err != nil {
		t.Fatal(err)
	}
	h.until("the resumed run, and the first one done", func() bool { return h.hasText("Running") && h.hasText("Completed") })
	h.finishChild()
	h.until("both done", func() bool { return h.hasText("Completed") && !h.hasText("Running") })
}

// The header's subagent menu stops a subagent that runs.
func TestTheSubagentMenuStopsARunningSubagent(t *testing.T) {
	const main, child = "11111111-1111-4111-8111-111111111111", "44444444-4444-4444-8444-444444444444"
	sc := subagentScenario()
	sc.Sessions = append(sc.Sessions, fakedaemon.SessionSpec{Title: "Worker: Translate", Cwd: "/Users/dev/acme-web",
		Messages: []fakedaemon.Message{{Role: "user", Text: "Translate"}}, Extra: map[string]any{"sessionId": child}})
	h := newHarness(t, sc, "")
	h.openSession("Fix the login race")
	h.until("the reply", func() bool { return h.hasText("Fix it") })
	if err := h.d.Notify(main, map[string]any{"type": "child_session_available", "childSessionId": child, "toolUseId": "toolu_new",
		"subagentType": "worker", "description": "Translate", "timestamp": 1}); err != nil {
		t.Fatal(err)
	}
	h.until("the subagent menu", func() bool { _, ok := h.findPrefix("3 subagents, 1 running"); return ok })
	name, _ := h.findPrefix("3 subagents, 1 running")
	h.click(name)
	h.until("the stop button", func() bool { _, ok := h.tt.Find("Stop subagent"); return ok })
	h.settle()
	h.click("Stop subagent")
	req, err := h.d.WaitForRequest("daemon.interrupt_session", 1)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.SessionID != child {
		t.Fatalf("interrupted %s", req.Params)
	}
}
