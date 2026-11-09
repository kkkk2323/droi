package memorywork

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/internal/memory"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
)

// A Memory pass on the real Kernel (ADR 0042): a conversation is read through conversation.entries, its Session
// Summary and the Memory Summary come from model.complete, and the Kernel ends the pass with the conversations and
// entries it started with — no hidden Session, no entry written.
func TestAMemoryPassOnTheKernelOpensNothingAndWritesNoEntry(t *testing.T) {
	summary, _ := json.Marshal(map[string]string{"summary": "# Task: fix login\nThe user said to use pnpm.", "slug": "fix login"})
	memorySummary, _ := json.Marshal(map[string]string{"memory_summary": validSummary + "- session_summaries/\n"})
	k := fakekernel.Start(t, fakekernel.Scenario{
		Steps: []fakekernel.Step{{Text: "Fixed; I used pnpm."}},
		Completions: []fakekernel.Completion{
			{Match: "Summarize this Session", Text: string(summary)},
			{Match: "current_summary", Text: string(memorySummary)},
		},
	})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	k.Ask(t, c, id, "Fix the login bug, and use pnpm", "Fixed; I used pnpm.", 30*time.Second)

	ctx := context.Background()
	counts := func() (int, int) {
		t.Helper()
		list, err := c.List(ctx, kernel.ListParams{Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		archived := true
		gone, err := c.List(ctx, kernel.ListParams{Archived: &archived, Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		entries, err := c.Entries(ctx, kernel.EntriesParams{ID: id, Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		return len(list.Items) + len(gone.Items), len(entries.Items)
	}
	conversations, entries := counts()
	before := len(k.Completions())

	dir := filepath.Join(t.TempDir(), "memory")
	w := Work{
		Dir:           dir,
		Run:           NewKernelRunner(c),
		Kernel:        c,
		Models:        Models{SessionSummary: fakekernel.ModelID, MemorySummary: fakekernel.ModelID},
		SessionPrompt: "You write Session Summaries.",
		MemoryPrompt:  "You keep the Memory Summary.",
		Home:          t.TempDir(),
		Now:           func() time.Time { return time.Now().Add(MinIdle + time.Hour) },
		Log:           func(s string) { t.Log(s) },
	}
	if err := w.Pass(ctx, MinIdle, false); err != nil {
		t.Fatalf("pass: %v\n%s", err, k.Logs())
	}

	if got, want := len(k.Completions())-before, 2; got != want {
		t.Fatalf("%d model.complete calls, want %d", got, want)
	}
	asked := k.Completions()[before]
	body, _ := json.Marshal(asked.Messages)
	if !strings.Contains(string(body), "You write Session Summaries.") || !strings.Contains(string(body), "User: Fix the login bug, and use pnpm") ||
		!strings.Contains(string(body), "Droid: Fixed; I used pnpm.") {
		t.Fatalf("the Session Summary call did not carry the prompt and the conversation: %s", body)
	}
	st := memory.LoadState(dir)
	rec := st.Sessions[strconv.FormatInt(int64(id), 10)]
	if rec == nil || rec.File == "" {
		t.Fatalf("no Session Summary for conversation %d: %+v", id, st.Sessions)
	}
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rec.File)))
	if err != nil || !strings.Contains(string(b), "The user said to use pnpm.") {
		t.Fatalf("the Session Summary: %q (%v)", b, err)
	}
	if got := memory.ReadSummary(dir); !strings.HasPrefix(got, "v1\n") {
		t.Fatalf("the Memory Summary: %q", got)
	}
	if c2, e2 := counts(); c2 != conversations || e2 != entries {
		t.Fatalf("the Kernel has %d conversations and %d entries after the pass, %d and %d before", c2, e2, conversations, entries)
	}
}
