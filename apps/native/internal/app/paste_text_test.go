package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/pasted"
)

// longPaste is a log of 31 lines, long enough to paste as a card.
var longPaste = "First line of the log\n" + strings.Repeat("a line that goes on\n", 29) + "last line"

// A long text pasted waits as a card, not in the text area, which would
// take seconds over it; it goes after the message, and the transcript
// shows it as the same card, opened on a click.
func TestPastingALongTextMakesACard(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "reply", Deltas: []string{"Noted."}}), "")
	h.openSession("Deploy")
	h.focusComposer()
	h.tt.SetClipboard(longPaste)
	h.tt.Key(ui.Cmd, ui.KeyV)
	h.frame()
	v := h.a.views[h.a.Route().SessionID]
	if v.composer.text != "" || len(v.composer.pasted) != 1 {
		t.Fatalf("the paste went to the text area (%d bytes) and %d cards", len(v.composer.text), len(v.composer.pasted))
	}
	if _, ok := h.tt.Find("Pasted text, 31 lines"); !ok {
		t.Fatal("the pasted text's card is not shown")
	}
	if !h.hasText("First line of the log") {
		t.Fatal("the card does not show the paste's first line")
	}
	if d := h.a.drafts.Load(v.id); !strings.HasSuffix(d.Text, "</pasted_text>") {
		t.Fatalf("the draft lost the paste: %q", d.Text)
	}

	// A short one the text area takes.
	h.tt.SetClipboard("see this")
	h.tt.Key(ui.Cmd, ui.KeyV)
	h.frame()
	if v.composer.text != "see this" || len(v.composer.pasted) != 1 {
		t.Fatalf("a short paste: text %q, %d cards", v.composer.text, len(v.composer.pasted))
	}

	h.click("Send")
	want := pasted.Join("see this", []pasted.Text{{Text: longPaste}})
	h.sent(protocol.MethodAddUserMessage, func(p map[string]any) bool { return p["text"] == want })
	if len(v.composer.pasted) != 0 {
		t.Fatal("the card stayed after the message went")
	}
	h.until("the paste's card in the transcript", func() bool { _, ok := h.tt.Find("Show text"); return ok })
	if h.hasText("a line that goes on") {
		t.Fatal("the transcript shows the paste before it is opened")
	}
	h.click("Show text")
	if !h.hasText("last line") {
		t.Fatal("opening the card does not show the paste")
	}
	if !h.hasText("see this") {
		t.Fatal("the message's own text is not shown")
	}
}

// A pasted text can go back into the text area, or be removed.
func TestAPastedTextGoesBackOrAway(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.click("Message")
	h.tt.SetClipboard(longPaste)
	h.tt.Key(ui.Cmd, ui.KeyV)
	h.frame()
	if h.a.newPage.text != "" || len(h.a.newPage.pasted) != 1 {
		t.Fatalf("the New session page has %d bytes of text and %d cards", len(h.a.newPage.text), len(h.a.newPage.pasted))
	}
	h.click("Show in text field")
	if h.a.newPage.text != longPaste || len(h.a.newPage.pasted) != 0 {
		t.Fatalf("Show in text field: %d bytes of text, %d cards", len(h.a.newPage.text), len(h.a.newPage.pasted))
	}

	h.a.newPage.text = ""
	h.frame()
	h.tt.Key(ui.Cmd, ui.KeyV)
	h.frame()
	h.click("Remove pasted text")
	if h.a.newPage.text != "" || len(h.a.newPage.pasted) != 0 {
		t.Fatal("removing the pasted text kept it")
	}
}

// A text with a line too long for the text area cannot go back into it.
func TestAPasteWithAVeryLongLineStaysACard(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.click("Message")
	h.tt.SetClipboard(strings.Repeat("abcdefghij ", 20000))
	h.tt.Key(ui.Cmd, ui.KeyV)
	h.frame()
	if h.a.newPage.text != "" || len(h.a.newPage.pasted) != 1 {
		t.Fatalf("the New session page has %d bytes of text and %d cards", len(h.a.newPage.text), len(h.a.newPage.pasted))
	}
	if _, ok := h.tt.Find("Show in text field"); ok {
		t.Fatal("a paste with a 220 KB line offers to go into the text area")
	}
}
