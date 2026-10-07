package memorywork

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/memory"
)

type workFixture struct {
	root      string
	workspace string
	store     *memory.Store
}

func newWorkFixture(t *testing.T) *workFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := memory.OpenStore(filepath.Join(root, "memory"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &workFixture{root: root, workspace: root, store: store}
}

func (f *workFixture) project() memory.Slot {
	return memory.Slot{Scope: memory.ScopeProject, Workspace: f.workspace}
}

func (f *workFixture) add(t *testing.T, text string, category memory.Category) memory.Entry {
	t.Helper()
	r, err := f.store.Add(f.project(), category, text)
	if err != nil || !r.OK {
		t.Fatalf("add %q: %v %s", text, err, r.Reason)
	}
	return r.Entry
}

func (f *workFixture) texts(t *testing.T, slot memory.Slot, category memory.Category) []string {
	t.Helper()
	entries, err := f.store.List(slot, category)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range entries {
		out = append(out, e.Text)
	}
	return out
}

// fakeRun is a stand-in Memory Session that records what it was asked and
// answers with answer.
type fakeRun struct {
	mu       sync.Mutex
	requests []Request
	answer   func(r Request, sent []sentEntry) (any, error)
}

func (f *fakeRun) run(_ context.Context, r Request) (json.RawMessage, error) {
	f.mu.Lock()
	f.requests = append(f.requests, r)
	f.mu.Unlock()
	var input struct{ Entries []sentEntry }
	_ = json.Unmarshal([]byte(r.Input), &input)
	v, err := f.answer(r, input.Entries)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func (f *fakeRun) titles() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, r := range f.requests {
		out = append(out, r.Title)
	}
	return out
}

type changes = map[string]any

func TestConsolidationSendsOneCategorySliceAtATimeAndAppliesTheChangesToTheIDsItSent(t *testing.T) {
	f := newWorkFixture(t)
	a, b, c := f.add(t, "uses pnpm", "convention"), f.add(t, "uses pnpm workspaces", "convention"), f.add(t, "tests next to code", "convention")
	failure0 := f.add(t, "build broke on node 20", "failure")
	f.add(t, "build broke on node 20 again", "failure")
	lonely := f.add(t, "only preference", "preference")
	fake := &fakeRun{answer: func(r Request, sent []sentEntry) (any, error) {
		if strings.HasSuffix(r.Title, "/ convention") {
			return changes{"keep": []string{c.ID}, "rewrite": []any{}, "remove": []any{},
				"merge": []any{changes{"ids": []string{a.ID, b.ID}, "text": "uses pnpm workspaces"}}}, nil
		}
		return changes{"keep": []any{}, "rewrite": []any{}, "remove": []string{sent[1].ID}, "merge": []any{}}, nil
	}}
	outcomes, err := Consolidate(context.Background(), Work{Store: f.store, Run: fake.run, ModelID: "glm-5.3-flash", Prompt: "P"}, f.project())
	if err != nil {
		t.Fatal(err)
	}
	// A category with one entry has nothing to consolidate.
	name := filepath.Base(f.root)
	if got, want := fake.titles(), []string{"Memory: consolidate " + name + " / failure", "Memory: consolidate " + name + " / convention"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("titles %q, want %q", got, want)
	}
	first := fake.requests[0]
	if first.Cwd != f.workspace || first.ModelID != "glm-5.3-flash" || first.Prompt != "P" || !reflect.DeepEqual(first.Schema, ConsolidationSchema) {
		t.Errorf("request %+v", first)
	}
	if !strings.HasPrefix(first.Input, `{"memory":"`+f.workspace+`","category":"failure","entries":[{"id":"`+failure0.ID+`","day":"`) {
		t.Errorf("input %s", first.Input)
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes %+v", outcomes)
	}
	if o := outcomes[0]; o.Category != "failure" || o.Entries != 2 || o.OK || !strings.Contains(o.Reason, failure0.ID) {
		t.Errorf("failure outcome %+v", o)
	}
	if o := outcomes[1]; o != (SliceOutcome{Category: "convention", Entries: 3, OK: true}) {
		t.Errorf("convention outcome %+v", o)
	}
	// The failure slice named only one of its two ids, so nothing changed there.
	if got := f.texts(t, f.project(), "failure"); len(got) != 2 {
		t.Errorf("failures %q", got)
	}
	got := f.texts(t, f.project(), "convention")
	sort.Strings(got)
	if want := []string{"tests next to code", "uses pnpm workspaces"}; !reflect.DeepEqual(got, want) {
		t.Errorf("conventions %q", got)
	}
	if _, ok, _ := f.store.Get(lonely.ID); !ok {
		t.Error("the lonely preference is gone")
	}
	summaries, _ := f.store.Summaries()
	if summaries[0].LastConsolidated == "" {
		t.Error("not marked consolidated")
	}
	md, err := os.ReadFile(filepath.Join(f.store.Dir(), memory.MarkdownPath(f.project())))
	if err != nil || !strings.Contains(string(md), "uses pnpm workspaces") {
		t.Errorf("markdown %v %s", err, md)
	}
}

func TestConsolidationRejectsAReplyThatTouchesAnUnsentEntryAndOneThatIsNotTheSchema(t *testing.T) {
	f := newWorkFixture(t)
	a, b := f.add(t, "a1", "convention"), f.add(t, "b2", "convention")
	outsider := f.add(t, "outsider", "insight")
	f.add(t, "other insight", "insight")
	calls := 0
	fake := &fakeRun{answer: func(Request, []sentEntry) (any, error) {
		calls++
		if calls == 1 {
			return changes{"keep": []string{a.ID, b.ID}, "rewrite": []any{}, "remove": []string{outsider.ID}, "merge": []any{}}, nil
		}
		return "prose", nil
	}}
	outcomes, err := Consolidate(context.Background(), Work{Store: f.store, Run: fake.run, ModelID: "m", Prompt: "P"}, f.project())
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 2 || outcomes[0].OK || outcomes[1].OK {
		t.Errorf("outcomes %+v", outcomes)
	}
	if got := f.texts(t, f.project(), ""); len(got) != 4 {
		t.Errorf("entries %q", got)
	}
	// Nothing held up, so the Memory does not count as consolidated.
	summaries, _ := f.store.Summaries()
	if summaries[0].LastConsolidated != "" {
		t.Error("marked consolidated")
	}
}

func TestConsolidationKeepsGoingWhenOneMemorySessionFails(t *testing.T) {
	f := newWorkFixture(t)
	f.add(t, "x1", "failure")
	f.add(t, "x2", "failure")
	a, b := f.add(t, "c1", "convention"), f.add(t, "c2", "convention")
	fake := &fakeRun{answer: func(r Request, _ []sentEntry) (any, error) {
		if strings.HasSuffix(r.Title, "/ failure") {
			return nil, errors.New("Daemon went away")
		}
		return changes{"keep": []string{a.ID, b.ID}, "rewrite": []any{}, "remove": []any{}, "merge": []any{}}, nil
	}}
	outcomes, err := Consolidate(context.Background(), Work{Store: f.store, Run: fake.run, ModelID: "m", Prompt: "P"}, f.project())
	if err != nil {
		t.Fatal(err)
	}
	want := []SliceOutcome{
		{Category: "failure", Entries: 2, Reason: "Daemon went away"},
		{Category: "convention", Entries: 2, OK: true},
	}
	if !reflect.DeepEqual(outcomes, want) {
		t.Errorf("outcomes %+v", outcomes)
	}
}

func TestConsolidationSplitsALargeCategoryIntoSlices(t *testing.T) {
	var entries []memory.Entry
	for i := range 5 {
		entries = append(entries, memory.Entry{ID: string(rune('0' + i)), Text: strings.Repeat("x", SliceChars/2-1)})
	}
	var sizes []int
	for _, s := range SlicesOf(entries) {
		sizes = append(sizes, len(s))
	}
	if !reflect.DeepEqual(sizes, []int{2, 2, 1}) {
		t.Errorf("slices %v", sizes)
	}
}

func TestConsolidationRunsAGlobalMemoryConsolidationInTheHomeFolder(t *testing.T) {
	f := newWorkFixture(t)
	a, _ := f.store.Add(memory.GlobalSlot, "preference", "terse")
	b, _ := f.store.Add(memory.GlobalSlot, "preference", "very terse")
	if !a.OK || !b.OK {
		t.Fatal("add failed")
	}
	fake := &fakeRun{answer: func(Request, []sentEntry) (any, error) {
		return changes{"keep": []any{}, "rewrite": []any{}, "remove": []string{a.Entry.ID},
			"merge": []any{changes{"ids": []string{b.Entry.ID}, "text": "terse"}}}, nil
	}}
	if _, err := Consolidate(context.Background(), Work{Store: f.store, Run: fake.run, ModelID: "m", Prompt: "P"}, memory.GlobalSlot); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if r := fake.requests[0]; r.Title != "Memory: consolidate Global Memory / preference" || r.Cwd != home || !strings.HasPrefix(r.Input, `{"memory":"global",`) {
		t.Errorf("request %+v", r)
	}
	if got := f.texts(t, memory.GlobalSlot, ""); !reflect.DeepEqual(got, []string{"terse"}) {
		t.Errorf("global %q", got)
	}
}

func TestParseSliceChangesRefusesAnythingButTheSchema(t *testing.T) {
	for _, reply := range []string{`"prose"`, `null`, `{}`, `{"keep":[1],"rewrite":[],"remove":[],"merge":[]}`,
		`{"keep":[],"rewrite":[{"id":"a"}],"remove":[],"merge":[]}`, `{"keep":[],"rewrite":[],"remove":[],"merge":[{"ids":"a","text":"t"}]}`} {
		if _, ok := ParseSliceChanges(json.RawMessage(reply)); ok {
			t.Errorf("accepted %s", reply)
		}
	}
	got, ok := ParseSliceChanges(json.RawMessage(`{"keep":["a"],"rewrite":[{"id":"b","text":"B","x":1}],"remove":["c"],"merge":[{"ids":["d","e"],"text":"DE"}]}`))
	want := memory.SliceChanges{Keep: []string{"a"}, Rewrite: []memory.Rewrite{{ID: "b", Text: "B"}}, Remove: []string{"c"}, Merge: []memory.Merge{{IDs: []string{"d", "e"}, Text: "DE"}}}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("parsed %+v %v", got, ok)
	}
}

