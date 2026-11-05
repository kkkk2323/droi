package workstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/hostsession"
	"github.com/kkkk2323/droi/apps/native/internal/memory"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/mcpserver"
)

const addition = "Answer in English."

// fixture is a Coordinator on a real Runner against the Fake Daemon, and a
// git repository for the Workspace. The Runner reaches the Daemon through a
// proxy that can answer requests itself, as a real Daemon answers for a
// Session it no longer has.
type fixture struct {
	t      *testing.T
	d      *fakedaemon.Daemon
	r      *hostsession.Runner
	c      *Coordinator
	dir    string
	repo   string
	wtRoot string
	ctx    context.Context
	proxy  string
	// sessionsDir stands for the Daemon's sessions folder.
	sessionsDir string

	mu sync.Mutex
	// refuse, when set, answers a request in the Daemon's place.
	refuse func(method, sessionID string) *droid.RPCError
	// swallow, when set, takes a request as done without the Daemon, such
	// as a message to a Session that is to run no turn of the Fake
	// Daemon's.
	swallow func(method, sessionID string) bool
	// gone are the Sessions archived from a Client: the Daemon no longer
	// loads or archives them.
	gone    map[string]bool
	refused []string
	logs    []string
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
	f := &fixture{t: t, repo: gitRepo(t), wtRoot: filepath.Join(t.TempDir(), "worktrees"), dir: filepath.Join(t.TempDir(), "workstreams"), sessionsDir: t.TempDir(), gone: map[string]bool{}}
	if sc.Input == nil {
		sc.Input = map[string]any{}
	}
	// The Fake Daemon makes worktrees only of the folders it is told are
	// repositories: the repository, and a folder in it.
	known := map[string]any{"currentBranch": "main", "branches": []string{"main"}}
	sc.Input["gitRepos"] = map[string]any{f.repo: known, filepath.Join(f.repo, "svc", "pay"): known}
	sc.Input["defaults"] = map[string]any{"worktreeDirectory": f.wtRoot}
	f.d = fakedaemon.Start(t, sc)
	srv := httptest.NewServer(http.HandlerFunc(f.serveProxy))
	t.Cleanup(srv.Close)
	f.proxy = "ws" + strings.TrimPrefix(srv.URL, "http") + "/daemon"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	f.ctx = ctx
	f.start()
	return f
}

// serveProxy carries one connection between the Runner and the Fake Daemon.
func (f *fixture) serveProxy(w http.ResponseWriter, r *http.Request) {
	header := http.Header{}
	for k, v := range r.Header {
		switch k := strings.ToLower(k); {
		case strings.HasPrefix(k, "sec-websocket"), k == "connection", k == "upgrade":
		default:
			header[http.CanonicalHeaderKey(k)] = v
		}
	}
	up, _, err := websocket.Dial(r.Context(), f.d.URL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer up.CloseNow()
	down, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer down.CloseNow()
	up.SetReadLimit(-1)
	down.SetReadLimit(-1)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		defer cancel()
		for {
			typ, b, err := up.Read(ctx)
			if err != nil || down.Write(ctx, typ, b) != nil {
				return
			}
		}
	}()
	for {
		typ, b, err := down.Read(ctx)
		if err != nil {
			return
		}
		var req struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params struct {
				SessionID string `json:"sessionId"`
			} `json:"params"`
		}
		_ = json.Unmarshal(b, &req)
		if answer := f.answer(req.Method, req.Params.SessionID); answer != nil && req.ID != "" {
			answer["type"], answer["jsonrpc"], answer["id"] = "response", "2.0", req.ID
			out, _ := json.Marshal(answer)
			if down.Write(ctx, websocket.MessageText, out) != nil {
				return
			}
			continue
		}
		if up.Write(ctx, typ, b) != nil {
			return
		}
	}
}

// answer is the proxy's own answer to a request; nil to pass it on.
func (f *fixture) answer(method, sessionID string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.swallow != nil && f.swallow(method, sessionID) {
		return map[string]any{"result": map[string]any{}}
	}
	var e *droid.RPCError
	switch {
	case method == "daemon.get_session_turn_outcome":
		// The Fake Daemon keeps no turn outcomes, and stops at a method it
		// has no handler for: answer as a droid before 1.4.1 does.
		e = &droid.RPCError{Code: -32601, Message: "Method not found"}
	case f.gone[sessionID] && (method == "daemon.load_session" || method == "daemon.archive_session"):
		e = &droid.RPCError{Code: -32004, Message: "Session not found"}
	case f.refuse != nil:
		e = f.refuse(method, sessionID)
	}
	if e == nil {
		return nil
	}
	f.refused = append(f.refused, method+" "+sessionID)
	return map[string]any{"error": e}
}

// quietAgents keeps the Fake Daemon from running a turn for a message to
// any Session but the orchestrator, whose turns a test then makes up.
func (f *fixture) quietAgents(orch string) {
	f.mu.Lock()
	f.swallow = func(method, id string) bool { return method == "daemon.add_user_message" && id != orch }
	f.mu.Unlock()
}

func (f *fixture) setRefuse(fn func(method, sessionID string) *droid.RPCError) {
	f.mu.Lock()
	f.refuse = fn
	f.mu.Unlock()
}

// archiveFromClient archives a Session as a Client of the Daemon does.
func (f *fixture) archiveFromClient(id string) {
	f.t.Helper()
	cl, err := f.r.Client()
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := cl.ArchiveSession(f.ctx, protocol.ArchiveSessionParams{SessionID: id}); err != nil {
		f.t.Fatal(err)
	}
	f.mu.Lock()
	f.gone[id] = true
	f.mu.Unlock()
}

// start connects a new Runner and Coordinator, as a launch of Droi does.
func (f *fixture) start() {
	t := f.t
	f.r = hostsession.New(hostsession.Config{
		ResolveURL:           func(context.Context) (string, error) { return f.proxy, nil },
		Credential:           func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: "fk-test"}, nil },
		SystemPromptAddition: func() string { return addition },
		Log:                  func(s string) { t.Log(s) },
	})
	f.c = New(Config{Dir: f.dir, SessionsDir: f.sessionsDir, Sessions: f.r, Log: func(s string) {
		t.Log(s)
		f.mu.Lock()
		f.logs = append(f.logs, s)
		f.mu.Unlock()
	}})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	// Closed before the Runner goes, so that no line of work outlives the
	// test.
	t.Cleanup(f.c.Close)
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

