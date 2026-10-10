package app

import (
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/internal/subagents"
)

func subagentScenario() fakedaemon.Scenario {
	const main = "11111111-1111-4111-8111-111111111111"
	worker := func(id, title, status string) fakedaemon.SessionSpec {
		return fakedaemon.SessionSpec{Title: title, Cwd: "/Users/dev/acme-web",
			Messages: []fakedaemon.Message{{Role: "user", Text: "Do it"}},
			Extra: map[string]any{"sessionId": id, "subagent": map[string]any{
				"callingSessionId": main, "callingToolUseId": "toolu_" + id[:4], "subagentType": "worker",
				"description": title, "status": status,
			}}}
	}
	return fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{
		{Title: "Fix the login race", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "Fix it"}},
			Extra: map[string]any{"sessionId": main}},
		worker("22222222-2222-4222-8222-222222222222", "Worker: Patch the refresh", "completed"),
		worker("33333333-3333-4333-8333-333333333333", "Worker: Write the test", "completed"),
	}}
}

// A subagent the list does not have yet still leads back to its caller,
// and runs as soon as its caller says it started.
func TestAnUnlistedSubagentLeadsBackAndRuns(t *testing.T) {
	const main, child = "11111111-1111-4111-8111-111111111111", "44444444-4444-4444-8444-444444444444"
	sc := subagentScenario()
	// The list does not tell this one's caller, as before the Daemon lists it as a subagent.
	sc.Sessions = append(sc.Sessions, fakedaemon.SessionSpec{Title: "Worker: Translate", Cwd: "/Users/dev/acme-web",
		Messages: []fakedaemon.Message{{Role: "user", Text: "Translate"}}, Extra: map[string]any{"sessionId": child}})
	h := newHarness(t, sc, "")
	h.openSession("Fix the login race")
	h.until("the reply", func() bool { return h.hasText("Fix it") })
	if err := h.d.Notify(main, map[string]any{"type": "child_session_available", "childSessionId": child, "toolUseId": "toolu_new",
		"subagentType": "worker", "description": "Translate", "timestamp": 1}); err != nil {
		t.Fatal(err)
	}
	h.until("the run", func() bool { r, ok := h.a.subagentRun(child); return ok && r.Status == subagents.Running })
	h.a.Go(Route{Name: "session", SessionID: child})
	h.until("the trail", func() bool { _, ok := h.tt.Find("Session hierarchy"); return ok })
	h.settle()
	trail, _ := h.tt.Find("Session hierarchy")
	h.tt.ClickAt(trail.X+20, trail.Y+trail.H/2)
	h.frame()
	if r := h.a.Route(); r.SessionID != main {
		t.Fatalf("route after the crumb %+v", r)
	}
}

func TestSubagentMenuAndTrail(t *testing.T) {
	h := newHarness(t, subagentScenario(), "")
	h.openSession("Fix the login race")
	h.until("the subagent menu", func() bool { _, ok := h.findPrefix("2 subagents"); return ok })
	name, _ := h.findPrefix("2 subagents")
	h.click(name)
	h.until("the subagents listed", func() bool { return h.hasText("Worker: Write the test") })
	h.settle()
	h.click("Worker: Patch the refresh")
	if r := h.a.Route(); r.SessionID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("route %+v", r)
	}

	// The subagent's header leads back to its caller and switches to a sibling.
	h.until("the trail", func() bool { _, ok := h.tt.Find("Session hierarchy"); return ok })
	// The title's menu is the trail's last button; the sidebar has a row of the same name.
	trail, _ := h.tt.Find("Session hierarchy")
	h.tt.ClickAt(trail.X+trail.W-20, trail.Y+trail.H/2)
	h.frame()
	h.until("the siblings", func() bool { return h.hasText("Worker: Write the test") })
	h.settle()
	h.click("Worker: Write the test")
	if r := h.a.Route(); r.SessionID != "33333333-3333-4333-8333-333333333333" {
		t.Fatalf("route after switching %+v", r)
	}
	h.until("the crumb", func() bool { _, ok := h.tt.Find("Session hierarchy"); return ok })
	h.settle()
	trail, _ = h.tt.Find("Session hierarchy")
	h.tt.ClickAt(trail.X+20, trail.Y+trail.H/2)
	h.frame()
	if r := h.a.Route(); r.SessionID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("route after the crumb %+v", r)
	}
}

// A Session whose subagents run shows it in the sidebar, even when only
// the Daemon says the subagent is working.
func TestTheSidebarShowsRunningSubagents(t *testing.T) {
	const main, worker = "11111111-1111-4111-8111-111111111111", "55555555-5555-4555-8555-555555555555"
	sc := subagentScenario()
	sc.Sessions = append(sc.Sessions, fakedaemon.SessionSpec{Title: "Worker: Fix the review", Cwd: "/Users/dev/acme-web",
		Messages: []fakedaemon.Message{{Role: "user", Text: "Fix"}},
		Extra: map[string]any{"sessionId": worker, "subagent": map[string]any{
			"callingSessionId": main, "callingToolUseId": "toolu_5555", "subagentType": "worker",
			"description": "Fix the review", "status": "running",
		}}})
	h := newHarness(t, sc, "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	if err := h.d.Notify(worker, map[string]any{"type": "droid_working_state_changed", "newState": "streaming_assistant_message"}); err != nil {
		t.Fatal(err)
	}
	h.until("the running subagent", func() bool { return h.hasText("1 subagent running") })
	if err := h.d.Notify(worker, map[string]any{"type": "droid_working_state_changed", "newState": "idle"}); err != nil {
		t.Fatal(err)
	}
	h.until("the subagent stopped", func() bool { return !h.hasText("1 subagent running") && h.hasText("1 message") })
}
