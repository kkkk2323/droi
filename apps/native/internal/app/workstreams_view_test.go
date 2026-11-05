package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/activity"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
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

func (h *harness) notifyState(id, state string) {
	h.t.Helper()
	if err := h.d.Notify(id, map[string]any{"type": "droid_working_state_changed", "newState": state}); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) finds(label string) bool {
	_, ok := h.tt.Find(label)
	return ok
}

// The sidebar keeps a Workstream's agents out of the Session list, marks its
// orchestrator, and counts the agents that wait on the user.
func TestTheSidebarLeavesWorkstreamAgentsOutAndCountsTheWaitingOnes(t *testing.T) {
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}
	h := workstreamsHarness(t, fake, nil)
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") && h.hasText("Payment migration lead") })
	if h.hasText("gRPC endpoints") || h.hasText("Client switch") {
		t.Fatalf("an agent is in the sidebar: %q", h.tt.Texts())
	}
	if !h.finds("Workstreams") || !h.finds("Orchestrator") {
		t.Fatal("no Workstreams row or no orchestrator mark")
	}
	if h.finds("Workstreams, 1 waiting on you") {
		t.Fatal("a waiting count before any agent waits")
	}
	h.notifyState("agent-1", "waiting_for_tool_confirmation")
	h.until("the waiting count", func() bool { return h.finds("Workstreams, 1 waiting on you") })
	h.notifyState("agent-1", "idle")
	h.until("no waiting count", func() bool { return !h.finds("Workstreams, 1 waiting on you") })
}

// Search finds no agent either, by title or by what it says.
func TestSearchLeavesWorkstreamAgentsOut(t *testing.T) {
	agent := sessions.Summary{SessionID: "agent-1", Title: "gRPC endpoints", Tags: []sessions.Tag{{Name: sessions.WorkstreamTag, Metadata: map[string]string{"workstreamId": "ws-1", "role": "agent"}}}}
	lead := sessions.Summary{SessionID: "orch-1", Title: "gRPC lead", Tags: []sessions.Tag{{Name: sessions.WorkstreamTag, Metadata: map[string]string{"workstreamId": "ws-1", "role": "orchestrator"}}}}
	listed := []sessions.Summary{agent, lead}
	if hits := titleHits(listed, "grpc"); len(hits) != 1 || hits[0].sessionID != "orch-1" {
		t.Fatalf("title hits %+v", hits)
	}
	res := &protocol.SearchSessionsResult{Sessions: []protocol.DaemonSearchSessionsSessionResult{{SessionID: "agent-1", Title: "gRPC endpoints"}, {SessionID: "orch-1", Title: "gRPC lead"}}}
	if hits := contentHits(res, listed); len(hits) != 1 || hits[0].sessionID != "orch-1" {
		t.Fatalf("content hits %+v", hits)
	}
}

// An agent this window has not loaded waits on a permission request, which
// its working state tells, or on a question, which only the Daemon's count
// of its prompts tells.
func TestHowAWorkstreamAgentWaits(t *testing.T) {
	a := &App{}
	a.workstreams.busy = map[string]activity.Activity{"asks": activity.NeedsInput, "asked": activity.NeedsInput, "works": activity.Working}
	a.workstreams.pending = map[string]pendingPrompts{"asked": {questions: 1}, "idle-asked": {questions: 1}, "works": {questions: 1}}
	type want struct {
		how      string
		fromList bool
	}
	for id, w := range map[string]want{
		"asks": {waitsApproval, false}, "asked": {waitsQuestion, true}, "idle-asked": {waitsQuestion, true}, "works": {}, "rests": {},
	} {
		if how, fromList := a.waitsOn(id); how != w.how || fromList != w.fromList {
			t.Errorf("%s waits on %q (from the list: %v), want %q (%v)", id, how, fromList, w.how, w.fromList)
		}
	}
}

