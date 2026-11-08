package memorywork

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/memory"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// fakeKernel is a Kernel whose conversations and entries the test writes. It answers two rows a page, whatever the
// caller asks, so a test also walks the pages.
type fakeKernel struct {
	mu      sync.Mutex
	rows    []kernel.Summary
	entries map[kernel.ID][]kernel.Entry
	models  []kernel.ModelInfo
	calls   []kernel.ModelCompleteParams
	listErr error
	answer  func(kernel.ModelCompleteParams) (kernel.ModelCompleteResult, error)
}

const fakePage = 2

func page[T any](items []T, cursor json.RawMessage) ([]T, json.RawMessage) {
	from := 0
	if len(cursor) > 0 {
		from, _ = strconv.Atoi(string(cursor))
	}
	to := min(from+fakePage, len(items))
	if to >= len(items) {
		return items[from:to], nil
	}
	return items[from:to], json.RawMessage(strconv.Itoa(to))
}

func (k *fakeKernel) Harness(context.Context) (kernel.HarnessInfo, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return kernel.HarnessInfo{Models: k.models}, nil
}

func (k *fakeKernel) List(_ context.Context, p kernel.ListParams) (kernel.ListResult, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.listErr != nil {
		return kernel.ListResult{}, k.listErr
	}
	var rows []kernel.Summary
	for _, r := range k.rows {
		if r.Archived == (p.Archived != nil && *p.Archived) {
			rows = append(rows, r)
		}
	}
	slices.SortFunc(rows, func(a, b kernel.Summary) int { return int(b.LastActivityAt - a.LastActivityAt) })
	items, next := page(rows, p.Cursor)
	return kernel.ListResult{Items: items, Cursor: next}, nil
}

func (k *fakeKernel) Entries(_ context.Context, p kernel.EntriesParams) (kernel.EntriesResult, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	items, next := page(k.entries[p.ID], p.Cursor)
	return kernel.EntriesResult{Items: items, Cursor: next}, nil
}

func (k *fakeKernel) ModelComplete(_ context.Context, p kernel.ModelCompleteParams) (kernel.ModelCompleteResult, error) {
	k.mu.Lock()
	k.calls = append(k.calls, p)
	answer := k.answer
	k.mu.Unlock()
	return answer(p)
}

func (k *fakeKernel) inputs() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []string
	for _, c := range k.calls {
		out = append(out, c.Input.(string))
	}
	return out
}

// summarized are the conversations the Session Summary calls were about, in the order asked.
func (k *fakeKernel) summarized() []string {
	var out []string
	for _, in := range k.inputs() {
		if _, rest, ok := strings.Cut(in, "- session_id: "); ok {
			out = append(out, strings.SplitN(rest, "\n", 2)[0])
		}
	}
	slices.Sort(out)
	return out
}

func (k *fakeKernel) reset() {
	k.mu.Lock()
	k.calls = nil
	k.mu.Unlock()
}

func newFakeKernel(f *fixture) *fakeKernel {
	k := &fakeKernel{
		entries: map[kernel.ID][]kernel.Entry{},
		models:  []kernel.ModelInfo{{Provider: "factory", ID: "flash"}, {Provider: "factory", ID: "pro"}},
	}
	k.answer = func(p kernel.ModelCompleteParams) (kernel.ModelCompleteResult, error) {
		var answer string
		var err error
		if strings.HasPrefix(p.Input.(string), "Summarize this Session") {
			answer, err = f.summary(Request{})
		} else {
			var in memoryInput
			if err := json.Unmarshal([]byte(p.Input.(string)), &in); err != nil {
				f.t.Fatalf("input %s", p.Input)
			}
			answer, err = f.memory(Request{}, in)
		}
		return kernel.ModelCompleteResult{Model: p.Model, Text: answer, JSON: json.RawMessage(answer)}, err
	}
	return k
}

