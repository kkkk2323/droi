package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/activity"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

func (h *harness) textCount(text string) int {
	n := 0
	for _, s := range h.tt.Texts() {
		if s == text {
			n++
		}
	}
	return n
}

// indexOf is the place of the first text on screen equal to text, -1 for
// none.
func (h *harness) indexOfText(text string) int {
	for i, s := range h.tt.Texts() {
		if s == text {
			return i
		}
	}
	return -1
}

// tabMenu chooses item from the menu of the tab of an agent titled title,
// which it finds by the tab's close button.
func (h *harness) tabMenu(title, item string) {
	h.t.Helper()
	r, ok := h.tt.Find("Close " + title)
	if !ok {
		h.t.Fatalf("no tab %q in %q", title, h.tt.Texts())
	}
	h.tt.RightClickAt(r.X-r.W*2, r.Y+r.H/2)
	h.frame()
	if err := h.tt.ChooseMenuItem(item); err != nil {
		h.t.Fatal(err)
	}
	h.frame()
}

// openOrchestrator opens the page of Workstream ws-1.
func (h *harness) openOrchestrator() {
	h.t.Helper()
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	h.a.Go(Route{Name: "session", SessionID: "orch-1"})
	h.until("the page", func() bool { return h.finds("Workstream tabs") })
}

// The sidebar lists a Workstream in a group of its own above the Workspaces,
// once, without its agents, under a title that starts another.
func TestTheSidebarListsWorkstreamsAboveTheWorkspaces(t *testing.T) {
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}
	h := workstreamsHarness(t, fake, nil)
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") && h.hasText("Payment migration lead") })
	if h.hasText("gRPC endpoints") || h.hasText("Client switch") {
		t.Fatalf("an agent is in the sidebar: %q", h.tt.Texts())
	}
	// A row is its name and its text.
	if n := h.textCount("Payment migration lead"); n != 2 {
		t.Fatalf("the orchestrator shows %d times: %q", n, h.tt.Texts())
	}
	for _, want := range []string{"Workstreams", "Beta", "New Workstream"} {
		if !h.hasText(want) && !h.finds(want) {
			t.Errorf("no %q in %q", want, h.tt.Texts())
		}
	}
	if !(h.indexOfText("Workstreams") < h.indexOfText("Payment migration lead") && h.indexOfText("Payment migration lead") < h.indexOfText("acme-web")) {
		t.Fatalf("the Workstreams are not above the Workspaces: %q", h.tt.Texts())
	}
	if h.finds("Workstreams, 1 waiting on you") {
		t.Fatal("the old top row is still there")
	}
}

// A Workstream's row says how many of its agents wait on the user or work.
func TestAWorkstreamRowSaysWhatItsAgentsDo(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.until("the records", func() bool { return h.hasText("Payment migration lead") && h.a.workstreams.list != nil })
	h.notifyState("agent-1", "waiting_for_tool_confirmation")
	h.until("the waiting count", func() bool { return h.shows("1 waiting on you") })
	h.notifyState("agent-1", "idle")
	h.notifyState("agent-2", "thinking")
	h.until("the working count", func() bool { return h.shows("1 agent working") && !h.shows("1 waiting on you") })
	h.notifyState("agent-2", "idle")
	h.until("nothing said", func() bool { return !h.shows("1 agent working") })
	// Its own state wins.
	h.notifyState("agent-2", "thinking")
	h.until("the agent counted", func() bool { return h.shows("1 agent working") })
	h.notifyState("orch-1", "thinking")
	h.until("the orchestrator working itself", func() bool { return h.shows("Working") && !h.shows("1 agent working") })
}

// With no Workstream the group offers to start one, and so does its title.
func TestAnEmptyWorkstreamsGroupOffersTheFirst(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{}, func(cfg *Config) {})
	h.a.workstreams.list = []gateway.Workstream{}
	sc := fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Fix the login race", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "Go on"}}}}}
	h = workstreamsHarnessOf(t, sc, &fakeWorkstreams{}, nil)
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	if !h.hasText("Beta") {
		t.Fatalf("no Workstreams title: %q", h.tt.Texts())
	}
	h.click("New Workstream")
	h.until("the dialog", func() bool { return h.finds("Workstream name") && h.finds("Choose a workspace") })
}