// An agent this window loaded waits as its Prompt says.
func TestALoadedWorkstreamAgentWaitsAsItsPromptSays(t *testing.T) {
	for _, tc := range []struct {
		name, label string
		turn        fakedaemon.Turn
		prompt      string
	}{
		{"permission", "gRPC endpoints, Waiting on you, Needs approval", fakedaemon.Turn{Kind: "permission", Command: "rm -rf build", Reply: "Removed."}, "rm -rf build"},
		{"question", "gRPC endpoints, Waiting on you, Has a question", fakedaemon.Turn{Kind: "askUser", Question: "Which environment?", Options: []string{"staging", "production"}}, "Which environment?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc := workstreamsScenario()
			sc.Turn = &tc.turn
			h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
			h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
			h.a.Go(Route{Name: "session", SessionID: "agent-1"})
			h.send("Port the endpoint")
			h.until("the prompt", func() bool { return h.hasText(tc.prompt) })
			if !h.a.loaded("agent-1") {
				t.Fatal("the agent is not loaded")
			}
			h.until("the waiting count", func() bool { return h.finds("Workstreams, 1 waiting on you") })
			h.a.Go(Route{Name: "workstreams", Workstream: "ws-1"})
			h.until("the agent's row", func() bool { return h.finds(tc.label) })
		})
	}
}

// With no Workstream yet, the page says what one is for and offers to start one.
func TestTheWorkstreamsPageInvitesToStartTheFirst(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{}, nil)
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("Workstreams")
	h.until("the empty page", func() bool { return h.hasText("No workstreams yet") })
	for _, want := range []string{
		"An orchestrator session splits a long goal among agents.",
		"Start one for work that spans days and splits into parts",
	} {
		if !h.hasText(want) {
			t.Errorf("no %q in %q", want, h.tt.Texts())
		}
	}
	h.click("New workstream")
	h.until("the form", func() bool { return h.finds("Workstream name") })
}

const notGitNote = "This folder is not a git repository, so agents share it instead of working in worktrees of their own."

// openNewWorkstream goes to the New workstream form.
func (h *harness) openNewWorkstream() {
	h.t.Helper()
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.a.Go(Route{Name: "workstreams", Tab: newWorkstreamTab})
	h.until("the form", func() bool { return h.finds("Workstream name") })
}

// The form asks for a name, a workspace the Daemon can use and a goal, with
// the focus on the field in error, says when the workspace is no git
// repository, then starts the Workstream with the agents on the
// orchestrator's model and opens the orchestrator's Session.
func TestStartingAWorkstream(t *testing.T) {
	plain, repo := t.TempDir(), t.TempDir()
	sc := workstreamsScenario()
	sc.ValidDirectories = []string{plain, repo}
	sc.Input = map[string]any{"gitRepos": map[string]any{repo: map[string]any{"currentBranch": "main", "branches": []string{"main"}}}}
	fake := &fakeWorkstreams{made: gateway.WorkstreamCreated{WorkstreamID: "ws-2", OrchestratorID: "orch-1"}}
	h := workstreamsHarnessOf(t, sc, fake, nil)
	h.openNewWorkstream()
	if !h.hasText("Same as orchestrator") {
		t.Fatalf("the agent model is not the orchestrator's: %q", h.tt.Texts())
	}
	h.until("the default model", func() bool { return h.hasText("Auto Model") })
	if slices.Contains(h.tt.Texts(), "None") {
		t.Fatalf("the default model names the effort None: %q", h.tt.Texts())
	}
	h.click("Start workstream")
	h.until("the name error", func() bool { return h.hasText("Enter a name.") })
	if !h.tt.Focused("Workstream name") {
		t.Fatal("the name field does not have the focus")
	}
	h.click("Workstream name")
	h.tt.Type("Payment migration")
	h.frame()
	h.click("Start workstream")
	h.until("the workspace error", func() bool { return h.hasText("Choose a workspace.") })
	if !h.tt.Focused("Workstream workspace") {
		t.Fatal("the workspace field does not have the focus")
	}

	h.tt.Type("/nowhere")
	h.frame()
	h.until("the Daemon's answer", func() bool { return h.hasText("Directory does not exist: /nowhere") })
	h.a.workstreams.form.workspace = plain
	h.until("the note", func() bool { return h.hasText(notGitNote) })
	h.a.workstreams.form.workspace = repo
	h.until("the repository checked", func() bool { return h.a.workstreams.checks[repo].done && !h.hasText(notGitNote) })

	h.click("Start workstream")
	h.until("the goal error", func() bool { return h.hasText("Enter the goal.") })
	if !h.tt.Focused("Workstream goal") {
		t.Fatal("the goal field does not have the focus")
	}
	h.shot("workstream-new")
	h.tt.Type("Move payments to gRPC.")
	h.frame()
	h.click("Start workstream")
	h.until("the orchestrator's Session", func() bool { r := h.a.Route(); return r.Name == "session" && r.SessionID == "orch-1" })
	created, _, _ := fake.calls()
	want := []gateway.NewWorkstream{{Title: "Payment migration", Workspace: repo, Goal: "Move payments to gRPC."}}
	if !slices.Equal(created, want) {
		t.Fatalf("created %+v, want %+v", created, want)
	}
}

