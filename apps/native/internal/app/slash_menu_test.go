package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

// Until the Daemon has loaded a Session it has no commands or skills for
// it. The slash suggestions wait for the load, so the skills are there
// when "/" is typed, not a minute later.
func TestSlashSuggestsTheSkillsOfASessionOpenedFromDisk(t *testing.T) {
	h := newHarness(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "One", Cwd: "/Users/dev/acme-web",
			Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}, Extra: map[string]any{"inactive": true}}},
		Input: map[string]any{"loadDelayMs": 300, "skills": []map[string]any{{"name": "wait-what", "description": "Re-pitch the last message"}}},
	}, "")
	h.openSession("One")
	h.until("the Session loaded", func() bool { return h.hasText("Context used") })
	h.click("Message")
	h.tt.Type("/")
	h.until("the skill in the suggestions", func() bool { return h.hasText("wait-what") })
}

// Picking a skill puts its tag before the text area; typing goes on in the
// text area without a click.
func TestPickingASkillKeepsTheFocusInTheComposer(t *testing.T) {
	h := newHarness(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "One", Cwd: "/Users/dev/acme-web",
			Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}}},
		Input: map[string]any{"skills": []map[string]any{{"name": "wait-what", "description": "Re-pitch the last message"}}},
	}, "")
	h.openSession("One")
	h.until("the Session loaded", func() bool { return h.hasText("Context used") })
	h.click("Message")
	h.tt.Type("/wait")
	h.until("the skill in the suggestions", func() bool { return h.hasText("wait-what") })
	h.tt.Key(0, ui.KeyTab)
	h.frame()
	h.tt.Type("go")
	h.frame()
	if got := h.a.views[h.a.Route().SessionID].composer.text; got != "/wait-what go" {
		t.Fatalf("text after the pick: %q", got)
	}
}
