package alerts

import "testing"

func expect(t *testing.T, tracker *Tracker, id, state string, want Event) {
	t.Helper()
	if got := tracker.Update(id, state); got != want {
		t.Errorf("Update(%q, %q) = %q, want %q", id, state, got, want)
	}
}

func TestATurnThatEndsIsOneCompletion(t *testing.T) {
	tr := NewTracker()
	expect(t, tr, "a", "idle", "")
	expect(t, tr, "a", "thinking", "")
	expect(t, tr, "a", "streaming_assistant_message", "")
	expect(t, tr, "a", "idle", Completion)
	expect(t, tr, "a", "idle", "")
}

func TestWaitingAlertsOncePerWait(t *testing.T) {
	tr := NewTracker()
	expect(t, tr, "a", "executing_tool", "")
	expect(t, tr, "a", "waiting_for_tool_confirmation", AwaitingInput)
	expect(t, tr, "a", "waiting_for_tool_confirmation", "")
	expect(t, tr, "a", "executing_tool", "")
	expect(t, tr, "a", "idle", Completion)
	// A new turn that waits again is a new wait.
	expect(t, tr, "a", "waiting_for_tool_confirmation", AwaitingInput)
}

func TestSessionsAreTrackedApart(t *testing.T) {
	tr := NewTracker()
	tr.Update("a", "thinking")
	tr.Update("b", "thinking")
	tr.Forget("a")
	expect(t, tr, "a", "idle", "")
	expect(t, tr, "b", "idle", Completion)
}