// Models and efforts picked go to the Host; an effort the agents' new model
// does not have is cleared.
func TestStartingAWorkstreamWithModelsOfItsOwn(t *testing.T) {
	fake := &fakeWorkstreams{made: gateway.WorkstreamCreated{WorkstreamID: "ws-2", OrchestratorID: "orch-1"}}
	h := workstreamsHarness(t, fake, nil)
	// The whole form, the pickers' panels too, in the window.
	h.tt.SetSize(refW, 1200)
	h.openNewWorkstream()
	h.until("the models", func() bool { dv := h.a.sessionDefaults(); return dv != nil && len(dv.Models) > 0 })
	f := &h.a.workstreams.form
	f.name, f.workspace, f.goal = "Payment migration", "/Users/dev/pay", "Move payments to gRPC."

	h.click("Same as orchestrator")
	h.until("the agent picker", func() bool { return h.finds("Claude Opus 4.1") })
	h.click("Claude Opus 4.1")
	h.until("Opus for the agents", func() bool { return f.agentModel == "claude-opus-4-1" && !f.agentOpen })
	h.click("Claude Opus 4.1")
	h.until("its efforts", func() bool { return h.finds("Off") })
	h.click("Off")
	if f.agentEffort != "off" {
		t.Fatalf("agent effort %q", f.agentEffort)
	}
	h.click("GPT-5")
	h.until("GPT-5 for the agents", func() bool { return f.agentModel == "gpt-5" && !f.agentOpen })
	if f.agentEffort != "" {
		t.Fatalf("GPT-5 has no effort Off, yet the agents keep %q", f.agentEffort)
	}

	h.click("Auto Model")
	h.until("the orchestrator picker", func() bool { return h.finds("Claude Opus 4.1") })
	h.click("Claude Opus 4.1")
	h.until("Opus for the orchestrator", func() bool { return f.model == "claude-opus-4-1" && !f.modelOpen })
	h.click("Claude Opus 4.1")
	h.until("its efforts", func() bool { return h.finds("High") })
	h.click("High")
	f.modelOpen = false
	h.frame()

	h.click("Start workstream")
	h.until("the orchestrator's Session", func() bool { return h.a.Route().Name == "session" })
	created, _, _ := fake.calls()
	want := []gateway.NewWorkstream{{Title: "Payment migration", Workspace: "/Users/dev/pay", Goal: "Move payments to gRPC.",
		Model: "claude-opus-4-1", ReasoningEffort: "high", AgentModel: "gpt-5", AgentReasoningEffort: ""}}
	if !slices.Equal(created, want) {
		t.Fatalf("created %+v, want %+v", created, want)
	}
}

// A start the Host refuses says why, and the form stays.
func TestAWorkstreamThatDoesNotStartSaysWhy(t *testing.T) {
	fake := &fakeWorkstreams{createErr: &gateway.RefusedRequest{Reason: "The workspace is gone"}}
	h := workstreamsHarness(t, fake, nil)
	h.openNewWorkstream()
	f := &h.a.workstreams.form
	f.name, f.workspace, f.goal = "Payment migration", "/Users/dev/pay", "Move payments to gRPC."
	h.click("Start workstream")
	h.until("the notice", func() bool { return h.hasText("Could not start the workstream: The workspace is gone") })
	if r := h.a.Route(); r.Name != "workstreams" || r.Tab != newWorkstreamTab {
		t.Fatalf("went to %+v", r)
	}
}

