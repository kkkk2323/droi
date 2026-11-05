package workstream

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/hostsession"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/mcpserver"
)

const addition = "Answer in English."

// fixture is a Coordinator on a real Runner against the Fake Daemon, and a
// git repository for the Workspace.
type fixture struct {
	t      *testing.T
	d      *fakedaemon.Daemon
	r      *hostsession.Runner
	c      *Coordinator
	dir    string
	repo   string
	wtRoot string
	ctx    context.Context
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := filepath.Join(t.TempDir(), "pay")
	if err := os.MkdirAll(repo, 0o777); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("pay\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-q", "-m", "first")
	return repo
}

func newFixture(t *testing.T, sc fakedaemon.Scenario) *fixture {
	t.Helper()
	f := &fixture{t: t, repo: gitRepo(t), wtRoot: filepath.Join(t.TempDir(), "worktrees"), dir: filepath.Join(t.TempDir(), "workstreams")}
	if sc.Input == nil {
		sc.Input = map[string]any{}
	}
	sc.Input["gitRepos"] = map[string]any{f.repo: map[string]any{"currentBranch": "main", "branches": []string{"main"}}}
	sc.Input["defaults"] = map[string]any{"worktreeDirectory": f.wtRoot}
	f.d = fakedaemon.Start(t, sc)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	f.ctx = ctx
	f.start()
	return f
}

// start connects a new Runner and Coordinator, as a launch of Droi does.
func (f *fixture) start() {
	t := f.t
	f.r = hostsession.New(hostsession.Config{
		ResolveURL:           func(context.Context) (string, error) { return f.d.URL, nil },
		Credential:           func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: "fk-test"}, nil },
		SystemPromptAddition: func() string { return addition },
		Log:                  func(s string) { t.Log(s) },
	})
	f.c = New(Config{Dir: f.dir, Sessions: f.r, Log: func(s string) { t.Log(s) }})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	f.r.Start(ctx)
	f.c.Start(ctx)
	f.waitFor("the Runner to connect", func() bool { _, err := f.r.Client(); return err == nil })
}

