package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/session"
)

// The Daemon cleans up a Session left idle for half an hour. Its page stays
// open, and the next message loads the Session again before it is sent.
func TestASessionTheDaemonCleanedUpTakesTheNextMessage(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "reply", Deltas: []string{"Back again."}}), "")
	h.openSession("Deploy")
	h.until("the Session loaded", func() bool { return h.hasText("Context used") })
	id := h.a.Route().SessionID
	if err := h.d.Notify(id, map[string]any{"type": "session_inactivity", "message": "Session cleaned up after inactivity", "timestamp": 1, "timeoutSeconds": 1800}); err != nil {
		t.Fatal(err)
	}
	h.until("the Session not loaded", func() bool { return h.ctl.Store().LoadState(id) == session.NotLoaded })

	h.click("Message")
	h.typeInto("still there?")
	if got := h.sessionText(); got != "still there?" {
		t.Fatalf("text typed after the cleanup: %q", got)
	}
	h.tt.Key(0, ui.KeyEnter)
	h.until("the reply", func() bool { return h.hasText("Back again.") })
	if _, err := h.d.WaitForRequest("daemon.load_session", 2); err != nil {
		t.Fatalf("the Session was not loaded again: %v", err)
	}
}

// A Session whose view was dropped is forgotten by the Store, but the
// Daemon still tells this Client when it cleans the Session up. Opening it
// again loads it; that older notice must not mark it not loaded again.
func TestASessionCleanedUpAfterItsViewWasDroppedOpensReady(t *testing.T) {
	var specs []fakedaemon.SessionSpec
	for _, title := range []string{"One", "Two", "Three", "Four"} {
		specs = append(specs, fakedaemon.SessionSpec{Title: title, Cwd: "/Users/dev/acme-web",
			Messages: []fakedaemon.Message{{Role: "user", Text: "hi " + title}}})
	}
	h := newHarness(t, fakedaemon.Scenario{Sessions: specs}, "")
	var first string
	for _, title := range []string{"One", "Two", "Three", "Four"} {
		h.openSession(title)
		id := h.a.Route().SessionID
		h.until("the Session loaded", func() bool { return h.ctl.Store().LoadState(id) == session.Loaded })
		if first == "" {
			first = id
		}
	}
	if _, kept := h.a.views[first]; kept {
		t.Fatal("the first Session's view is still kept")
	}
	if err := h.d.Notify(first, map[string]any{"type": "session_inactivity", "message": "Session cleaned up after inactivity", "timestamp": 1, "timeoutSeconds": 1800}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.d.WaitForRequest("daemon.load_session", 4); err != nil {
		t.Fatal(err)
	}
	// The notice reaches the Client before the next click.
	time.Sleep(200 * time.Millisecond)

	h.openSession("One")
	if _, err := h.d.WaitForRequest("daemon.load_session", 5); err != nil {
		t.Fatal(err)
	}
	h.until("the Session loaded again", func() bool {
		h.frame()
		return h.ctl.Store().LoadState(first) == session.Loaded
	})
	time.Sleep(200 * time.Millisecond)
	h.frame()
	if ls := h.ctl.Store().LoadState(first); ls != session.Loaded {
		t.Fatalf("load state after opening it again: %s", ls)
	}
	h.click("Message")
	h.typeInto("back")
	if got := h.sessionText(); got != "back" {
		t.Fatalf("text typed after opening it again: %q", got)
	}
}
