package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
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
