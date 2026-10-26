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
	"time"

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
	if o := outcomes[0]; o.Group != "failure" || o.Entries != 2 || o.OK || !strings.Contains(o.Reason, failure0.ID) {
		t.Errorf("failure outcome %+v", o)
	}
	if o := outcomes[1]; o != (SliceOutcome{Group: "convention", Entries: 3, OK: true}) {
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
		{Group: "failure", Entries: 2, Reason: "Daemon went away"},
		{Group: "convention", Entries: 2, OK: true},
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
	if r := fake.requests[0]; r.Title != "Memory: consolidate Global Memory / core" || r.Cwd != home || !strings.HasPrefix(r.Input, `{"memory":"global","group":"core",`) {
		t.Errorf("request %+v", r)
	}
	if got := f.texts(t, memory.GlobalSlot, ""); !reflect.DeepEqual(got, []string{"terse"}) {
		t.Errorf("global %q", got)
	}
}

// globalFixture saves Global Memory entries as Sessions would: one in a
// project, one in a chat without a project, one by a Memory Session.
func globalFixture(t *testing.T, f *workFixture) (sessions string, fromProject, fromChat, unknown memory.Entry) {
	t.Helper()
	sessions = filepath.Join(f.root, "sessions")
	folder := filepath.Join(sessions, "-somewhere")
	if err := os.MkdirAll(folder, 0o777); err != nil {
		t.Fatal(err)
	}
	chat := filepath.Join(f.root, "chat")
	for id, cwd := range map[string]string{"in-project": f.workspace, "in-chat": chat} {
		if err := os.WriteFile(filepath.Join(folder, id+".jsonl"), []byte(`{"type":"session_start","cwd":`+jsonText(cwd)+`}`+"\n"), 0o666); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(folder, "in-chat.settings.json"), []byte(`{"tags":[{"name":"droi.scratch"}]}`), 0o666); err != nil {
		t.Fatal(err)
	}
	global := func(session, text string, category memory.Category) memory.Entry {
		r, err := f.store.Add(memory.GlobalSlot, category, text)
		if err != nil || !r.OK {
			t.Fatalf("add %q: %v %s", text, err, r.Reason)
		}
		if session != "" {
			if err := f.store.LogCall(memory.Call{SessionID: session, Tool: "memory_add", Slot: &memory.GlobalSlot, OK: true, Written: r.Entry.ID}); err != nil {
				t.Fatal(err)
			}
		}
		return r.Entry
	}
	fromProject = global("in-project", filepath.Base(f.workspace)+" deploys with fly", "insight")
	fromChat = global("in-chat", "Project Zomboid needs Java 17", "insight")
	unknown = global("", "answer in Chinese", "preference")
	return sessions, fromProject, fromChat, unknown
}

func TestGlobalConsolidationSaysWhereEntriesWereWrittenAndMovesOneToItsProject(t *testing.T) {
	f := newWorkFixture(t)
	sessions, fromProject, fromChat, unknown := globalFixture(t, f)
	fake := &fakeRun{answer: func(r Request, sent []sentEntry) (any, error) {
		return changes{"keep": []string{unknown.ID}, "rewrite": []any{}, "remove": []string{fromChat.ID}, "merge": []any{},
			"move": []any{changes{"id": fromProject.ID, "workspace": f.workspace}}, "topic": []any{}}, nil
	}}
	outcomes, err := Consolidate(context.Background(), Work{Store: f.store, Run: fake.run, ModelID: "m", Prompt: "P", Sessions: sessions}, memory.GlobalSlot)
	if err != nil {
		t.Fatal(err)
	}
	// The core is sent whole, every category together.
	if got, want := fake.titles(), []string{"Memory: consolidate Global Memory / core"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("titles %q, want %q", got, want)
	}
	for _, o := range outcomes {
		if !o.OK {
			t.Errorf("outcome %+v", o)
		}
	}
	want := `{"memory":"global","group":"core","entries":[` +
		`{"id":"` + fromProject.ID + `","day":"` + fromProject.Day + `","category":"insight","text":` + jsonText(filepath.Base(f.workspace)+" deploys with fly") + `,"found":0,"written_in":` + jsonText(f.workspace) + `},` +
		`{"id":"` + fromChat.ID + `","day":"` + fromChat.Day + `","category":"insight","text":"Project Zomboid needs Java 17","found":0,"written_in":"chat"},` +
		`{"id":"` + unknown.ID + `","day":"` + unknown.Day + `","category":"preference","text":"answer in Chinese","found":0}],` +
		`"projects":[` + jsonText(f.workspace) + `]}`
	if got := fake.requests[0].Input; got != want {
		t.Errorf("input %s\nwant  %s", got, want)
	}
	if got := f.texts(t, memory.GlobalSlot, ""); !reflect.DeepEqual(got, []string{"answer in Chinese"}) {
		t.Errorf("global %q", got)
	}
	if moved, _, _ := f.store.Get(fromProject.ID); moved.Scope != memory.ScopeProject || moved.Workspace != f.workspace {
		t.Errorf("moved %+v", moved)
	}
	md, err := os.ReadFile(filepath.Join(f.store.Dir(), memory.MarkdownPath(f.project())))
	if err != nil || !strings.Contains(string(md), filepath.Base(f.workspace)+" deploys with fly") {
		t.Errorf("project markdown %v %s", err, md)
	}
}

