package controller

import (
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

func toolUse(id string) []protocol.ToolConfirmationInfo {
	return []protocol.ToolConfirmationInfo{{ToolUse: protocol.ToolUse{ID: id, Name: "Execute"}}}
}

var options = []protocol.ToolConfirmationListItem{{Label: "Yes", Value: protocol.ToolConfirmationOutcomeProceedOnce}, {Label: "No", Value: protocol.ToolConfirmationOutcomeCancel}}

func noParents(s string, extra []string) []string { return append([]string{s}, extra...) }

func TestRestoreReplaysAnswerKeptWhileInactive(t *testing.T) {
	p := newPrompts()
	p.addPermission(&Permission{RequestID: "r1", SessionID: "s", AssociatedSessionIDs: []string{"s"}, ToolUses: toolUse("tu1"), Options: options}, nil)
	p.addAskUser(&AskUser{RequestID: "a1", SessionID: "s", ToolCallID: "tc1"}, nil)
	p.markPromptSessionsInactive()
	if !p.inactive["s"] {
		t.Fatal("session with prompts not inactive")
	}
	// Answered while the worker was gone: kept, prompt resolved locally.
	p.bufPerm["tu1"] = bufferedPermission{sessionID: "s", answer: PermissionAnswer{SelectedOption: protocol.ToolConfirmationOutcomeProceedOnce}}
	p.dropPermission("r1", protocol.ToolConfirmationOutcomeProceedOnce, nil)

	// The restored worker asks again under new request ids.
	res := &protocol.LoadSessionResult{
		PendingPermissions: []protocol.LoadSessionResultPendingPermissionsItem{
			{RequestID: "r2", ToolUses: toolUse("tu1"), Options: options},
			{RequestID: "r3", ToolUses: toolUse("tu2"), Options: options},
		},
		PendingAskUserRequests: []protocol.LoadSessionResultPendingAskUserRequestsItem{{RequestID: "a1", ToolCallID: "tc1"}},
	}
	out, replies := p.restore("s", res, noParents, nil)
	if len(replies) != 1 || replies[0].id != "r2" {
		t.Fatalf("replies: %+v", replies)
	}
	if r := replies[0].result.(protocol.RequestPermissionResponse); r.SelectedOption != protocol.ToolConfirmationOutcomeProceedOnce || r.SessionID != "s" {
		t.Fatalf("replayed %+v", r)
	}
	if _, ok := p.bufPerm["tu1"]; ok {
		t.Fatal("answer kept after replay")
	}
	if _, ok := p.perms["r3"]; !ok || !p.perms["r3"].Restored {
		t.Fatal("unanswered permission not restored")
	}
	if _, ok := p.asks["a1"]; !ok {
		t.Fatal("pending AskUser of the same request id dropped")
	}
	if p.inactive["s"] {
		t.Fatal("session still inactive after load")
	}
	var requested []string
	for _, e := range out {
		if r, ok := e.(PermissionRequested); ok {
			requested = append(requested, r.Permission.RequestID)
		}
		if _, ok := e.(AskUserRequested); ok {
			t.Fatal("AskUser pending under the same id requested again")
		}
	}
	if len(requested) != 1 || requested[0] != "r3" {
		t.Fatalf("requested %v", requested)
	}
}

func TestRestoreDropsPromptsTheDaemonNoLongerHas(t *testing.T) {
	p := newPrompts()
	p.addPermission(&Permission{RequestID: "r1", SessionID: "s", AssociatedSessionIDs: []string{"s"}, ToolUses: toolUse("tu1"), Options: options}, nil)
	// A child's permission relayed to its parent is not the parent's load's to drop.
	p.addPermission(&Permission{RequestID: "r9", SessionID: "s", AssociatedSessionIDs: []string{"s", "parent"}, ToolUses: toolUse("tu9"), Options: options}, nil)
	out, _ := p.restore("s", &protocol.LoadSessionResult{}, noParents, nil)
	if _, ok := p.perms["r1"]; ok {
		t.Fatal("stale permission kept")
	}
	if _, ok := p.perms["r9"]; !ok {
		t.Fatal("relayed permission dropped")
	}
	if len(out) != 1 {
		t.Fatalf("events %+v", out)
	}
	if r, ok := out[0].(PermissionResolved); !ok || r.RequestID != "r1" || r.SelectedOption != "" {
		t.Fatalf("event %+v", out[0])
	}
}

func TestClearStaleKeepsConcurrentPrompts(t *testing.T) {
	p := newPrompts()
	p.addPermission(&Permission{RequestID: "r1", SessionID: "s", AssociatedSessionIDs: []string{"s"}, ToolUses: toolUse("tu1"), Options: options}, nil)
	p.addPermission(&Permission{RequestID: "r2", SessionID: "s", AssociatedSessionIDs: []string{"s"}, ToolUses: toolUse("tu2"), Options: options}, nil)
	p.dropPermission("r1", protocol.ToolConfirmationOutcomeProceedOnce, nil)
	// The agent resumes after r1; r2 is still asked.
	p.clearStale("s", nil)
	if _, ok := p.perms["r2"]; !ok {
		t.Fatal("sibling of an answered concurrent permission dropped")
	}
	p.dropPermission("r2", protocol.ToolConfirmationOutcomeProceedOnce, nil)
	if p.concurrent["s"] {
		t.Fatal("concurrent mark kept after the batch drained")
	}

	p.addPermission(&Permission{RequestID: "r3", SessionID: "s", AssociatedSessionIDs: []string{"s"}, ToolUses: toolUse("tu3"), Options: options}, nil)
	out := p.clearStale("s", nil)
	if _, ok := p.perms["r3"]; ok || len(out) != 1 {
		t.Fatal("single stale permission kept")
	}
}

func TestRepeatedToolUseReplacesPermission(t *testing.T) {
	p := newPrompts()
	p.addPermission(&Permission{RequestID: "r1", SessionID: "s", AssociatedSessionIDs: []string{"s"}, ToolUses: toolUse("tu1"), Options: options}, nil)
	out := p.addPermission(&Permission{RequestID: "r2", SessionID: "s", AssociatedSessionIDs: []string{"s"}, ToolUses: toolUse("tu1"), Options: options}, nil)
	if _, ok := p.perms["r1"]; ok || len(p.perms) != 1 {
		t.Fatal("old request for the same tool use kept")
	}
	if len(out) != 2 {
		t.Fatalf("events %+v", out)
	}
	if again := p.addPermission(&Permission{RequestID: "r2", SessionID: "s", AssociatedSessionIDs: []string{"parent"}, ToolUses: toolUse("tu1"), Options: options}, nil); len(again) != 0 {
		t.Fatal("repeat of a pending request announced again")
	}
	if got := p.perms["r2"].AssociatedSessionIDs; len(got) != 2 {
		t.Fatalf("associated sessions not merged: %v", got)
	}
}
