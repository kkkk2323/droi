package app

import (
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"
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

// Editing takes the message out of the Daemon's queue and puts it back in
// the composer, which can send it again.
func TestAQueuedMessageCanBeEdited(t *testing.T) {
	h := newHarness(t, deployScenario(slowTurn()), "")
	h.openSession("Deploy")
	h.queueDuringTurn("fix the typo")
	h.click("Edit queued message")
	req, err := h.d.WaitForRequest("daemon.resolve_queued_user_message", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(req.Params), `"action":"delete"`) {
		t.Fatalf("params %v", req.Params)
	}
	h.until("the message back in the composer", func() bool { return h.sessionText() == "fix the typo" })
	v := h.a.views[h.a.Route().SessionID]
	if n := len(h.a.ctl.Store().Session(v.id).QueuedMessages()); n != 0 {
		t.Fatalf("%d messages still queued", n)
	}
}

func TestAQueuedMessageGivesBackItsTextAndImages(t *testing.T) {
	text, _ := protocol.NewContentBlock(map[string]any{"type": "text", "text": "see this"})
	pic, _ := protocol.NewContentBlock(map[string]any{"type": "image", "source": protocol.Base64ImageSource{Type: "base64", Data: "AAAA", MediaType: "image/jpeg"}})
	got, images := queuedContent(session.QueuedMessage{Content: []protocol.ContentBlock{text, pic}})
	if got != "see this" || len(images) != 1 || images[0].Data != "AAAA" || images[0].MediaType != "image/jpeg" || images[0].ID == "" {
		t.Fatalf("%q %+v", got, images)
	}
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

// A finger held on Queue hands the message to the running turn, as ⌘↩ does on
// a keyboard; a tap queues it after the turn.
func TestAPhonesQueueHeldHandsTheMessageToTheTurn(t *testing.T) {
	h := newHarnessWith(t, deployScenario(slowTurn()), "", func(c *Config) { c.Phone = true; c.InsetTop = false })
	h.tt.SetSize(402, 874)
	h.openSession("Deploy")
	h.send("Deploy it")
	h.until("the turn to run", func() bool { _, ok := h.tt.Find("Cancel"); return ok })
	_ = h.tt.Click("Message")
	h.frame()
	h.tt.Type("use the staging key")
	h.frame()
	q, ok := h.tt.Find("Queue")
	if !ok {
		t.Fatal("no Queue button")
	}
	x, y := q.X+q.W/2, q.Y+q.H/2
	h.tt.FingerDown(x, y)
	h.tt.Advance(700 * time.Millisecond)
	h.tt.FingerUp(x, y)
	h.frame()
	req, err := h.d.WaitForRequest("daemon.add_user_message", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(req.Params), `"queuePlacement":"end_of_turn"`) {
		t.Fatalf("held Queue sent %s", req.Params)
	}
}
