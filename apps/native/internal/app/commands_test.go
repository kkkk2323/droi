package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
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
	first, second := h.a.sidebar.numbered[0], h.a.sidebar.numbered[1]
	h.tt.HoldModifiers(0)
	h.frame()
	h.tt.Key(ui.Cmd, ui.Key2)
	h.frame()
	if r := h.a.Route(); r.Name != "session" || r.SessionID != second {
		t.Fatalf("⌘2 went to %+v, want %s", r, second)
	}
	// From the Session's page, where the composer has the keyboard.
	h.settle()
	h.tt.Key(ui.Cmd, ui.Key1)
	h.frame()
	if r := h.a.Route(); r.Name != "session" || r.SessionID != first {
		t.Fatalf("⌘1 went to %+v, want %s", r, first)
	}
	h.tt.Key(ui.Cmd, ui.Key9) // past the list: nothing
	h.frame()
	if r := h.a.Route(); r.SessionID != first {
		t.Fatalf("⌘9 went to %+v", r)
	}
}

// The numbers ⌘ shows take the place of the times without moving a row.
func TestHoldingCmdLeavesTheSidebarRowsInPlace(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.settle()
	titles := []string{"Refactor invoice generator", "Add dark mode toggle", "Fix the login race"}
	rows := func() []ui.Rect {
		var out []ui.Rect
		for _, title := range titles {
			r, ok := h.tt.Find(title)
			if !ok {
				t.Fatalf("no row %q", title)
			}
			out = append(out, r)
		}
		return out
	}
	before := rows()
	h.tt.HoldModifiers(ui.Cmd)
	h.frame()
	held := rows()
	h.tt.HoldModifiers(0)
	h.frame()
	for i := range titles {
		if held[i] != before[i] {
			t.Errorf("%q moved from %+v to %+v with ⌘ held", titles[i], before[i], held[i])
		}
	}
}