func TestConsolidationRejectsAMoveToAWorkspaceItDidNotOffer(t *testing.T) {
	f := newWorkFixture(t)
	sessions, fromProject, fromChat, _ := globalFixture(t, f)
	fake := &fakeRun{answer: func(r Request, sent []sentEntry) (any, error) {
		return changes{"keep": []string{fromChat.ID, sent[2].ID}, "rewrite": []any{}, "remove": []any{}, "merge": []any{},
			"move": []any{changes{"id": fromProject.ID, "workspace": "/somewhere/else"}}}, nil
	}}
	outcomes, err := Consolidate(context.Background(), Work{Store: f.store, Run: fake.run, ModelID: "m", Prompt: "P", Sessions: sessions}, memory.GlobalSlot)
	if err != nil {
		t.Fatal(err)
	}
	if o := outcomes[0]; o.OK || !strings.Contains(o.Reason, "was not offered") {
		t.Errorf("outcome %+v", o)
	}
	if got := f.texts(t, memory.GlobalSlot, "insight"); len(got) != 2 {
		t.Errorf("global insights %q", got)
	}
}

func TestParseSliceChangesRefusesAnythingButTheSchema(t *testing.T) {
	for _, reply := range []string{`"prose"`, `null`, `{}`, `{"keep":[1],"rewrite":[],"remove":[],"merge":[]}`,
		`{"keep":[],"rewrite":[{"id":"a"}],"remove":[],"merge":[]}`, `{"keep":[],"rewrite":[],"remove":[],"merge":[{"ids":"a","text":"t"}]}`,
		`{"keep":[],"rewrite":[],"remove":[],"merge":[],"move":{}}`, `{"keep":[],"rewrite":[],"remove":[],"merge":[],"move":[{"id":"a"}]}`,
		`{"keep":[],"rewrite":[],"remove":[],"merge":[],"topic":[{"id":"a","topic":1}]}`, `{"keep":[],"rewrite":[],"remove":[],"merge":[{"ids":["a"],"text":"t","category":2}]}`} {
		if _, ok := ParseSliceChanges(json.RawMessage(reply)); ok {
			t.Errorf("accepted %s", reply)
		}
	}
	got, ok := ParseSliceChanges(json.RawMessage(`{"keep":["a"],"rewrite":[{"id":"b","text":"B","x":1}],"remove":["c"],"merge":[{"ids":["d","e"],"text":"DE","category":"correction"}],"move":[{"id":"f","workspace":"/w"}],"topic":[{"id":"g","topic":"Home network"}]}`))
	want := memory.SliceChanges{Keep: []string{"a"}, Rewrite: []memory.Rewrite{{ID: "b", Text: "B"}}, Remove: []string{"c"}, Merge: []memory.Merge{{IDs: []string{"d", "e"}, Text: "DE", Category: "correction"}}, Move: []memory.Move{{ID: "f", Workspace: "/w"}}, Topic: []memory.TopicChange{{ID: "g", Topic: "Home network"}}}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("parsed %+v %v", got, ok)
	}
	// A reply read from the final text may leave move, topic and a merge's category out.
	got, ok = ParseSliceChanges(json.RawMessage(`{"keep":["a"],"rewrite":[],"remove":[],"merge":[{"ids":["d"],"text":"D"}]}`))
	want = memory.SliceChanges{Keep: []string{"a"}, Remove: []string{}, Rewrite: []memory.Rewrite{}, Merge: []memory.Merge{{IDs: []string{"d"}, Text: "D"}}, Move: []memory.Move{}, Topic: []memory.TopicChange{}}
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

