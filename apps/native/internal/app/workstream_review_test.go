package app

import (
	"github.com/egoist/mygo/ui"

	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
)

// Two agents the Host starts under one name each have a row: the rows do not
// share a key, which the toolkit refuses.
func TestTwoStartingRowsOfOneNameShowTogether(t *testing.T) {
	w := paymentWorkstream()
	at := time.Now().UTC().Format(time.RFC3339)
	w.Starting = []gateway.WorkstreamStarting{{Title: "Docs", StartedAt: at}, {Title: "Docs", StartedAt: at}, {StartedAt: at}, {StartedAt: at}}
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
	h.openOrchestrator()
	h.until("the rows", func() bool { return h.textCount("Docs") == 2 && h.textCount("New agent") == 2 })
}

// A narrow window has no room for the panel beside the page: it opens over
// it, from the same buttons.
func TestANarrowWindowOpensThePanelOverThePage(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.tt.SetSize(700, 700)
	h.frame()
	h.a.Go(Route{Name: "session", SessionID: "orch-1"})
	h.until("the button", func() bool { return h.finds("Show panel") })
	if h.finds("Workstream panel") {
		t.Fatal("the panel is out before it was asked for")
	}
	h.click("Show panel")
	h.until("the drawer", func() bool { return h.finds("Workstream panel") && h.hasText("gRPC endpoints") })
	h.click("Hide panel")
	h.until("the drawer gone", func() bool { return !h.finds("Workstream panel") })
	h.notifyState("agent-2", "thinking")
	h.until("the count", func() bool { return h.hasText("1 agent working") })
	box, ok := h.tt.Find("Message composer")
	if !ok {
		t.Fatal("no composer")
	}
	h.tt.ClickAt(box.X+box.W-70, box.Y-20)
	h.frame()
	h.until("the drawer from the count", func() bool { return h.finds("Workstream panel") })
	h.click("gRPC endpoints, Idle")
	h.until("the agent's tab with the drawer shut", func() bool {
		return h.a.wsState("ws-1").Active == "agent-1" && !h.finds("Workstream panel")
	})
}

// The shared folder's files are listed newest first however many there are,
// and the folder is not read again by every frame.
func TestTheFilesPanelKeepsTheNewestAndReadsRarely(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	for i := range 600 {
		p := filepath.Join(dir, fmt.Sprintf("d%d", i%20), fmt.Sprintf("f%d.md", i))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
		_ = os.Chtimes(p, old, old.Add(time.Duration(i)*time.Second))
	}
	newest := filepath.Join(dir, "zz", "newest.md")
	_ = os.MkdirAll(filepath.Dir(newest), 0o755)
	_ = os.WriteFile(newest, []byte("x"), 0o644)
	w := paymentWorkstream()
	w.SharedFolder = dir
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
	h.openOrchestrator()
	h.click("Files")
	h.until("the newest first", func() bool { return h.hasText("zz/newest.md") })
	before := h.a.workstreams.files.reads
	for range 30 {
		h.tt.Frame()
	}
	if got := h.a.workstreams.files.reads - before; got > 1 {
		t.Fatalf("the folder was read %d times by 30 frames", got)
	}
}

// Each group of the agents folds, and stays folded.
func TestEveryGroupOfTheAgentsFolds(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.until("the idle agents", func() bool { return h.finds("gRPC endpoints, Idle") })
	h.click("Idle")
	h.until("folded", func() bool { return !h.finds("gRPC endpoints, Idle") })
	if !slices.Contains(h.a.wsState("ws-1").Folded, "idle") {
		t.Fatalf("not kept: %+v", h.a.wsState("ws-1"))
	}
	fresh := &App{prefs: h.a.prefs}
	if !slices.Contains(fresh.wsState("ws-1").Folded, "idle") {
		t.Fatal("not saved")
	}
	h.click("Idle")
	h.until("open again", func() bool { return h.finds("gRPC endpoints, Idle") })
}

// An agent that works only in Direct is marked when this device wants
// Script; while following another computer, the device's default is that
// computer's, which the window does not know, so only the agent's own mode
// counts.
func TestTheDirectMarkNeedsNoLocalDefaultWhileFollowing(t *testing.T) {
	no := false
	if on, _ := directMarkFor("direct_only", &no, true, false); on {
		t.Fatal("marked while this device asks for Direct")
	}
	if on, _ := directMarkFor("direct_only", &no, true, true); !on {
		t.Fatal("not marked while following, though the agent is in Direct")
	}
	if on, _ := directMarkFor("script_only", &no, false, true); on {
		t.Fatal("marked a Script agent")
	}
}

