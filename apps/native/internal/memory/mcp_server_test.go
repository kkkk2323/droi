package memory

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const testSession = "622a994c-cfbe-483c-848b-fbcd93ee8055"

type serverEnv struct {
	memoryDir, workspace, factoryHome, sessionsFolder string
	in                                                *io.PipeWriter
	stderr                                            *lockedBuilder
	mu                                                sync.Mutex
	nextID                                            int
	waiting                                           map[int]chan map[string]any
	lines                                             chan string
	done                                              chan int
}

type lockedBuilder struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuilder) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuilder) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func startServer(t *testing.T) *serverEnv {
	t.Helper()
	root := must[string](t)(filepath.EvalSymlinks(t.TempDir()))
	s := &serverEnv{
		memoryDir: filepath.Join(root, "memory"), workspace: filepath.Join(root, "workspace"),
		factoryHome: filepath.Join(root, "home", ".factory"), stderr: &lockedBuilder{},
		nextID: 1, waiting: map[int]chan map[string]any{}, lines: make(chan string, 100), done: make(chan int, 1),
	}
	if err := os.Mkdir(s.workspace, 0o777); err != nil {
		t.Fatal(err)
	}
	// The Daemon keeps a file per Session that says where it runs; the server's
	// own cwd is the Daemon's, not the Session's.
	s.sessionsFolder = filepath.Join(s.factoryHome, "sessions", strings.ReplaceAll(s.workspace, "/", "-"))
	if err := os.MkdirAll(s.sessionsFolder, 0o777); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(s.sessionsFolder, testSession+".jsonl"), `{"type":"session_start","id":"`+testSession+`","title":"t","cwd":"`+s.workspace+`"}`+"\n")

	env := map[string]string{"DROI_MEMORY_DIR": s.memoryDir, "FACTORY_HOME_OVERRIDE": filepath.Dir(s.factoryHome)}
	lookup := func(key string) (string, bool) { v, ok := env[key]; return v, ok }
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s.in = inW
	go func() {
		s.done <- serveMain(lookup, inR, outW, s.stderr)
		outW.Close()
	}()
	go func() {
		scanner := bufio.NewScanner(outR)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				continue
			}
			id, _ := m["id"].(float64)
			s.mu.Lock()
			ch := s.waiting[int(id)]
			s.mu.Unlock()
			if ch != nil {
				ch <- m
			} else {
				s.lines <- line
			}
		}
	}()
	t.Cleanup(func() {
		s.in.Close()
		select {
		case code := <-s.done:
			if code != 0 {
				t.Errorf("server exited %d: %s", code, s.stderr.String())
			}
		case <-time.After(10 * time.Second):
			t.Error("server did not stop")
		}
	})
	return s
}

func (s *serverEnv) write(t *testing.T, message any) {
	t.Helper()
	b, _ := json.Marshal(message)
	if _, err := s.in.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
}

func (s *serverEnv) request(t *testing.T, method string, params map[string]any) map[string]any {
	t.Helper()
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	ch := make(chan map[string]any, 1)
	s.waiting[id] = ch
	s.mu.Unlock()
	message := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		message["params"] = params
	}
	s.write(t, message)
	select {
	case m := <-ch:
		return m
	case <-time.After(10 * time.Second):
		t.Fatalf("no answer to %s", method)
		return nil
	}
}

type callResult struct {
	text    string
	isError bool
}

func (s *serverEnv) call(t *testing.T, name string, args map[string]any, sessionID ...string) callResult {
	t.Helper()
	session := testSession
	if len(sessionID) > 0 {
		session = sessionID[0]
	}
	params := map[string]any{"name": name, "arguments": args}
	if session != "" {
		params["_meta"] = map[string]any{"assemblySessionId": session, "caller": "AGENT"}
	}
	result := s.request(t, "tools/call", params)["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	isError, _ := result["isError"].(bool)
	return callResult{text, isError}
}

func (s *serverEnv) openStore(t *testing.T) *Store {
	t.Helper()
	store := must[*Store](t)(OpenStore(s.memoryDir))
	t.Cleanup(func() { store.Close() })
	return store
}

var savedID = regexp.MustCompile(`Saved ([0-9a-f]{8}) in Project Memory`)

func TestServerInitializesAndListsItsFiveTools(t *testing.T) {
	s := startServer(t)
	init := s.request(t, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}})
	b, _ := json.Marshal(init)
	eq(t, string(b), `{"id":1,"jsonrpc":"2.0","result":{"capabilities":{"tools":{}},"protocolVersion":"2025-06-18","serverInfo":{"name":"droi-memory","title":"Droi Memory","version":"1.0.0"}}}`)
	s.write(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})

	listed := s.request(t, "tools/list", nil)["result"].(map[string]any)["tools"].([]any)
	var names []string
	readOnly := map[string]bool{}
	var addRequired []any
	for _, tool := range listed {
		m := tool.(map[string]any)
		name := m["name"].(string)
		names = append(names, name)
		readOnly[name] = m["annotations"].(map[string]any)["readOnlyHint"].(bool)
		if name == "memory_add" {
			addRequired = m["inputSchema"].(map[string]any)["required"].([]any)
		}
	}
	eq(t, names, []string{"memory_search", "memory_list", "memory_add", "memory_replace", "memory_remove"})
	eq(t, readOnly, map[string]bool{"memory_search": true, "memory_list": true, "memory_add": false, "memory_replace": false, "memory_remove": false})
	eq(t, addRequired, []any{"scope", "category", "text"})
	select {
	case line := <-s.lines:
		t.Fatalf("answered a notification: %s", line)
	default:
	}
}