func TestWorkstreamLabelsAreWorkstreamsInChinese(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, func(cfg *Config) { prefs.Language.Set(cfg.Prefs, string(l10n.Chinese)) })
	t.Cleanup(func() { l10n.Set("", "") })
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	h.notifyState("agent-1", "waiting_for_tool_confirmation")
	h.until("the row's status", func() bool { return h.shows("1 个等你处理") })
	if !h.shows("Workstreams") || !h.shows("新建 Workstream") && !h.finds("新建 Workstream") {
		t.Fatalf("the title is not Workstreams: %q", h.tt.Texts())
	}
	h.notifyState("agent-1", "idle")
	h.notifyState("agent-2", "thinking")
	h.until("the working status", func() bool { return h.shows("1 个 Agent 运行中") })
}

// Search finds no agent, by title or by what it says; an orchestrator stays.
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

// An orchestrator's page shows the agents in a side panel, grouped, each
// with what it does and no old report; the archived ones stay folded.
func TestAnOrchestratorPageListsItsAgentsInASidePanel(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.notifyState("agent-1", "waiting_for_tool_confirmation")
	h.notifyState("agent-2", "thinking")
	h.until("the groups", func() bool {
		return h.hasText("Waiting on you") && h.hasText("Working") && h.finds("gRPC endpoints, Needs your approval")
	})
	if !h.finds("Client switch, Working") {
		t.Fatalf("no working row: %q", h.tt.Texts())
	}
	if h.hasText("Finished") || h.hasText("Idle") {
		t.Fatalf("an old report, or an empty group: %q", h.tt.Texts())
	}
	if h.hasText("Old cleanup") {
		t.Fatal("an archived agent shows before its group is opened")
	}
	h.click("Archived")
	h.until("the archived agent", func() bool { return h.finds("Old cleanup, Archived") })
	h.shot("workstream-panel")
	h.notifyState("agent-1", "idle")
	h.notifyState("agent-2", "idle")
	h.until("the idle group", func() bool {
		return h.hasText("Idle") && h.finds("gRPC endpoints, Idle") && h.finds("Client switch, Idle")
	})
	if h.hasText("Finished") {
		t.Fatalf("an idle agent shows its report: %q", h.tt.Texts())
	}
}

// A working agent never shows the report it made before the orchestrator
// gave it new work.
func TestAWorkingAgentDoesNotShowItsOldReport(t *testing.T) {
	w := paymentWorkstream()
	w.Agents[0].Awaiting = true
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
	h.openOrchestrator()
	h.notifyState("agent-1", "thinking")
	h.until("the row", func() bool { return h.finds("gRPC endpoints, Working") })
	for _, s := range h.tt.Texts() {
		if strings.Contains(s, "Finished") || strings.Contains(s, "5m") {
			t.Fatalf("the old report shows in %q", h.tt.Texts())
		}
	}
}

// A click on an agent opens its Session in a tab of the page, where the
// composer talks to the agent; closing the tab goes back.
func TestAnAgentOpensInATabOfTheOrchestratorPage(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.until("the agent's row", func() bool { return h.finds("gRPC endpoints, Idle") })
	h.click("gRPC endpoints, Idle")
	h.until("the tab", func() bool { return h.finds("Close gRPC endpoints") })
	if r := h.a.Route(); r.SessionID != "orch-1" {
		t.Fatalf("left the page: %+v", r)
	}
	st := h.a.wsState("ws-1")
	if st.Active != "agent-1" || len(st.Tabs) != 1 {
		t.Fatalf("state %+v", st)
	}
	h.until("the agent's Session", func() bool {
		return h.a.loaded("agent-1") && h.a.view("agent-1").workstreamPlaceholder() == "Message this agent…"
	})
	if h.finds("Orchestrator") {
		t.Fatal("the orchestrator's mark is on an agent's composer")
	}
	h.shot("workstream-agent-tab")
	h.click("Close gRPC endpoints")
	h.until("the orchestrator back", func() bool {
		return h.a.view("orch-1").workstreamPlaceholder() == "Message the orchestrator…" && h.finds("Orchestrator")
	})
	if st := h.a.wsState("ws-1"); st.Active != "" || len(st.Tabs) != 0 {
		t.Fatalf("state %+v", st)
	}
}