func (f *workFixture) global(t *testing.T, topic, text string, category memory.Category) memory.Entry {
	t.Helper()
	r, err := f.store.AddWithTopic(memory.GlobalSlot, category, text, topic)
	if err != nil || !r.OK {
		t.Fatalf("add %q: %v %s", text, err, r.Reason)
	}
	return r.Entry
}

func TestGlobalConsolidationFilesTheCoreThenConsolidatesEachTopic(t *testing.T) {
	f := newWorkFixture(t)
	pref := f.global(t, "", "answer in Chinese", "preference")
	quirk := f.global(t, "", "aliyun CLI is configured as a RAM user", "tool-quirk")
	known := f.global(t, "Anlan cloud", "aliyun CLI lives in ~/.factory/bin", "tool-quirk")
	lone := f.global(t, "Home network", "win.myhome runs WSL2", "insight")
	fake := &fakeRun{answer: func(r Request, sent []sentEntry) (any, error) {
		if strings.HasSuffix(r.Title, "/ core") {
			return changes{"keep": []string{pref.ID}, "rewrite": []any{}, "remove": []any{}, "merge": []any{}, "move": []any{},
				"topic": []any{changes{"id": quirk.ID, "topic": "Anlan cloud"}}}, nil
		}
		return changes{"keep": []any{}, "rewrite": []any{}, "remove": []any{}, "move": []any{}, "topic": []any{},
			"merge": []any{changes{"ids": []string{quirk.ID, known.ID}, "text": "aliyun CLI (~/.factory/bin) is configured as a RAM user", "category": ""}}}, nil
	}}
	outcomes, err := Consolidate(context.Background(), Work{Store: f.store, Run: fake.run, ModelID: "m", Prompt: "P"}, memory.GlobalSlot)
	if err != nil {
		t.Fatal(err)
	}
	// The topic gets what the core filed; a lone topic entry is not sent.
	if got, want := fake.titles(), []string{"Memory: consolidate Global Memory / core", "Memory: consolidate Global Memory / topic: Anlan cloud"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("titles %q, want %q", got, want)
	}
	if want := (SliceOutcome{Group: "Anlan cloud", Entries: 2, OK: true}); len(outcomes) != 2 || !outcomes[0].OK || outcomes[1] != want {
		t.Errorf("outcomes %+v", outcomes)
	}
	if in := fake.requests[0].Input; !strings.HasSuffix(in, `"topics":["Anlan cloud","Home network"]}`) {
		t.Errorf("core input %s", in)
	}
	if in := fake.requests[1].Input; !strings.HasPrefix(in, `{"memory":"global","group":"topic","topic":"Anlan cloud","entries":[`) {
		t.Errorf("topic input %s", in)
	}
	got, _ := f.store.ListTopic(memory.GlobalSlot, "", "Anlan cloud")
	if len(got) != 1 || got[0].Text != "aliyun CLI (~/.factory/bin) is configured as a RAM user" || got[0].Category != "tool-quirk" {
		t.Errorf("topic %+v", got)
	}
	if e, _, _ := f.store.Get(lone.ID); e.Topic != "Home network" {
		t.Errorf("lone %+v", e)
	}
	layers, _ := f.store.GlobalLayers()
	if layers.SortedAt == "" || layers.Filed != 0 || layers.CoreEntries != 1 {
		t.Errorf("layers %+v", layers)
	}
	if _, err := os.Stat(filepath.Join(f.store.Dir(), "memory.backup-"+time.Now().UTC().Format("2006-01-02")+".sqlite")); err != nil {
		t.Error(err)
	}
	md, _ := os.ReadFile(filepath.Join(f.store.Dir(), "global.md"))
	if !strings.Contains(string(md), "## Topic: Anlan cloud") {
		t.Errorf("markdown %s", md)
	}
}

