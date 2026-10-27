package memorywork

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/memory"
)

func eq[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
}

const validSummary = "v1\n## User Profile\nA developer.\n## User preferences\n- Chinese.\n## General Tips\n- pnpm.\n## What's in Memory\n"

// fixture is a Memory folder, a sessions folder and a fake Daemon whose
// Memory Sessions answer what the test says.
type fixture struct {
	t        *testing.T
	root     string
	dir      string
	sessions string
	now      time.Time

	mu       sync.Mutex
	requests []Request
	// summary answers a Session Summary request; memory a Memory Summary one.
	summary func(Request) (string, error)
	memory  func(Request, memoryInput) (string, error)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, root: root, dir: filepath.Join(root, "memory"), sessions: filepath.Join(root, "sessions"), now: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)}
	f.summary = func(Request) (string, error) {
		return `{"summary":"# Task: fix login\nThe user said to use pnpm.","slug":"Fix Login"}`, nil
	}
	f.memory = func(_ Request, in memoryInput) (string, error) {
		var files []string
		for _, c := range in.Changed {
			files = append(files, c.File)
		}
		files = append(files, in.Removed...)
		b, _ := json.Marshal(map[string]string{"memory_summary": validSummary + "- " + strings.Join(files, ", ")})
		return string(b), nil
	}
	return f
}

func (f *fixture) run(_ context.Context, r Request) (Reply, error) {
	f.mu.Lock()
	f.requests = append(f.requests, r)
	n := len(f.requests)
	f.mu.Unlock()
	id := fmt.Sprintf("memory-%d", n)
	var answer string
	var err error
	if strings.HasPrefix(r.Title, "Memory: summarize") {
		answer, err = f.summary(r)
	} else {
		var in memoryInput
		if err := json.Unmarshal([]byte(r.Input), &in); err != nil {
			f.t.Fatalf("input %s", r.Input)
		}
		answer, err = f.memory(r, in)
	}
	return Reply{JSON: json.RawMessage(answer), SessionID: id}, err
}

func (f *fixture) work() Work {
	return Work{Dir: f.dir, Run: f.run, Models: Models{SessionSummary: "flash", MemorySummary: "pro"}, SessionPrompt: "S", MemoryPrompt: "M", Home: f.root, Now: func() time.Time { return f.now }}
}

func (f *fixture) pass(minIdle time.Duration) error {
	f.t.Helper()
	return f.work().Pass(context.Background(), minIdle, false)
}

func (f *fixture) titles() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.requests {
		out = append(out, r.Title)
	}
	return out
}

func (f *fixture) reset() {
	f.mu.Lock()
	f.requests = nil
	f.mu.Unlock()
}

func jsonLine(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// session writes a transcript the hook recorded, last changed idle ago.
func (f *fixture) session(id, cwd string, idle time.Duration, prompts ...string) string {
	f.t.Helper()
	lines := []string{jsonLine(map[string]any{"type": "session_start", "id": id, "cwd": cwd, "title": "Session " + id})}
	for _, p := range prompts {
		lines = append(lines, jsonLine(map[string]any{"type": "message", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": p}}}}))
		lines = append(lines, jsonLine(map[string]any{"type": "message", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "done: " + p}}}}))
	}
	file := filepath.Join(f.sessions, id+".jsonl")
	f.write(file, strings.Join(lines, "\n"))
	at := f.now.Add(-idle)
	if err := os.Chtimes(file, at, at); err != nil {
		f.t.Fatal(err)
	}
	if err := memory.RecordStart(f.dir, memory.Started{SessionID: id, Transcript: file, Cwd: cwd, At: iso(at)}); err != nil {
		f.t.Fatal(err)
	}
	return file
}

func (f *fixture) write(file, body string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(body), 0o666); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) read(rel string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, filepath.FromSlash(rel)))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

