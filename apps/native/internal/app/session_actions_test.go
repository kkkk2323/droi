package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// The menu by the header's title pins, copies and renames the Session.
func TestTheSessionHeaderMenuPinsCopiesAndRenames(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.openSession("Fix the login race")
	id := h.a.route.SessionID
	h.click("Session actions")
	h.click("Pin")
	if !prefs.PinnedSessions.Has(h.a.prefs, id) {
		t.Fatal("Pin did not pin the Session")
	}
	h.click("Session actions")
	h.click("Copy session ID")
	if got := h.tt.Clipboard(); got != id {
		t.Fatalf("copied %q, want %q", got, id)
	}
	h.click("Session actions")
	h.click("Copy session details")
	if got := h.tt.Clipboard(); !strings.Contains(got, id) {
		t.Fatalf("the details %q do not name the Session", got)
	}
	h.click("Session actions")
	h.click("Unpin")
	if prefs.PinnedSessions.Has(h.a.prefs, id) {
		t.Fatal("Unpin did not unpin the Session")
	}
	h.click("Session actions")
	h.click("Rename")
	h.until("the title field", func() bool { _, ok := h.tt.Find("Session title"); return ok })
}

// On Kernel, Rename writes the conversation's title as a chosen one and Archive archives it, each through index.update.
// The row changes where it is, the others keep their order, and a read of the list again does not bring the
// archived one back.
func TestOnKernelRenameAndArchiveWriteTheIndex(t *testing.T) {
	h := newHarness(t, timesScenario(), "")
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	h.until("the list", func() bool { return h.hasText("Add dark mode toggle") })
	h.settle()
	if !h.above("Fix the login race", "Add dark mode toggle") {
		t.Fatal("the newest is not first")
	}
	h.chooseFromMenu("Add dark mode toggle", "Rename")
	h.until("the title field", func() bool { return h.tt.Focused("Session title") })
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Type("Night theme")
	h.tt.Key(0, ui.KeyEnter)
	h.until("the new title", func() bool { return h.hasText("Night theme") && !h.hasText("Add dark mode toggle") })
	row := h.kernel.row(t, "s-dark")
	if row.Title != "Night theme" || row.TitleSource != kernel.TitleSourceChosen {
		t.Fatalf("Kernel's row %+v", row)
	}
	h.settle()
	if !h.above("Fix the login race", "Night theme") {
		t.Fatal("a rename moved the row")
	}

	h.chooseFromMenu("Fix the login race", "Archive")
	h.archivedOnBackend("s-fix", true)
	h.until("the row gone", func() bool { return !h.hasText("Fix the login race") })
	h.a.reloadList()
	h.settle()
	if h.hasText("Fix the login race") {
		t.Fatal("a read of the list brought the archived row back")
	}
	if !h.hasText("Night theme") {
		t.Fatal("archiving one row took another")
	}
}