// sessionIDs are the Sessions of method's requests, in order.
func (f *fixture) sessionIDs(method string) []string {
	var out []string
	for _, p := range f.requests(method) {
		var a struct{ SessionID string }
		_ = json.Unmarshal(p, &a)
		out = append(out, a.SessionID)
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
	SessionID          string                  `json:"sessionId"`
	Cwd                string                  `json:"cwd"`
	Title              string                  `json:"title"`
	ModelID            string                  `json:"modelId"`
	ReasoningEffort    string                  `json:"reasoningEffort"`
	AutonomyLevel      string                  `json:"autonomyLevel"`
	Tags               []protocol.SessionTag   `json:"tags"`
	DisabledToolIDs    []string                `json:"disabledToolIds"`
	Worktree           bool                    `json:"worktree"`
	WorktreeLifecycle  string                  `json:"worktreeLifecycle"`
	WorktreeNewBranch  string                  `json:"worktreeNewBranch"`
	WorktreeBaseBranch string                  `json:"worktreeBaseBranch"`
	SystemPrompt       struct{ Append string } `json:"systemPrompt"`
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

// long is a turn of about n half-seconds.
func long(n int) *fakedaemon.Turn {
	return &fakedaemon.Turn{Kind: "reply", Deltas: slices.Repeat([]string{"."}, n), DelayMs: 500}
}

func turnCompleted(reason string) map[string]any {
	return map[string]any{"type": "agent_turn_completed", "reason": reason, "tokenUsage": map[string]any{"inputTokens": 0, "outputTokens": 0, "cacheCreationTokens": 0, "cacheReadTokens": 0, "thinkingTokens": 0}}
}

// The prompts and their parts as workstream-prompts.md has them.

func wantOrchestratorPrompt(title, workspace, shared, where string) string {
	return `# You lead a Droi Workstream

You are the orchestrator of the Workstream "` + title + `", working in ` + workspace + `. You plan the work, split it among agents, and keep the user informed. You do not edit code or run builds yourself; read files only as much as planning needs.

## Agents

- An agent is a Droid session of its own. ` + where + ` It keeps its context for the life of the Workstream, so give one agent one area of work (a service, a layer, a kind of task) and keep sending it the work in that area.
- spawn_agent starts one. Give it a short title naming its area, and a prompt that states the area, the first task, the constraints (what not to touch, conventions, which tests to run) and what done means. The agent knows only what the prompt and the shared folder tell it.
- send_message gives an existing agent more work or an answer. Prefer it to a new agent for the same area.
- After spawn_agent or send_message, end your turn. Agents report on their own: reports arrive as messages headed [From agent "<title>" (<id>)], and Droi also tells you when an agent's turn ends without a report. Do not call list_agents in a loop to wait.
- list_agents shows each agent's state (working, idle, or waiting for the user: an approval or a question the user answers in that agent's session), its worktree and branch, and its last report.
- interrupt_agent stops an agent's current turn. archive_agent ends an agent whose work is merged or no longer needed; Droi removes its worktree when it holds nothing unmerged.

## Shared folder

` + shared + ` is read and written by you and every agent. Keep the plan, the decisions, the interfaces agents agree on and the hand-over notes there, as files with clear names, instead of repeating them in every message.

## Recurring checks

To check something on a schedule (a deployment, a CI run, a review), use the Loop tool in this session.

## The user

Answer the user's messages directly. When agents report, summarize briefly: what is done, what is next, what needs the user. Ask the user when a decision is theirs (scope, trade-offs, anything destructive or outside this computer, such as pushing, merging or deploying), and do not let agents do those without the user's go-ahead.`
}

func orchestratorInRepo(workspace string) string {
	return "It works in its own git worktree and branch of " + workspace + "."
}

func orchestratorOutsideRepo(workspace string) string {
	return workspace + " is not a git repository, so every agent works in it directly; give agents areas that do not touch the same files."
}

func wantAgentPrompt(title, workstream, shared, where string) string {
	return `# You are an agent of a Droi Workstream

You are the agent "` + title + `" of the Workstream "` + workstream + `". An orchestrator session leads it: it sends you work in messages headed [From orchestrator] and reads what you report.

` + where + `

When you finish a task, when you are blocked, or when you find something that changes the plan, call report with:
- kind: "done", "blocked" or "update";
- message: what you did or found, where (files, commits, branch), how you checked it, and what is left or what you need.
Keep the message short; put long material in the shared folder and name the file. Then end your turn.

The shared folder ` + shared + ` holds the plan, the decisions and the hand-over notes of the whole Workstream. Read what concerns your area before you start, and write there what other agents need from you.

The user may also talk to you in this session. Answer them as usual; report those turns only when they change your task.`
}

func agentInWorktree(repository, branch string) string {
	return "You work in your working directory, a git worktree of " + repository + " on the branch " + branch + ". Stay in it; other agents work in worktrees of their own."
}

func agentInWorkspace(workspace string) string {
	return "You work in " + workspace + ", which other agents share; change only the files of your area."
}

func TestThePromptsReadAsWritten(t *testing.T) {
	if got, want := OrchestratorPrompt("Payments", "/w/pay", "/d/ws/shared", true), wantOrchestratorPrompt("Payments", "/w/pay", "/d/ws/shared", orchestratorInRepo("/w/pay")); got != want {
		t.Errorf("orchestrator in a repository:\n%s", got)
	}
	if got, want := OrchestratorPrompt("Notes", "/w/notes", "/d/ws/shared", false), wantOrchestratorPrompt("Notes", "/w/notes", "/d/ws/shared", orchestratorOutsideRepo("/w/notes")); got != want {
		t.Errorf("orchestrator outside a repository:\n%s", got)
	}
	cases := []struct {
		at    Place
		where string
	}{
		{Place{Workspace: "/w/pay", Repository: "/w/pay", Branch: "droid/api"}, agentInWorktree("/w/pay", "droid/api")},
		{Place{Workspace: "/w/mono/svc/pay", Repository: "/w/mono", Subfolder: filepath.Join("svc", "pay"), Branch: "droid/api"},
			agentInWorktree("/w/mono", "droid/api") + " The Workstream's workspace is the folder " + filepath.Join("svc", "pay") + " in it."},
		{Place{Workspace: "/w/notes"}, agentInWorkspace("/w/notes")},
	}
	for _, c := range cases {
		if got, want := AgentPrompt("API", "Payments", "/d/ws/shared", c.at), wantAgentPrompt("API", "Payments", "/d/ws/shared", c.where); got != want {
			t.Errorf("agent at %+v:\n%s", c.at, got)
		}
	}
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
	if want := addition + "\n\n" + wantOrchestratorPrompt("Payments", f.repo, shared, orchestratorInRepo(f.repo)); ip.SystemPrompt.Append != want {
		t.Fatalf("system prompt %q", ip.SystemPrompt.Append)
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

func TestAWorkstreamStartsWhenItsGoalDoesNotReachTheOrchestrator(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	f.setRefuse(func(method, _ string) *droid.RPCError {
		if method == "daemon.add_user_message" {
			return &droid.RPCError{Code: -32603, Message: "No active session found for ID"}
		}
		return nil
	})
	made := f.create("", "")
	if made.WorkstreamID == "" || made.OrchestratorID == "" {
		t.Fatalf("made %+v", made)
	}
	if list, _ := f.c.List(f.ctx); len(list) != 1 || list[0].OrchestratorID != made.OrchestratorID {
		t.Fatalf("list %+v", list)
	}
	f.mu.Lock()
	tries := slices.Clone(f.refused)
	f.mu.Unlock()
	if want := []string{"daemon.add_user_message " + made.OrchestratorID, "daemon.add_user_message " + made.OrchestratorID}; !reflect.DeepEqual(tries, want) {
		t.Fatalf("tries %q", tries)
	}
}

func TestSpawnAgentStartsAnAgentOnABranchOfItsOwnWithItsTask(t *testing.T) {
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
	if ip.Cwd != f.repo || ip.Title != "API" || !ip.Worktree || ip.WorktreeLifecycle != "persistent" || ip.WorktreeNewBranch != "droid/api" || ip.WorktreeBaseBranch != "" ||
		ip.ModelID != "auto" || ip.ReasoningEffort != "none" || ip.AutonomyLevel != "low" || !reflect.DeepEqual(ip.Tags, wantTags) || !reflect.DeepEqual(ip.DisabledToolIDs, wantHidden) {
		t.Fatalf("initialize_session %+v", ip)
	}
	shared := filepath.Join(f.dir, made.WorkstreamID, SharedDir)
	if want := addition + "\n\n" + wantAgentPrompt("API", "Payments", shared, agentInWorktree(f.repo, "droid/api")); ip.SystemPrompt.Append != want {
		t.Fatalf("system prompt %q", ip.SystemPrompt.Append)
	}
	if got := f.messages(id); !reflect.DeepEqual(got, []string{"[From orchestrator]\nMove /pay to gRPC."}) {
		t.Fatalf("agent messages %q", got)
	}
	if a.WorktreePath == "" || a.Branch != "droid/api" || a.BaseBranch != "main" || a.Model != "auto" || !strings.HasPrefix(a.WorktreePath, f.wtRoot) {
		t.Fatalf("agent %+v", a)
	}
	for _, part := range []string{`Started agent "API" (` + id + `)`, a.WorktreePath, "droid/api", "Its report arrives as a message"} {
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

func TestEachAgentGetsABranchNoOtherHas(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	run(t, f.repo, "branch", "droid/api")
	api := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	if a := f.agent(made.WorkstreamID, api); a.Branch != "droid/api-2" || f.initialized(api).WorktreeNewBranch != "droid/api-2" {
		t.Fatalf("agent %+v", a)
	}
	// A title is one line without quotes, in the agent's messages too.
	web := f.spawn(made.OrchestratorID, "  \"Web\"\n  client ", "Move the web client.")
	if a := f.agent(made.WorkstreamID, web); a.Title != "Web client" || a.Branch != "droid/web-client" || f.initialized(web).Title != "Web client" {
		t.Fatalf("agent %+v", a)
	}
	if _, err := f.call(ToolReport, map[string]any{"kind": "update", "message": "Halfway."}, web); err != nil {
		t.Fatal(err)
	}
	if got := f.messages(made.OrchestratorID); !slices.Contains(got, `[From agent "Web client" (`+web+`)] update`+"\nHalfway.") {
		t.Fatalf("orchestrator messages %q", got)
	}
	// Two agents of one title started at once.
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if _, err := f.c.Call(f.ctx, ToolSpawnAgent, map[string]any{"title": "Docs", "prompt": "Write the docs."}, made.OrchestratorID); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	var branches []string
	for _, a := range loadAll(f.dir, func(string) {})[made.WorkstreamID].Agents {
		branches = append(branches, a.Branch)
	}
	slices.Sort(branches)
	if want := []string{"droid/api-2", "droid/docs", "droid/docs-2", "droid/web-client"}; !reflect.DeepEqual(branches, want) {
		t.Fatalf("branches %q", branches)
	}
}

func TestAnAgentOnADetachedHeadGetsABranchFromItsCommit(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	run(t, f.repo, "checkout", "-q", "--detach")
	commit := run(t, f.repo, "rev-parse", "HEAD")
	id := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	if ip := f.initialized(id); !ip.Worktree || ip.WorktreeNewBranch != "droid/api" || ip.WorktreeBaseBranch != "" {
		t.Fatalf("initialize_session %+v", ip)
	}
	if a := f.agent(made.WorkstreamID, id); a.BaseBranch != commit || a.Branch != "droid/api" {
		t.Fatalf("agent %+v", a)
	}
}

func TestAnAgentOfAWorkspaceInsideARepositoryIsToldWhereItIs(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	sub := filepath.Join(f.repo, "svc", "pay")
	if err := os.MkdirAll(sub, 0o777); err != nil {
		t.Fatal(err)
	}
	made, err := f.c.Create(f.ctx, gateway.NewWorkstream{Title: "Pay", Workspace: sub, Goal: "Tidy it."})
	if err != nil {
		t.Fatal(err)
	}
	id := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	ip := f.initialized(id)
	shared := filepath.Join(f.dir, made.WorkstreamID, SharedDir)
	where := agentInWorktree(f.repo, "droid/api") + " The Workstream's workspace is the folder " + filepath.Join("svc", "pay") + " in it."
	if want := addition + "\n\n" + wantAgentPrompt("API", "Pay", shared, where); !ip.Worktree || ip.SystemPrompt.Append != want {
		t.Fatalf("initialize_session %+v", ip)
	}
	if a := f.agent(made.WorkstreamID, id); a.BaseBranch != "main" {
		t.Fatalf("agent %+v", a)
	}
}

func TestAnAgentOutsideAGitRepositoryWorksInTheWorkspace(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	for _, plain := range []string{t.TempDir(), func() string {
		// A repository with no commit has no branch to cut a worktree's from.
		dir := t.TempDir()
		run(t, dir, "init", "-q", "-b", "main")
		return dir
	}()} {
		made, err := f.c.Create(f.ctx, gateway.NewWorkstream{Title: "Notes", Workspace: plain, Goal: "Tidy the notes."})
		if err != nil {
			t.Fatal(err)
		}
		shared := filepath.Join(f.dir, made.WorkstreamID, SharedDir)
		if want := addition + "\n\n" + wantOrchestratorPrompt("Notes", plain, shared, orchestratorOutsideRepo(plain)); f.initialized(made.OrchestratorID).SystemPrompt.Append != want {
			t.Fatalf("%s: the orchestrator is not told the workspace is no repository", plain)
		}
		id := f.spawn(made.OrchestratorID, "Index", "Index them.")
		ip := f.initialized(id)
		if want := addition + "\n\n" + wantAgentPrompt("Index", "Notes", shared, agentInWorkspace(plain)); ip.Worktree || ip.WorktreeNewBranch != "" || ip.SystemPrompt.Append != want {
			t.Fatalf("initialize_session %+v", ip)
		}
		if a := f.agent(made.WorkstreamID, id); a.WorktreePath != "" || a.Branch != "" || a.BaseBranch != "" {
			t.Fatalf("agent %+v", a)
		}
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

func TestAMessageAfterAReportIsWaitedOnWhileTheAgentWorksOn(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: long(12)})
	made := f.create("", "")
	id := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	f.waitFor("the agent to work", func() bool { return working(f.r.Session(id)) })
	if _, err := f.call(ToolReport, map[string]any{"kind": "done", "message": "Moved /pay."}, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.call(ToolSendMessage, map[string]any{"agent_id": id, "message": "Now /refund."}, made.OrchestratorID); err != nil {
		t.Fatal(err)
	}
	// The reported turn ends, and the agent goes on working on the message.
	if err := f.d.Notify(id, turnCompleted("completed")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(settleWait + 600*time.Millisecond)
	if !working(f.r.Session(id)) {
		t.Fatal("the turn ended too soon for this test")
	}
	report := `[From agent "API" (` + id + `)] done` + "\nMoved /pay."
	if got := f.messages(made.OrchestratorID); !reflect.DeepEqual(got, []string{"Move the payment service to gRPC.", report}) || !f.agent(made.WorkstreamID, id).Awaiting {
		t.Fatalf("orchestrator messages %q, agent %+v", got, f.agent(made.WorkstreamID, id))
	}
	// Its end without a report about the message is the orchestrator's news.
	prefix := `[From agent "API" (` + id + `)] Its turn ended without a report (completed).`
	f.waitFor("the notice", func() bool {
		return slices.ContainsFunc(f.messages(made.OrchestratorID), func(m string) bool { return strings.HasPrefix(m, prefix) })
	})
	f.waitFor("the agent no longer waited on", func() bool { return !f.agent(made.WorkstreamID, id).Awaiting })
	if n := len(f.messages(made.OrchestratorID)); n != 3 {
		t.Fatalf("%d orchestrator messages", n)
	}
}

func TestTheOrchestratorHearsOfAFailedTurnWithItsError(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	f.quietAgents(made.OrchestratorID)
	id := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	if err := f.d.Notify(id, map[string]any{"type": "droid_working_state_changed", "newState": "streaming_assistant_message"}); err != nil {
		t.Fatal(err)
	}
	f.waitFor("the agent to work", func() bool { return working(f.r.Session(id)) })
	if err := f.d.Notify(id, map[string]any{"type": "error", "message": "Rate limited", "errorType": "Error", "timestamp": "2026-11-05T00:00:00.000Z"}); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Notify(id, turnCompleted("error")); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Notify(id, map[string]any{"type": "droid_working_state_changed", "newState": "idle"}); err != nil {
		t.Fatal(err)
	}
	want := `[From agent "API" (` + id + `)] Its turn ended without a report (error: Rate limited).`
	f.waitFor("the notice", func() bool { return slices.Contains(f.messages(made.OrchestratorID), want) })
}

// The Fake Daemon has no worker to lose: a Session marked working with no
// turn behind it is idle when the Runner loads it after the Daemon is back.
func loseTheWorker(f *fixture, id string) {
	f.t.Helper()
	if err := f.d.Notify(id, map[string]any{"type": "droid_working_state_changed", "newState": "streaming_assistant_message"}); err != nil {
		f.t.Fatal(err)
	}
	f.waitFor("the agent to work", func() bool { return working(f.r.Session(id)) })
	if err := f.d.GoDown(); err != nil {
		f.t.Fatal(err)
	}
	if err := f.d.ComeBack(); err != nil {
		f.t.Fatal(err)
	}
}

func TestATurnWhoseWorkerWasLostIsNoticedAndSaysWhetherItReported(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	f.quietAgents(made.OrchestratorID)
	silent := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	reported := f.spawn(made.OrchestratorID, "Web", "Move the web client.")
	if _, err := f.call(ToolReport, map[string]any{"kind": "update", "message": "Halfway."}, reported); err != nil {
		t.Fatal(err)
	}
	loseTheWorker(f, silent)
	want := `[From agent "API" (` + silent + `)] Its turn ended without a report (process_exit: ` + hostsession.LostTouch + `).`
	f.waitFor("the notice", func() bool { return slices.Contains(f.messages(made.OrchestratorID), want) })
	loseTheWorker(f, reported)
	want = `[From agent "Web" (` + reported + `)] Its turn ended (process_exit: ` + hostsession.LostTouch + `) after its report.`
	f.waitFor("the notice after the report", func() bool { return slices.Contains(f.messages(made.OrchestratorID), want) })
}

func TestInterruptAgentStopsItsTurnWithoutANotice(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: long(10)})
	made := f.create("", "")
	id := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	f.waitFor("the agent to work", func() bool { return working(f.r.Session(id)) })
	text, err := f.call(ToolInterruptAgent, map[string]any{"agent_id": id}, made.OrchestratorID)
	if err != nil || text != `Interrupted agent "API" (`+id+`).` {
		t.Fatal(text, err)
	}
	if !slices.Contains(f.sessionIDs("daemon.interrupt_session"), id) || f.agent(made.WorkstreamID, id).Awaiting {
		t.Fatal("not interrupted")
	}
	f.idle(id)
	time.Sleep(500 * time.Millisecond)
	if got := f.messages(made.OrchestratorID); len(got) != 1 {
		t.Fatalf("orchestrator messages %q", got)
	}
}

func TestAnOrchestratorActsOnlyOnItsOwnAgents(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	mine := f.create("", "")
	other, err := f.c.Create(f.ctx, gateway.NewWorkstream{Title: "Other", Workspace: f.repo, Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	id := f.spawn(mine.OrchestratorID, "API", "Move /pay.")
	for _, tool := range []string{ToolSendMessage, ToolInterruptAgent, ToolArchiveAgent} {
		if _, err := f.call(tool, map[string]any{"agent_id": id, "message": "m"}, other.OrchestratorID); err == nil || !strings.Contains(err.Error(), "This workstream has no agent "+id) {
			t.Errorf("%s from another orchestrator: %v", tool, err)
		}
	}
	if len(f.requests("daemon.interrupt_session"))+len(f.requests("daemon.archive_session")) != 0 || len(f.messages(id)) != 1 {
		t.Fatal("another orchestrator reached the agent")
	}
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
	got := f.updates(before)
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
	f.waitFor("the turn's end", func() bool { return !f.agent(made.WorkstreamID, a).Awaiting })
	if n := len(f.requests("daemon.update_session_settings")); n != before {
		t.Fatalf("%d more update_session_settings", n-before)
	}
}

type update struct {
	SessionID       string                `json:"sessionId"`
	ModelID         string                `json:"modelId"`
	ReasoningEffort string                `json:"reasoningEffort"`
	Tags            []protocol.SessionTag `json:"tags"`
}

// updates are the update_session_settings requests from the before-th on.
func (f *fixture) updates(before int) []update {
	var got []update
	for _, p := range f.requests("daemon.update_session_settings")[before:] {
		var u update
		_ = json.Unmarshal(p, &u)
		got = append(got, u)
	}
	return got
}

func TestAChangeOfEffortAloneIsTakenAndRecorded(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("glm-5.3-flash", "high")
	id := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	f.waitFor("the turn's end", func() bool { return !f.agent(made.WorkstreamID, id).Awaiting })
	f.idle(id)
	before := len(f.requests("daemon.update_session_settings"))
	if err := f.c.SetAgentSettings(f.ctx, gateway.WorkstreamSettings{ID: made.WorkstreamID, AgentModel: "glm-5.3-flash", AgentReasoningEffort: "low"}); err != nil {
		t.Fatal(err)
	}
	got := f.updates(before)
	if len(got) != 3 || !reflect.DeepEqual(got[1], update{SessionID: id, ModelID: "glm-5.3-flash", ReasoningEffort: "low"}) || got[2].Tags[0].Metadata["agentReasoningEffort"] != "low" {
		t.Fatalf("update_session_settings %+v", got)
	}
	if a := f.agent(made.WorkstreamID, id); a.Model != "glm-5.3-flash" || a.ReasoningEffort != "low" {
		t.Fatalf("agent %+v", a)
	}
}

func TestSameAsOrchestratorKeepsAnAgentOnWhatItStartedWith(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	id := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	f.waitFor("the turn's end", func() bool { return !f.agent(made.WorkstreamID, id).Awaiting })
	f.idle(id)
	if err := f.r.UpdateSettings(f.ctx, protocol.UpdateSessionSettingsParams{SessionID: made.OrchestratorID, ModelID: "claude-opus-4-1"}); err != nil {
		t.Fatal(err)
	}
	f.waitFor("the orchestrator's model", func() bool { return f.r.Session(made.OrchestratorID).Settings().ModelID == "claude-opus-4-1" })
	if _, err := f.call(ToolSendMessage, map[string]any{"agent_id": id, "message": "Next."}, made.OrchestratorID); err != nil {
		t.Fatal(err)
	}
	f.waitFor("the turn's end", func() bool { return !f.agent(made.WorkstreamID, id).Awaiting })
	f.idle(id)
	if got := f.r.Session(id).Settings().ModelID; got != "auto" || slices.Contains(f.sessionIDs("daemon.update_session_settings"), id) {
		t.Fatalf("the agent followed the orchestrator to %q", got)
	}
	// Saving the settings is the user's choice: the agents take the
	// orchestrator's model then.
	if err := f.c.SetAgentSettings(f.ctx, gateway.WorkstreamSettings{ID: made.WorkstreamID}); err != nil {
		t.Fatal(err)
	}
	if got := f.r.Session(id).Settings().ModelID; got != "claude-opus-4-1" || f.agent(made.WorkstreamID, id).Model != "claude-opus-4-1" {
		t.Fatalf("the agent is on %q", got)
	}
}

func TestSettledWaitsForTheSettingsAskedOrNewsOfOthers(t *testing.T) {
	var mu sync.Mutex
	model, effort := "glm-5.3-flash", "high"
	current := func() (string, string) { mu.Lock(); defer mu.Unlock(); return model, effort }
	// The model is there already; the effort comes a little later.
	time.AfterFunc(100*time.Millisecond, func() { mu.Lock(); effort = "low"; mu.Unlock() })
	if m, e := settled(t.Context(), current, nil, "glm-5.3-flash", "high", "glm-5.3-flash", "low", 3*time.Second); m != "glm-5.3-flash" || e != "low" {
		t.Fatalf("took %s/%s", m, e)
	}
	// The Daemon fits the effort to the model: the news says so.
	news := make(chan struct{}, 1)
	mu.Lock()
	effort = "medium"
	mu.Unlock()
	time.AfterFunc(50*time.Millisecond, func() { news <- struct{}{} })
	start := time.Now()
	if m, e := settled(t.Context(), current, news, "glm-5.3-flash", "low", "glm-5.3-flash", "xhigh", 3*time.Second); m != "glm-5.3-flash" || e != "medium" || time.Since(start) > time.Second {
		t.Fatalf("took %s/%s after %v", m, e, time.Since(start))
	}
	// No news: the wait ends.
	start = time.Now()
	if _, e := settled(t.Context(), current, nil, "glm-5.3-flash", "medium", "glm-5.3-flash", "xhigh", 100*time.Millisecond); e != "medium" || time.Since(start) > time.Second {
		t.Fatalf("took %s after %v", e, time.Since(start))
	}
	// News of settings as they were, such as of an earlier change, is not
	// the answer: the wait goes on for the change.
	stale := make(chan struct{}, 1)
	stale <- struct{}{}
	time.AfterFunc(200*time.Millisecond, func() { mu.Lock(); effort = "low"; mu.Unlock() })
	if m, e := settled(t.Context(), current, stale, "glm-5.3-flash", "medium", "glm-5.3-flash", "low", 3*time.Second); m != "glm-5.3-flash" || e != "low" {
		t.Fatalf("took %s/%s", m, e)
	}
}

// addWorktree makes the checkout the Fake Daemon only names.
func (f *fixture) addWorktree(a Agent) {
	f.t.Helper()
	run(f.t, f.repo, "worktree", "add", "-q", "-b", a.Branch, a.WorktreePath)
}

type cleanup struct {
	WorktreePath      string `json:"worktreePath"`
	DeleteLocalBranch bool   `json:"deleteLocalBranch"`
	Force             *bool  `json:"force"`
}

func TestArchiveAgentRemovesACleanWorktreeAndItsBranchAndKeepsOneWithChanges(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	clean := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	dirty := f.spawn(made.OrchestratorID, "Web", "Move the web client.")
	ca, da := f.agent(made.WorkstreamID, clean), f.agent(made.WorkstreamID, dirty)
	f.addWorktree(ca)
	f.addWorktree(da)
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
	var p cleanup
	if len(cleanups) != 1 || json.Unmarshal(cleanups[0], &p) != nil || p != (cleanup{WorktreePath: ca.WorktreePath, DeleteLocalBranch: true}) {
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
	// Removing the clean one's worktree archived its Session.
	if archived := f.sessionIDs("daemon.archive_session"); !reflect.DeepEqual(archived, []string{dirty}) {
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

func TestAMergedAgentsWorktreeAndBranchGo(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	id := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	a := f.agent(made.WorkstreamID, id)
	f.addWorktree(a)
	if err := os.WriteFile(filepath.Join(a.WorktreePath, "api.go"), []byte("package api\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	run(t, a.WorktreePath, "add", ".")
	run(t, a.WorktreePath, "commit", "-q", "-m", "api")
	run(t, f.repo, "merge", "-q", "--ff-only", a.Branch)
	f.idle(id)
	text, err := f.call(ToolArchiveAgent, map[string]any{"agent_id": id}, made.OrchestratorID)
	if err != nil || !strings.Contains(text, "Removed its worktree") {
		t.Fatal(text, err)
	}
	var p cleanup
	if c := f.requests("daemon.cleanup_worktree"); len(c) != 1 || json.Unmarshal(c[0], &p) != nil || p != (cleanup{WorktreePath: a.WorktreePath, DeleteLocalBranch: true}) {
		t.Fatalf("cleanup_worktree %s", c)
	}
}

func TestArchivingAWorkstreamArchivesItsAgentsAndOrchestratorAndNamesTheKeptWorktrees(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	committed := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	ag := f.agent(made.WorkstreamID, committed)
	f.addWorktree(ag)
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
	if archived := f.sessionIDs("daemon.archive_session"); !reflect.DeepEqual(archived, []string{committed, made.OrchestratorID}) {
		t.Fatalf("archived %v", archived)
	}
	if _, err := os.Stat(filepath.Join(f.dir, made.WorkstreamID, SharedDir)); err != nil {
		t.Fatal("the shared folder went")
	}
	if list, _ := f.c.List(f.ctx); list[0].ArchivedAt == "" {
		t.Fatal("not archived")
	}
	if _, err := f.call(ToolSpawnAgent, map[string]any{"title": "X", "prompt": "Y"}, made.OrchestratorID); err == nil || !strings.Contains(err.Error(), `The workstream "Payments" is archived`) {
		t.Fatalf("an archived orchestrator spawned: %v", err)
	}
	if _, err := f.c.Archive(f.ctx, made.WorkstreamID); err == nil {
		t.Fatal("archived twice")
	}
}

func TestArchivingAWorkstreamStopsEveryWorkingAgentAtOnce(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: long(10)})
	made := f.create("", "")
	a := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	b := f.spawn(made.OrchestratorID, "Web", "Move the web client.")
	f.waitFor("both agents to work", func() bool { return working(f.r.Session(a)) && working(f.r.Session(b)) })
	if _, err := f.c.Archive(f.ctx, made.WorkstreamID); err != nil {
		t.Fatal(err)
	}
	reqs, err := f.d.Requests()
	if err != nil {
		t.Fatal(err)
	}
	at := func(method, id string) int {
		return slices.IndexFunc(reqs, func(r fakedaemon.Request) bool {
			var p struct{ SessionID string }
			_ = json.Unmarshal(r.Params, &p)
			return r.Method == method && p.SessionID == id
		})
	}
	first := min(at("daemon.archive_session", a), at("daemon.archive_session", b))
	if at("daemon.interrupt_session", a) < 0 || at("daemon.interrupt_session", b) < 0 || first < 0 ||
		at("daemon.interrupt_session", a) > first || at("daemon.interrupt_session", b) > first {
		t.Fatalf("requests %v", reqs)
	}
	// The interrupted turns are not the orchestrator's news.
	time.Sleep(500 * time.Millisecond)
	if got := f.messages(made.OrchestratorID); len(got) != 1 {
		t.Fatalf("orchestrator messages %q", got)
	}
}

func TestAnArchiveCutShortGoesOnWhereItStopped(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	a := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	f.idle(a)
	once := true
	f.setRefuse(func(method, id string) *droid.RPCError {
		if method == "daemon.archive_session" && id == made.OrchestratorID && once {
			once = false
			return &droid.RPCError{Code: -32603, Message: "busy"}
		}
		return nil
	})
	if _, err := f.c.Archive(f.ctx, made.WorkstreamID); err == nil {
		t.Fatal("no error")
	}
	if f.agent(made.WorkstreamID, a).ArchivedAt == "" {
		t.Fatal("the agent is not archived")
	}
	if _, err := f.c.Archive(f.ctx, made.WorkstreamID); err != nil {
		t.Fatal(err)
	}
	if archived := f.sessionIDs("daemon.archive_session"); !reflect.DeepEqual(archived, []string{a, made.OrchestratorID}) {
		t.Fatalf("archived %v", archived)
	}
}

func TestSessionsArchivedFromAClientCountAsArchived(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	a := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	b := f.spawn(made.OrchestratorID, "Web", "Move the web client.")
	f.idle(a)
	f.idle(b)
	f.archiveFromClient(a)
	if text, err := f.call(ToolArchiveAgent, map[string]any{"agent_id": a}, made.OrchestratorID); err != nil || !strings.HasPrefix(text, `Archived agent "API"`) {
		t.Fatal(text, err)
	}
	if f.agent(made.WorkstreamID, a).ArchivedAt == "" {
		t.Fatal("the agent is not archived")
	}
	f.archiveFromClient(b)
	f.archiveFromClient(made.OrchestratorID)
	if _, err := f.c.Archive(f.ctx, made.WorkstreamID); err != nil {
		t.Fatal(err)
	}
	if list, _ := f.c.List(f.ctx); list[0].ArchivedAt == "" || list[0].Agents[1].ArchivedAt == "" {
		t.Fatalf("list %+v", list)
	}
}

func TestAfterARestartTheOrchestratorHearsOnceOfAnIdleAgentItWaitedOn(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{
			{Title: "Payments", Cwd: "/w/pay", Messages: []fakedaemon.Message{{Role: "user", Text: "Move it."}}},
			{Title: "API", Cwd: "/w/pay", Messages: []fakedaemon.Message{{Role: "user", Text: "[From orchestrator]\nMove /pay."}, {Role: "assistant", Text: "Halfway there."}}},
			{Title: "Web", Cwd: "/w/pay", Messages: []fakedaemon.Message{{Role: "assistant", Text: "Done earlier."}}},
			{Title: "Docs", Cwd: "/w/pay"},
			{Title: "Gone", Cwd: "/w/pay"},
			{Title: "Reported", Cwd: "/w/pay"},
		},
		Turn: reply("Noted."),
	})
	ids := make([]string, len(f.d.Sessions))
	for i, s := range f.d.Sessions {
		ids[i] = s.SessionID
	}
	orch, api, web, docs, gone, rep := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]
	w := &Workstream{ID: "payments-abc123", Title: "Payments", Workspace: "/w/pay", OrchestratorID: orch, CreatedAt: "2026-11-05T10:00:00Z", Agents: []*Agent{
		{SessionID: api, Title: "API", CreatedAt: "2026-11-05T10:01:00Z", Awaiting: true},
		{SessionID: web, Title: "Web", CreatedAt: "2026-11-05T10:02:00Z"},
		{SessionID: docs, Title: "Docs", CreatedAt: "2026-11-05T10:03:00Z", Awaiting: true},
		{SessionID: gone, Title: "Gone", CreatedAt: "2026-11-05T10:04:00Z", Awaiting: true},
		{SessionID: rep, Title: "Reported", CreatedAt: "2026-11-05T10:05:00Z", Awaiting: true, ReportedSinceMessage: true},
	}}
	if err := os.MkdirAll(filepath.Join(f.dir, w.ID), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := save(f.dir, w); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.gone[gone] = true
	f.mu.Unlock()
	f.setRefuse(func(method, id string) *droid.RPCError {
		if method == "daemon.load_session" && id == docs {
			return &droid.RPCError{Code: -32603, Message: "busy"}
		}
		return nil
	})
	f.start()
	want := []string{
		`[From agent "API" (` + api + `)] Its turn ended without a report (Droi restarted while it worked). The start of its last reply:` + "\nHalfway there.",
		`[From agent "Docs" (` + docs + `)] Its turn ended without a report (Droi restarted while it worked).`,
		`[From agent "Reported" (` + rep + `)] Its turn ended (Droi restarted while it worked) after its report.`,
	}
	f.waitFor("the restart notices", func() bool {
		got := f.messages(orch)
		return len(got) == len(want) && !slices.ContainsFunc(want, func(m string) bool { return !slices.Contains(got, m) })
	})
	if a := f.agent(w.ID, api); a.Awaiting {
		t.Fatal("still awaiting")
	}
	if a := f.agent(w.ID, gone); a.ArchivedAt == "" || a.Awaiting {
		t.Fatalf("the agent archived from a Client %+v", a)
	}
	// Another restart says nothing again.
	f.start()
	time.Sleep(time.Second)
	if got := f.messages(orch); len(got) != len(want) {
		t.Fatalf("orchestrator messages %q", got)
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
	if len(got) != 1 || got[0].AgentID != id || got[0].Status != "idle" || got[0].Worktree != a.WorktreePath || got[0].Branch != "droid/api" ||
		got[0].LastActivity == "" || got[0].LastReport == nil || got[0].LastReport.Kind != "blocked" || len(got[0].LastReport.Message) != 300 {
		t.Fatalf("list_agents %s", text)
	}
}

func TestAnOldRecordStillReads(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "w-1"), 0o777); err != nil {
		t.Fatal(err)
	}
	old := `{"id":"w-1","title":"W","workspace":"/w","orchestratorId":"o","createdAt":"2026-11-05T10:00:00Z","agents":[{"sessionId":"a","title":"A","createdAt":"2026-11-05T10:01:00Z","awaiting":true,"reportedThisTurn":true}]}`
	if err := os.WriteFile(filepath.Join(dir, "w-1", recordFile), []byte(old), 0o666); err != nil {
		t.Fatal(err)
	}
	w := loadAll(dir, func(s string) { t.Log(s) })["w-1"]
	if w == nil || len(w.Agents) != 1 || !w.Agents[0].Awaiting || w.Agents[0].ReportedSinceMessage {
		t.Fatalf("record %+v", w)
	}
}

// handOff does what a Client's /handoff does: the Daemon starts a new
// Session with the old one's tags, and droid writes its transcript, which
// names the old one as its parent, beside the old one's.
func (f *fixture) handOff(old string) string {
	f.t.Helper()
	return f.handOffTo(old, filepath.Join(f.sessionsDir, "-w-pay"))
}

// handOffTo hands old off as handOff does, writing the new transcript in
// dir: the folder of the Session's cwd, which /cwd may have changed.
func (f *fixture) handOffTo(old, dir string) string {
	f.t.Helper()
	cl, err := f.r.Client()
	if err != nil {
		f.t.Fatal(err)
	}
	res, err := cl.CompactSession(f.ctx, protocol.CompactSessionParams{SessionID: old, Mode: "handoff"})
	if err != nil {
		f.t.Fatal(err)
	}
	tags := f.initialized(old).Tags
	if memory.FindSessionTranscript(f.sessionsDir, old) == "" {
		f.writeTranscript(old, "", tags)
	}
	f.writeTranscriptIn(dir, res.NewSessionID, old, false, tags)
	return res.NewSessionID
}

// writeTranscript writes the files the Daemon keeps of a Session: its
// transcript's first line and its settings.
func (f *fixture) writeTranscript(id, parent string, tags []protocol.SessionTag) {
	f.t.Helper()
	f.writeTranscriptIn(filepath.Join(f.sessionsDir, "-w-pay"), id, parent, false, tags)
}

// writeTranscriptIn writes a Session's files in dir; a fork's first line
// names where it forked, as droid's /fork and rewind write it.
func (f *fixture) writeTranscriptIn(dir, id, parent string, fork bool, tags []protocol.SessionTag) {
	f.t.Helper()
	if err := os.MkdirAll(dir, 0o777); err != nil {
		f.t.Fatal(err)
	}
	start := map[string]any{"type": "session_start", "id": id, "title": "New Session", "version": 2, "cwd": f.repo}
	if parent != "" {
		start["parent"] = parent
	}
	if fork {
		start["forkedAtMessageId"] = nil
	}
	line, _ := json.Marshal(start)
	settings, _ := json.Marshal(map[string]any{"model": "auto", "tags": tags})
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), append(line, '\n'), 0o666); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".settings.json"), settings, 0o666); err != nil {
		f.t.Fatal(err)
	}
}

func TestAnOrchestratorHandedOffGoesOnInItsNewSession(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	agent := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	f.waitFor("the agent's turn to end", func() bool { return !f.agent(made.WorkstreamID, agent).Awaiting })
	f.idle(agent)
	next := f.handOff(made.OrchestratorID)
	text, err := f.call(ToolListAgents, nil, next)
	if err != nil || !strings.Contains(text, agent) {
		t.Fatal(text, err)
	}
	if list, _ := f.c.List(f.ctx); list[0].OrchestratorID != next {
		t.Fatalf("list %+v", list[0])
	}
	if f.r.Following(made.OrchestratorID) || !f.r.Following(next) {
		t.Fatal("the Runner follows the old link")
	}
	if got := f.loadedHiding(next); !reflect.DeepEqual(got, orchestratorHidden()) {
		t.Fatalf("the new link was loaded hiding %v", got)
	}
	if _, err := f.call(ToolSendMessage, map[string]any{"agent_id": agent, "message": "Now /refund."}, next); err != nil {
		t.Fatal(err)
	}
	// The agent's turn end is news to the new link.
	prefix := `[From agent "API" (` + agent + `)] Its turn ended without a report`
	f.waitFor("the notice", func() bool {
		return slices.ContainsFunc(f.messages(next), func(m string) bool { return strings.HasPrefix(m, prefix) })
	})
	// The old link is the orchestrator no more.
	if _, err := f.call(ToolListAgents, nil, made.OrchestratorID); err == nil {
		t.Fatal("the old link listed the agents")
	}
	if got := loadAll(f.dir, func(string) {})[made.WorkstreamID].OrchestratorID; got != next {
		t.Fatalf("recorded orchestrator %s", got)
	}
}

func TestAReportAfterTheOrchestratorWasHandedOffGoesToItsNewSession(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	agent := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	f.waitFor("the notice of the agent's first turn", func() bool { return len(f.messages(made.OrchestratorID)) == 2 })
	before := len(f.messages(made.OrchestratorID))
	next := f.handOff(made.OrchestratorID)
	if _, err := f.call(ToolReport, map[string]any{"kind": "done", "message": "Moved /pay."}, agent); err != nil {
		t.Fatal(err)
	}
	report := `[From agent "API" (` + agent + `)] done` + "\nMoved /pay."
	if !slices.Contains(f.messages(next), report) || len(f.messages(made.OrchestratorID)) != before {
		t.Fatalf("new link %q, old link %q", f.messages(next), f.messages(made.OrchestratorID))
	}
}

func TestAnAgentHandedOffHearsAndReportsInItsNewSession(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	api := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	web := f.spawn(made.OrchestratorID, "Web", "Move the web client.")
	f.waitFor("the turns to end", func() bool {
		return !f.agent(made.WorkstreamID, api).Awaiting && !f.agent(made.WorkstreamID, web).Awaiting
	})
	f.idle(api)
	f.idle(web)

	// The orchestrator still names the agent by the id it knows.
	nextAPI := f.handOff(api)
	if _, err := f.call(ToolSendMessage, map[string]any{"agent_id": api, "message": "Now /refund."}, made.OrchestratorID); err != nil {
		t.Fatal(err)
	}
	if got := f.messages(nextAPI); !slices.Contains(got, "[From orchestrator]\nNow /refund.") || len(f.messages(api)) != 1 {
		t.Fatalf("new link %q, old link %q", got, f.messages(api))
	}
	if a := loadAll(f.dir, func(string) {})[made.WorkstreamID].Agents[0]; a.SessionID != nextAPI {
		t.Fatalf("recorded agent %+v", a)
	}
	if got := f.loadedHiding(nextAPI); !reflect.DeepEqual(got, agentHidden()) {
		t.Fatalf("the new link was loaded hiding %v", got)
	}

	// A report from a link the Host has not heard of yet.
	nextWeb := f.handOff(web)
	if _, err := f.call(ToolReport, map[string]any{"kind": "done", "message": "Moved the web client."}, nextWeb); err != nil {
		t.Fatal(err)
	}
	if want := `[From agent "Web" (` + nextWeb + `)] done` + "\nMoved the web client."; !slices.Contains(f.messages(made.OrchestratorID), want) {
		t.Fatalf("orchestrator messages %q", f.messages(made.OrchestratorID))
	}
	if list, _ := f.c.List(f.ctx); list[0].Agents[0].SessionID != nextAPI || list[0].Agents[1].SessionID != nextWeb {
		t.Fatalf("list %+v", list[0].Agents)
	}
	if _, err := f.call(ToolReport, map[string]any{"kind": "done", "message": "x"}, web); err == nil {
		t.Fatal("the old link reported")
	}
}

func TestASessionWithACopiedTagButNoLinkToTheRecordIsRefused(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	agent := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	f.idle(agent)
	stranger := func(tag protocol.SessionTag, parent string) string {
		opened, err := f.r.Open(f.ctx, hostsession.Spec{Cwd: f.repo, Tags: []protocol.SessionTag{tag}})
		if err != nil {
			t.Fatal(err)
		}
		f.writeTranscript(opened.SessionID, parent, []protocol.SessionTag{tag})
		return opened.SessionID
	}
	orchTag := f.initialized(made.OrchestratorID).Tags[0]
	agentTag := f.initialized(agent).Tags[0]
	unrelated := stranger(orchTag, "")
	f.writeTranscript("11111111-2222-3333-4444-555555555555", "", nil)
	for _, id := range []string{unrelated, stranger(orchTag, "11111111-2222-3333-4444-555555555555"), stranger(agentTag, made.OrchestratorID)} {
		if _, err := f.call(ToolListAgents, nil, id); err == nil || !strings.Contains(err.Error(), "Only the orchestrator") {
			t.Errorf("list_agents from %s: %v", id, err)
		}
	}
	for _, id := range []string{stranger(agentTag, ""), stranger(orchTag, agent)} {
		if _, err := f.call(ToolReport, map[string]any{"kind": "done", "message": "x"}, id); err == nil || !strings.Contains(err.Error(), "Only an agent") {
			t.Errorf("report from %s: %v", id, err)
		}
	}
	if w := loadAll(f.dir, func(string) {})[made.WorkstreamID]; w.OrchestratorID != made.OrchestratorID || w.Agents[0].SessionID != agent {
		t.Fatalf("record %+v", w)
	}
}

// loadedHiding are the tools the last load_session of id hid.
func (f *fixture) loadedHiding(id string) []string {
	f.t.Helper()
	var hidden []string
	for _, p := range f.requests("daemon.load_session") {
		var lp struct {
			SessionID       string   `json:"sessionId"`
			DisabledToolIDs []string `json:"disabledToolIds"`
		}
		_ = json.Unmarshal(p, &lp)
		if lp.SessionID == id {
			hidden = lp.DisabledToolIDs
		}
	}
	return hidden
}

func TestAForkOfAWorkstreamSessionIsNoHandoff(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	agent := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	f.idle(agent)
	next := f.handOff(made.OrchestratorID)
	// A fork copies the tags and names its source as parent, beside it and
	// newer than the handoff.
	fork := "11111111-2222-3333-4444-555555555555"
	f.writeTranscriptIn(filepath.Join(f.sessionsDir, "-w-pay"), fork, made.OrchestratorID, true, f.initialized(made.OrchestratorID).Tags)
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(f.sessionsDir, "-w-pay", fork+".jsonl"), later, later); err != nil {
		t.Fatal(err)
	}
	if list, _ := f.c.List(f.ctx); list[0].OrchestratorID != next {
		t.Fatalf("the orchestrator is %s, want the handoff %s", list[0].OrchestratorID, next)
	}
	if _, err := f.call(ToolListAgents, nil, fork); err == nil || !strings.Contains(err.Error(), "Only the orchestrator") {
		t.Fatalf("list_agents from the fork: %v", err)
	}
}

func TestAHandoffAfterTheSessionMovedFolderIsFollowed(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	// /cwd moved the orchestrator: droid writes the handoff's transcript in
	// the folder of where it works now.
	moved := t.TempDir()
	f.r.Session(made.OrchestratorID).SetCwd(moved)
	next := f.handOffTo(made.OrchestratorID, memory.SessionFolder(f.sessionsDir, moved))
	if list, _ := f.c.List(f.ctx); list[0].OrchestratorID != next {
		t.Fatalf("the orchestrator is %s, want %s", list[0].OrchestratorID, next)
	}
}

func TestArchivingAWorkstreamArchivesTheSessionsItsSessionsWereHandedOffFrom(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	agent := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	f.waitFor("the agent's turn to end", func() bool { return !f.agent(made.WorkstreamID, agent).Awaiting })
	f.idle(agent)
	nextOrch := f.handOff(made.OrchestratorID)
	nextAgent := f.handOff(agent)
	f.c.List(f.ctx)
	if w := loadAll(f.dir, func(string) {})[made.WorkstreamID]; !reflect.DeepEqual(w.PreviousOrchestratorIDs, []string{made.OrchestratorID}) {
		t.Fatalf("record %+v", w)
	}
	// The old orchestrator was archived from a Client already.
	f.archiveFromClient(made.OrchestratorID)
	if _, err := f.c.Archive(f.ctx, made.WorkstreamID); err != nil {
		t.Fatal(err)
	}
	archived := f.sessionIDs("daemon.archive_session")
	for _, id := range []string{agent, nextAgent, nextOrch} {
		if !slices.Contains(archived, id) {
			t.Errorf("%s is not archived: %v", id, archived)
		}
	}
}

func TestAnArchiveCutShortStillTellsOfTheAgentsItCouldNotStop(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: long(6)})
	made := f.create("", "")
	a := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	b := f.spawn(made.OrchestratorID, "Web", "Move the web client.")
	f.waitFor("both agents to work", func() bool { return working(f.r.Session(a)) && working(f.r.Session(b)) })
	f.setRefuse(func(method, id string) *droid.RPCError {
		if method == "daemon.interrupt_session" && id == b {
			return &droid.RPCError{Code: -32603, Message: "busy"}
		}
		return nil
	})
	if _, err := f.c.Archive(f.ctx, made.WorkstreamID); err == nil {
		t.Fatal("no error")
	}
	prefix := `[From agent "Web" (` + b + `)] Its turn ended`
	f.waitFor("the notice of the agent still working", func() bool {
		return slices.ContainsFunc(f.messages(made.OrchestratorID), func(m string) bool { return strings.HasPrefix(m, prefix) })
	})
	if got := f.messages(made.OrchestratorID); len(got) != 2 {
		t.Fatalf("orchestrator messages %q", got)
	}
}

func TestACreateThatFailsLeavesNoFolderAndNamesItsWorkstream(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	f.setRefuse(func(method, id string) *droid.RPCError {
		if method == "daemon.initialize_session" {
			return &droid.RPCError{Code: -32603, Message: "busy"}
		}
		return nil
	})
	if _, err := f.c.Create(f.ctx, gateway.NewWorkstream{Title: "Payments", Workspace: f.repo, Goal: "Move it."}); err == nil {
		t.Fatal("no error")
	}
	if entries, _ := os.ReadDir(f.dir); len(entries) != 0 {
		t.Fatalf("left %v", entries)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.ContainsFunc(f.logs, func(s string) bool { return strings.Contains(s, "workstream payments-") }) {
		t.Fatalf("logs %q", f.logs)
	}
}

func TestCloseEndsTheLinesOfWorkAndTakesNoMore(t *testing.T) {
	f := newFixture(t, fakedaemon.Scenario{Turn: reply("ok")})
	made := f.create("", "")
	id := f.spawn(made.OrchestratorID, "API", "Move /pay.")
	f.idle(id)
	_, a := f.c.agentOf(id)
	started := make(chan struct{})
	var ended bool
	go f.c.inOrder(f.ctx, a, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		ended = true
		return ctx.Err()
	})
	<-started
	f.c.Close()
	if !ended {
		t.Fatal("Close did not wait for the line of work")
	}
	ran := false
	if err := f.c.inOrder(f.ctx, a, func(context.Context) error { ran = true; return nil }); err == nil || ran {
		t.Fatalf("a closed Coordinator ran more work: %v", err)
	}
}

// The Host looks for a newer link before each message it sends: in one
// Workspace folder of the Daemon's, reading each transcript's first line
// once.
func BenchmarkFindingANewerLinkInAFolderOf1000Sessions(b *testing.B) {
	dir := b.TempDir()
	folder := filepath.Join(dir, "-w-pay")
	if err := os.MkdirAll(folder, 0o777); err != nil {
		b.Fatal(err)
	}
	for i := range 1000 {
		line := fmt.Sprintf(`{"type":"session_start","id":"s%d","cwd":"/w/pay"}`+"\n", i)
		if err := os.WriteFile(filepath.Join(folder, fmt.Sprintf("s%d.jsonl", i)), []byte(line), 0o666); err != nil {
			b.Fatal(err)
		}
	}
	l := newLineage(dir)
	if l.successor("s0", "") != "" {
		b.Fatal("a successor")
	}
	b.ResetTimer()
	for b.Loop() {
		l.successor("s0", "")
	}
}
