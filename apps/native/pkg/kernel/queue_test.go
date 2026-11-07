package kernel

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// note is a notification with params built from v.
func note(method string, v any) Notification {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return Notification{Method: method, Params: raw}
}

// wholeView7 is a whole view push of conversation id.
func whole(id int, marker string) Notification {
	return note(NotifyConversationView, map[string]any{
		"id": id, "kind": ViewKindValue,
		"value": map[string]any{"before": marker},
	})
}

// drain takes everything the queue holds, in the order a reader would get it.
func drain(q *notifQueue) []Notification {
	var out []Notification
	for {
		n, ok := q.take()
		if !ok {
			return out
		}
		out = append(out, n)
	}
}

// held is how many notifications the queue would hand a reader now.
func held(q *notifQueue) int { return q.live() }

// TestTheQueueHoldsOneStatePerConversation is the bound that matters most: a
// reader that stops reading costs one state per conversation, whatever the
// Kernel sent in between.
func TestTheQueueHoldsOneStatePerConversation(t *testing.T) {
	q := newNotifQueue()
	for i := 0; i < 100_000; i++ {
		q.put(whole(7, fmt.Sprintf("state-%d", i)))
	}
	if got := held(q); got != 1 {
		t.Fatalf("100,000 pushes of one conversation left %d in the queue, want 1", got)
	}
	last := drain(q)
	if len(last) != 1 || !strings.Contains(string(last[0].Params), "state-99999") {
		t.Fatalf("the state kept is not the newest: %s", last[0].Params)
	}
	if last[0].Folded != 99_999 {
		t.Fatalf("Folded = %d, want 99,999", last[0].Folded)
	}
}

// TestTheQueueHoldsOneStatePerTenConversations is the same with ten of them: the
// bound is the number of conversations, not the number of pushes.
func TestTheQueueHoldsOneStatePerTenConversations(t *testing.T) {
	q := newNotifQueue()
	for i := 1; i <= 10; i++ {
		for j := 0; j < 10_000; j++ {
			q.put(whole(i, fmt.Sprintf("state-%d", j)))
		}
	}
	if got := held(q); got != 10 {
		t.Fatalf("10 conversations x 10,000 pushes left %d in the queue, want 10", got)
	}
	got := drain(q)
	if len(got) != 10 {
		t.Fatalf("10 conversations gave %d notifications", len(got))
	}
	for _, n := range got {
		if !strings.Contains(string(n.Params), "state-9999") {
			t.Fatalf("a conversation kept an older state: %s", n.Params)
		}
		if n.Folded != 9_999 {
			t.Fatalf("Folded = %d, want 9,999", n.Folded)
		}
	}
}

// TestTheQueueKeepsTheNewestWarnings: a warning is advice, so the newest ones
// are kept and the count of the ones lost travels on the next one.
func TestTheQueueKeepsTheNewestWarnings(t *testing.T) {
	q := newNotifQueue()
	for i := 0; i < 100_000; i++ {
		q.put(note(NotifyWarning, map[string]any{"kind": "extension_error", "message": fmt.Sprintf("w-%d", i)}))
	}
	if got := held(q); got != queueMaxWarnings {
		t.Fatalf("the queue holds %d warnings, want %d", got, queueMaxWarnings)
	}
	got := drain(q)
	if len(got) != queueMaxWarnings {
		t.Fatalf("%d warnings came out, want %d", len(got), queueMaxWarnings)
	}
	if !strings.Contains(string(got[0].Params), "w-99744") {
		t.Fatalf("the oldest warning kept is not the 99,744th: %s", got[0].Params)
	}
	if got[0].Dropped != 100_000-queueMaxWarnings {
		t.Fatalf("Dropped = %d, want %d", got[0].Dropped, 100_000-queueMaxWarnings)
	}
	// The count is reported once: the rest were already accounted for.
	for _, n := range got[1:] {
		if n.Dropped != 0 {
			t.Fatalf("a later warning repeats the drop count: %+v", n)
		}
	}
}