func message(role string, content any) json.RawMessage {
	b, _ := json.Marshal([]map[string]any{{"role": role, "content": content, "timestamp": 1}})
	return b
}

// conversation adds a conversation the user had, last active idle ago, with a question and an answer per prompt.
func (k *fakeKernel) conversation(f *fixture, id kernel.ID, kind string, idle time.Duration, prompts ...string) *kernel.Summary {
	k.mu.Lock()
	defer k.mu.Unlock()
	at := f.now.Add(-idle).UnixMilli()
	k.rows = append(k.rows, kernel.Summary{ID: id, Kind: kind, Title: fmt.Sprintf("Conversation %d", id), Workspace: f.root, CreatedAt: at - 1000, LastActivityAt: at})
	var entries []kernel.Entry
	for i, p := range prompts {
		entries = append(entries,
			kernel.Entry{ID: id*100 + kernel.ID(2*i), ConversationID: id, Kind: kernel.EntryUser, Model: message("user", p)},
			kernel.Entry{ID: id*100 + kernel.ID(2*i+1), ConversationID: id, Kind: kernel.EntryAssistant, Model: message("assistant", []any{map[string]any{"type": "text", "text": "done: " + p}})})
	}
	k.entries[id] = entries
	return &k.rows[len(k.rows)-1]
}

func (k *fakeKernel) touch(id kernel.ID, at time.Time) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for i := range k.rows {
		if k.rows[i].ID == id {
			k.rows[i].LastActivityAt = at.UnixMilli()
		}
	}
}

func (f *fixture) kernelWork(k *fakeKernel) Work {
	w := f.work()
	w.Run, w.Kernel = NewKernelRunner(k), k
	return w
}

// A conversation on the Kernel is summarized from its entries with model.complete: the prompt as the system text,
// the schema, a low effort and the model's provider; the Memory Summary takes it in. Kernel has no method here that
// opens a conversation, so none can be opened.
func TestAKernelConversationIsSummarizedThroughModelComplete(t *testing.T) {
	f := newFixture(t)
	k := newFakeKernel(f)
	k.conversation(f, 7, "chat", 7*time.Hour, "Fix the login bug")
	k.mu.Lock()
	k.rows[0].Title = "Fix login"
	k.entries[7] = append(k.entries[7],
		kernel.Entry{ID: 702, ConversationID: 7, Kind: kernel.EntryAssistant, Model: message("assistant", []any{
			map[string]any{"type": "thinking", "thinking": "hidden"},
			map[string]any{"type": "toolCall", "name": "Execute", "arguments": map[string]any{"command": "pnpm test"}},
		})},
		kernel.Entry{ID: 703, ConversationID: 7, Kind: kernel.EntryToolResult, Model: message("toolResult", []any{map[string]any{"type": "text", "text": "ok"}})},
		kernel.Entry{ID: 704, ConversationID: 7, Kind: kernel.EntryUser, Model: message("user", "不对，用 pnpm")},
	)
	k.mu.Unlock()

	if err := f.kernelWork(k).Pass(context.Background(), MinIdle, false); err != nil {
		t.Fatal(err)
	}
	eq(t, len(k.calls), 2)
	s := k.calls[0]
	schema, _ := json.Marshal(SessionSummarySchema)
	eq(t, []any{s.Model, s.System, s.ThinkingLevel, string(s.Schema)}, []any{kernel.ModelRef{Provider: "factory", ModelID: "flash"}, "S", "low", string(schema)})
	in := s.Input.(string)
	if !strings.Contains(in, "User: Fix the login bug\nDroid: done: Fix the login bug\n[tool call] Execute {\"command\":\"pnpm test\"}\n[tool result] ok\nUser: 不对，用 pnpm\n") ||
		!strings.Contains(in, "- session_id: 7\n- workspace: "+f.root+" (") || !strings.Contains(in, "- title: Fix login\n") || strings.Contains(in, "hidden") {
		t.Fatal(in)
	}
	m := k.calls[1]
	eq(t, []any{m.Model, m.System, m.ThinkingLevel}, []any{kernel.ModelRef{Provider: "factory", ModelID: "pro"}, "M", "medium"})

	name := "session_summaries/2026-10-27T05-00-00-7-fix-login.md"
	eq(t, f.read(name), "session_id: 7\nupdated_at: 2026-10-27T05:00:00Z\nworkspace: "+f.root+"\ntitle: Fix login\n\n# Task: fix login\nThe user said to use pnpm.\n")
	eq(t, f.read(memory.SummaryFile), validSummary+"- "+name+"\n")
	st := memory.LoadState(f.dir)
	eq(t, []string{st.Runs[0].SessionID, st.Runs[0].Model, st.Runs[1].SessionID, st.Runs[1].Model}, []string{"", "pro", "", "flash"})
	eq(t, st.Sessions["7"].Conversation, int64(7))

	// Nothing changed: no call at all.
	k.reset()
	if err := f.kernelWork(k).Pass(context.Background(), MinIdle, false); err != nil {
		t.Fatal(err)
	}
	eq(t, len(k.calls), 0)

	// The conversation went on: summarized again, and the summary of before is replaced.
	k.touch(7, f.now.Add(-30*time.Minute))
	f.now = f.now.Add(7 * time.Hour)
	if err := f.kernelWork(k).Pass(context.Background(), MinIdle, false); err != nil {
		t.Fatal(err)
	}
	eq(t, k.summarized(), []string{"7"})
	files, _ := memory.Files(f.dir)
	var summaries []string
	for _, file := range files {
		if strings.HasPrefix(file.Path, memory.SessionSummariesDir+"/") {
			summaries = append(summaries, file.Path)
		}
	}
	eq(t, summaries, []string{"session_summaries/2026-10-27T11-30-00-7-fix-login.md"})
}