// A Workstream's page has its facts, its agents grouped by what they do,
// the archived ones folded, and opens each agent's Session.
func TestAWorkstreamsPageGroupsItsAgents(t *testing.T) {
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}
	var opened []string
	var mu sync.Mutex
	h := workstreamsHarness(t, fake, func(cfg *Config) {
		cfg.OpenPath = func(p string) error { mu.Lock(); opened = append(opened, p); mu.Unlock(); return nil }
	})
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	h.notifyState("agent-1", "waiting_for_tool_confirmation")
	h.notifyState("agent-2", "thinking")
	h.click("Workstreams")
	h.until("the list", func() bool { return h.finds("Payment migration, 2 agents, 1 waiting on you") })
	h.shot("workstreams")
	h.click("Payment migration")
	h.until("the page", func() bool { return h.hasText("Shared folder") && h.hasText("Waiting on you") })
	for _, want := range []string{"/Users/dev/pay", "/Users/dev/droi-data/workstreams/ws-1/shared", "Same as orchestrator",
		"Needs approval", "droi/grpc-endpoints", "Finished · 5m", "Client switch"} {
		if !h.hasText(want) {
			t.Errorf("no %q in %q", want, h.tt.Texts())
		}
	}
	for _, want := range []string{"gRPC endpoints, Waiting on you, Needs approval", "Client switch, Working"} {
		if !h.finds(want) {
			t.Errorf("no row named %q", want)
		}
	}
	if h.hasText("Old cleanup") {
		t.Fatal("an archived agent shows before its group is opened")
	}
	h.click("Archived")
	h.until("the archived agent", func() bool { return h.finds("Old cleanup, Archived") })
	h.shot("workstream")

	h.click("Open shared folder")
	h.until("the shared folder opened", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return slices.Equal(opened, []string{"/Users/dev/droi-data/workstreams/ws-1/shared"})
	})
	h.click("gRPC endpoints")
	if r := h.a.Route(); r.Name != "session" || r.SessionID != "agent-1" {
		t.Fatalf("went to %+v", r)
	}
}

// An archived Workstream is folded in the list, and its page shows its
// agents, which open, without the actions that change it; an opened
// archived agent, which the list leaves out, links back to it.
func TestAnArchivedWorkstream(t *testing.T) {
	at := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	old := gateway.Workstream{ID: "ws-0", Title: "Old migration", Workspace: "/Users/dev/pay", SharedFolder: "/Users/dev/droi-data/workstreams/ws-0/shared",
		OrchestratorID: "orch-0", CreatedAt: at, ArchivedAt: at,
		Agents: []gateway.WorkstreamAgent{{SessionID: "agent-0", Title: "Old agent", CreatedAt: at, ArchivedAt: at}}}
	sc := workstreamsScenario()
	agentTags := []map[string]any{workstreamTag("ws-0", sessions.WorkstreamAgent, "orch-0")}
	sc.Sessions = append(sc.Sessions,
		fakedaemon.SessionSpec{Title: "Old lead", Cwd: "/Users/dev/pay", Messages: []fakedaemon.Message{{Role: "user", Text: "Go on"}},
			Extra: map[string]any{"sessionId": "orch-0", "archivedAt": at, "tags": []map[string]any{workstreamTag("ws-0", sessions.WorkstreamOrchestrator, "")}}},
		fakedaemon.SessionSpec{Title: "Old agent", Cwd: "/Users/dev/pay", Messages: []fakedaemon.Message{{Role: "user", Text: "Clean it up"}},
			Extra: map[string]any{"sessionId": "agent-0", "archivedAt": at, "tags": agentTags, "settings": map[string]any{"tags": agentTags}}})
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream(), old}}
	h := workstreamsHarnessOf(t, sc, fake, nil)
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	h.click("Workstreams")
	h.until("the list", func() bool { return h.hasText("Payment migration") && h.finds("Archived") })
	if h.hasText("Old migration") {
		t.Fatal("an archived Workstream shows before its section is opened")
	}
	h.click("Archived")
	h.until("the archived Workstream", func() bool { return h.finds("Old migration, 1 agent") })
	h.click("Old migration")
	h.until("its page", func() bool { return h.finds("Old agent, Archived") })
	if h.finds("More actions") {
		t.Fatal("an archived Workstream offers to archive it")
	}
	if !h.finds("Open orchestrator") {
		t.Fatal("no Open orchestrator")
	}
	h.shot("workstream-archived")
	h.click("Old agent")
	if r := h.a.Route(); r.Name != "session" || r.SessionID != "agent-0" {
		t.Fatalf("went to %+v", r)
	}
	h.until("the link", func() bool { return h.finds("Workstream: Old migration") })
	h.click("Workstream: Old migration")
	if r := h.a.Route(); r.Name != "workstreams" || r.Workstream != "ws-0" {
		t.Fatalf("went to %+v", r)
	}
}

