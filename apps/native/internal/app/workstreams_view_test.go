package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	h.tt.RightClickAt(r.X-r.W/2, r.Y+r.H/2)
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

// Settings' "Show Workstreams", off, takes the Workstreams group out of the
// sidebar, orchestrators included, and leaves the Workstreams as they are;
// on again, the group is back.
func TestShowWorkstreamsOffHidesTheGroup(t *testing.T) {
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}
	h := workstreamsHarness(t, fake, nil)
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") && h.hasText("Payment migration lead") })

	h.openSettingsTab("general")
	h.clickSwitch("Show Workstreams", func() bool { return !prefs.ShowWorkstreams.Get(h.a.prefs) })
	h.a.Go(Route{Name: "home"})
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.settle()
	for _, gone := range []string{"Workstreams", "Payment migration lead", "New Workstream"} {
		if h.hasText(gone) || h.finds(gone) {
			t.Errorf("%q is still in the sidebar: %q", gone, h.tt.Texts())
		}
	}
	if len(h.a.workstreams.list) != 1 {
		t.Fatalf("hiding the group changed the Workstreams: %v", h.a.workstreams.list)
	}

	h.openSettingsTab("general")
	h.clickSwitch("Show Workstreams", func() bool { return prefs.ShowWorkstreams.Get(h.a.prefs) })
	h.a.Go(Route{Name: "home"})
	h.until("the group again", func() bool { return h.hasText("Payment migration lead") && h.finds("New Workstream") })
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
	// Hide the side panel so both tabs stand side by side at readable width.
	h.click("Hide panel")
	h.until("the panel gone", func() bool { return h.finds("Show panel") })
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
	// The agent's Session must be loaded to hear of the change: a change
	// sent before is the Daemon's to report when it loads.
	h.until("the agent's Session loaded", func() bool { return h.a.loaded("agent-1") })
	h.a.showTab("ws-1", "")
	h.notifyState("agent-1", "idle")
	h.notifyState("agent-2", "thinking")
	// The panel lists them: no count over the composer, only the sidebar's
	// (a name and a text).
	h.until("agent working, the other at rest", func() bool { return h.finds("Client switch, Working") && !h.finds("View Agent") })
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

// Many open agent tabs stay readable in a scrollable strip without shrinking
// the orchestrator tab or pushing the bar's action buttons off-screen.
func TestAgentTabsScrollHorizontallyWhenOverflowing(t *testing.T) {
	sc := workstreamsScenario()
	w := paymentWorkstream()
	for i := 4; i <= 7; i++ {
		id := fmt.Sprintf("agent-%d", i)
		title := fmt.Sprintf("Worker %d", i)
		sc.Sessions = append(sc.Sessions, fakedaemon.SessionSpec{
			Title: title, Cwd: "/Users/dev/pay", Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}},
			Extra: map[string]any{"sessionId": id, "tags": []map[string]any{workstreamTag("ws-1", sessions.WorkstreamAgent, "orch-1")}},
		})
		w.Agents = append(w.Agents, gateway.WorkstreamAgent{SessionID: id, Title: title, CreatedAt: w.CreatedAt})
	}
	h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
	h.openOrchestrator()
	h.until("agent rows", func() bool { return h.finds("Worker 7, Idle") })
	for _, id := range []string{"agent-1", "agent-2", "agent-4", "agent-5", "agent-6", "agent-7"} {
		h.a.openAgent("ws-1", id)
		h.frame()
	}
	h.settle()
	orch, ok := h.tt.Find("Payment migration lead")
	if !ok || orch.W < 100 {
		t.Fatalf("orchestrator tab missing or crushed: %+v", orch)
	}
	settingsBtn, ok := h.tt.Find("Workstream settings")
	if !ok || settingsBtn.X+settingsBtn.W > refW {
		t.Fatalf("settings button pushed out of view: %+v", settingsBtn)
	}
	lastTab, ok := h.tt.Find("Close Worker 7")
	if !ok || lastTab.W == 0 {
		t.Fatalf("scrolled active tab close button not visible: %+v", lastTab)
	}
}