func TestAnIdleSessionGetsASessionSummaryAndTheMemorySummaryTakesItIn(t *testing.T) {
	f := newFixture(t)
	f.session("4fab1d5a-5524-400f-802c-ec74d28e5cf8", f.root, 7*time.Hour, "Fix the login bug", "不对，用 pnpm")
	if err := f.pass(MinIdle); err != nil {
		t.Fatal(err)
	}
	eq(t, f.titles(), []string{"Memory: summarize Session 4fab1d5a-5524-400f-802c-ec74d28e5cf8", "Memory: update the Memory Summary"})
	r := f.requests[0]
	eq(t, []any{r.ModelID, r.Prompt, r.Cwd, string(r.ReasoningEffort)}, []any{"flash", "S", f.root, ""})
	if !strings.Contains(r.Input, "User: Fix the login bug\nDroid: done: Fix the login bug\nUser: 不对，用 pnpm\n") || !strings.Contains(r.Input, "- workspace: "+f.root+" (") {
		t.Fatal(r.Input)
	}
	name := "session_summaries/2026-10-27T05-00-00-4fab1d5a-fix-login.md"
	eq(t, f.read(name), "session_id: 4fab1d5a-5524-400f-802c-ec74d28e5cf8\nupdated_at: 2026-10-27T05:00:00Z\nworkspace: "+f.root+"\ntitle: Session 4fab1d5a-5524-400f-802c-ec74d28e5cf8\n\n# Task: fix login\nThe user said to use pnpm.\n")
	m := f.requests[1]
	eq(t, []any{m.ModelID, m.Prompt, string(m.ReasoningEffort)}, []any{"pro", "M", "medium"})
	eq(t, f.read(memory.SummaryFile), validSummary+"- "+name+"\n")
	st := memory.LoadState(f.dir)
	eq(t, st.Phase2.Inputs[name], memory.Hash([]byte(f.read(name))))
	eq(t, st.Summaries[name].SessionID, "4fab1d5a-5524-400f-802c-ec74d28e5cf8")
	eq(t, len(st.Runs), 2)
	eq(t, []string{st.Runs[0].SessionID, st.Runs[0].Kind, st.Runs[0].Model, st.Runs[1].Kind}, []string{"memory-2", memory.RunMemorySummary, "pro", memory.RunSessionSummary})

	// Nothing changed: no Memory Session at all.
	f.reset()
	if err := f.pass(MinIdle); err != nil {
		t.Fatal(err)
	}
	eq(t, len(f.titles()), 0)
}

func TestASessionStillGoingOnOrTooOldOrNotTheUsersIsLeftAlone(t *testing.T) {
	f := newFixture(t)
	f.session("recent", f.root, time.Hour, "hi there")
	f.session("old", f.root, 40*24*time.Hour, "hi there")
	sub := f.session("sub", f.root, 7*time.Hour, "do x")
	f.write(strings.TrimSuffix(sub, ".jsonl")+".settings.json", `{"tags":[{"name":"subagent"}]}`)
	mem := f.session("mem", f.root, 7*time.Hour, "do y")
	f.write(strings.TrimSuffix(mem, ".jsonl")+".settings.json", `{"tags":[{"name":"droi.memory"}]}`)
	f.session("empty", f.root, 7*time.Hour)
	if err := f.pass(MinIdle); err != nil {
		t.Fatal(err)
	}
	eq(t, len(f.titles()), 0)
	st := memory.LoadState(f.dir)
	eq(t, st.Sessions["sub"].Skipped, "subagent")
	eq(t, st.Sessions["mem"].Skipped, "droi.memory")
	_, old := st.Sessions["old"]
	eq(t, old, false)
	eq(t, st.Sessions["empty"].SummarizedMtime != 0 && st.Sessions["empty"].File == "", true)
	eq(t, Waiting(st, f.now), 1)
	// Update now's shorter wait takes the recent one too.
	if err := f.work().Pass(context.Background(), UpdateNowIdle, true); err != nil {
		t.Fatal(err)
	}
	eq(t, f.titles()[0], "Memory: summarize Session recent")
}

func TestASessionThatWentOnIsSummarizedAgainInPlaceOfItsLastSummary(t *testing.T) {
	f := newFixture(t)
	file := f.session("s1", f.root, 7*time.Hour, "first")
	_ = f.pass(MinIdle)
	first := memory.LoadState(f.dir).Sessions["s1"].File
	st := memory.LoadState(f.dir)
	st.Summaries[first].Reads, st.Summaries[first].LastRead = 3, iso(f.now)
	_ = memory.SaveState(f.dir, st)
	later := f.now.Add(-6*time.Hour - time.Minute)
	_ = os.Chtimes(file, later, later)
	f.summary = func(Request) (string, error) { return `{"summary":"second","slug":"again"}`, nil }
	_ = f.pass(MinIdle)
	st = memory.LoadState(f.dir)
	second := st.Sessions["s1"].File
	eq(t, second, "session_summaries/2026-10-27T05-59-00-s1-again.md")
	if _, err := os.Stat(filepath.Join(f.dir, first)); !os.IsNotExist(err) {
		t.Error("the last summary is still there")
	}
	eq(t, st.Summaries[second].Reads, 3)
	if _, ok := st.Summaries[first]; ok {
		t.Error("the last summary is still recorded")
	}
	if !strings.Contains(f.read(memory.SummaryFile), second) {
		t.Error("the Memory Summary did not take in the new summary")
	}
}

