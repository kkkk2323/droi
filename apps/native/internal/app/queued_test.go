package app

import (
	"strings"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

// queueDuringTurn sends a message, then queues text while that turn runs.
func (h *harness) queueDuringTurn(text string) {
	h.t.Helper()
	h.send("Deploy it")
	h.until("the turn to run", func() bool { _, ok := h.tt.Find("Cancel"); return ok })
	_ = h.tt.Click("Message")
	h.frame()
	h.tt.Type(text)
	h.frame()
	h.click("Queue")
	h.until("the queued message", func() bool { _, ok := h.tt.Find(text); return ok })
}

func slowTurn() *fakedaemon.Turn {
	return &fakedaemon.Turn{Kind: "reply", Deltas: []string{"One. ", "Two. ", "Three."}, DelayMs: 1500}
}

// The Daemon answers a delete without a notification, so the row has to go
// once the request succeeds.
func TestAQueuedMessageCanBeRemoved(t *testing.T) {
	h := newHarness(t, deployScenario(slowTurn()), "")
	h.openSession("Deploy")
	h.queueDuringTurn("never mind")
	h.click("Remove queued message")
	req, err := h.d.WaitForRequest("daemon.resolve_queued_user_message", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(req.Params), `"action":"delete"`) {
		t.Fatalf("params %v", req.Params)
	}
	h.until("the queued message to go", func() bool { return !h.hasText("never mind") })
}

func TestAQueuedMessageCopiesItsSelection(t *testing.T) {
	h := newHarness(t, deployScenario(slowTurn()), "")
	h.openSession("Deploy")
	const text = "open a PR when done"
	h.queueDuringTurn(text)
	r, _ := h.tt.Find(text)
	h.tt.Press(r.X+1, r.Y+r.H/2)
	h.tt.Move(r.X+r.W-1, r.Y+r.H/2)
	h.tt.Release(r.X+r.W-1, r.Y+r.H/2)
	h.frame()
	h.tt.Command("copy")
	h.frame()
	if got := h.tt.Clipboard(); got == "" || !strings.HasPrefix(text, got) {
		t.Fatalf("copied %q", got)
	}
}