// Sidebar rows allow inline renaming via context menu (Enter saves, Escape
// cancels) and inline symbol picking without breaking row navigation.
func TestSidebarWorkstreamRenameAndInlineSymbolPicker(t *testing.T) {
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}
	h := workstreamsHarness(t, fake, nil)
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })

	// Inline symbol picker on the row's icon button.
	h.click("Change symbol for Payment migration lead")
	h.until("the symbol grid", func() bool { return h.finds("Workstream symbol: target") })
	h.click("Workstream symbol: target")
	h.until("the symbol edit", func() bool {
		for _, e := range fake.edited() {
			if e.Symbol != nil && *e.Symbol == "target" && e.Title == nil {
				return true
			}
		}
		return false
	})

	// Escape cancels inline rename without sending an edit.
	if err := h.tt.RightClick("Payment migration lead"); err != nil {
		t.Fatal(err)
	}
	h.frame()
	if err := h.tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatal(err)
	}
	h.frame()
	h.until("rename input", func() bool { return h.finds("Workstream name") })
	h.tt.Key(0, ui.KeyEscape)
	h.frame()
	if h.finds("Workstream name") {
		t.Fatal("rename input stayed after Escape")
	}

	// Enter commits inline rename.
	if err := h.tt.RightClick("Payment migration lead"); err != nil {
		t.Fatal(err)
	}
	h.frame()
	if err := h.tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatal(err)
	}
	h.frame()
	h.replaceText("Workstream name", "Checkout v2")
	h.tt.Key(0, ui.KeyEnter)
	h.until("the name edit", func() bool {
		for _, e := range fake.edited() {
			if e.Title != nil && *e.Title == "Checkout v2" && e.Symbol == nil {
				return true
			}
		}
		return false
	})

	// Clicking the row title still navigates to the orchestrator page.
	h.click("Payment migration lead")
	h.until("the orchestrator page", func() bool { return h.finds("Workstream tabs") })
}

// Focused rows in the Agents panel navigate with ArrowUp, ArrowDown, Home and
// End while previewing the corresponding agent tab.
func TestAgentsPanelKeyboardNavigation(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.until("rows", func() bool { return h.finds("gRPC endpoints, Idle") && h.finds("Client switch, Idle") })

	// Move keyboard focus via Tab until it lands on the first visible Agent row.
	for range 80 {
		if h.tt.Focused("Client switch, Idle") {
			break
		}
		h.tt.Key(0, ui.KeyTab)
		h.frame()
	}
	if !h.tt.Focused("Client switch, Idle") {
		t.Fatal("could not tab to first agent row")
	}

	h.tt.Key(0, ui.KeyDown)
	h.until("second agent focused and opened", func() bool {
		return h.tt.Focused("gRPC endpoints, Idle") && h.a.wsState("ws-1").Active == "agent-1"
	})
	// Focus must stay on the panel list rather than jumping to the newly opened tab's composer.
	h.settle()
	if !h.tt.Focused("gRPC endpoints, Idle") {
		t.Fatal("composer stole focus after keyboard navigation")
	}

	h.tt.Key(0, ui.KeyUp)
	h.until("first agent focused and opened", func() bool {
		return h.tt.Focused("Client switch, Idle") && h.a.wsState("ws-1").Active == "agent-2"
	})
	h.tt.Key(0, ui.KeyEnd)
	h.until("end agent", func() bool { return h.tt.Focused("gRPC endpoints, Idle") })
	h.tt.Key(0, ui.KeyHome)
	h.until("home agent", func() bool { return h.tt.Focused("Client switch, Idle") })
}

