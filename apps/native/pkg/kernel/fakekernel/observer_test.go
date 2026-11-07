package fakekernel_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
)

// missingID is a conversation no Kernel made, so a call that would change state fails with -32000 and
// leaves the Kernel alone: the Observer records what the client asked, not what succeeded.
const missingID = kernel.ID(999999)

// TestObserverRecordsEveryMethod is the seam's contract: every method of the protocol can be observed,
// and the methods the test drives are exactly the ones the contract lists. A method added to pkg/kernel
// without one here fails the count, which is what makes the shell worth writing by hand over reflection.
func TestObserverRecordsEveryMethod(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{})
	c := k.Dial(t)
	o := k.Observe(c)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// The real conversation the no-argument methods need; it is itself observed.
	created, err := o.Create(ctx, kernel.CreateParams{
		Agent: &kernel.AgentChange{Model: kernel.Some(kernel.ModelRef{Provider: fakekernel.ProviderID, ModelID: fakekernel.ModelID})},
		Cwd:   k.Workspace,
		Kind:  "chat",
	})
	if err != nil {
		t.Fatalf("conversation.create: %v\n%s", err, k.Logs())
	}
	id := created.ConversationID

	// Every call below may fail — most of them are pointed at a conversation that does not exist, or at a
	// capability M1 leaves false — and none of that matters: the Observer records the request.
	calls := []struct {
		method string
		call   func() error
	}{
		{kernel.MethodHarnessInfo, func() error { _, err := o.Harness(ctx); return err }},
		{kernel.MethodConversationCreate, func() error {
			_, err := o.Create(ctx, kernel.CreateParams{Cwd: k.Workspace, Kind: "chat"})
			return err
		}},
		{kernel.MethodConversationList, func() error { _, err := o.List(ctx, kernel.ListParams{Limit: 5}); return err }},
		{kernel.MethodConversationOpen, func() error {
			_, err := o.Open(ctx, kernel.OpenParams{ID: id, Window: &kernel.Window{Min: 20, Max: 200}})
			return err
		}},
		{kernel.MethodConversationUnwatch, func() error { return o.Unwatch(ctx, id) }},
		{kernel.MethodConversationSubmit, func() error {
			_, err := o.Submit(ctx, kernel.SubmitParams{ID: missingID, Input: "never placed"})
			return err
		}},
		{kernel.MethodConversationAbort, func() error { return o.Abort(ctx, kernel.AbortParams{ID: missingID}) }},
		{kernel.MethodSubmissionAbort, func() error {
			_, err := o.AbortSubmission(ctx, kernel.AbortSubmissionParams{ID: missingID, SubmissionID: 1})
			return err
		}},
		{kernel.MethodSubmissionGet, func() error {
			_, err := o.SubmissionGet(ctx, kernel.AbortSubmissionParams{ID: missingID, SubmissionID: 1})
			return err
		}},
		{kernel.MethodConversationConfig, func() error {
			_, err := o.Configure(ctx, kernel.ConfigureParams{ID: missingID, Change: &kernel.AgentChange{}})
			return err
		}},
		{kernel.MethodConversationCompact, func() error {
			_, err := o.Compact(ctx, kernel.CompactParams{ID: missingID})
			return err
		}},
		{kernel.MethodConversationFork, func() error {
			_, err := o.Fork(ctx, kernel.ForkParams{ID: missingID, At: 1})
			return err
		}},
		{kernel.MethodConversationEntries, func() error {
			_, err := o.Entries(ctx, kernel.EntriesParams{ID: id, Limit: 5})
			return err
		}},
		{kernel.MethodConversationUsage, func() error { _, err := o.Usage(ctx, id); return err }},
		{kernel.MethodTaskAbort, func() error { _, err := o.AbortTask(ctx, kernel.AbortTaskParams{TaskID: 1}); return err }},
		{kernel.MethodIndexUpdate, func() error { return o.UpdateIndex(ctx, kernel.IndexUpdateParams{ID: id}) }},
		{kernel.MethodProcessList, func() error { _, err := o.Processes(ctx, id); return err }},
		{kernel.MethodProcessStop, func() error {
			_, err := o.StopProcess(ctx, kernel.StopProcessParams{ID: id, ProcessID: "p1"})
			return err
		}},
		{kernel.MethodApprovalRespond, func() error {
			_, err := o.RespondApproval(ctx, kernel.ApprovalRespondParams{RequestID: "r1", Outcome: kernel.ApprovalCancel})
			return err
		}},
		{kernel.MethodAskRespond, func() error {
			_, err := o.RespondAsk(ctx, kernel.AskRespondParams{RequestID: "r2"})
			return err
		}},
		{kernel.MethodMCPList, func() error { _, err := o.MCP(ctx); return err }},
		{kernel.MethodMCPSet, func() error {
			_, err := o.SetMCP(ctx, kernel.MCPSetParams{Name: "srv", Remove: true})
			return err
		}},
		{kernel.MethodMCPAuthorize, func() error {
			_, err := o.AuthorizeMCP(ctx, kernel.MCPAuthorizeParams{Name: "srv"})
			return err
		}},
		{kernel.MethodMCPAuthorizeCancel, func() error {
			return o.CancelMCPAuthorize(ctx, kernel.MCPAuthorizeParams{Name: "srv"})
		}},
		{kernel.MethodSkillsList, func() error { _, err := o.Skills(ctx); return err }},
		{kernel.MethodSkillsSetDisabled, func() error {
			_, err := o.SetSkillDisabled(ctx, kernel.SkillsSetDisabledParams{Name: "s", Disabled: true})
			return err
		}},
		{kernel.MethodModelComplete, func() error {
			_, err := o.ModelComplete(ctx, kernel.ModelCompleteParams{
				Model: kernel.ModelRef{Provider: fakekernel.ProviderID, ModelID: fakekernel.ModelID},
				Input: "hi",
			})
			return err
		}},
		// Last, because it ends the Kernel: an answer is still an answer, and nothing after it would get one.
		{kernel.MethodKernelShutdown, func() error { _, err := o.Shutdown(ctx); return err }},
	}

	for _, c := range calls {
		_ = c.call()
	}

	// The setup's own create is the first call observed, so the list below is offset by one.
	got := o.Calls()
	if len(got) != len(calls)+1 {
		t.Fatalf("%d calls recorded, want %d: %v", len(got), len(calls)+1, methodsOf(got))
	}
	if got[0].Method != kernel.MethodConversationCreate || got[0].Params == nil {
		t.Fatalf("the first call is %q with params %s, want the setup's conversation.create",
			got[0].Method, got[0].Params)
	}
	for i, c := range calls {
		if got[i+1].Method != c.method {
			t.Errorf("call %d recorded as %q, want %q", i, got[i+1].Method, c.method)
		}
	}

	// The seam and the contract agree in both directions: no method of the contract goes unobserved by
	// this test, and the test invents no method the contract does not have.
	seen := map[string]bool{}
	for _, call := range got {
		seen[call.Method] = true
	}
	for _, method := range kernel.Methods {
		if !seen[method] {
			t.Errorf("%s was not observed", method)
		}
	}
	if len(seen) != len(kernel.Methods) {
		t.Errorf("observed %d distinct methods, the contract has %d", len(seen), len(kernel.Methods))
	}
}

