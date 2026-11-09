package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestAStalledConsumerCostsABoundedQueue is the review's 100,000-push probe as a
// test: a real connection, a consumer that never reads Notifications, and an RPC
// answer used as an ordered fence after the pushes — so the pushes are certainly
// in by the time the queue is measured.
//
// The numbers are the point. A reader that stops reading must cost a bounded
// queue and a bounded amount of memory, and the pushes it cannot apply must end
// the connection rather than pile up: a delta reader that lost one step can
// never build the right state from the rest, and reopening starts it from a
// whole view.
func TestAStalledConsumerCostsABoundedQueue(t *testing.T) {
	// One kilobyte of payload per push, which is what a view push costs.
	padding := strings.Repeat("x", 1024)
	fenceCall := func(t *testing.T, c *Client, f *fakeKernel) {
		t.Helper()
		answered := make(chan error, 1)
		go func() { answered <- c.Call(context.Background(), "review.fence", nil, nil) }()
		req := f.next(t)
		f.reply(t, req.ID, map[string]bool{"ok": true})
		if err := <-answered; err != nil {
			t.Fatalf("the fence: %v", err)
		}
	}

	for _, tc := range []struct {
		kind string
		push func(f *fakeKernel, t *testing.T, i int)
		// wantAtMost is how many notifications may still be waiting.
		wantAtMost int
		// wantClosed says whether the queue is allowed to end the connection.
		wantClosed bool
	}{
		{
			// One conversation's state: the newest supersedes the rest.
			kind: "whole",
			push: func(f *fakeKernel, t *testing.T, i int) {
				f.notify(t, NotifyConversationView, map[string]any{
					"id": 7, "kind": ViewKindValue,
					"value": map[string]string{"text": fmt.Sprintf("%06d%s", i, padding)},
				})
			},
			wantAtMost: 1,
		},
		{
			// Advice: the newest are kept and the rest counted.
			kind: "warning",
			push: func(f *fakeKernel, t *testing.T, i int) {
				f.notify(t, NotifyWarning, map[string]any{
					"kind": "probe", "message": fmt.Sprintf("%06d%s", i, padding),
				})
			},
			wantAtMost: queueMaxWarnings,
		},
		{
			// A delta reader's steps: every one has to be applied, so a queue
			// that cannot hold them ends the connection instead.
			kind: "delta",
			push: func(f *fakeKernel, t *testing.T, i int) {
				f.notify(t, NotifyConversationView, json.RawMessage(fmt.Sprintf(
					`{"id":7,"kind":"value","value":null,"ops":[{"op":"replace","path":"/text","value":"%06d%s"}]}`, i, padding)))
			},
			wantAtMost: queueMaxViewSteps,
			wantClosed: true,
		},
		{
			// A reset and a state, alternating: the resets are steps too.
			kind: "reset-whole",
			push: func(f *fakeKernel, t *testing.T, i int) {
				kind := ViewKindValue
				if i%2 == 0 {
					kind = ViewKindFull
				}
				f.notify(t, NotifyConversationView, map[string]any{
					"id": 7, "kind": kind,
					"value": map[string]string{"text": fmt.Sprintf("%06d%s", i, padding)},
				})
			},
			wantAtMost: queueMaxViewSteps + 1,
			wantClosed: true,
		},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			f := startFakeKernel(t)
			c := dialFake(t, f)
			// The connection is established by a call, so the pushes below are
			// certainly addressed to it.
			fenceCall(t, c, f)

			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			started := time.Now()
			pushed := 0
			for i := 0; i < 100_000; i++ {
				if tc.wantClosed && c.Err() != nil {
					// The queue ended the connection rather than drop a step a
					// reader cannot rebuild. Nothing else can be pushed, and
					// that is the outcome this case expects.
					break
				}
				tc.push(f, t, i)
				pushed++
			}
			// The fence is the last frame the Client reads: everything the
			// pushes carried is in the queue by the time its answer is handled.
			if c.Err() == nil {
				fenceCall(t, c, f)
			}
			runtime.GC()
			runtime.ReadMemStats(&after)

			queued := c.queue.live()
			var retained int
			c.queue.mu.Lock()
			for i := c.queue.head; i < len(c.queue.entries); i++ {
				retained += len(c.queue.entries[i].note.Params)
			}
			c.queue.mu.Unlock()
			growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
			t.Logf("%d %s pushes keep %d notifications (%d bytes of payload); heap growth %d bytes in %s",
				pushed, tc.kind, queued, retained, growth, time.Since(started).Round(time.Millisecond))

			if queued > tc.wantAtMost {
				t.Fatalf("%d notifications are waiting, want at most %d", queued, tc.wantAtMost)
			}
			// 4 MiB of heap for a queue of at most a few thousand kilobyte
			// payloads, and the Go runtime's own slack.
			if growth > 16<<20 {
				t.Fatalf("the queue cost %d bytes of heap for %d notifications", growth, queued)
			}
			closed := c.Err() != nil
			if closed != tc.wantClosed {
				t.Fatalf("connection closed = %v, want %v (err: %v)", closed, tc.wantClosed, c.Err())
			}
			runtime.KeepAlive(c)
		})
	}
}
