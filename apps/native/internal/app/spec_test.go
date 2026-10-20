package app

import (
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"
)

func specScenario() fakedaemon.Scenario {
	return fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{
			Title: "Add a greeting", Cwd: "/Users/dev/acme-web",
			Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}},
			Extra:    map[string]any{"settings": map[string]any{"interactionMode": "spec"}},
		}},
		Turn: &fakedaemon.Turn{Kind: "spec", Plan: "## Steps\n\n1. Create `hello.txt` with a greeting\n2. Read it back to check it", Title: "Add hello.txt", Reply: "Created hello.txt."},
	}
}

// inMode tells whether the Session is in the interaction mode.
func (h *harness) inMode(id string, mode protocol.DroidInteractionMode) bool {
	s := h.a.ctl.Store().Session(id)
	return s != nil && s.Settings().InteractionMode == mode
}

// Approved to go on in a new Session, the plan is carried out there, and
// the window follows it.
func TestASpecHandoffOpensTheNewSession(t *testing.T) {
	h := newHarness(t, specScenario(), "")
	h.openSession("Add a greeting")
	h.send("Plan hello.txt")
	h.until("the spec review", func() bool { return h.hasText("Approve the plan above") })
	h.shoot("spec-review")
	from := h.a.Route().SessionID
	h.click("Start in a new session")
	h.click("Proceed in a new session")
	h.until("the new Session", func() bool { r := h.a.Route(); return r.Name == "session" && r.SessionID != from })
	h.until("the work in the new Session", func() bool { return h.hasText("Created hello.txt.") })
	if !h.hasText("Implement the following plan") {
		t.Errorf("the new Session does not show the plan it was given: %q", h.tt.Texts())
	}
}

// The Spec switch beside the composer turns Spec mode on and off.
func TestTheSpecSwitchTogglesSpecMode(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "reply", Deltas: []string{"On it."}}), "")
	h.openSession("Deploy")
	h.until("the Spec switch", func() bool { _, ok := h.tt.Find("Spec mode"); return ok })
	id := h.a.Route().SessionID
	h.click("Spec mode")
	h.until("Spec mode on", func() bool { return h.inMode(id, protocol.DroidInteractionModeSpec) })
	h.click("Spec mode")
	h.until("Spec mode off", func() bool { return h.inMode(id, protocol.DroidInteractionModeAuto) })
}

// A Session started from the New session page with Spec on starts in Spec
// mode.
func TestTheNewSessionPageStartsASessionInSpecMode(t *testing.T) {
	h := newHarness(t, draftScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.click("Spec mode")
	h.start("Plan a greeting")
	h.until("the Session", func() bool { return h.a.Route().Name == "session" })
	id := h.a.Route().SessionID
	h.until("Spec mode", func() bool { return h.inMode(id, protocol.DroidInteractionModeSpec) })
}
