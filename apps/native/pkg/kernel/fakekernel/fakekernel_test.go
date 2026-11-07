package fakekernel_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
)

// TestFakeKernelAnswersAScriptedTurn is the package's own proof: a real Kernel, a real conversation, a
// scripted reply, and the view push a client draws from. Every view test that migrates to the Kernel will do
// these four things.
func TestFakeKernelAnswersAScriptedTurn(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Match: "hello there", Chunks: []string{"hi ", "there"}, Text: "hi there"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)

	k.Submit(t, c, id, "hello there")

	reply := k.WaitForReply(t, c, id, "hi there", 30*time.Second)
	if strings.TrimSpace(reply) != "hi there" {
		t.Fatalf("reply = %q, want %q", reply, "hi there")
	}

	// The push a viewer draws from: a whole view of that conversation, holding the reply.
	push := k.WaitForView(t, c, func(push fakekernel.ViewPush) bool {
		if push.ID != id || !push.Whole || push.View == nil {
			return false
		}
		for _, entry := range push.View.Entries {
			if entry.Kind == "pi.assistant" {
				return true
			}
		}
		return false
	}, 30*time.Second)
	if push.View.Conversation.ID != id {
		t.Fatalf("view is for conversation %d, want %d", push.View.Conversation.ID, id)
	}

	// The model saw the prompt the Kernel built, which is what a test of prompt assembly reads.
	requests := k.Requests()
	if len(requests) == 0 {
		t.Fatal("the Kernel made no model request")
	}
	if prompt := requests[0].Prompt(); !strings.Contains(prompt, "hello there") {
		t.Fatalf("the first request's prompt = %q, want it to hold the message", prompt)
	}
	if k.Unmatched() != 0 {
		t.Fatalf("%d requests matched no step", k.Unmatched())
	}
}

// TestFakeKernelRunsAScriptedToolCall checks the other half of scripting: the model asks for a tool, the
// Kernel really runs it, and the result lands in the transcript. A view test of tool output needs this to be
// real rather than a fixture.
func TestFakeKernelRunsAScriptedToolCall(t *testing.T) {
	dir := t.TempDir()
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Match: "list files", Call: &fakekernel.Call{Name: "bash", Arguments: map[string]any{
			"command": "echo written-by-the-kernel",
		}}},
		// The follow-up request's last user message is still "list files": a tool result is a "tool"
		// message, not a user one, so the step that follows a tool call matches the same prompt.
		{Match: "list files", Text: "the command printed it"},
	}})
	c := k.Dial(t)
	id := k.CreateConversationIn(t, c, dir)

	k.Submit(t, c, id, "list files")
	k.WaitForReply(t, c, id, "the command printed it", 60*time.Second)

	// The tool's own output is in the transcript, which is what a tool card renders.
	deadline := time.Now().Add(10 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		entries, err := c.Entries(ctx, kernel.EntriesParams{ID: id, Limit: 200})
		cancel()
		if err == nil {
			for _, entry := range entries.Items {
				// pi.tool-result is the Entry kind a tool card renders, and its model holds the tool's own
				// output as content blocks.
				if entry.Kind == "pi.tool-result" &&
					strings.Contains(string(entry.Model), "written-by-the-kernel") {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the tool result never reached the transcript\n%s", k.Logs())
		}
		time.Sleep(50 * time.Millisecond)
	}
}
