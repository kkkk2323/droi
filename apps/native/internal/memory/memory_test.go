package memory

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func eq[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o666); err != nil {
		t.Fatal(err)
	}
}

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

const validSummary = "v1\n## User Profile\nA developer.\n## User preferences\n- Answers in Chinese.\n## General Tips\n- Run pnpm check.\n## What's in Memory\n### /Users/dev/app\n- session_summaries/a.md — the login fix\n"

func TestCheckSummaryWantsTheVersionTheHeadingsAndTheSize(t *testing.T) {
	eq(t, CheckSummary(validSummary), "")
	if CheckSummary(strings.TrimPrefix(validSummary, "v1\n")) == "" {
		t.Error("took a summary without v1")
	}
	if CheckSummary(strings.Replace(validSummary, "## General Tips\n", "", 1)) == "" {
		t.Error("took a summary without General Tips")
	}
	if CheckSummary(validSummary+strings.Repeat("x", SummaryMaxBytes)) == "" {
		t.Error("took a summary over the size")
	}
}

func TestResolveOpensOnlyMemorysOwnFiles(t *testing.T) {
	dir := tempDir(t)
	writeFile(t, filepath.Join(dir, SummaryFile), validSummary)
	writeFile(t, filepath.Join(dir, SessionSummariesDir, "a.md"), "x")
	writeFile(t, filepath.Join(dir, NotesDir, "n.md"), "x")
	writeFile(t, filepath.Join(dir, StateFile), "{}")
	writeFile(t, filepath.Join(filepath.Dir(dir), "secret.md"), "x")
	for _, ok := range []string{SummaryFile, "session_summaries/a.md", `notes\n.md`, "./notes/n.md"} {
		if _, _, err := Resolve(dir, ok); err != nil {
			t.Errorf("refused %q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", StateFile, "../secret.md", "notes/../../secret.md", "/etc/passwd", "session_summaries/missing.md", "session_summaries", "notes/n.txt"} {
		if _, _, err := Resolve(dir, bad); err == nil {
			t.Errorf("opened %q", bad)
		}
	}
	if err := os.Symlink(filepath.Join(filepath.Dir(dir), "secret.md"), filepath.Join(dir, NotesDir, "link.md")); err == nil {
		if _, _, err := Resolve(dir, "notes/link.md"); err == nil {
			t.Error("followed a symbolic link")
		}
	}
	_, rel, _ := Resolve(dir, "./notes/n.md")
	eq(t, rel, "notes/n.md")
}

func TestWriteNoteKeepsEveryNoteAndRefusesSecrets(t *testing.T) {
	dir := tempDir(t)
	at := time.Date(2026, 10, 27, 9, 21, 3, 0, time.UTC)
	first, err := WriteNote(dir, "The user asked to remember: deploy with make ship", "s1", at)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, first, "notes/2026-10-27T09-21-03-the-user-asked-to-remember-deploy-with.md")
	second, _ := WriteNote(dir, "The user asked to remember: deploy with make ship", "", at)
	eq(t, second, "notes/2026-10-27T09-21-03-the-user-asked-to-remember-deploy-with-2.md")
	chinese, _ := WriteNote(dir, "用户要求记住：周报发在周五", "", at)
	eq(t, chinese, "notes/2026-10-27T09-21-03-note.md")
	b, _ := os.ReadFile(filepath.Join(dir, first))
	eq(t, string(b), "written_at: 2026-10-27T09:21:03Z\nsession_id: s1\n\nThe user asked to remember: deploy with make ship\n")
	if _, err := WriteNote(dir, "the staging db uses password=hunter2", "", at); err == nil {
		t.Error("saved a password")
	}
	if _, err := WriteNote(dir, "   ", "", at); err == nil {
		t.Error("saved an empty note")
	}
}

func TestStartedSessionsAreRecordedOnce(t *testing.T) {
	dir := tempDir(t)
	first := Started{SessionID: "s1", Transcript: "/t/s1.jsonl", Cwd: "/w", At: "2026-10-27T09:00:00Z"}
	if err := RecordStart(dir, first); err != nil {
		t.Fatal(err)
	}
	_ = RecordStart(dir, Started{SessionID: "s1", Transcript: "/t/s1.jsonl", Cwd: "/w", At: "2026-10-27T10:00:00Z"})
	got, err := ReadStarted(dir)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got, []Started{first})
	ForgetStarted(dir, "s1")
	got, _ = ReadStarted(dir)
	eq(t, len(got), 0)
}

