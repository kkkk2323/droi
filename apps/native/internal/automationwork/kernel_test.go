package automationwork

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/internal/automations"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
)

// fakeRuns is a Kernel that records what a start asked of it.
type fakeRuns struct {
	mu        sync.Mutex
	calls     []string
	created   kernel.CreateParams
	title     string
	policy    kernel.PolicySetParams
	submitted kernel.SubmitParams
	createErr error
	submitErr error
	status    kernel.SubmissionGet
	archived  []kernel.ID
	policies  []string
}

func (f *fakeRuns) note(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }

func (f *fakeRuns) Harness(context.Context) (kernel.HarnessInfo, error) {
	f.note("harness.info")
	return kernel.HarnessInfo{Models: []kernel.ModelInfo{{Provider: "factory", ID: "flash", Thinking: []string{"low", "high"}}}}, nil
}

func (f *fakeRuns) Create(_ context.Context, p kernel.CreateParams) (kernel.CreateResult, error) {
	f.note("conversation.create")
	f.created = p
	return kernel.CreateResult{ConversationID: 42}, f.createErr
}

func (f *fakeRuns) UpdateIndex(_ context.Context, p kernel.IndexUpdateParams) error {
	f.note("index.update")
	if p.Archived != nil && *p.Archived {
		f.mu.Lock()
		f.archived = append(f.archived, p.ID)
		f.mu.Unlock()
	}
	if len(p.Title) > 0 {
		_ = json.Unmarshal(p.Title, &f.title)
	}
	return nil
}

func (f *fakeRuns) PolicySet(_ context.Context, p kernel.PolicySetParams) (kernel.PolicyResult, error) {
	f.note("policy.set")
	f.mu.Lock()
	f.policy = p
	f.policies = append(f.policies, p.ID.String()+"="+p.Policy.Autonomy)
	f.mu.Unlock()
	return kernel.PolicyResult{}, nil
}

func (f *fakeRuns) Submit(_ context.Context, p kernel.SubmitParams) (kernel.SubmitResult, error) {
	f.note("conversation.submit")
	f.submitted = p
	return kernel.SubmitResult{SubmissionID: 7}, f.submitErr
}

func (f *fakeRuns) SubmissionGet(context.Context, kernel.AbortSubmissionParams) (kernel.SubmissionGet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}

