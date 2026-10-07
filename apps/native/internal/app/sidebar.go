package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/activity"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
	"github.com/kkkk2323/droi/apps/native/internal/sessions"
)

// Folded like a Workspace group, in the same preference; no Workspace key has
// these prefixes.
const (
	pinnedSectionKey     = "droi:pinned"
	workspacesSectionKey = "droi:workspaces"
)

type sidebarState struct {
	query    string
	search   searchState
	revealed map[string]int
	renaming string
	rename   string
}

// sidebarView is the web Client's SessionSidebar.
func (a *App) sidebarView(c *ui.Context, listed []sessions.Summary, reported map[string]string, listErr string, listDone, more bool) {
	k, t := a.kit, a.kit.T
	pins := sessions.Pins{Workspaces: set(prefs.PinnedWorkspaces.Get(a.prefs)), Sessions: set(prefs.PinnedSessions.Get(a.prefs))}
	order := a.sortOrder(listed)
	groups := sessions.GroupByWorkspace(sessions.FoldContinued(sessions.WithoutAutomationRuns(sessions.MainSessions(listed))), pins, order, true)
	busy := a.activity(reported)
	selected := ""
	if a.route.Name == "session" {
		selected = a.route.SessionID
	}

	ui.Column(c).Role(ui.RoleGroup).Label("Sessions").Fill().Background(t.Sidebar).TextColor(t.SidebarForeground).Children(func() {
		ui.Box(c).Height(44).Shrink(0).DragWindow()
		ui.Column(c).Gap(k.Px(4)).Padding(0, k.Px(8), k.Px(8), k.Px(8)).Shrink(0).Children(func() {
			if a.sidebarRow(c, "plus", "New session", false).Clicked() {
				a.Go(Route{Name: "new"})
			}
			if a.sidebarRow(c, "clock", "Automations", a.route.Name == "automations").Clicked() {
				a.Go(Route{Name: "automations"})
			}
			ui.Row(c).Gap(k.Px(4)).Children(func() {
				a.searchBox(c)
				a.sortMenu(c, groups)
			})
		})
		ui.Scroll(c).Grow(1).Padding(0, k.Px(8), k.Px(8), k.Px(8)).Children(func() {
			if strings.TrimSpace(a.sidebar.query) != "" {
				a.searchResults(c, selected)
				return
			}
			if listErr != "" {
				k.Text(c, listErr, 12, 16).TextColor(t.DestructiveForeground).Padding(k.Px(4), k.Px(8))
			}
			if !listDone && len(groups) == 0 {
				a.skeleton(c)
			}
			if listDone && len(groups) == 0 && listErr == "" {
				k.Text(c, "No sessions yet.", 12, 16).TextColor(t.MutedForeground).Padding(k.Px(4), k.Px(8))
			}
			var recents, loose *sessions.Group
			var pinned, rest []sessions.Group
			for i := range groups {
				g := &groups[i]
				switch {
				case g.Scratch:
					recents = g
				case g.Key == sessions.PinnedSessionsGroupKey:
					loose = g
				case pins.Workspaces[g.Key]:
					pinned = append(pinned, *g)
				default:
					rest = append(rest, *g)
				}
			}
			section := func(g sessions.Group) { a.workspaceSection(c, g, groups, order, selected, busy) }
			if loose != nil || len(pinned) > 0 {
				all := pinned
				if loose != nil {
					all = append([]sessions.Group{*loose}, pinned...)
				}
				a.foldable(c, "Pinned", pinnedSectionKey, all, busy, func() {
					for _, g := range all {
						section(g)
					}
				})
			}
			if len(rest) > 0 && (loose != nil || len(pinned) > 0 || recents != nil) {
				a.foldable(c, "Workspaces", workspacesSectionKey, rest, busy, func() {
					for _, g := range rest {
						section(g)
					}
				})
			} else {
				for _, g := range rest {
					section(g)
				}
			}
			if recents != nil {
				section(*recents)
			}
			if more {
				b := ui.ButtonBase(c).Height(k.Px(28)).PaddingX(k.Px(8)).Gap(k.Px(6)).Radius(k.Px(8)).Justify(ui.Start).Margin(k.Px(4), 0, 0, 0)
				color := t.MutedForeground
				if b.Hovered() {
					b.Background(t.SidebarAccent.Alpha(0.6))
					color = t.Foreground
				}
				b.Children(func() {
					k.Icon(c, "chevron-down", 14, color)
					k.Text(c, "Load older sessions", 12, 16).TextColor(color)
				})
				if b.Clicked() {
					go a.loadOlder()
				}
			}
		})
		ui.Row(c).Padding(k.Px(4), k.Px(8), k.Px(8), k.Px(8)).Shrink(0).Children(func() {
			b := k.IconButton(c, "settings", "Settings", 32)
			if b.Clicked() {
				a.Go(Route{Name: "settings"})
			}
		})
	})
}

