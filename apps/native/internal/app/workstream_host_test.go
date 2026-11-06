package app

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/defaults"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// scriptHarness is the window with a Host that answers which models may use
// Script with list, or says nothing of them for nil.
func scriptHarness(t *testing.T, fake *fakeWorkstreams, list *gateway.ScriptModelList) *harness {
	t.Helper()
	h := startHarnessGateway(t, workstreamsScenario(), "", "", func(o *gateway.Options) {
		o.Workstreams = fake
		if list != nil {
			o.ScriptModels = func() gateway.ScriptModelList { return *list }
		}
	}, nil)
	closeTransport(t, h)
	return h
}

func ptr[T any](v T) *T { return &v }

// The Host's note of an agent it is starting stands in for it until its
// Session is there.
func TestAnAgentTheHostIsStartingHasARow(t *testing.T) {
	w := paymentWorkstream()
	w.Starting = []gateway.WorkstreamStarting{{Title: "Docs sweep", StartedAt: time.Now().UTC().Format(time.RFC3339)}}
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
	h.openOrchestrator()
	h.until("the row", func() bool { return h.hasText("Docs sweep") && h.hasText("Starting") })
}

// An agent that can use only Direct tools wears a mark while the device
// does not ask for Direct, and the mark says why.
func TestADirectAgentWearsAMarkUnlessTheDeviceWantsDirect(t *testing.T) {
	w := paymentWorkstream()
	w.Agents[0].ToolMode = string(defaults.DirectOnly)
	w.Agents[0].ScriptCapable = ptr(false)
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
	h.openOrchestrator()
	h.until("the mark", func() bool { return h.hasText("Direct") })
	if got := h.textCount("Direct"); got != 1 {
		t.Fatalf("%d marks, want one: %q", got, h.tt.Texts())
	}
	defaults.SetDefaultToolMode(h.a.prefs, defaults.DirectOnly)
	h.until("no mark", func() bool { return !h.hasText("Direct") })
}

// The new dialog sends its symbol and no goal.
func TestTheNewDialogSendsTheSymbol(t *testing.T) {
	fake := &fakeWorkstreams{made: gateway.WorkstreamCreated{WorkstreamID: "ws-2", OrchestratorID: "orch-1"}}
	h := workstreamsHarness(t, fake, nil)
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	h.click("New Workstream")
	h.until("the dialog", func() bool { return h.finds("Workstream name") })
	h.click("Workstream symbol: flag")
	h.until("the grid", func() bool { return h.finds("Workstream symbol: target") })
	h.click("Workstream symbol: target")
	h.click("Workstream name")
	h.tt.Type("Billing")
	h.frame()
	h.tt.Key(0, ui.KeyEnter)
	h.until("created", func() bool { c, _, _ := fake.calls(); return len(c) == 1 })
	if c, _, _ := fake.calls(); c[0].Symbol != "target" || c[0].Goal != "" {
		t.Fatalf("created %+v", c)
	}
}

// Settings rename and re-symbol through the Host.
func TestWorkstreamSettingsEditTheNameAndSymbol(t *testing.T) {
	fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}
	h := workstreamsHarness(t, fake, nil)
	h.openOrchestrator()
	h.click("Workstream settings")
	h.until("the dialog", func() bool { return h.finds("Workstream symbol: flag") })
	h.click("Workstream symbol: flag")
	h.until("the grid", func() bool { return h.finds("Workstream symbol: rocket") || h.finds("Workstream symbol: target") })
	h.click("Workstream symbol: target")
	h.until("the symbol edit", func() bool {
		for _, e := range fake.edited() {
			if e.Symbol != nil && *e.Symbol == "target" && e.Title == nil {
				return true
			}
		}
		return false
	})
	h.replaceText("Workstream name", "Billing move")
	h.tt.Key(0, ui.KeyEnter)
	h.until("the name edit", func() bool {
		for _, e := range fake.edited() {
			if e.Title != nil && *e.Title == "Billing move" && e.Symbol == nil {
				return true
			}
		}
		return false
	})
}

// A model the Host says cannot use Script gets the hint, and one click
// allows it for this computer.
func TestTheSettingsOfferScriptForAModelThatLacksIt(t *testing.T) {
	w := paymentWorkstream()
	w.AgentModel = "claude-opus-4-1"
	list := &gateway.ScriptModelList{Factory: []string{"gpt-5"}, Known: true}
	h := scriptHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, list)
	h.openOrchestrator()
	h.click("Workstream settings")
	h.until("the hint", func() bool { return h.hasText("This model can only use Direct tools") && h.finds("Allow Script") })
	h.click("Allow Script")
	h.until("allowed", func() bool {
		for _, m := range prefs.ScriptModels.Get(h.a.prefs) {
			if m == "claude-opus-4-1" {
				return true
			}
		}
		return false
	})
	h.until("no hint", func() bool { return !h.hasText("This model can only use Direct tools") })
}

// A Host that cannot say which models use Script, or does not know
// Factory's list yet, gets no hint: the window cannot tell.
func TestNoScriptHintWhenTheHostCannotSay(t *testing.T) {
	for name, list := range map[string]*gateway.ScriptModelList{
		"an older Host": nil,
		"unknown list":  {Known: false},
	} {
		t.Run(strings.ReplaceAll(name, " ", "-"), func(t *testing.T) {
			w := paymentWorkstream()
			w.AgentModel = "claude-opus-4-1"
			h := scriptHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, list)
			h.openOrchestrator()
			h.click("Workstream settings")
			h.until("the dialog", func() bool { return h.hasText("Agent model") })
			h.settle()
			if h.hasText("This model can only use Direct tools") {
				t.Fatalf("a hint for %s", name)
			}
		})
	}
}

// Beside the panel the page is narrow, and the composer's settings, the
// Orchestrator's mark and the tools stay on one row, as icons.
func TestTheComposerBarStaysOneRowBesideThePanel(t *testing.T) {
	h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
	h.openOrchestrator()
	h.until("the bar", func() bool {
		for _, label := range []string{"Model and reasoning effort", "Spec mode", "Autonomy: Low autonomy", "Orchestrator", "Skills and MCP servers", "Send"} {
			if !h.finds(label) {
				return false
			}
		}
		return true
	})
	var ys []float32
	for _, label := range []string{"Model and reasoning effort", "Spec mode", "Autonomy: Low autonomy", "Orchestrator", "Skills and MCP servers", "Send"} {
		r, ok := h.tt.Find(label)
		if !ok {
			t.Fatalf("no %q in %q", label, h.tt.Texts())
		}
		ys = append(ys, r.Y+r.H/2)
	}
	for _, y := range ys {
		if y < ys[0]-8 || y > ys[0]+8 {
			t.Fatalf("the bar is on more than one row: %v", ys)
		}
	}
	if h.hasText("Low autonomy") && !h.finds("Autonomy: Low autonomy") {
		t.Fatal("autonomy in words beside the panel")
	}
}

// A page wide enough keeps the bar in words.
func TestTheComposerBarKeepsItsWordsOnAWidePage(t *testing.T) {
	h := newHarness(t, workstreamsScenario(), "")
	h.until("the list", func() bool { return h.hasText("Fix the login race") })
	h.click("Fix the login race")
	h.until("the bar", func() bool { return h.hasText("Spec") && h.hasText("Skills") })
}
