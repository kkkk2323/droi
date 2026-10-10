package app

import (
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/prefs"
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