// An agent and an orchestrator handed off to new Sessions are those
// Sessions, though the Host's records still name the old ones.
func TestAHandedOffWorkstreamAgentAndOrchestratorAreTheirNewSessions(t *testing.T) {
	sc := workstreamsScenario()
	say := []fakedaemon.Message{{Role: "user", Text: "Go on"}}
	sc.Sessions = append(sc.Sessions,
		fakedaemon.SessionSpec{Title: "gRPC endpoints, continued", Cwd: "/Users/dev/pay-worktrees/grpc", Messages: say,
			Extra: map[string]any{"sessionId": "agent-1b", "tags": []map[string]any{workstreamTag("ws-1", sessions.WorkstreamAgent, "orch-1"), continues("agent-1")}}},
		fakedaemon.SessionSpec{Title: "Payment migration lead, continued", Cwd: "/Users/dev/pay", Messages: say,
			Extra: map[string]any{"sessionId": "orch-1b", "tags": []map[string]any{workstreamTag("ws-1", sessions.WorkstreamOrchestrator, ""), continues("orch-1")}}})
	h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead, continued") })
	h.notifyState("agent-1b", "waiting_for_tool_confirmation")
	h.until("the waiting count", func() bool { return h.finds("Workstreams, 1 waiting on you") })
	if h.a.workstreams.orchWaiting["orch-1b"] != 1 {
		t.Fatalf("the new orchestrator is not lifted: %v", h.a.workstreams.orchWaiting)
	}
	h.a.Go(Route{Name: "workstreams", Workstream: "ws-1"})
	h.until("the agent's row", func() bool { return h.finds("gRPC endpoints, Waiting on you, Needs approval") })
	h.click("gRPC endpoints")
	if r := h.a.Route(); r.SessionID != "agent-1b" {
		t.Fatalf("went to %+v", r)
	}
	h.a.Go(Route{Name: "workstreams", Workstream: "ws-1"})
	h.until("the page", func() bool { return h.finds("Open orchestrator") })
	h.click("Open orchestrator")
	if r := h.a.Route(); r.SessionID != "orch-1b" {
		t.Fatalf("went to %+v", r)
	}
}

