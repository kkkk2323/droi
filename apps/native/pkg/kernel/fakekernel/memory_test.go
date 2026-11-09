package fakekernel_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
)

// A Kernel started with a Memory folder gives its conversations the Memory tools, and tells every connection which
// Session Summary the model read: the Host counts it (ADR 0042).
func TestAKernelWithMemoryToldWhichSessionSummaryWasRead(t *testing.T) {
	dir := t.TempDir()
	file := "session_summaries/2026-10-20T10-00-00-aaaaaaaa-login.md"
	if err := os.MkdirAll(filepath.Join(dir, "session_summaries"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(file)), []byte("session_id: a\n\n# Fix login\nUse pnpm.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	k := fakekernel.Start(t, fakekernel.Scenario{
		MemoryDir: dir,
		Steps: []fakekernel.Step{
			{Match: "login", Call: &fakekernel.Call{Name: "memory_read", Arguments: map[string]any{"path": file}}},
			{Match: "login", Text: "Use pnpm."},
		},
	})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	k.Submit(t, c, id, "what did we do about login?")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("no memory.read\n%s", k.Logs())
		case n := <-c.Notifications():
			if n.Method != kernel.NotifyMemoryRead {
				continue
			}
			var read kernel.MemoryRead
			if err := n.Decode(&read); err != nil {
				t.Fatal(err)
			}
			if read.File != file {
				t.Fatalf("read %q, want %q", read.File, file)
			}
			if _, err := time.Parse(time.RFC3339, read.At); err != nil {
				t.Fatalf("at %q: %v", read.At, err)
			}
			return
		}
	}
}