func TestReadsAreTakenOnceTheyHaveSettled(t *testing.T) {
	dir := tempDir(t)
	if err := RecordRead(dir, "session_summaries/a.md"); err != nil {
		t.Fatal(err)
	}
	_ = RecordRead(dir, "session_summaries/b.md")
	now := time.Now()
	got, _ := TakeReads(dir, now, time.Minute)
	eq(t, len(got), 0)
	got, _ = TakeReads(dir, now.Add(2*time.Minute), time.Minute)
	eq(t, []string{got[0].File, got[1].File}, []string{"session_summaries/a.md", "session_summaries/b.md"})
	got, _ = TakeReads(dir, now.Add(4*time.Minute), time.Minute)
	eq(t, len(got), 0)
}

func TestSlug(t *testing.T) {
	eq(t, Slug("Fix the Login bug!", 60), "fix-the-login-bug")
	eq(t, Slug("修复登录", 60), "")
	eq(t, Slug("a b c d e f", 5), "a-b-c")
}

func TestHeaderReadsTheKeysBeforeTheFirstBlankLine(t *testing.T) {
	eq(t, Header("session_id: s\nupdated_at: 2026-10-27T09:00:00Z\n\nbody: no"), map[string]string{"session_id": "s", "updated_at": "2026-10-27T09:00:00Z"})
	eq(t, Header("# Title\nx: y"), map[string]string{})
}