func TestAFailedSessionSummaryIsTriedAgainLaterThenLeft(t *testing.T) {
	f := newFixture(t)
	f.session("s1", f.root, 7*time.Hour, "first")
	f.summary = func(Request) (string, error) { return `"prose"`, nil }
	_ = f.pass(MinIdle)
	st := memory.LoadState(f.dir)
	eq(t, []any{st.Sessions["s1"].Failures, st.Sessions["s1"].RetryAt}, []any{1, "2026-10-27T13:00:00Z"})
	eq(t, st.Runs[0].Error, "The reply did not match the schema.")
	f.reset()
	_ = f.pass(MinIdle)
	eq(t, len(f.titles()), 0)
	for i := 0; i < MaxFailures; i++ {
		f.now = f.now.Add(4 * time.Hour)
		_ = f.pass(MinIdle)
	}
	st = memory.LoadState(f.dir)
	eq(t, st.Sessions["s1"].Failures, MaxFailures)
	eq(t, Waiting(st, f.now), 0)
}

func TestAMemorySessionTheDaemonCouldNotStartIsNoFailureOfTheSession(t *testing.T) {
	f := newFixture(t)
	f.session("s1", f.root, 7*time.Hour, "first")
	w := f.work()
	w.Run = func(context.Context, Request) (Reply, error) {
		return Reply{}, errors.New("daemon error -32603: Droid agent host capacity is exhausted; retry after an active session closes")
	}
	_ = w.Pass(context.Background(), MinIdle, false)
	rec := memory.LoadState(f.dir).Sessions["s1"]
	eq(t, []any{rec.Failures, rec.RetryAt, rec.SummarizedMtime}, []any{0, "2026-10-27T13:00:00Z", int64(0)})
}
func TestARunRecordsTheModelTheDaemonRan(t *testing.T) {
	f := newFixture(t)
	f.session("s1", f.root, 7*time.Hour, "first")
	w := f.work()
	w.Run = func(ctx context.Context, r Request) (Reply, error) {
		reply, err := f.run(ctx, r)
		reply.Model = "gpt-6-sol"
		return reply, err
	}
	if err := w.Pass(context.Background(), MinIdle, false); err != nil {
		t.Fatal(err)
	}
	st := memory.LoadState(f.dir)
	eq(t, []string{st.Runs[0].Model, st.Runs[1].Model}, []string{"gpt-6-sol", "gpt-6-sol"})
}
func TestAStoppedPassCountsNoFailure(t *testing.T) {
	f := newFixture(t)
	f.session("s1", f.root, 7*time.Hour, "first")
	_, _ = memory.WriteNote(f.dir, "The user asked to remember: tabs", "", f.now)
	ctx, cancel := context.WithCancel(context.Background())
	w := f.work()
	w.Run = func(context.Context, Request) (Reply, error) {
		cancel()
		return Reply{SessionID: "memory-1"}, context.Canceled
	}
	_ = w.Pass(ctx, MinIdle, false)
	st := memory.LoadState(f.dir)
	rec := st.Sessions["s1"]
	eq(t, []any{rec.Failures, rec.RetryAt, st.Phase2.LastError, st.Phase2.RetryAt}, []any{0, "", "", ""})
}
func TestNothingWorthKeepingLeavesNoSessionSummary(t *testing.T) {
	f := newFixture(t)
	f.session("s1", f.root, 7*time.Hour, "hello")
	f.summary = func(Request) (string, error) { return `{"summary":"  ","slug":""}`, nil }
	_ = f.pass(MinIdle)
	eq(t, memory.LoadState(f.dir).Sessions["s1"].File, "")
	eq(t, f.titles(), []string{"Memory: summarize Session s1"})
}

