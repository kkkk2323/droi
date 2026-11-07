package sessionview

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
)

// The documents this package reads are the Kernel's own, not this test's idea of them. A fixture of a shape the
// Kernel never sends passes and reads nothing in the window, so the inbox is checked against a Kernel that
// produced it: an item is queued by a submit while a run is in progress, which is the only way one exists.

// TestARealKernelAnswersTheInboxOfAQueuedMessage: pi.inbox is {"items":[{id, mode, content}]}, and the id is the
// submission conversation.submit answered — which is how the composer pairs a queued message with
// submission.abort. An inbox read as {"queued":[...]} would compile, find nothing, and never say so.
func TestARealKernelAnswersTheInboxOfAQueuedMessage(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Chunks: []string{"the ", "first ", "answer ", "takes ", "a ", "while"}, Delay: 120 * time.Millisecond},
		{Text: "the second answer"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	st := NewStore(3)
	if err := st.Open(ctx, c, id, fakekernel.DefaultWindow); err != nil {
		t.Fatalf("conversation.open: %v\n%s", err, k.Logs())
	}
	pumpNotifications(st, c)

	await(t, "the first view", func() bool { return st.State(id).Opened() })
	if items := st.State(id).InboxItems(); len(items) != 0 {
		t.Fatalf("a conversation nobody wrote to queues %+v", items)
	}

	// A follow-up while the run is in progress is queued rather than sent: the inbox is exactly what is not yet
	// in the transcript.
	first := k.Submit(t, c, id, "hello")
	await(t, "the run to start", func() bool { return st.State(id).Busy() })
	var run struct {
		TaskID kernel.ID   `json:"taskId"`
		Inputs []kernel.ID `json:"inputs"`
		Turns  *int        `json:"turns"`
	}
	if err := json.Unmarshal(st.State(id).Run(), &run); err != nil || run.TaskID == 0 || len(run.Inputs) == 0 || run.Turns == nil {
		t.Fatalf("the real Kernel's run is not {taskId, inputs, turns}: %+v, %v", run, err)
	}
	queued, err := c.Submit(ctx, kernel.SubmitParams{ID: id, Input: "queued one", WhenBusy: kernel.WhenBusyFollowUp})
	if err != nil {
		t.Fatalf("conversation.submit followUp: %v\n%s", err, k.Logs())
	}

	var items []InboxItem
	await(t, "the queued input", func() bool {
		items = st.State(id).InboxItems()
		return len(items) > 0
	})
	if len(items) != 1 {
		t.Fatalf("the inbox holds %d items, want the one queued: %+v", len(items), items)
	}
	if items[0].ID != int64(queued.SubmissionID) {
		t.Errorf("the item's id is %d, want the submission %d conversation.submit answered", items[0].ID, queued.SubmissionID)
	}
	if items[0].Mode != kernel.WhenBusyFollowUp {
		t.Errorf("the item's mode is %q, want %q", items[0].Mode, kernel.WhenBusyFollowUp)
	}
	if string(items[0].Content) != `"queued one"` {
		t.Errorf("the item's content is %s, want what was submitted", items[0].Content)
	}

	// Once both turns are done the list is empty again: an item leaves it when a boundary places it.
	k.WaitForReply(t, c, first, "the first answer", 60*time.Second)
	await(t, "the inbox to empty", func() bool {
		return !st.State(id).Busy() && len(st.State(id).InboxItems()) == 0
	})
}

// pumpNotifications hands the connection's pushes to the store the way the window's pump does, on the generation
// the store is on.
func pumpNotifications(st *Store, c *kernel.Client) {
	gen := st.Generation()
	go func() {
		for n := range c.Notifications() {
			switch n.Method {
			case kernel.NotifyConversationView:
				var view kernel.ViewNotification
				if n.Decode(&view) == nil {
					st.ApplyView(gen, view)
				}
			case kernel.NotifyConversationBusy:
				var busy kernel.BusyNotification
				if n.Decode(&busy) == nil {
					st.ApplyBusy(gen, busy)
				}
			}
		}
	}()
}

// await waits for what a Kernel produces, and fails with what it says when the wait runs out.
func await(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("waited a minute for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