func line(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func msg(role string, blocks ...any) string {
	return line(map[string]any{"type": "message", "message": map[string]any{"role": role, "content": blocks}})
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func TestReadTranscriptRendersTheConversationAndToolLines(t *testing.T) {
	dir := tempDir(t)
	file := filepath.Join(dir, "s.jsonl")
	writeFile(t, file, strings.Join([]string{
		line(map[string]any{"type": "session_start", "id": "s", "cwd": "/w", "title": "Login"}),
		msg("user", text("<system-reminder>skills</system-reminder>")),
		msg("user", text("<system-reminder>date</system-reminder>\nFix the login bug")),
		msg("assistant", map[string]any{"type": "thinking", "thinking": "hmm"}, text("Looking at auth.ts"), map[string]any{"type": "tool_use", "id": "t", "name": "Read", "input": map[string]any{"file_path": "/w/auth.ts"}}),
		msg("user", map[string]any{"type": "tool_result", "tool_use_id": "t", "content": []any{text("export function login() {}")}}),
		msg("user", map[string]any{"type": "tool_result", "tool_use_id": "u", "is_error": true, "content": "not found"}),
		line(map[string]any{"type": "compaction_state", "summaryText": "The user fixed login."}),
		"not json",
		line(map[string]any{"type": "message", "message": map[string]any{"role": "user", "content": "不对，是 session.ts"}}),
	}, "\n"))
	got, err := ReadTranscript(file, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got.Text, "User: Fix the login bug\nDroid: Looking at auth.ts\n[tool call] Read {\"file_path\":\"/w/auth.ts\"}\n[tool result] export function login() {}\n[tool error] not found\n[Earlier part of the Session, as compacted] The user fixed login.\nUser: 不对，是 session.ts\n")
	eq(t, []any{got.Cwd, got.Title, got.UserPrompts, got.Subagent, got.Omitted}, []any{"/w", "Login", 2, false, false})
}

func TestReadTranscriptKeepsTheUsersWordsFirstWhenTheBudgetIsShort(t *testing.T) {
	dir := tempDir(t)
	file := filepath.Join(dir, "s.jsonl")
	writeFile(t, file, strings.Join([]string{
		line(map[string]any{"type": "session_start", "id": "s", "cwd": "/w", "callingSessionId": "parent"}),
		msg("user", text("first prompt")),
		msg("assistant", text(strings.Repeat("long reply ", 20))),
		msg("assistant", map[string]any{"type": "tool_use", "name": "Execute", "input": map[string]any{"command": "ls"}}),
		msg("user", text("second prompt")),
		msg("assistant", text("short reply")),
	}, "\n"))
	got, _ := ReadTranscript(file, 60)
	eq(t, got.Text, "User: first prompt\n[... omitted ...]\nUser: second prompt\nDroid: short reply\n")
	eq(t, got.Omitted, true)
	eq(t, got.Subagent, true)
}

func TestReadTranscriptKeepsTheDaemonsNoticesApartFromTheUsersWords(t *testing.T) {
	dir := tempDir(t)
	file := filepath.Join(dir, "s.jsonl")
	notice := func(visibility, s string) string {
		return line(map[string]any{"type": "message", "message": map[string]any{"role": "user", "visibility": visibility, "content": []any{text(s)}}})
	}
	writeFile(t, file, strings.Join([]string{
		msg("user", text("/retro go through it\n\n<system-notification>\nSkills provide <skill>the skill's body</skill>\n</system-notification>")),
		notice("llm_only", "Background task completed.\ntask_id: b83\noutput: 56 keys are dead"),
		notice("user_only", "Request blocked by model provider safety checks."),
		notice("both", "Error: 400 Bad Request"),
		notice("llm_only", "Request interrupted by user"),
	}, "\n"))
	got, err := ReadTranscript(file, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got.Text, "User: /retro go through it\n[notice] Background task completed. task_id: b83 output: 56 keys are dead\n[notice] Request blocked by model provider safety checks.\n[notice] Error: 400 Bad Request\n[notice] Request interrupted by user\n")
	eq(t, got.UserPrompts, 1)
}
func TestRedactSecretsReplacesEveryCredential(t *testing.T) {
	for label, s := range secretTexts {
		if got := RedactSecrets(s); FindSecret(got) != "" || !strings.Contains(got, Redacted) {
			t.Errorf("%s: %q", label, got)
		}
	}
	eq(t, RedactSecrets("Run pnpm check."), "Run pnpm check.")
}

// Secrets are built from parts so the source holds none.
var secretTexts = map[string]string{
	"an OpenAI key":         "use sk-" + strings.Repeat("aB3", 12) + " for the tests",
	"a Factory key":         "FACTORY_API_KEY is fk-" + strings.Repeat("a1B2", 7),
	"an AWS access key":     "the key AKIA" + "IOSFODNN7EXAMPLE works",
	"a GitHub token":        "push with ghp_" + strings.Repeat("aB3d", 9),
	"a Slack token":         "xoxb-" + "1234567890-abcdefghijkl",
	"a bearer token":        "Authorization: Bearer abcdefghijklmnopqrstuvwxyz.123456",
	"a JWT":                 "token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N",
	"a private key":         "-----BEGIN OPENSSH " + "PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA",
	"a password assignment": "the staging db uses password=hunter2",
	"an api_key assignment": `api_key: "0123456789abcdef0123"`,
}

func TestFindSecretRefusesCredentialsAndLetsOrdinaryTextThrough(t *testing.T) {
	for label, s := range secretTexts {
		if FindSecret(s) == "" {
			t.Errorf("lets %s through", label)
		}
	}
	for _, s := range []string{"Run pnpm check && pnpm test before committing.", "Passwords are hashed with argon2 in src/auth.", "The token budget is 200k."} {
		if reason := FindSecret(s); reason != "" {
			t.Errorf("refuses %q: %s", s, reason)
		}
	}
	if reason := FindSecret("password=hunter2"); strings.Contains(reason, "hunter2") {
		t.Errorf("repeats the secret: %q", reason)
	}
}

func writeSessionFile(t *testing.T, dir, folder, id string, lines ...string) string {
	t.Helper()
	file := filepath.Join(dir, folder, id+".jsonl")
	writeFile(t, file, strings.Join(lines, "\n"))
	return file
}

func TestSessionFilesTellTheWorkspaceAndTheTags(t *testing.T) {
	dir := tempDir(t)
	writeSessionFile(t, dir, "-Users-dev-app", "bbb", `{"type":"session_start","id":"bbb","cwd":"/Users/dev/app"}`)
	transcript := FindSessionTranscript(dir, "bbb")
	eq(t, ReadSessionWorkspace(transcript), "/Users/dev/app")
	eq(t, FindSessionTranscript(dir, "../etc/passwd"), "")
	eq(t, IsMemorySessionTranscript(transcript), false)
	writeFile(t, SessionSettingsFile(transcript), `{"tags":[{"name":"droi.memory"}]}`)
	eq(t, IsMemorySessionTranscript(transcript), true)
	eq(t, SessionTags(transcript), []string{"droi.memory"})
	env := func(key string) (string, bool) {
		if key == "FACTORY_HOME_OVERRIDE" {
			return "/tmp/f", true
		}
		return "", false
	}
	eq(t, SessionsDir(env), filepath.Join("/tmp/f", ".factory", "sessions"))
}

// fakeWorktree lays out what `git worktree add` leaves behind: a main
// checkout whose .git keeps the worktree's repository, and the worktree,
// elsewhere, whose .git file points there.
func fakeWorktree(t *testing.T) (main, worktree string) {
	t.Helper()
	root := tempDir(t)
	main = filepath.Join(root, "repo")
	worktree = filepath.Join(root, "worktrees", "feature", "repo")
	gitdir := filepath.Join(main, ".git", "worktrees", "feature")
	for _, dir := range []string{gitdir, filepath.Join(worktree, "src")} {
		if err := os.MkdirAll(dir, 0o777); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(gitdir, "commondir"), "../..\n")
	writeFile(t, filepath.Join(worktree, ".git"), "gitdir: "+gitdir+"\n")
	return main, worktree
}

func TestAWorktreeBelongsToItsMainCheckout(t *testing.T) {
	main, worktree := fakeWorktree(t)
	eq(t, WorkspaceOf(worktree), main)
	eq(t, WorkspaceOf(filepath.Join(worktree, "src")), filepath.Join(main, "src"))
	eq(t, WorkspaceOf(main), main)
	eq(t, WorkspaceOf("/nowhere/gone"), "/nowhere/gone")
}

func TestASubmoduleIsItsOwnWorkspace(t *testing.T) {
	root := tempDir(t)
	lib := filepath.Join(root, "lib")
	if err := os.MkdirAll(filepath.Join(root, ".git", "modules", "lib"), 0o777); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(lib, ".git"), "gitdir: ../.git/modules/lib\n")
	eq(t, WorkspaceOf(lib), lib)
}

func TestAWorktreeMadeByGitBelongsToItsMainCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := tempDir(t)
	main := filepath.Join(root, "repo")
	worktree := filepath.Join(root, "elsewhere", "repo")
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(main, 0o777); err != nil {
		t.Fatal(err)
	}
	git(main, "init", "-q")
	git(main, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "start")
	git(main, "worktree", "add", "-q", worktree)
	eq(t, WorkspaceOf(worktree), main)
}

