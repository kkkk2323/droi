package kernel

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReview228R3MixedSubscriptionOrder(t *testing.T) {
	c := &Client{opts: Options{MaxQueuedNotes: 2}, wake: make(chan struct{}, 1)}
	notes := []Notification{
		review228Note(NotifyConversationView, `{"id":7,"kind":"value","value":{"before":"old"}}`),
		review228Note(NotifyConversationView, `{"id":7,"kind":"full","value":{"before":"reset"}}`),
		review228Note(NotifyConversationView, `{"id":7,"kind":"value","value":null,"ops":["delta"]}`),
		review228Note(NotifyConversationView, `{"id":7,"kind":"value","value":{"before":"latest"}}`),
	}
	for _, n := range notes {
		c.enqueue(n)
	}
	if len(c.queued) == 0 || string(c.queued[len(c.queued)-1].Params) != string(notes[3].Params) {
		t.Fatalf("overflow dropped the final whole view after full/delta: queue=%v lost=%d", c.queued, c.lost)
	}
}

func TestReview228R3CancelledQueuedCallsDoNotLeakPending(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	c.writeMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- c.Call(ctx, MethodIndexUpdate, map[string]any{"id": 7, "archived": true}, nil)
	}()
	deadline := time.Now().Add(time.Second)
	for {
		c.mu.Lock()
		queued := len(c.pending) == 1
		c.mu.Unlock()
		if queued {
			break
		}
		if time.Now().After(deadline) {
			c.writeMu.Unlock()
			cancel()
			t.Fatal("request did not reach the writer")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	c.writeMu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Call error=%v", err)
	}
	c.mu.Lock()
	left := len(c.pending)
	c.mu.Unlock()
	if left != 0 {
		t.Fatalf("unsent request leaked %d pending entries", left)
	}
	// A late/unsolicited answer must not reinsert a request or block dispatch.
	c.dispatch(rpcFrame{ID: []byte(`"1"`), Result: []byte(`{}`)})
	c.mu.Lock()
	left = len(c.pending)
	c.mu.Unlock()
	if left != 0 {
		t.Fatalf("late answer leaked %d pending entries", left)
	}
}
