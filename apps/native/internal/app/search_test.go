package app

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/v2/internal/app/testsupport"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/sessions"
)

func TestTitleHitsMatchTheTitleNewestFirst(t *testing.T) {
	listed := []sessions.Summary{
		{SessionID: "old", Title: "空调遥控", UpdatedAt: 10},
		{SessionID: "new", Title: "微信小程序空调控制转网页版", UpdatedAt: 20},
		{SessionID: "other", Title: "Fix the login race", UpdatedAt: 30},
		{SessionID: "sub", Title: "Worker: 空调", UpdatedAt: 40, CallingSessionID: "new"},
		{SessionID: "case", Title: "LOGIN page", UpdatedAt: 5},
	}
	var got []string
	for _, h := range titleHits(listed, "空调") {
		got = append(got, h.sessionID)
	}
	if len(got) != 2 || got[0] != "new" || got[1] != "old" {
		t.Fatalf("title hits %v", got)
	}
	if hits := titleHits(listed, "login"); len(hits) != 2 {
		t.Fatalf("case-insensitive title hits %+v", hits)
	}
}

func TestMergeHitsKeepsTheFirstAndTakesSnippets(t *testing.T) {
	snippet := []snippetRun{{text: "空调", match: true}}
	got := mergeHits([]searchHit{{sessionID: "a"}}, []searchHit{{sessionID: "b"}, {sessionID: "a", snippet: snippet}})
	if len(got) != 2 || got[0].sessionID != "a" || len(got[0].snippet) != 1 || got[1].sessionID != "b" {
		t.Fatalf("merged %+v", got)
	}
	var many []searchHit
	for i := range searchLimit + 5 {
		many = append(many, searchHit{sessionID: string(rune('A' + i))})
	}
	if got := mergeHits(nil, many); len(got) != searchLimit {
		t.Fatalf("merged %d hits, want %d", len(got), searchLimit)
	}
}

// The Daemon searches Memory Sessions too, and gives no tags with its hits:
// a hit titled as Droi titles them is one, unless the list has it.
func TestContentHitsLeaveOutMemorySessions(t *testing.T) {
	res := &protocol.SearchSessionsResult{Sessions: []protocol.DaemonSearchSessionsSessionResult{
		{SessionID: "work", Title: "Evaluate enterprise version"},
		{SessionID: "memory", Title: sessions.MemoryTitlePrefix + "update the Memory Summary"},
		{SessionID: "named", Title: sessions.MemoryTitlePrefix + "notes I keep"},
	}}
	listed := []sessions.Summary{{SessionID: "named", Title: sessions.MemoryTitlePrefix + "notes I keep"}}
	var got []string
	for _, h := range contentHits(res, listed) {
		got = append(got, h.sessionID)
	}
	if len(got) != 2 || got[0] != "work" || got[1] != "named" {
		t.Fatalf("content hits %v", got)
	}
}

func TestWorkspaceHitsMatchTheNameOnly(t *testing.T) {
	groups := []sessions.Group{
		{Key: "/w/acme-web", Label: "acme-web", Path: "/w/acme-web"},
		{Key: "/w/billing", Label: "Billing", Path: "/w/billing"},
		{Key: "scratch", Label: "Recents", Scratch: true},
		{Key: sessions.PinnedSessionsGroupKey, Label: "Pinned"},
	}
	var got []string
	for _, g := range workspaceHits(groups, "bill") {
		got = append(got, g.Key)
	}
	if len(got) != 1 || got[0] != "/w/billing" {
		t.Fatalf("workspace hits %v", got)
	}
	if hits := workspaceHits(groups, "w/"); len(hits) != 0 {
		t.Fatalf("the path matched: %+v", hits)
	}
	if hits := workspaceHits(groups, "re"); len(hits) != 0 {
		t.Fatalf("Recents or Pinned matched: %+v", hits)
	}
}