func TestSecretsAreRedactedFromSessionSummariesAndWhatTheyAreWrittenFrom(t *testing.T) {
	f := newFixture(t)
	f.session("s1", f.root, 7*time.Hour, "deploy with the key fk-abcdefghijklmnopqrstuvwxyz")
	f.summary = func(Request) (string, error) {
		return `{"summary":"The staging db uses password=hunter2.","slug":"db"}`, nil
	}
	_ = f.pass(MinIdle)
	if body := f.read(memory.LoadState(f.dir).Sessions["s1"].File); strings.Contains(body, "hunter2") || !strings.Contains(body, memory.Redacted) {
		t.Fatal(body)
	}
	if in := f.requests[0].Input; strings.Contains(in, "fk-abc") || !strings.Contains(in, "deploy with the key "+memory.Redacted) {
		t.Fatal(in)
	}
}

func TestNotesComeFirstAndALargeChangeGoesInBatches(t *testing.T) {
	f := newFixture(t)
	for i := range 3 {
		f.write(filepath.Join(f.dir, memory.SessionSummariesDir, fmt.Sprintf("2026-10-2%dT00-00-00-x-%d.md", i, i)), "updated_at: 2026-10-2"+fmt.Sprint(i)+"T00:00:00Z\n\n"+strings.Repeat("字", 18_000))
	}
	st := memory.LoadState(f.dir)
	for i := range 3 {
		st.Summaries[fmt.Sprintf("session_summaries/2026-10-2%dT00-00-00-x-%d.md", i, i)] = &memory.SummaryRecord{UpdatedAt: "2026-10-20T00:00:00Z", GeneratedAt: iso(f.now)}
	}
	_ = memory.SaveState(f.dir, st)
	note, _ := memory.WriteNote(f.dir, "The user asked to remember: tabs", "", f.now)
	var seen []memoryInput
	f.memory = func(r Request, in memoryInput) (string, error) {
		seen = append(seen, in)
		b, _ := json.Marshal(map[string]string{"memory_summary": validSummary + fmt.Sprintf("batch %d", in.Batch)})
		return string(b), nil
	}
	if err := f.pass(MinIdle); err != nil {
		t.Fatal(err)
	}
	eq(t, f.titles(), []string{"Memory: update the Memory Summary (1 of 2)", "Memory: update the Memory Summary (2 of 2)"})
	files := func(in memoryInput) []string {
		var out []string
		for _, c := range in.Changed {
			out = append(out, c.File)
		}
		return out
	}
	eq(t, files(seen[0]), []string{note, "session_summaries/2026-10-20T00-00-00-x-0.md", "session_summaries/2026-10-21T00-00-00-x-1.md"})
	eq(t, files(seen[1]), []string{"session_summaries/2026-10-22T00-00-00-x-2.md"})
	eq(t, seen[1].CurrentSummary, validSummary+"batch 1\n")
	eq(t, []int{seen[0].Batches, seen[1].Batch}, []int{2, 2})
	eq(t, f.read(memory.SummaryFile), validSummary+"batch 2\n")
}

func TestAnAnswerOverTheSizeIsShortenedOnce(t *testing.T) {
	f := newFixture(t)
	note, _ := memory.WriteNote(f.dir, "The user asked to remember: tabs", "", f.now)
	long := validSummary + strings.Repeat("字", memory.SummaryMaxBytes/3)
	var seen []memoryInput
	f.memory = func(_ Request, in memoryInput) (string, error) {
		seen = append(seen, in)
		answer := long
		if in.ShortenTo > 0 {
			answer = validSummary + "short"
		}
		b, _ := json.Marshal(map[string]string{"memory_summary": answer})
		return string(b), nil
	}
	if err := f.pass(MinIdle); err != nil {
		t.Fatal(err)
	}
	eq(t, f.titles(), []string{"Memory: update the Memory Summary", "Memory: shorten the Memory Summary"})
	eq(t, []int{seen[0].CurrentBytes, seen[0].LimitBytes, seen[0].ShortenTo}, []int{0, memory.SummaryMaxBytes, 0})
	eq(t, seen[1].CurrentSummary, long+"\n")
	eq(t, []int{seen[1].CurrentBytes, seen[1].ShortenTo, len(seen[1].Changed), len(seen[1].Removed)}, []int{len(long) + 1, ShortenToBytes, 0, 0})
	eq(t, f.read(memory.SummaryFile), validSummary+"short\n")
	st := memory.LoadState(f.dir)
	if st.Phase2.Inputs[note] == "" || st.Phase2.LastError != "" {
		t.Fatalf("the note was not taken in: %+v", st.Phase2)
	}
	// Shortening is part of the work, not a failure to show.
	eq(t, []string{st.Runs[0].Error, st.Runs[1].Error}, []string{"", ""})
}