// TestAQuestionFloodEndsTheConnection: an approval is a question a session is
// waiting for, so the queue does not drop one — it ends the connection, and the
// Client that reconnects opens its conversations again.
func TestAQuestionFloodEndsTheConnection(t *testing.T) {
	f := startFakeKernel(t)
	c, err := Dial(t.Context(), Options{URL: f.url(), Token: "t-test", NotesBuffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// Nobody reads past the first, so nothing drains the queue.
	for i := 0; i < 2*queueMaxRequests; i++ {
		c.enqueue(note(NotifyApprovalRequested, map[string]any{
			"requestId": fmt.Sprintf("a-%d", i), "id": 7, "tool": "bash",
		}))
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("a queue that cannot hold another approval did not end the connection")
	}
	if err := c.Err(); err == nil || !strings.Contains(err.Error(), "approvals and asks") {
		t.Fatalf("Err() = %v, want the queue's reason", err)
	}
}

// TestTheReaderWakesToTheQuestion: a reader that was stuck takes the approval
// before the conversation state it also missed, whatever arrived first.
func TestTheReaderWakesToTheQuestion(t *testing.T) {
	q := newNotifQueue()
	for i := 0; i < 100; i++ {
		q.put(whole(7, fmt.Sprintf("state-%d", i)))
		q.put(note(NotifyApprovalRequested, map[string]any{"requestId": fmt.Sprintf("a-%d", i), "id": 7, "tool": "bash"}))
	}
	first, ok := q.take()
	if !ok {
		t.Fatal("the queue is empty")
	}
	if first.Method != NotifyApprovalRequested {
		t.Fatalf("the first notification a stuck reader gets is %s, want the approval", first.Method)
	}
	// Every approval is still there, in order, before any state.
	var ids []string
	for {
		n, ok := q.take()
		if !ok {
			break
		}
		if n.Method != NotifyApprovalRequested {
			break
		}
		var p struct {
			RequestID string `json:"requestId"`
		}
		_ = json.Unmarshal(n.Params, &p)
		ids = append(ids, p.RequestID)
	}
	if len(ids)+1 != 100 {
		t.Fatalf("%d of 100 approvals came out before the first state", len(ids)+1)
	}
}

// TestTheQueueStaysUnderAMemoryBound is the point of the whole exercise: a
// reader that stops reading while one conversation's state streams in must cost
// one state, not a hundred thousand.
//
// The bar is the interface request's own: 100,000 pushes of about a kilobyte,
// and under 4 MiB of heap growth.
func TestTheQueueStaysUnderAMemoryBound(t *testing.T) {
	blob := strings.Repeat("x", 1024)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	q := newNotifQueue()
	for i := 0; i < 100_000; i++ {
		q.put(whole(7, blob+fmt.Sprintf("-%d", i)))
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("100,000 pushes of one conversation: queue holds %d; heap growth %d bytes", held(q), growth)
	if held(q) != 1 {
		t.Fatalf("the queue holds %d notifications, want the one state", held(q))
	}
	if growth > 4<<20 {
		t.Fatalf("keeping up costs %d bytes of heap, want under 4 MiB", growth)
	}
}

// TestTheQueueStaysBoundedWhenEveryBucketFloods is the same with every kind at
// once: the bound is the sum of the buckets, and the heap stays within what that
// many kilobyte payloads cost.
func TestTheQueueStaysBoundedWhenEveryBucketFloods(t *testing.T) {
	blob := strings.Repeat("y", 1024)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	q := newNotifQueue()
	for i := 0; i < 20_000; i++ {
		// Conversation ids start at 1: id 0 is not a conversation, and a view
		// push for one is classified as a step, which is a different bucket.
		id := i%256 + 1
		q.put(whole(id, blob))
		q.put(note(NotifyConversationBusy, map[string]any{"id": id, "run": nil}))
		q.put(note(NotifyIndexChanged, map[string]any{"id": id, "title": blob}))
		q.put(note(NotifyTaskChanged, map[string]any{"taskId": fmt.Sprintf("t-%d", i), "text": blob}))
		q.put(note(NotifyWarning, map[string]any{"message": blob}))
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	bound := 3*queueConversations + queueMaxSteps + queueMaxWarnings
	t.Logf("100,000 mixed pushes: queue holds %d (bound %d); heap growth %d bytes", held(q), bound, growth)
	if held(q) > bound {
		t.Fatalf("the queue holds %d notifications, above its bound of %d", held(q), bound)
	}
	if growth > 32<<20 {
		t.Fatalf("the flood costs %d bytes of heap for a queue of %d", growth, bound)
	}
}

// TestTheQueueIsBoundedWhateverTheConsumerDoes is the whole-queue version of the
// per-bucket tests above: mix every kind, flood each, and the total stays at the
// sum of the buckets.
func TestTheQueueIsBoundedWhateverTheConsumerDoes(t *testing.T) {
	q := newNotifQueue()
	for i := 0; i < 20_000; i++ {
		id := i%500 + 1
		q.put(whole(id, "state"))
		q.put(note(NotifyConversationBusy, map[string]any{"id": id, "run": nil}))
		q.put(note(NotifyIndexChanged, map[string]any{"id": id}))
		q.put(note(NotifyTaskChanged, map[string]any{"taskId": fmt.Sprintf("t-%d", i)}))
		q.put(note(NotifyProcessChanged, map[string]any{"id": 7, "processId": fmt.Sprintf("p-%d", i)}))
		q.put(note(NotifyApprovalResolved, map[string]any{"requestId": fmt.Sprintf("r-%d", i)}))
		q.put(note(NotifyWarning, map[string]any{"message": "w"}))
		q.put(note("some.future.method", map[string]any{"x": i}))
	}
	bound := 3*queueConversations + queueMaxRequests + 2*queueMaxSteps + queueMaxViewSteps +
		queueMaxWarnings + queueMaxUnknown + 1
	if got := held(q); got > bound {
		t.Fatalf("the queue holds %d notifications, above its bound of %d", got, bound)
	}
	t.Logf("mixed flood: %d notifications held, bound %d", held(q), bound)
}

// TestAnUnknownMethodIsForwardedAndBounded: a newer Kernel must not be able to
// grow the queue, and must not be silently swallowed either.
func TestAnUnknownMethodIsForwardedAndBounded(t *testing.T) {
	q := newNotifQueue()
	for i := 0; i < 1000; i++ {
		q.put(note("some.future.method", map[string]any{"x": i}))
	}
	got := drain(q)
	if len(got) != queueMaxUnknown {
		t.Fatalf("%d unknown notifications kept, want %d", len(got), queueMaxUnknown)
	}
	if got[0].Dropped != 1000-queueMaxUnknown {
		t.Fatalf("Dropped = %d, want %d", got[0].Dropped, 1000-queueMaxUnknown)
	}
}