// An orchestrator whose agent waits counts as needing input in the sidebar,
// and as working while one works: it shows, though older than the recent
// ones, and a folded Workspace counts it.
func TestAWorkstreamOrchestratorTakesItsAgentsState(t *testing.T) {
	sc := workstreamsScenario()
	sc.Sessions[1].Extra["updatedAt"] = time.Now().Add(-5 * 24 * time.Hour).Unix()
	h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.until("the older orchestrator folded away", func() bool { return h.hasText("Show 1 older") && !h.hasText("Payment migration lead") })
	h.until("the records", func() bool { return h.a.workstreams.list != nil })

	shows := func(text string) bool { return slices.Contains(h.tt.Texts(), text) }
	// The Workstreams row's mark is named so once; the orchestrator row's
	// status is named and says so.
	times := func(text string) int {
		n := 0
		for _, x := range h.tt.Texts() {
			if x == text {
				n++
			}
		}
		return n
	}
	h.notifyState("agent-1", "waiting_for_tool_confirmation")
	h.until("the orchestrator lifted by its waiting agent", func() bool {
		return h.hasText("Payment migration lead") && times("1 waiting on you") == 3
	})
	if h.hasText("Needs input") {
		t.Fatal("the orchestrator, which waits on nothing itself, says Needs input")
	}
	h.shot("workstream-sidebar-lifted")
	busy := h.a.activity(nil)
	h.a.liftOrchestrators(busy)
	if n, ok := activity.CountBusy([]sessions.Summary{{SessionID: "orch-1"}}, busy, nil); !ok || n != (activity.Busy{NeedsInput: 1}) {
		t.Fatalf("a folded Workspace counts %+v", n)
	}

	// Its own state wins.
	h.notifyState("orch-1", "thinking")
	h.until("the orchestrator working itself", func() bool { return shows("Working") && times("1 waiting on you") == 1 })
	h.notifyState("orch-1", "idle")

	h.notifyState("agent-1", "idle")
	h.notifyState("agent-2", "thinking")
	h.until("the orchestrator lifted by its working agent", func() bool {
		return h.hasText("Payment migration lead") && shows("1 agent working") && !shows("Working") && times("1 waiting on you") == 0
	})
	h.notifyState("agent-2", "idle")
	// It stays in view, unread since it worked itself, but no longer lifted.
	h.until("the orchestrator no longer lifted", func() bool { return !shows("1 agent working") && !shows("Working") })
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

// An agent this window has not loaded waits on a question as long as the
// Daemon counts one: the count goes as the agent works again, and is read
// again once it rests.
func TestPromptsTheDaemonCountsForAnUnloadedWorkstreamAgent(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	opened := &openedSessions{questions: map[string]int64{"agent-1": 1}}
	h.a.workstreams.openedSessions = opened.read
	h.a.loadPending(true)
	h.a.Go(Route{Name: "workstreams", Workstream: "ws-1"})
	h.until("the question", func() bool {
		return h.finds("gRPC endpoints, Waiting on you, Has a question") && h.a.workstreams.waiting == 1
	})

	// Answered on the phone: the agent works on.
	opened.set(nil)
	h.notifyState("agent-1", "thinking")
	h.until("no question", func() bool { return h.finds("gRPC endpoints, Working") && h.a.workstreams.waiting == 0 })
	reads := opened.count()
	h.notifyState("agent-1", "idle")
	h.until("a read as it rests", func() bool { return opened.count() > reads })
	h.until("still no question", func() bool { return h.finds("gRPC endpoints, Idle") && h.a.workstreams.waiting == 0 })

	// Asked during a turn: the Daemon counts it once the agent rests.
	h.notifyState("agent-2", "thinking")
	h.until("the agent working", func() bool { return h.finds("Client switch, Working") })
	opened.set(map[string]int64{"agent-2": 1})
	h.notifyState("agent-2", "idle")
	h.until("its question", func() bool {
		return h.finds("Client switch, Waiting on you, Has a question") && h.finds("Workstreams, 1 waiting on you")
	})
}

// Changing the agent model goes to the Host, which moves the agents.
func TestChangingAWorkstreamsAgentModel(t *testing.T) {
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}
	h := workstreamsHarness(t, fake, nil)
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	h.a.Go(Route{Name: "workstreams", Workstream: "ws-1"})
	h.until("the page", func() bool { return h.hasText("Shared folder") })
	h.until("the models", func() bool { dv := h.a.sessionDefaults(); return dv != nil && len(dv.Models) > 0 })
	h.click("Same as orchestrator")
	h.until("the picker", func() bool { return h.finds("GPT-5") })
	h.click("GPT-5")
	h.until("the saved notice", func() bool { return h.hasText("Saved. Agents switch before their next turn.") })
	_, settings, _ := fake.calls()
	if want := []gateway.WorkstreamSettings{{ID: "ws-1", AgentModel: "gpt-5"}}; !slices.Equal(settings, want) {
		t.Fatalf("settings %+v, want %+v", settings, want)
	}
}

// Archiving asks first, then lists the worktrees the Host kept.
func TestArchivingAWorkstreamSaysWhichWorktreesStay(t *testing.T) {
	for _, tc := range []struct {
		kept   []string
		notice string
	}{
		{[]string{"/Users/dev/pay-worktrees/grpc"}, "Archived. Kept 1 worktree with changes: /Users/dev/pay-worktrees/grpc"},
		{[]string{"/w/grpc", "/w/clients"}, "Archived. Kept 2 worktrees with changes: /w/grpc, /w/clients"},
		{nil, "Archived."},
	} {
		t.Run(tc.notice, func(t *testing.T) {
			fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}, kept: tc.kept}
			h := workstreamsHarness(t, fake, nil)
			h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
			h.a.Go(Route{Name: "workstreams", Workstream: "ws-1"})
			h.until("the page", func() bool { return h.hasText("Shared folder") })
			h.click("More actions")
			h.click("Archive workstream…")
			h.until("the dialog", func() bool { return h.hasText("Archive “Payment migration”?") })
			if !h.hasText("Worktrees without changes are deleted") {
				t.Fatalf("no consequences in %q", h.tt.Texts())
			}
			h.click("Archive workstream")
			h.until("the notice", func() bool { return slices.Contains(h.tt.Texts(), tc.notice) })
			if _, _, archived := fake.calls(); !slices.Equal(archived, []string{"ws-1"}) {
				t.Fatalf("archived %v", archived)
			}
			h.until("the archived page", func() bool { return !h.finds("More actions") })
		})
	}
}