func TestAShortenedAnswerStillOverTheSizeLosesItsLastPointers(t *testing.T) {
	f := newFixture(t)
	_, _ = memory.WriteNote(f.dir, "The user asked to remember: tabs", "", f.now)
	var pointers []string
	for i := range 200 {
		pointers = append(pointers, fmt.Sprintf("- session_summaries/2026-10-%02dT00-00-00-x-%03d.md — what session %d did and when it matters", i%28+1, i, i))
	}
	long := validSummary + strings.Join(pointers, "\n")
	f.memory = func(Request, memoryInput) (string, error) {
		b, _ := json.Marshal(map[string]string{"memory_summary": long})
		return string(b), nil
	}
	if err := f.pass(MinIdle); err != nil {
		t.Fatal(err)
	}
	eq(t, f.titles(), []string{"Memory: update the Memory Summary", "Memory: shorten the Memory Summary"})
	got := memory.ReadSummary(f.dir)
	if len(got) >= ShortenToBytes || !strings.HasPrefix(got, validSummary+pointers[0]+"\n") || strings.Contains(got, pointers[len(pointers)-1]) || !strings.HasSuffix(got, "\n") {
		t.Fatalf("%d bytes:\n%s", len(got), got)
	}
	eq(t, memory.CheckSummary(got), "")
	st := memory.LoadState(f.dir)
	eq(t, []string{st.Runs[0].Error, st.Runs[1].Error, st.Phase2.LastError}, []string{"", "", ""})
}

func TestFittingASummaryDropsSessionSummaryPointersBeforeAnythingElse(t *testing.T) {
	var lines []string
	for i := range 60 {
		lines = append(lines, fmt.Sprintf("- notes/2026-10-27T00-00-00-imported-p%02d-corrections.md — the user's corrections for project %d, which still apply", i, i))
		lines = append(lines, fmt.Sprintf("- session_summaries/2026-10-%02dT00-00-00-x-%03d.md — what session %d did and when it matters", i%28+1, i, i))
	}
	long := validSummary + strings.Join(lines, "\n") + "\n"
	got := fitSummary(long)
	if strings.Contains(got, "session_summaries/") && len(got) >= ShortenToBytes {
		t.Fatalf("kept pointers over %d bytes: %d", ShortenToBytes, len(got))
	}
	for i := range 60 {
		if !strings.Contains(got, fmt.Sprintf("imported-p%02d-corrections.md", i)) {
			t.Fatalf("lost note %d:\n%s", i, got)
		}
	}
	eq(t, memory.CheckSummary(got), "")
}

func TestFittingASummaryWithoutPointersCutsOnlyToTheLimit(t *testing.T) {
	var lines []string
	for i := range 200 {
		lines = append(lines, fmt.Sprintf("- [note] project %03d: the user's rule that still applies there", i))
	}
	long := validSummary + strings.Join(lines, "\n") + "\n"
	got := fitSummary(long)
	if len(got) >= memory.SummaryMaxBytes || len(got) < memory.SummaryMaxBytes-200 || !strings.HasPrefix(got, validSummary+lines[0]+"\n") {
		t.Fatalf("%d bytes", len(got))
	}
}
func TestAnAnswerTooLongBeforeWhatsInMemoryFailsTheBatch(t *testing.T) {
	f := newFixture(t)
	_, _ = memory.WriteNote(f.dir, "The user asked to remember: tabs", "", f.now)
	f.memory = func(Request, memoryInput) (string, error) {
		long := strings.Replace(validSummary, "- pnpm.\n", strings.Repeat("- a tip that goes on.\n", 600), 1)
		b, _ := json.Marshal(map[string]string{"memory_summary": long})
		return string(b), nil
	}
	err := f.pass(MinIdle)
	if err == nil || !strings.Contains(err.Error(), "bytes") {
		t.Fatalf("error %v", err)
	}
	eq(t, memory.ReadSummary(f.dir), "")
}
func TestAFailedUpdateKeepsTheSummaryAndWhatItTookInAndWaitsAnHour(t *testing.T) {
	f := newFixture(t)
	for i := range 2 {
		f.write(filepath.Join(f.dir, memory.NotesDir, fmt.Sprintf("n%d.md", i)), strings.Repeat("x", 30_000))
	}
	calls := 0
	f.memory = func(Request, memoryInput) (string, error) {
		calls++
		if calls == 2 {
			return `{"memory_summary":"no version line"}`, nil
		}
		b, _ := json.Marshal(map[string]string{"memory_summary": validSummary})
		return string(b), nil
	}
	err := f.pass(MinIdle)
	if err == nil || !strings.Contains(err.Error(), "v1") {
		t.Fatalf("error %v", err)
	}
	st := memory.LoadState(f.dir)
	eq(t, len(st.Phase2.Inputs), 1)
	eq(t, st.Phase2.RetryAt, "2026-10-27T13:00:00Z")
	if st.Phase2.LastError == "" || f.read(memory.SummaryFile) != validSummary {
		t.Fatal("the summary or the error is wrong")
	}
	f.reset()
	_ = f.pass(MinIdle)
	eq(t, len(f.titles()), 0)
	if err := f.work().Pass(context.Background(), MinIdle, true); err != nil {
		t.Fatal(err)
	}
	eq(t, f.titles(), []string{"Memory: update the Memory Summary"})
	eq(t, memory.LoadState(f.dir).Phase2.LastError, "")
}

