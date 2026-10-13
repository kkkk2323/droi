package app

import (
	"encoding/json"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/defaults"
)

func TestTheNewSessionPageStartsASessionWithTheToolModePicked(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	defaults.SetDefaultToolMode(h.a.prefs, defaults.DirectOnly)
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the models", func() bool {
		dv := h.a.sessionDefaults()
		return dv != nil && len(dv.Models) > 0
	})
	h.frame()
	h.click("Model and reasoning effort")
	h.until("the Tools row", func() bool { _, ok := h.tt.Find("Tool calls"); return ok })
	h.click("Script")
	if h.a.newPage.toolMode != defaults.ScriptOnly {
		t.Fatalf("the page's tool mode is %q", h.a.newPage.toolMode)
	}
	if !h.hasText("Script") {
		t.Fatal("the picker's trigger does not name the mode")
	}
	h.click("Start session")
	h.until("the Session", func() bool { return h.a.Route().Name == "session" })
	// The Session opened is the last one asked for: a draft taken over, or
	// one created on send.
	var asked string
	reqs, _ := h.d.Requests()
	for _, r := range reqs {
		if r.Method == "daemon.initialize_session" {
			var p struct {
				ToolExecutionMode string `json:"toolExecutionMode"`
			}
			_ = json.Unmarshal(r.Params, &p)
			asked = p.ToolExecutionMode
		}
	}
	if asked != "script_only" {
		t.Fatalf("the Session was asked for %q", asked)
	}
}