// activity is what each busy Session is doing, from the Daemon's reports
// and the Sessions this window has loaded.
func (a *App) activity(reported map[string]string) map[string]activity.Activity {
	loaded := map[string]string{}
	if a.ctl != nil {
		store := a.ctl.Store()
		for _, id := range store.SessionIDs() {
			if s := store.Session(id); s != nil && s.LoadState() == "LOADED" {
				loaded[id] = string(s.WorkingState())
			}
		}
	}
	compacting := map[string]bool{}
	for id, v := range a.views {
		if v.compacting {
			compacting[id] = true
		}
	}
	return activity.Snapshot(reported, loaded, compacting)
}

func (a *App) sortOrder(listed []sessions.Summary) sessions.Order {
	seen := prefs.SessionsFirstSeen.Get(a.prefs)
	if next := sessions.NoteFirstSeen(seen, listed, float64(a.cfg.Now().UnixMilli())); next != nil {
		prefs.SessionsFirstSeen.Set(a.prefs, next)
		seen = next
	}
	o := sessions.DefaultOrder
	if w := prefs.WorkspaceSort.Get(a.prefs); w != "" {
		o.Workspaces = sessions.WorkspaceSort(w)
	}
	if s := prefs.SessionSort.Get(a.prefs); s != "" {
		o.Sessions = sessions.SessionSort(s)
	}
	o.Manual = prefs.ManualWorkspaces.Get(a.prefs)
	o.FirstSeen = seen
	return o
}

// sidebarRow is a SidebarRow: an icon and a label, h-8, rounded-lg.
func (a *App) sidebarRow(c *ui.Context, icon, label string, selected bool) *ui.Element {
	k, t := a.kit, a.kit.T
	b := ui.ButtonBase(c).Label(label).Height(k.Px(32)).PaddingX(k.Px(8)).Gap(k.Px(8)).Radius(k.Px(10)).Justify(ui.Start).Cursor(ui.CursorPointer)
	kit.Selected(b, selected)
	switch {
	case selected:
		b.Background(t.SidebarAccent)
	case b.Hovered():
		b.Background(t.SidebarAccent.Alpha(0.6))
	}
	b.Children(func() {
		k.Icon(c, icon, 16, t.MutedForeground)
		k.Text(c, label, 13, 19.5).TextColor(t.Foreground)
	})
	return b
}

// sortMenu is the list-filter button and its menu of orders.
func (a *App) sortMenu(c *ui.Context, groups []sessions.Group) {
	k, t := a.kit, a.kit.T
	b := ui.ButtonBase(c).Label("Sort").Tooltip("Sort").Size(k.Px(32), k.Px(32)).Radius(k.Px(10)).Cursor(ui.CursorPointer)
	color := t.MutedForeground
	if b.Hovered() {
		b.Background(t.SidebarAccent.Alpha(0.6))
		color = t.Foreground
	}
	b.Children(func() { k.Icon(c, "list-filter", 16, color) })
	o := a.sortOrder(nil)
	b.Menu(func(m *ui.Menu) {
		m.Item("Sort workspaces").Disabled(true)
		for _, w := range sessions.WorkspaceSorts {
			if m.Item(sessions.WorkspaceSortLabels[w]).Checked(o.Workspaces == w).Chosen() {
				if w == sessions.SortManual && len(o.Manual) == 0 {
					var keys []string
					for _, g := range groups {
						if !g.Scratch && g.Key != sessions.PinnedSessionsGroupKey {
							keys = append(keys, g.Key)
						}
					}
					prefs.ManualWorkspaces.Set(a.prefs, keys)
				}
				prefs.WorkspaceSort.Set(a.prefs, string(w))
			}
		}
		m.Separator()
		m.Item("Sort sessions").Disabled(true)
		for _, s := range sessions.SessionSorts {
			if m.Item(sessions.SessionSortLabels[s]).Checked(o.Sessions == s).Chosen() {
				prefs.SessionSort.Set(a.prefs, string(s))
			}
		}
	})
}

