package sessions

import (
	"testing"
	"time"
)

// The list dates a Session by the last message sent to it, not by the
// Daemon's date, which every hook run and every step of a turn moves; a
// Session never sent one through a Gateway keeps the Daemon's.
func TestSessionsAreDatedByTheLastMessageSent(t *testing.T) {
	list := []Summary{
		{SessionID: "opened", Cwd: "/w/a", UpdatedAt: 900},
		{SessionID: "sent", Cwd: "/w/a", UpdatedAt: 100},
		{SessionID: "cli", Cwd: "/w/a", UpdatedAt: 500},
	}
	o := DefaultOrder
	o.Times = map[string]Times{"opened": {SentAt: 50_000}, "sent": {SentAt: 600_000}}
	gs := GroupByWorkspace(list, Pins{}, o, false)
	eq(t, ids(gs[0].Sessions), []string{"sent", "cli", "opened"})
	eq(t, UsedAt(list[2], o.Times), int64(500_000))
	recent := RecentWorkspaces(list, o.Times)
	eq(t, recent[0].LastUsedAt, int64(600))
}

// Working Sessions come first, then those with a result nobody has seen,
// then the rest, each by when it was last used; the Created order is a
// plain list.
func TestBusyThenUnreadThenNewest(t *testing.T) {
	list := []Summary{
		{SessionID: "new", Cwd: "/w/a", UpdatedAt: 40},
		{SessionID: "done", Cwd: "/w/a", UpdatedAt: 10},
		{SessionID: "working-old", Cwd: "/w/a", UpdatedAt: 20},
		{SessionID: "working", Cwd: "/w/a", UpdatedAt: 30},
	}
	o := DefaultOrder
	o.Busy = map[string]bool{"working-old": true, "working": true}
	o.Unread = map[string]bool{"done": true, "working": true}
	eq(t, ids(GroupByWorkspace(list, Pins{}, o, false)[0].Sessions), []string{"working", "working-old", "done", "new"})
	o.Sessions = SessionsCreated
	o.FirstSeen = map[string]float64{"new": 4, "done": 3, "working-old": 2, "working": 1}
	eq(t, ids(GroupByWorkspace(list, Pins{}, o, false)[0].Sessions), []string{"new", "done", "working-old", "working"})
}

// While the pointer is over the list its rows keep their places: a held
// Session keeps the Key it had, and one new to the list takes its own.
func TestHeldKeysKeepTheRowsInPlace(t *testing.T) {
	list := []Summary{
		{SessionID: "a", Cwd: "/w/a", UpdatedAt: 20},
		{SessionID: "b", Cwd: "/w/a", UpdatedAt: 10},
	}
	o := DefaultOrder
	o.Held = o.HoldKeys(list)
	o.Times = map[string]Times{"b": {SentAt: 99_000}}
	o.Busy = map[string]bool{"b": true}
	list = append(list, Summary{SessionID: "c", Cwd: "/w/a", UpdatedAt: 15})
	eq(t, ids(GroupByWorkspace(list, Pins{}, o, false)[0].Sessions), []string{"a", "c", "b"})
	o.Held = nil
	eq(t, ids(GroupByWorkspace(list, Pins{}, o, false)[0].Sessions), []string{"b", "a", "c"})
}

// The time view lists every Session once: the pinned, then the busy and
// unread, then the rest by the local day they were last used.
func TestGroupByTime(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, 10, 29, 9, 30, 0, 0, loc)
	at := func(d time.Time) int64 { return d.Unix() }
	scratch := []Tag{{Name: ScratchTag}}
	list := []Summary{
		{SessionID: "old", Cwd: "/w/alpha", UpdatedAt: at(now.AddDate(0, 0, -9))},
		{SessionID: "late-yesterday", Cwd: "/w/beta", UpdatedAt: at(time.Date(2026, 10, 28, 23, 59, 0, 0, loc))},
		{SessionID: "early-today", Cwd: "/u/.droi/chats/2026-10-29-aaaaaa", Tags: scratch, UpdatedAt: at(time.Date(2026, 10, 29, 0, 1, 0, 0, loc))},
		{SessionID: "pinned", Cwd: "/w/alpha", UpdatedAt: at(now.AddDate(0, 0, -30))},
		{SessionID: "working", Cwd: "/w/alpha", UpdatedAt: at(now.AddDate(0, 0, -2))},
		{SessionID: "sent-now", Cwd: "/w/beta", UpdatedAt: at(now.AddDate(0, 0, -20))},
	}
	o := DefaultOrder
	o.Busy = map[string]bool{"working": true}
	o.Times = map[string]Times{"sent-now": {SentAt: now.UnixMilli()}}
	gs := GroupByTime(list, Pins{Sessions: map[string]bool{"pinned": true}}, o, now)
	var got [][]string
	for _, g := range gs {
		got = append(got, append([]string{g.Key}, ids(g.Sessions)...))
	}
	eq(t, got, [][]string{
		{PinnedSessionsGroupKey, "pinned"},
		{NowGroupKey, "working"},
		{TodayGroupKey, "sent-now", "early-today"},
		{YesterdayGroupKey, "late-yesterday"},
		{EarlierGroupKey, "old"},
	})
	eq(t, gs[2].Places["sent-now"], "beta")
	eq(t, gs[2].Places["early-today"], "")
}