func TestAKernelRunIsAConversationOfItsOwnThatRunsAtItsOwnLevel(t *testing.T) {
	f := &fakeRuns{status: kernel.SubmissionGet{Status: "placed"}}
	var kept []string
	ended := make(chan string, 1)
	st := &KernelStarter{
		Client: func() (KernelRuns, error) { return f, nil },
		KeepPolicy: func(id kernel.ID, level string) error {
			f.note("keep")
			kept = append(kept, id.String()+"="+level)
			return nil
		},
		Every: 10 * time.Millisecond,
	}
	st.finished = func(runID string, status automations.RunStatus, text string) {
		ended <- runID + " " + string(status) + " " + text
	}
	defer st.Close()
	id, err := st.Start(context.Background(), RunSpec{
		RunID: "r1", Title: "[Automation] Nightly lint", Cwd: "/w", ModelID: "flash", ReasoningEffort: "high",
		SkipPermissions: true, Prompt: "lint everything",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "42" {
		t.Fatalf("the run's Session is %q", id)
	}
	if got := strings.Join(f.calls, ","); got != "harness.info,conversation.create,index.update,keep,policy.set,conversation.submit" {
		t.Fatalf("calls: %s", got)
	}
	c := f.created
	if c.Kind != "automation" || c.Cwd != "/w" || c.RequestID != "automation-run:r1" || c.Agent == nil ||
		c.Agent.Model.Value == nil || *c.Agent.Model.Value != (kernel.ModelRef{Provider: "factory", ModelID: "flash"}) || *c.Agent.ThinkingLevel.Value != "high" {
		t.Fatalf("create: %+v", c)
	}
	if f.title != "[Automation] Nightly lint" || f.policy.ID == nil || *f.policy.ID != 42 || f.policy.Policy.Autonomy != "all" || kept[0] != "42=all" {
		t.Fatalf("title %q, policy %+v, kept %v", f.title, f.policy, kept)
	}
	if f.submitted.Input != "lint everything" || f.submitted.RequestID != "automation-run:r1" || f.submitted.WhenBusy != kernel.WhenBusyFollowUp {
		t.Fatalf("submit: %+v", f.submitted)
	}
	select {
	case got := <-ended:
		t.Fatalf("the run ended while its turn was placed: %s", got)
	case <-time.After(50 * time.Millisecond):
	}
	f.mu.Lock()
	f.status = kernel.SubmissionGet{Status: "unanswered", Reason: "model_error", Detail: "the quota is used up"}
	f.mu.Unlock()
	select {
	case got := <-ended:
		if got != "r1 failed the quota is used up" {
			t.Fatalf("ended %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run's end never came")
	}
}

func TestAKernelStartThatFailsSaysWhetherToTryAgain(t *testing.T) {
	ctx := context.Background()
	none := &KernelStarter{Client: func() (KernelRuns, error) { return nil, errors.New("no connection") }}
	if _, err := none.Start(ctx, RunSpec{RunID: "r"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("no Kernel: %v", err)
	}
	refused := &fakeRuns{createErr: &kernel.RPCError{Code: kernel.CodeInvalidParams, Message: "cwd"}}
	st := &KernelStarter{Client: func() (KernelRuns, error) { return refused, nil }}
	if _, err := st.Start(ctx, RunSpec{RunID: "r"}); err == nil || errors.Is(err, ErrUnavailable) {
		t.Fatalf("a refusal: %v", err)
	}
	half := &fakeRuns{submitErr: errors.New("connection closed")}
	st = &KernelStarter{Client: func() (KernelRuns, error) { return half, nil }}
	var hs *HalfStartedError
	if _, err := st.Start(ctx, RunSpec{RunID: "r"}); !errors.As(err, &hs) {
		t.Fatalf("a submit that failed after the conversation was made: %v", err)
	}
}

func TestASubmissionsEndIsTheRunsEnd(t *testing.T) {
	for _, tc := range []struct {
		in     kernel.SubmissionGet
		status automations.RunStatus
		text   string
		ended  bool
	}{
		{kernel.SubmissionGet{Status: "queued"}, "", "", false},
		{kernel.SubmissionGet{Status: "placed"}, "", "", false},
		{kernel.SubmissionGet{Status: "done"}, automations.RunSucceeded, "", true},
		{kernel.SubmissionGet{Status: "unanswered", Reason: "aborted"}, automations.RunInterrupted, "The run ended without an answer (aborted).", true},
		{kernel.SubmissionGet{Status: "unanswered", Reason: "interrupted", Detail: "Droi quit"}, automations.RunInterrupted, "Droi quit", true},
		{kernel.SubmissionGet{Status: "unanswered", Reason: "no_model"}, automations.RunFailed, "The run ended without an answer (no_model).", true},
	} {
		status, text, ended := SubmissionOutcome(tc.in)
		if status != tc.status || text != tc.text || ended != tc.ended {
			t.Errorf("%+v: %q %q %v", tc.in, status, text, ended)
		}
	}
}

// An Automation run on the real Kernel, started by the Scheduler as the Host starts it: a conversation of kind
// automation, named for the Automation, whose model gets the prompt, and whose run is recorded as succeeded.
func TestAnAutomationRunsOnTheKernel(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{{Match: "Say PONG", Text: "PONG"}}})
	c := k.Dial(t)
	var kept []string
	var mu sync.Mutex
	st := &KernelStarter{
		Client: func() (KernelRuns, error) { return c, nil },
		KeepPolicy: func(id kernel.ID, level string) error {
			mu.Lock()
			kept = append(kept, level)
			mu.Unlock()
			return nil
		},
		Every: 50 * time.Millisecond,
	}
	defer st.Close()
	s := New(Config{Dir: t.TempDir(), Starter: st, Hold: func() func() { return func() {} }})
	st.Attach(s)
	a, err := s.Save(automations.Automation{Name: "Ping", Prompt: "Say PONG", Schedule: "0 9 * * *", Model: fakekernel.ModelID, WorkingDirectory: k.Workspace})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
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
	if last.Status != automations.RunSucceeded || last.RunID != run.RunID {
		t.Fatalf("the run ended %+v\n%s", last, k.Logs())
	}
	list, err := c.List(ctx, kernel.ListParams{Kind: "automation"})
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("automation conversations: %+v (%v)", list.Items, err)
	}
	conv := list.Items[0]
	if conv.Title != "[Automation] Ping" || conv.ID.String() != last.SessionID || conv.Workspace != k.Workspace {
		t.Fatalf("the run's conversation: %+v, run %+v", conv, last)
	}
	mu.Lock()
	defer mu.Unlock()
	// Kept while the run worked, forgotten once it ended.
	if strings.Join(kept, ",") != "all," {
		t.Fatalf("the level kept for the run: %v", kept)
	}
}

func TestAnAutomationsLevelOnTheKernel(t *testing.T) {
	for level, want := range map[string]string{"": "all", "high": "all", "medium": "safe", "low": "edits", "off": "ask", "meduim": "ask"} {
		if got := KernelAutonomy(level); got != want {
			t.Errorf("%q is %q, want %q", level, got, want)
		}
	}
}

// A start that fails once the conversation is made archives it and forgets its level: the next try makes another.
func TestAHalfStartedRunLeavesNoConversationListed(t *testing.T) {
	f := &fakeRuns{submitErr: errors.New("connection closed")}
	var kept []string
	st := &KernelStarter{
		Client:     func() (KernelRuns, error) { return f, nil },
		KeepPolicy: func(id kernel.ID, level string) error { kept = append(kept, id.String()+"="+level); return nil },
	}
	var hs *HalfStartedError
	if _, err := st.Start(context.Background(), RunSpec{RunID: "r", SkipPermissions: true}); !errors.As(err, &hs) {
		t.Fatalf("start: %v", err)
	}
	if len(f.archived) != 1 || f.archived[0] != 42 {
		t.Fatalf("archived %v", f.archived)
	}
	if strings.Join(kept, ",") != "42=all,42=" {
		t.Fatalf("kept %v", kept)
	}
}

// A Kernel that starts again during a run gets the run's level again on the new connection, and the level is
// forgotten once the run ends.
func TestARunsLevelGoesToANewConnection(t *testing.T) {
	first := &fakeRuns{status: kernel.SubmissionGet{Status: "placed"}}
	second := &fakeRuns{status: kernel.SubmissionGet{Status: "placed"}}
	var mu sync.Mutex
	current := KernelRuns(first)
	var kept []string
	ended := make(chan struct{}, 1)
	st := &KernelStarter{
		Client: func() (KernelRuns, error) { mu.Lock(); defer mu.Unlock(); return current, nil },
		KeepPolicy: func(id kernel.ID, level string) error {
			mu.Lock()
			kept = append(kept, id.String()+"="+level)
			mu.Unlock()
			return nil
		},
		Every: 10 * time.Millisecond,
	}
	st.finished = func(string, automations.RunStatus, string) { ended <- struct{}{} }
	defer st.Close()
	if _, err := st.Start(context.Background(), RunSpec{RunID: "r", AutonomyLevel: "medium", Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	current = second
	mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for {
		second.mu.Lock()
		got := strings.Join(second.policies, ",")
		second.mu.Unlock()
		if got == "42=safe" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the new connection's policies: %q", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	second.mu.Lock()
	second.status = kernel.SubmissionGet{Status: "done"}
	second.mu.Unlock()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the run's end never came")
	}
	st.Close()
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(kept, ",") != "42=safe,42=" {
		t.Fatalf("kept %v", kept)
	}
	if n := strings.Count(strings.Join(first.policies, ","), "42="); n != 1 {
		t.Fatalf("the first connection's policies: %v", first.policies)
	}
}