func TestSortTopicsFilesTheCoreOnceAndChangesNoText(t *testing.T) {
	f := newWorkFixture(t)
	pref := f.global(t, "", "answer in Chinese", "preference")
	quirk := f.global(t, "", "aliyun CLI is configured as a RAM user", "tool-quirk")
	rule := f.global(t, "", "never run kubectl on production", "correction")
	fake := &fakeRun{answer: func(r Request, sent []sentEntry) (any, error) {
		return changes{"topics": []any{changes{"id": pref.ID, "topic": ""}, changes{"id": quirk.ID, "topic": "Anlan cloud"}, changes{"id": rule.ID, "topic": "Anlan cloud"}}}, nil
	}}
	w := Work{Store: f.store, Run: fake.run, ModelID: "m", Prompt: "S"}
	filed, err := SortTopics(context.Background(), w)
	if err != nil || filed != 2 {
		t.Fatalf("filed %d: %v", filed, err)
	}
	r := fake.requests[0]
	if r.Title != "Memory: sort Global Memory into topics" || r.Prompt != "S" || !reflect.DeepEqual(r.Schema, SortingSchema) || !strings.HasPrefix(r.Input, `{"memory":"global","entries":[{"id":"`+rule.ID+`","day":"`) {
		t.Errorf("request %+v", r)
	}
	got, _ := f.store.ListTopic(memory.GlobalSlot, "", "Anlan cloud")
	if len(got) != 2 || got[0].Text != rule.Text || got[1].Text != quirk.Text {
		t.Errorf("topic %+v", got)
	}
	layers, _ := f.store.GlobalLayers()
	if layers.SortedAt == "" || layers.Filed != 2 || layers.CoreEntries != 1 {
		t.Errorf("layers %+v", layers)
	}
	// Once sorted, never again.
	if filed, err := SortTopics(context.Background(), w); err != nil || filed != 0 || len(fake.requests) != 1 {
		t.Errorf("again: %d %v, %d requests", filed, err, len(fake.requests))
	}
}

func TestSortTopicsMarksNothingWhenTheAnswerIsRejected(t *testing.T) {
	f := newWorkFixture(t)
	f.global(t, "", "a fact", "insight")
	f.global(t, "", "another fact", "insight")
	fake := &fakeRun{answer: func(Request, []sentEntry) (any, error) {
		return changes{"topics": []any{changes{"id": "mistyped", "topic": "Somewhere"}}}, nil
	}}
	if _, err := SortTopics(context.Background(), Work{Store: f.store, Run: fake.run}); !errors.Is(err, ErrSortFailed) {
		t.Fatalf("error %v", err)
	}
	if layers, _ := f.store.GlobalLayers(); layers.SortedAt != "" || len(layers.Topics) != 0 {
		t.Errorf("layers %+v", layers)
	}
}

func TestSortTopicsMarksAnEmptyCoreSortedWithoutAsking(t *testing.T) {
	f := newWorkFixture(t)
	fake := &fakeRun{answer: func(Request, []sentEntry) (any, error) { return nil, errors.New("asked") }}
	if filed, err := SortTopics(context.Background(), Work{Store: f.store, Run: fake.run}); err != nil || filed != 0 || len(fake.requests) != 0 {
		t.Fatalf("%d %v", filed, err)
	}
	if layers, _ := f.store.GlobalLayers(); layers.SortedAt == "" || layers.Filed != 0 {
		t.Errorf("layers %+v", layers)
	}
}

