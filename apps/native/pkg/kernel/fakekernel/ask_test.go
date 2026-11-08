package fakekernel_test

import (
	"context"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
)

// nextAsk is the next ask.requested on c, skipping every other push.
func nextAsk(t *testing.T, c *kernel.Client, within time.Duration) kernel.AskRequested {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("no ask.requested in %s", within)
		case n, ok := <-c.Notifications():
			if !ok {
				t.Fatal("the connection ended before an ask.requested")
			}
			if n.Method != kernel.NotifyAskRequested {
				continue
			}
			var req kernel.AskRequested
			if err := n.Decode(&req); err != nil {
				t.Fatalf("ask.requested: %v", err)
			}
			return req
		}
	}
}

// TestFakeKernelAsksAQuestion: the model asks, the test answers, and the model's next turn reads the answer.
func TestFakeKernelAsksAQuestion(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Match: "which way", Call: &fakekernel.Call{Name: "ask_user", Arguments: map[string]any{
			"context":   "Choosing the way the test goes.",
			"questions": []any{map[string]any{"question": "Left or right?", "options": []any{map[string]any{"id": "l", "label": "Left"}, map[string]any{"id": "r", "label": "Right"}}}},
		}}},
		{Match: "which way", Text: "going left"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	sub := k.Submit(t, c, id, "which way")
	req := nextAsk(t, c, 30*time.Second)
	if req.ID != id || !req.Blocking || len(req.Questions) != 1 || req.Questions[0].QuestionID != "q1" {
		t.Fatalf("ask = %+v", req)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := c.RespondAsk(ctx, kernel.AskRespondParams{RequestID: req.RequestID, Answers: []kernel.AskAnswer{{QuestionID: "q1", Selected: []string{"l"}}}}); err != nil {
		t.Fatalf("ask.respond: %v", err)
	}
	k.WaitForReply(t, c, sub, "going left", 60*time.Second)
	if _, err := c.RespondAsk(ctx, kernel.AskRespondParams{RequestID: req.RequestID, Answers: []kernel.AskAnswer{{QuestionID: "q1", Selected: []string{"r"}}}}); !kernel.Is(err, kernel.CodeRequestGone) {
		t.Fatalf("a second answer = %v, want %d", err, kernel.CodeRequestGone)
	}
}