// foldable is the Pinned or Workspaces section: a label row folding a run
// of Workspace groups.
func (a *App) foldable(c *ui.Context, label, key string, groups []sessions.Group, busy map[string]activity.Activity, fn func()) {
	open := !prefs.FoldedWorkspaces.Has(a.prefs, key)
	var list []sessions.Summary
	for _, g := range groups {
		list = append(list, g.Sessions...)
	}
	a.sectionHeader(c, label, "", open, list, busy, func() { prefs.FoldedWorkspaces.Toggle(a.prefs, key) }, nil)
	if open {
		ui.Column(c).Children(fn)
	}
}

// sectionHeader is a section's label row: a click folds it, the chevron
// shows on hover.
func (a *App) sectionHeader(c *ui.Context, label, title string, open bool, list []sessions.Summary, busy map[string]activity.Activity, toggle func(), action func(hovered bool)) {
	k, t := a.kit, a.kit.T
	row := ui.Row(c).Height(k.Px(28)).Margin(k.Px(4), 0, 0, 0).Radius(k.Px(10))
	hovered := row.Hovered()
	if hovered {
		row.Background(t.SidebarAccent.Alpha(0.6))
	}
	row.Children(func() {
		b := ui.ButtonBase(c).Grow(1).FillHeight().PaddingX(k.Px(8)).Gap(k.Px(4)).Justify(ui.Start).Radius(k.Px(10)).Expanded(open).Cursor(ui.CursorPointer)
		if title != "" {
			b.Tooltip(title)
		}
		color := t.MutedForeground.Alpha(0.8)
		if hovered {
			color = t.Foreground
		}
		b.Children(func() {
			k.Text(c, label, 11, 16.5).FontWeight(500).TextColor(color).SingleLine()
			rot := float32(0)
			if open {
				rot = 90
			}
			ch := k.Icon(c, "chevron-right", 12, color).Rotate(rot)
			if !hovered {
				ch.Opacity(0)
			}
			if !open {
				if n, ok := activity.CountBusy(list, busy, nil); ok {
					a.busyMark(c, n)
				}
			}
		})
		if b.Clicked() {
			toggle()
		}
		if action != nil {
			action(hovered)
		}
	})
}

// busyMark is a folded group's count of Sessions working or waiting.
func (a *App) busyMark(c *ui.Context, n activity.Busy) {
	k, t := a.kit, a.kit.T
	ui.Row(c).Role(ui.RoleStatus).Gap(k.Px(8)).PaddingX(k.Px(4)).Margin(0, 0, 0, ui.Auto).Children(func() {
		if n.NeedsInput > 0 {
			ui.Row(c).Gap(k.Px(4)).Children(func() {
				k.Icon(c, "circle-alert", 12, t.Attention)
				k.Text(c, itoa(n.NeedsInput), 11, 16.5).TextColor(t.Attention)
			})
		}
		if n.Working > 0 {
			ui.Row(c).Gap(k.Px(4)).Children(func() {
				k.Spinner(c, 12, t.Info)
				k.Text(c, itoa(n.Working), 11, 16.5).TextColor(t.Info)
			})
		}
	})
}

