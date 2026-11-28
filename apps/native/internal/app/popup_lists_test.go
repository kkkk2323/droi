package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
)

// The model picker and the Skills dialog scroll their lists: the rows
// must take room, not only be built.
func TestPopupListsShowTheirRows(t *testing.T) {
	h := newHarness(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "One", Cwd: "/Users/dev/acme-web",
		Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}}}}, "")
	h.openSession("One")
	h.until("the composer", func() bool { _, ok := h.tt.Find("Model and reasoning effort"); return ok })
	h.settle()

	h.click("Model and reasoning effort")
	h.until("the models", func() bool { _, ok := h.tt.Find("Models"); return ok })
	h.frame()
	if r, _ := h.tt.Find("Models"); r.H < 40 {
		t.Errorf("the model list takes no room: %+v", r)
	}
	h.tt.Key(0, ui.KeyEscape)
	h.settle()

	h.click("Skills and MCP servers")
	h.until("the dialog", func() bool { _, ok := h.tt.Find("Tab panel"); return ok })
	h.frame()
	if r, _ := h.tt.Find("Tab panel"); r.H < 40 {
		t.Errorf("the Skills panel takes no room: %+v", r)
	}
}

// A skill whose SKILL.md frontmatter droid rejected (droid 1.23) is listed but
// not loaded: its row says so and names the fields.
func TestASkillDroidCouldNotLoadSaysWhy(t *testing.T) {
	h := newHarness(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "One", Cwd: "/Users/dev/acme-web",
			Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}}},
		Input: map[string]any{"skills": []map[string]any{
			{"name": "wait-what", "description": "Re-pitch the last message"},
			{"name": "deploy", "description": "Ship it", "frontmatterIssues": []map[string]any{
				{"path": "allowed-tools", "code": "invalid_type", "expected": "array"},
				{"path": "version", "code": "invalid"},
			}},
		}},
	}, "")
	h.openSession("One")
	h.until("the composer", func() bool { _, ok := h.tt.Find("Skills and MCP servers"); return ok })
	h.settle()
	h.click("Skills and MCP servers")
	h.until("the skills", func() bool { return h.hasText("deploy") && h.hasText("wait-what") })
	if !h.hasText("Invalid frontmatter") || !h.hasText("`allowed-tools` must be a list; `version` is invalid") {
		t.Fatal("the skill droid could not load does not say why")
	}
	h.shot("skills-invalid-frontmatter")
}
