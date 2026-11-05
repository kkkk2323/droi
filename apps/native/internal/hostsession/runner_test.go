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
	"github.com/kkkk2323/droid-sdk-go/controller"
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

func (e *events) count(match func(Event) bool) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, ev := range e.all {
		if match(ev) {
			n++
		}
	}
	return n
}

func isTurnEnd(e Event) bool { _, ok := e.(TurnEnded); return ok }

// start runs a Runner against the Fake Daemon until the test ends, once it
// is connected.
func start(t *testing.T, d *fakedaemon.Daemon, addition string, mods ...func(*Config)) (*Runner, *events) {
	t.Helper()
	cfg := Config{
		ResolveURL:           func(context.Context) (string, error) { return d.URL, nil },
		Credential:           func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: "fk-test"}, nil },
		SystemPromptAddition: func() string { return addition },
		Log:                  func(s string) { t.Log(s) },
	}
	for _, mod := range mods {
		mod(&cfg)
	}
	r := New(cfg)
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
	// The Daemon takes one connection's requests in order: one sent after
	// Open is recorded after any Open sent.
	if err := r.Close(testContext(t), opened.SessionID); err != nil {
		t.Fatal(err)
	}
	got := methods(t, d)
	if !slices.Contains(got, "daemon.close_session") || slices.Contains(got, "daemon.add_user_message") {
		t.Errorf("a message was sent without a prompt: %v", got)
	}
}

func TestSkipPermissionsIsKeptForTheWorkersAReloadSpawns(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"ok"}}})
	r, _ := start(t, d, "")
	if _, err := r.Open(testContext(t), Spec{Cwd: repo, Prompt: "Go.", SkipPermissions: true, DisableInactivityTimeout: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.WaitForRequest("daemon.add_user_message", 1); err != nil {
		t.Fatal(err)
	}
	reqs, err := d.Requests()
	if err != nil {
		t.Fatal(err)
	}
	load := slices.IndexFunc(reqs, func(q fakedaemon.Request) bool { return q.Method == "daemon.load_session" })
	add := slices.IndexFunc(reqs, func(q fakedaemon.Request) bool { return q.Method == "daemon.add_user_message" })
	if load < 0 || add < load {
		t.Fatalf("requests %v", methods(t, d))
	}
	var p map[string]any
	_ = json.Unmarshal(reqs[load].Params, &p)
	if p["skipPermissionsUnsafe"] != true || p["disableInactivityTimeout"] != true {
		t.Fatalf("load_session %s", reqs[load].Params)
	}
}

func TestLastReplyIsThisTurnsOnly(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"First."}, DelayMs: 300}})
	r, seen := start(t, d, "")
	ctx := testContext(t)
	opened, err := r.Open(ctx, Spec{Cwd: repo, Prompt: "One."})
	if err != nil {
		t.Fatal(err)
	}
	if end := seen.wait(t, "first turn end", isTurnEnd).(TurnEnded); end.LastReply != "First." {
		t.Fatalf("first turn %+v", end)
	}
	// Interrupted before it streams a word: a turn with no text.
	if err := r.Send(ctx, opened.SessionID, "Two."); err != nil {
		t.Fatal(err)
	}
	if err := r.Interrupt(ctx, opened.SessionID); err != nil {
		t.Fatal(err)
	}
	end := seen.wait(t, "cancelled turn end", func(e Event) bool {
		t, ok := e.(TurnEnded)
		return ok && t.Reason == protocol.AgentTurnCompletionReasonCancelled
	}).(TurnEnded)
	if end.LastReply != "" {
		t.Fatalf("a turn without text replied %q", end.LastReply)
	}
}

func TestTurnEndedCarriesTheTurnsFirstError(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{})
	r, seen := start(t, d, "")
	opened, err := r.Open(testContext(t), Spec{Cwd: repo})
	if err != nil {
		t.Fatal(err)
	}
	notify(t, d, opened.SessionID, map[string]any{"type": "error", "message": "Between turns", "errorType": "Error", "timestamp": "2026-11-05T00:00:00.000Z"})
	notify(t, d, opened.SessionID, working("streaming_assistant_message"))
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