// workspaceSection is a Workspace's header and its Sessions.
func (a *App) workspaceSection(c *ui.Context, g sessions.Group, all []sessions.Group, order sessions.Order, selected string, busy map[string]activity.Activity) {
	k, t := a.kit, a.kit.T
	loose := g.Key == sessions.PinnedSessionsGroupKey
	flat := g.Scratch || loose
	open := loose || !prefs.FoldedWorkspaces.Has(a.prefs, g.Key)
	pinnedWS := prefs.PinnedWorkspaces.Has(a.prefs, g.Key)
	pinnedIDs := set(prefs.PinnedSessions.Get(a.prefs))
	newHere := func() {
		if g.Scratch {
			a.Go(Route{Name: "new", Scratch: true})
		} else {
			a.Go(Route{Name: "new", Workspace: g.Path})
		}
	}
	toggle := func() { prefs.FoldedWorkspaces.Toggle(a.prefs, g.Key) }
	newButton := func(shown bool) {
		b := ui.ButtonBase(c).Label("New session in "+g.Label).Tooltip("New session in "+g.Label).
			Size(k.Px(24), k.Px(24)).Radius(k.Px(8)).Margin(0, k.Px(4), 0, 0).Cursor(ui.CursorPointer)
		color := t.MutedForeground
		if b.Hovered() {
			b.Background(t.SidebarAccent)
			color = t.Foreground
		}
		if !shown && !b.Focused() {
			b.Opacity(0)
		}
		b.Children(func() { k.Icon(c, "square-pen", 14, color) })
		if b.Clicked() {
			newHere()
		}
	}
	manual := order.Workspaces == sessions.SortManual && !flat
	var shown []string
	if manual {
		for _, x := range all {
			if !x.Scratch && x.Key != sessions.PinnedSessionsGroupKey {
				shown = append(shown, x.Key)
			}
		}
	}
	menu := func(m *ui.Menu) {
		if manual {
			at := indexOf(shown, g.Key)
			if at >= 0 && len(shown) > 1 {
				if m.Item("Move up").Disabled(at == 0).Chosen() {
					prefs.ManualWorkspaces.Set(a.prefs, sessions.MoveWorkspace(shown, g.Key, shown[at-1], false))
				}
				if m.Item("Move down").Disabled(at == len(shown)-1).Chosen() {
					prefs.ManualWorkspaces.Set(a.prefs, sessions.MoveWorkspace(shown, g.Key, shown[at+1], true))
				}
				m.Separator()
			}
		}
		if !g.Scratch {
			label := "Pin workspace"
			if pinnedWS {
				label = "Unpin workspace"
			}
			if m.Item(label).Chosen() {
				prefs.PinnedWorkspaces.Toggle(a.prefs, g.Key)
			}
		}
		if m.Item("New session here").Chosen() {
			newHere()
		}
	}

	section := ui.Column(c).Key(g.Key).Role(ui.RoleGroup).Label(g.Label).Margin(0, 0, k.Px(8), 0)
	section.Children(func() {
		switch {
		case loose:
		case g.Scratch:
			wrap := ui.Column(c).ContextMenu(menu)
			wrap.Children(func() {
				a.sectionHeader(c, g.Label, "Sessions without a workspace", open, g.Sessions, busy, toggle, newButton)
			})
			wrap.Opacity(1)
		default:
			h := ui.Row(c).Role(ui.RoleHeading).Height(k.Px(32)).Radius(k.Px(10)).ContextMenu(menu)
			hovered := h.Hovered()
			if hovered {
				h.Background(t.SidebarAccent.Alpha(0.6))
			}
			h.Children(func() {
				b := ui.ButtonBase(c).Grow(1).MinWidth(0).FillHeight().PaddingX(k.Px(8)).Gap(k.Px(8)).Justify(ui.Start).Radius(k.Px(10)).
					Expanded(open).Tooltip(g.Path).Cursor(ui.CursorPointer)
				b.Children(func() {
					ui.Box(c).Size(k.Px(16), k.Px(16)).Shrink(0).Center().Children(func() {
						folder := "folder-open"
						if !open {
							folder = "folder"
						}
						if hovered {
							rot := float32(0)
							if open {
								rot = 90
							}
							k.Icon(c, "chevron-right", 14, t.MutedForeground).Rotate(rot)
						} else {
							k.IconStroke(c, folder, 1.75, 14, t.MutedForeground)
						}
					})
					k.Text(c, g.Label, 13, 19.5).FontWeight(500).TextColor(t.Foreground).SingleLine().Shrink(1)
					if !open {
						if n, ok := activity.CountBusy(g.Sessions, busy, nil); ok {
							a.busyMark(c, n)
						}
					}
				})
				if b.Clicked() {
					toggle()
				}
				newButton(hovered)
			})
		}
		if !open {
			return
		}
		if a.sidebar.revealed == nil {
			a.sidebar.revealed = map[string]int{}
		}
		visible, hidden := sessions.VisibleSessions(g.Sessions, a.sidebar.revealed[g.Key], a.cfg.Now().UnixMilli(), pinnedIDs)
		ui.Column(c).Gap(1).Children(func() {
			for _, s := range visible {
				a.sessionRow(c, s, flat, s.SessionID == selected, busy[s.SessionID], pinnedIDs[s.SessionID])
			}
		})
		if hidden > 0 {
			b := ui.ButtonBase(c).Height(k.Px(28)).Radius(k.Px(10)).Justify(ui.Start).Padding(0, k.Px(8), 0, k.Px(indent(flat))).Cursor(ui.CursorPointer)
			color := t.MutedForeground
			if b.Hovered() {
				b.Background(t.SidebarAccent.Alpha(0.6))
				color = t.Foreground
			}
			b.Children(func() { k.Text(c, "Show "+itoa(hidden)+" older", 11, 16.5).TextColor(color) })
			if b.Clicked() {
				a.sidebar.revealed[g.Key] += sessions.OlderBatch
			}
		}
	})
}

