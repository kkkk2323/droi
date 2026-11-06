package automationwork

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/automations"
	"github.com/kkkk2323/droi/apps/native/internal/hostsession"
)

func newRunner(t *testing.T, d *fakedaemon.Daemon) *hostsession.Runner {
	t.Helper()
	return hostsession.New(hostsession.Config{
		ResolveURL: func(context.Context) (string, error) { return d.URL, nil },
		Credential: func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: "fk-test"}, nil },
		Log:        func(s string) { t.Log(s) },
	})
}

func connect(t *testing.T, r *hostsession.Runner) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.Start(ctx)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := r.Client(); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the Runner did not connect")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// hostHarness is a Scheduler whose runs open Sessions on the Fake Daemon
// through a real Runner.
func hostHarness(t *testing.T, sessions Sessions) (*harness, *SessionStarter) {
	h := &harness{t: t, dir: filepath.Join(t.TempDir(), "automations"), starter: &fakeStarter{}}
	h.set("08:00")
	st := NewSessionStarter(sessions, func(s string) { t.Log(s) })
	t.Cleanup(st.Close)
	h.s = New(Config{
		Dir: h.dir, Starter: st, Location: shanghai,
		Now: func() time.Time {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.clock
		},
		Hold: func() func() {
			h.mu.Lock()
			h.held++
			h.holdCalls++
			h.mu.Unlock()
			return func() {
				h.mu.Lock()
				h.held--
				h.mu.Unlock()
			}
		},
		Log: func(s string) { t.Log(s) },
	})
	st.Attach(h.s)
	return h, st
}

func (h *harness) waitRun(what string, ok func(automations.Run) bool) automations.Run {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if runs, err := h.s.Runs(h.only(), 1); err == nil && len(runs) == 1 && ok(runs[0]) {
			return runs[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	runs, _ := h.s.Runs(h.only(), 0)
	h.t.Fatalf("no %s: %+v", what, runs)
	return automations.Run{}
}

func (h *harness) only() string {
	items, _ := h.s.List()
	if len(items) != 1 {
		h.t.Fatalf("%d automations", len(items))
	}
	return items[0].ID
}

type initParams struct {
	SessionID             string                `json:"sessionId"`
	Cwd                   string                `json:"cwd"`
	Title                 string                `json:"title"`
	ModelID               string                `json:"modelId"`
	ReasoningEffort       string                `json:"reasoningEffort"`
	AutonomyLevel         string                `json:"autonomyLevel"`
	SkipPermissionsUnsafe *bool                 `json:"skipPermissionsUnsafe"`
	Tags                  []protocol.SessionTag `json:"tags"`
	DisabledToolIDs       []string              `json:"disabledToolIds"`
}

func requestsOf(t *testing.T, d *fakedaemon.Daemon, method string) []json.RawMessage {
	t.Helper()
	reqs, err := d.Requests()
	if err != nil {
		t.Fatal(err)
	}
	var out []json.RawMessage
	for _, r := range reqs {
		if r.Method == method {
			out = append(out, r.Params)
		}
	}
	return out
}

func TestARunOpensItsSessionOnTheDaemonAndClosesItWhenItsTurnEnds(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"All", " green."}, DelayMs: 20}})
	r := newRunner(t, d)
	connect(t, r)
	h, _ := hostHarness(t, r)
	a := h.save(automations.Automation{Name: "Daily report", Schedule: "0 9 * * *", Prompt: "Report.", Model: "glm-5.3-flash", ReasoningEffort: "high"})
	run, err := h.s.RunNow(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	ended := h.waitRun("succeeded run", func(r automations.Run) bool { return r.Status == automations.RunSucceeded })
	if ended.RunID != run.RunID || ended.SessionID != run.SessionID || ended.SessionID == "" {
		t.Fatalf("run %+v, ended %+v", run, ended)
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(requestsOf(t, d, "daemon.close_session")) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	closes := requestsOf(t, d, "daemon.close_session")
	var closed struct {
		SessionID string `json:"sessionId"`
	}
	if len(closes) != 1 || json.Unmarshal(closes[0], &closed) != nil || closed.SessionID != run.SessionID {
		t.Fatalf("close_session %s", closes)
	}
	if len(requestsOf(t, d, "daemon.archive_session")) != 0 {
		t.Fatal("archived the run's Session")
	}

	inits := requestsOf(t, d, "daemon.initialize_session")
	var p initParams
	if len(inits) != 1 || json.Unmarshal(inits[0], &p) != nil {
		t.Fatalf("initialize_session %s", inits)
	}
	if p.Title != "[Automation] Daily report" || p.Cwd != filepath.Join(h.dir, a.ID) || p.ModelID != "glm-5.3-flash" || p.ReasoningEffort != "high" ||
		p.SkipPermissionsUnsafe == nil || !*p.SkipPermissionsUnsafe || !slices.Contains(p.DisabledToolIDs, "CreateAutomation") ||
		!slices.Contains(p.DisabledToolIDs, "mcp:droi-workstream") || !slices.Contains(p.DisabledToolIDs, "mcp:droi-automations") || len(p.Tags) != 1 || p.Tags[0].Name != "droi.automation" ||
		p.Tags[0].Metadata["runId"] != run.RunID || p.Tags[0].Metadata["trigger"] != "manual" || p.Tags[0].Metadata["automationId"] != a.ID {
		t.Fatalf("initialize_session %s", inits[0])
	}
	var msg struct {
		Text string `json:"text"`
	}
	if adds := requestsOf(t, d, "daemon.add_user_message"); len(adds) != 1 || json.Unmarshal(adds[0], &msg) != nil || msg.Text != "Report." {
		t.Fatalf("add_user_message %s", adds)
	}
	if h.holds() != 0 {
		t.Fatalf("%d holds after the run", h.holds())
	}
}

func TestARunWaitsForTheDaemon(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"ok"}}})
	r := newRunner(t, d)
	h, _ := hostHarness(t, r)
	a := h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *", Prompt: "Report."})
	h.tick("09:00")
	if runs := h.runs(a.ID); len(runs) != 0 || h.item(a.ID).Running || h.holdsTaken() != 0 {
		t.Fatalf("without a Daemon: %+v", runs)
	}
	if _, err := h.s.RunNow(context.Background(), a.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Run now without a Daemon: %v", err)
	}
	connect(t, r)
	h.tick("09:01")
	if runs := h.runs(a.ID); len(runs) != 1 || runs[0].Trigger != automations.TriggerSchedule || runs[0].SessionID == "" {
		t.Fatalf("once the Daemon is there: %+v", runs)
	}
}