func (f *fixture) waitFor(what string, cond func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			f.t.Fatalf("no %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f *fixture) requests(method string) []json.RawMessage {
	f.t.Helper()
	reqs, err := f.d.Requests()
	if err != nil {
		f.t.Fatal(err)
	}
	var out []json.RawMessage
	for _, r := range reqs {
		if r.Method == method {
			out = append(out, r.Params)
		}
	}
	return out
}

// messages are the texts the Daemon was sent for a Session.
func (f *fixture) messages(sessionID string) []string {
	var out []string
	for _, p := range f.requests("daemon.add_user_message") {
		var m struct{ SessionID, Text string }
		_ = json.Unmarshal(p, &m)
		if m.SessionID == sessionID {
			out = append(out, m.Text)
		}
	}
	return out
}

type initParams struct {
	SessionID         string                  `json:"sessionId"`
	Cwd               string                  `json:"cwd"`
	Title             string                  `json:"title"`
	ModelID           string                  `json:"modelId"`
	ReasoningEffort   string                  `json:"reasoningEffort"`
	AutonomyLevel     string                  `json:"autonomyLevel"`
	Tags              []protocol.SessionTag   `json:"tags"`
	DisabledToolIDs   []string                `json:"disabledToolIds"`
	Worktree          bool                    `json:"worktree"`
	WorktreeLifecycle string                  `json:"worktreeLifecycle"`
	SystemPrompt      struct{ Append string } `json:"systemPrompt"`
}

func (f *fixture) initialized(sessionID string) initParams {
	f.t.Helper()
	for _, p := range f.requests("daemon.initialize_session") {
		var ip initParams
		_ = json.Unmarshal(p, &ip)
		if ip.SessionID == sessionID {
			return ip
		}
	}
	f.t.Fatalf("no initialize_session of %s", sessionID)
	return initParams{}
}

func (f *fixture) create(agentModel, agentEffort string) gateway.WorkstreamCreated {
	f.t.Helper()
	made, err := f.c.Create(f.ctx, gateway.NewWorkstream{
		Title: "Payments", Workspace: f.repo, Goal: "Move the payment service to gRPC.",
		Model: "glm-5.3-flash", ReasoningEffort: "high", AgentModel: agentModel, AgentReasoningEffort: agentEffort,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return made
}

func (f *fixture) call(tool string, args map[string]any, sessionID string) (string, error) {
	f.t.Helper()
	res, err := f.c.Call(f.ctx, tool, args, sessionID)
	if err != nil {
		return "", err
	}
	if res.IsError {
		f.t.Fatalf("an error result: %+v", res)
	}
	return resultText(res), nil
}

func resultText(r mcpserver.Result) string {
	var b strings.Builder
	for _, c := range r.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

// spawn starts an agent from the orchestrator and answers its id.
func (f *fixture) spawn(orch, title, prompt string) string {
	f.t.Helper()
	before := len(f.requests("daemon.initialize_session"))
	if _, err := f.call(ToolSpawnAgent, map[string]any{"title": title, "prompt": prompt}, orch); err != nil {
		f.t.Fatal(err)
	}
	all := f.requests("daemon.initialize_session")
	if len(all) != before+1 {
		f.t.Fatalf("%d initialize_session, want %d", len(all), before+1)
	}
	var ip initParams
	_ = json.Unmarshal(all[len(all)-1], &ip)
	return ip.SessionID
}

func (f *fixture) agent(wsID, id string) Agent {
	f.c.mu.Lock()
	defer f.c.mu.Unlock()
	return *agentIn(f.c.all[wsID], id)
}

func (f *fixture) idle(id string) {
	f.t.Helper()
	f.waitFor("an idle "+id, func() bool {
		s := f.r.Session(id)
		return s != nil && !working(s)
	})
}

func reply(deltas ...string) *fakedaemon.Turn {
	return &fakedaemon.Turn{Kind: "reply", Deltas: deltas, DelayMs: 20}
}

func TestCreateStartsTheOrchestratorWithItsPromptTagsToolsAndGoal(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("Planning.")})
	made := f.create("gpt-6", "")
	shared := filepath.Join(f.dir, made.WorkstreamID, SharedDir)
	if info, err := os.Stat(shared); err != nil || !info.IsDir() {
		t.Fatalf("no shared folder: %v", err)
	}
	ip := f.initialized(made.OrchestratorID)
	wantTags := []protocol.SessionTag{{Name: "droi.workstream", Metadata: map[string]string{"workstreamId": made.WorkstreamID, "role": "orchestrator", "agentModel": "gpt-6"}}}
	wantHidden := []string{"CreateAutomation", "ListAutomations", "ReadAutomation", "EditAutomation", "DeleteAutomation", "RunAutomation", "ListAutomationTemplates", "mcp:droi-workstream/report"}
	if ip.Cwd != f.repo || ip.Title != "Payments" || ip.ModelID != "glm-5.3-flash" || ip.ReasoningEffort != "high" || ip.Worktree ||
		!reflect.DeepEqual(ip.Tags, wantTags) || !reflect.DeepEqual(ip.DisabledToolIDs, wantHidden) {
		t.Fatalf("initialize_session %+v", ip)
	}
	if want := addition + "\n\n" + OrchestratorPrompt("Payments", f.repo, shared, true); ip.SystemPrompt.Append != want {
		t.Fatalf("system prompt %q", ip.SystemPrompt.Append)
	}
	for _, part := range []string{`orchestrator of the Workstream "Payments", working in ` + f.repo, "It works in its own git worktree and branch of " + f.repo + ".", shared + " is read and written"} {
		if !strings.Contains(ip.SystemPrompt.Append, part) {
			t.Errorf("the prompt lacks %q", part)
		}
	}
	if got := f.messages(made.OrchestratorID); !reflect.DeepEqual(got, []string{"Move the payment service to gRPC."}) {
		t.Fatalf("orchestrator messages %q", got)
	}
	list, _ := f.c.List(f.ctx)
	if len(list) != 1 || list[0].ID != made.WorkstreamID || list[0].SharedFolder != shared || list[0].AgentModel != "gpt-6" || list[0].Agents == nil {
		t.Fatalf("list %+v", list)
	}
	// The record outlives the Host.
	if got := loadAll(f.dir, func(s string) { t.Log(s) })[made.WorkstreamID]; got == nil || got.OrchestratorID != made.OrchestratorID {
		t.Fatalf("record %+v", got)
	}

	for _, bad := range []gateway.NewWorkstream{{Workspace: f.repo, Goal: "g"}, {Title: "t", Goal: "g"}, {Title: "t", Workspace: f.repo}, {Title: "t", Workspace: filepath.Join(f.repo, "missing"), Goal: "g"}} {
		_, err := f.c.Create(f.ctx, bad)
		if _, refused := err.(*gateway.RefusedRequest); !refused {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestSpawnAgentStartsAnAgentInAWorktreeWithItsTask(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("On it.")})
	made := f.create("", "")
	before := len(f.requests("daemon.initialize_session"))
	text, err := f.call(ToolSpawnAgent, map[string]any{"title": "API", "prompt": "Move /pay to gRPC."}, made.OrchestratorID)
	if err != nil {
		t.Fatal(err)
	}
	all := f.requests("daemon.initialize_session")
	if len(all) != before+1 {
		t.Fatalf("%d initialize_session", len(all))
	}
	var ip initParams
	_ = json.Unmarshal(all[len(all)-1], &ip)
	id := ip.SessionID
	a := f.agent(made.WorkstreamID, id)
	// The Fake Daemon starts every Session on auto, which the agent takes
	// from the orchestrator.
	wantTags := []protocol.SessionTag{{Name: "droi.workstream", Metadata: map[string]string{
		"workstreamId": made.WorkstreamID, "role": "agent", "orchestratorId": made.OrchestratorID, "agentModel": "auto", "agentReasoningEffort": "none",
	}}}
	wantHidden := []string{"CreateAutomation", "ListAutomations", "ReadAutomation", "EditAutomation", "DeleteAutomation", "RunAutomation", "ListAutomationTemplates",
		"mcp:droi-workstream/spawn_agent", "mcp:droi-workstream/send_message", "mcp:droi-workstream/list_agents", "mcp:droi-workstream/interrupt_agent", "mcp:droi-workstream/archive_agent"}
	if ip.Cwd != f.repo || ip.Title != "API" || !ip.Worktree || ip.WorktreeLifecycle != "persistent" || ip.ModelID != "auto" || ip.ReasoningEffort != "none" ||
		ip.AutonomyLevel != "low" || !reflect.DeepEqual(ip.Tags, wantTags) || !reflect.DeepEqual(ip.DisabledToolIDs, wantHidden) {
		t.Fatalf("initialize_session %+v", ip)
	}
	shared := filepath.Join(f.dir, made.WorkstreamID, SharedDir)
	if want := addition + "\n\n" + AgentPrompt("API", "Payments", f.repo, shared, true); ip.SystemPrompt.Append != want || !strings.Contains(want, `agent "API" of the Workstream "Payments"`) {
		t.Fatalf("system prompt %q", ip.SystemPrompt.Append)
	}
	if got := f.messages(id); !reflect.DeepEqual(got, []string{"[From orchestrator]\nMove /pay to gRPC."}) {
		t.Fatalf("agent messages %q", got)
	}
	if a.WorktreePath == "" || a.Branch == "" || a.BaseBranch != "main" || a.Model != "auto" || !strings.HasPrefix(a.WorktreePath, f.wtRoot) {
		t.Fatalf("agent %+v", a)
	}
	for _, part := range []string{`Started agent "API" (` + id + `)`, a.WorktreePath, a.Branch, "Its report arrives as a message"} {
		if !strings.Contains(text, part) {
			t.Errorf("the answer %q lacks %q", text, part)
		}
	}

	// Only the orchestrator spawns, and only an agent reports.
	for _, caller := range []string{"", "someone-else", id} {
		if _, err := f.call(ToolSpawnAgent, map[string]any{"title": "X", "prompt": "Y"}, caller); err == nil || !strings.Contains(err.Error(), "Only the orchestrator") {
			t.Errorf("spawn_agent from %q: %v", caller, err)
		}
	}
	if _, err := f.call(ToolReport, map[string]any{"kind": "done", "message": "x"}, made.OrchestratorID); err == nil || !strings.Contains(err.Error(), "Only an agent") {
		t.Errorf("report from the orchestrator: %v", err)
	}
	if _, err := f.call(ToolSendMessage, map[string]any{"agent_id": "nobody", "message": "x"}, made.OrchestratorID); err == nil {
		t.Error("a message to an agent of no workstream")
	}
	if n := len(f.requests("daemon.initialize_session")); n != before+1 {
		t.Fatalf("a refused call started a Session: %d", n)
	}
}

func TestAnAgentOutsideAGitRepositoryWorksInTheWorkspace(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	plain := t.TempDir()
	made, err := f.c.Create(f.ctx, gateway.NewWorkstream{Title: "Notes", Workspace: plain, Goal: "Tidy the notes."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.initialized(made.OrchestratorID).SystemPrompt.Append, plain+" is not a git repository") {
		t.Fatal("the orchestrator is not told the workspace is no repository")
	}
	id := f.spawn(made.OrchestratorID, "Index", "Index them.")
	ip := f.initialized(id)
	if ip.Worktree || !strings.Contains(ip.SystemPrompt.Append, "You work in "+plain+", which other agents share") {
		t.Fatalf("initialize_session %+v", ip)
	}
	if a := f.agent(made.WorkstreamID, id); a.WorktreePath != "" || a.BaseBranch != "" {
		t.Fatalf("agent %+v", a)
	}
}

func TestAReportReachesTheOrchestratorAndNoNoticeFollowsIt(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"Moving", " the", " endpoint", " now."}, DelayMs: 400}})
	made := f.create("", "")
	id := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	f.waitFor("the agent to work", func() bool { return working(f.r.Session(id)) })
	text, err := f.call(ToolReport, map[string]any{"kind": "done", "message": "Moved /pay in commit abc123."}, id)
	if err != nil || text == "" {
		t.Fatal(text, err)
	}
	f.waitFor("the turn's end", func() bool { return !f.agent(made.WorkstreamID, id).Awaiting })
	time.Sleep(300 * time.Millisecond)
	want := []string{"Move the payment service to gRPC.", `[From agent "API" (` + id + `)] done` + "\nMoved /pay in commit abc123."}
	if got := f.messages(made.OrchestratorID); !reflect.DeepEqual(got, want) {
		t.Fatalf("orchestrator messages %q", got)
	}
	if r := f.agent(made.WorkstreamID, id).LastReport; r == nil || r.Kind != "done" || r.Text != "Moved /pay in commit abc123." || r.At == "" {
		t.Fatalf("last report %+v", r)
	}
	if _, err := f.call(ToolReport, map[string]any{"kind": "finished", "message": "x"}, id); err == nil {
		t.Error("a report of no known kind")
	}
}

