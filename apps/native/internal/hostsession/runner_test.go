package hostsession

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"
)

const repo = "/Users/dev/acme"

// events gathers what a Runner's subscribers hear.
type events struct {
	mu  sync.Mutex
	all []Event
}

func (e *events) add(ev Event) {
	e.mu.Lock()
	e.all = append(e.all, ev)
	e.mu.Unlock()
}

func (e *events) wait(t *testing.T, what string, match func(Event) bool) Event {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		for _, ev := range e.all {
			if match(ev) {
				e.mu.Unlock()
				return ev
			}
		}
		e.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no %s", what)
	return nil
}

// start runs a Runner against the Fake Daemon until the test ends, once it
// is connected.
func start(t *testing.T, d *fakedaemon.Daemon, addition string) (*Runner, *events) {
	t.Helper()
	r := New(Config{
		ResolveURL:           func(context.Context) (string, error) { return d.URL, nil },
		Credential:           func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: "fk-test"}, nil },
		SystemPromptAddition: func() string { return addition },
		Log:                  func(s string) { t.Log(s) },
	})
	seen := &events{}
	r.Subscribe(seen.add)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.Start(ctx)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := r.Client(); err == nil {
			return r, seen
		}
		if time.Now().After(deadline) {
			t.Fatal("the Runner did not connect")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func methods(t *testing.T, d *fakedaemon.Daemon) []string {
	t.Helper()
	reqs, err := d.Requests()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range reqs {
		out = append(out, r.Method)
	}
	return out
}

func testContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestOpenStartsTheSessionItsSpecAsksForAndHearsItsTurnEnd(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{
		Turn:  &fakedaemon.Turn{Kind: "reply", Deltas: []string{"All", " green."}, DelayMs: 20},
		Input: map[string]any{"gitRepos": map[string]any{repo: map[string]any{"currentBranch": "main", "branches": []string{"main"}}}},
	})
	r, seen := start(t, d, "Be brief.")
	opened, err := r.Open(testContext(t), Spec{
		Title:           "[Automation] Daily check",
		Cwd:             repo,
		ModelID:         "glm-5.3-flash",
		ReasoningEffort: protocol.ReasoningEffortHigh,
		AutonomyLevel:   protocol.AutonomyLevelMedium,
		Tags:            []protocol.SessionTag{{Name: "droi.automation", Metadata: map[string]string{"automationId": "daily-check"}}},
		SystemPrompt:    "Report in one line.",
		Prompt:          "Check the build.",
		Worktree:        true,
		DisabledTools:   []string{"CreateAutomation", "mcp:droi-workstream"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened.SessionID == "" || opened.WorktreePath == "" || opened.WorktreeBranch == "" || opened.ModelID == "" {
		t.Fatalf("opened %+v", opened)
	}
	init, err := d.WaitForRequest("daemon.initialize_session", 1)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		MachineID         string                `json:"machineId"`
		SessionID         string                `json:"sessionId"`
		Cwd               string                `json:"cwd"`
		Title             string                `json:"title"`
		ModelID           string                `json:"modelId"`
		ReasoningEffort   string                `json:"reasoningEffort"`
		AutonomyLevel     string                `json:"autonomyLevel"`
		Tags              []protocol.SessionTag `json:"tags"`
		DisabledToolIDs   []string              `json:"disabledToolIds"`
		Worktree          bool                  `json:"worktree"`
		WorktreeLifecycle string                `json:"worktreeLifecycle"`
		Skip              *bool                 `json:"skipPermissionsUnsafe"`
		SystemPrompt      map[string]string     `json:"systemPrompt"`
		Token             string                `json:"token"`
	}
	if err := json.Unmarshal(init.Params, &p); err != nil {
		t.Fatal(err)
	}
	wantPrompt := map[string]string{"type": "preset", "preset": "droid", "append": "Be brief.\n\nReport in one line."}
	if p.MachineID != "local" || p.SessionID != opened.SessionID || p.Cwd != repo || p.Title != "[Automation] Daily check" ||
		p.ModelID != "glm-5.3-flash" || p.ReasoningEffort != "high" || p.AutonomyLevel != "medium" ||
		!reflect.DeepEqual(p.Tags, []protocol.SessionTag{{Name: "droi.automation", Metadata: map[string]string{"automationId": "daily-check"}}}) ||
		!reflect.DeepEqual(p.DisabledToolIDs, []string{"CreateAutomation", "mcp:droi-workstream"}) ||
		!p.Worktree || p.WorktreeLifecycle != "persistent" || p.Skip != nil ||
		!reflect.DeepEqual(p.SystemPrompt, wantPrompt) || p.Token != "fk-test" {
		t.Errorf("initialize_session %s", init.Params)
	}
	msg, err := d.WaitForRequest("daemon.add_user_message", 1)
	if err != nil {
		t.Fatal(err)
	}
	var m struct{ SessionID, Text string }
	if _ = json.Unmarshal(msg.Params, &m); m.SessionID != opened.SessionID || m.Text != "Check the build." {
		t.Errorf("add_user_message %s", msg.Params)
	}

	seen.wait(t, "working state", func(e Event) bool {
		w, ok := e.(WorkingChanged)
		return ok && w.SessionID == opened.SessionID && w.State != protocol.DroidWorkingStateIdle
	})
	end := seen.wait(t, "turn end", func(e Event) bool { _, ok := e.(TurnEnded); return ok }).(TurnEnded)
	if end.SessionID != opened.SessionID || end.Reason != protocol.AgentTurnCompletionReasonCompleted || end.LastReply != "All green." || end.Error != "" {
		t.Fatalf("turn end %+v", end)
	}
	if s := r.Session(opened.SessionID); s == nil {
		t.Fatal("the Session is not followed")
	}
	if perms, questions := r.Pending(opened.SessionID); perms != 0 || questions != 0 {
		t.Fatalf("pending %d %d", perms, questions)
	}
}

func TestABlankSystemPromptIsLeftOutAndNoPromptSendsNothing(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"ok"}}})
	r, _ := start(t, d, "  \n")
	opened, err := r.Open(testContext(t), Spec{Cwd: repo, SystemPrompt: " ", SkipPermissions: true, DisableInactivityTimeout: true})
	if err != nil {
		t.Fatal(err)
	}
	init, err := d.WaitForRequest("daemon.initialize_session", 1)
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	_ = json.Unmarshal(init.Params, &p)
	if _, ok := p["systemPrompt"]; ok || p["skipPermissionsUnsafe"] != true || p["disableInactivityTimeout"] != true || p["worktree"] != nil || p["disabledToolIds"] != nil {
		t.Errorf("initialize_session %s", init.Params)
	}
	if opened.WorktreePath != "" {
		t.Errorf("opened %+v", opened)
	}
	time.Sleep(200 * time.Millisecond)
	if slices.Contains(methods(t, d), "daemon.add_user_message") {
		t.Error("a message was sent without a prompt")
	}
}