// A Workstream Session's header links to its Workstream's page.
func TestAWorkstreamSessionsHeaderLinksToItsWorkstream(t *testing.T) {
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}
	h := workstreamsHarness(t, fake, nil)
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	h.a.Go(Route{Name: "session", SessionID: "agent-1"})
	h.until("the link", func() bool { return h.finds("Workstream: Payment migration") })
	h.click("Workstream: Payment migration")
	if r := h.a.Route(); r.Name != "workstreams" || r.Workstream != "ws-1" {
		t.Fatalf("went to %+v", r)
	}
	h.until("the page", func() bool { return h.hasText("Shared folder") })
}

// A followed computer's Workstreams show, without what reaches this
// computer's folders, and its Daemon says whether a folder there is a git
// repository.
func TestAFollowedComputersWorkstreams(t *testing.T) {
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}
	o := startOtherComputerWith(t, workstreamsScenario(), "", func(g *gateway.Options) { g.Workstreams = fake })
	h := followHarness(t, localSessions(), o.paired(), nil, func(cfg *Config) {
		cfg.OpenPath = func(string) error { return nil }
	})
	closeTransport(t, h)
	h.until("the other computer's Sessions", func() bool { return h.hasText("Payment migration lead") })
	h.a.Go(Route{Name: "workstreams", Workstream: "ws-1"})
	h.until("the page", func() bool { return h.hasText("Shared folder") && h.finds("Open orchestrator") })
	if h.finds("Open shared folder") {
		t.Fatal("the window offers to open a folder on another computer")
	}
	h.a.Go(Route{Name: "workstreams", Tab: newWorkstreamTab})
	h.until("the form", func() bool { return h.finds("Workstream name") })
	if h.finds("Choose a workspace") {
		t.Fatal("the window offers this computer's folders")
	}
	h.a.workstreams.form.workspace = "/Users/dev/pay"
	h.until("the note", func() bool { return h.hasText(notGitNote) })
}

// A Host without Workstreams, an older Droi, says so.
func TestAnOlderHostSaysItHasNoWorkstreams(t *testing.T) {
	h := newHarness(t, workstreamsScenario(), "")
	closeTransport(t, h)
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("Workstreams")
	h.until("the older Host", func() bool {
		return h.hasText("This computer runs an older Droi without workstreams. Update Droi there.")
	})
}

// A clock the test moves: the real time, plus what it was moved by.
type movedClock struct{ by atomic.Int64 }

func (m *movedClock) now() time.Time { return time.Now().Add(time.Duration(m.by.Load())) }

func (m *movedClock) move(d time.Duration) { m.by.Add(int64(d)) }

// While a waiting count comes from the Daemon's list of opened Sessions, the
// window reads the list again once the refresh time has passed, on any page.
func TestAWorkstreamAgentsListedQuestionIsReadAgainInTime(t *testing.T) {
	clock := &movedClock{}
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, func(cfg *Config) { cfg.Now = clock.now })
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	opened := &openedSessions{questions: map[string]int64{"agent-1": 1}}
	h.a.workstreams.openedSessions = opened.read
	h.a.loadPending(true)
	h.until("the question counted", func() bool { return h.finds("Workstreams, 1 waiting on you") })
	if r := h.a.Route(); r.Name != "home" && r.Name != "session" {
		t.Fatalf("on %+v", r)
	}
	reads := opened.count()
	for range 50 {
		h.frame()
	}
	if got := opened.count(); got != reads {
		t.Fatalf("%d reads before the refresh time, want %d", got, reads)
	}
	clock.move(31 * time.Second)
	h.until("one more read", func() bool { return opened.count() == reads+1 })
	for range 50 {
		h.frame()
		time.Sleep(2 * time.Millisecond)
	}
	if got := opened.count(); got != reads+1 {
		t.Fatalf("%d reads after the refresh, want %d", got, reads+1)
	}
}