// The search box's icons sit in the middle of its height, at each density.
func TestTheSearchBoxCentersItsIcons(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	for _, compact := range []bool{false, true} {
		h.a.kit.Compact = compact
		h.click("Search sessions and workspaces")
		h.tt.Key(ui.Cmd, ui.KeyA)
		h.tt.Type("zz")
		h.until("the clear button", func() bool { _, ok := h.tt.Find("Clear search"); return ok })
		box, _ := h.tt.Find("Search sessions and workspaces")
		x, _ := h.tt.Find("Clear search")
		if d := (box.Y + box.H/2) - (x.Y + x.H/2); d > 0.5 || d < -0.5 {
			t.Fatalf("compact %v: the clear button %+v is off the middle of the box %+v", compact, x, box)
		}
	}
}

// The sidebar's search finds a Workspace by its name and shows it with its
// Sessions, as the sidebar does.
func TestTheSidebarSearchFindsWorkspaces(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("Search sessions and workspaces")
	h.tt.Type("billing")
	h.until("the Workspace", func() bool { return h.hasText("Refactor invoice generator") && !h.hasText("Add dark mode toggle") })
	if !h.hasText("billing-service") {
		t.Fatalf("no billing-service in %q", h.tt.Texts())
	}
	h.click("Refactor invoice generator")
	h.until("the Session", func() bool { return h.a.Route().Name == "session" })

	h.click("Search sessions and workspaces")
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Type("zzzz")
	h.until("the empty result", func() bool { return h.hasText("No sessions or workspaces match.") })
}

func TestMarkedRunsMarkEveryPlaceInAnyCase(t *testing.T) {
	got := markedRuns("  Check the 内存 cache,\n then the 内存 pool ", "内存")
	want := []snippetRun{{text: "Check the "}, {text: "内存", match: true}, {text: " cache, then the "}, {text: "内存", match: true}, {text: " pool"}}
	if !slices.Equal(got, want) {
		t.Fatalf("runs %+v", got)
	}
	if got := markedRuns("Login and LOGIN", "login"); len(got) != 3 || !got[0].match || got[1].match || !got[2].match {
		t.Fatalf("case-insensitive runs %+v", got)
	}
	if got := markedRuns("", "x"); got != nil {
		t.Fatalf("an empty preview has runs %+v", got)
	}
}

// searchHitsNow are the search's hits and the query they are for.
func (h *harness) searchHitsNow() ([]searchHit, string) {
	s := &h.a.sidebar.search
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits, s.query
}

