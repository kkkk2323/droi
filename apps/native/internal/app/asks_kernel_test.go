package app

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
)

// askHarness is a window on Kernel whose model, asked "Help me", asks question (blocking or not), then answers
// first and, after the person's answer arrives as a message of its own, second.
func askHarness(t *testing.T, blocking bool, first, second string) *harness {
	t.Helper()
	question := map[string]any{
		"context":  "Asked while writing the test.",
		"blocking": blocking,
		"questions": []any{map[string]any{
			"question": "What should the test do offline?",
			"options":  []any{map[string]any{"id": "skip", "label": "Skip when offline"}, map[string]any{"id": "fail", "label": "Fail"}},
		}},
	}
	scriptKernel(t, func(sc *fakekernel.Scenario) {
		steps := []fakekernel.Step{
			{Match: "Help me", Call: &fakekernel.Call{Name: "ask_user", ID: "call_ask", Arguments: question}},
			{Match: "Help me", Text: first},
		}
		if second != "" {
			steps = append(steps, fakekernel.Step{Match: "Skip when offline", Text: second})
		}
		sc.Steps = append(steps, sc.Steps...)
	})
	h := kernelHarness(t)
	h.openSession("Add dark mode toggle")
	return h
}

// A blocking question takes the composer's place, marks the Session as waiting, and the option picked is the
// answer the model reads.
func TestAKernelQuestionIsAnsweredOnItsCard(t *testing.T) {
	h := askHarness(t, true, "Skipping it is.", "")
	id := h.d.Sessions[1].SessionID
	h.send("Help me")
	h.until("the question", func() bool { return h.finds("Droi has a question") })
	if h.finds("Message") {
		t.Error("the composer is still there while the run waits on the question")
	}
	h.a.mu.Lock()
	state := h.a.busy[id]
	h.a.mu.Unlock()
	if state != string(protocol.DroidWorkingStateWaitingForToolConfirmation) {
		t.Errorf("the list holds the Session as %q while it waits", state)
	}
	h.click("Skip when offline")
	h.click("Answer")
	h.until("the model's reply", func() bool { return h.hasText("Skipping it is.") })
	h.until("the card to go", func() bool { return !h.finds("Droi has a question") })
	var told bool
	for _, r := range h.kernel.k.Requests() {
		for _, m := range r.Messages {
			if m.Role == "tool" && strings.Contains(string(m.Content), "A: Skip when offline") {
				told = true
			}
		}
	}
	if !told {
		t.Error("the model was not given the answer")
	}
}

// A question that does not block sits above the composer while the agent works on; its answer is the next
// message of the conversation.
func TestAQueuedKernelQuestionsAnswerIsTheNextMessage(t *testing.T) {
	h := askHarness(t, false, "Carrying on.", "Thanks, skipping when offline.")
	h.send("Help me")
	h.until("the queued question and the reply", func() bool {
		return h.finds("Droi has a queued question") && h.hasText("Carrying on.")
	})
	if !h.finds("Message") {
		t.Error("a question that does not block took the composer's place")
	}
	h.click("Skip when offline")
	h.click("Answer")
	h.until("the reply to the answer", func() bool { return h.hasText("Thanks, skipping when offline.") })
	// The answer quotes the question, so the card is told by its own label.
	h.until("the card to go", func() bool { return !h.finds("Droi has a queued question") })
}

// A queued question stays on screen while a later command of the same run waits on its approval card: both
// wait for the person.
func TestAQueuedKernelQuestionStaysBesideAnApprovalCard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the command is bash's")
	}
	marker := filepath.Join(t.TempDir(), "ran")
	question := map[string]any{
		"context":  "Asked while writing the test.",
		"blocking": false,
		"questions": []any{map[string]any{
			"question": "What should the test do offline?",
			"options":  []any{map[string]any{"id": "skip", "label": "Skip when offline"}},
		}},
	}
	scriptKernel(t, func(sc *fakekernel.Scenario) {
		sc.Steps = append([]fakekernel.Step{
			{Match: "Help me", Call: &fakekernel.Call{Name: "ask_user", ID: "call_ask", Arguments: question}},
			{Match: "Help me", Call: &fakekernel.Call{Name: "bash", ID: "call_run", Arguments: map[string]any{"command": "touch " + marker}}},
			{Match: "Help me", Text: "Done."},
		}, sc.Steps...)
	})
	h := kernelHarness(t)
	conv, _ := h.kernel.id(h.d.Sessions[1].SessionID)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := h.testConn().Configure(ctx, kernel.ConfigureParams{ID: conv, Change: &kernel.AgentChange{Cwd: kernel.Some(t.TempDir())}}); err != nil {
		t.Fatal(err)
	}
	h.openSession("Add dark mode toggle")
	h.send("Help me")
	h.until("the approval card", func() bool { return h.hasText("Droi wants to run bash") })
	if !h.finds("Droi has a queued question") {
		t.Error("the queued question went while the approval card waits")
	}
	h.click("Allow once")
	h.until("the model's answer", func() bool { return h.hasText("Done.") })
	if !h.finds("Droi has a queued question") {
		t.Error("the queued question went with the approval")
	}
}
