package app

import (
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/sessions"
)

// TestBusyAtIsTheTimeATurnWasFirstSeen: a busy Session keeps the time it had as its turn was first seen, while the
// Kernel dates it anew at each step; a Session whose turn ended loses it, and its next turn takes a time of its own.
func TestBusyAtIsTheTimeATurnWasFirstSeen(t *testing.T) {
	a := &App{}
	order := func(busy ...string) sessions.Order {
		o := sessions.DefaultOrder
		o.Busy = map[string]bool{}
		for _, id := range busy {
			o.Busy[id] = true
		}
		return o
	}
	list := func(a, b int64) []sessions.Summary {
		return []sessions.Summary{{SessionID: "a", UpdatedAt: a}, {SessionID: "b", UpdatedAt: b}}
	}
	if got := a.busyAt(list(10, 20), order("a", "b")); got["a"] != 10_000 || got["b"] != 20_000 {
		t.Fatalf("first seen: %v", got)
	}
	if got := a.busyAt(list(30, 25), order("a", "b")); got["a"] != 10_000 || got["b"] != 20_000 {
		t.Errorf("a step moved a running turn's time: %v", got)
	}
	// A view of part of the list keeps the times of the Sessions it does not list.
	if got := a.busyAt(list(30, 25)[:1], order("a", "b")); got["b"] != 20_000 {
		t.Errorf("a Session busy elsewhere lost its time: %v", got)
	}
	if got := a.busyAt(list(30, 25), order("b")); len(got) != 1 || got["b"] != 20_000 {
		t.Errorf("a turn that ended kept its time: %v", got)
	}
	if got := a.busyAt(list(40, 25), order("a", "b")); got["a"] != 40_000 {
		t.Errorf("a new turn kept the old one's time: %v", got)
	}
}