func TestTurnEndedCarriesTheTurnsFirstError(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{})
	r, seen := start(t, d, "")
	opened, err := r.Open(testContext(t), Spec{Cwd: repo})
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range []string{"Rate limited", "Gave up"} {
		if err := d.Notify(opened.SessionID, map[string]any{"type": "error", "message": msg, "errorType": "Error", "timestamp": "2026-11-05T00:00:00.000Z"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Notify(opened.SessionID, map[string]any{"type": "agent_turn_completed", "reason": "error", "tokenUsage": map[string]any{"inputTokens": 0, "outputTokens": 0, "cacheCreationTokens": 0, "cacheReadTokens": 0, "thinkingTokens": 0}}); err != nil {
		t.Fatal(err)
	}
	end := seen.wait(t, "turn end", func(e Event) bool { _, ok := e.(TurnEnded); return ok }).(TurnEnded)
	if end.Reason != protocol.AgentTurnCompletionReasonError || end.Error != "Rate limited" {
		t.Fatalf("turn end %+v", end)
	}
}

func TestArchiveInterruptsAWorkingSessionFirst(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"a", "b", "c", "d", "e", "f"}, DelayMs: 500}})
	r, seen := start(t, d, "")
	ctx := testContext(t)
	opened, err := r.Open(ctx, Spec{Cwd: repo, Prompt: "Work for a while."})
	if err != nil {
		t.Fatal(err)
	}
	seen.wait(t, "working state", func(e Event) bool {
		w, ok := e.(WorkingChanged)
		return ok && w.State != protocol.DroidWorkingStateIdle
	})
	if err := r.Archive(ctx, opened.SessionID); err != nil {
		t.Fatal(err)
	}
	got := methods(t, d)
	interrupt, archive, closing := slices.Index(got, "daemon.interrupt_session"), slices.Index(got, "daemon.archive_session"), slices.Index(got, "daemon.close_session")
	if interrupt < 0 || archive < interrupt || closing < archive {
		t.Fatalf("requests %v", got)
	}
	if r.Following(opened.SessionID) {
		t.Fatal("still following an archived Session")
	}
}

func TestCloseClosesAndStopsFollowing(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{})
	r, _ := start(t, d, "")
	ctx := testContext(t)
	opened, err := r.Open(ctx, Spec{Cwd: repo})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Following(opened.SessionID) {
		t.Fatal("not following an opened Session")
	}
	if err := r.Close(ctx, opened.SessionID); err != nil {
		t.Fatal(err)
	}
	req, err := d.WaitForRequest("daemon.close_session", 1)
	if err != nil {
		t.Fatal(err)
	}
	var p struct{ SessionID string }
	if _ = json.Unmarshal(req.Params, &p); p.SessionID != opened.SessionID {
		t.Fatalf("close_session %s", req.Params)
	}
	if r.Following(opened.SessionID) || slices.Contains(methods(t, d), "daemon.archive_session") {
		t.Fatal("Close archived, or still follows")
	}
}

func TestSendLoadsASessionTheRunnerDoesNotFollow(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "Orchestrator", Cwd: repo}},
		Turn:     &fakedaemon.Turn{Kind: "reply", Deltas: []string{"Noted."}, DelayMs: 20},
	})
	r, seen := start(t, d, "")
	id := d.Sessions[0].SessionID
	if err := r.Send(testContext(t), id, "[From agent] done"); err != nil {
		t.Fatal(err)
	}
	got := methods(t, d)
	if load, add := slices.Index(got, "daemon.load_session"), slices.Index(got, "daemon.add_user_message"); load < 0 || add < load {
		t.Fatalf("requests %v", got)
	}
	end := seen.wait(t, "turn end", func(e Event) bool { _, ok := e.(TurnEnded); return ok }).(TurnEnded)
	if end.SessionID != id || end.LastReply != "Noted." {
		t.Fatalf("turn end %+v", end)
	}
	if err := r.UpdateSettings(testContext(t), protocol.UpdateSessionSettingsParams{SessionID: id, ModelID: "glm-5.3-flash"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.WaitForRequest("daemon.update_session_settings", 1); err != nil {
		t.Fatal(err)
	}
}
