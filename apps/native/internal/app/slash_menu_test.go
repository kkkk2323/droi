package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
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

// Backspace in the empty text area removes the picked skill's tag; typing
// goes on in the text area.
func TestRemovingAPickedSkillKeepsTheFocusInTheComposer(t *testing.T) {
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
	h.tt.Key(0, ui.KeyBackspace)
	h.frame()
	if _, ok := h.tt.Find("Skill wait-what"); ok {
		t.Fatal("Backspace kept the skill's tag")
	}
	h.tt.Type("go")
	h.frame()
	if got := h.a.views[h.a.Route().SessionID].composer.text; got != "go" {
		t.Fatalf("text after removing the skill: %q", got)
	}
}

// The New session page shows a picked skill as a tag before its text area,
// as a Session's composer does; Backspace in the empty text area removes it.
func TestTheNewSessionPageShowsAPickedSkillsTag(t *testing.T) {
	h := newHarness(t, draftScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.click("Message")
	h.typeInto("/wa")
	h.until("the skill in the suggestions", func() bool { return h.hasText("wait-what") })
	h.tt.Key(0, ui.KeyTab)
	h.frame()
	if _, ok := h.tt.Find("Skill wait-what"); !ok {
		t.Fatalf("no tag for the picked skill: %q", h.tt.Texts())
	}
	h.typeInto("go")
	if got := h.a.newPage.text; got != "/wait-what go" {
		t.Fatalf("text after the pick: %q", got)
	}
	for range "go" {
		h.tt.Key(0, ui.KeyBackspace)
		h.frame()
	}
	h.tt.Key(0, ui.KeyBackspace)
	h.frame()
	if _, ok := h.tt.Find("Skill wait-what"); ok {
		t.Fatal("Backspace kept the skill's tag")
	}
	h.typeInto("go")
	if got := h.a.newPage.text; got != "go" {
		t.Fatalf("text after removing the skill: %q", got)
	}
}