// mcpClient speaks to serveMain over pipes.
type mcpClient struct {
	in    *io.PipeWriter
	lines *bufio.Scanner
	done  chan int
}

func startServer(t *testing.T, env map[string]string) *mcpClient {
	t.Helper()
	lookup := func(key string) (string, bool) { v, ok := env[key]; return v, ok }
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &mcpClient{in: inW, lines: bufio.NewScanner(outR), done: make(chan int, 1)}
	c.lines.Buffer(make([]byte, 1<<20), 1<<20)
	go func() {
		c.done <- serveMain(lookup, inR, outW, io.Discard)
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	return c
}

func (c *mcpClient) call(t *testing.T, id int, method string, params any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if _, err := c.in.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
	if !c.lines.Scan() {
		t.Fatal("no answer")
	}
	var reply map[string]any
	if err := json.Unmarshal(c.lines.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

func TestTheMemoryServerAnswersOverStdio(t *testing.T) {
	dir := tempDir(t)
	writeFile(t, filepath.Join(dir, SummaryFile), validSummary)
	c := startServer(t, map[string]string{"DROI_MEMORY_DIR": dir})
	init := c.call(t, 1, "initialize", map[string]any{"protocolVersion": "2025-06-18"})
	eq(t, init["result"].(map[string]any)["serverInfo"].(map[string]any)["name"], any("memory"))
	list := c.call(t, 2, "tools/list", map[string]any{})
	var names []string
	for _, tool := range list["result"].(map[string]any)["tools"].([]any) {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	eq(t, names, []string{"memory_search", "memory_read", "memory_list", "memory_note"})
	found := c.call(t, 3, "tools/call", map[string]any{"name": "memory_search", "arguments": map[string]any{"query": "pnpm"}})
	content := found["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	eq(t, content, "1 lines in Memory hold every word of \"pnpm\" (read a file with memory_read):\nmemory_summary.md:7: - Run pnpm check.\n")
	c.in.Close()
	if code := <-c.done; code != 0 {
		t.Fatalf("exit %d", code)
	}
}

func TestTheMemoryServerNeedsItsFolder(t *testing.T) {
	var errOut strings.Builder
	eq(t, serveMain(func(string) (string, bool) { return "", false }, strings.NewReader(""), io.Discard, &errOut), 1)
	if !strings.Contains(errOut.String(), "DROI_MEMORY_DIR") {
		t.Fatal(errOut.String())
	}
}