func indent(flat bool) float32 {
	if flat {
		return 8
	}
	return 32
}

// sessionRow is one Session in the sidebar: title, then what it is doing or
// its message count, and how long ago it changed.
func (a *App) sessionRow(c *ui.Context, s sessions.Summary, flat, selected bool, doing activity.Activity, pinned bool) {
	k, t := a.kit, a.kit.T
	if a.sidebar.renaming == s.SessionID {
		a.renameField(c, s, flat)
		return
	}
	b := ui.ButtonBase(c).Key(s.SessionID).Label(s.Title).Tooltip(s.Title).Column().AlignItems(ui.Stretch).Justify(ui.Start).
		Gap(k.Px(2)).Padding(k.Px(6), k.Px(8), k.Px(6), k.Px(indent(flat))).Radius(k.Px(10)).Cursor(ui.CursorPointer)
	kit.Selected(b, selected)
	switch {
	case selected:
		b.Background(t.SidebarAccent)
	case b.Hovered():
		b.Background(t.SidebarAccent.Alpha(0.6))
	}
	b.Children(func() {
		ui.Row(c).Gap(k.Px(6)).Children(func() {
			title := k.Text(c, s.Title, 13, 19.5).TextColor(t.Foreground).SingleLine().Grow(1).Shrink(1)
			if a.unread[s.SessionID] {
				title.FontWeight(500)
				kit.Dot(c, k.Px(6), t.Info).Role(ui.RoleImage).Label("Unread")
			}
			if pinned {
				k.Icon(c, "pin", 12, t.Foreground).Opacity(0.7).Label("Pinned")
			}
			if s.ArchivedAt != "" {
				k.Icon(c, "archive", 12, t.Foreground).Opacity(0.7).Label("Archived")
			}
		})
		ui.Row(c).Gap(k.Px(6)).Children(func() {
			status := func(icon string, color ui.Color, label string, spin bool) {
				ui.Row(c).Role(ui.RoleStatus).Label(label).Gap(k.Px(4)).Shrink(1).MinWidth(0).Children(func() {
					if spin {
						k.Spinner(c, 12, color)
					} else {
						k.Icon(c, icon, 12, color)
					}
					k.Text(c, label, 11, 16.5).TextColor(color).SingleLine()
				})
			}
			switch {
			case doing == activity.NeedsInput:
				status("circle-alert", t.Attention, "Needs input", false)
			case doing == activity.Compacting:
				status("", t.Info, "Compacting", true)
			case doing == activity.Working:
				status("", t.Info, "Working", true)
			case s.MessagesCount != nil:
				ui.Row(c).Gap(k.Px(4)).Shrink(1).MinWidth(0).Children(func() {
					k.Icon(c, "message-square", 12, t.MutedForeground)
					k.Text(c, plural(*s.MessagesCount, "message", "messages"), 11, 16.5).TextColor(t.MutedForeground).SingleLine()
				})
			}
			ui.Spacer(c)
			k.Text(c, relativeTime(time.Unix(s.UpdatedAt, 0), a.cfg.Now()), 11, 16.5).TextColor(t.MutedForeground).FontFeatures("tnum").Shrink(0)
		})
	})
	if b.Clicked() {
		a.Go(Route{Name: "session", SessionID: s.SessionID})
	}
	b.ContextMenu(func(m *ui.Menu) {
		if m.Item("Rename").Chosen() {
			a.sidebar.renaming, a.sidebar.rename = s.SessionID, s.Title
		}
		pin := "Pin"
		if pinned {
			pin = "Unpin"
		}
		if m.Item(pin).Chosen() {
			prefs.PinnedSessions.Toggle(a.prefs, s.SessionID)
		}
		m.Separator()
		if m.Item("Copy session ID").Chosen() {
			c.WriteClipboard(s.SessionID)
		}
		if m.Item("Copy session details").Chosen() {
			c.WriteClipboard(a.sessionDetails(s))
		}
		m.Separator()
		archive := "Archive"
		if s.ArchivedAt != "" {
			archive = "Unarchive"
		}
		if m.Item(archive).Chosen() {
			go a.toggleArchive(s)
		}
	})
}