func TestAFileGoneIsSentAsRemovedAndAnInvalidSummaryIsWrittenAgain(t *testing.T) {
	f := newFixture(t)
	f.write(filepath.Join(f.dir, memory.NotesDir, "a.md"), "a")
	f.write(filepath.Join(f.dir, memory.NotesDir, "b.md"), "b")
	_ = f.pass(MinIdle)
	_ = os.Remove(filepath.Join(f.dir, memory.NotesDir, "a.md"))
	var seen memoryInput
	f.memory = func(_ Request, in memoryInput) (string, error) {
		seen = in
		b, _ := json.Marshal(map[string]string{"memory_summary": validSummary})
		return string(b), nil
	}
	_ = f.pass(MinIdle)
	eq(t, seen.Removed, []string{"notes/a.md"})
	eq(t, len(seen.Changed), 0)
	f.write(filepath.Join(f.dir, memory.SummaryFile), "edited by hand")
	_ = f.pass(MinIdle)
	eq(t, seen.CurrentSummary, "")
	eq(t, len(seen.Changed), 1)
}

func TestSelectDropsWhatNoOneReadAndKeepsTheMostRead(t *testing.T) {
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	st := memory.LoadState(t.TempDir())
	st.Summaries["fresh"] = &memory.SummaryRecord{UpdatedAt: "2026-10-20T00:00:00Z", GeneratedAt: "2026-10-20T00:00:00Z"}
	st.Summaries["stale"] = &memory.SummaryRecord{UpdatedAt: "2026-09-01T00:00:00Z", GeneratedAt: "2026-09-01T00:00:00Z"}
	st.Summaries["read"] = &memory.SummaryRecord{UpdatedAt: "2026-09-01T00:00:00Z", GeneratedAt: "2026-09-01T00:00:00Z", Reads: 2, LastRead: "2026-10-26T00:00:00Z"}
	keep, drop := Select(st, now)
	eq(t, keep, []string{"fresh", "read"})
	eq(t, drop, []string{"stale"})
	for i := range MaxSummaries {
		st.Summaries[fmt.Sprintf("n%03d", i)] = &memory.SummaryRecord{UpdatedAt: "2026-10-25T00:00:00Z", GeneratedAt: "2026-10-25T00:00:00Z"}
	}
	keep, drop = Select(st, now)
	eq(t, len(keep), MaxSummaries)
	if !slices.Contains(keep, "read") || slices.Contains(keep, "fresh") || !slices.Contains(drop, "fresh") {
		t.Fatalf("keep %d, drop %v", len(keep), drop)
	}
}

func TestADroppedSessionSummaryIsDeletedAndSentAsRemoved(t *testing.T) {
	f := newFixture(t)
	f.session("s1", f.root, 7*time.Hour, "first")
	_ = f.pass(MinIdle)
	name := memory.LoadState(f.dir).Sessions["s1"].File
	f.now = f.now.Add(31 * 24 * time.Hour)
	var seen memoryInput
	f.memory = func(_ Request, in memoryInput) (string, error) {
		seen = in
		b, _ := json.Marshal(map[string]string{"memory_summary": validSummary})
		return string(b), nil
	}
	_ = f.pass(MinIdle)
	eq(t, seen.Removed, []string{name})
	if _, err := os.Stat(filepath.Join(f.dir, name)); !os.IsNotExist(err) {
		t.Error("the file is still there")
	}
	_, known := memory.LoadState(f.dir).Sessions["s1"]
	eq(t, known, false)
}