func TestServerAddsToTheWorkspaceItRunsInThenSearchesReplacesAndRemoves(t *testing.T) {
	s := startServer(t)
	added := s.call(t, "memory_add", map[string]any{"scope": "project", "category": "convention", "text": "Run pnpm check before committing"})
	eq(t, added.isError, false)
	m := savedID.FindStringSubmatch(added.text)
	if m == nil {
		t.Fatal(added.text)
	}
	id := m[1]

	store := s.openStore(t)
	eq(t, texts(must[[]Entry](t)(store.List(Slot{Scope: ScopeProject, Workspace: s.workspace}, ""))), []string{"Run pnpm check before committing"})
	eq(t, must[bool](t)(store.HasWrite(testSession)), true)
	eq(t, must[bool](t)(store.HasWrite("another-session")), false)
	store.Close()

	if found := s.call(t, "memory_search", map[string]any{"query": "pnpm"}); !strings.Contains(found.text, id+" [project/convention") {
		t.Fatal(found.text)
	}
	if found := s.call(t, "memory_search", map[string]any{"query": "pnpm", "scope": "global"}); !strings.HasPrefix(found.text, "No entries") {
		t.Fatal(found.text)
	}
	eq(t, s.call(t, "memory_replace", map[string]any{"id": id, "text": "Run pnpm check && pnpm test"}).text, "Replaced "+id+" in Project Memory.")
	if listed := s.call(t, "memory_list", map[string]any{"scope": "project"}); !strings.Contains(listed.text, "pnpm check && pnpm test") {
		t.Fatal(listed.text)
	}
	eq(t, s.call(t, "memory_remove", map[string]any{"id": id}).text, "Removed "+id+".")
	eq(t, s.call(t, "memory_remove", map[string]any{"id": id}), callResult{"No Memory entry has the id " + id + ".", true})

	// Every write exported the Markdown file.
	md := string(must[[]byte](t)(os.ReadFile(filepath.Join(s.memoryDir, MarkdownPath(Slot{Scope: ScopeProject, Workspace: s.workspace})))))
	if !strings.Contains(md, "No entries yet.") {
		t.Fatal(md)
	}
}

func TestServerKeepsGlobalMemoryApart(t *testing.T) {
	s := startServer(t)
	s.call(t, "memory_add", map[string]any{"scope": "global", "category": "preference", "text": "Answer in Chinese"})
	if r := s.call(t, "memory_list", map[string]any{"scope": "global"}); !strings.Contains(r.text, "[global/preference") {
		t.Fatal(r.text)
	}
	if r := s.call(t, "memory_list", map[string]any{"scope": "project"}); !strings.HasPrefix(r.text, "No entries") {
		t.Fatal(r.text)
	}
}

func TestServerReachesOnlyGlobalMemoryForASessionItCannotPlace(t *testing.T) {
	s := startServer(t)
	r := s.call(t, "memory_add", map[string]any{"scope": "project", "category": "insight", "text": "x"}, "unknown-session")
	if !r.isError || !strings.Contains(r.text, `scope "global"`) {
		t.Fatalf("%+v", r)
	}
	eq(t, s.call(t, "memory_add", map[string]any{"scope": "global", "category": "preference", "text": "terse answers"}, "").isError, false)
}

