package fakekernel_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
)

// nextApproval is the next approval.requested on c, skipping every other push.
func nextApproval(t *testing.T, c *kernel.Client, within time.Duration) kernel.ApprovalRequested {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("no approval.requested in %s", within)
		case n, ok := <-c.Notifications():
			if !ok {
				t.Fatal("the connection ended before an approval.requested")
			}
			if n.Method != kernel.NotifyApprovalRequested {
				continue
			}
			var req kernel.ApprovalRequested
			if err := n.Decode(&req); err != nil {
				t.Fatalf("approval.requested: %v", err)
			}
			return req
		}
	}
}

// noApproval fails when an approval.requested is among the pushes c has had.
func noApproval(t *testing.T, c *kernel.Client) {
	t.Helper()
	for {
		select {
		case n := <-c.Notifications():
			if n.Method == kernel.NotifyApprovalRequested {
				t.Fatalf("an approval was asked for that the policy should have allowed: %s", n.Params)
			}
		default:
			return
		}
	}
}

// TestFakeKernelHoldsACallForApproval is the approval round trip at the "ask" level, as the Host drives it: a
// call waits for an answer, "always allow this file" covers the same file written again, and a cancel keeps the
// file from being written and tells the model so.
func TestFakeKernelHoldsACallForApproval(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Autonomy: "ask", Steps: []fakekernel.Step{
		{Match: "write it", Call: &fakekernel.Call{Name: "write", Arguments: map[string]any{"path": "notes.md", "content": "one"}}},
		{Match: "write it", Call: &fakekernel.Call{Name: "write", Arguments: map[string]any{"path": "./notes.md", "content": "two"}}},
		{Match: "write it", Text: "written twice"},
		{Match: "try another", Call: &fakekernel.Call{Name: "write", Arguments: map[string]any{"path": "denied.txt", "content": "x"}}},
		{Match: "try another", Text: "understood"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	workspace, err := filepath.EvalSymlinks(k.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(workspace, "notes.md")

	sub := k.Submit(t, c, id, "write it")
	req := nextApproval(t, c, 30*time.Second)
	if req.ID != id || req.Tool != "write" || req.ScopeHint != "ask" {
		t.Fatalf("approval = %+v, want a write of conversation %d at ask", req, id)
	}
	if _, err := os.Stat(notes); err == nil {
		t.Fatal("the file was written before anyone answered")
	}
	var always *kernel.ApprovalOption
	for i, option := range req.Options {
		if option.ID == "proceed_always_file" {
			always = &req.Options[i]
		}
	}
	if always == nil || always.Scope == nil || always.Scope.Path != notes {
		t.Fatalf("options = %+v, want one that always allows %s", req.Options, notes)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := c.RespondApproval(ctx, kernel.ApprovalRespondParams{RequestID: req.RequestID, Outcome: always.ID}); err != nil {
		t.Fatalf("approval.respond: %v", err)
	}
	k.WaitForReply(t, c, sub, "written twice", 60*time.Second)
	noApproval(t, c)
	if got, _ := os.ReadFile(notes); string(got) != "two" {
		t.Fatalf("notes.md = %q, want the second write", got)
	}
	// Answered once: a second answer finds nothing to answer.
	if _, err := c.RespondApproval(ctx, kernel.ApprovalRespondParams{RequestID: req.RequestID, Outcome: "cancel"}); !kernel.Is(err, kernel.CodeRequestGone) {
		t.Fatalf("a second answer = %v, want %d", err, kernel.CodeRequestGone)
	}

	sub = k.Submit(t, c, id, "try another")
	req = nextApproval(t, c, 30*time.Second)
	if _, err := c.RespondApproval(ctx, kernel.ApprovalRespondParams{RequestID: req.RequestID, Outcome: "cancel"}); err != nil {
		t.Fatalf("approval.respond: %v", err)
	}
	entries := k.WaitForEntries(t, c, sub, func(entries []fakekernel.Entry) bool {
		for _, entry := range entries {
			if entry.Kind == "pi.tool-result" && strings.Contains(entry.Text, "denied") {
				return true
			}
		}
		return false
	}, 60*time.Second)
	if len(entries) == 0 {
		t.Fatal("no tool result")
	}
	k.WaitForReply(t, c, sub, "understood", 30*time.Second)
	if _, err := os.Stat(filepath.Join(workspace, "denied.txt")); err == nil {
		t.Fatal("a cancelled write wrote the file")
	}
}
