package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
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