func TestServerGivesAMemorySessionNoMemoryOfItsOwn(t *testing.T) {
	s := startServer(t)
	memorySession := "9b1f2c3d-0000-4000-8000-000000000001"
	writeFile(t, filepath.Join(s.sessionsFolder, memorySession+".jsonl"), `{"type":"session_start","id":"`+memorySession+`","cwd":"`+s.workspace+`"}`+"\n")
	writeFile(t, filepath.Join(s.sessionsFolder, memorySession+".settings.json"), `{"tags":[{"name":"droi.memory"}]}`)
	for _, c := range []struct {
		name string
		args map[string]any
	}{
		{"memory_add", map[string]any{"scope": "global", "category": "insight", "text": "x"}},
		{"memory_search", map[string]any{"query": "anything"}},
	} {
		r := s.call(t, c.name, c.args, memorySession)
		if !r.isError || !strings.Contains(r.text, "Memory Session") {
			t.Fatalf("%s: %+v", c.name, r)
		}
	}
	if r := s.call(t, "memory_list", map[string]any{"scope": "global"}); !strings.HasPrefix(r.text, "No entries") {
		t.Fatal(r.text)
	}
}

func TestServerGivesAScratchSessionNoProjectMemory(t *testing.T) {
	s := startServer(t)
	scratch := "9b1f2c3d-0000-4000-8000-000000000002"
	writeFile(t, filepath.Join(s.sessionsFolder, scratch+".jsonl"), `{"type":"session_start","id":"`+scratch+`","cwd":"`+s.workspace+`"}`+"\n")
	writeFile(t, filepath.Join(s.sessionsFolder, scratch+".settings.json"), `{"tags":[{"name":"droi.scratch"}]}`)
	r := s.call(t, "memory_add", map[string]any{"scope": "project", "category": "convention", "text": "x"}, scratch)
	if !r.isError || !regexp.MustCompile(`no Project Memory.*"global"`).MatchString(r.text) {
		t.Fatalf("%+v", r)
	}
	for _, c := range []struct {
		name string
		args map[string]any
	}{
		{"memory_search", map[string]any{"query": "x"}},
		{"memory_list", map[string]any{"scope": "project"}},
	} {
		if r := s.call(t, c.name, c.args, scratch); r.isError || !strings.Contains(r.text, "no Project Memory") {
			t.Fatalf("%s: %+v", c.name, r)
		}
	}
	if r := s.call(t, "memory_add", map[string]any{"scope": "global", "category": "insight", "text": "win.myhome runs WSL2"}, scratch); r.isError || !strings.Contains(r.text, "in Global Memory") {
		t.Fatalf("%+v", r)
	}
	eq(t, must[[]Entry](t)(s.openStore(t).List(Slot{Scope: ScopeProject, Workspace: s.workspace}, "")), []Entry{})
}

