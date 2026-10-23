package app

import (
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
)

// A Session left with unsent text says so in the sidebar until it is sent;
// the open Session does not, as its composer shows the text.
func TestTheSidebarMarksASessionLeftWithADraft(t *testing.T) {
	h := newHarness(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{
			{Title: "One", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "first"}}},
			{Title: "Two", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "second"}}},
		},
		Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"Noted."}},
	}, "")
	h.openSession("One")
	h.focusComposer()
	h.typeInto("half a thought")
	if h.hasButton("Draft") {
		t.Fatal("the open Session is marked as a draft")
	}
	h.click("Two")
	h.until("the draft mark", func() bool { return h.hasButton("Draft") })
	h.click("One")
	h.until("the draft back in the composer", func() bool { return h.sessionText() == "half a thought" })
	h.click("Send")
	h.until("the reply", func() bool { return h.hasText("Noted.") })
	h.openRow("Two", h.d.Sessions[1].SessionID)
	h.until("the other Session", func() bool { return h.hasText("second") })
	if h.hasButton("Draft") {
		t.Fatal("a sent draft is still marked")
	}
}
