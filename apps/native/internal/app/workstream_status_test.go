package app

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/sessions"
)

// waitFor draws frames until cond holds and returns how long that took, or
// -1 when it did not within limit.
func (h *harness) waitFor(limit time.Duration, cond func() bool) time.Duration {
	start := time.Now()
	for time.Since(start) < limit {
		h.frame()
		if cond() {
			return time.Since(start)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return -1
}

func (h *harness) shows(text string) bool { return slices.Contains(h.tt.Texts(), text) }

// An agent the Host just recorded counts as working at once, though the
// Daemon lists its Session late and the orchestrator does nothing meanwhile.
func TestANewAgentIsCountedBeforeTheDaemonListsIt(t *testing.T) {
	sc := workstreamsScenario()
	sc.Sessions = append(sc.Sessions, fakedaemon.SessionSpec{Title: "New agent", Cwd: "/Users/dev/pay-worktrees/new",
		Messages: []fakedaemon.Message{{Role: "user", Text: "Go on"}},
		Extra: map[string]any{"sessionId": "agent-9", "unwritten": true,
			"tags": []map[string]any{workstreamTag("ws-1", sessions.WorkstreamAgent, "orch-1")}}})
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}
	h := workstreamsHarnessOf(t, sc, fake, nil)
	h.until("the records", func() bool { return h.hasText("Payment migration lead") && h.a.workstreams.list != nil })

	w := paymentWorkstream()
	w.Agents = append(w.Agents, gateway.WorkstreamAgent{SessionID: "agent-9", Title: "New agent", Branch: "droi/new", CreatedAt: time.Now().UTC().Format(time.RFC3339), Awaiting: true})
	fake.mu.Lock()
	fake.list = []gateway.Workstream{w}
	fake.mu.Unlock()
	go func() {
		time.Sleep(3500 * time.Millisecond)
		_ = h.d.Write("agent-9")
	}()
	h.notifyState("agent-9", "thinking")
	// The list is read at 0 s and 1 s, then at 3 s: the first retry is the
	// earliest the Session list could show it.
	if d := h.waitFor(900*time.Millisecond, func() bool { return h.shows("1 agent working") }); d < 0 {
		t.Fatalf("the new agent is not counted before the Daemon lists it; texts %q", h.tt.Texts())
	}
}

// An agent that already works when the window starts, which no event told
// of, is counted from the Daemon's list of opened Sessions.
func TestAnAgentWorkingBeforeTheWindowStartedIsCounted(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.a.workstreams.openedSessions = func(context.Context) (*protocol.ListOpenedSessionsResult, error) {
		return &protocol.ListOpenedSessionsResult{Sessions: []protocol.DaemonOpenedSessionInfo{
			{SessionID: "agent-2", WorkingState: protocol.DroidWorkingStateStreamingAssistantMessage},
		}}, nil
	}
	h.until("the records", func() bool { return h.hasText("Payment migration lead") && h.a.workstreams.list != nil })
	h.a.loadPending(true)
	if d := h.waitFor(3*time.Second, func() bool { return h.shows("1 agent working") }); d < 0 {
		t.Fatalf("the working agent is not counted; texts %q", h.tt.Texts())
	}
	// Its own end is heard as any other.
	h.notifyState("agent-2", "idle")
	if d := h.waitFor(3*time.Second, func() bool { return !h.shows("1 agent working") }); d < 0 {
		t.Fatalf("the agent that rested is still counted; texts %q", h.tt.Texts())
	}
}

// The Daemon may count an agent's question only after the agent rested: the
// window reads again a little later, not only when something else changes.
func TestAQuestionCountedAfterTheAgentRestedIsShown(t *testing.T) {
	clock := &movedClock{}
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, func(cfg *Config) { cfg.Now = clock.now })
	h.until("the records", func() bool { return h.hasText("Payment migration lead") && h.a.workstreams.list != nil })
	opened := &openedSessions{questions: map[string]int64{}}
	h.a.workstreams.openedSessions = opened.read
	h.notifyState("agent-2", "thinking")
	h.until("the agent working", func() bool { return h.shows("1 agent working") })
	h.notifyState("agent-2", "idle")
	h.until("the agent rested", func() bool { return !h.shows("1 agent working") && opened.count() > 0 })
	// The reads the rest itself causes end; the Daemon counts the question
	// only after them.
	for range 60 {
		h.frame()
		time.Sleep(5 * time.Millisecond)
	}
	reads := opened.count()
	opened.set(map[string]int64{"agent-2": 1})
	for range 40 {
		h.frame()
		time.Sleep(5 * time.Millisecond)
	}
	if h.shows("1 waiting on you") || opened.count() != reads {
		t.Fatalf("read again at once: %d reads, want %d", opened.count(), reads)
	}
	clock.move(1100 * time.Millisecond)
	h.until("a second read", func() bool { return opened.count() > reads })
	h.until("the question", func() bool { return h.shows("1 waiting on you") })
}
