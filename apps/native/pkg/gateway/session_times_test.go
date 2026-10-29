package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
	"github.com/kkkk2323/droi/apps/native/pkg/sessiontimes"
)

func notification(sessionID, state string) string {
	return `{"jsonrpc":"2.0","method":"daemon.session_notification","params":{"sessionId":"` + sessionID +
		`","notification":{"type":"droid_working_state_changed","newState":"` + state + `"}}}`
}

// The Gateway dates a Session by the messages a Client sends it and ends
// its turn when the Daemon reports it idle, before the Client hears so;
// what droid adds for the model or another role is no message sent.
func TestTheBridgesNoteMessagesSentAndTurnsEnded(t *testing.T) {
	d := startStandInDaemon(t)
	times := sessiontimes.Open("", nil)
	opts := baseOptions(d)
	opts.SessionTimes = times
	g := startGateway(t, opts)
	c := connect(t, daemonURL(g.URL(), testToken))

	send(t, c, frame(addUserMessage, "1", map[string]any{"sessionId": "s1", "text": "hi"}))
	send(t, c, frame(addUserMessage, "2", map[string]any{"sessionId": "s2", "text": "note", "visibility": "llm_only"}))
	send(t, c, frame(addUserMessage, "3", map[string]any{"sessionId": "s3", "text": "x", "role": "system"}))
	for range 3 {
		nextMessage(t, c)
	}
	got := times.Snapshot().Sessions
	if got["s1"].SentAt == 0 || len(got) != 1 {
		t.Fatalf("times %+v", got)
	}

	eventually(t, func() bool { return d.socketCount() == 1 })
	d.mu.Lock()
	upstream := d.sockets[0]
	d.mu.Unlock()
	for _, state := range []string{"thinking", "idle"} {
		if err := upstream.Write(context.Background(), websocket.MessageText, []byte(notification("s1", state))); err != nil {
			t.Fatal(err)
		}
		nextMessage(t, c)
	}
	if s1 := times.Snapshot().Sessions["s1"]; !s1.Unread() {
		t.Fatalf("the turn did not end before the Client heard it: %+v", s1)
	}
}

// A Client reads the Session Times whole, then waits for what changes, and
// a Session it shows reads on every Client.
func TestClientsFollowTheSessionTimes(t *testing.T) {
	times := sessiontimes.Open("", nil)
	times.Sent("s1")
	opts := baseOptions(nil)
	opts.SessionTimes = times
	g := startGateway(t, opts)
	client, err := NewClient(daemonURL(g.URL(), testToken))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	all, err := client.SessionTimes(ctx, 0)
	if err != nil || !all.Full || all.Sessions["s1"].SentAt == 0 {
		t.Fatalf("whole: %+v %v", all, err)
	}
	next := make(chan sessions.TimesSnapshot, 1)
	go func() {
		snap, err := client.SessionTimes(ctx, all.Rev)
		if err != nil {
			t.Error(err)
		}
		next <- snap
	}()
	// Whether the view comes before the wait begins or during it, the
	// revision moves past the one waited after.
	if err := client.SessionViewed(ctx, "s2"); err != nil {
		t.Fatal(err)
	}
	snap := <-next
	if snap.Full || snap.Rev == all.Rev || snap.Sessions["s2"].ViewedAt == 0 || len(snap.Sessions) != 1 {
		t.Fatalf("change: %+v", snap)
	}

	other, err := NewClient(daemonURL(startGateway(t, baseOptions(nil)).URL(), testToken))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.SessionTimes(ctx, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a Gateway without Session Times answered %v", err)
	}
	// Closing the Gateway ends a long poll at once; shutting its listener
	// down waits for every request.
	waiting := make(chan error, 1)
	go func() {
		_, err := client.SessionTimes(ctx, snap.Rev)
		waiting <- err
	}()
	closed := make(chan struct{})
	go func() { _ = g.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(longPoll):
		t.Fatal("closing waited for the long poll")
	}
	<-waiting
	g = startGateway(t, opts)
	bad, _ := NewClient(daemonURL(g.URL(), "nope"))
	var refused *StatusError
	if _, err := bad.SessionTimes(ctx, 0); !errors.As(err, &refused) || refused.Status != 401 {
		t.Fatalf("a bad token answered %v", err)
	}
}
