package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
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
	t.Run("sync 01 tabs no overlap", func(t *testing.T) {
		sc := workstreamsScenario()
		for i := 1; i <= 4; i++ {
			sc.Sessions = append(sc.Sessions, fakedaemon.SessionSpec{
				Title: fmt.Sprintf("Sub %d", i), Cwd: "/Users/dev/pay", Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}},
				Extra: map[string]any{
					"sessionId": fmt.Sprintf("agent-sub-%d", i),
					"subagent": map[string]any{
						"callingSessionId": "agent-7", "callingToolUseId": fmt.Sprintf("toolu_as%d", i),
						"subagentType": "worker", "description": fmt.Sprintf("Sub %d", i), "status": "completed",
					},
				},
			})
		}
		w := paymentWorkstream()
		for i := 4; i <= 7; i++ {
			id := fmt.Sprintf("agent-%d", i)
			title := fmt.Sprintf("Worker tab %d", i)
			sc.Sessions = append(sc.Sessions, fakedaemon.SessionSpec{
				Title: title, Cwd: "/Users/dev/pay", Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}},
				Extra: map[string]any{"sessionId": id, "tags": []map[string]any{workstreamTag("ws-1", sessions.WorkstreamAgent, "orch-1")}},
			})
			w.Agents = append(w.Agents, gateway.WorkstreamAgent{SessionID: id, Title: title, CreatedAt: w.CreatedAt})
		}
		h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
		h.openOrchestrator()
		h.until("agents", func() bool { return h.finds("Worker tab 7, Idle") })
		for _, id := range []string{"agent-1", "agent-2", "agent-4", "agent-5", "agent-6", "agent-7"} {
			h.a.openAgent("ws-1", id)
			h.frame()
		}
		h.until("subagent button", func() bool { return h.finds("4 subagents") && h.hasText("4 subagents") })
		h.shot("workstream-sync-01-tabs-no-overlap")
	})
	t.Run("sync 02 agents panel full", func(t *testing.T) {
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
						"subagentType": "worker", "description": "Check TLS certs", "status": "running",
					},
				},
			},
			fakedaemon.SessionSpec{
				Title: "Verify webhook retries", Cwd: "/Users/dev/pay-worktrees/clients", Messages: []fakedaemon.Message{{Role: "user", Text: "verify"}},
				Extra: map[string]any{
					"sessionId": "sub-w3",
					"subagent": map[string]any{
						"callingSessionId": "agent-2", "callingToolUseId": "toolu_w3",
						"subagentType": "worker", "description": "Verify webhook retries", "status": "completed",
					},
				},
			},
			fakedaemon.SessionSpec{
				Title: "Waiting worker", Cwd: "/Users/dev/pay-worktrees/wait", Messages: []fakedaemon.Message{{Role: "user", Text: "wait"}},
				Extra: map[string]any{"sessionId": "agent-wait", "tags": []map[string]any{workstreamTag("ws-1", sessions.WorkstreamAgent, "orch-1")}},
			},
			fakedaemon.SessionSpec{
				Title: "Working worker", Cwd: "/Users/dev/pay-worktrees/work", Messages: []fakedaemon.Message{{Role: "user", Text: "work"}},
				Extra: map[string]any{"sessionId": "agent-work", "tags": []map[string]any{workstreamTag("ws-1", sessions.WorkstreamAgent, "orch-1")}},
			},
		)
		w := paymentWorkstream()
		at := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339)
		w.Agents = append(w.Agents,
			gateway.WorkstreamAgent{SessionID: "agent-wait", Title: "Waiting worker", Branch: "droi/wait", CreatedAt: at},
			gateway.WorkstreamAgent{SessionID: "agent-work", Title: "Working worker", Branch: "droi/work", CreatedAt: at},
			gateway.WorkstreamAgent{
				SessionID: "agent-blocked", Title: "Blocked worker", Branch: "droi/blocked", CreatedAt: at,
				LastReport: &gateway.WorkstreamReport{Kind: "blocked", Text: "Waiting on schema", At: at},
			},
		)
		h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
		num219, num210 := float64(219), float64(210)
		h.a.workstreams.resolvePRs = func(_ context.Context, _ protocol.ResolvePullRequestStatusesParams) (*protocol.ResolvePullRequestStatusesResult, error) {
			sub1, _ := protocol.NewDaemonPullRequestSubject(protocol.DaemonPullRequestSubjectBranch{Kind: protocol.DaemonPullRequestSubjectKindBranch, SessionID: "agent-1"})
			sub2, _ := protocol.NewDaemonPullRequestSubject(protocol.DaemonPullRequestSubjectBranch{Kind: protocol.DaemonPullRequestSubjectKindBranch, SessionID: "agent-2"})
			return &protocol.ResolvePullRequestStatusesResult{
				Statuses: []protocol.DaemonPullRequestStatusResult{
					{Subject: sub1, Branch: "droi/grpc-endpoints", Record: &protocol.DaemonPullRequestRecord{State: protocol.DaemonPullRequestStateOpen, URL: "https://github.com/acme/pay/pull/219", Title: "Migrate gRPC endpoints", Number: &num219}},
					{Subject: sub2, Branch: "droi/client-switch", Record: &protocol.DaemonPullRequestRecord{State: protocol.DaemonPullRequestStateMerged, URL: "https://github.com/acme/pay/pull/210", Title: "Switch client adapter", Number: &num210}},
				},
			}, nil
		}
		h.tt.SetSize(refW, 820)
		h.openOrchestrator()
		h.notifyState("agent-wait", "waiting_for_tool_confirmation")
		h.notifyState("agent-work", "streaming_assistant_message")
		h.until("all groups and badges", func() bool {
			return h.finds("Waiting on you") && h.finds("Working") && h.finds("Pull request #219, Open") && h.finds("gRPC endpoints, 2 subagents")
		})
		h.click("gRPC endpoints, 2 subagents")
		h.click("Archived")
		h.until("expanded subagents and archived", func() bool {
			return h.finds("Trace RPC layer, Subagent") && h.hasText("Old cleanup")
		})
		h.shot("workstream-sync-02-agents-panel")
	})
	t.Run("sync 03 goal plan", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Payment Migration Plan\n\n- Migrate gRPC endpoints to v2\n- Switch client SDK adapter\n- Add end-to-end verification tests\n\n## Status\n\nIn progress.\n"), 0o644)
		w := paymentWorkstream()
		w.SharedFolder = dir
		h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
		h.openOrchestrator()
		h.click("Goal")
		h.until("plan rendered", func() bool { return h.hasText("Payment Migration Plan") })
		h.shot("workstream-sync-03-goal-plan")
	})
	t.Run("sync 04 prs tab", func(t *testing.T) {
		w := paymentWorkstream()
		h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
		num219, num210, num205 := float64(219), float64(210), float64(205)
		h.a.workstreams.resolvePRs = func(_ context.Context, _ protocol.ResolvePullRequestStatusesParams) (*protocol.ResolvePullRequestStatusesResult, error) {
			sub1, _ := protocol.NewDaemonPullRequestSubject(protocol.DaemonPullRequestSubjectBranch{Kind: protocol.DaemonPullRequestSubjectKindBranch, SessionID: "agent-1"})
			sub2, _ := protocol.NewDaemonPullRequestSubject(protocol.DaemonPullRequestSubjectBranch{Kind: protocol.DaemonPullRequestSubjectKindBranch, SessionID: "agent-2"})
			sub3, _ := protocol.NewDaemonPullRequestSubject(protocol.DaemonPullRequestSubjectBranch{Kind: protocol.DaemonPullRequestSubjectKindBranch, SessionID: "agent-3"})
			return &protocol.ResolvePullRequestStatusesResult{
				Statuses: []protocol.DaemonPullRequestStatusResult{
					{Subject: sub1, Branch: "droi/grpc-endpoints", Record: &protocol.DaemonPullRequestRecord{State: protocol.DaemonPullRequestStateOpen, URL: "https://github.com/acme/pay/pull/219", Title: "Migrate gRPC endpoints", Number: &num219}},
					{Subject: sub2, Branch: "droi/client-switch", Record: &protocol.DaemonPullRequestRecord{State: protocol.DaemonPullRequestStateMerged, URL: "https://github.com/acme/pay/pull/210", Title: "Switch client adapter", Number: &num210}},
					{Subject: sub3, Branch: "droi/cleanup", Record: &protocol.DaemonPullRequestRecord{State: protocol.DaemonPullRequestStateDraft, URL: "https://github.com/acme/pay/pull/205", Title: "Clean up legacy adapters", Number: &num205}},
				},
			}, nil
		}
		h.openOrchestrator()
		h.click("PRs")
		h.until("prs list", func() bool {
			return h.hasText("#219 Migrate gRPC endpoints") && h.hasText("#210 Switch client adapter") && h.hasText("#205 Clean up legacy adapters")
		})
		h.shot("workstream-sync-04-prs-tab")
	})
	t.Run("sync 05 files tree", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.MkdirAll(filepath.Join(dir, "perf", "data"), 0o755)
		_ = os.MkdirAll(filepath.Join(dir, "notes"), 0o755)
		_ = os.WriteFile(filepath.Join(dir, "perf", "data", "metrics.json"), []byte("{}"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "perf", "report.md"), []byte("# Perf"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "notes", "handoff.md"), []byte("# Handoff"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan"), 0o644)
		w := paymentWorkstream()
		w.SharedFolder = dir
		h := workstreamsHarness(t, &fakeWorkstreams{list: []gateway.Workstream{w}}, nil)
		h.openOrchestrator()
		h.click("Files")
		h.until("collapsed tree", func() bool { return h.finds("perf") && h.finds("notes") && h.finds("plan.md") })
		h.shot("workstream-sync-05-files-collapsed")
		h.click("perf")
		h.until("perf expanded", func() bool { return h.finds("data") && h.finds(filepath.Join("perf", "report.md")) })
		h.click("data")
		h.until("data expanded", func() bool { return h.finds(filepath.Join("perf", "data", "metrics.json")) })
		h.shot("workstream-sync-05-files-expanded")
	})
	t.Run("sync 06 settings reasoning dropdown", func(t *testing.T) {
		fake := &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}
		h := workstreamsHarness(t, fake, nil)
		h.tt.SetSize(refW, 820)
		h.openOrchestrator()
		h.click("Workstream settings")
		h.until("settings", func() bool { return h.finds("Orchestrator model") && h.finds("Agent reasoning") })
		h.a.saveAgentModel("ws-1", "gpt-5", "xhigh")
		h.until("saved xhigh", func() bool {
			_, settings, _ := fake.calls()
			return len(settings) == 1 && settings[0].AgentReasoningEffort == "xhigh"
		})
		h.click("Agent reasoning level")
		h.until("xhigh menu", func() bool { return h.hasText("Low") && h.hasText("Extra high") })
		h.shot("workstream-sync-06-settings-reasoning")
	})
	t.Run("sync 07 transcript activity and rename", func(t *testing.T) {
		sc := workstreamsScenario()
		nested := func(id, name string, input map[string]any) map[string]any {
			use := wsUse(id, name, input)
			if name == "Read" {
				use["name"] = "Read"
			}
			use["scriptExecution"] = map[string]any{"runId": "s1", "outerToolUseId": "s1"}
			return use
		}
		sc.Sessions[1].Messages = []fakedaemon.Message{
			{Role: "user", Text: "Run migration and rename the workers"},
			{Role: "assistant", Content: []map[string]any{
				{"type": "tool_use", "id": "s1", "name": "Script", "input": map[string]any{"script": "x"}},
				nested("n1", "spawn_agent", map[string]any{"title": "Alpha", "prompt": "a"}),
				nested("n2", "Read", map[string]any{"file_path": "/x"}),
			}},
			wsResult("n1", `Started agent "Alpha" (agent-7).`, false),
			wsResult("n2", "text", false),
			{Role: "tool", Content: []map[string]any{{"type": "tool_result", "toolUseId": "s1", "content": []map[string]any{
				{"type": "text", "text": "[Script completed · 2 calls]"},
				{"type": "text", "text": `{"toolCallId":"s1","status":"completed"}`},
			}}}},
			{Role: "assistant", Text: "Started the initial investigation."},
			{Role: "assistant", Content: []map[string]any{
				wsUse("r1", "rename_agent", map[string]any{"agent_id": "agent-1", "title": "gRPC API"}),
				wsUse("r2", "rename_agent", map[string]any{"agent_id": "agent-2", "title": "SDK switch"}),
			}},
			wsResult("r1", `Renamed agent "gRPC API" (agent-1) (was "gRPC endpoints").`, false),
			wsResult("r2", `The user renamed agent "Client switch" (agent-2), so its title stays.`, true),
		}
		h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}, nil)
		h.openOrchestrator()
		h.until("activity and rename", func() bool {
			return h.finds("Activity · 2 events") && h.hasText("Renamed 1 of 2 agents") && h.hasText("Renamed gRPC API")
		})
		h.shot("workstream-sync-07-transcript-activity-rename")
	})
}
