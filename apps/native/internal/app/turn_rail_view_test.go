package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

func railScenario(turns int) fakedaemon.Scenario {
	var msgs []fakedaemon.Message
	for i := range turns {
		msgs = append(msgs, fakedaemon.Message{Role: "user", Text: fmt.Sprintf("Question %d", i)})
		msgs = append(msgs, fakedaemon.Message{Role: "assistant", Text: fmt.Sprintf("Answer %d.\n\n%s", i, strings.Repeat("A line of the answer that wraps.\n\n", 6))})
	}
	return fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Long", Cwd: "/Users/dev/acme-web", Messages: msgs}}}
}

func TestTurnRailJumpsToAMessage(t *testing.T) {
	h := newHarness(t, railScenario(12), "")
	h.tt.SetSize(1400, 800)
	h.openSession("Long")
	h.until("the rail", func() bool { _, ok := h.tt.Find("Jump to message 1"); return ok })
	h.settle()
	if _, ok := h.tt.Find("Jump to message 12, current"); !ok {
		t.Fatalf("the last message is not current: %q", h.tt.Texts())
	}
	h.tt.Move(0, 0)
	r, _ := h.tt.Find("Jump to message 2")
	h.tt.Move(r.X+r.W-4, r.Y+r.H/2)
	h.frame()
	if !h.hasText("Question 1") || !h.hasText("Answer 1.") {
		t.Errorf("no preview of message 2")
	}
	h.click("Jump to message 2")
	h.settle()
	h.settle()
	if _, ok := h.tt.Find("Jump to message 2, current"); !ok {
		t.Fatalf("the jump did not land on message 2: %q", h.tt.Texts())
	}
}

// A message from before the loaded ones is on the rail too; a click loads
// the way to it.
func TestTurnRailLoadsAnOlderMessage(t *testing.T) {
	// Older pages than the transcript loads on its own at the first frame.
	h := newHarness(t, railScenario(LoadedMessageLimit), "")
	h.tt.SetSize(1400, 800)
	h.openSession("Long")
	h.until("every message on the rail", func() bool { _, ok := h.tt.Find("Jump to message 1"); return ok })
	v := h.a.views[h.a.Route().SessionID]
	before := len(v.entries)
	h.until("the rail's whole list", func() bool { return len(v.rail.all) == LoadedMessageLimit })
	h.settle()
	// Scroll the rail to its first mark, the pointer over it.
	cur, _ := h.tt.Find(fmt.Sprintf("Jump to message %d, current", LoadedMessageLimit))
	x, y := cur.X+cur.W-4, cur.Y+cur.H/2
	h.tt.Move(x, y)
	h.tt.Scroll(x, y, 0, -100000)
	h.frame()
	h.frame()
	first, ok := h.tt.Find("Jump to message 1")
	if !ok || first.H == 0 {
		t.Fatalf("the first mark is not in view: %+v", first)
	}
	h.click("Jump to message 1")
	h.until("the first message loaded", func() bool {
		return len(v.entries) > before && v.entries[0].User && entryText(v.entries[0]) == "Question 0"
	})
	h.settle()
	h.settle()
	if !h.hasText("Question 0") {
		t.Fatalf("the jump did not show the first message")
	}
}
