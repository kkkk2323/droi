package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

func TestCommandsHaveOneKeyEach(t *testing.T) {
	ids, keys := map[string]bool{}, map[string]bool{}
	for _, c := range Commands() {
		if ids[c.ID] || keys[c.Key] {
			t.Errorf("%s (%s) is there twice", c.ID, c.Key)
		}
		ids[c.ID], keys[c.Key] = true, true
		if shortcut(c.ID) == "" {
			t.Errorf("%s shows no shortcut", c.ID)
		}
	}
}

// The menu bar's commands do what the window's buttons do.
func TestCommandsRun(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.a.Run(CmdSettings)
	h.frame()
	if r := h.a.Route(); r.Name != "settings" {
		t.Fatalf("Settings went to %+v", r)
	}
	h.a.Run(CmdNewSession)
	h.frame()
	if r := h.a.Route(); r.Name != "new" {
		t.Fatalf("New Session went to %+v", r)
	}
	shown := prefs.SidebarVisible.Get(h.a.prefs)
	h.a.Run(CmdToggleSidebar)
	if prefs.SidebarVisible.Get(h.a.prefs) == shown {
		t.Fatal("Toggle Sidebar left the sidebar as it was")
	}
	h.a.Run(CmdToggleSidebar)
	h.a.Run(CmdScrollToLatest) // no Session open: nothing to scroll
}

// Holding ⌘ numbers the sidebar's first Sessions, which ⌘n opens.
func TestCmdNumbersTheSidebar(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.settle()
	if h.hasText(shortcut(sessionCommand(1))) {
		t.Fatal("numbers without ⌘ held")
	}
	h.tt.HoldModifiers(ui.Cmd)
	h.frame()
	for n := 1; n <= 3; n++ {
		if !h.hasText(shortcut(sessionCommand(n))) {
			t.Fatalf("no %s with ⌘ held: %q", shortcut(sessionCommand(n)), h.tt.Texts())
		}
	}
	second := h.a.sidebar.numbered[1]
	h.tt.HoldModifiers(0)
	h.frame()
	h.a.Run(sessionCommand(2))
	h.frame()
	if r := h.a.Route(); r.Name != "session" || r.SessionID != second {
		t.Fatalf("⌘2 went to %+v, want %s", r, second)
	}
	h.a.Run(sessionCommand(9)) // past the list: nothing
	if r := h.a.Route(); r.SessionID != second {
		t.Fatalf("⌘9 went to %+v", r)
	}
}