// Runs, Memory's own work and subagents are no user's; a conversation still going on, one too old, and one with
// nothing said are not summarized; an archived one is.
func TestKernelConversationsNoUserWorkedInAreLeftAlone(t *testing.T) {
	f := newFixture(t)
	k := newFakeKernel(f)
	k.conversation(f, 1, "chat", 7*time.Hour, "mine")
	k.conversation(f, 2, "scratch", 8*time.Hour, "scratch work")
	k.conversation(f, 3, "automation", 7*time.Hour, "report")
	k.conversation(f, 4, "workstream", 7*time.Hour, "plan")
	k.conversation(f, 5, "memory", 7*time.Hour, "x")
	owner := kernel.ID(1)
	k.conversation(f, 6, "chat", 7*time.Hour, "subagent work").OwnerID = &owner
	k.conversation(f, 7, "chat", time.Hour, "still going")
	k.conversation(f, 8, "chat", 40*24*time.Hour, "long ago")
	k.conversation(f, 9, "chat", 9*time.Hour, "archived work").Archived = true
	k.conversation(f, 10, "chat", 10*time.Hour)
	if err := f.kernelWork(k).Pass(context.Background(), MinIdle, false); err != nil {
		t.Fatal(err)
	}
	eq(t, k.summarized(), []string{"1", "2", "9"})
	st := memory.LoadState(f.dir)
	var known []string
	for id := range st.Sessions {
		known = append(known, id)
	}
	slices.Sort(known)
	// 7 waits; 10 is summarized as having nothing to say.
	eq(t, known, []string{"1", "10", "2", "7", "9"})
	eq(t, Waiting(st, f.now), 1)
	eq(t, st.Sessions["10"].File, "")
	if st.Sessions["10"].SummarizedMtime == 0 {
		t.Fatal("the conversation with nothing said is not marked summarized")
	}
}