// renameField is a row's title as a field: Enter saves, Escape keeps the title.
func (a *App) renameField(c *ui.Context, s sessions.Summary, flat bool) {
	k, t := a.kit, a.kit.T
	ui.Row(c).Key(s.SessionID).Radius(k.Px(10)).Background(t.SidebarAccent).Padding(k.Px(6), k.Px(8), k.Px(6), k.Px(indent(flat))).Children(func() {
		in := ui.TextInputBase(c, &a.sidebar.rename).Label("Session title").AutoFocus().Grow(1).Height(k.Px(32)).PaddingX(k.Px(6)).
			Radius(k.Px(8)).Border(1, t.Border).Background(t.Background).FontSize(k.Px(13)).TextColor(t.Foreground)
		done := func(save bool) {
			title := strings.TrimSpace(a.sidebar.rename)
			a.sidebar.renaming = ""
			if save && title != "" && title != s.Title {
				go a.renameSession(s.SessionID, title)
			}
		}
		switch {
		case in.Submitted():
			done(true)
		case in.Shortcut(0, ui.KeyEscape):
			done(false)
		}
	})
}

func (a *App) renameSession(id, title string) {
	cl, err := a.ctl.Client()
	if err != nil {
		return
	}
	if _, err := cl.RenameSession(a.ctx, protocol.RenameSessionParams{SessionID: id, Title: title}); err == nil {
		a.refreshList()
	}
}

func (a *App) toggleArchive(s sessions.Summary) {
	cl, err := a.ctl.Client()
	if err != nil {
		return
	}
	if s.ArchivedAt != "" {
		_, err = cl.UnarchiveSession(a.ctx, protocol.UnarchiveSessionParams{SessionID: s.SessionID})
	} else {
		_, err = cl.ArchiveSession(a.ctx, protocol.ArchiveSessionParams{SessionID: s.SessionID})
		if err == nil {
			a.cfg.Update(func() {
				if a.route.SessionID == s.SessionID {
					prefs.LastSessionID.Set(a.prefs, "")
					a.Go(Route{Name: "home"})
				}
			})
		}
	}
	if err == nil {
		a.refreshList()
	}
}

// skeleton stands in for the list while it loads.
func (a *App) skeleton(c *ui.Context) {
	k, t := a.kit, a.kit.T
	ui.Column(c).Gap(k.Px(8)).Padding(k.Px(4), k.Px(8)).Children(func() {
		for i := range 4 {
			ui.Box(c).Height(k.Px(24)).WidthPercent(float32(70 + (i%3)*10)).Radius(k.Px(8)).Background(t.SidebarAccent.Alpha(0.7))
		}
	})
}

func set(list []string) map[string]bool {
	m := make(map[string]bool, len(list))
	for _, s := range list {
		m[s] = true
	}
	return m
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}