// TestObserverRecordsTheParamsTheCallerSent: the recorded params are the caller's own JSON, in the
// contract's field names, so an assertion on them is an assertion on the wire.
func TestObserverRecordsTheParamsTheCallerSent(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{})
	c := k.Dial(t)
	o := k.Observe(c)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	want := kernel.CreateParams{
		Agent: &kernel.AgentChange{
			Model:         kernel.Some(kernel.ModelRef{Provider: fakekernel.ProviderID, ModelID: fakekernel.ModelID}),
			ThinkingLevel: kernel.Some("high"),
			Cwd:           kernel.Some(k.Workspace),
		},
		Cwd:       k.Workspace,
		Kind:      "chat",
		RequestID: "observer-1",
	}
	if _, err := o.Create(ctx, want); err != nil {
		t.Fatalf("conversation.create: %v\n%s", err, k.Logs())
	}

	call := o.WaitForCall(t, kernel.MethodConversationCreate, 1, 5*time.Second)
	var got kernel.CreateParams
	if err := json.Unmarshal(call.Params, &got); err != nil {
		t.Fatalf("the recorded params do not decode as CreateParams: %v: %s", err, call.Params)
	}
	if got.RequestID != want.RequestID || got.Kind != want.Kind || got.Cwd != want.Cwd {
		t.Errorf("recorded %+v, want the request's %+v", got, want)
	}
	if got.Agent == nil || got.Agent.ThinkingLevel == nil || got.Agent.ThinkingLevel.Value == nil ||
		*got.Agent.ThinkingLevel.Value != "high" {
		t.Errorf("the agent's thinking level is not in the recorded params: %s", call.Params)
	}
	var raw map[string]any
	if err := json.Unmarshal(call.Params, &raw); err != nil {
		t.Fatalf("the recorded params are not a JSON object: %v", err)
	}
	if _, ok := raw["requestId"]; !ok {
		t.Errorf("the field is not spelled as the contract spells it: %s", call.Params)
	}
}

