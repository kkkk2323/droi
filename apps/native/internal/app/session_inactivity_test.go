package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/session"
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
