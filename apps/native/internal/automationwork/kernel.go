package automationwork

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/automations"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

var _ KernelRuns = (*kernel.Client)(nil)

// A run on the Kernel (M4 4L): a conversation of kind automation, its own approval level, the prompt as one
// submission, and the run's end as Pi settles that submission. The submission's record is durable, so a run
// that outlives a Kernel restart is still followed to its end.

// KernelRuns is what a KernelStarter needs of the Kernel. *kernel.Client is one.
type KernelRuns interface {
	Harness(ctx context.Context) (kernel.HarnessInfo, error)
	Create(ctx context.Context, p kernel.CreateParams) (kernel.CreateResult, error)
	UpdateIndex(ctx context.Context, p kernel.IndexUpdateParams) error
	PolicySet(ctx context.Context, p kernel.PolicySetParams) (kernel.PolicyResult, error)
	Submit(ctx context.Context, p kernel.SubmitParams) (kernel.SubmitResult, error)
	SubmissionGet(ctx context.Context, p kernel.AbortSubmissionParams) (kernel.SubmissionGet, error)
}

// AutomationKind is the kind of a run's conversation.
const AutomationKind = "automation"

// KernelStarter is the Starter of a Host that runs a Kernel.
type KernelStarter struct {
	// Client answers the Host's connection to the Kernel, an error while there is none.
	Client func() (KernelRuns, error)
	// KeepPolicy stores a conversation's own approval level with the Host, which pushes it again whenever the
	// Kernel starts again: the Kernel keeps none of its own.
	KeepPolicy func(id kernel.ID, autonomy string) error
	// Every is how often a run's submission is read; 2 seconds when zero.
	Every time.Duration
	Log   func(string)

	mu       sync.Mutex
	finished func(runID string, status automations.RunStatus, errText string)
	stop     chan struct{}
	follows  sync.WaitGroup
}

// Attach reports the runs' ends to s.
func (st *KernelStarter) Attach(s *Scheduler) {
	st.mu.Lock()
	st.finished = s.Finished
	st.mu.Unlock()
}

// Close stops following the runs; their ends are no longer reported.
func (st *KernelStarter) Close() {
	st.mu.Lock()
	if st.stop == nil {
		st.stop = make(chan struct{})
	}
	select {
	case <-st.stop:
	default:
		close(st.stop)
	}
	st.mu.Unlock()
	st.follows.Wait()
}

func (st *KernelStarter) stopped() <-chan struct{} {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.stop == nil {
		st.stop = make(chan struct{})
	}
	return st.stop
}

func (st *KernelStarter) log(s string) {
	if st.Log != nil {
		st.Log(s)
	}
}

// KernelAutonomy is the Kernel's approval level for an Automation's: without asking ("") is all, and the three
// levels are the window's own mapping of Low, Medium and High. One it does not know, from a HEARTBEAT.md edited by
// hand, is "ask", the level that asks most, as the window reads one.
func KernelAutonomy(level string) string {
	switch level {
	case "low":
		return "edits"
	case "medium":
		return "safe"
	case "", "high":
		return "all"
	}
	return "ask"
}

