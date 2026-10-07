package app

import (
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/transcript"
)

func TestRailItems(t *testing.T) {
	text := func(s string) []transcript.Block { return []transcript.Block{{Kind: transcript.Text, Text: s}} }
	entries := []*transcript.Entry{
		{ID: "r0", Blocks: text("the middle of a reply")},
		{ID: "u1", User: true, Blocks: text("fix  the\nlogin")},
		{ID: "r1", Blocks: text("Done.")},
		{ID: "r1b", Blocks: text("More.")},
		{ID: "u2", User: true, Blocks: []transcript.Block{{Kind: transcript.Picture}}},
	}
	all := []*transcript.Entry{{ID: "u0", User: true, Blocks: text("start")}, entries[1], entries[4]}
	items := railItems(entries, unloadedUserMessages(all, entries))
	if len(items) != 3 {
		t.Fatalf("items %+v", items)
	}
	want := []railItem{
		{id: "u0", entry: -1, prompt: "start"},
		{id: "u1", entry: 1, prompt: "fix the login", response: "Done."},
		{id: "u2", entry: 4, prompt: "Attached image"},
	}
	for i := range want {
		if items[i] != want[i] {
			t.Errorf("item %d: %+v, want %+v", i, items[i], want[i])
		}
	}
	for entry, active := range map[int]int{-1: 2, 0: 0, 1: 1, 3: 1, 4: 2} {
		if got := activeRailItem(items, entry); got != active {
			t.Errorf("entry %d: active %d, want %d", entry, got, active)
		}
	}
	if got := unloadedUserMessages(all, nil); len(got) != 3 {
		t.Errorf("with nothing loaded all are unloaded: %d", len(got))
	}
}