func notify(t *testing.T, d *fakedaemon.Daemon, sessionID string, n map[string]any) {
	t.Helper()
	if err := d.Notify(sessionID, n); err != nil {
		t.Fatal(err)
	}
}

func working(state string) map[string]any {
	return map[string]any{"type": "droid_working_state_changed", "newState": state}
}

func workingIn(state protocol.DroidWorkingState) func(Event) bool {
	return func(e Event) bool { w, ok := e.(WorkingChanged); return ok && w.State == state }
}

func isLostTurn(e Event) bool {
	t, ok := e.(TurnEnded)
	return ok && t.Reason == protocol.AgentTurnCompletionReasonProcessExit && t.Error == WorkerStopped
}

// isUnseenTurn is the end of a turn that ended while the Runner was not
// connected: a load cannot tell a completed turn from a lost worker.
func isUnseenTurn(e Event) bool {
	t, ok := e.(TurnEnded)
	return ok && t.Reason == protocol.AgentTurnCompletionReasonProcessExit && t.Error == LostTouch
}

// The Fake Daemon has no worker to kill: a Notify marks the Session working
// with no turn behind it, so the reload after the Daemon comes back finds it
// idle, as a real Daemon that lost the worker would.
func TestATurnLostToADaemonRestartEndsOnceWhenTheSessionIsLoadedAgain(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{})
	r, seen := start(t, d, "")
	opened, err := r.Open(testContext(t), Spec{Cwd: repo})
	if err != nil {
		t.Fatal(err)
	}
	notify(t, d, opened.SessionID, working("streaming_assistant_message"))
	seen.wait(t, "working state", workingIn(protocol.DroidWorkingStateStreamingAssistantMessage))
	if err := d.GoDown(); err != nil {
		t.Fatal(err)
	}
	if err := d.ComeBack(); err != nil {
		t.Fatal(err)
	}
	end := seen.wait(t, "lost turn end", isUnseenTurn).(TurnEnded)
	if end.SessionID != opened.SessionID || end.Error != "the Host lost touch with the Daemon while the Session worked" {
		t.Fatalf("turn end %+v", end)
	}
	// Events come in order: once this one is heard, a second end would be too.
	notify(t, d, opened.SessionID, working("idle"))
	seen.wait(t, "idle", workingIn(protocol.DroidWorkingStateIdle))
	if n := seen.count(isTurnEnd); n != 1 {
		t.Fatalf("%d turn ends", n)
	}
}

// The Fake Daemon refuses to send session_process_exited (its notification
// schema leaves it out), so the Controller's events are given to the Runner
// directly.
func TestATurnWhoseWorkerExitedEndsOnce(t *testing.T) {
	r := New(Config{})
	seen := &events{}
	r.Subscribe(seen.add)
	r.follow("s", true)
	r.see(controller.SessionNotification{SessionID: "s", Value: &protocol.DroidWorkingStateChangedNotification{NewState: protocol.DroidWorkingStateExecutingTool}})
	r.see(controller.SessionNotification{SessionID: "s", Value: &protocol.CreateMessageNotification{Message: protocol.FactoryDroidMessage{
		Role: protocol.MessageRoleAssistant, Content: []protocol.ContentBlock{textBlock(t, "Running the tests.")},
	}}})
	for range 2 {
		r.see(controller.SessionNotification{SessionID: "s", Value: &protocol.SessionProcessExitedNotification{Message: "exited"}})
	}
	if n := seen.count(isTurnEnd); n != 1 {
		t.Fatalf("%d turn ends", n)
	}
	if end := seen.all[1].(TurnEnded); !isLostTurn(end) || end.SessionID != "s" || end.LastReply != "Running the tests." {
		t.Fatalf("turn end %+v", end)
	}
}

