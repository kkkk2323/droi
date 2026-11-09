package droidimport

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
)

// Against a real Kernel, which reads a conversation newest first unless asked and fills in what an imported
// message left out: a session droid went on with goes on in its conversation, each entry once.
func TestOnTheKernelASessionThatWentOnGoesOnInItsConversation(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{})
	c := k.Dial(t)
	ctx := context.Background()
	dir := t.TempDir()
	lines := []any{start("s1"),
		msg(at(1), user("read a.go")),
		msg(at(2), answer(txt("Reading."), use("t1", "Read"))),
		msg(at(3), user([]any{result("t1", "package a", false)})),
		msg(at(4), answer(txt("It is package a."))),
	}
	path := transcript(t, dir, "s1", lines...)
	store := OpenStore(filepath.Join(dir, "imports.json"))
	first, _, err := Import(ctx, c, store, Source{ID: "s1", Path: path}, 1000, nil)
	if err != nil {
		t.Fatal(err)
	}
	transcript(t, dir, "s1", append(lines, msg(at(5), user("and b.go?")), msg(at(6), answer(txt("There is no b.go."))))...)
	second, conv, err := Import(ctx, c, store, Source{ID: "s1", Path: path}, 1000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("a new conversation %d, want %d", second, first)
	}
	page, err := c.Entries(ctx, kernel.EntriesParams{ID: first, Query: ascending, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range page.Items {
		if e.Kind != "droi.meta" && e.Kind != "droi.import" {
			kinds = append(kinds, e.Kind)
		}
	}
	want := []string{kernel.EntryUser, kernel.EntryAssistant, kernel.EntryToolResult, kernel.EntryAssistant, kernel.EntryNote, kernel.EntryUser, kernel.EntryAssistant}
	if strings.Join(kinds, ",") != strings.Join(want, ",") || len(conv.Entries) != 6 {
		t.Fatalf("the conversation holds %v", kinds)
	}
}