// The Goal tab shows an empty state when no plan exists, then renders
// plan.md (or promise.md fallback) from the shared folder.
func TestGoalTabShowsPlanMarkdownAndEmptyState(t *testing.T) {
	dir := t.TempDir()
	w := paymentWorkstream()
	w.SharedFolder = dir
	clock := &movedClock{}
	openedCh := make(chan string, 1)
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, func(cfg *Config) {
		cfg.Now = clock.now
		cfg.OpenPath = func(p string) error { openedCh <- p; return nil }
	})
	h.openOrchestrator()
	h.click("Goal")
	h.until("empty state", func() bool { return h.hasText("No plan yet") })

	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planPath, []byte("# Migration Plan\n\n- Step one\n- Step two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	clock.move(2 * time.Second)
	h.until("plan rendered", func() bool {
		return h.hasText("plan.md") && h.hasText("Migration Plan") && h.hasText("Step one")
	})

	h.click("Open plan.md")
	select {
	case got := <-openedCh:
		if got != planPath {
			t.Fatalf("opened = %q, want %q", got, planPath)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for OpenPath(%q)", planPath)
	}

	// Removing plan.md falls back to promise.md when present.
	if err := os.Remove(planPath); err != nil {
		t.Fatal(err)
	}
	promisePath := filepath.Join(dir, "promise.md")
	if err := os.WriteFile(promisePath, []byte("## Promise\n\nAll tests stay green.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	clock.move(2 * time.Second)
	h.until("promise rendered", func() bool {
		return h.hasText("promise.md") && h.hasText("All tests stay green.")
	})
}

// §5.1: Agent tabs clip inside their scrollable strip and never overlap the
// right-side action buttons even when subagent and git controls narrow the bar.
func TestAgentTabsClipAndNeverOverlapRightToolbar(t *testing.T) {
	sc := workstreamsScenario()
	sc.Sessions = append(sc.Sessions,
		fakedaemon.SessionSpec{
			Title: "Orch subagent task", Cwd: "/Users/dev/pay", Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}},
			Extra: map[string]any{
				"sessionId": "sub-orch-1",
				"subagent": map[string]any{
					"callingSessionId": "orch-1", "callingToolUseId": "toolu_osub1",
					"subagentType": "worker", "description": "Orch subagent task", "status": "completed",
				},
			},
		},
		fakedaemon.SessionSpec{
			Title: "Subagent task", Cwd: "/Users/dev/pay", Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}},
			Extra: map[string]any{
				"sessionId": "sub-1",
				"subagent": map[string]any{
					"callingSessionId": "agent-7", "callingToolUseId": "toolu_sub1",
					"subagentType": "worker", "description": "Subagent task", "status": "completed",
				},
			},
		},
	)
	w := paymentWorkstream()
	for i := 4; i <= 7; i++ {
		id := fmt.Sprintf("agent-%d", i)
		title := fmt.Sprintf("Very long worker tab %d", i)
		sc.Sessions = append(sc.Sessions, fakedaemon.SessionSpec{
			Title: title, Cwd: "/Users/dev/pay", Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}},
			Extra: map[string]any{"sessionId": id, "tags": []map[string]any{workstreamTag("ws-1", sessions.WorkstreamAgent, "orch-1")}},
		})
		w.Agents = append(w.Agents, gateway.WorkstreamAgent{SessionID: id, Title: title, CreatedAt: w.CreatedAt})
	}
	h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
	h.openOrchestrator()
	h.until("agent rows", func() bool { return h.finds("Very long worker tab 7, Idle") })
	for _, id := range []string{"agent-1", "agent-2", "agent-4", "agent-5", "agent-6", "agent-7"} {
		h.a.openAgent("ws-1", id)
		h.frame()
	}
	h.settle()
	subBtn, ok := h.tt.Find("1 subagent")
	if !ok {
		t.Fatalf("expected subagent toolbar button, got none")
	}
	tabList, ok := h.tt.Find("Workstream tabs")
	if !ok {
		t.Fatalf("expected Workstream tabs")
	}
	if tabList.X+tabList.W > subBtn.X+1 {
		t.Fatalf("tabList (%+v) overlaps subagent toolbar button (%+v)", tabList, subBtn)
	}
	if !h.finds("Orchestrator subagents") {
		t.Fatalf("expected top-level Orchestrator subagents group in panel")
	}
	h.shot("workstream-tabs-clipped")
}