// TestObserverCallsOfFiltersByMethod: a test reads only the calls it is about.
func TestObserverCallsOfFiltersByMethod(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{})
	c := k.Dial(t)
	o := k.Observe(c)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for i := 0; i < 3; i++ {
		if _, err := o.List(ctx, kernel.ListParams{Limit: i + 1}); err != nil {
			t.Fatalf("conversation.list: %v\n%s", err, k.Logs())
		}
	}
	if _, err := o.Harness(ctx); err != nil {
		t.Fatalf("harness.info: %v\n%s", err, k.Logs())
	}
	if got := len(o.CallsOf(kernel.MethodConversationList)); got != 3 {
		t.Errorf("%d conversation.list calls, want 3", got)
	}
	if got := o.Count(kernel.MethodHarnessInfo); got != 1 {
		t.Errorf("%d harness.info calls, want 1", got)
	}
	if got := len(o.CallsOf(kernel.MethodConversationFork)); got != 0 {
		t.Errorf("%d conversation.fork calls, want none", got)
	}
}

// TestObserverWaitForCallFailsWithWhatItSaw: a wait that times out says which call never came and what did,
// so a failing assertion does not send the reader back to a print statement.
func TestObserverWaitForCallFailsWithWhatItSaw(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{})
	c := k.Dial(t)
	o := k.Observe(c)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := o.Harness(ctx); err != nil {
		t.Fatalf("harness.info: %v\n%s", err, k.Logs())
	}

	// A subtest, so the expected failure is the subtest's and not this test's.
	t.Run("missing", func(t *testing.T) {
		fake := &recordingTB{TB: t}
		o.WaitForCall(fake, kernel.MethodConversationSubmit, 1, 200*time.Millisecond)
		if !fake.failed {
			t.Fatal("waiting for a call that never came did not fail the test")
		}
		if !strings.Contains(fake.message, kernel.MethodConversationSubmit) {
			t.Errorf("the failure does not name the method: %q", fake.message)
		}
		if !strings.Contains(fake.message, kernel.MethodHarnessInfo) {
			t.Errorf("the failure does not list the calls it saw: %q", fake.message)
		}
	})
}

// TestObserverRecordsConcurrentCalls: the window calls from several goroutines, so the recording is safe
// and complete under them.
func TestObserverRecordsConcurrentCalls(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{})
	c := k.Dial(t)
	o := k.Observe(c)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = o.List(ctx, kernel.ListParams{Limit: 1})
		}()
	}
	wg.Wait()
	if got := o.Count(kernel.MethodConversationList); got != n {
		t.Fatalf("%d calls recorded, want %d", got, n)
	}
}

func methodsOf(calls []fakekernel.RPC) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Method
	}
	return out
}

// recordingTB turns a test failure into a value the test can assert on, for the waits that are meant to fail.
type recordingTB struct {
	testing.TB
	failed  bool
	message string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.failed = true
	r.message = fmt.Sprintf(format, args...)
}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.failed = true
	r.message = fmt.Sprintf(format, args...)
}

func (r *recordingTB) FailNow() {}
func (r *recordingTB) Fail()    {}

// deadlineTB is a test that reports a deadline the wait must respect, over a recorder so the expected
// failure lands in a value this test can assert on rather than failing the real test.
type deadlineTB struct {
	*recordingTB
	at time.Time
}

func (d *deadlineTB) Deadline() (time.Time, bool) { return d.at, true }

// TestObserverWaitForCallRespectsTheTestDeadline: a wait must not outlive the test. A test runner kills a
// test at its own deadline, and a wait that polls past it spends the last second of the run and then cuts
// through the test's cleanups; so the wait takes the nearer of its own within and the test's deadline, minus
// the margin, and reports the missing call itself.
//
// The two halves: a deadline 300ms away with a 30s within ends in about 300ms, and one already inside the
// margin ends at once.
func TestObserverWaitForCallRespectsTheTestDeadline(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{})
	o := k.Observe(k.Dial(t))

	recorded := &recordingTB{TB: t}
	near := &deadlineTB{recordingTB: recorded, at: time.Now().Add(300 * time.Millisecond)}
	start := time.Now()
	o.WaitForCall(near, kernel.MethodConversationSubmit, 1, 30*time.Second)
	took := time.Since(start)
	if !recorded.failed {
		t.Fatal("the wait did not fail without the test deadline")
	}
	if took > 5*time.Second {
		t.Fatalf("the wait took %s with a deadline 300ms away and within 30s", took)
	}
	if !strings.Contains(recorded.message, kernel.MethodConversationSubmit) {
		t.Errorf("the failure does not name the method: %q", recorded.message)
	}

	// Already inside the reporting margin: the wait gives up at once rather than poll.
	recorded = &recordingTB{TB: t}
	expired := &deadlineTB{recordingTB: recorded, at: time.Now().Add(200 * time.Millisecond)}
	start = time.Now()
	o.WaitForCall(expired, kernel.MethodConversationSubmit, 1, 30*time.Second)
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the wait took %s with its deadline already inside the margin", took)
	}
	if !recorded.failed {
		t.Fatal("the wait did not fail with its deadline inside the margin")
	}
}