// A model no provider offers, and a Kernel that went away, are no fault of the conversation: it is tried again in
// an hour, as often as it takes. A model that answered badly counts against it.
func TestAKernelFailureCountsOnlyWhenTheModelWasAsked(t *testing.T) {
	f := newFixture(t)
	k := newFakeKernel(f)
	k.conversation(f, 1, "chat", 7*time.Hour, "mine")
	pass := func() {
		t.Helper()
		_ = f.kernelWork(k).Pass(context.Background(), MinIdle, false)
	}
	rec := func() memory.SessionRecord { return *memory.LoadState(f.dir).Sessions["1"] }

	k.models = []kernel.ModelInfo{{Provider: "factory", ID: "pro"}}
	pass()
	eq(t, len(k.calls), 0)
	eq(t, []any{rec().Failures, rec().RetryAt}, []any{0, iso(f.now.Add(RetryAfter))})

	k.models = []kernel.ModelInfo{{Provider: "factory", ID: "flash"}, {Provider: "factory", ID: "pro"}}
	k.answer = func(kernel.ModelCompleteParams) (kernel.ModelCompleteResult, error) {
		return kernel.ModelCompleteResult{}, errors.New("websocket: closed")
	}
	f.now = f.now.Add(RetryAfter)
	pass()
	eq(t, []any{rec().Failures, rec().RetryAt}, []any{0, iso(f.now.Add(RetryAfter))})
	st := memory.LoadState(f.dir)
	eq(t, st.Runs[0].Error, ErrKernelGone.Error())

	k.answer = func(kernel.ModelCompleteParams) (kernel.ModelCompleteResult, error) {
		return kernel.ModelCompleteResult{}, &kernel.RPCError{Code: kernel.CodeModelInvalid, Message: "the answer does not match the schema"}
	}
	f.now = f.now.Add(RetryAfter)
	pass()
	eq(t, []any{rec().Failures, rec().RetryAt}, []any{1, iso(f.now.Add(RetryAfter))})

	// A model that ran out of time was asked: it counts, rather than being taken for a Kernel that went away and
	// tried for ever.
	k.answer = func(kernel.ModelCompleteParams) (kernel.ModelCompleteResult, error) {
		return kernel.ModelCompleteResult{}, fmt.Errorf("kernel: model.complete: %w", context.DeadlineExceeded)
	}
	f.now = f.now.Add(RetryAfter)
	pass()
	eq(t, rec().Failures, 2)
}

// A list that fails forgets nothing: the conversations it knew still wait.
func TestAFailedListKeepsTheConversationsKnown(t *testing.T) {
	f := newFixture(t)
	k := newFakeKernel(f)
	k.conversation(f, 1, "chat", time.Hour, "going on")
	if err := f.kernelWork(k).Pass(context.Background(), MinIdle, false); err != nil {
		t.Fatal(err)
	}
	eq(t, Waiting(memory.LoadState(f.dir), f.now), 1)
	k.listErr = errors.New("kernel: closed")
	if err := f.kernelWork(k).Pass(context.Background(), MinIdle, false); err != nil {
		t.Fatal(err)
	}
	eq(t, Waiting(memory.LoadState(f.dir), f.now), 1)
}

// The Controller on the Kernel says the Kernel is not running, not the Daemon, until it has a connection.
func TestTheControllerOnTheKernelWaitsForIt(t *testing.T) {
	f := newFixture(t)
	if err := ResetPrompts(f.dir); err != nil {
		t.Fatal(err)
	}
	k := newFakeKernel(f)
	k.conversation(f, 1, "chat", 7*time.Hour, "mine")
	var connected Kernel
	c := New(Options{MemoryDir: f.dir, Kernel: func() Kernel { return connected }, Models: func() Models { return Models{SessionSummary: "flash", MemorySummary: "pro"} }, Now: func() time.Time { return f.now }})
	if err := c.UpdateNow(); err == nil || err.Error() != "The Kernel is not running." {
		t.Fatalf("UpdateNow without a Kernel: %v", err)
	}
	connected = k
	if err := c.UpdateNow(); err != nil {
		t.Fatal(err)
	}
	eq(t, k.summarized(), []string{"1"})
}