// §5.2 & §5.3: Worker Task subagents stay collapsed by default under their owning
// Worker row, expand on click to jump to the subagent session, and every agent
// state + group header renders its status icon.
func TestWorkerSubagentsCollapsedByDefaultAndAllStateIcons(t *testing.T) {
	sc := workstreamsScenario()
	sc.Sessions = append(sc.Sessions,
		fakedaemon.SessionSpec{
			Title: "Trace RPC layer", Cwd: "/Users/dev/pay-worktrees/grpc", Messages: []fakedaemon.Message{{Role: "user", Text: "trace"}},
			Extra: map[string]any{
				"sessionId": "sub-w1",
				"subagent": map[string]any{
					"callingSessionId": "agent-1", "callingToolUseId": "toolu_w1",
					"subagentType": "worker", "description": "Trace RPC layer", "status": "completed",
				},
			},
		},
		fakedaemon.SessionSpec{
			Title: "Check TLS certs", Cwd: "/Users/dev/pay-worktrees/grpc", Messages: []fakedaemon.Message{{Role: "user", Text: "tls"}},
			Extra: map[string]any{
				"sessionId": "sub-w2",
				"subagent": map[string]any{
					"callingSessionId": "agent-1", "callingToolUseId": "toolu_w2",
					"subagentType": "worker", "description": "Check TLS certs", "status": "completed",
				},
			},
		},
	)
	w := paymentWorkstream()
	at := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339)
	w.Agents = append(w.Agents, gateway.WorkstreamAgent{
		SessionID: "agent-blocked", Title: "Blocked worker", Branch: "droi/blocked", CreatedAt: at,
		LastReport: &gateway.WorkstreamReport{Kind: "blocked", Text: "Waiting on schema", At: at},
	})
	h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
	h.openOrchestrator()
	h.until("subagent toggle", func() bool { return h.finds("gRPC endpoints, 2 subagents") })
	if h.finds("Trace RPC layer, Subagent") {
		t.Fatal("worker subagents should start collapsed by default")
	}
	// All visible state icons have clear accessible status labels.
	for _, want := range []string{"Done", "Blocked", "Idle"} {
		if !h.finds(want) {
			t.Fatalf("missing agent status icon label %q", want)
		}
	}
	h.click("Archived")
	h.until("archived icon", func() bool { return h.finds("Archived") })

	// Expanding worker subagents reveals them and allows opening one.
	h.click("gRPC endpoints, 2 subagents")
	h.until("expanded subagent", func() bool { return h.finds("Trace RPC layer, Subagent") })
	h.shot("workstream-subagents-expanded")
	h.click("Trace RPC layer, Subagent")
	h.until("navigated to subagent", func() bool { return h.a.Route().SessionID == "sub-w1" })
}

// §5.4: Files panel organizes the shared folder into a collapsible tree (folders
// collapsed by default, auto-expanded while searching).
func TestFilesPanelRendersCollapsibleFolderTree(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "perf", "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "perf", "data", "metrics.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "perf", "report.md"), []byte("# Perf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := paymentWorkstream()
	w.SharedFolder = dir
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
	h.openOrchestrator()
	h.click("Files")
	h.until("tree root", func() bool { return h.finds("perf") && h.finds("readme.txt") })
	if h.finds(filepath.Join("perf", "report.md")) || h.finds(filepath.Join("perf", "data", "metrics.json")) {
		t.Fatal("nested files should stay hidden while folder is collapsed")
	}
	h.shot("workstream-files-tree-collapsed")

	h.click("perf")
	h.until("perf expanded", func() bool { return h.finds("data") && h.finds(filepath.Join("perf", "report.md")) })
	h.click("data")
	h.until("data expanded", func() bool { return h.finds(filepath.Join("perf", "data", "metrics.json")) })

	// Collapse perf again, then verify searching auto-expands matching directories.
	h.click("perf")
	h.until("perf collapsed", func() bool { return !h.finds(filepath.Join("perf", "report.md")) })
	h.click("Search files")
	h.tt.Type("metrics")
	h.until("search auto-expanded", func() bool { return h.finds(filepath.Join("perf", "data", "metrics.json")) })
}

