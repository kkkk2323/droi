package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func review228Note(method, params string) Notification {
	return Notification{Method: method, Params: json.RawMessage(params)}
}

// review228Drain takes everything a Client's queue holds.
func review228Drain(c *Client) []Notification { return drain(c.queue) }

func TestReview228WholeViewMustNotCrossAFullPush(t *testing.T) {
	c := &Client{queue: newNotifQueue(), wake: make(chan struct{}, 1)}
	c.enqueue(review228Note(NotifyConversationView, `{"id":7,"kind":"value","value":{"conversation":{"id":7},"entries":[{"id":1,"conversationId":7,"kind":"pi.system"}],"docs":{}},"ops":[],"retired":false}`))
	c.enqueue(review228Note(NotifyConversationView, `{"id":7,"kind":"full","value":{"conversation":{"id":7},"entries":[{"id":2,"conversationId":7,"kind":"pi.system"}],"docs":{}},"ops":[],"retired":false}`))
	c.enqueue(review228Note(NotifyConversationView, `{"id":7,"kind":"value","value":{"conversation":{"id":7},"entries":[{"id":3,"conversationId":7,"kind":"pi.system"}],"docs":{}},"ops":[],"retired":false}`))
	queued := review228Drain(c)
	if len(queued) != 2 {
		t.Fatalf("the reset and the state after it came out as %d entries", len(queued))
	}
	last := queued[len(queued)-1]
	var v struct {
		Kind  string `json:"kind"`
		Value struct {
			Entries []Entry `json:"entries"`
		} `json:"value"`
	}
	if err := json.Unmarshal(last.Params, &v); err != nil {
		t.Fatal(err)
	}
	if v.Kind != "value" || len(v.Value.Entries) != 1 || v.Value.Entries[0].ID != 3 {
		t.Fatalf("the last applied view is %s, not latest", last.Params)
	}
	// The reset is in front of it, so a delta reader applies the whole view and
	// then the state after it, in that order.
	if !strings.Contains(string(queued[0].Params), `"kind":"full"`) {
		t.Fatalf("the reset is not the first entry: %s", queued[0].Params)
	}
}

func TestReview228MustArriveFloodKeepsEveryStep(t *testing.T) {
	c := &Client{queue: newNotifQueue(), wake: make(chan struct{}, 1)}
	notes := []Notification{
		review228Note(NotifyApprovalRequested, `{"requestId":"a","id":7}`),
		review228Note(NotifyAskRequested, `{"requestId":"q","id":7}`),
		review228Note(NotifyConversationView, `{"id":7,"kind":"full","value":{"before":"reset"}}`),
		review228Note(NotifyConversationView, `{"id":7,"kind":"value","value":null,"ops":["first"]}`),
		review228Note(NotifyConversationView, `{"id":7,"kind":"value","value":null,"ops":["second"]}`),
		review228Note(NotifyConversationBusy, `{"id":7,"run":null}`),
	}
	for _, n := range notes {
		c.enqueue(n)
	}
	queued := review228Drain(c)
	if len(queued) != len(notes) {
		t.Fatalf("kept %d of %d must-arrive notifications: %v", len(queued), len(notes), queued)
	}
	seen := map[string]bool{}
	for _, n := range queued {
		seen[string(n.Params)] = true
	}
	for _, n := range notes {
		if !seen[string(n.Params)] {
			t.Fatalf("a notification was changed or lost: %v", n)
		}
	}
}
func TestReview228CancelledQueuedCallMustNotBeSent(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	// Another active write holds this mutex while this request queues.
	c.writeMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- c.Call(ctx, MethodIndexUpdate, map[string]any{"id": 7, "archived": true}, nil)
	}()
	deadline := time.Now().Add(time.Second)
	for {
		c.mu.Lock()
		queued := len(c.pending) > 0
		c.mu.Unlock()
		if queued {
			break
		}
		if time.Now().After(deadline) {
			c.writeMu.Unlock()
			cancel()
			t.Fatal("request never reached the writer")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	c.writeMu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("call error=%v", err)
	}
	select {
	case sent := <-f.frames:
		t.Fatalf("a request cancelled before its write still reached the Kernel: method=%s params=%s", sent.Method, sent.Params)
	case <-time.After(100 * time.Millisecond):
	}
}
