package app

import (
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
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
