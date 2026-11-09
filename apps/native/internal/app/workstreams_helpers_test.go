package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/sessions"
)

// fakeWorkstreams is the Host's Workstreams for the harness's Gateway: the
// Fake Daemon has none. It keeps the records in memory and the calls made.
type fakeWorkstreams struct {
	mu       sync.Mutex
	list     []gateway.Workstream
	created  []gateway.NewWorkstream
	settings []gateway.WorkstreamSettings
	archived []string
	// made is what Create answers, or createErr; kept what Archive answers.
	made      gateway.WorkstreamCreated
	createErr error
	kept      []string
	edits     []gateway.WorkstreamEdit
}

// Edit makes the fake a gateway.WorkstreamEditor.
func (f *fakeWorkstreams) Edit(_ context.Context, e gateway.WorkstreamEdit) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edits = append(f.edits, e)
	return nil
}

func (f *fakeWorkstreams) edited() []gateway.WorkstreamEdit {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.edits)
}

func (f *fakeWorkstreams) List(context.Context) ([]gateway.Workstream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.list), nil
}

func (f *fakeWorkstreams) Create(_ context.Context, w gateway.NewWorkstream) (gateway.WorkstreamCreated, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, w)
	if f.createErr != nil {
		return gateway.WorkstreamCreated{}, f.createErr
	}
	f.list = append(f.list, gateway.Workstream{ID: f.made.WorkstreamID, Title: w.Title, Workspace: w.Workspace, OrchestratorID: f.made.OrchestratorID})
	return f.made, nil
}

func (f *fakeWorkstreams) SetAgentSettings(_ context.Context, s gateway.WorkstreamSettings) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings = append(f.settings, s)
	for i := range f.list {
		if f.list[i].ID == s.ID {
			f.list[i].AgentModel, f.list[i].AgentReasoningEffort = s.AgentModel, s.AgentReasoningEffort
		}
	}
	return nil
}

func (f *fakeWorkstreams) Archive(_ context.Context, id string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.archived = append(f.archived, id)
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range f.list {
		if f.list[i].ID != id {
			continue
		}
		f.list[i].ArchivedAt = now
		agents := slices.Clone(f.list[i].Agents)
		for j := range agents {
			if agents[j].ArchivedAt == "" {
				agents[j].ArchivedAt = now
			}
		}
		f.list[i].Agents = agents
	}
	return f.kept, nil
}

// calls reads what was asked of the fake, under its lock.
func (f *fakeWorkstreams) calls() (created []gateway.NewWorkstream, settings []gateway.WorkstreamSettings, archived []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.created), slices.Clone(f.settings), slices.Clone(f.archived)
}

func workstreamTag(ws, role, orchestrator string) map[string]any {
	meta := map[string]string{"workstreamId": ws, "role": role}
	if orchestrator != "" {
		meta["orchestratorId"] = orchestrator
	}
	return map[string]any{"name": sessions.WorkstreamTag, "metadata": meta}
}

func continues(parent string) map[string]any {
	return map[string]any{"name": sessions.ContinuesTag, "metadata": map[string]string{"parent": parent}}
}

// workstreamsScenario has a Session of no Workstream, and Workstream ws-1's
// orchestrator and two agents.
func workstreamsScenario() fakedaemon.Scenario {
	say := []fakedaemon.Message{{Role: "user", Text: "Go on"}}
	return fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{
		{Title: "Fix the login race", Cwd: "/Users/dev/acme-web", Messages: say},
		{Title: "Payment migration lead", Cwd: "/Users/dev/pay", Messages: say,
			Extra: map[string]any{"sessionId": "orch-1", "tags": []map[string]any{workstreamTag("ws-1", sessions.WorkstreamOrchestrator, "")}}},
		{Title: "gRPC endpoints", Cwd: "/Users/dev/pay-worktrees/grpc", Messages: say,
			Extra: map[string]any{"sessionId": "agent-1", "tags": []map[string]any{workstreamTag("ws-1", sessions.WorkstreamAgent, "orch-1")}}},
		{Title: "Client switch", Cwd: "/Users/dev/pay-worktrees/clients", Messages: say,
			Extra: map[string]any{"sessionId": "agent-2", "tags": []map[string]any{workstreamTag("ws-1", sessions.WorkstreamAgent, "orch-1")}}},
	}}
}