func TestATurnWithoutAReportSendsTheNoticeAndAUsersTurnNothing(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("All", " green.")})
	made := f.create("", "")
	id := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	notice := `[From agent "API" (` + id + `)] Its turn ended without a report (completed). The start of its last reply:` + "\nAll green."
	f.waitFor("the notice", func() bool { return slices.Contains(f.messages(made.OrchestratorID), notice) })
	f.idle(id)
	// The user talks to the agent: its turn is not the orchestrator's.
	sent := len(f.messages(made.OrchestratorID))
	if err := f.r.Send(f.ctx, id, "What did you change?"); err != nil {
		t.Fatal(err)
	}
	f.waitFor("the user's turn", func() bool { return len(f.messages(id)) == 2 })
	f.waitFor("the user's turn to end", func() bool { s := f.r.Session(id); return s != nil && !working(s) && len(s.Messages()) >= 4 })
	time.Sleep(300 * time.Millisecond)
	if got := f.messages(made.OrchestratorID); len(got) != sent {
		t.Fatalf("the user's turn reached the orchestrator: %q", got[sent:])
	}

	// send_message makes it the orchestrator's again.
	if _, err := f.call(ToolSendMessage, map[string]any{"agent_id": id, "message": "Now /refund."}, made.OrchestratorID); err != nil {
		t.Fatal(err)
	}
	if got := f.messages(id); got[len(got)-1] != "[From orchestrator]\nNow /refund." {
		t.Fatalf("agent messages %q", got)
	}
	f.waitFor("a second notice", func() bool { return len(f.messages(made.OrchestratorID)) == sent+1 })
}