// Start creates the run's conversation and submits its prompt. The conversation and the submission carry the run's
// id as their request id, so a call the Kernel took but did not answer is not taken twice. A start that fails once
// the conversation is made archives it: the Scheduler tries again with a new run.
func (st *KernelStarter) Start(ctx context.Context, run RunSpec) (string, error) {
	c, err := st.Client()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	fail := func(err error, created bool) error {
		var refused *kernel.RPCError
		switch {
		case errors.As(err, &refused):
			return err
		case created:
			return &HalfStartedError{Err: err}
		}
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	info, err := c.Harness(ctx)
	if err != nil {
		return "", fail(err, false)
	}
	agent := runAgent(info, run)
	if agent == nil && run.ModelID != "" {
		st.log(fmt.Sprintf("run %s: the Kernel does not offer the model %q; the run uses the Kernel's own", run.RunID, run.ModelID))
	}
	created, err := c.Create(ctx, kernel.CreateParams{
		Kind:      AutomationKind,
		Cwd:       run.Cwd,
		Agent:     agent,
		RequestID: "automation-run:" + run.RunID,
	})
	if err != nil {
		return "", fail(err, false)
	}
	id := created.ConversationID
	discard := func(err error) error {
		archived := true
		actx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if aerr := c.UpdateIndex(actx, kernel.IndexUpdateParams{ID: id, Archived: &archived}); aerr != nil {
			st.log(fmt.Sprintf("archiving the conversation %s of run %s: %v", id, run.RunID, aerr))
		}
		cancel()
		st.forget(id)
		return fail(err, true)
	}
	if err := c.UpdateIndex(ctx, kernel.IndexUpdateParams{ID: id, Title: kernel.TitleText(run.Title), TitleSource: kernel.TitleSourceChosen}); err != nil {
		return "", discard(err)
	}
	level := KernelAutonomy(string(run.AutonomyLevel))
	if run.SkipPermissions {
		level = "all"
	}
	// Stored before it is pushed: a Kernel that starts again between the two gets it from the Host.
	if st.KeepPolicy != nil {
		if err := st.KeepPolicy(id, level); err != nil {
			return "", discard(err)
		}
	}
	if _, err := c.PolicySet(ctx, kernel.PolicySetParams{ID: &id, Policy: kernel.PolicyChange{Autonomy: level}}); err != nil {
		return "", discard(err)
	}
	sub, err := c.Submit(ctx, kernel.SubmitParams{ID: id, Input: run.Prompt, WhenBusy: kernel.WhenBusyFollowUp, RequestID: "automation-run:" + run.RunID})
	if err != nil {
		return "", discard(err)
	}
	st.follows.Add(1)
	go st.follow(run.RunID, id, sub.SubmissionID, level, c)
	return id.String(), nil
}

// runAgent is the run's model and thinking level, when the Kernel offers the model; nil leaves the Kernel's own.
func runAgent(info kernel.HarnessInfo, run RunSpec) *kernel.AgentChange {
	i := slices.IndexFunc(info.Models, func(m kernel.ModelInfo) bool { return m.ID == run.ModelID })
	if i < 0 {
		return nil
	}
	m := info.Models[i]
	change := &kernel.AgentChange{Model: kernel.Some(kernel.ModelRef{Provider: m.Provider, ModelID: m.ID})}
	if effort := string(run.ReasoningEffort); effort != "" && slices.Contains(m.Thinking, effort) {
		change.ThinkingLevel = kernel.Some(effort)
	}
	return change
}

// forget drops the level kept for a run's conversation: the run is over, and a stored level per run would pile up
// in Settings and be pushed again on every connection.
func (st *KernelStarter) forget(id kernel.ID) {
	if st.KeepPolicy == nil {
		return
	}
	if err := st.KeepPolicy(id, ""); err != nil {
		st.log(fmt.Sprintf("forgetting the level of conversation %s: %v", id, err))
	}
}

// follow reads the run's submission until Pi settles it, through whatever connection the Host has then. A new
// connection is a Kernel that may have started again, with no level for the conversation: level ("" for none of the
// run's own) is given to it again before the run's next tool call waits for an approval nobody gives.
func (st *KernelStarter) follow(runID string, id, submission kernel.ID, level string, last KernelRuns) {
	defer st.follows.Done()
	end := func(status automations.RunStatus, text string) {
		if level != "" {
			st.forget(id)
		}
		st.report(runID, status, text)
	}
	every := st.Every
	if every <= 0 {
		every = 2 * time.Second
	}
	stop := st.stopped()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			if level != "" {
				st.forget(id)
			}
			return
		case <-tick.C:
		}
		c, err := st.Client()
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if c != last && level != "" {
			if _, err := c.PolicySet(ctx, kernel.PolicySetParams{ID: &id, Policy: kernel.PolicyChange{Autonomy: level}}); err != nil {
				st.log(fmt.Sprintf("the level of run %s on a new connection: %v", runID, err))
			} else {
				last = c
			}
		}
		got, err := c.SubmissionGet(ctx, kernel.AbortSubmissionParams{ID: id, SubmissionID: submission})
		cancel()
		if err != nil {
			if kernel.Is(err, kernel.CodeSubmissionGone) || kernel.Is(err, kernel.CodeConversationGone) {
				end(automations.RunFailed, err.Error())
				return
			}
			continue
		}
		if status, text, ended := SubmissionOutcome(got); ended {
			end(status, text)
			return
		}
	}
}

func (st *KernelStarter) report(runID string, status automations.RunStatus, text string) {
	st.mu.Lock()
	finished := st.finished
	st.mu.Unlock()
	if finished != nil {
		finished(runID, status, text)
	}
}

// SubmissionOutcome is how a run whose submission Pi settled ended: answered is succeeded, stopped is
// interrupted, anything else failed with the model's words or Pi's reason. ended is false while it runs.
func SubmissionOutcome(s kernel.SubmissionGet) (status automations.RunStatus, text string, ended bool) {
	switch s.Status {
	case "done":
		return automations.RunSucceeded, "", true
	case "unanswered":
	default:
		return "", "", false
	}
	text = s.Detail
	if text == "" {
		text = "The run ended without an answer (" + s.Reason + ")."
	}
	switch s.Reason {
	case "aborted", "interrupted":
		return automations.RunInterrupted, text, true
	}
	return automations.RunFailed, text, true
}