// The orchestrator in Direct is marked too.
func TestADirectOrchestratorWearsTheMark(t *testing.T) {
	w := paymentWorkstream()
	w.OrchestratorToolMode = "direct_only"
	no := false
	w.OrchestratorScriptCapable = &no
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
	h.openOrchestrator()
	h.until("the mark", func() bool { return h.hasText("Direct") })
}

// What the window keeps of Workstreams that are gone, and of agent tabs for
// agents that are archived or gone, is dropped.
func TestTheSavedPagesLoseWhatIsGone(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.until("the list", func() bool { return h.a.findWorkstream("ws-1") != nil })
	h.a.setWsState("ws-gone", func(u *workstreamUI) { u.Closed = true })
	h.a.setWsState("ws-1", func(u *workstreamUI) { u.Tabs = []string{"agent-1", "agent-ghost", "agent-3"} })
	h.a.loadWorkstreams(true)
	h.until("pruned", func() bool { return len(h.a.wsState("ws-1").Tabs) == 1 })
	if got := h.a.wsState("ws-1").Tabs; got[0] != "agent-1" {
		t.Fatalf("tabs %v", got)
	}
	if h.a.wsState("ws-gone").Closed {
		t.Fatal("the state of a Workstream that is gone stays")
	}
}

// A Host from before workstreams started from a name wants a goal: the
// dialog says to update it.
func TestAnOlderHostThatWantsAGoalIsTold(t *testing.T) {
	fake := &fakeWorkstreams{createErr: &gateway.RefusedRequest{Reason: "Enter the goal."}}
	h := workstreamsHarness(t, fake, nil)
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	h.click("New Workstream")
	h.until("the dialog", func() bool { return h.finds("Workstream name") })
	h.click("Workstream name")
	h.tt.Type("Billing")
	h.frame()
	h.tt.Key(0, ui.KeyEnter)
	h.until("the advice", func() bool { return h.hasText("Update Droi") })
}

// The agents' model says what it inherits.
func TestInheritNamesTheModelItComesTo(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.click("Workstream settings")
	h.until("the dialog", func() bool { return h.hasText("Agent model") && h.hasText("Inherit (") })
}

// Any other Session keeps the words of its bar at the widths between the
// narrow window and a wide one, where it wraps if it must.
func TestAPlainSessionKeepsItsBarInWordsWhenTheWindowIsModest(t *testing.T) {
	for _, width := range []int{900, 800, 780} {
		h := newHarness(t, workstreamsScenario(), "")
		h.tt.SetSize(width, 640)
		h.frame()
		h.until("the list", func() bool { return h.hasText("Fix the login race") })
		h.click("Fix the login race")
		h.until("the bar", func() bool { return h.hasText("Spec") && h.hasText("Skills") && h.hasText("Low autonomy") })
	}
}

// The drawer is shut by going to another page.
func TestTheDrawerDoesNotOutliveItsPage(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.tt.SetSize(700, 700)
	h.frame()
	h.a.Go(Route{Name: "session", SessionID: "orch-1"})
	h.until("the button", func() bool { return h.finds("Show panel") })
	h.click("Show panel")
	h.until("the drawer", func() bool { return h.finds("Workstream panel") })
	h.a.Go(Route{Name: "home"})
	h.frame()
	h.a.Go(Route{Name: "session", SessionID: "orch-1"})
	h.until("the page", func() bool { return h.finds("Show panel") })
	if h.finds("Workstream panel") || h.a.workstreams.paneDrawer {
		t.Fatal("the drawer came back with the page")
	}
}

// Opening the drawer of a narrow window leaves the wide window's choice
// of keeping the panel away as it was.
func TestTheDrawerLeavesThePanelsSavedChoiceAlone(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.click("Hide panel")
	h.until("hidden", func() bool { return h.a.wsState("ws-1").Closed })
	h.tt.SetSize(700, 700)
	h.frame()
	h.until("the button", func() bool { return h.finds("Show panel") })
	h.click("Show panel")
	h.until("the drawer", func() bool { return h.finds("Workstream panel") })
	if !h.a.wsState("ws-1").Closed {
		t.Fatal("the drawer opened the panel for the wide window")
	}
	h.click("Hide panel")
	h.tt.SetSize(1024, 640)
	h.frame()
	h.until("wide again", func() bool { return h.finds("Show panel") && !h.finds("Workstream panel") })
}
