package app

import (
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/sessions"
)

func listedOrder(a *App) []string {
	var out []string
	for _, g := range sessions.GroupByWorkspace(a.listed, sessions.Pins{}, sessions.DefaultOrder, false) {
		for _, s := range g.Sessions {
			out = append(out, s.SessionID)
		}
	}
	return out
}

// Two working Sessions keep the places their turns gave them: the steps
// inside a turn, and a fresh read of the list, do not swap them.
func TestTouchKeepsWorkingSessionsInPlace(t *testing.T) {
	now := time.Unix(100, 0)
	a := New(Config{Now: func() time.Time { return now }})
	daemonList := func(aAt, bAt int64) []sessions.Summary {
		return []sessions.Summary{
			{SessionID: "A", Cwd: "/w", UpdatedAt: aAt},
			{SessionID: "B", Cwd: "/w", UpdatedAt: bAt},
		}
	}
	a.listed = daemonList(50, 40)

	a.touch("B", true)
	now = time.Unix(101, 0)
	a.touch("A", true)
	if got := listedOrder(a); got[0] != "A" {
		t.Fatalf("the Session whose turn began last is not first: %v", got)
	}
	now = time.Unix(110, 0)
	a.touch("B", true)
	if got := listedOrder(a); got[0] != "A" {
		t.Fatalf("a step inside B's turn moved it up: %v", got)
	}

	list := daemonList(105, 109)
	a.holdTurns(list)
	a.listed = list
	if got := listedOrder(a); got[0] != "A" {
		t.Fatalf("reading the list again swapped the working Sessions: %v", got)
	}

	a.touch("A", false)
	list = daemonList(120, 109)
	a.holdTurns(list)
	if list[0].UpdatedAt != 120 || list[1].UpdatedAt != 100 {
		t.Fatalf("a finished Session takes the Daemon's date, a working one its turn's: %+v", list)
	}
}