// On Kernel the search is one conversation.list{query}: it finds a conversation by the text of its messages, also a
// Chinese word of two characters, and by a title somebody gave it; a hit's second line is its newest message.
func TestTheKernelSearchFindsMessagesAndNames(t *testing.T) {
	h := newHarness(t, timesScenario(), "")
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	h.until("the list", func() bool { return h.hasText("Add dark mode toggle") })
	h.kernel.add(t, "s-fix", fakedaemon.Message{Role: "user", Text: "Check the 内存 cache first"})
	h.click("Search sessions and workspaces")
	h.tt.Type("内存")
	h.until("the hit", func() bool {
		hits, q := h.searchHitsNow()
		return q == "内存" && len(hits) == 1 && hits[0].sessionID == "s-fix" && h.hasText("Fix the login race")
	})
	hits, _ := h.searchHitsNow()
	if want := []snippetRun{{text: "Check the "}, {text: "内存", match: true}, {text: " cache first"}}; !slices.Equal(hits[0].snippet, want) {
		t.Fatalf("second line %+v", hits[0].snippet)
	}
	if h.hasText("Add dark mode toggle") {
		t.Fatal("a conversation without the word is a hit")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.kernel.client(t).UpdateIndex(ctx, kernel.IndexUpdateParams{ID: h.kernel.convs["s-bill"], Title: kernel.TitleText("Quarterly ledger")}); err != nil {
		t.Fatal(err)
	}
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Type("ledger")
	h.until("the renamed hit", func() bool {
		hits, q := h.searchHitsNow()
		return q == "ledger" && len(hits) == 1 && hits[0].sessionID == "s-bill"
	})

	// Clearing the box brings the list back.
	h.click("Clear search")
	h.until("the list again", func() bool { return h.hasText("Add dark mode toggle") && h.hasText("Fix the login race") })
}

// While the list shows the archived conversations, the search finds them by their messages too: Kernel searches
// the live ones unless asked for the archived.
func TestTheKernelSearchFindsArchivedOnesWhileTheListShowsThem(t *testing.T) {
	h := newHarness(t, timesScenario(), "")
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	h.until("the list", func() bool { return h.hasText("Add dark mode toggle") })
	h.kernel.add(t, "s-bill", fakedaemon.Message{Role: "user", Text: "reconcile the quarterly ledger"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	archived := true
	if err := h.kernel.client(t).UpdateIndex(ctx, kernel.IndexUpdateParams{ID: h.kernel.convs["s-bill"], Archived: &archived}); err != nil {
		t.Fatal(err)
	}
	prefs.ShowArchived.Set(h.a.prefs, true)
	h.click("Search sessions and workspaces")
	h.tt.Type("ledger")
	h.until("the archived hit", func() bool {
		hits, q := h.searchHitsNow()
		return q == "ledger" && len(hits) == 1 && hits[0].sessionID == "s-bill"
	})
}

// An answer that comes after another query was typed is not shown: its hits are not for the text in the box.
func TestALateSearchAnswerIsNotShown(t *testing.T) {
	h := newHarness(t, timesScenario(), "")
	h.until("the list", func() bool { return h.hasText("Add dark mode toggle") })
	s := &h.a.sidebar.search
	h.a.search("login")
	s.mu.Lock()
	first := s.seq
	s.mu.Unlock()
	h.a.search("dark mode")
	if h.a.publishSearch(first, "login", []searchHit{{sessionID: "s-fix", title: "Fix the login race"}}, true, nil) {
		t.Fatal("the earlier query is still the one answered")
	}
	if _, q := h.searchHitsNow(); q == "login" {
		t.Fatal("a late answer for an earlier query is shown")
	}
}

// The Kernel search answers at most searchLimit conversations, found by their messages.
func TestTheKernelSearchStopsAtItsLimit(t *testing.T) {
	h := newHarness(t, timesScenario(), "")
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for i := range searchLimit + 5 {
		entries, err := testsupport.History([]fakedaemon.Message{{Role: "user", Text: fmt.Sprintf("needle %d", i)}}, time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.kernel.client(t).Import(ctx, kernel.ImportParams{Cwd: "/Users/dev/acme-web", Title: fmt.Sprintf("Row %d", i), Entries: entries}); err != nil {
			t.Fatal(err)
		}
	}
	h.until("the list", func() bool { return h.hasText("Add dark mode toggle") })
	h.click("Search sessions and workspaces")
	h.tt.Type("needle")
	h.until("the hits", func() bool {
		hits, q := h.searchHitsNow()
		s := &h.a.sidebar.search
		s.mu.Lock()
		pending := s.pending
		s.mu.Unlock()
		return q == "needle" && !pending && len(hits) == searchLimit
	})
}

// A page of live hits leaves room for a newer archived one while the list shows the archived: both reads are
// merged newest first before the page is cut.
func TestTheKernelSearchKeepsANewerArchivedHitPastAFullPage(t *testing.T) {
	h := newHarness(t, timesScenario(), "")
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	importRow := func(title, text string, at time.Time, archived bool) {
		entries, err := testsupport.History([]fakedaemon.Message{{Role: "user", Text: text}}, at)
		if err != nil {
			t.Fatal(err)
		}
		p := kernel.ImportParams{Cwd: "/Users/dev/acme-web", Title: title, Entries: entries}
		if archived {
			p.Archived = &archived
		}
		if _, err := h.kernel.client(t).Import(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	for i := range searchLimit + 5 {
		importRow(fmt.Sprintf("Row %d", i), fmt.Sprintf("needle %d", i), time.Now().Add(-time.Hour), false)
	}
	importRow("Old ledger", "the needle we put away", time.Now(), true)
	prefs.ShowArchived.Set(h.a.prefs, true)
	h.until("the list", func() bool { return h.hasText("Add dark mode toggle") })
	h.click("Search sessions and workspaces")
	h.tt.Type("needle")
	h.until("the archived hit among the first page", func() bool {
		hits, q := h.searchHitsNow()
		s := &h.a.sidebar.search
		s.mu.Lock()
		pending := s.pending
		s.mu.Unlock()
		return q == "needle" && !pending && len(hits) == searchLimit &&
			slices.ContainsFunc(hits, func(hit searchHit) bool { return hit.title == "Old ledger" })
	})
}
