package automationwork

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/automations"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
)

// A loop is an Automation that posts into a conversation (decision Q6): its run is a submission there, at the
// conversation's own model and level.

func TestALoopsRunPostsIntoItsConversation(t *testing.T) {
	f := &fakeRuns{status: kernel.SubmissionGet{Status: "placed"}}
	st := &KernelStarter{Client: func() (KernelRuns, error) { return f, nil }, Every: time.Hour}
	defer st.Close()
	id, err := st.Start(context.Background(), RunSpec{RunID: "r1", Conversation: "9", Prompt: "check the build", ModelID: "flash", SkipPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	if id != "9" {
		t.Fatalf("the run's Session is %q", id)
	}
	if got := strings.Join(f.calls, ","); got != "conversation.submit" {
		t.Fatalf("calls: %s", got)
	}
	if s := f.submitted; s.ID != 9 || s.Input != "check the build" || s.RequestID != "automation-run:r1" || s.WhenBusy != kernel.WhenBusyFollowUp {
		t.Fatalf("submit: %+v", s)
	}
}

func TestALoopWhoseConversationIsGoneIsRefused(t *testing.T) {
	gone := &fakeRuns{submitErr: &kernel.RPCError{Code: kernel.CodeConversationGone, Message: "gone"}}
	st := &KernelStarter{Client: func() (KernelRuns, error) { return gone, nil }}
	if _, err := st.Start(context.Background(), RunSpec{RunID: "r", Conversation: "9"}); !errors.Is(err, ErrTargetGone) || errors.Is(err, ErrUnavailable) {
		t.Fatalf("a gone conversation: %v", err)
	}
	if _, err := st.Start(context.Background(), RunSpec{RunID: "r", Conversation: "not-a-number"}); !errors.Is(err, ErrTargetGone) {
		t.Fatalf("no conversation: %v", err)
	}
	lost := &fakeRuns{submitErr: errors.New("connection closed")}
	st = &KernelStarter{Client: func() (KernelRuns, error) { return lost, nil }}
	if _, err := st.Start(context.Background(), RunSpec{RunID: "r", Conversation: "9"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a lost connection: %v", err)
	}
}

func TestTheDaemonLeavesALoopForTheKernel(t *testing.T) {
	if _, err := (&SessionStarter{}).Start(context.Background(), RunSpec{RunID: "r", Conversation: "9"}); !errors.Is(err, ErrLoopNeedsKernel) || errors.Is(err, ErrUnavailable) {
		t.Fatalf("a loop on the Daemon: %v", err)
	}
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Loop", Schedule: "*/10 * * * *", Conversation: "9"})
	h.starter.fail = []error{ErrLoopNeedsKernel}
	h.tick("08:10")
	if runs := h.runs(a.ID); len(runs) != 1 || runs[0].Status != automations.RunFailed || !h.item(a.ID).Paused {
		t.Fatalf("records %+v, paused %v", runs, h.item(a.ID).Paused)
	}
}

// A loop whose conversation is gone fails once and pauses, rather than fail at every interval.
func TestALoopWhoseConversationIsGonePauses(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Loop", Schedule: "*/10 * * * *", Conversation: "9"})
	h.starter.fail = []error{ErrTargetGone}
	h.tick("08:10")
	if runs := h.runs(a.ID); len(runs) != 1 || runs[0].Status != automations.RunFailed {
		t.Fatalf("records %+v", runs)
	}
	if !h.item(a.ID).Paused {
		t.Fatal("the loop was not paused")
	}
	if got := h.starter.last().Conversation; got != "9" {
		t.Fatalf("the run's conversation %q", got)
	}
	h.tick("08:20")
	if h.starter.count() != 1 {
		t.Fatalf("tried %d times", h.starter.count())
	}
}

// A loop on the real Kernel: its run is a turn of the conversation it names, and no conversation is made for it.
func TestALoopRunsInItsConversationOnTheKernel(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{{Match: "Say PONG", Text: "PONG"}}})
	c := k.Dial(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	agent := &kernel.AgentChange{Model: kernel.Some(kernel.ModelRef{Provider: fakekernel.ProviderID, ModelID: fakekernel.ModelID})}
	conv, err := c.Create(ctx, kernel.CreateParams{Kind: "chat", Cwd: k.Workspace, Agent: agent})
	if err != nil {
		t.Fatal(err)
	}
	st := &KernelStarter{Client: func() (KernelRuns, error) { return c, nil }, Every: 50 * time.Millisecond}
	defer st.Close()
	s := New(Config{Dir: t.TempDir(), Starter: st, Hold: func() func() { return func() {} }})
	st.Attach(s)
	a, err := s.Save(automations.Automation{Name: "Loop: Say PONG", Prompt: "Say PONG", Schedule: "*/10 * * * *", Timezone: "UTC", Conversation: conv.ConversationID.String()})
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.RunNow(ctx, a.ID)
	if err != nil {
		t.Fatalf("run now: %v\n%s", err, k.Logs())
	}
	var last automations.Run
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		runs, err := s.Runs(a.ID, 1)
		if err == nil && len(runs) == 1 && runs[0].Status != automations.RunRunning {
			last = runs[0]
			break
		}
	}
	if last.Status != automations.RunSucceeded || last.RunID != run.RunID || last.SessionID != conv.ConversationID.String() {
		t.Fatalf("the run ended %+v\n%s", last, k.Logs())
	}
	list, err := c.List(ctx, kernel.ListParams{Kind: "automation"})
	if err != nil || len(list.Items) != 0 {
		t.Fatalf("automation conversations: %+v (%v)", list.Items, err)
	}
	asked := false
	for _, r := range k.Requests() {
		asked = asked || r.Prompt() == "Say PONG"
	}
	if !asked {
		t.Fatalf("the model was not asked\n%s", k.Logs())
	}
	if _, err := st.Start(ctx, RunSpec{RunID: "r-gone", Conversation: "999999", Prompt: "Say PONG"}); !errors.Is(err, ErrTargetGone) {
		t.Fatalf("a conversation the Kernel does not have: %v", err)
	}
}
