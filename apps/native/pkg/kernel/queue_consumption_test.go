package kernel

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResponsiveConsumerReleasesStorage(t *testing.T) {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	q := newNotifQueue()
	padding := strings.Repeat("x", 1024)
	for i := 0; i < 100000; i++ {
		q.put(whole(7, fmt.Sprintf("%d%s", i, padding)))
		if _, ok := q.take(); !ok {
			t.Fatal("missing whole view")
		}
	}
	retained := 0
	for _, s := range q.entries {
		retained += len(s.note.Params)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	t.Logf("live=%d entries=%d head=%d dead=%d retainedPayload=%d heapGrowth=%d", q.live(), len(q.entries), q.head, q.dead, retained, int64(after.HeapAlloc)-int64(before.HeapAlloc))
	if len(q.entries) > 2048 || retained > 4<<20 {
		t.Fatal("an empty, continuously consumed queue retains unbounded history")
	}
}

func TestConsumedDeltasDoNotExhaustCapacity(t *testing.T) {
	q := newNotifQueue()
	for i := 0; i <= queueMaxViewSteps; i++ {
		n := note(NotifyConversationView, map[string]any{"id": 7, "kind": "value", "value": nil, "ops": []any{}})
		if q.put(n) {
			t.Fatalf("overflow at delta %d with live=%d, viewSteps=%d (every earlier delta consumed)", i+1, q.live(), len(q.viewSteps.items))
		}
		if _, ok := q.take(); !ok {
			t.Fatal("missing delta")
		}
	}
}

func TestResponsiveWebSocketConsumer(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	for i := 0; i <= queueMaxViewSteps; i++ {
		f.notify(t, NotifyConversationView, json.RawMessage(`{"id":7,"kind":"value","value":null,"ops":[]}`))
		select {
		case _, ok := <-c.Notifications():
			if !ok {
				t.Fatalf("closed at delta %d despite draining every notification: %v", i+1, c.Err())
			}
		case <-time.After(5 * time.Second):
			t.Fatal("notification not delivered")
		}
	}
}

func TestDropsStayWithTheirKind(t *testing.T) {
	q := newNotifQueue()
	for i := 0; i <= queueMaxWarnings; i++ {
		q.put(note(NotifyWarning, map[string]any{"message": i}))
	}
	q.put(note(NotifyApprovalRequested, map[string]any{"id": 7, "requestId": "a"}))
	first, _ := q.take()
	next, _ := q.take()
	t.Logf("first=%s dropped=%d; next=%s dropped=%d", first.Method, first.Dropped, next.Method, next.Dropped)
	if first.Dropped != 0 || next.Dropped != 1 {
		t.Fatal("warning loss was charged to the unrelated priority approval")
	}
}

func TestBusyIdleFinalState(t *testing.T) {
	q := newNotifQueue()
	q.put(note(NotifyConversationBusy, map[string]any{"id": 7, "run": map[string]any{"id": 1}}))
	q.put(whole(7, "final"))
	q.put(note(NotifyConversationBusy, map[string]any{"id": 7, "run": nil}))
	for _, n := range drain(q) {
		if n.Method == NotifyConversationBusy {
			var p BusyNotification
			_ = n.Decode(&p)
			if p.Busy() {
				t.Fatal("final idle was lost")
			}
		}
	}
}

func TestAlternatingPutTakeReleasesPayloadAndBucketCapacity(t *testing.T) {
	for _, n := range []Notification{
		whole(7, "payload"),
		note(NotifyConversationView, map[string]any{"id": 7, "kind": "value", "ops": []any{}}),
		note(NotifyWarning, map[string]any{"message": "payload"}),
		note("future.method", map[string]any{"message": "payload"}),
		note(NotifyApprovalRequested, map[string]any{"id": 7, "requestId": "a"}),
	} {
		t.Run(n.Method+string(n.Params), func(t *testing.T) {
			q := newNotifQueue()
			for i := 0; i < 10_000; i++ {
				if q.put(n) {
					t.Fatalf("overflow on notification %d with no backlog", i)
				}
				got, ok := q.take()
				if !ok || string(got.Params) != string(n.Params) {
					t.Fatal("notification payload was not delivered intact")
				}
				for _, s := range q.entries {
					if !s.live && (s.note.Params != nil || s.note.Method != "" || s.key != "") {
						t.Fatal("consumed slot still owns its payload or key")
					}
				}
				if len(q.entries) > 1024 || q.dead < 0 {
					t.Fatalf("consumed slots not reclaimed: entries=%d head=%d dead=%d", len(q.entries), q.head, q.dead)
				}
				if q.live() != 0 || len(q.views.at) != 0 || len(q.views.order) != 0 ||
					len(q.viewSteps.items) != 0 || len(q.warnings.items) != 0 ||
					len(q.unknown.items) != 0 || len(q.approvals.items) != 0 {
					t.Fatal("delivery did not return bucket capacity")
				}
			}
		})
	}
}

func TestPriorityDeliveriesReleaseSlotsBeforeTheSequentialReader(t *testing.T) {
	q := newNotifQueue()
	q.put(whole(7, "still queued"))
	for i := 0; i < 10_000; i++ {
		q.put(note(NotifyApprovalRequested, map[string]any{"id": 7, "requestId": i}))
		n, ok := q.take()
		if !ok || n.Method != NotifyApprovalRequested {
			t.Fatal("approval did not take priority over the queued view")
		}
		if q.dead < 0 || len(q.entries) > 1025 {
			t.Fatalf("priority slots not reclaimed: entries=%d dead=%d", len(q.entries), q.dead)
		}
		for _, s := range q.entries {
			if !s.live && s.note.Params != nil {
				t.Fatal("priority delivery retained its payload")
			}
		}
	}
	n, ok := q.take()
	if !ok || !strings.Contains(string(n.Params), "still queued") {
		t.Fatal("compaction lost the older live view")
	}
	if _, ok := q.take(); ok || q.dead < 0 {
		t.Fatalf("dead-slot accounting after draining: live=%v dead=%d", ok, q.dead)
	}
}

func TestResolutionsRemoveOnlyTheMatchingQueuedRequests(t *testing.T) {
	for _, pair := range [][2]string{
		{NotifyApprovalRequested, NotifyApprovalResolved},
		{NotifyAskRequested, NotifyAskResolved},
	} {
		t.Run(pair[1], func(t *testing.T) {
			q := newNotifQueue()
			other := NotifyAskRequested
			if pair[0] == other {
				other = NotifyApprovalRequested
			}
			q.put(note(other, map[string]any{"id": 7, "requestId": "same"}))
			for i := 1; i < queueMaxRequests; i++ {
				q.put(note(pair[0], map[string]any{"id": 7, "requestId": "same"}))
			}
			q.put(note(pair[1], map[string]any{"id": 7, "requestId": "same"}))
			if got := len(q.approvals.items); got != 1 {
				t.Fatalf("resolved requests still occupy capacity: got %d, want 1", got)
			}
			if q.put(note(pair[0], map[string]any{"id": 7, "requestId": "new"})) {
				t.Fatal("resolved requests caused overflow")
			}
			got := drain(q)
			if len(got) != 3 || got[0].Method != other || fieldKey(got[1], "requestId") != "new" || got[2].Method != pair[1] {
				t.Fatalf("unrelated request or resolution lost: %+v", got)
			}
		})
	}
}

func TestUnknownAndWarningDropCountsAreIndependent(t *testing.T) {
	q := newNotifQueue()
	for i := 0; i < queueMaxUnknown+2; i++ {
		q.put(note("future.method", map[string]any{"i": i}))
	}
	for i := 0; i < queueMaxWarnings+3; i++ {
		q.put(note(NotifyWarning, map[string]any{"i": i}))
	}
	got := drain(q)
	if got[0].Dropped != 2 || got[queueMaxUnknown].Dropped != 3 {
		t.Fatalf("unknown drops=%d, warning drops=%d; want 2 and 3", got[0].Dropped, got[queueMaxUnknown].Dropped)
	}
	for i, n := range got {
		if i != 0 && i != queueMaxUnknown && n.Dropped != 0 {
			t.Fatalf("drop count repeated on notification %d", i)
		}
	}
}

func TestNonAdjacentFoldingDoesNotAlsoCountAsDropped(t *testing.T) {
	q := newNotifQueue()
	q.put(whole(7, "old"))
	q.put(note(NotifyWarning, map[string]any{"message": "between states"}))
	q.put(whole(7, "new"))
	got := drain(q)
	if len(got) != 2 || got[1].Folded != 1 || got[0].Dropped != 0 || got[1].Dropped != 0 {
		t.Fatalf("folded state was also counted as dropped: %+v", got)
	}
}
