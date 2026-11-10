package automationwork

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/v2/internal/automations"
	"github.com/kkkk2323/droi/apps/native/v2/internal/hostsession"
)

// Sessions is what a SessionStarter needs of the Host's Sessions
// (hostsession.Runner).
type Sessions interface {
	Client() (*droid.Client, error)
	Open(ctx context.Context, s hostsession.Spec) (hostsession.Opened, error)
	Close(ctx context.Context, sessionID string) error
	Subscribe(fn func(hostsession.Event)) (unsubscribe func())
	Following(sessionID string) bool
}

// closeTimeout bounds closing a run's Session once its turn ended.
const closeTimeout = 30 * time.Second

// SessionStarter is the Starter of the Host: it opens a run's Session on the
// Host's own connection to its Daemon, reports the end of its turn to the
// Scheduler, and closes the Session then; the Session stays listed, for
// the Automations page to open.
type SessionStarter struct {
	sessions    Sessions
	log         func(string)
	unsubscribe func()

	mu       sync.Mutex
	finished func(runID string, status automations.RunStatus, errText string)
	// runs are the open runs' ids, by Session.
	runs map[string]string
	// opening counts the Opens under way; early keeps the turn ends that
	// came meanwhile for Sessions not yet known, as one may come before its
	// Open returns.
	opening int
	early   map[string]hostsession.TurnEnded
}

// NewSessionStarter is a SessionStarter over sessions; Attach gives it the
// Scheduler to report to.
func NewSessionStarter(sessions Sessions, log func(string)) *SessionStarter {
	if log == nil {
		log = func(string) {}
	}
	st := &SessionStarter{sessions: sessions, log: log, runs: map[string]string{}, early: map[string]hostsession.TurnEnded{}}
	st.unsubscribe = sessions.Subscribe(st.see)
	return st
}

// Attach reports the runs' ends to s.
func (st *SessionStarter) Attach(s *Scheduler) {
	st.mu.Lock()
	st.finished = s.Finished
	st.mu.Unlock()
}

// Close stops hearing the Sessions' turn ends.
func (st *SessionStarter) Close() { st.unsubscribe() }

// Start opens the run's Session and sends its prompt. Without a connection
// to the Daemon, or when the Daemon did not answer, it fails with
// ErrUnavailable, a *HalfStartedError when the Session had opened; a
// refusal of the Daemon's is any other error. A Session that started but
// did not take its prompt is closed.
func (st *SessionStarter) Start(ctx context.Context, run RunSpec) (string, error) {
	if run.Conversation != "" {
		return "", ErrLoopNeedsKernel
	}
	if _, err := st.sessions.Client(); err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	st.mu.Lock()
	st.opening++
	st.mu.Unlock()
	opened, err := st.sessions.Open(ctx, hostsession.Spec{
		Title:           run.Title,
		Cwd:             run.Cwd,
		ModelID:         run.ModelID,
		ReasoningEffort: run.ReasoningEffort,
		AutonomyLevel:   run.AutonomyLevel,
		SkipPermissions: run.SkipPermissions,
		Tags:            run.Tags,
		Prompt:          run.Prompt,
		DisabledTools:   run.DisabledTools,
	})
	st.mu.Lock()
	st.opening--
	ended, endedEarly := st.early[opened.SessionID]
	delete(st.early, opened.SessionID)
	if err == nil && !endedEarly {
		st.runs[opened.SessionID] = run.RunID
	}
	if st.opening == 0 {
		clear(st.early)
	}
	st.mu.Unlock()
	if err != nil {
		if opened.SessionID != "" {
			st.close(opened.SessionID)
		}
		var refused *droid.RPCError
		switch {
		case errors.As(err, &refused):
		case opened.SessionID != "":
			err = &HalfStartedError{Err: err}
		default:
			err = fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		return "", err
	}
	if endedEarly {
		go st.finish(run.RunID, ended)
	}
	return opened.SessionID, nil
}

// Abandon gives up a run whose Session the Runner no longer follows (the
// Daemon lost it, or the Runner could not load it again), so that no turn
// end will come for it.
func (st *SessionStarter) Abandon(run automations.Run) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.runs[run.SessionID] != run.RunID || st.sessions.Following(run.SessionID) {
		return false
	}
	delete(st.runs, run.SessionID)
	return true
}

// see hears the Runner's events on the Controller's event goroutine, so it
// hands a run's end to a goroutine of its own.
func (st *SessionStarter) see(e hostsession.Event) {
	t, ok := e.(hostsession.TurnEnded)
	if !ok {
		return
	}
	st.mu.Lock()
	runID, known := st.runs[t.SessionID]
	if known {
		delete(st.runs, t.SessionID)
	} else if st.opening > 0 {
		st.early[t.SessionID] = t
	}
	st.mu.Unlock()
	if known {
		go st.finish(runID, t)
	}
}

func (st *SessionStarter) finish(runID string, t hostsession.TurnEnded) {
	status, text := Outcome(t)
	st.mu.Lock()
	finished := st.finished
	st.mu.Unlock()
	if finished != nil {
		finished(runID, status, text)
	}
	st.close(t.SessionID)
}

func (st *SessionStarter) close(sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	if err := st.sessions.Close(ctx, sessionID); err != nil {
		st.log("close the run's Session " + sessionID + ": " + err.Error())
	}
}

// Outcome is how a run whose turn ended this way ended: succeeded when
// the turn completed, interrupted when someone stopped it or when the turn
// ended while the Host was not connected (hostsession.LostTouch), else
// failed with the turn's error or, without one, its reason (a worker that
// stopped mid-turn reads hostsession.WorkerStopped).
func Outcome(t hostsession.TurnEnded) (automations.RunStatus, string) {
	switch t.Reason {
	case protocol.AgentTurnCompletionReasonCompleted:
		return automations.RunSucceeded, ""
	case protocol.AgentTurnCompletionReasonCancelled:
		return automations.RunInterrupted, t.Error
	}
	if t.Error == hostsession.LostTouch {
		return automations.RunInterrupted, lost
	}
	if t.Error != "" {
		return automations.RunFailed, t.Error
	}
	return automations.RunFailed, string(t.Reason)
}