// An agent's Session opened from anywhere is its Workstream's tab.
func TestAnAgentSessionOpenedFromElsewhereIsATab(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	h.a.Go(Route{Name: "session", SessionID: "agent-2"})
	if r := h.a.Route(); r.SessionID != "orch-1" {
		t.Fatalf("went to %+v", r)
	}
	h.until("the tab", func() bool { return h.finds("Close Client switch") })
	if st := h.a.wsState("ws-1"); st.Active != "agent-2" {
		t.Fatalf("state %+v", st)
	}
}

// The tabs close one by one, to the right of one, or all.
func TestAgentTabsClose(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.until("the rows", func() bool { return h.finds("gRPC endpoints, Idle") && h.finds("Client switch, Idle") })
	h.click("gRPC endpoints, Idle")
	h.click("Client switch, Idle")
	h.until("both tabs", func() bool { return h.finds("Close gRPC endpoints") && h.finds("Close Client switch") })
	if st := h.a.wsState("ws-1"); len(st.Tabs) != 2 || st.Active != "agent-2" {
		t.Fatalf("state %+v", st)
	}
	// The first tab's menu closes what is to its right.
	h.tabMenu("gRPC endpoints", "Close tabs to the right")
	h.until("one tab", func() bool { return len(h.a.wsState("ws-1").Tabs) == 1 })
	if st := h.a.wsState("ws-1"); st.Tabs[0] != "agent-1" || st.Active != "agent-1" {
		t.Fatalf("state %+v", st)
	}
	h.tabMenu("gRPC endpoints", "Close all")
	h.until("no tabs", func() bool { return len(h.a.wsState("ws-1").Tabs) == 0 && h.a.wsState("ws-1").Active == "" })
}

// The tabs, and the panel put away, are kept: a new App on the same
// preferences finds them as they were.
func TestTabsAndThePanelAreKept(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.until("the row", func() bool { return h.finds("gRPC endpoints, Idle") })
	h.click("gRPC endpoints, Idle")
	h.click("Hide panel")
	h.until("the panel gone", func() bool { return h.finds("Show panel") && !h.finds("Workstream panel") })
	st := h.a.wsState("ws-1")
	if !st.Closed || st.Active != "agent-1" {
		t.Fatalf("state %+v", st)
	}
	fresh := &App{prefs: h.a.prefs}
	if got := fresh.wsState("ws-1"); !got.Closed || got.Active != "agent-1" || len(got.Tabs) != 1 {
		t.Fatalf("not kept: %+v", got)
	}
	h.click("Show panel")
	h.until("the panel back", func() bool { return h.finds("Workstream panel") })
}

// The orchestrator's page asks the user to look at the agent that waits, and
// counts the agents at work while the panel is out of sight.
func TestTheOrchestratorPageNotesWhatWaitsAndWorks(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.until("the mark", func() bool { return h.finds("Orchestrator") })
	if h.finds("View Agent") {
		t.Fatal("an attention card before anything waits")
	}
	h.notifyState("agent-1", "waiting_for_tool_confirmation")
	h.until("the card", func() bool { return h.finds("View Agent") && h.finds("Needs your attention") })
	h.shot("workstream-attention")
	h.click("View Agent")
	h.until("the agent's tab", func() bool { return h.a.wsState("ws-1").Active == "agent-1" })
	h.a.showTab("ws-1", "")
	h.notifyState("agent-1", "idle")
	h.notifyState("agent-2", "thinking")
	// The panel lists them: no count over the composer, only the sidebar's
	// (a name and a text).
	h.until("agent working", func() bool { return h.finds("Client switch, Working") })
	if h.textCount("1 agent working") != 2 {
		t.Fatalf("the count shows with the panel open: %q", h.tt.Texts())
	}
	h.click("Hide panel")
	h.until("the count", func() bool { return h.textCount("1 agent working") == 4 })
	h.shot("workstream-working")
	// The sidebar's row has the same words: the button is the one that ends
	// the composer's top edge, at the right.
	box, ok := h.tt.Find("Message composer")
	if !ok {
		t.Fatalf("no composer: %q", h.tt.Texts())
	}
	h.tt.ClickAt(box.X+box.W-70, box.Y-20)
	h.frame()
	h.until("the panel", func() bool { return h.finds("Workstream panel") })
}

