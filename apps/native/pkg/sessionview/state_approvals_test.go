package sessionview

import (
	"errors"
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

func approvalIDs(s *State) []string {
	var ids []string
	for _, p := range s.Approvals() {
		ids = append(ids, p.RequestID)
	}
	return ids
}

func pendingApproval(id string) kernel.PendingRequest {
	return kernel.PendingRequest{Kind: kernel.PendingApproval, RequestID: id, ID: 7, Tool: "bash"}
}

// The cards a conversation shows are the requests heard of and the ones conversation.open listed, once each, less
// the ones heard answered: the two notifications come on a queue of their own, so the list can land after them.
func TestApprovalsMergeTheOpenListWithWhatWasHeard(t *testing.T) {
	s := NewState(7)
	s.ApplyApproval(kernel.ApprovalRequested{RequestID: "b", ID: 7, Tool: "bash"})
	s.ApplyResolved("a")
	if !s.takePending([]kernel.PendingRequest{pendingApproval("a"), pendingApproval("b"), {Kind: kernel.PendingAsk, RequestID: "q", ID: 7}}) {
		// "a" was answered before the list came; "b" is already shown; the ask is 3B's.
		t.Log("nothing new, as expected")
	}
	if got := approvalIDs(s); len(got) != 1 || got[0] != "b" {
		t.Fatalf("approvals = %v, want [b]", got)
	}
	s.ApplyApproval(kernel.ApprovalRequested{RequestID: "c", ID: 7, Tool: "write"})
	s.ApplyApproval(kernel.ApprovalRequested{RequestID: "c", ID: 7, Tool: "write"})
	if got := approvalIDs(s); len(got) != 2 || got[1] != "c" {
		t.Fatalf("approvals = %v, want [b c]", got)
	}
	if !s.ApplyResolved("b") || s.ApplyResolved("b") {
		t.Fatal("an answer takes its card once")
	}
	if got := approvalIDs(s); len(got) != 1 || got[0] != "c" {
		t.Fatalf("approvals = %v, want [c]", got)
	}
}

// A new connection's open says which requests still wait: the ones of the connection before went with it.
func TestReopeningDropsTheApprovalsOfTheConnectionBefore(t *testing.T) {
	s := NewState(7)
	s.ApplyApproval(kernel.ApprovalRequested{RequestID: "old", ID: 7})
	s.ApplyResolved("answered-before")
	s.expectOpen()
	// Until the open answers, the card stays: the Kernel may well still be waiting on it.
	if got := approvalIDs(s); len(got) != 1 || got[0] != "old" {
		t.Fatalf("approvals during the reopen = %v, want [old]", got)
	}
	// One asked on the new connection while the open was under way.
	s.ApplyApproval(kernel.ApprovalRequested{RequestID: "during", ID: 7})
	s.takePending([]kernel.PendingRequest{pendingApproval("answered-before"), pendingApproval("new")})
	if got := approvalIDs(s); len(got) != 3 || got[0] != "during" || got[1] != "answered-before" || got[2] != "new" {
		t.Fatalf("approvals = %v, want [during answered-before new]", got)
	}
}

// A reopen the Kernel refuses keeps the cards, and a later open still says which wait.
func TestAFailedReopenKeepsTheCards(t *testing.T) {
	s := NewState(7)
	s.ApplyApproval(kernel.ApprovalRequested{RequestID: "old", ID: 7})
	s.expectOpen()
	s.failOpen(errors.New("no"))
	s.takePending(nil)
	if got := approvalIDs(s); len(got) != 1 || got[0] != "old" {
		t.Fatalf("approvals = %v, want [old]", got)
	}
}

// Questions live beside approvals: each list holds its own kind, and an answer takes either.
func TestQuestionsAndApprovalsAreKeptApart(t *testing.T) {
	s := NewState(7)
	s.ApplyApproval(kernel.ApprovalRequested{RequestID: "a", ID: 7})
	s.ApplyAsk(kernel.AskRequested{RequestID: "q", ID: 7, Blocking: true})
	s.takePending([]kernel.PendingRequest{{Kind: kernel.PendingAsk, RequestID: "later", ID: 7}})
	if got := approvalIDs(s); len(got) != 1 || got[0] != "a" {
		t.Fatalf("approvals = %v", got)
	}
	asks := s.Asks()
	if len(asks) != 2 || asks[0].RequestID != "q" || len(BlockingAsks(asks)) != 1 || len(QueuedAsks(asks)) != 1 {
		t.Fatalf("asks = %+v", asks)
	}
	s.ApplyResolved("q")
	if len(s.Asks()) != 1 {
		t.Fatalf("asks after the answer = %+v", s.Asks())
	}
}