// A followed Session the Daemon no longer has ends its turn, once, and is no
// longer followed.
func TestASessionTheDaemonNoLongerHasEnds(t *testing.T) {
	r := New(Config{})
	seen := &events{}
	r.Subscribe(seen.add)
	r.follow("s", true)
	r.see(controller.SessionNotification{SessionID: "s", Value: &protocol.DroidWorkingStateChangedNotification{NewState: protocol.DroidWorkingStateThinking}})
	r.see(controller.SessionNotFound{SessionID: "s"})
	r.see(controller.SessionNotFound{SessionID: "s"})
	r.see(controller.SessionNotFound{SessionID: "never-followed"})
	if n := seen.count(isTurnEnd); n != 1 {
		t.Fatalf("%d turn ends", n)
	}
	end := seen.all[1].(TurnEnded)
	if end.SessionID != "s" || end.Reason != protocol.AgentTurnCompletionReasonProcessExit || end.Error != "the Daemon no longer has this Session" {
		t.Fatalf("turn end %+v", end)
	}
	if r.Following("s") {
		t.Fatal("still following a Session the Daemon does not have")
	}
}

func textBlock(t *testing.T, text string) protocol.ContentBlock {
	t.Helper()
	b, err := protocol.NewContentBlock(&protocol.TextBlock{Type: "text", Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// reconnectsAndKeepsFollowing shows a followed Session's notifications
// reaching the Runner again after the Daemon went down and came back.
func reconnectsAndKeepsFollowing(t *testing.T, mods ...func(*Config)) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"Back."}, DelayMs: 20}})
	r, seen := start(t, d, "", mods...)
	ctx := testContext(t)
	opened, err := r.Open(ctx, Spec{Cwd: repo})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.GoDown(); err != nil {
		t.Fatal(err)
	}
	if err := d.ComeBack(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.WaitForRequest("daemon.load_session", 1); err != nil {
		t.Fatal("the followed Session was not loaded again")
	}
	// A notification needs no call that would load the Session first.
	notify(t, d, opened.SessionID, working("thinking"))
	seen.wait(t, "working state after the reconnect", workingIn(protocol.DroidWorkingStateThinking))
	notify(t, d, opened.SessionID, working("idle"))
	if err := r.Send(ctx, opened.SessionID, "Still there?"); err != nil {
		t.Fatal(err)
	}
	end := seen.wait(t, "turn end after the reconnect", func(e Event) bool {
		t, ok := e.(TurnEnded)
		return ok && t.Reason == protocol.AgentTurnCompletionReasonCompleted
	}).(TurnEnded)
	if end.LastReply != "Back." {
		t.Fatalf("turn end %+v", end)
	}
}

func TestFollowedSessionsAreLoadedAgainAfterTheControllersReconnect(t *testing.T) {
	reconnectsAndKeepsFollowing(t)
}

func TestFollowedSessionsAreLoadedAgainAfterTheRunnersOwnConnect(t *testing.T) {
	reconnectsAndKeepsFollowing(t, func(c *Config) { c.maxReconnectAttempts = -1 })
}

func TestArchiveLoadsASessionTheRunnerDoesNotFollow(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Agent", Cwd: repo}}})
	r, _ := start(t, d, "")
	id := d.Sessions[0].SessionID
	if err := r.Archive(testContext(t), id); err != nil {
		t.Fatal(err)
	}
	got := methods(t, d)
	if load, archive := slices.Index(got, "daemon.load_session"), slices.Index(got, "daemon.archive_session"); load < 0 || archive < load {
		t.Fatalf("requests %v", got)
	}
	if r.Following(id) {
		t.Fatal("still following an archived Session")
	}
}