// A workspace the Daemon could not use when typed is asked about again at
// Start, which goes on; a folder made a repository after the note is asked
// about again once the form opens again.
func TestAWorkstreamWorkspaceCheckedBeforeIsAskedAgain(t *testing.T) {
	fake := &fakeWorkstreams{made: gateway.WorkstreamCreated{WorkstreamID: "ws-2", OrchestratorID: "orch-1"}}
	h := workstreamsHarness(t, fake, nil)
	h.openNewWorkstream()
	f := &h.a.workstreams.form
	f.name, f.goal, f.workspace = "Payment migration", "Move payments to gRPC.", "/Users/dev/later"
	h.until("the Daemon's answer", func() bool { return h.hasText("Directory does not exist: /Users/dev/later") })
	validations := h.count("daemon.validate_working_directory")
	h.click("Start workstream")
	h.until("the orchestrator's Session", func() bool { return h.a.Route().Name == "session" })
	if created, _, _ := fake.calls(); len(created) != 1 || created[0].Workspace != "/Users/dev/later" {
		t.Fatalf("created %+v", created)
	}
	h.until("the folder asked about again", func() bool { return h.count("daemon.validate_working_directory") > validations })

	h.openNewWorkstream()
	f.name, f.goal, f.workspace = "Payment migration", "Move payments to gRPC.", "/Users/dev/pay"
	h.until("the note", func() bool { return h.hasText(notGitNote) })
	lists := h.count("daemon.list_git_branches")
	h.click("Cancel")
	h.openNewWorkstream()
	if f.workspace != "" || len(h.a.workstreams.checks) != 0 {
		t.Fatalf("the form opens again with %q and %d answers", f.workspace, len(h.a.workstreams.checks))
	}
	f.name, f.goal, f.workspace = "Payment migration", "Move payments to gRPC.", "/Users/dev/pay"
	h.until("the folder asked about again", func() bool { return h.count("daemon.list_git_branches") > lists })
	h.click("Start workstream")
	h.until("the second Workstream", func() bool { created, _, _ := fake.calls(); return len(created) == 2 })
}

// A Daemon that cannot be asked leaves no answer behind: the form asks
// again once it is back.
func TestAWorkstreamWorkspaceIsAskedAgainOnceTheDaemonIsBack(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{}, nil)
	h.openNewWorkstream()
	if err := h.d.GoDown(); err != nil {
		t.Fatal(err)
	}
	h.until("the Daemon gone", func() bool { return !h.a.ctl.Status().Connected })
	h.a.workstreams.form.workspace = "/Users/dev/pay"
	h.until("an attempt", func() bool { return h.a.workstreams.typedAt.After(time.Now().Add(time.Second)) })
	if _, ok := h.a.workstreams.checks["/Users/dev/pay"]; ok {
		t.Fatal("an answer kept from a Daemon that could not be asked")
	}
	if err := h.d.ComeBack(); err != nil {
		t.Fatal(err)
	}
	h.until("the note", func() bool { return h.hasText(notGitNote) })
}

// An archive the Host's bound cut short says how to finish it.
func TestAWorkstreamArchiveCutShortSaysHowToFinish(t *testing.T) {
	// A Gateway between the window and the Host's answers 504 to an
	// archive, as the Host's does when its bound cuts one short.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	target, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == gateway.WorkstreamArchivePath {
			w.WriteHeader(http.StatusGatewayTimeout)
			_, _ = w.Write([]byte(`{"error":"Archiving took too long and stopped partway."}`))
			return
		}
		httputil.NewSingleHostReverseProxy(target).ServeHTTP(w, r)
	}))
	t.Cleanup(proxy.Close)
	var token string
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}
	h := startHarnessGateway(t, workstreamsScenario(), "", "", func(o *gateway.Options) {
		o.Workstreams, o.Port, token = fake, port, o.LocalToken()
	}, func(cfg *Config) {
		socket, err := gateway.SocketURL(proxy.URL, token)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Gateway, err = gateway.NewClient(socket); err != nil {
			t.Fatal(err)
		}
	})
	closeTransport(t, h)
	if h.gw.Port() != port {
		t.Skipf("the Gateway could not take port %d", port)
	}
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	h.a.Go(Route{Name: "workstreams", Workstream: "ws-1"})
	h.until("the page", func() bool { return h.hasText("Shared folder") })
	h.click("More actions")
	h.click("Archive workstream…")
	h.click("Archive workstream")
	h.until("the notice", func() bool {
		return h.hasText("Archiving took too long, and some agents are not archived yet. Archive the workstream again to finish.")
	})
}

// A deadline the window's own call runs into reads as an archive cut short.
func TestAWorkstreamArchiveFailureReads(t *testing.T) {
	for err, want := range map[error]string{
		gateway.ErrWorkstreamArchiveUnfinished: "Archiving took too long, and some agents are not archived yet. Archive the workstream again to finish.",
		context.DeadlineExceeded:               "Archiving took too long, and some agents are not archived yet. Archive the workstream again to finish.",
		errors.New("the Daemon is gone"):       "Could not archive the workstream: the Daemon is gone",
	} {
		if got := archiveFailed(err); got != want {
			t.Errorf("%v reads %q, want %q", err, got, want)
		}
	}
}
