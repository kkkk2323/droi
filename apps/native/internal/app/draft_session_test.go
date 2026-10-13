package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

func draftScenario() fakedaemon.Scenario {
	sc := sessionsScenario()
	sc.Turn = &fakedaemon.Turn{Kind: "reply", Deltas: []string{"On it."}}
	sc.Input = map[string]any{"skills": []map[string]any{{"name": "wait-what", "description": "Re-pitch the last message"}}}
	return sc
}

func (h *harness) count(method string) int {
	reqs, _ := h.d.Requests()
	n := 0
	for _, r := range reqs {
		if r.Method == method {
			n++
		}
	}
	return n
}

// The New session page opens a Draft Session for its Workspace, so "/"
// offers that Workspace's skills before anything is sent; the first send
// takes the draft over instead of creating another Session.
func TestSlashOnTheNewSessionPageSuggestsTheWorkspacesSkills(t *testing.T) {
	h := newHarness(t, draftScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.click("Message")
	h.typeInto("/wa")
	h.until("the skill in the suggestions", func() bool { return h.hasText("wait-what") })
	h.tt.Key(0, ui.KeyTab)
	h.frame()
	h.typeInto("go")
	if got := h.a.newPage.text; got != "/wait-what go" {
		t.Fatalf("text after the pick: %q", got)
	}
	h.tt.Key(0, ui.KeyEnter)
	h.until("the Session", func() bool { return h.a.Route().Name == "session" })
	id := h.a.Route().SessionID
	if n := h.count("daemon.initialize_session"); n != 1 {
		t.Fatalf("%d Sessions created, want the draft alone", n)
	}
	req, err := h.d.WaitForRequest("daemon.update_session_settings", 1)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		SessionID string            `json:"sessionId"`
		Tags      *[]map[string]any `json:"tags"`
	}
	_ = json.Unmarshal(req.Params, &p)
	if p.SessionID != id || p.Tags == nil || len(*p.Tags) != 0 {
		t.Fatalf("the take sent %s", req.Params)
	}
	h.until("the Session listed", func() bool {
		for _, s := range h.a.listed {
			if s.SessionID == id {
				return true
			}
		}
		return false
	})
}

// Leaving the New session page without sending closes its draft.
func TestLeavingTheNewSessionPageClosesItsDraft(t *testing.T) {
	h := newHarness(t, draftScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the draft", func() bool { return h.count("daemon.list_skills") > 0 })
	h.click("Fix the login race")
	if _, err := h.d.WaitForRequest("daemon.close_session", 1); err != nil {
		t.Fatal(err)
	}
}