func TestTheOrchestratorHearsOfAFailedTurnWithItsError(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"a", "b", "c", "d"}, DelayMs: 400}})
	made := f.create("", "")
	id := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	f.waitFor("the agent to work", func() bool { return working(f.r.Session(id)) })
	if _, err := f.call(ToolReport, map[string]any{"kind": "update", "message": "Halfway."}, id); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Notify(id, map[string]any{"type": "error", "message": "Rate limited", "errorType": "Error", "timestamp": "2026-11-05T00:00:00.000Z"}); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Notify(id, map[string]any{"type": "agent_turn_completed", "reason": "error", "tokenUsage": map[string]any{"inputTokens": 0, "outputTokens": 0, "cacheCreationTokens": 0, "cacheReadTokens": 0, "thinkingTokens": 0}}); err != nil {
		t.Fatal(err)
	}
	prefix := `[From agent "API" (` + id + `)] Its turn ended without a report (error: Rate limited).`
	f.waitFor("the notice", func() bool {
		return slices.ContainsFunc(f.messages(made.OrchestratorID), func(m string) bool { return strings.HasPrefix(m, prefix) })
	})
}

func TestNewAgentSettingsMoveIdleAgentsButNotOneSwitchedByHand(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	a := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	b := f.spawn(made.OrchestratorID, "Web", "Move the web client.")
	f.waitFor("both turns to end", func() bool {
		return !f.agent(made.WorkstreamID, a).Awaiting && !f.agent(made.WorkstreamID, b).Awaiting
	})
	f.idle(a)
	f.idle(b)
	// The user picks another model in the Web agent's own Session.
	if err := f.r.UpdateSettings(f.ctx, protocol.UpdateSessionSettingsParams{SessionID: b, ModelID: "claude-opus-4-1"}); err != nil {
		t.Fatal(err)
	}
	f.waitFor("the user's model", func() bool { return f.r.Session(b).Settings().ModelID == "claude-opus-4-1" })
	before := len(f.requests("daemon.update_session_settings"))

	if err := f.c.SetAgentSettings(f.ctx, gateway.WorkstreamSettings{ID: made.WorkstreamID, AgentModel: "glm-5.3-flash", AgentReasoningEffort: "high"}); err != nil {
		t.Fatal(err)
	}
	type update struct {
		SessionID       string                `json:"sessionId"`
		ModelID         string                `json:"modelId"`
		ReasoningEffort string                `json:"reasoningEffort"`
		Tags            []protocol.SessionTag `json:"tags"`
	}
	var got []update
	for _, p := range f.requests("daemon.update_session_settings")[before:] {
		var u update
		_ = json.Unmarshal(p, &u)
		got = append(got, u)
	}
	want := []update{
		{SessionID: made.OrchestratorID, Tags: []protocol.SessionTag{{Name: "droi.workstream", Metadata: map[string]string{"workstreamId": made.WorkstreamID, "role": "orchestrator", "agentModel": "glm-5.3-flash", "agentReasoningEffort": "high"}}}},
		{SessionID: a, ModelID: "glm-5.3-flash", ReasoningEffort: "high"},
		{SessionID: a, Tags: []protocol.SessionTag{{Name: "droi.workstream", Metadata: map[string]string{"workstreamId": made.WorkstreamID, "role": "agent", "orchestratorId": made.OrchestratorID, "agentModel": "glm-5.3-flash", "agentReasoningEffort": "high"}}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("update_session_settings\n got %+v\nwant %+v", got, want)
	}
	if ag := f.agent(made.WorkstreamID, a); ag.Model != "glm-5.3-flash" || ag.ReasoningEffort != "high" {
		t.Fatalf("agent %+v", ag)
	}
	if bg := f.agent(made.WorkstreamID, b); bg.Model != "auto" {
		t.Fatalf("agent switched by hand %+v", bg)
	}
	list, _ := f.c.List(f.ctx)
	if list[0].AgentModel != "glm-5.3-flash" || list[0].AgentReasoningEffort != "high" {
		t.Fatalf("list %+v", list[0])
	}
	// Nothing changed: nothing is sent again.
	before = len(f.requests("daemon.update_session_settings"))
	if _, err := f.call(ToolSendMessage, map[string]any{"agent_id": a, "message": "Next."}, made.OrchestratorID); err != nil {
		t.Fatal(err)
	}
	if n := len(f.requests("daemon.update_session_settings")); n != before {
		t.Fatalf("%d more update_session_settings", n-before)
	}
}

// addWorktree makes the checkout the Fake Daemon only names.
func (f *fixture) addWorktree(a Agent, branch string) {
	f.t.Helper()
	run(f.t, f.repo, "worktree", "add", "-q", "-b", branch, a.WorktreePath)
}

func TestArchiveAgentRemovesACleanWorktreeAndKeepsOneWithChanges(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	clean := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	dirty := f.spawn(made.OrchestratorID, "Web", "Move the web client.")
	ca, da := f.agent(made.WorkstreamID, clean), f.agent(made.WorkstreamID, dirty)
	f.addWorktree(ca, "api")
	f.addWorktree(da, "web")
	if err := os.WriteFile(filepath.Join(da.WorktreePath, "web.go"), []byte("package web\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	f.idle(clean)
	f.idle(dirty)

	text, err := f.call(ToolArchiveAgent, map[string]any{"agent_id": clean}, made.OrchestratorID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Removed its worktree "+ca.WorktreePath) {
		t.Fatalf("answer %q", text)
	}
	cleanups := f.requests("daemon.cleanup_worktree")
	var p struct {
		WorktreePath      string `json:"worktreePath"`
		DeleteLocalBranch bool   `json:"deleteLocalBranch"`
		Force             bool   `json:"force"`
	}
	if len(cleanups) != 1 || json.Unmarshal(cleanups[0], &p) != nil || p.WorktreePath != ca.WorktreePath || p.Force || p.DeleteLocalBranch {
		t.Fatalf("cleanup_worktree %s", cleanups)
	}

	text, err = f.call(ToolArchiveAgent, map[string]any{"agent_id": dirty}, made.OrchestratorID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Kept its worktree "+da.WorktreePath) || !strings.Contains(text, "uncommitted changes") {
		t.Fatalf("answer %q", text)
	}
	if n := len(f.requests("daemon.cleanup_worktree")); n != 1 {
		t.Fatalf("%d cleanup_worktree", n)
	}
	var archived []string
	for _, p := range f.requests("daemon.archive_session") {
		var a struct{ SessionID string }
		_ = json.Unmarshal(p, &a)
		archived = append(archived, a.SessionID)
	}
	// Removing the clean one's worktree archived its Session.
	if !reflect.DeepEqual(archived, []string{dirty}) {
		t.Fatalf("archived %v", archived)
	}
	if f.agent(made.WorkstreamID, clean).ArchivedAt == "" {
		t.Fatal("the record is not archived")
	}
	if _, err := f.call(ToolSendMessage, map[string]any{"agent_id": clean, "message": "x"}, made.OrchestratorID); err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("a message to an archived agent: %v", err)
	}
	if _, err := f.call(ToolReport, map[string]any{"kind": "done", "message": "x"}, clean); err == nil {
		t.Fatal("an archived agent reported")
	}
}

func TestArchivingAWorkstreamArchivesItsAgentsAndOrchestratorAndNamesTheKeptWorktrees(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	committed := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	ag := f.agent(made.WorkstreamID, committed)
	f.addWorktree(ag, "api")
	if err := os.WriteFile(filepath.Join(ag.WorktreePath, "api.go"), []byte("package api\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	run(t, ag.WorktreePath, "add", ".")
	run(t, ag.WorktreePath, "commit", "-q", "-m", "api")
	f.idle(committed)
	kept, err := f.c.Archive(f.ctx, made.WorkstreamID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kept, []string{ag.WorktreePath}) || len(f.requests("daemon.cleanup_worktree")) != 0 {
		t.Fatalf("kept %v", kept)
	}
	var archived []string
	for _, p := range f.requests("daemon.archive_session") {
		var a struct{ SessionID string }
		_ = json.Unmarshal(p, &a)
		archived = append(archived, a.SessionID)
	}
	if !reflect.DeepEqual(archived, []string{committed, made.OrchestratorID}) {
		t.Fatalf("archived %v", archived)
	}
	if _, err := os.Stat(filepath.Join(f.dir, made.WorkstreamID, SharedDir)); err != nil {
		t.Fatal("the shared folder went")
	}
	if list, _ := f.c.List(f.ctx); list[0].ArchivedAt == "" {
		t.Fatal("not archived")
	}
	if _, err := f.call(ToolSpawnAgent, map[string]any{"title": "X", "prompt": "Y"}, made.OrchestratorID); err == nil {
		t.Fatal("an archived orchestrator spawned")
	}
	if _, err := f.c.Archive(f.ctx, made.WorkstreamID); err == nil {
		t.Fatal("archived twice")
	}
}

func TestAfterARestartTheOrchestratorHearsOfAnIdleAgentItWaitedOn(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{
			{Title: "Payments", Cwd: "/w/pay", Messages: []fakedaemon.Message{{Role: "user", Text: "Move it."}}},
			{Title: "API", Cwd: "/w/pay", Messages: []fakedaemon.Message{{Role: "user", Text: "[From orchestrator]\nMove /pay."}, {Role: "assistant", Text: "Halfway there."}}},
			{Title: "Web", Cwd: "/w/pay", Messages: []fakedaemon.Message{{Role: "assistant", Text: "Done earlier."}}},
		},
		Turn: reply("Noted."),
	})
	orch, api, web := f.d.Sessions[0].SessionID, f.d.Sessions[1].SessionID, f.d.Sessions[2].SessionID
	w := &Workstream{ID: "payments-abc123", Title: "Payments", Workspace: "/w/pay", OrchestratorID: orch, CreatedAt: "2026-11-05T10:00:00Z", Agents: []*Agent{
		{SessionID: api, Title: "API", CreatedAt: "2026-11-05T10:01:00Z", Awaiting: true},
		{SessionID: web, Title: "Web", CreatedAt: "2026-11-05T10:02:00Z"},
	}}
	if err := os.MkdirAll(filepath.Join(f.dir, w.ID), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := save(f.dir, w); err != nil {
		t.Fatal(err)
	}
	f.start()
	want := `[From agent "API" (` + api + `)] Its turn ended without a report (Droi restarted while it worked). The start of its last reply:` + "\nHalfway there."
	f.waitFor("the restart notice", func() bool { return slices.Contains(f.messages(orch), want) })
	time.Sleep(300 * time.Millisecond)
	if got := f.messages(orch); len(got) != 1 {
		t.Fatalf("orchestrator messages %q", got)
	}
	if f.agent(w.ID, api).Awaiting {
		t.Fatal("still awaiting")
	}
}

func TestListAgentsShowsStatusWorktreeAndLastReport(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	id := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	f.idle(id)
	if _, err := f.call(ToolReport, map[string]any{"kind": "blocked", "message": strings.Repeat("x", 400)}, id); err != nil {
		t.Fatal(err)
	}
	text, err := f.call(ToolListAgents, nil, made.OrchestratorID)
	if err != nil {
		t.Fatal(err)
	}
	var got []listedAgent
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(text, err)
	}
	a := f.agent(made.WorkstreamID, id)
	if len(got) != 1 || got[0].AgentID != id || got[0].Status != "idle" || got[0].Worktree != a.WorktreePath || got[0].Branch != a.Branch ||
		got[0].LastActivity == "" || got[0].LastReport == nil || got[0].LastReport.Kind != "blocked" || len(got[0].LastReport.Message) != 300 {
		t.Fatalf("list_agents %s", text)
	}
}
