package kernel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// A mixed subscription's pushes all reach the reader, in the order that makes
// them apply: the whole view the reader missed, the reset, the step, and then
// the conversation's latest state — which nothing may evict.
func TestReview228R3MixedSubscriptionOrder(t *testing.T) {
	c := &Client{queue: newNotifQueue(), wake: make(chan struct{}, 1)}
	notes := []Notification{
		review228Note(NotifyConversationView, `{"id":7,"kind":"value","value":{"before":"old"}}`),
		review228Note(NotifyConversationView, `{"id":7,"kind":"full","value":{"before":"reset"}}`),
		review228Note(NotifyConversationView, `{"id":7,"kind":"value","value":null,"ops":["delta"]}`),
		review228Note(NotifyConversationView, `{"id":7,"kind":"value","value":{"before":"latest"}}`),
	}
	for _, n := range notes {
		c.enqueue(n)
	}
	queued := review228Drain(c)
	if len(queued) == 0 || string(queued[len(queued)-1].Params) != string(notes[3].Params) {
		t.Fatalf("the conversation's latest state is not the last entry: %v", queued)
	}
	// The reset and the step are still in front of it, in the order they came.
	if !strings.Contains(string(queued[0].Params), `"kind":"full"`) || !strings.Contains(string(queued[1].Params), "delta") {
		t.Fatalf("the reset and the step were reordered: %v", queued)
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