// A started Workstream with no message yet greets its user.
func TestAnEmptyOrchestratorShowsTheWelcome(t *testing.T) {
	sc := workstreamsScenario()
	sc.Sessions[1].Messages = nil
	h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.until("the welcome", func() bool { return h.hasText("Welcome to your workstream!") })
	for _, want := range []string{"Working directory", "/Users/dev/pay", "How this workstream works", "Agents work on their own", "Shared notes and plans"} {
		if !h.hasText(want) {
			t.Errorf("no %q in %q", want, h.tt.Texts())
		}
	}
	h.shot("workstream-welcome")
	h.click("Hide panel")
	h.until("the panel gone", func() bool { return h.finds("Show panel") })
	h.click("Files")
	h.until("the panel on Files", func() bool { return h.finds("Workstream panel") && h.hasText("Search files") })
}

// The dialog starts a Workstream from a name and a folder: Enter does, and no
// goal or model is asked.
func TestStartingAWorkstreamFromTheDialog(t *testing.T) {
	fake := &fakeWorkstreams{made: gateway.WorkstreamCreated{WorkstreamID: "ws-2", OrchestratorID: "orch-1"}}
	h := workstreamsHarness(t, fake, nil)
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	h.click("New Workstream")
	h.until("the dialog", func() bool { return h.finds("Workstream name") })
	for _, no := range []string{"Goal", "Orchestrator model", "Agent model"} {
		if h.hasText(no) {
			t.Fatalf("the dialog asks %q", no)
		}
	}
	h.shot("workstream-new")
	start := h.finds("Start")
	if !start {
		t.Fatal("no Start")
	}
	h.click("Workstream name")
	h.tt.Type("Payment migration")
	h.frame()
	h.tt.Key(0, ui.KeyEnter)
	h.until("the orchestrator's page", func() bool { return h.a.Route().Name == "session" && h.a.Route().SessionID == "orch-1" })
	created, _, _ := fake.calls()
	if len(created) != 1 || created[0].Title != "Payment migration" || created[0].Goal != "" || created[0].Workspace == "" {
		t.Fatalf("created %+v", created)
	}
	if h.a.workstreams.newOpen {
		t.Fatal("the dialog stays")
	}
}

// A name over 120 characters is cut, and an empty one does not start.
func TestTheDialogKeepsTheNameShortAndNeedsOne(t *testing.T) {
	fake := &fakeWorkstreams{made: gateway.WorkstreamCreated{WorkstreamID: "ws-2", OrchestratorID: "orch-1"}}
	h := workstreamsHarness(t, fake, nil)
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	h.a.openNewWorkstream()
	h.frame()
	h.a.startWorkstream()
	if h.a.workstreams.form.err == "" {
		t.Fatal("an empty name started")
	}
	h.a.workstreams.form.name = strings.Repeat("é", 150)
	h.frame()
	h.frame()
	if n := len([]rune(h.a.workstreams.form.name)); n != workstreamNameMax {
		t.Fatalf("name of %d characters", n)
	}
	if created, _, _ := fake.calls(); len(created) != 0 {
		t.Fatalf("created %+v", created)
	}
}

// Archiving is asked from the orchestrator's menu, and its dialog archives.
func TestArchivingAWorkstreamFromItsMenu(t *testing.T) {
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}, kept: []string{"/Users/dev/pay-worktrees/grpc"}}
	h := workstreamsHarness(t, fake, nil)
	h.openOrchestrator()
	h.click("Workstream menu")
	h.until("the menu", func() bool { return h.finds("Archive Workstream…") })
	h.click("Archive Workstream…")
	h.until("the dialog", func() bool { return h.finds("Archive Workstream") && h.hasText("Its agents stop and are archived") })
	h.click("Archive Workstream")
	h.until("archived", func() bool { _, _, archived := fake.calls(); return len(archived) == 1 && archived[0] == "ws-1" })
	if h.a.workstreams.archiveFor != "" {
		t.Fatal("the dialog stays")
	}
}

// Settings change the agents' model through the Host, and rename through the
// orchestrator's Session.
func TestWorkstreamSettingsSaveTheAgentModel(t *testing.T) {
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}
	h := workstreamsHarness(t, fake, nil)
	h.openOrchestrator()
	h.click("Workstream settings")
	h.until("the dialog", func() bool { return h.hasText("Models") && h.hasText("Inherit") })
	h.shot("workstream-settings")
	h.a.saveAgentModel("ws-1", "claude-sonnet-5-5", "")
	h.until("saved", func() bool {
		_, settings, _ := fake.calls()
		return len(settings) == 1 && settings[0].AgentModel == "claude-sonnet-5-5"
	})
}
