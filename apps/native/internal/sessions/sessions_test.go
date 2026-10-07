package sessions

import (
	"reflect"
	"testing"
)

func ids(list []Summary) []string {
	out := []string{}
	for _, s := range list {
		out = append(out, s.SessionID)
	}
	return out
}

func labels(gs []Group) []string {
	out := []string{}
	for _, g := range gs {
		out = append(out, g.Label)
	}
	return out
}

func n(i int) *int { return &i }

func eq(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestGroupsByRepoRootNewestFirst(t *testing.T) {
	gs := GroupByWorkspace([]Summary{
		{SessionID: "a", Cwd: "/w/alpha", UpdatedAt: 10},
		{SessionID: "b", Cwd: "/w/beta/sub", RepoRoot: "/w/beta", UpdatedAt: 30},
		{SessionID: "c", Cwd: "/w/alpha", UpdatedAt: 20},
		{SessionID: "d", Cwd: "/w/beta", UpdatedAt: 5},
	}, Pins{}, DefaultOrder, false)
	eq(t, labels(gs), []string{"beta", "alpha"})
	eq(t, ids(gs[0].Sessions), []string{"b", "d"})
	eq(t, ids(gs[1].Sessions), []string{"c", "a"})
}

// A Session in a worktree belongs to its main checkout, in the list and
// among the recent Workspaces, even when the Daemon names no repoRoot.
func TestWorktreeSessionsGroupUnderTheirRepository(t *testing.T) {
	tree := &Worktree{Path: "/home/.factory/worktrees/ab12cd34/alpha", Branch: "droid/x", RepoRoot: "/w/alpha", Lifecycle: "ephemeral"}
	list := []Summary{
		{SessionID: "a", Cwd: "/w/alpha", UpdatedAt: 10},
		{SessionID: "t", Cwd: tree.Path, UpdatedAt: 20, Worktree: tree},
	}
	gs := GroupByWorkspace(list, Pins{}, DefaultOrder, false)
	eq(t, labels(gs), []string{"alpha"})
	eq(t, ids(gs[0].Sessions), []string{"t", "a"})
	recent := RecentWorkspaces(list)
	if len(recent) != 1 || recent[0].Path != "/w/alpha" || recent[0].LastUsedAt != 20 {
		t.Fatalf("recent = %+v", recent)
	}
	if !tree.Ephemeral() || (&Worktree{Lifecycle: "persistent"}).Ephemeral() || (&Worktree{}).Ephemeral() || (*Worktree)(nil).Ephemeral() {
		t.Fatal("only an ephemeral worktree goes with its Session")
	}
}

func TestMoreConversationsFirstEmptyLast(t *testing.T) {
	gs := GroupByWorkspace([]Summary{
		{Cwd: "/w/busy", UpdatedAt: 10, MessagesCount: n(4)},
		{Cwd: "/w/busy", UpdatedAt: 11, MessagesCount: n(9)},
		{Cwd: "/w/once", UpdatedAt: 50, MessagesCount: n(800)},
		{Cwd: "/w/empty", UpdatedAt: 90, MessagesCount: n(0)},
		{Cwd: "/w/empty", UpdatedAt: 91, MessagesCount: n(0)},
	}, Pins{}, DefaultOrder, false)
	eq(t, labels(gs), []string{"busy", "once", "empty"})
	if got := GroupByWorkspace([]Summary{{SessionID: "x"}}, Pins{}, DefaultOrder, false)[0].Label; got != "Unknown workspace" {
		t.Fatal(got)
	}
}

func TestPins(t *testing.T) {
	list := []Summary{
		{SessionID: "a", Cwd: "/w/alpha", UpdatedAt: 10},
		{SessionID: "b", Cwd: "/w/beta", UpdatedAt: 30},
		{SessionID: "c", Cwd: "/w/alpha", UpdatedAt: 20},
		{SessionID: "g", Cwd: "/w/gamma", UpdatedAt: 40},
	}
	gs := GroupByWorkspace(list, Pins{Workspaces: map[string]bool{"/w/alpha": true}, Sessions: map[string]bool{"a": true}}, DefaultOrder, false)
	eq(t, labels(gs), []string{"alpha", "gamma", "beta"})
	eq(t, ids(gs[0].Sessions), []string{"a", "c"})

	apart := GroupByWorkspace(list, Pins{Workspaces: map[string]bool{"/w/alpha": true}, Sessions: map[string]bool{"a": true, "b": true}}, DefaultOrder, true)
	var got [][]string
	for _, g := range apart {
		got = append(got, append([]string{g.Key}, ids(g.Sessions)...))
	}
	eq(t, got, [][]string{{PinnedSessionsGroupKey, "b", "a"}, {"/w/alpha", "c"}, {"/w/gamma", "g"}})
}

func TestSortingGroups(t *testing.T) {
	list := []Summary{
		{SessionID: "b1", Cwd: "/w/beta", UpdatedAt: 100},
		{SessionID: "b2", Cwd: "/w/beta", UpdatedAt: 200},
		{SessionID: "a1", Cwd: "/w/alpha", UpdatedAt: 900},
		{SessionID: "c1", Cwd: "/w/gamma", UpdatedAt: 500},
	}
	keys := func(o Order) []string { return labels(GroupByWorkspace(list, Pins{}, o, false)) }
	eq(t, keys(DefaultOrder), []string{"beta", "alpha", "gamma"})
	eq(t, keys(Order{Workspaces: SortRecent}), []string{"alpha", "gamma", "beta"})
	eq(t, keys(Order{Workspaces: SortName}), []string{"alpha", "beta", "gamma"})
	eq(t, keys(Order{Workspaces: SortManual, Manual: []string{"/w/gamma"}}), []string{"gamma", "beta", "alpha"})
	eq(t, labels(GroupByWorkspace(list, Pins{Workspaces: map[string]bool{"/w/gamma": true}}, Order{Workspaces: SortName}, false)), []string{"gamma", "alpha", "beta"})
	beta := func(o Order) []string {
		for _, g := range GroupByWorkspace(list, Pins{}, o, false) {
			if g.Label == "beta" {
				return ids(g.Sessions)
			}
		}
		return nil
	}
	eq(t, beta(DefaultOrder), []string{"b2", "b1"})
	eq(t, beta(Order{Sessions: SessionsCreated, FirstSeen: map[string]float64{"b1": 5000, "b2": 1000}}), []string{"b1", "b2"})

	shown := []string{"/w/a", "/w/b", "/w/c"}
	eq(t, MoveWorkspace(shown, "/w/c", "/w/a", false), []string{"/w/c", "/w/a", "/w/b"})
	eq(t, MoveWorkspace(shown, "/w/a", "/w/b", true), []string{"/w/b", "/w/a", "/w/c"})
	eq(t, MoveWorkspace(shown, "/w/a", "/w/a", true), shown)
}

func TestNoteFirstSeen(t *testing.T) {
	known := map[string]float64{"old": 1}
	if NoteFirstSeen(known, []Summary{{SessionID: "old"}}, 10000) != nil {
		t.Fatal("nothing new still changes")
	}
	eq(t, NoteFirstSeen(known, []Summary{{SessionID: "fresh", UpdatedAt: 50}, {SessionID: "later", UpdatedAt: 99}}, 60000),
		map[string]float64{"old": 1, "fresh": 50000, "later": 60000})
}

func TestScratchGoesToRecents(t *testing.T) {
	scratch := []Tag{{Name: ScratchTag}}
	gs := GroupByWorkspace([]Summary{
		{SessionID: "s1", Cwd: "/u/.droi/chats/2026-09-25-aaaaaa", Tags: scratch, UpdatedAt: 50, MessagesCount: n(9)},
		{SessionID: "p", Cwd: "/w/alpha", UpdatedAt: 10},
		{SessionID: "s2", Cwd: "/u/.droi/chats/2026-09-26-bbbbbb", Tags: scratch, UpdatedAt: 60, MessagesCount: n(9)},
	}, Pins{}, DefaultOrder, false)
	eq(t, labels(gs), []string{"alpha", "Recents"})
	eq(t, gs[1].Scratch, true)
	eq(t, ids(gs[1].Sessions), []string{"s2", "s1"})

	gs = GroupByWorkspace([]Summary{
		{SessionID: "lost", Cwd: "/u/.droi/chats/2026-10-02-5c4802", UpdatedAt: 70},
		{SessionID: "p", Cwd: "/w/alpha", UpdatedAt: 10},
		{SessionID: "dated", Cwd: "/w/2026-10-02-notes", UpdatedAt: 5},
	}, Pins{}, DefaultOrder, false)
	eq(t, labels(gs), []string{"alpha", "2026-10-02-notes", "Recents"})
}

func TestWorkspaceLabel(t *testing.T) {
	for in, want := range map[string]string{"/Users/me/dev/droi": "droi", "/Users/me/dev/droi/": "droi", `C:\code\thing`: "thing"} {
		if got := WorkspaceLabel(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestVisibleSessions(t *testing.T) {
	const now = int64(1_800_000_000_000)
	const day = int64(24 * 60 * 60)
	list := []Summary{
		{SessionID: "today", UpdatedAt: now/1000 - day},
		{SessionID: "lastWeek", UpdatedAt: now/1000 - 7*day},
		{SessionID: "lastMonth", UpdatedAt: now/1000 - 30*day},
	}
	v, h := VisibleSessions(list, 0, now, nil)
	eq(t, ids(v), []string{"today"})
	eq(t, h, 2)
	v, h = VisibleSessions(list, 1, now, nil)
	eq(t, ids(v), []string{"today", "lastWeek"})
	eq(t, h, 1)
	v, h = VisibleSessions(list, 0, now, map[string]bool{"lastMonth": true})
	eq(t, ids(v), []string{"today", "lastMonth"})
	eq(t, h, 1)
}

func TestContinuations(t *testing.T) {
	tags := ContinuationTags("old", []Tag{{Name: "team", Metadata: map[string]string{"id": "1"}}})
	child := Summary{SessionID: "new", Tags: tags, ParentID: ContinuationParent(tags)}
	eq(t, child.ParentID, "old")
	eq(t, ids(FoldContinued([]Summary{child, {SessionID: "old"}, {SessionID: "other"}})), []string{"new", "other"})
	eq(t, ids(FoldContinued([]Summary{{SessionID: "same", ParentID: "same"}, {SessionID: "other"}})), []string{"same", "other"})
	eq(t, ContinuationTags("b", ContinuationTags("a", nil)), []Tag{{Name: ContinuesTag, Metadata: map[string]string{"parent": "b"}}})

	a, b, c := Summary{SessionID: "a"}, Summary{SessionID: "b", ParentID: "a"}, Summary{SessionID: "c", ParentID: "b"}
	eq(t, ids(ContinuationChain([]Summary{c, a, b}, c)), []string{"b", "a"})
	x, y := Summary{SessionID: "x", ParentID: "y"}, Summary{SessionID: "y", ParentID: "x"}
	eq(t, ids(ContinuationChain([]Summary{x, y}, x)), []string{"y"})
}