func TestServerLogsEveryCall(t *testing.T) {
	s := startServer(t)
	added := s.call(t, "memory_add", map[string]any{"scope": "project", "category": "convention", "text": "Run pnpm check before committing"})
	id := regexp.MustCompile(`Saved ([0-9a-f]{8})`).FindStringSubmatch(added.text)[1]
	s.call(t, "memory_search", map[string]any{"query": "pnpm"})
	s.call(t, "memory_search", map[string]any{"query": "docker"})
	s.call(t, "memory_search", map[string]any{"query": "x", "scope": "nowhere"})
	s.call(t, "memory_remove", map[string]any{"id": id})

	db := must[*sql.DB](t)(sql.Open("sqlite", filepath.Join(s.memoryDir, DatabaseFile)))
	defer db.Close()
	rows := must[*sql.Rows](t)(db.Query(`SELECT c.session_id AS sessionId, c.tool, c.slot IS NOT NULL AS reached, c.query, c.ok,
           (SELECT group_concat(h.role || ':' || h.entry) FROM call_entries h WHERE h.call = c.rowid) AS entries
         FROM calls c ORDER BY c.rowid`))
	var got []string
	for rows.Next() {
		var session, tool string
		var reached, ok int
		var query, entries sql.NullString
		if err := rows.Scan(&session, &tool, &reached, &query, &ok, &entries); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%s %s %d %v %d %v", session, tool, reached, query, ok, entries))
	}
	rows.Close()
	null := sql.NullString{}
	str := func(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
	want := []string{
		fmt.Sprintf("%s memory_add 1 %v 1 %v", testSession, null, str("written:"+id)),
		fmt.Sprintf("%s memory_search 1 %v 1 %v", testSession, str("pnpm"), str("found:"+id)),
		fmt.Sprintf("%s memory_search 1 %v 1 %v", testSession, str("docker"), null),
		fmt.Sprintf("%s memory_search 0 %v 0 %v", testSession, null, null),
		fmt.Sprintf("%s memory_remove 1 %v 1 %v", testSession, null, str("written:"+id)),
	}
	eq(t, got, want)
	eq(t, must[Usage](t)(s.openStore(t).Usage(Slot{Scope: ScopeProject, Workspace: s.workspace})), Usage{Searches: 2, EmptySearches: 1})
}

func TestServerAnswersAnAddWithEntriesOfEitherScopeThatMaySayTheSameThing(t *testing.T) {
	s := startServer(t)
	old := s.call(t, "memory_add", map[string]any{"scope": "global", "category": "tool-quirk", "text": "droid 0.231 gates Script behind the script_tools feature flag; set FACTORY_FEATURE_FLAGS_SNAPSHOT_PATH to turn it on."})
	oldID := regexp.MustCompile(`Saved ([0-9a-f]{8})`).FindStringSubmatch(old.text)[1]
	if strings.Contains(old.text, "may say the same thing") {
		t.Fatal(old.text)
	}

	added := s.call(t, "memory_add", map[string]any{"scope": "project", "category": "insight", "text": "Since droid 0.233 the script_tools feature flag is gone, so FACTORY_FEATURE_FLAGS_SNAPSHOT_PATH is no longer needed for Script."})
	newID := regexp.MustCompile(`Saved ([0-9a-f]{8}) in Project Memory\.`).FindStringSubmatch(added.text)[1]
	eq(t, added.isError, false)
	for _, want := range []string{"1 entry already in Memory may say the same thing", "- " + oldID + " [global/tool-quirk", "remove " + newID} {
		if !strings.Contains(added.text, want) {
			t.Fatalf("%q lacks %q", added.text, want)
		}
	}

	unrelated := s.call(t, "memory_add", map[string]any{"scope": "project", "category": "convention", "text": "The Phone App is pinned to Expo SDK 57."})
	if !regexp.MustCompile(`^Saved [0-9a-f]{8} in Project Memory\.$`).MatchString(unrelated.text) {
		t.Fatal(unrelated.text)
	}

	db := must[*sql.DB](t)(sql.Open("sqlite", filepath.Join(s.memoryDir, DatabaseFile)))
	defer db.Close()
	var similar []string
	rows := must[*sql.Rows](t)(db.Query(`SELECT entry FROM call_entries WHERE role = 'similar'`))
	for rows.Next() {
		var e string
		_ = rows.Scan(&e)
		similar = append(similar, e)
	}
	rows.Close()
	eq(t, similar, []string{oldID})
}

func TestServerReportsRefusalsAsToolErrors(t *testing.T) {
	s := startServer(t)
	if r := s.call(t, "memory_add", map[string]any{"scope": "project", "category": "insight", "text": "password=hunter2"}); !r.isError || !strings.Contains(r.text, "password") {
		t.Fatalf("%+v", r)
	}
	eq(t, s.call(t, "memory_add", map[string]any{"scope": "project", "category": "gossip", "text": "x"}), callResult{
		"memory_add needs a scope, a category (failure, correction, insight, preference, convention, tool-quirk) and text.", true,
	})
}

func TestServerAnswersUnknownMethodsWithAnErrorAndIgnoresNotifications(t *testing.T) {
	s := startServer(t)
	s.write(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled"})
	unknown := s.request(t, "resources/list", nil)
	eq(t, unknown["error"], any(map[string]any{"code": float64(-32601), "message": "Method not found: resources/list"}))
	ping := s.request(t, "ping", nil)
	eq(t, ping["result"], any(map[string]any{}))
	select {
	case line := <-s.lines:
		t.Fatalf("answered a notification: %s", line)
	default:
	}
}

func TestServerAnswersAParseError(t *testing.T) {
	s := startServer(t)
	if _, err := s.in.Write([]byte("not json\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-s.lines:
		eq(t, line, `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}`)
	case <-time.After(10 * time.Second):
		t.Fatal("no answer")
	}
}

func TestServeMainNeedsTheMemoryFolder(t *testing.T) {
	var stderr strings.Builder
	code := serveMain(func(string) (string, bool) { return "", false }, strings.NewReader(""), io.Discard, &stderr)
	eq(t, code, 1)
	eq(t, stderr.String(), "droi-memory: DROI_MEMORY_DIR is not set\n")
}

func TestToolsAreTheElectronServersToTheByte(t *testing.T) {
	var tools []map[string]any
	if err := json.Unmarshal(Tools(), &tools); err != nil {
		t.Fatal(err)
	}
	eq(t, len(tools), 5)
	line, err := jsonText(rpcMessage{JSONRPC: "2.0", ID: json.RawMessage("7"), Result: struct {
		Tools json.RawMessage `json:"tools"`
	}{Tools()}})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, line, `{"jsonrpc":"2.0","id":7,"result":{"tools":`+strings.TrimSpace(string(toolsJSON))+`}}`)
}
