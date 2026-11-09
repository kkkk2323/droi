package app

import (
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/gateway"
)

func themedWorkstreams(t *testing.T, theme string, sc fakedaemon.Scenario, w gateway.Workstream) *harness {
	t.Helper()
	fake := &fakeWorkstreams{list: []gateway.Workstream{w}}
	h := startHarnessGateway(t, sc, theme, "", func(o *gateway.Options) { o.Workstreams = fake }, nil)
	closeTransport(t, h)
	return h
}

// The views the review asked to see, written to DROI_NATIVE_SHOTS for a
// look; nothing is checked here beyond that they draw.
func TestWorkstreamShotsForTheReview(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		w := paymentWorkstream()
		w.Agents = nil
		h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
		h.openOrchestrator()
		h.until("empty", func() bool { return h.hasText("No agents yet") })
		h.shot("workstream-agents-empty")
	})
	t.Run("two waiting", func(t *testing.T) {
		h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
		h.openOrchestrator()
		h.notifyState("agent-1", "waiting_for_tool_confirmation")
		h.notifyState("agent-2", "waiting_for_tool_confirmation")
		h.until("two", func() bool { return h.hasText("2 waiting") || h.textCount("Needs your approval") >= 2 })
		h.shot("workstream-attention-two")
	})
	t.Run("starting", func(t *testing.T) {
		w := paymentWorkstream()
		at := time.Now().UTC().Format(time.RFC3339)
		w.Starting = []gateway.WorkstreamStarting{{Title: "Docs sweep", StartedAt: at}}
		h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
		h.openOrchestrator()
		h.until("row", func() bool { return h.hasText("Docs sweep") })
		h.shot("workstream-starting")
	})
	t.Run("dark", func(t *testing.T) {
		h := themedWorkstreams(t, "dark", orchestratorTalks(), paymentWorkstream())
		h.a.Go(Route{Name: "session", SessionID: "orch-1"})
		h.until("rows", func() bool { return h.hasText("Started 2 of 3 agents") })
		h.shot("workstream-dark-transcript")
		h.click("Received message from")
		h.until("report", func() bool { return h.hasText("Moved the first endpoint.") })
		h.shot("workstream-dark-panel")
	})
	t.Run("git dialog", func(t *testing.T) {
		sc := workstreamsScenario()
		sc.Input = map[string]any{"gitRepos": map[string]any{"/Users/dev/pay": map[string]any{"branches": []string{"main"}, "currentBranch": "main"}}}
		h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{}, nil)
		h.until("list", func() bool { return h.hasText("Payment migration lead") })
		h.a.openNewWorkstream()
		h.until("branch", func() bool { return h.hasText("BRANCH") && h.hasText("main") })
		h.shot("workstream-new-git")
	})
	t.Run("narrow drawer", func(t *testing.T) {
		h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
		h.tt.SetSize(700, 700)
		h.frame()
		h.a.Go(Route{Name: "session", SessionID: "orch-1"})
		h.until("button", func() bool { return h.finds("Show panel") })
		h.click("Show panel")
		h.until("drawer", func() bool { return h.finds("Workstream panel") })
		h.shot("workstream-narrow-drawer")
	})
}
