package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
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
	// made is what Create answers; kept what Archive answers.
	made gateway.WorkstreamCreated
	kept []string
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

func workstreamTag(ws, role, orchestrator string) []map[string]any {
	meta := map[string]string{"workstreamId": ws, "role": role}
	if orchestrator != "" {
		meta["orchestratorId"] = orchestrator
	}
	return []map[string]any{{"name": sessions.WorkstreamTag, "metadata": meta}}
}

// workstreamsScenario has a Session of no Workstream, and Workstream ws-1's
// orchestrator and two agents.
func workstreamsScenario() fakedaemon.Scenario {
	say := []fakedaemon.Message{{Role: "user", Text: "Go on"}}
	return fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{
		{Title: "Fix the login race", Cwd: "/Users/dev/acme-web", Messages: say},
		{Title: "Payment migration lead", Cwd: "/Users/dev/pay", Messages: say,
			Extra: map[string]any{"sessionId": "orch-1", "tags": workstreamTag("ws-1", sessions.WorkstreamOrchestrator, "")}},
		{Title: "gRPC endpoints", Cwd: "/Users/dev/pay-worktrees/grpc", Messages: say,
			Extra: map[string]any{"sessionId": "agent-1", "tags": workstreamTag("ws-1", sessions.WorkstreamAgent, "orch-1")}},
		{Title: "Client switch", Cwd: "/Users/dev/pay-worktrees/clients", Messages: say,
			Extra: map[string]any{"sessionId": "agent-2", "tags": workstreamTag("ws-1", sessions.WorkstreamAgent, "orch-1")}},
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

// workstreamsHarness is the window with a Host whose Workstreams are fake.
func workstreamsHarness(t *testing.T, fake *fakeWorkstreams, with func(*Config)) *harness {
	t.Helper()
	h := startHarnessGateway(t, workstreamsScenario(), "", "", func(o *gateway.Options) { o.Workstreams = fake }, with)
	// The Gateway Client's requests share http.DefaultTransport, which can
	// leave a connection dialed but never used; the Gateway's shutdown waits
	// seconds for one. Before the harness's own cleanup.
	t.Cleanup(func() {
		h.a.Close()
		http.DefaultTransport.(*http.Transport).CloseIdleConnections()
	})
	return h
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
	if _, ok := h.tt.Find("Workstreams"); !ok {
		t.Fatal("no Workstreams row")
	}
	if _, ok := h.tt.Find("1 waiting on you"); ok {
		t.Fatal("a waiting count before any agent waits")
	}
	if err := h.d.Notify("agent-1", map[string]any{"type": "droid_working_state_changed", "newState": "waiting_for_tool_confirmation"}); err != nil {
		t.Fatal(err)
	}
	h.until("the waiting count", func() bool { _, ok := h.tt.Find("1 waiting on you"); return ok })
	if err := h.d.Notify("agent-1", map[string]any{"type": "droid_working_state_changed", "newState": "idle"}); err != nil {
		t.Fatal(err)
	}
	h.until("no waiting count", func() bool { _, ok := h.tt.Find("1 waiting on you"); return !ok })
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

// An agent waits on a permission request, or on a question, which the
// Daemon counts for a Session the window has not loaded.
func TestHowAnAgentWaits(t *testing.T) {
	a := &App{}
	a.workstreams.busy = map[string]activity.Activity{"asks": activity.NeedsInput, "asked": activity.NeedsInput, "works": activity.Working}
	a.workstreams.pending = map[string]pendingPrompts{"asked": {questions: 1}, "idle-asked": {questions: 1}, "works": {questions: 1}}
	for id, want := range map[string]string{"asks": waitsApproval, "asked": waitsQuestion, "idle-asked": waitsQuestion, "works": "", "rests": ""} {
		if got := a.waitsOn(id); got != want {
			t.Errorf("%s waits on %q, want %q", id, got, want)
		}
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
	h.until("the form", func() bool { _, ok := h.tt.Find("Workstream name"); return ok })
}

// The form asks for a name, a workspace and a goal, says when the workspace
// is no git repository, then starts the Workstream with the agents on the
// orchestrator's model and opens the orchestrator's Session.
func TestStartingAWorkstream(t *testing.T) {
	fake := &fakeWorkstreams{made: gateway.WorkstreamCreated{WorkstreamID: "ws-2", OrchestratorID: "orch-1"}}
	h := workstreamsHarness(t, fake, nil)
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("Workstreams")
	h.until("the empty page", func() bool { return h.hasText("No workstreams yet") })
	h.click("New workstream")
	h.until("the form", func() bool { _, ok := h.tt.Find("Workstream name"); return ok })
	if !h.hasText("Same as orchestrator") {
		t.Fatalf("the agent model is not the orchestrator's: %q", h.tt.Texts())
	}
	h.click("Start workstream")
	h.until("the name error", func() bool { return h.hasText("Enter a name.") })
	h.click("Workstream name")
	h.tt.Type("Payment migration")
	h.frame()
	h.click("Start workstream")
	h.until("the workspace error", func() bool { return h.hasText("Choose a workspace.") })

	plain := t.TempDir()
	h.click("Workstream workspace")
	h.tt.Type(plain)
	h.frame()
	h.until("the note", func() bool { return h.hasText("This folder is not a git repository") })
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.a.workstreams.form.workspace = repo
	h.frame()
	if h.hasText("This folder is not a git repository") {
		t.Fatal("a repository is said to be none")
	}

	h.click("Start workstream")
	h.until("the goal error", func() bool { return h.hasText("Enter the goal.") })
	h.shot("workstream-new")
	h.click("Workstream goal")
	h.tt.Type("Move payments to gRPC.")
	h.frame()
	h.click("Start workstream")
	h.until("the orchestrator's Session", func() bool { r := h.a.Route(); return r.Name == "session" && r.SessionID == "orch-1" })
	created, _, _ := fake.calls()
	if len(created) != 1 {
		t.Fatalf("created %+v", created)
	}
	want := gateway.NewWorkstream{Title: "Payment migration", Workspace: repo, Goal: "Move payments to gRPC."}
	if created[0] != want {
		t.Fatalf("created %+v, want %+v", created[0], want)
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
	if err := h.d.Notify("agent-1", map[string]any{"type": "droid_working_state_changed", "newState": "waiting_for_tool_confirmation"}); err != nil {
		t.Fatal(err)
	}
	if err := h.d.Notify("agent-2", map[string]any{"type": "droid_working_state_changed", "newState": "thinking"}); err != nil {
		t.Fatal(err)
	}
	h.click("Workstreams")
	h.until("the list", func() bool { return h.hasText("Payment migration") && h.hasText("2 agents") })
	h.until("the row's waiting count", func() bool { _, ok := h.tt.Find("1 waiting on you"); return ok })
	h.shot("workstreams")
	h.click("Payment migration")
	h.until("the page", func() bool { return h.hasText("Shared folder") && h.hasText("Waiting on you") })
	for _, want := range []string{"/Users/dev/pay", "/Users/dev/droi-data/workstreams/ws-1/shared", "Same as orchestrator",
		"Needs approval", "droi/grpc-endpoints", "Done · 5m", "Working", "Client switch"} {
		if !h.hasText(want) {
			t.Errorf("no %q in %q", want, h.tt.Texts())
		}
	}
	if h.hasText("Old cleanup") {
		t.Fatal("an archived agent shows before its group is opened")
	}
	h.click("Archived")
	h.until("the archived agent", func() bool { return h.hasText("Old cleanup") })
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

// Changing the agent model goes to the Host, which moves the agents.
func TestChangingAWorkstreamsAgentModel(t *testing.T) {
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}
	h := workstreamsHarness(t, fake, nil)
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	h.a.Go(Route{Name: "workstreams", Workstream: "ws-1"})
	h.until("the page", func() bool { return h.hasText("Shared folder") })
	h.until("the models", func() bool { dv := h.a.sessionDefaults(); return dv != nil && len(dv.Models) > 0 })
	h.click("Same as orchestrator")
	h.until("the picker", func() bool { _, ok := h.tt.Find("GPT-5"); return ok })
	h.click("GPT-5")
	h.until("the saved notice", func() bool { return h.hasText("Saved. Agents switch before their next turn.") })
	_, settings, _ := fake.calls()
	if want := []gateway.WorkstreamSettings{{ID: "ws-1", AgentModel: "gpt-5"}}; !slices.Equal(settings, want) {
		t.Fatalf("settings %+v, want %+v", settings, want)
	}
}

// Archiving asks first, then lists the worktrees the Host kept.
func TestArchivingAWorkstreamSaysWhichWorktreesStay(t *testing.T) {
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}, kept: []string{"/Users/dev/pay-worktrees/grpc"}}
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
	h.until("the notice", func() bool {
		return h.hasText("Archived. Kept 1 worktrees with changes: /Users/dev/pay-worktrees/grpc")
	})
	if _, _, archived := fake.calls(); !slices.Equal(archived, []string{"ws-1"}) {
		t.Fatalf("archived %v", archived)
	}
	h.until("the archived page", func() bool { _, ok := h.tt.Find("More actions"); return !ok })
}

// A Workstream Session's header links to its Workstream's page.
func TestAWorkstreamSessionsHeaderLinksToItsWorkstream(t *testing.T) {
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}
	h := workstreamsHarness(t, fake, nil)
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	h.a.Go(Route{Name: "session", SessionID: "agent-1"})
	h.until("the link", func() bool { _, ok := h.tt.Find("Workstream: Payment migration"); return ok })
	h.click("Workstream: Payment migration")
	if r := h.a.Route(); r.Name != "workstreams" || r.Workstream != "ws-1" {
		t.Fatalf("went to %+v", r)
	}
	h.until("the page", func() bool { return h.hasText("Shared folder") })
}

// A Host without Workstreams, an older Droi, says so.
func TestAnOlderHostSaysItHasNoWorkstreams(t *testing.T) {
	h := newHarness(t, workstreamsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("Workstreams")
	h.until("the older Host", func() bool {
		return h.hasText("This computer runs an older Droi without workstreams. Update Droi there.")
	})
}