func writeTranscript(t *testing.T, f *workFixture) string {
	t.Helper()
	path := filepath.Join(f.root, "session.jsonl")
	lines := []string{
		`{"type":"session_start","cwd":` + jsonText(f.workspace) + `}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"Use pnpm here, not npm"}]}}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":"Switching to pnpm."}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o666); err != nil {
		t.Fatal(err)
	}
	return path
}

type sentInput struct {
	Workspace  *string         `json:"workspace"`
	Existing   []existingEntry `json:"existing"`
	Transcript string          `json:"transcript"`
}

func TestExtractionSendsTheConversationAndInsertsTheEntriesItGetsBack(t *testing.T) {
	f := newWorkFixture(t)
	f.add(t, "already known", "convention")
	fake := &fakeRun{answer: func(Request, []sentEntry) (any, error) {
		return changes{"entries": []any{
			changes{"scope": "project", "category": "correction", "text": "Use pnpm, not npm"},
			changes{"scope": "global", "category": "preference", "text": "Likes short answers"},
			changes{"scope": "project", "category": "gossip", "text": "not a category"},
			changes{"scope": "project", "category": "insight", "text": "token is password=abc123"},
		}}, nil
	}}
	const id = "0427515a-a1da-4056-9e5e-c4d2e1780ed1"
	added, refused, err := Extract(context.Background(), Work{Store: f.store, Run: fake.run, ModelID: "glm-5.3-flash", Prompt: "E"}, memory.ExtractionRequest{
		SessionID: id, TranscriptPath: writeTranscript(t, f), Cwd: f.workspace, Event: memory.EventSessionEnd, RequestedAt: "2026-01-01T00:00:00.000Z",
	})
	if err != nil || added != 2 || refused != 2 {
		t.Fatalf("added %d refused %d: %v", added, refused, err)
	}
	r := fake.requests[0]
	if r.Title != "Memory: extract from 0427515a" || r.Cwd != f.workspace || r.Prompt != "E" || !reflect.DeepEqual(r.Schema, ExtractionSchema) {
		t.Errorf("request %+v", r)
	}
	wantInput := `{"workspace":` + jsonText(f.workspace) + `,"existing":[{"category":"convention","text":"already known"}],"transcript":"User: Use pnpm here, not npm\n\nAssistant: Switching to pnpm."}`
	if r.Input != wantInput {
		t.Errorf("input %s\nwant  %s", r.Input, wantInput)
	}
	if got := f.texts(t, f.project(), "correction"); !reflect.DeepEqual(got, []string{"Use pnpm, not npm"}) {
		t.Errorf("corrections %q", got)
	}
	if got := f.texts(t, memory.GlobalSlot, ""); !reflect.DeepEqual(got, []string{"Likes short answers"}) {
		t.Errorf("global %q", got)
	}
	if _, err := os.Stat(filepath.Join(f.store.Dir(), "global.md")); err != nil {
		t.Error(err)
	}
	// A later request for the same Session is skipped by the hook.
	if wrote, _ := f.store.HasWrite(id); !wrote {
		t.Error("the write was not recorded")
	}
}

func TestExtractionTakesOnlyGlobalEntriesFromAScratchSession(t *testing.T) {
	f := newWorkFixture(t)
	f.add(t, "project rule", "convention")
	_, _ = f.store.Add(memory.GlobalSlot, "preference", "terse")
	fake := &fakeRun{answer: func(Request, []sentEntry) (any, error) {
		return changes{"entries": []any{
			changes{"scope": "project", "category": "convention", "text": "this chat folder uses pnpm"},
			changes{"scope": "global", "category": "insight", "text": "win.myhome runs WSL2 with systemd"},
		}}, nil
	}}
	added, refused, err := Extract(context.Background(), Work{Store: f.store, Run: fake.run, ModelID: "m", Prompt: "E"}, memory.ExtractionRequest{
		SessionID: "chat", TranscriptPath: writeTranscript(t, f), Cwd: f.workspace, Scratch: true, Event: memory.EventSessionEnd,
	})
	if err != nil || added != 1 || refused != 1 {
		t.Fatalf("added %d refused %d: %v", added, refused, err)
	}
	var input sentInput
	if err := json.Unmarshal([]byte(fake.requests[0].Input), &input); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(fake.requests[0].Input, `{"workspace":null,`) || input.Workspace != nil {
		t.Errorf("input %s", fake.requests[0].Input)
	}
	if want := []existingEntry{{Category: "preference", Text: "terse"}}; !reflect.DeepEqual(input.Existing, want) {
		t.Errorf("existing %+v", input.Existing)
	}
	if got := f.texts(t, f.project(), ""); !reflect.DeepEqual(got, []string{"project rule"}) {
		t.Errorf("project %q", got)
	}
	if got := f.texts(t, memory.GlobalSlot, ""); !reflect.DeepEqual(got, []string{"win.myhome runs WSL2 with systemd", "terse"}) {
		t.Errorf("global %q", got)
	}
}

func TestExtractionDoesNothingForATranscriptThatIsGone(t *testing.T) {
	f := newWorkFixture(t)
	fake := &fakeRun{answer: func(Request, []sentEntry) (any, error) { return changes{"entries": []any{}}, nil }}
	added, refused, err := Extract(context.Background(), Work{Store: f.store, Run: fake.run, ModelID: "m", Prompt: "E"}, memory.ExtractionRequest{
		SessionID: "s", TranscriptPath: filepath.Join(f.root, "missing.jsonl"), Cwd: f.workspace, Event: memory.EventPreCompact,
	})
	if err != nil || added != 0 || refused != 0 || len(fake.requests) != 0 {
		t.Errorf("added %d refused %d requests %d: %v", added, refused, len(fake.requests), err)
	}
}

func TestTranscriptTextKeepsTheEnd(t *testing.T) {
	text := TranscriptText([]memory.Turn{{Role: "user", Text: strings.Repeat("a", TranscriptChars)}, {Role: "assistant", Text: "end"}})
	if jsLength(text) != TranscriptChars || !strings.HasSuffix(text, "\n\nAssistant: end") {
		t.Errorf("length %d, tail %q", jsLength(text), text[len(text)-20:])
	}
}
