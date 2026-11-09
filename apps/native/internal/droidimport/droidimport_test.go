package droidimport

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// transcript writes a droid transcript of lines into dir/<folder>/<id>.jsonl and answers its path.
func transcript(t *testing.T, dir, id string, lines ...any) string {
	t.Helper()
	folder := filepath.Join(dir, "-Users-dev-acme")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, l := range lines {
		if s, ok := l.(string); ok {
			b.WriteString(s)
		} else {
			raw, _ := json.Marshal(l)
			b.Write(raw)
		}
		b.WriteByte('\n')
	}
	path := filepath.Join(folder, id+".jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

type m = map[string]any

func start(id string, extra ...m) m {
	l := m{"type": "session_start", "id": id, "title": "A session", "cwd": "/Users/dev/acme"}
	for _, e := range extra {
		for k, v := range e {
			l[k] = v
		}
	}
	return l
}

func msg(at string, message m) m {
	return m{"type": "message", "id": at, "timestamp": at, "message": message}
}

func user(content any, extra ...m) m {
	x := m{"role": "user", "content": content}
	for _, e := range extra {
		for k, v := range e {
			x[k] = v
		}
	}
	return x
}

func answer(blocks ...m) m {
	content := []any{}
	for _, b := range blocks {
		content = append(content, b)
	}
	return m{"role": "assistant", "content": content, "apiProvider": "anthropic", "modelId": "claude-x"}
}

func txt(s string) m { return m{"type": "text", "text": s} }
func use(id, name string) m {
	return m{"type": "tool_use", "id": id, "name": name, "input": m{"path": "a.go"}}
}
func result(id string, content any, isErr bool) m {
	return m{"type": "tool_result", "tool_use_id": id, "content": content, "is_error": isErr}
}
func picture(data string) m {
	return m{"type": "image", "source": m{"type": "base64", "media_type": "image/jpeg", "data": data}}
}

// entry is one converted entry's kind and first message, decoded.
type entry struct {
	kind string
	msg  m
	head string
	data m
}

func decode(t *testing.T, conv Conversation) []entry {
	t.Helper()
	var out []entry
	for _, e := range conv.Entries {
		var msgs []m
		if len(e.Model) > 0 {
			if err := json.Unmarshal(e.Model, &msgs); err != nil || len(msgs) != 1 {
				t.Fatalf("%s: model %s", e.Kind, e.Model)
			}
		}
		x := entry{kind: e.Kind, head: string(e.Head)}
		if len(msgs) == 1 {
			x.msg = msgs[0]
		}
		if len(e.Data) > 0 {
			_ = json.Unmarshal(e.Data, &x.data)
		}
		out = append(out, x)
	}
	return out
}

func kinds(es []entry) string {
	var k []string
	for _, e := range es {
		k = append(k, e.kind)
	}
	return strings.Join(k, " ")
}

func at(sec int) string {
	return time.Date(2026, 1, 2, 10, 0, sec, 0, time.UTC).Format(time.RFC3339Nano)
}

func ms(sec int) float64 {
	return float64(time.Date(2026, 1, 2, 10, 0, sec, 0, time.UTC).UnixMilli())
}

// A turn comes over as Pi writes one: the prompt, the answer whose tool_use is a toolCall, the result named after
// its call, the next answer; a note first says where it came from.
func TestATurnComesOverAsPiWritesOne(t *testing.T) {
	path := transcript(t, t.TempDir(), "s1",
		start("s1"),
		msg(at(1), user("fix the test")),
		msg(at(2), answer(m{"type": "thinking", "thinking": "secret", "signature": "sig"}, txt("Reading."), use("c1", "Read"))),
		msg(at(3), user([]any{result("c1", "package a", false)})),
		msg(at(4), answer(txt("Done."))),
	)
	conv, err := Convert(path)
	if err != nil {
		t.Fatal(err)
	}
	es := decode(t, conv)
	if got := kinds(es); got != "pi.user pi.assistant pi.tool-result pi.assistant" {
		t.Fatalf("kinds %s", got)
	}
	if es[0].msg["content"] != "fix the test" || es[0].msg["timestamp"] != ms(1) {
		t.Errorf("user %v", es[0].msg)
	}
	a := es[1].msg
	blocks := a["content"].([]any)
	if len(blocks) != 2 || blocks[0].(m)["text"] != "Reading." || blocks[1].(m)["type"] != "toolCall" || blocks[1].(m)["id"] != "c1" ||
		blocks[1].(m)["arguments"].(m)["path"] != "a.go" {
		t.Errorf("answer blocks %v", blocks)
	}
	if a["stopReason"] != "toolUse" || a["provider"] != "anthropic" || a["model"] != "claude-x" {
		t.Errorf("answer %v", a)
	}
	r := es[2].msg
	if r["role"] != "toolResult" || r["toolCallId"] != "c1" || r["toolName"] != "Read" || r["isError"] != false ||
		r["content"].([]any)[0].(m)["text"] != "package a" {
		t.Errorf("result %v", r)
	}
	if es[3].msg["stopReason"] != "stop" {
		t.Errorf("last answer %v", es[3].msg)
	}
	raw, _ := json.Marshal(conv.Entries)
	if strings.Contains(string(raw), "secret") {
		t.Error("thinking came over")
	}
	if conv.Title != "A session" || conv.Cwd != "/Users/dev/acme" || conv.CreatedAt != int64(ms(1)) || conv.LastAt != int64(ms(4)) || conv.Messages != 4 {
		t.Errorf("conversation %+v", conv)
	}
	if len(conv.Hash) != 64 {
		t.Errorf("hash %q", conv.Hash)
	}
}

// What droid showed only the user, or only the model, stays out; so do the reminders droid put into a prompt.
func TestDroidsOwnMessagesStayOut(t *testing.T) {
	path := transcript(t, t.TempDir(), "s1",
		start("s1"),
		msg(at(1), user([]any{txt("<system-reminder>\nbe careful\n</system-reminder>"), txt("the real prompt")})),
		msg(at(2), user([]any{txt("<system-notification>a task ended</system-notification>")})),
		msg(at(3), user("a reminder for the model", m{"visibility": "llm_only"})),
		msg(at(4), user([]any{}, m{"visibility": "user_only", "hookEventName": "Stop"})),
		msg(at(5), m{"role": "assistant", "content": []any{use("inner", "Read")}, "visibility": "user_only"}),
		msg(at(6), user([]any{result("inner", "x", false)}, m{"visibility": "user_only"})),
		msg(at(7), answer(m{"type": "thinking", "thinking": "only thinking"})),
		`{not json`,
		msg(at(8), user("both sides", m{"visibility": "both"})),
	)
	conv, err := Convert(path)
	if err != nil {
		t.Fatal(err)
	}
	es := decode(t, conv)
	if got := kinds(es); got != "pi.user pi.user" {
		t.Fatalf("kinds %s", got)
	}
	if es[0].msg["content"] != "the real prompt" || es[1].msg["content"] != "both sides" {
		t.Errorf("prompts %v / %v", es[0].msg, es[1].msg)
	}
	if conv.Left != 7 {
		t.Errorf("left %d, want 7", conv.Left)
	}
}

// A call nothing answered gets a result that says so before the conversation goes on, since a model's API refuses
// it; a result whose call is not the last answer's stays out; a result droid showed only the user still answers.
func TestEveryCallHasOneResult(t *testing.T) {
	path := transcript(t, t.TempDir(), "s1",
		start("s1"),
		msg(at(1), user("go")),
		msg(at(2), answer(use("c1", "Read"), use("c2", "Grep"))),
		msg(at(3), user([]any{result("c2", "found", false)}, m{"visibility": "user_only"})),
		msg(at(4), user("stop that")),
		msg(at(5), user([]any{result("c1", "late", false), result("nobody", "x", false)})),
		msg(at(6), answer(use("c3", "Execute"))),
		msg(at(7), m{"type": "message", "timestamp": at(7), "message": user("a notice", m{"visibility": "llm_only"})}),
	)
	conv, err := Convert(path)
	if err != nil {
		t.Fatal(err)
	}
	es := decode(t, conv)
	if got := kinds(es); got != "pi.user pi.assistant pi.tool-result pi.tool-result pi.user pi.assistant pi.tool-result" {
		t.Fatalf("kinds %s", got)
	}
	if es[2].msg["toolCallId"] != "c2" || es[2].msg["isError"] != false {
		t.Errorf("c2 %v", es[2].msg)
	}
	if es[3].msg["toolCallId"] != "c1" || es[3].msg["isError"] != true || es[3].msg["timestamp"] != ms(4) ||
		es[3].msg["content"].([]any)[0].(m)["text"] != noResult {
		t.Errorf("c1 %v", es[3].msg)
	}
	if es[6].msg["toolCallId"] != "c3" || es[6].msg["isError"] != true {
		t.Errorf("c3 %v", es[6].msg)
	}
	// The made-up result at the end is the last activity, or the history would come in unread.
	if conv.LastAt != int64(ms(7)) {
		t.Errorf("last %d, want %d", conv.LastAt, int64(ms(7)))
	}
}

// A compaction starts the context again with its summary, wrapped as Pi's own; pictures come over as Pi's.
func TestACompactionAndPicturesComeOver(t *testing.T) {
	path := transcript(t, t.TempDir(), "s1",
		start("s1"),
		msg(at(1), user([]any{txt("look"), picture("AAAA")})),
		msg(at(2), answer(use("c1", "Screenshot"))),
		m{"type": "compaction_state", "id": "k1", "timestamp": at(3), "summaryText": "we looked", "anchorMessage": m{"id": at(2), "index": 1}, "removedCount": 2},
		msg(at(4), user([]any{result("c1", []any{txt("shot"), picture("BBBB")}, false)})),
		m{"type": "compaction_state", "id": "k2", "timestamp": at(5), "summaryText": "  "},
		m{"type": "todo_state", "timestamp": at(6), "todos": "x"},
	)
	conv, err := Convert(path)
	if err != nil {
		t.Fatal(err)
	}
	es := decode(t, conv)
	if got := kinds(es); got != "pi.user pi.assistant pi.tool-result pi.compaction" {
		t.Fatalf("kinds %s", got)
	}
	if img := es[0].msg["content"].([]any)[1].(m); img["type"] != "image" || img["data"] != "AAAA" || img["mimeType"] != "image/jpeg" {
		t.Errorf("picture %v", img)
	}
	if es[2].msg["isError"] != true {
		t.Errorf("the call before the compaction was not answered first: %v", es[2].msg)
	}
	c := es[3]
	if c.head != `"self"` || c.msg["role"] != "user" || c.msg["content"] != compactionPrefix+"we looked"+compactionSuffix || c.msg["timestamp"] != ms(3) {
		t.Errorf("compaction %+v", c)
	}
}

// The settings file beside the transcript says whether droid archived the session and what it ran with.
func TestSettingsComeOver(t *testing.T) {
	path := transcript(t, t.TempDir(), "s1", start("s1"), msg(at(1), user("hi")))
	settings := strings.TrimSuffix(path, ".jsonl") + ".settings.json"
	if err := os.WriteFile(settings, []byte(`{"model":"claude-x","reasoningEffort":"high","archivedAt":1700000000000}`), 0o644); err != nil {
		t.Fatal(err)
	}
	conv, err := Convert(path)
	if err != nil {
		t.Fatal(err)
	}
	if !conv.Archived || conv.Model != "claude-x" || conv.Effort != "high" {
		t.Errorf("settings %+v", conv)
	}
}

// The list holds sessions with something said, newest first; a subagent's, a Memory Session, one with nothing said
// and a file that is no transcript are left out. A long session_start line is read whole.
func TestListOffersSessionsWithAConversation(t *testing.T) {
	dir := t.TempDir()
	older := transcript(t, dir, "older", start("older", m{"title": strings.Repeat("long ", 2000)}), msg(at(1), user("hi")))
	newer := transcript(t, dir, "newer", start("newer"), msg(at(1), user("hello")))
	transcript(t, dir, "sub", start("sub", m{"callingSessionId": "older"}), msg(at(1), user("task")))
	transcript(t, dir, "empty", start("empty"), msg(at(1), user([]any{}, m{"visibility": "user_only"})))
	// Droi's Memory Sessions, droid exec runs, a mission's sessions and an Automation's runs are no one's
	// conversation.
	for id, tag := range map[string]string{"memory": "droi.memory", "exec": "exec", "worker": "mission-worker", "mission": "mission-session", "auto": "automation", "droi-auto": "droi.automation"} {
		path := transcript(t, dir, id, start(id), msg(at(1), user("work")))
		if err := os.WriteFile(strings.TrimSuffix(path, ".jsonl")+".settings.json", []byte(`{"tags":[{"name":"`+tag+`"}]}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	transcript(t, dir, "junk", `{"type":"message"}`)
	now := time.Now()
	_ = os.Chtimes(older, now.Add(-time.Hour), now.Add(-time.Hour))
	_ = os.Chtimes(newer, now, now)
	list, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range list {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "newer,older" {
		t.Fatalf("listed %v", ids)
	}
	if list[0].Cwd != "/Users/dev/acme" || list[0].Path != newer || list[0].Size == 0 {
		t.Errorf("source %+v", list[0])
	}
	if none, err := List(filepath.Join(dir, "missing")); err != nil || len(none) != 0 {
		t.Errorf("a missing folder lists %v, %v", none, err)
	}
}

// A compaction keeps what came after its anchor, as Pi's keeps everything from its head; a result whose call went
// into the summary is not kept, and nothing kept makes the summary its own head.
func TestACompactionKeepsWhatCameAfterItsAnchor(t *testing.T) {
	path := transcript(t, t.TempDir(), "s1",
		start("s1"),
		m{"type": "message", "id": "a", "timestamp": at(1), "message": user("first")},
		m{"type": "message", "id": "b", "timestamp": at(2), "message": answer(use("c1", "Read"))},
		m{"type": "message", "id": "r", "timestamp": at(3), "message": user([]any{result("c1", "x", false)})},
		m{"type": "message", "id": "c", "timestamp": at(4), "message": answer(txt("read it"))},
		m{"type": "compaction_state", "timestamp": at(5), "summaryText": "kept from b", "anchorMessage": m{"id": "a"}},
		m{"type": "compaction_state", "timestamp": at(6), "summaryText": "kept from c", "anchorMessage": m{"id": "b"}},
		m{"type": "compaction_state", "timestamp": at(7), "summaryText": "nothing kept", "anchorMessage": m{"id": "c"}},
	)
	conv, err := Convert(path)
	if err != nil {
		t.Fatal(err)
	}
	es := decode(t, conv)
	if got := kinds(es); got != "pi.user pi.assistant pi.tool-result pi.assistant pi.compaction pi.compaction pi.compaction" {
		t.Fatalf("kinds %s", got)
	}
	// After a: b, its result, c; after b: its result, which answers a call the summary holds, so c.
	if conv.Heads[4] != 1 || conv.Heads[5] != 3 || es[4].head != "" || es[5].head != "" {
		t.Errorf("heads %v / %q %q", conv.Heads, es[4].head, es[5].head)
	}
	// After c: the two summaries, which are not droid's messages; nothing of droid's is kept.
	if _, ok := conv.Heads[6]; ok || es[6].head != `"self"` {
		t.Errorf("the last compaction: heads %v, head %q", conv.Heads, es[6].head)
	}
}

func TestBatchesKeepToBothBounds(t *testing.T) {
	small := kernel.ImportEntry{Kind: "pi.user", Model: make(json.RawMessage, 100)}
	big := kernel.ImportEntry{Kind: "pi.user", Model: make(json.RawMessage, 5000)}
	spans := func(es []kernel.ImportEntry, heads map[int]int, per, max int) string {
		return fmt.Sprint(batches(es, heads, per, max))
	}
	if got := spans(nil, nil, 3, 1000); got != "[[0 0]]" {
		t.Errorf("no entries: %v", got)
	}
	seven := []kernel.ImportEntry{small, small, small, small, small, small, small}
	if got := spans(seven, nil, 3, 1<<20); got != "[[0 3] [3 6] [6 7]]" {
		t.Errorf("by count: %v", got)
	}
	if got := spans([]kernel.ImportEntry{small, big, small, small}, nil, 100, 1000); got != "[[0 1] [1 2] [2 4]]" {
		t.Errorf("by size: %v", got)
	}
	// A head in the call the compaction would join moves it to the next; one in an earlier call does not.
	if got := spans(seven, map[int]int{2: 1, 5: 0}, 100, 1<<20); got != "[[0 2] [2 7]]" {
		t.Errorf("by head: %v", got)
	}
}

// A Droi 1.x scratch session comes over as a scratch conversation.
func TestAScratchSessionComesOverAsOne(t *testing.T) {
	dir := t.TempDir()
	path := transcript(t, dir, "s1", start("s1"), msg(at(1), user("try this")))
	if err := os.WriteFile(strings.TrimSuffix(path, ".jsonl")+".settings.json", []byte(`{"tags":[{"name":"droi.scratch"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &recorder{}
	if _, _, err := Import(context.Background(), r, OpenStore(filepath.Join(dir, "i.json")), Source{ID: "s1", Path: path}, 1000, nil); err != nil {
		t.Fatal(err)
	}
	if r.imports[0].Kind != "scratch" {
		t.Fatalf("kind %q", r.imports[0].Kind)
	}
}

// recorder is a Kernel that keeps the calls it got and the entries they wrote, numbers conversations from 42 and
// entries from 100, and answers a key it saw before with what it wrote then, as the Kernel does.
type recorder struct {
	imports   []kernel.ImportParams
	configure []kernel.ConfigureParams
	index     []kernel.IndexUpdateParams
	fail      int
	next      kernel.ID
	convs     kernel.ID
	held      map[kernel.ID][]kernel.Entry
	keys      map[string]kernel.ImportResult
}

func (r *recorder) Import(_ context.Context, p kernel.ImportParams) (kernel.ImportResult, error) {
	r.imports = append(r.imports, p)
	if r.fail > 0 && len(r.imports) == r.fail {
		return kernel.ImportResult{}, context.DeadlineExceeded
	}
	if res, ok := r.keys[p.Key]; ok && p.Key != "" {
		return res, nil
	}
	if r.held == nil {
		r.held, r.keys = map[kernel.ID][]kernel.Entry{}, map[string]kernel.ImportResult{}
	}
	id := p.ID
	if id == 0 {
		id = 42 + r.convs
		r.convs++
	}
	res := kernel.ImportResult{ConversationID: id}
	for _, e := range p.Entries {
		r.next++
		res.EntryIDs = append(res.EntryIDs, 99+r.next)
		r.held[id] = append(r.held[id], kernel.Entry{ID: 99 + r.next, ConversationID: id, Kind: e.Kind, Model: e.Model, Data: e.Data})
	}
	if p.Key != "" {
		r.keys[p.Key] = res
	}
	return res, nil
}
func (r *recorder) UpdateIndex(_ context.Context, p kernel.IndexUpdateParams) error {
	r.index = append(r.index, p)
	return nil
}
func (r *recorder) Configure(_ context.Context, p kernel.ConfigureParams) (kernel.ConfigureResult, error) {
	r.configure = append(r.configure, p)
	return kernel.ConfigureResult{}, nil
}
func (r *recorder) Entries(_ context.Context, p kernel.EntriesParams) (kernel.EntriesResult, error) {
	return kernel.EntriesResult{Items: r.held[p.ID]}, nil
}

// kinds are the kinds of the entries conversation id holds, in order.
func (r *recorder) kinds(id kernel.ID) []string {
	var out []string
	for _, e := range r.held[id] {
		out = append(out, e.Kind)
	}
	return out
}

// An import creates with the first batch and appends the rest, keyed by session, file and place, so a retry
// writes the same calls; the note, the agent, the archive and the seen mark are set apart from the entries.
func TestWriteKeysEveryBatch(t *testing.T) {
	dir := t.TempDir()
	lines := []any{start("s1")}
	for i := range 5 {
		lines = append(lines, msg(at(i+1), user("prompt")))
	}
	path := transcript(t, dir, "s1", lines...)
	settings := strings.TrimSuffix(path, ".jsonl") + ".settings.json"
	_ = os.WriteFile(settings, []byte(`{"archivedAt":1}`), 0o644)
	src := Source{ID: "s1", Path: path}
	agent := &kernel.AgentChange{ThinkingLevel: kernel.Some("high")}
	run := func(fail int) (*recorder, error) {
		r := &recorder{fail: fail}
		store := OpenStore(filepath.Join(dir, "imports.json"))
		_, _, err := Import(context.Background(), r, store, src, 2, func(Conversation) *kernel.AgentChange { return agent })
		return r, err
	}
	first, err := run(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.imports) != 4 {
		t.Fatalf("%d calls for 5 entries 2 a call and the note", len(first.imports))
	}
	p0, p1, note := first.imports[0], first.imports[1], first.imports[3]
	if p0.ID != 0 || p0.Title != "A session" || p0.Cwd != "/Users/dev/acme" || p0.Kind != "chat" || p0.Archived != nil ||
		p0.CreatedAt == nil || *p0.CreatedAt != int64(ms(1)) || p0.Agent != nil {
		t.Errorf("first call %+v", p0)
	}
	if p1.ID != 42 || p1.Title != "" || p1.Cwd != "" || p1.CreatedAt != nil {
		t.Errorf("second call %+v", p1)
	}
	if !strings.HasPrefix(p0.Key, "droid-import/v1/s1/") || !strings.HasSuffix(p0.Key, "/0-2") || !strings.HasSuffix(first.imports[2].Key, "/4-5") {
		t.Errorf("keys %q %q", p0.Key, first.imports[2].Key)
	}
	var data m
	if len(note.Entries) != 1 || note.Entries[0].Kind != kernel.EntryNote || json.Unmarshal(note.Entries[0].Data, &data) != nil ||
		data["sourceSessionId"] != "s1" || data["at"] != ms(5) || !strings.Contains(note.Key, "/note-") {
		t.Errorf("note %+v %v", note, data)
	}
	if len(first.configure) != 1 || first.configure[0].ID != 42 || first.configure[0].Change != agent {
		t.Errorf("configure %+v", first.configure)
	}
	if len(first.index) != 1 || string(first.index[0].ViewedAt) != kernel.ViewedAtTime(int64(ms(5))).String() ||
		first.index[0].Archived == nil || !*first.index[0].Archived {
		t.Errorf("index %+v", first.index)
	}
	rec, ok := OpenStore(filepath.Join(dir, "imports.json")).Get("s1")
	if !ok || rec.Conversation != 42 || len(rec.Hash) != 64 {
		t.Errorf("record %+v", rec)
	}
}

// An import that stops part way leaves a record of the conversation it made, and the next import goes on with it:
// the same conversation, each entry once, the note once.
func TestAnImportThatStoppedGoesOnInItsConversation(t *testing.T) {
	dir := t.TempDir()
	lines := []any{start("s1")}
	for i := range 5 {
		lines = append(lines, msg(at(i+1), user(fmt.Sprintf("prompt %d", i))))
	}
	src := Source{ID: "s1", Path: transcript(t, dir, "s1", lines...)}
	store := OpenStore(filepath.Join(dir, "imports.json"))
	r := &recorder{fail: 2}
	if _, _, err := Import(context.Background(), r, store, src, 2, nil); err == nil {
		t.Fatal("the failing run did not fail")
	}
	rec, _ := store.Get("s1")
	if rec.Conversation != 42 || !rec.Partial || rec.Current(src) {
		t.Fatalf("record after the failure %+v", rec)
	}
	r.fail = 0
	id, _, err := Import(context.Background(), r, store, src, 2, nil)
	if err != nil || id != 42 {
		t.Fatalf("retry: %d, %v", id, err)
	}
	if got := r.kinds(42); len(got) != 6 || got[5] != kernel.EntryNote {
		t.Fatalf("the conversation holds %v", got)
	}
	if r.convs != 1 {
		t.Fatalf("%d conversations made", r.convs)
	}
	if rec, _ := store.Get("s1"); rec.Partial || rec.Conversation != 42 || len(rec.Hash) != 64 {
		t.Fatalf("record after the retry %+v", rec)
	}
}

// A session droid went on with after its import goes on in the same conversation: only what came since is sent,
// and no second note.
func TestASessionThatWentOnInDroidGoesOnInItsConversation(t *testing.T) {
	dir := t.TempDir()
	lines := []any{start("s1"), msg(at(1), user("first")), msg(at(2), answer(txt("answer one")))}
	path := transcript(t, dir, "s1", lines...)
	store := OpenStore(filepath.Join(dir, "imports.json"))
	r := &recorder{}
	if _, _, err := Import(context.Background(), r, store, Source{ID: "s1", Path: path}, 1000, nil); err != nil {
		t.Fatal(err)
	}
	transcript(t, dir, "s1", append(lines, msg(at(3), user("second")), msg(at(4), answer(txt("answer two"))))...)
	id, _, err := Import(context.Background(), r, store, Source{ID: "s1", Path: path}, 1000, nil)
	if err != nil || id != 42 || r.convs != 1 {
		t.Fatalf("second import: %d, %v, %d conversations", id, err, r.convs)
	}
	eq := func(got, want []string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	eq(r.kinds(42), []string{kernel.EntryUser, kernel.EntryAssistant, kernel.EntryNote, kernel.EntryUser, kernel.EntryAssistant})
	last := r.imports[len(r.imports)-1]
	if last.ID != 42 || len(last.Entries) != 2 || !strings.HasSuffix(last.Key, "/2-4") {
		t.Fatalf("the continuation sent %+v", last)
	}
}

// A conversation that went on in Droi is not droid's to add to: droid's later session comes over as a new one.
func TestAConversationThatWentOnInDroiIsLeftAsItIs(t *testing.T) {
	dir := t.TempDir()
	lines := []any{start("s1"), msg(at(1), user("first")), msg(at(2), answer(txt("answer one")))}
	path := transcript(t, dir, "s1", lines...)
	store := OpenStore(filepath.Join(dir, "imports.json"))
	r := &recorder{}
	if _, _, err := Import(context.Background(), r, store, Source{ID: "s1", Path: path}, 1000, nil); err != nil {
		t.Fatal(err)
	}
	r.held[42] = append(r.held[42], kernel.Entry{ID: 900, Kind: kernel.EntryUser, Model: json.RawMessage(`[{"role":"user","content":"asked in Droi","timestamp":9}]`)})
	transcript(t, dir, "s1", append(lines, msg(at(3), user("second")))...)
	id, _, err := Import(context.Background(), r, store, Source{ID: "s1", Path: path}, 1000, nil)
	if err != nil || id != 43 {
		t.Fatalf("second import: %d, %v", id, err)
	}
	if len(r.held[42]) != 4 {
		t.Fatalf("the Droi conversation changed: %v", r.kinds(42))
	}
}

// A compaction's head is the ID the Kernel gave the entry it keeps from, which an earlier call wrote.
func TestWriteGivesAHeadItsEntryID(t *testing.T) {
	entry := kernel.ImportEntry{Kind: kernel.EntryUser, Model: json.RawMessage(`[{"role":"user","content":"x","timestamp":1}]`)}
	conv := Conversation{ID: "s1", Hash: strings.Repeat("a", 64), Entries: []kernel.ImportEntry{entry, entry, entry, entry}, Heads: map[int]int{3: 1}}
	r := &recorder{}
	if _, err := Write(context.Background(), r, conv, 100, nil); err != nil {
		t.Fatal(err)
	}
	if len(r.imports) != 3 || len(r.imports[0].Entries) != 3 || len(r.imports[1].Entries) != 1 {
		t.Fatalf("calls %+v", r.imports)
	}
	if got := string(r.imports[1].Entries[0].Head); got != "101" {
		t.Errorf("head %s, want the second entry's ID 101", got)
	}
	if conv.Entries[3].Head != nil {
		t.Error("Write changed the conversation's own entries")
	}
}
func TestARecordKnowsItsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "imports.json")
	s := OpenStore(path)
	mod := time.UnixMilli(1700000000000)
	if err := s.Put("s1", Record{Conversation: 7, Size: 10, ModTime: mod.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	r, ok := OpenStore(path).Get("s1")
	if !ok || r.Conversation != 7 {
		t.Fatalf("read back %+v", r)
	}
	if !r.Current(Source{Size: 10, ModTime: mod}) || r.Current(Source{Size: 11, ModTime: mod}) || r.Current(Source{Size: 10, ModTime: mod.Add(time.Second)}) {
		t.Error("Current does not follow the file")
	}
}

func TestMarkdownKeepsTheConversation(t *testing.T) {
	path := transcript(t, t.TempDir(), "s1",
		start("s1"),
		msg(at(1), user("fix it")),
		msg(at(2), answer(txt("Reading."), use("c1", "Read"))),
		msg(at(3), user([]any{result("c1", strings.Repeat("x", 3000)+"```", true)})),
		m{"type": "compaction_state", "timestamp": at(4), "summaryText": "so far"},
	)
	conv, err := Convert(path)
	if err != nil {
		t.Fatal(err)
	}
	md := Markdown(conv)
	for _, s := range []string{"# A session", "`/Users/dev/acme`", "## User\n\nfix it", "## Droid\n\nReading.", "Tool `Read`", "Error of `Read`", "## Earlier part, as compacted", "so far"} {
		if !strings.Contains(md, s) {
			t.Errorf("no %q in:\n%s", s, md)
		}
	}
	if strings.Contains(md, strings.Repeat("x", resultCap+1)) || strings.Count(md, "```")%2 != 0 {
		t.Errorf("the result is not cut and fenced:\n%s", md)
	}
}