func TestExtractionFromAChatFilesWhatItIsAboutUnderATopic(t *testing.T) {
	f := newWorkFixture(t)
	f.global(t, "Home network", "win.myhome runs WSL2", "insight")
	fake := &fakeRun{answer: func(Request, []sentEntry) (any, error) {
		return changes{"entries": []any{
			changes{"scope": "global", "category": "preference", "text": "Likes short answers", "topic": ""},
			changes{"scope": "global", "category": "insight", "text": "win.myhome has 15 GB RAM", "topic": "Home network"},
		}}, nil
	}}
	added, refused, err := Extract(context.Background(), Work{Store: f.store, Run: fake.run, ModelID: "m", Prompt: "E"}, memory.ExtractionRequest{
		SessionID: "chat", TranscriptPath: writeTranscript(t, f), Cwd: f.workspace, Scratch: true, Event: memory.EventSessionEnd,
	})
	if err != nil || added != 2 || refused != 0 {
		t.Fatalf("added %d refused %d: %v", added, refused, err)
	}
	if in := fake.requests[0].Input; !strings.Contains(in, `"existing":[{"category":"insight","text":"win.myhome runs WSL2","topic":"Home network"}],"topics":["Home network"],`) {
		t.Errorf("input %s", in)
	}
	got, _ := f.store.ListTopic(memory.GlobalSlot, "", "Home network")
	if len(got) != 2 {
		t.Errorf("topic %+v", got)
	}
}

func TestSortTopicsKeepsWhatTheAnswerLeftOutInTheCore(t *testing.T) {
	f := newWorkFixture(t)
	a := f.global(t, "", "aliyun CLI is configured", "tool-quirk")
	b := f.global(t, "", "answer in Chinese", "preference")
	fake := &fakeRun{answer: func(Request, []sentEntry) (any, error) {
		return changes{"topics": []any{changes{"id": a.ID, "topic": "Anlan cloud"}, changes{"id": "mistyped", "topic": "x"}, changes{"id": a.ID, "topic": "later"}}}, nil
	}}
	filed, err := SortTopics(context.Background(), Work{Store: f.store, Run: fake.run})
	if err != nil || filed != 1 {
		t.Fatalf("filed %d: %v", filed, err)
	}
	if e, _, _ := f.store.Get(a.ID); e.Topic != "Anlan cloud" {
		t.Errorf("a %+v", e)
	}
	if e, _, _ := f.store.Get(b.ID); e.Topic != "" {
		t.Errorf("b %+v", e)
	}
}

func TestGlobalConsolidationNeverMovesTheUsersCorrectionsOrPreferences(t *testing.T) {
	f := newWorkFixture(t)
	sessions, fromProject, _, _ := globalFixture(t, f)
	rule := f.global(t, "", "never run kubectl on production", "correction")
	if err := f.store.LogCall(memory.Call{SessionID: "in-project", Tool: "memory_add", Slot: &memory.GlobalSlot, OK: true, Written: rule.ID}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeRun{answer: func(r Request, sent []sentEntry) (any, error) {
		keep := []string{}
		for _, e := range sent {
			if e.ID != rule.ID && e.ID != fromProject.ID {
				keep = append(keep, e.ID)
			}
		}
		return changes{"keep": keep, "rewrite": []any{}, "remove": []any{}, "merge": []any{}, "topic": []any{},
			"move": []any{changes{"id": rule.ID, "workspace": f.workspace}, changes{"id": fromProject.ID, "workspace": f.workspace}}}, nil
	}}
	outcomes, err := Consolidate(context.Background(), Work{Store: f.store, Run: fake.run, Sessions: sessions}, memory.GlobalSlot)
	if err != nil || len(outcomes) != 1 || !outcomes[0].OK {
		t.Fatalf("outcomes %+v %v", outcomes, err)
	}
	if e, _, _ := f.store.Get(rule.ID); e.Scope != memory.ScopeGlobal {
		t.Errorf("the correction moved: %+v", e)
	}
	if e, _, _ := f.store.Get(fromProject.ID); e.Scope != memory.ScopeProject {
		t.Errorf("the insight did not move: %+v", e)
	}
}

func TestAnEntryMovesOnlyToAProjectItNames(t *testing.T) {
	for _, c := range []struct {
		text, folder string
		want         bool
	}{
		{"anlan CLI: repo grep needs --ref", "/Users/dev/anlan-cli", true},
		{"reads metrics through ~/dev/kube-mygo", "/Users/dev/kube-mygo", true},
		{"Droid's Execute kills background processes", "/Users/dev/droi", false},
		{"git merge does not accept -F -", "/Users/dev/kube-mygo", false},
		{"安蓝 的 K8s 节点没有 conntrack", "/Users/dev/安蓝", true},
	} {
		if got := names(c.text, filepath.Base(c.folder)); got != c.want {
			t.Errorf("names(%q, %q) = %v", c.text, c.folder, got)
		}
	}
}