func paymentWorkstream() gateway.Workstream {
	at := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
	return gateway.Workstream{
		ID: "ws-1", Title: "Payment migration", Workspace: "/Users/dev/pay", SharedFolder: "/Users/dev/droi-data/workstreams/ws-1/shared",
		OrchestratorID: "orch-1", CreatedAt: at,
		Agents: []gateway.WorkstreamAgent{
			{SessionID: "agent-1", Title: "gRPC endpoints", Branch: "droi/grpc-endpoints", CreatedAt: at,
				LastReport: &gateway.WorkstreamReport{Kind: "done", Text: "Moved the first endpoint.", At: at}},
			{SessionID: "agent-2", Title: "Client switch", Branch: "droi/client-switch", CreatedAt: at},
			{SessionID: "agent-3", Title: "Old cleanup", Branch: "droi/cleanup", CreatedAt: at, ArchivedAt: at},
		},
	}
}

// shot writes the window to DROI_NATIVE_SHOTS, when it names a folder, for
// a look; the Workstreams have no reference screens.
func (h *harness) shot(name string) {
	dir := os.Getenv("DROI_NATIVE_SHOTS")
	if dir == "" {
		return
	}
	h.settle()
	_ = os.MkdirAll(dir, 0o755)
	writePNG(filepath.Join(dir, name+".png"), h.tt.Image())
}

// closeTransport ends the App before the harness's own cleanup, and the
// idle connections of http.DefaultTransport, which the Gateway Client's
// requests share: one dialed but never used holds the Gateway's shutdown
// for seconds.
func closeTransport(t *testing.T, h *harness) {
	t.Cleanup(func() {
		h.a.Close()
		http.DefaultTransport.(*http.Transport).CloseIdleConnections()
	})
}

// workstreamsHarness is the window with a Host whose Workstreams are fake.
func workstreamsHarness(t *testing.T, fake *fakeWorkstreams, with func(*Config)) *harness {
	t.Helper()
	return workstreamsHarnessOf(t, workstreamsScenario(), fake, with)
}

func workstreamsHarnessOf(t *testing.T, sc fakedaemon.Scenario, fake *fakeWorkstreams, with func(*Config)) *harness {
	t.Helper()
	h := startHarnessGateway(t, sc, "", "", func(o *gateway.Options) { o.Workstreams = fake }, with)
	closeTransport(t, h)
	return h
}

// notifyState sends a working state the Daemon reports for a Session, and
// waits until the window applied it: a state the Client has not read yet
// would make a row's group depend on how many frames the test drew meanwhile.
func (h *harness) notifyState(id, state string) {
	h.t.Helper()
	if err := h.d.Notify(id, map[string]any{"type": "droid_working_state_changed", "newState": state}); err != nil {
		h.t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.a.mu.Lock()
		applied := h.a.busy[id] == state
		h.a.mu.Unlock()
		if applied || time.Now().After(deadline) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (h *harness) finds(label string) bool {
	_, ok := h.tt.Find(label)
	return ok
}

// openedSessions stands in for the Daemon's list of opened Sessions, with
// the prompts it counts; reads counts the reads.
type openedSessions struct {
	mu        sync.Mutex
	questions map[string]int64
	reads     int
}

func (o *openedSessions) set(questions map[string]int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.questions = questions
}

func (o *openedSessions) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.reads
}

func (o *openedSessions) read(context.Context) (*protocol.ListOpenedSessionsResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.reads++
	res := &protocol.ListOpenedSessionsResult{}
	for id, n := range o.questions {
		res.Sessions = append(res.Sessions, protocol.DaemonOpenedSessionInfo{SessionID: id, WorkingState: protocol.DroidWorkingStateIdle, PendingAskUserRequestCount: &n})
	}
	return res, nil
}

// A clock the test moves: the real time, plus what it was moved by.
type movedClock struct{ by atomic.Int64 }

func (m *movedClock) now() time.Time { return time.Now().Add(time.Duration(m.by.Load())) }

func (m *movedClock) move(d time.Duration) { m.by.Add(int64(d)) }
