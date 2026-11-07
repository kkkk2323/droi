package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func review228Note(method, params string) Notification {
	return Notification{Method: method, Params: json.RawMessage(params)}
}

func TestReview228WholeViewMustNotCrossAFullPush(t *testing.T) {
	c := &Client{wake: make(chan struct{}, 1)}
	c.enqueue(review228Note(NotifyConversationView, `{"id":7,"kind":"value","value":{"conversation":{"id":7},"entries":[{"id":1,"conversationId":7,"kind":"pi.system"}],"docs":{}},"ops":[],"retired":false}`))
	c.enqueue(review228Note(NotifyConversationView, `{"id":7,"kind":"full","value":{"conversation":{"id":7},"entries":[{"id":2,"conversationId":7,"kind":"pi.system"}],"docs":{}},"ops":[],"retired":false}`))
	c.enqueue(review228Note(NotifyConversationView, `{"id":7,"kind":"value","value":{"conversation":{"id":7},"entries":[{"id":3,"conversationId":7,"kind":"pi.system"}],"docs":{}},"ops":[],"retired":false}`))
	last := c.queued[len(c.queued)-1]
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
		t.Fatalf("the last applied view is %s, not latest; queue=%v", last.Params, c.queued)
	}
}

func TestReview228MustArriveFloodKeepsEveryStep(t *testing.T) {
	c := &Client{opts: Options{MaxQueuedNotes: 2}, wake: make(chan struct{}, 1)}
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
	if len(c.queued) != len(notes) {
		t.Fatalf("kept %d of %d must-arrive notifications: %v", len(c.queued), len(notes), c.queued)
	}
	for i := range notes {
		if c.queued[i].Method != notes[i].Method || string(c.queued[i].Params) != string(notes[i].Params) {
			t.Fatalf("notification %d changed: %v", i, c.queued)
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
