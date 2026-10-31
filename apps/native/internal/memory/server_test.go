package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type serverFixture struct {
	dir string
	o   ServerOptions
}

func newServerFixture(t *testing.T) *serverFixture {
	t.Helper()
	dir := tempDir(t)
	writeFile(t, filepath.Join(dir, SummaryFile), validSummary)
	writeFile(t, filepath.Join(dir, SessionSummariesDir, "2026-10-20T10-00-00-aaaaaaaa-login.md"), "session_id: aaaaaaaa\nupdated_at: 2026-10-20T10:00:00Z\nworkspace: /Users/dev/app\ntitle: Fix login\n\n# Fix login\nThe user said to use pnpm, not npm.\nThe session store lives in src/session.ts.\n")
	writeFile(t, filepath.Join(dir, SessionSummariesDir, "2026-10-26T10-00-00-bbbbbbbb-deploy.md"), "session_id: bbbbbbbb\nupdated_at: 2026-10-26T10:00:00Z\nworkspace: /Users/dev/infra\ntitle: Deploy\n\n# Deploy\nDeploy with make ship.\n")
	return &serverFixture{dir: dir, o: ServerOptions{
		Dir:             dir,
		IsMemorySession: func(id string) bool { return id == "memory-session" },
		Now:             func() time.Time { return time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC) },
	}}
}

func (f *serverFixture) call(t *testing.T, name string, args map[string]any) (string, bool) {
	t.Helper()
	r, err := HandleToolCall(f.o, name, args, "s1")
	if err != nil {
		t.Fatal(err)
	}
	return r.Content[0].Text, r.IsError
}

func TestSearchWantsEveryWordFirstThenAny(t *testing.T) {
	f := newServerFixture(t)
	got, _ := f.call(t, "memory_search", map[string]any{"query": "pnpm npm"})
	eq(t, got, "2 lines in Memory hold every word of \"pnpm npm\" (read a file with memory_read):\nsession_summaries/2026-10-20T10-00-00-aaaaaaaa-login.md:7: The user said to use pnpm, not npm.\nmemory_summary.md:7: - Run pnpm check.\n")
	got, _ = f.call(t, "memory_search", map[string]any{"query": "deploy session.ts"})
	if !strings.HasPrefix(got, "4 lines in Memory hold some words of") {
		t.Fatal(got)
	}
	got, _ = f.call(t, "memory_search", map[string]any{"query": `"make ship"`})
	if !strings.Contains(got, "2026-10-26T10-00-00-bbbbbbbb-deploy.md:7: Deploy with make ship.") {
		t.Fatal(got)
	}
	got, _ = f.call(t, "memory_search", map[string]any{"query": "kubernetes"})
	eq(t, got, `Nothing in Memory matches "kubernetes".`)
	if _, isErr := f.call(t, "memory_search", map[string]any{"query": " "}); !isErr {
		t.Error("searched for nothing")
	}
}

func TestReadNumbersTheLinesAndCountsASessionSummarysRead(t *testing.T) {
	f := newServerFixture(t)
	got, isErr := f.call(t, "memory_read", map[string]any{"path": "session_summaries/2026-10-20T10-00-00-aaaaaaaa-login.md", "offset": float64(6), "limit": float64(2)})
	if isErr {
		t.Fatal(got)
	}
	eq(t, got, "session_summaries/2026-10-20T10-00-00-aaaaaaaa-login.md, lines 6-7 of 8:\n6: # Fix login\n7: The user said to use pnpm, not npm.\n")
	_, _ = f.call(t, "memory_read", map[string]any{"path": SummaryFile})
	reads, _ := TakeReads(f.dir, time.Now().Add(time.Hour), time.Minute)
	eq(t, len(reads), 1)
	eq(t, reads[0].File, "session_summaries/2026-10-20T10-00-00-aaaaaaaa-login.md")
	if got, isErr := f.call(t, "memory_read", map[string]any{"path": "../../etc/passwd"}); !isErr || !strings.Contains(got, "Memory has no file") {
		t.Fatal(got)
	}
}

