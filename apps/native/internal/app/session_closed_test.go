package app

import (
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"
)

// The Host closes an Automation run's Session once its turn ends, and the
// Daemon tells every Client attached to it, so the Store drops the Session
// while its page may be open. The page stays, and the Session loads again:
// its transcript comes back, and it takes the next message.
func TestASessionClosedWhileItsPageIsOpenStaysOpen(t *testing.T) {
	keepOnDaemon(t, goesWithCleanup)
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "reply", Deltas: []string{"Back again."}}), "")
	h.openSession("Deploy")
	h.until("the Session loaded", func() bool { return h.hasText("Context used") })
	id := h.a.Route().SessionID
	loadsBefore := h.count("daemon.load_session")
	if err := h.ctl.CloseSession(t.Context(), protocol.CloseSessionParams{SessionID: id}); err != nil {
		t.Fatal(err)
	}
	h.until("the Session loaded again", func() bool {
		return h.count("daemon.load_session") > loadsBefore && h.ctl.Store().LoadState(id) == session.Loaded && h.hasText("hi")
	})
	if got := h.a.Route().SessionID; got != id {
		t.Fatalf("the page moved to %q", got)
	}
	h.send("still there?")
	h.until("the reply", func() bool { return h.hasText("Back again.") })
}