// refusingPrompt opens the Session, then fails as a Daemon that refused its
// prompt would.
type refusingPrompt struct {
	*hostsession.Runner
	opened string
}

func (r *refusingPrompt) Open(ctx context.Context, s hostsession.Spec) (hostsession.Opened, error) {
	s.Prompt = ""
	o, err := r.Runner.Open(ctx, s)
	if err != nil {
		return o, err
	}
	r.opened = o.SessionID
	return o, &droid.RPCError{Code: -32602, Message: "no such model"}
}

func TestARunWhoseSessionDidNotTakeItsPromptFailsAndLeavesNoSession(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"ok"}}})
	r := newRunner(t, d)
	connect(t, r)
	refusing := &refusingPrompt{Runner: r}
	h, _ := hostHarness(t, refusing)
	a := h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *", Prompt: "Report.", Model: "nope"})
	h.tick("09:00")
	runs := h.runs(a.ID)
	if len(runs) != 1 || runs[0].Status != automations.RunFailed || runs[0].Error != "daemon error -32602: no such model" {
		t.Fatalf("records %+v", runs)
	}
	var closed struct {
		SessionID string `json:"sessionId"`
	}
	if closes := requestsOf(t, d, "daemon.close_session"); len(closes) != 1 || json.Unmarshal(closes[0], &closed) != nil || closed.SessionID != refusing.opened {
		t.Fatalf("close_session %s, opened %s", closes, refusing.opened)
	}
	h.tick("09:01")
	if len(h.runs(a.ID)) != 1 {
		t.Fatal("tried again after the Daemon refused it")
	}
}

// earlyEnd is Sessions whose turn ends before Open returns.
type earlyEnd struct {
	mu     sync.Mutex
	subs   []func(hostsession.Event)
	closed []string
}

func (e *earlyEnd) Client() (*droid.Client, error) { return nil, nil }

func (e *earlyEnd) Open(context.Context, hostsession.Spec) (hostsession.Opened, error) {
	e.mu.Lock()
	subs := slices.Clone(e.subs)
	e.mu.Unlock()
	for _, fn := range subs {
		fn(hostsession.TurnEnded{SessionID: "s1", Reason: protocol.AgentTurnCompletionReasonError, Error: "model overloaded"})
		fn(hostsession.TurnEnded{SessionID: "other", Reason: protocol.AgentTurnCompletionReasonCompleted})
	}
	return hostsession.Opened{SessionID: "s1"}, nil
}

func (e *earlyEnd) Close(_ context.Context, id string) error {
	e.mu.Lock()
	e.closed = append(e.closed, id)
	e.mu.Unlock()
	return nil
}

func (e *earlyEnd) Following(string) bool { return true }

func (e *earlyEnd) Subscribe(fn func(hostsession.Event)) func() {
	e.mu.Lock()
	e.subs = append(e.subs, fn)
	e.mu.Unlock()
	return func() {}
}

