package transcript_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

// The two shapes the real Kernel produces, drawn through the real SDK and the real Kernel.
//
// The synthetic views above pin the mapping; these pin that the mapping meets what the Kernel actually writes. A
// `submit{input:"hello"}` stores a user message whose content is a bare string, and a provider that refuses the
// request stores an answer with no content and only an `errorMessage` — neither of which a hand-written fixture
// would have guessed, and both of which the window would have shown as nothing.

// TestARealKernelSubmitDrawsTheUsersRow: the Host submits a string, and the window shows the line the user typed.
func TestARealKernelSubmitDrawsTheUsersRow(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{{Text: "world"}}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	sub := k.Submit(t, c, id, "hello")
	k.WaitForReply(t, c, sub, "world", 30*time.Second)

	view := openView(t, c, id)
	entries := transcript.BuildView(view)
	if len(entries) != 2 {
		t.Fatalf("%d rows, want the question and the answer: %s", len(entries), describe(entries))
	}
	if !entries[0].User || entries[0].Blocks[0].Text != "hello" {
		t.Fatalf("the user's row reads %+v", entries[0])
	}
	if entries[1].User || !strings.Contains(entries[1].Blocks[0].Text, "world") {
		t.Fatalf("the answer reads %+v", entries[1])
	}
}

// TestARealKernelProviderFailureDrawsItsError: the model refuses, the Kernel stores an empty failed answer, and the
// window says what the provider said instead of leaving the question alone on the page.
func TestARealKernelProviderFailureDrawsItsError(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{{
		Failure: &fakekernel.Failure{Status: 400, Body: `{"error":{"message":"probe invalid request","type":"invalid_request_error"}}`},
	}}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sub, err := c.Submit(ctx, kernel.SubmitParams{ID: id, Input: []kernel.ContentBlock{kernel.TextBlock("hello")}})
	if err != nil {
		t.Fatalf("conversation.submit: %v", err)
	}
	// The turn ends unanswered: a refused request produces no answer entry to wait for.
	deadline := time.Now().Add(20 * time.Second)
	for {
		record, err := c.SubmissionGet(ctx, kernel.AbortSubmissionParams{ID: id, SubmissionID: sub.SubmissionID})
		if err != nil {
			t.Fatalf("submission.get: %v", err)
		}
		if record.Status == "unanswered" {
			if record.Reason == "" {
				t.Error("the submission ended without a reason")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the submission never ended: %+v", record)
		}
		time.Sleep(10 * time.Millisecond)
	}

	entries := transcript.BuildView(openView(t, c, id))
	if len(entries) != 2 {
		t.Fatalf("%d rows, want the question and the failed answer: %s", len(entries), describe(entries))
	}
	failed := entries[1]
	if !failed.IsError {
		t.Errorf("the failed answer is not marked: %+v", failed)
	}
	if len(failed.Blocks) == 0 {
		t.Fatal("the failed answer drew nothing")
	}
	if !strings.Contains(failed.Blocks[0].Text, "probe invalid request") {
		t.Errorf("the answer's sentence is %q, want the provider's own words", failed.Blocks[0].Text)
	}
}

// TestARealKernelFinishedToolStopsSayingRunning: a tool's result is its own entry, and the cluster header must
// read it. This is the end-to-end half of TestTheClusterOfFinishedCallsIsNotRunning.
func TestARealKernelFinishedToolStopsSayingRunning(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Call: &fakekernel.Call{Name: "read", Arguments: map[string]any{"path": "note.txt"}}},
		{Text: "read it"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	// The file the model asks for, in the Kernel's own workspace.
	if err := os.WriteFile(filepath.Join(k.Workspace, "note.txt"), []byte("the note\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sub := k.Submit(t, c, id, "read note.txt")
	k.WaitForReply(t, c, sub, "read it", 60*time.Second)

	entries := transcript.BuildView(openView(t, c, id))
	var calls []*transcript.ToolCall
	for _, e := range entries {
		for _, b := range e.Blocks {
			calls = append(calls, b.Calls...)
		}
	}
	if len(calls) == 0 {
		t.Fatalf("no tool call in %s", describe(entries))
	}
	shown := transcript.ReadResult(calls[0])
	if shown.Pending {
		t.Fatal("the result reads as pending although the turn finished")
	}
	label, pending := transcript.ClusterLabel(calls)
	if pending {
		t.Fatalf("the finished tool's cluster still says %q", label)
	}
}

// openView is the window a client draws: conversation.open's whole view.
func openView(t testing.TB, c *kernel.Client, id kernel.ID) kernel.View {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	opened, err := c.Open(ctx, kernel.OpenParams{ID: id})
	if err != nil {
		t.Fatalf("conversation.open: %v", err)
	}
	return opened.View
}

// describe is the rows as a failure message can read them.
func describe(entries []*transcript.Entry) string {
	var parts []string
	for _, e := range entries {
		kinds := make([]string, len(e.Blocks))
		for i, b := range e.Blocks {
			kinds[i] = blockKind(b.Kind) + ":" + b.Text
		}
		parts = append(parts, e.ID+"(user="+boolWord(e.User)+")["+strings.Join(kinds, ", ")+"]")
	}
	return strings.Join(parts, " ")
}

func boolWord(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