func TestReadsKeepASessionSummaryFromAgingOut(t *testing.T) {
	f := newFixture(t)
	f.session("s1", f.root, 7*time.Hour, "first")
	_ = f.pass(MinIdle)
	name := memory.LoadState(f.dir).Sessions["s1"].File
	if err := memory.RecordRead(f.dir, name); err != nil {
		t.Fatal(err)
	}
	f.now = time.Now().Add(2 * time.Minute)
	_ = f.pass(MinIdle)
	_ = f.pass(MinIdle)
	rec := memory.LoadState(f.dir).Summaries[name]
	eq(t, rec.Reads, 1)
}

// legacyDatabase writes memory.sqlite as the Memory before ADR 0031 kept it.
func legacyDatabase(t *testing.T, dir string, topics bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, LegacyDatabase))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		"CREATE TABLE slots (key TEXT PRIMARY KEY, workspace TEXT, last_consolidated TEXT)",
		"CREATE TABLE entries (rowid INTEGER PRIMARY KEY, id TEXT NOT NULL UNIQUE, slot TEXT NOT NULL, category TEXT NOT NULL, day TEXT NOT NULL, text TEXT NOT NULL)",
		"INSERT INTO slots VALUES ('g', NULL, NULL), ('p', '/Users/dev/app', NULL)",
		"INSERT INTO entries (id, slot, category, day, text) VALUES ('a', 'g', 'preference', '2026-10-01', 'Answer in Chinese.'), ('b', 'p', 'correction', '2026-10-02', 'Use pnpm, not npm.'), ('c', 'p', 'convention', '2026-10-03', 'Tests sit next to the code.'), ('d', 'p', 'insight', '2026-10-05', 'The login lives in src/auth.'), ('e', 'g', 'insight', '2026-10-04', 'win.myhome runs WSL2.')",
	}
	if topics {
		stmts = append(stmts, "CREATE TABLE entry_topics (entry TEXT PRIMARY KEY, topic TEXT NOT NULL)", "INSERT INTO entry_topics VALUES ('e', 'Home network')")
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheEarlierMemoryIsImportedOnce(t *testing.T) {
	f := newFixture(t)
	legacyDatabase(t, f.dir, true)
	if err := f.pass(MinIdle); err != nil {
		t.Fatal(err)
	}
	files, _ := memory.Files(f.dir)
	var names []string
	for _, x := range files {
		names = append(names, x.Path)
	}
	app := (legacyGroup{workspace: "/Users/dev/app"}).slug()
	home := (legacyGroup{topic: "Home network"}).slug()
	global := (legacyGroup{}).slug()
	eq(t, names, []string{
		memory.SummaryFile,
		"session_summaries/2026-10-04T00-00-00-imported-" + home + "-1.md",
		"session_summaries/2026-10-05T00-00-00-imported-" + app + "-1.md",
		"notes/2026-10-27T12-00-00-imported-" + app + "-corrections.md",
		"notes/2026-10-27T12-00-00-imported-" + global + "-preferences.md",
	})
	eq(t, f.read("notes/2026-10-27T12-00-00-imported-"+app+"-corrections.md"), "written_at: 2026-10-27T12:00:00Z\nimported_from: /Users/dev/app\n\nThe user's corrections for /Users/dev/app, kept by Droi's earlier Memory with the day each was recorded. Each still applies unless a later Memory Note says otherwise.\n\n- Use pnpm, not npm. (2026-10-02)\n")
	body := f.read("session_summaries/2026-10-05T00-00-00-imported-" + app + "-1.md")
	if !strings.HasPrefix(body, "session_id: imported\nupdated_at: 2026-10-05T00:00:00Z\nworkspace: /Users/dev/app\ntitle: Earlier Memory of /Users/dev/app\n\n") ||
		!strings.Contains(body, "\n## Conventions\n- Tests sit next to the code. (2026-10-03)\n\n## Insights\n- The login lives in src/auth. (2026-10-05)\n") {
		t.Fatal(body)
	}
	st := memory.LoadState(f.dir)
	eq(t, st.ImportedAt, "2026-10-27T12:00:00Z")
	eq(t, len(st.Phase2.Inputs), 4)
	f.reset()
	_ = f.pass(MinIdle)
	eq(t, len(f.titles()), 0)
}

func TestAnEarlierMemoryWithoutTopicsIsImportedToo(t *testing.T) {
	f := newFixture(t)
	legacyDatabase(t, f.dir, false)
	if err := f.pass(MinIdle); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "session_summaries", "2026-10-04T00-00-00-imported-"+(legacyGroup{}).slug()+"-1.md")); err != nil {
		t.Fatal(err)
	}
}