func TestATurnThatEndsBeforeOpenReturnsEndsTheRun(t *testing.T) {
	sessions := &earlyEnd{}
	h, st := hostHarness(t, sessions)
	a := h.save(automations.Automation{Name: "Quick", Schedule: "0 9 * * *", Prompt: "p"})
	h.tick("09:00")
	ended := h.waitRun("failed run", func(r automations.Run) bool { return r.Status == automations.RunFailed })
	if ended.Error != "model overloaded" || ended.SessionID != "s1" || h.item(a.ID).Running {
		t.Fatalf("ended %+v", ended)
	}
	st.mu.Lock()
	left := len(st.early) + len(st.runs)
	st.mu.Unlock()
	if left != 0 {
		t.Fatal("kept a turn end it no longer needs")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		sessions.mu.Lock()
		closed := slices.Clone(sessions.closed)
		sessions.mu.Unlock()
		if slices.Equal(closed, []string{"s1"}) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("closed %v", closed)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestOutcome(t *testing.T) {
	for _, c := range []struct {
		t      hostsession.TurnEnded
		status automations.RunStatus
		text   string
	}{
		{hostsession.TurnEnded{Reason: protocol.AgentTurnCompletionReasonCompleted}, automations.RunSucceeded, ""},
		{hostsession.TurnEnded{Reason: protocol.AgentTurnCompletionReasonCancelled}, automations.RunInterrupted, ""},
		{hostsession.TurnEnded{Reason: protocol.AgentTurnCompletionReasonProcessExit, Error: hostsession.WorkerStopped}, automations.RunFailed, hostsession.WorkerStopped},
		{hostsession.TurnEnded{Reason: protocol.AgentTurnCompletionReasonProcessExit, Error: hostsession.LostTouch}, automations.RunInterrupted, "Droi lost touch with the Daemon during the run."},
		{hostsession.TurnEnded{Reason: protocol.AgentTurnCompletionReasonProcessExit, Error: hostsession.SessionGone}, automations.RunFailed, hostsession.SessionGone},
		{hostsession.TurnEnded{Reason: protocol.AgentTurnCompletionReasonError, Error: "model overloaded"}, automations.RunFailed, "model overloaded"},
		{hostsession.TurnEnded{Reason: protocol.AgentTurnCompletionReasonModelUsageExhausted}, automations.RunFailed, "model_usage_exhausted"},
	} {
		if status, text := Outcome(c.t); status != c.status || text != c.text {
			t.Errorf("%+v: %s %q, want %s %q", c.t, status, text, c.status, c.text)
		}
	}
}

// quietSessions opens Sessions whose turns never end, followed while
// following is set; with half set, Open fails once the Session is open.
type quietSessions struct {
	mu        sync.Mutex
	following bool
	half      error
	opened    int
	closed    []string
}

func (q *quietSessions) Client() (*droid.Client, error) { return nil, nil }

func (q *quietSessions) Open(context.Context, hostsession.Spec) (hostsession.Opened, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.opened++
	return hostsession.Opened{SessionID: fmt.Sprint("s", q.opened)}, q.half
}

func (q *quietSessions) Close(_ context.Context, id string) error {
	q.mu.Lock()
	q.closed = append(q.closed, id)
	q.mu.Unlock()
	return nil
}

func (q *quietSessions) Subscribe(func(hostsession.Event)) func() { return func() {} }

func (q *quietSessions) Following(string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.following
}

func TestARunWhoseSessionIsNoLongerFollowedIsInterruptedAtTheNextTick(t *testing.T) {
	sessions := &quietSessions{following: true}
	h, st := hostHarness(t, sessions)
	a := h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *"})
	h.tick("09:00")
	h.tick("09:01")
	if !h.item(a.ID).Running || h.holds() != 1 {
		t.Fatalf("a followed run ended: %+v", h.runs(a.ID))
	}
	sessions.mu.Lock()
	sessions.following = false
	sessions.mu.Unlock()
	h.tick("09:02")
	runs := h.runs(a.ID)
	if len(runs) != 1 || runs[0].Status != automations.RunInterrupted || runs[0].Error != "Droi lost touch with the Daemon during the run." ||
		h.holds() != 0 || h.item(a.ID).Running {
		t.Fatalf("records %+v, %d holds", runs, h.holds())
	}
	st.mu.Lock()
	left := len(st.runs)
	st.mu.Unlock()
	if left != 0 {
		t.Fatal("still waits for the lost run's end")
	}
}

func TestARunWhoseSessionOpenedButDidNotStartIsTriedTwiceMore(t *testing.T) {
	sessions := &quietSessions{half: errors.New("write: broken pipe")}
	h, _ := hostHarness(t, sessions)
	a := h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *"})
	h.tick("09:00")
	h.tick("09:01")
	if runs := h.runs(a.ID); len(runs) != 0 {
		t.Fatalf("records %+v", runs)
	}
	h.tick("09:02")
	h.tick("09:03")
	runs := h.runs(a.ID)
	sessions.mu.Lock()
	closed := slices.Clone(sessions.closed)
	sessions.mu.Unlock()
	if len(runs) != 1 || runs[0].Status != automations.RunFailed || runs[0].Error != "write: broken pipe" || !slices.Equal(closed, []string{"s1", "s2", "s3"}) {
		t.Fatalf("records %+v, closed %v", runs, closed)
	}
}
