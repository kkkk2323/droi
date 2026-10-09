package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

// The list reads every page of Sessions, so a Workspace whose Sessions are
// all older than the first page still shows: in the sidebar, behind "Show
// 1 older workspace" once a month has passed, and in the picker, behind
// "Show N more". Reading the first page again keeps the older ones.
func TestWorkspacesBeyondTheFirstPageShow(t *testing.T) {
	now := time.Now().Unix()
	const day = 24 * 60 * 60
	var specs []fakedaemon.SessionSpec
	// Older than three days, the busy Sessions wait behind their groups'
	// "Show N older", which keeps the sidebar short enough to click in.
	for i := range sessionPage + 20 {
		specs = append(specs, fakedaemon.SessionSpec{Title: fmt.Sprintf("Busy %d", i), Cwd: fmt.Sprintf("/Users/dev/busy-%d", i%6), Extra: map[string]any{"updatedAt": now - 4*day - int64(i)*60}})
	}
	specs = append(specs,
		fakedaemon.SessionSpec{Title: "Last week's tool", Cwd: "/Users/dev/week-tool", Extra: map[string]any{"updatedAt": now - 7*day}},
		fakedaemon.SessionSpec{Title: "An old prototype", Cwd: "/Users/dev/old-proto", Extra: map[string]any{"updatedAt": now - 60*day}},
	)
	h := newHarness(t, fakedaemon.Scenario{Sessions: specs}, "")
	listed := func() int {
		h.a.mu.Lock()
		defer h.a.mu.Unlock()
		if h.a.older != olderRead {
			return -1
		}
		return len(h.a.listed)
	}
	h.until("every page", func() bool { return listed() == len(specs) })
	h.until("last week's Workspace", func() bool { return h.hasText("week-tool") })
	if h.hasText("old-proto") {
		t.Fatal("a Workspace unused for two months shows before \"Show 1 older workspace\"")
	}
	h.tt.Scroll(100, refH/2, 0, 2000)
	h.settle()
	h.click("Show 1 older workspace")
	h.until("the old Workspace", func() bool { return h.hasText("old-proto") })

	h.a.refreshList()
	if n := listed(); n != len(specs) {
		t.Fatalf("reading the first page again left %d Sessions of %d", n, len(specs))
	}

	h.click("New session")
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.click("Workspace")
	h.until("the search", func() bool { _, ok := h.tt.Find("Search projects"); return ok })
	rows := func() string {
		all := strings.Join(h.tt.Texts(), "|")
		_, after, _ := strings.Cut(all, "|Projects|")
		before, _, _ := strings.Cut(after, "|Other folder…")
		return before
	}
	if got := rows(); strings.Contains(got, "week-tool") || !strings.Contains(got, "busy-4") || !strings.HasSuffix(got, "|Show 3 more|Show 3 more") {
		t.Fatalf("the picker lists %q", got)
	}
	h.click("Show 3 more")
	if got := rows(); !strings.Contains(got, "busy-5") || !strings.Contains(got, "week-tool") || !strings.Contains(got, "|old-proto|old-proto|/Users/dev/old-proto") || strings.Contains(got, "Show 3 more") {
		t.Fatalf("after \"Show 3 more\" the picker lists %q", got)
	}
}
