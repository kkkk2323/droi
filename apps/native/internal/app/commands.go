package app

import (
	"runtime"
	"strconv"
	"strings"

	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

// Command is an action the menu bar, its shortcut and the window's buttons
// share: the menu bar's item takes the key, and the buttons run it too.
type Command struct {
	ID    string
	Label string
	// Key is its accelerator, such as "CmdOrCtrl+N".
	Key string
	// Hidden: in no menu, for its shortcut alone.
	Hidden bool
}

const (
	CmdNewSession     = "new-session"
	CmdSettings       = "settings"
	CmdToggleSidebar  = "toggle-sidebar"
	CmdScrollToLatest = "scroll-to-latest"
)

// sessionCommand is ⌘n's, which opens the sidebar's nth Session.
func sessionCommand(n int) string { return "session-" + strconv.Itoa(n) }

// Commands are the window's commands, for the menu bar.
func Commands() []Command {
	cmds := []Command{
		{ID: CmdNewSession, Label: L("New Session"), Key: "CmdOrCtrl+N"},
		{ID: CmdSettings, Label: L("Settings…"), Key: "CmdOrCtrl+,"},
		{ID: CmdToggleSidebar, Label: L("Toggle Sidebar"), Key: "CmdOrCtrl+B"},
		{ID: CmdScrollToLatest, Label: L("Scroll to Latest"), Key: "CmdOrCtrl+J"},
	}
	for n := 1; n <= 9; n++ {
		cmds = append(cmds, Command{ID: sessionCommand(n), Label: L("Session %d", n), Key: "CmdOrCtrl+" + strconv.Itoa(n), Hidden: true})
	}
	return cmds
}

// Run does a command. Main thread only.
func (a *App) Run(id string) {
	switch id {
	case CmdNewSession:
		a.Go(Route{Name: "new"})
	case CmdSettings:
		a.Go(Route{Name: "settings"})
	case CmdToggleSidebar:
		if !a.narrow {
			prefs.SidebarVisible.Set(a.prefs, !prefs.SidebarVisible.Get(a.prefs))
		}
	case CmdScrollToLatest:
		if v := a.views[a.route.SessionID]; a.route.Name == "session" && v != nil {
			v.list.FollowEnd = true
			v.list.ScrollToEnd()
		}
	}
	if rest, ok := strings.CutPrefix(id, "session-"); ok {
		n, _ := strconv.Atoi(rest)
		if n >= 1 && n <= len(a.sidebar.numbered) {
			a.Go(Route{Name: "session", SessionID: a.sidebar.numbered[n-1]})
		}
	}
}

// shortcut is how a command's key reads on this computer: ⌘B on macOS,
// Ctrl+B elsewhere.
func shortcut(id string) string {
	for _, c := range Commands() {
		if c.ID == id {
			key := strings.TrimPrefix(c.Key, "CmdOrCtrl+")
			if runtime.GOOS == "darwin" {
				return "⌘" + key
			}
			return "Ctrl+" + key
		}
	}
	return ""
}

// withShortcut is a tooltip that tells a command's shortcut.
func withShortcut(label, id string) string { return label + " (" + shortcut(id) + ")" }