func TestListShowsTheSessionSummariesNewestFirstAndTheNotes(t *testing.T) {
	f := newServerFixture(t)
	if _, err := WriteNote(f.dir, "The user asked to remember: tabs, not spaces", "", time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	got, _ := f.call(t, "memory_list", map[string]any{})
	eq(t, got, "memory_summary.md: the Memory Summary, shown at the start of every Session.\n"+
		"2 Session Summaries, newest first:\n"+
		"- session_summaries/2026-10-26T10-00-00-bbbbbbbb-deploy.md · 2026-10-26 · /Users/dev/infra · Deploy\n"+
		"- session_summaries/2026-10-20T10-00-00-aaaaaaaa-login.md · 2026-10-20 · /Users/dev/app · Fix login\n"+
		"1 Memory Notes:\n- notes/2026-10-27T08-00-00-the-user-asked-to-remember-tabs-not.md\n")
	got, _ = f.call(t, "memory_list", map[string]any{"workspace": "dev/app"})
	if strings.Contains(got, "infra") || !strings.Contains(got, "1 Session Summaries") {
		t.Fatal(got)
	}
}

func TestNoteSavesWhatTheUserAskedFor(t *testing.T) {
	f := newServerFixture(t)
	got, isErr := f.call(t, "memory_note", map[string]any{"text": "The user asked to forget: the old staging host"})
	if isErr || !strings.HasPrefix(got, "Saved notes/2026-10-27T09-00-00-the-user-asked-to-forget-the-old-staging.md.") {
		t.Fatal(got)
	}
	b, _ := os.ReadFile(filepath.Join(f.dir, NotesDir, "2026-10-27T09-00-00-the-user-asked-to-forget-the-old-staging.md"))
	if !strings.Contains(string(b), "session_id: s1\n") {
		t.Fatal(string(b))
	}
	if got, isErr := f.call(t, "memory_note", map[string]any{"text": "api_key: 0123456789abcdef0123"}); !isErr || strings.Contains(got, "0123456789") {
		t.Fatal(got)
	}
}

func TestAMemorySessionGetsNoMemoryTools(t *testing.T) {
	f := newServerFixture(t)
	r, _ := HandleToolCall(f.o, "memory_search", map[string]any{"query": "pnpm"}, "memory-session")
	eq(t, r.IsError, true)
	eq(t, r.Content[0].Text, memorySessionRefusal)
}

func TestSessionStartRecordsTheSessionAndShowsTheSummary(t *testing.T) {
	f := newServerFixture(t)
	transcript := filepath.Join(f.dir, "sessions", "s1.jsonl")
	writeFile(t, transcript, `{"type":"session_start","cwd":"/w"}`)
	out, err := RunHook(f.dir, HookInput{HookEventName: "SessionStart", SessionID: "s1", TranscriptPath: transcript, Cwd: "/w", Source: "startup"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "<memory-context>\n## Memory\n") || !strings.Contains(out, "========= MEMORY_SUMMARY BEGINS =========\n"+strings.TrimSpace(validSummary)+"\n========= MEMORY_SUMMARY ENDS =========\n") || !strings.HasSuffix(out, "</memory-context>\n") {
		t.Fatal(out)
	}
	started, _ := ReadStarted(f.dir)
	eq(t, len(started), 1)
	eq(t, []string{started[0].SessionID, started[0].Transcript, started[0].Cwd}, []string{"s1", transcript, "/w"})
}

func TestSessionStartShowsTheNotesTheSummaryHasNotTakenIn(t *testing.T) {
	f := newServerFixture(t)
	at := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	taken, _ := WriteNote(f.dir, "The user asked to remember: tabs", "", at)
	_, _ = WriteNote(f.dir, "The user asked to remember: deploys on Fridays are fine", "", at.Add(time.Minute))
	b, _ := os.ReadFile(filepath.Join(f.dir, filepath.FromSlash(taken)))
	st := LoadState(f.dir)
	st.Phase2.Inputs[taken] = Hash(b)
	if err := SaveState(f.dir, st); err != nil {
		t.Fatal(err)
	}
	out, _ := RunHook(f.dir, HookInput{HookEventName: "SessionStart"})
	if !strings.Contains(out, "does not include yet, newest first; they win over it:\n- The user asked to remember: deploys on Fridays are fine\n</memory-context>") || strings.Contains(out, "remember: tabs") {
		t.Fatal(out)
	}
}

func TestSessionStartBeforeAnySummaryExplainsWhy(t *testing.T) {
	dir := tempDir(t)
	out, _ := RunHook(dir, HookInput{HookEventName: "SessionStart"})
	if !strings.Contains(out, noSummary) {
		t.Fatal(out)
	}
}

func TestTheHookStaysOutOfMemorySessionsAndOtherEvents(t *testing.T) {
	f := newServerFixture(t)
	transcript := writeSessionFile(t, f.dir, "sessions", "mem", `{"type":"session_start","cwd":"/w"}`)
	writeFile(t, SessionSettingsFile(transcript), `{"tags":[{"name":"droi.memory"}]}`)
	out, _ := RunHook(f.dir, HookInput{HookEventName: "SessionStart", SessionID: "mem", TranscriptPath: transcript})
	eq(t, out, "")
	out, _ = RunHook(f.dir, HookInput{HookEventName: "SessionEnd", SessionID: "s", TranscriptPath: transcript})
	eq(t, out, "")
	started, _ := ReadStarted(f.dir)
	eq(t, len(started), 0)
}

func TestTheHookEntryPrintsAndAlwaysExitsZero(t *testing.T) {
	f := newServerFixture(t)
	env := func(key string) (string, bool) {
		if key == "DROI_MEMORY_DIR" {
			return f.dir, true
		}
		return "", false
	}
	var out, errOut strings.Builder
	eq(t, hookMain(env, strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"x"}`), &out, &errOut), 0)
	if !strings.Contains(out.String(), "MEMORY_SUMMARY BEGINS") || errOut.String() != "" {
		t.Fatalf("%q %q", out.String(), errOut.String())
	}
	out.Reset()
	eq(t, hookMain(env, strings.NewReader("not json"), &out, &errOut), 0)
	if out.String() != "" || !strings.HasPrefix(errOut.String(), "memory hook: ") {
		t.Fatalf("%q %q", out.String(), errOut.String())
	}
}