// §5.5: PRs tab and clickable Agent row PR badges resolve statuses from the
// Daemon and fall back to PR URLs in agent reports.
func TestPRsTabAndAgentRowBadges(t *testing.T) {
	w := paymentWorkstream()
	w.Agents[0].LastReport = &gateway.WorkstreamReport{
		Kind: "done",
		Text: "Opened https://github.com/acme/pay/pull/219 for review.",
		At:   w.CreatedAt,
	}
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
	num219 := float64(219)
	num210 := float64(210)
	h.a.workstreams.resolvePRs = func(_ context.Context, p protocol.ResolvePullRequestStatusesParams) (*protocol.ResolvePullRequestStatusesResult, error) {
		sub1, _ := protocol.NewDaemonPullRequestSubject(protocol.DaemonPullRequestSubjectURL{
			Kind: protocol.DaemonPullRequestSubjectKindURL, SessionID: "agent-1", URL: "https://github.com/acme/pay/pull/219",
		})
		sub2, _ := protocol.NewDaemonPullRequestSubject(protocol.DaemonPullRequestSubjectBranch{
			Kind: protocol.DaemonPullRequestSubjectKindBranch, SessionID: "agent-2",
		})
		return &protocol.ResolvePullRequestStatusesResult{
			Statuses: []protocol.DaemonPullRequestStatusResult{
				{
					Subject: sub1,
					Branch:  "droi/grpc-endpoints",
					Record: &protocol.DaemonPullRequestRecord{
						State:  protocol.DaemonPullRequestStateOpen,
						URL:    "https://github.com/acme/pay/pull/219",
						Title:  "Migrate gRPC endpoints",
						Number: &num219,
					},
				},
				{
					Subject: sub2,
					Branch:  "droi/client-switch",
					Record: &protocol.DaemonPullRequestRecord{
						State:  protocol.DaemonPullRequestStateMerged,
						URL:    "https://github.com/acme/pay/pull/210",
						Title:  "Switch client adapter",
						Number: &num210,
					},
				},
			},
		}, nil
	}
	h.openOrchestrator()
	h.until("agent PR badge", func() bool {
		return h.finds("Pull request #219, Open") && h.finds("Pull request #210, Merged")
	})
	h.click("PRs")
	h.until("PRs list", func() bool {
		return h.hasText("#219 Migrate gRPC endpoints") && h.hasText("#210 Switch client adapter")
	})
	h.shot("workstream-prs-tab")
}

// §5.6: Workstream settings exposes separate Orchestrator/Agent reasoning level
// selects (including Extra high / xhigh) and queues rapid saves instead of
// dropping them while another save is in flight.
func TestWorkstreamSettingsAgentReasoningLevelsAndQueuedSave(t *testing.T) {
	gate := make(chan struct{})
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}, setGate: gate}
	h := workstreamsHarness(t, fake, nil)
	h.openOrchestrator()
	h.click("Workstream settings")
	h.until("settings dialog", func() bool { return h.finds("Orchestrator model") && h.finds("Agent reasoning") })
	if h.finds("Orchestrator reasoning") {
		t.Fatal("Auto Model only supports none and should hide Orchestrator reasoning")
	}

	// Pick GPT-5 as Agent model, then pick Extra high (xhigh) from Agent reasoning.
	h.a.saveAgentModel("ws-1", "gpt-5", "")
	// While the first save is blocked on gate, trigger a second change to xhigh.
	h.a.saveAgentModel("ws-1", "gpt-5", "xhigh")
	close(gate)
	h.until("queued xhigh saved", func() bool {
		_, settings, _ := fake.calls()
		return len(settings) == 2 && settings[1].AgentModel == "gpt-5" && settings[1].AgentReasoningEffort == "xhigh"
	})
	h.settle()
	// Also verify the Agent reasoning dropdown opens and exposes Extra high in UI.
	h.click("Agent reasoning level")
	h.until("xhigh option", func() bool { return h.hasText("Low") && h.hasText("Extra high") })
	h.shot("workstream-settings-reasoning")
}