func TestWithoutAnEarlierMemoryTheImportIsOnlyMarkedDone(t *testing.T) {
	f := newFixture(t)
	_ = f.pass(MinIdle)
	eq(t, memory.LoadState(f.dir).ImportedAt, "2026-10-27T12:00:00Z")
}

func TestTheControllerShowsTheStatusAndUpdatesNow(t *testing.T) {
	f := newFixture(t)
	up := true
	var changes int
	c := New(Options{
		MemoryDir: f.dir,
		Runner: func() Run {
			if up {
				return f.run
			}
			return nil
		},
		Models:   func() Models { return Models{SessionSummary: "flash", MemorySummary: "pro"} },
		OnChange: func() { changes++ },
		Home:     f.root,
		Now:      func() time.Time { return f.now },
	})
	f.session("s1", f.root, 20*time.Minute, "first")
	f.session("s2", f.root, time.Minute, "second")
	_, _ = memory.WriteNote(f.dir, "The user asked to remember: tabs", "", f.now)
	s, err := c.Status()
	if err != nil {
		t.Fatal(err)
	}
	eq(t, []int{s.Summaries, s.Notes, s.Waiting, s.SummaryBytes}, []int{0, 1, 2, 0})
	up = false
	if err := c.UpdateNow(); err == nil || err.Error() != "The Daemon is not running." {
		t.Fatalf("error %v", err)
	}
	up = true
	if err := c.UpdateNow(); err != nil {
		t.Fatal(err)
	}
	s, _ = c.Status()
	eq(t, []int{s.Summaries, s.Notes, s.Waiting}, []int{1, 1, 1})
	eq(t, s.SummaryUpdatedAt, "2026-10-27T12:00:00Z")
	eq(t, s.Working, false)
	eq(t, len(s.Runs), 2)
	if changes < 2 {
		t.Errorf("%d changes", changes)
	}
	if !strings.Contains(f.requests[0].Prompt, "Droi's Memory") || !strings.Contains(f.requests[1].Prompt, "Memory Summary") {
		t.Error("the shipped prompts were not used")
	}
	if c.newNotes() {
		t.Error("the note was not taken in")
	}
	_, _ = memory.WriteNote(f.dir, "The user asked to forget: tabs", "", f.now)
	if !c.newNotes() {
		t.Error("a new note went unseen")
	}
}

func TestTheControllerPassesWhenStartedAndStops(t *testing.T) {
	f := newFixture(t)
	done := make(chan struct{}, 4)
	c := New(Options{MemoryDir: f.dir, Home: f.root, Now: func() time.Time { return f.now }, Runner: func() Run {
		return func(ctx context.Context, r Request) (Reply, error) {
			defer func() { done <- struct{}{} }()
			return f.run(ctx, r)
		}
	}})
	_, _ = memory.WriteNote(f.dir, "The user asked to remember: tabs", "", f.now)
	c.Start()
	c.Start()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("no pass")
	}
	c.Stop()
	eq(t, f.titles(), []string{"Memory: update the Memory Summary"})
}

func TestPromptsAreCopiedForTheUserToEditAndResetToDroisOwn(t *testing.T) {
	dir := t.TempDir()
	text, err := ReadPrompt(dir, PromptMemorySummary)
	if err != nil || !strings.Contains(text, "Memory Summary") {
		t.Fatal(text, err)
	}
	file := filepath.Join(dir, PromptFiles[PromptMemorySummary])
	_ = os.WriteFile(file, []byte("mine"), 0o666)
	text, _ = ReadPrompt(dir, PromptMemorySummary)
	eq(t, text, "mine")
	earlierPrompts[PromptMemorySummary] = []string{memory.Hash([]byte("mine"))}
	t.Cleanup(func() { delete(earlierPrompts, PromptMemorySummary) })
	text, _ = ReadPrompt(dir, PromptMemorySummary)
	if text == "mine" {
		t.Error("an unedited earlier prompt was kept")
	}
	_ = os.WriteFile(file, []byte("mine again"), 0o666)
	if err := ResetPrompts(dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	if string(b) == "mine again" {
		t.Error("reset kept the edit")
	}
}

var _ = errors.New