func TestNoConnectWithoutACredential(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{})
	var mu sync.Mutex
	signedIn := false
	var poke func()
	r := New(Config{
		ResolveURL:    func(context.Context) (string, error) { return d.URL, nil },
		Credential:    func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: "fk-test"}, nil },
		HasCredential: func() bool { mu.Lock(); defer mu.Unlock(); return signedIn },
		OnChange: func(fn func()) func() {
			mu.Lock()
			poke = fn
			mu.Unlock()
			return func() {}
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.Start(ctx)
	time.Sleep(300 * time.Millisecond)
	if _, err := r.Client(); err == nil {
		t.Fatal("connected without a credential")
	}
	mu.Lock()
	signedIn = true
	fn := poke
	mu.Unlock()
	fn()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := r.Client(); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no connect after the sign-in")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// turnOutcomes answers get_session_turn_outcome for a Runner's test.
type turnOutcomes struct {
	mu    sync.Mutex
	asked []string
	reply func(sessionID, turnID string) (*protocol.DaemonGetSessionTurnOutcomeResult, error)
}

func (o *turnOutcomes) get(_ context.Context, p protocol.GetSessionTurnOutcomeParams) (*protocol.DaemonGetSessionTurnOutcomeResult, error) {
	o.mu.Lock()
	o.asked = append(o.asked, p.SessionID+"/"+p.TurnID)
	o.mu.Unlock()
	return o.reply(p.SessionID, p.TurnID)
}

func available(t *testing.T, sessionID, turnID string, reason protocol.AgentTurnCompletionReason, final string) *protocol.DaemonGetSessionTurnOutcomeResult {
	t.Helper()
	outcome, err := protocol.NewAgentTurnOutcome(&protocol.AgentTurnOutcomeText{Type: "agent_turn_outcome", TurnID: turnID, Reason: reason, ResultKind: "text"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := protocol.NewDaemonGetSessionTurnOutcomeResult(&protocol.DaemonGetSessionTurnOutcomeResultAvailable{
		Status: "available", SessionID: sessionID, TurnID: turnID, Outcome: outcome, FinalOutput: final,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &res
}

// inTurn is a Runner following Session s in a turn its prompt turnID
// started.
func inTurn(t *testing.T, o *turnOutcomes, turnID string) (*Runner, *events) {
	t.Helper()
	r := New(Config{turnOutcome: o.get})
	seen := &events{}
	r.Subscribe(seen.add)
	r.follow("s", true)
	r.mu.Lock()
	r.following["s"].turnID = turnID
	r.mu.Unlock()
	r.see(controller.SessionNotification{SessionID: "s", Value: &protocol.DroidWorkingStateChangedNotification{NewState: protocol.DroidWorkingStateStreamingAssistantMessage}})
	return r, seen
}

// A turn a load finds over ends as the Daemon recorded it
// (daemon.get_session_turn_outcome, droid 1.4.1), with its final output.
func TestATurnALoadFindsOverEndsAsTheDaemonRecordedIt(t *testing.T) {
	for _, c := range []struct {
		reason protocol.AgentTurnCompletionReason
		final  string
	}{
		{protocol.AgentTurnCompletionReasonCompleted, "All green."},
		{protocol.AgentTurnCompletionReasonError, ""},
		{protocol.AgentTurnCompletionReasonCancelled, ""},
	} {
		o := &turnOutcomes{reply: func(s, turn string) (*protocol.DaemonGetSessionTurnOutcomeResult, error) {
			return available(t, s, turn, c.reason, c.final), nil
		}}
		r, seen := inTurn(t, o, "m-1")
		r.see(controller.SessionLoaded{SessionID: "s", Result: &protocol.LoadSessionResult{}})
		end := seen.wait(t, "turn end", isTurnEnd).(TurnEnded)
		if end.SessionID != "s" || end.Reason != c.reason || end.Error != "" || end.LastReply != c.final {
			t.Errorf("%s: turn end %+v", c.reason, end)
		}
		if o.asked[0] != "s/m-1" {
			t.Errorf("asked %v", o.asked)
		}
	}
}

// Without the Daemon's record of the turn (an older droid, or one it could
// not verify), the turn ends as lost touch.
func TestATurnALoadFindsOverWithoutARecordIsLostTouch(t *testing.T) {
	for name, reply := range map[string]func(t *testing.T, s, turn string) (*protocol.DaemonGetSessionTurnOutcomeResult, error){
		"refused": func(*testing.T, string, string) (*protocol.DaemonGetSessionTurnOutcomeResult, error) {
			return nil, &droid.RPCError{Code: -32601, Message: "Method not found"}
		},
		"unavailable": func(t *testing.T, s, turn string) (*protocol.DaemonGetSessionTurnOutcomeResult, error) {
			res, err := protocol.NewDaemonGetSessionTurnOutcomeResult(&protocol.DaemonGetSessionTurnOutcomeResultUnavailable{
				Status: "unavailable", SessionID: s, TurnID: turn, Reason: "turn_unverified",
			})
			return &res, err
		},
		"another turn's": func(t *testing.T, s, _ string) (*protocol.DaemonGetSessionTurnOutcomeResult, error) {
			return available(t, s, "m-0", protocol.AgentTurnCompletionReasonCompleted, "old"), nil
		},
	} {
		o := &turnOutcomes{reply: func(s, turn string) (*protocol.DaemonGetSessionTurnOutcomeResult, error) { return reply(t, s, turn) }}
		r, seen := inTurn(t, o, "m-1")
		r.see(controller.SessionLoaded{SessionID: "s", Result: &protocol.LoadSessionResult{}})
		if end := seen.wait(t, "turn end", isTurnEnd).(TurnEnded); !isUnseenTurn(end) {
			t.Errorf("%s: turn end %+v", name, end)
		}
	}
	// No prompt of the Runner's started the turn: nothing to ask.
	o := &turnOutcomes{reply: func(string, string) (*protocol.DaemonGetSessionTurnOutcomeResult, error) { panic("asked") }}
	r, seen := inTurn(t, o, "")
	r.see(controller.SessionLoaded{SessionID: "s", Result: &protocol.LoadSessionResult{}})
	if end := seen.wait(t, "turn end", isTurnEnd).(TurnEnded); !isUnseenTurn(end) {
		t.Errorf("turn end %+v", end)
	}
}

// Open sends its prompt with a message id of its own, the turn's id the
// Daemon records its outcome under.
func TestOpenNamesItsPromptsTurn(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"ok"}}})
	r, seen := start(t, d, "")
	if _, err := r.Open(testContext(t), Spec{Cwd: repo, Prompt: "Report."}); err != nil {
		t.Fatal(err)
	}
	seen.wait(t, "turn end", isTurnEnd)
	reqs, err := d.Requests()
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		MessageID string `json:"messageId"`
	}
	for _, req := range reqs {
		if req.Method == "daemon.add_user_message" {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(p.MessageID) != 36 {
		t.Fatalf("messageId %q", p.MessageID)
	}
}

// Subscribers hear a gone Session's end while the Runner still follows it,
// so that one checking Following meanwhile does not end it a second time.
func TestAGoneSessionIsFollowedUntilItsEndIsHeard(t *testing.T) {
	r := New(Config{})
	var followedThen []bool
	r.Subscribe(func(e Event) {
		if _, ok := e.(TurnEnded); ok {
			followedThen = append(followedThen, r.Following("s"))
		}
	})
	r.follow("s", true)
	r.see(controller.SessionNotFound{SessionID: "s"})
	if !slices.Equal(followedThen, []bool{true}) || r.Following("s") {
		t.Fatalf("followed while heard %v, after %v", followedThen, r.Following("s"))
	}
}

// A Load that fails stops following only a Session it began to follow.
func TestAFailedLoadKeepsAFollowItDidNotAdd(t *testing.T) {
	r := New(Config{})
	r.follow("s", true)
	if err := r.Load(context.Background(), "s"); err == nil {
		t.Fatal("loaded without a connection")
	}
	if !r.Following("s") {
		t.Fatal("a failed Load stopped following a followed Session")
	}
	if err := r.Load(context.Background(), "t"); err == nil || r.Following("t") {
		t.Fatalf("Load of t: %v, following %v", err, r.Following("t"))
	}
}

// A reload that keeps failing other than for a Session the Daemon does not
// have is tried again, and after reloadTries ends the turn as lost touch.
func TestAReloadThatKeepsFailingEndsTheTurn(t *testing.T) {
	r := New(Config{reloadRetry: 10 * time.Millisecond})
	seen := &events{}
	r.Subscribe(seen.add)
	r.follow("s", true)
	r.follow("idle", true)
	r.see(controller.SessionNotification{SessionID: "s", Value: &protocol.DroidWorkingStateChangedNotification{NewState: protocol.DroidWorkingStateThinking}})
	r.reloadFollowed(testContext(t))
	end := seen.wait(t, "lost turn", isUnseenTurn).(TurnEnded)
	if end.SessionID != "s" {
		t.Fatalf("turn end %+v", end)
	}
	time.Sleep(100 * time.Millisecond)
	if n := seen.count(isTurnEnd); n != 1 || !r.Following("idle") {
		t.Fatalf("%d turn ends, idle followed %v", n, r.Following("idle"))
	}
}
