package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/activity"
	"github.com/kkkk2323/droi/apps/native/internal/archive"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
	"github.com/kkkk2323/droi/apps/native/pkg/subagents"
)

// Folded like a Workspace group, in the same preference; no Workspace key has
// these prefixes.
const (
	pinnedSectionKey     = "droi:pinned"
	workspacesSectionKey = "droi:workspaces"
	liveRunsSectionKey   = "droi:running-automations"
)

type sidebarState struct {
	query    string
	search   searchState
	revealed map[string]int
	renaming string
	rename   string
	// numbered are the Sessions the sidebar showed in the last frame, in
	// order, for ⌘1…⌘9.
	numbered []string
	// staleShown shows the Workspaces unused for a month.
	staleShown bool
	// subagentsRunning is how many subagents run under each listed row, this frame.
	subagentsRunning map[string]int
	runningKey       string
	// searchBox is the search's text field, for the composer to leave the focus in.
	searchBox ui.Handle
}

// sidebarView is the web Client's SessionSidebar.
func (a *App) sidebarView(c *ui.Context, listed []sessions.Summary, reported map[string]string, listErr string, listDone bool) {
	k, t := a.kit, a.kit.T
	pins := sessions.Pins{Workspaces: set(a.pinnedWorkspaces().Get(a.prefs)), Sessions: set(prefs.PinnedSessions.Get(a.prefs))}
	order := a.sortOrder(listed)
	groups := a.sidebarGroups(listed, pins, order)
	busy := a.activity(reported)
	a.noteSubagentsRunning(listed, busy)
	selected := ""
	if a.route.Name == "session" {
		selected = a.route.SessionID
	}

	ui.Column(c).Role(ui.RoleGroup).Label(L("Sessions")).Fill().Background(t.Sidebar).TextColor(t.SidebarForeground).Children(func() {
		ui.Box(c).Height(44).Shrink(0).DragWindow()
		a.sidebarHeader(c)
		ui.Column(c).Gap(k.Px(2)).Padding(k.Px(8), k.Px(8), k.Px(12), k.Px(8)).Shrink(0).Children(func() {
			if a.sidebarRow(c, "plus", L("New session"), false, nil).Tooltip(withShortcut(L("New session"), CmdNewSession)).Clicked() {
				a.Run(CmdNewSession)
			}
			live := a.automations.live
			if a.sidebarRow(c, "clock", L("Automations"), a.route.Name == "automations", func() {
				if n, ok := activity.CountBusy(live, busy, a.sidebar.subagentsRunning); ok {
					a.busyMark(c, n)
				}
			}).Clicked() {
				a.Go(Route{Name: "automations"})
			}
			ui.Row(c).Gap(k.Px(4)).Children(func() {
				a.searchBox(c)
				a.sortMenu(c, groups)
			})
		})
		ui.Scroll(c).Grow(1).Padding(0, k.Px(8), k.Px(8), k.Px(8)).Children(func() {
			if strings.TrimSpace(a.sidebar.query) != "" {
				a.searchResults(c, selected, groups, order, busy)
				return
			}
			if listErr != "" {
				k.Text(c, listErr, 12, 16).TextColor(t.DestructiveForeground).Padding(k.Px(4), k.Px(8))
			}
			if !listDone && len(groups) == 0 {
				a.skeleton(c)
			}
			if listDone && len(groups) == 0 && listErr == "" {
				k.Text(c, L("No sessions yet."), 12, 16).TextColor(t.MutedForeground).Padding(k.Px(4), k.Px(8))
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
			if live := a.automations.live; len(live) > 0 {
				a.foldable(c, L("Running automations"), liveRunsSectionKey, []sessions.Group{{Sessions: live}}, busy, func() {
					ui.Column(c).Role(ui.RoleGroup).Label(L("Running automations")).Gap(1).Margin(0, 0, k.Px(8), 0).Children(func() {
						for _, s := range live {
							a.sessionRow(c, s, true, s.SessionID == selected, busy[s.SessionID], false)
						}
					})
				})
			}
			section := func(g sessions.Group) { a.workspaceSection(c, g, groups, order, selected, busy) }
			active, stale := sessions.SplitStale(rest, a.cfg.Now().UnixMilli(), selected)
			restSections := func() {
				for _, g := range active {
					section(g)
				}
				if len(stale) == 0 {
					return
				}
				label, icon := L("Show %d older workspaces", len(stale)), "chevron-down"
				if len(stale) == 1 {
					label = L("Show 1 older workspace")
				}
				if a.sidebar.staleShown {
					label, icon = L("Hide older workspaces"), "chevron-up"
				}
				b := ui.ButtonBase(c).Label(label).Height(k.Px(28)).PaddingX(k.Px(8)).Gap(k.Px(6)).Radius(k.Px(8)).Justify(ui.Start).
					Expanded(a.sidebar.staleShown).Tooltip(L("Workspaces with no session in the last 30 days")).Cursor(ui.CursorPointer)
				color := t.MutedForeground
				if b.Hovered() {
					b.Background(t.SidebarAccent.Alpha(0.6))
					color = t.Foreground
				}
				b.Children(func() {
					k.Icon(c, icon, 14, color)
					k.Text(c, label, 12, 16).TextColor(color)
				})
				if b.Clicked() {
					a.sidebar.staleShown = !a.sidebar.staleShown
				}
				if a.sidebar.staleShown {
					for _, g := range stale {
						section(g)
					}
				}
			}
			if loose != nil || len(pinned) > 0 {
				all := pinned
				if loose != nil {
					all = append([]sessions.Group{*loose}, pinned...)
				}
				a.foldable(c, L("Pinned"), pinnedSectionKey, all, busy, func() {
					for _, g := range all {
						section(g)
					}
				})
			}
			if len(rest) > 0 && (loose != nil || len(pinned) > 0 || recents != nil) {
				a.foldable(c, L("Workspaces"), workspacesSectionKey, rest, busy, restSections)
			} else {
				restSections()
			}
			if recents != nil {
				section(*recents)
			}
		})
		ui.Row(c).Padding(k.Px(6), k.Px(8)).BorderWidth(1, 0, 0, 0).BorderColor(t.SidebarBorder).Shrink(0).Children(func() {
			b := a.sidebarRow(c, "settings", L("Settings"), false, nil).Grow(1).Tooltip(withShortcut(L("Settings"), CmdSettings))
			if b.Clicked() {
				a.Run(CmdSettings)
			}
		})
	})
}

// listMemo keeps what the views build from the list between frames: with
// thousands of Sessions, building it takes milliseconds, too long to do
// again for every frame of a spinner.
type listMemo struct {
	groupsKey string
	groups    []sessions.Group
	recentRev uint64
	recentOK  bool
	recent    []sessions.RecentWorkspace
	// firstSeen is decoded from firstSeenRaw, the stored sessionsFirstSeen,
	// which holds every Session ever listed: decoding it takes about a
	// millisecond with thousands of them.
	firstSeenRaw string
	firstSeen    map[string]float64
	// notedRev and notedRaw are the list and the stored sessionsFirstSeen
	// NoteFirstSeen last looked through, which takes a lookup per Session.
	notedRev uint64
	notedRaw string
	noted    bool
	// navKey names the list and the Session nav was found for, without its
	// runs, which change without the list.
	navKey string
	nav    subagentNav
}

// sidebarGroups is the sidebar's Workspace groups, built again only when
// the list, the pins, the order or the language changed.
func (a *App) sidebarGroups(listed []sessions.Summary, pins sessions.Pins, order sessions.Order) []sessions.Group {
	m := &a.listMemo
	key := fmt.Sprint(a.shownRev, l10n.Code(), a.pinnedWorkspaces().Get(a.prefs), prefs.PinnedSessions.Get(a.prefs), order.Workspaces, order.Sessions, order.Manual, len(order.FirstSeen))
	if m.groups == nil || m.groupsKey != key {
		m.groupsKey = key
		m.groups = sessions.GroupByWorkspace(sessions.FoldContinued(sessions.WithoutAutomationRuns(sessions.MainSessions(listed))), pins, order, true)
		if a.cfg.SessionList != nil && a.cfg.Computer == nil {
			a.cfg.SessionList.show(m.groups)
		}
	}
	return m.groups
}

// recentWorkspaces is sessions.RecentWorkspaces of the list, built again
// only when the list changed.
func (a *App) recentWorkspaces(listed []sessions.Summary) []sessions.RecentWorkspace {
	m := &a.listMemo
	if !m.recentOK || m.recentRev != a.shownRev {
		m.recentRev, m.recentOK = a.shownRev, true
		m.recent = sessions.RecentWorkspaces(listed)
	}
	return m.recent
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
		if v.Compacting() {
			compacting[id] = true
		}
	}
	return activity.Snapshot(reported, loaded, compacting)
}

// noteSubagentsRunning counts the running subagents under each listed row.
// A subagent runs by the Store's account of it or, when its caller is not
// loaded here, by the working state the Daemon reports for it. The counts
// are built again only when the list or the running subagents changed.
func (a *App) noteSubagentsRunning(listed []sessions.Summary, busy map[string]activity.Activity) {
	runs := map[string]subagents.Run{}
	var ids []string
	for _, s := range listed {
		if s.CallingSessionID == "" {
			continue
		}
		run, ok := a.subagentRun(s.SessionID)
		if !ok && busy[s.SessionID] != "" {
			run, ok = subagents.Run{Status: subagents.Running}, true
		}
		if ok && subagents.IsRunning(run.Status) {
			runs[s.SessionID] = run
			ids = append(ids, s.SessionID)
		}
	}
	key := fmt.Sprint(a.shownRev, ids)
	if a.sidebar.subagentsRunning != nil && a.sidebar.runningKey == key {
		return
	}
	a.sidebar.runningKey = key
	a.sidebar.subagentsRunning = subagents.RunningCounts(listed, runs)
}

// firstSeen is the stored sessionsFirstSeen, decoded again only when it
// changed. Callers must not change the map.
func (a *App) firstSeen() map[string]float64 {
	m := &a.listMemo
	raw, _ := a.prefs.Get(prefs.SessionsFirstSeen.Key)
	if m.firstSeen == nil || raw != m.firstSeenRaw {
		m.firstSeenRaw, m.firstSeen = raw, prefs.SessionsFirstSeen.Parse(raw)
	}
	return m.firstSeen
}

func (a *App) sortOrder(listed []sessions.Summary) sessions.Order {
	seen := a.firstSeen()
	if m := &a.listMemo; len(listed) > 0 && (!m.noted || m.notedRev != a.shownRev || m.notedRaw != m.firstSeenRaw) {
		if next := sessions.NoteFirstSeen(seen, listed, float64(a.cfg.Now().UnixMilli())); next != nil {
			prefs.SessionsFirstSeen.Set(a.prefs, next)
			seen = next
		} else {
			m.noted, m.notedRev, m.notedRaw = true, a.shownRev, m.firstSeenRaw
		}
	}
	o := sessions.DefaultOrder
	if w := prefs.WorkspaceSort.Get(a.prefs); w != "" {
		o.Workspaces = sessions.WorkspaceSort(w)
	}
	if s := prefs.SessionSort.Get(a.prefs); s != "" {
		o.Sessions = sessions.SessionSort(s)
	}
	o.Manual = a.manualWorkspaces().Get(a.prefs)
	o.FirstSeen = seen
	return o
}

// sidebarRow is a SidebarRow: an icon and a label, h-7, rounded-lg, as the
// reference's list rows.
func (a *App) sidebarRow(c *ui.Context, icon, label string, selected bool, trailing func()) ui.Element {
	k, t := a.kit, a.kit.T
	b := ui.ButtonBase(c).Label(label).Height(k.Px(k.Dense(28, 32))).PaddingX(k.Px(8)).Gap(k.Px(8)).Radius(k.Px(8)).Justify(ui.Start).Cursor(ui.CursorPointer)
	kit.Selected(b, selected)
	switch {
	case selected:
		b.Background(t.SidebarAccent)
	case b.Hovered():
		b.Background(t.SidebarAccent.Alpha(0.5))
	}
	b.Children(func() {
		k.IconStroke(c, icon, 1.6, 14, t.SidebarForeground)
		k.Text(c, label, k.Dense(12, 13), k.Dense(18, 19.5)).TextColor(t.SidebarForeground)
		if trailing != nil {
			trailing()
		}
	})
	return b
}

// sortMenu is the list-filter button and its menu of orders.
func (a *App) sortMenu(c *ui.Context, groups []sessions.Group) {
	k, t := a.kit, a.kit.T
	b := ui.ButtonBase(c).Label(L("Sort")).Tooltip(L("Sort")).Size(k.Px(k.Dense(28, 32)), k.Px(k.Dense(28, 32))).Radius(k.Px(8)).Cursor(ui.CursorPointer)
	color := t.MutedForeground
	if b.Hovered() {
		b.Background(t.SidebarAccent)
		color = t.Foreground
	}
	b.Children(func() { k.Icon(c, "list-filter", 16, color) })
	o := a.sortOrder(nil)
	b.Menu(func(m *ui.Menu) {
		m.Item(L("Sort workspaces")).Disabled(true)
		for _, w := range sessions.WorkspaceSorts {
			if m.Item(l10n.T(sessions.WorkspaceSortLabels[w])).Checked(o.Workspaces == w).Chosen() {
				if w == sessions.SortManual && len(o.Manual) == 0 {
					var keys []string
					for _, g := range groups {
						if !g.Scratch && g.Key != sessions.PinnedSessionsGroupKey {
							keys = append(keys, g.Key)
						}
					}
					a.manualWorkspaces().Set(a.prefs, keys)
				}
				prefs.WorkspaceSort.Set(a.prefs, string(w))
			}
		}
		m.Separator()
		m.Item(L("Sort sessions")).Disabled(true)
		for _, s := range sessions.SessionSorts {
			if m.Item(l10n.T(sessions.SessionSortLabels[s])).Checked(o.Sessions == s).Chosen() {
				prefs.SessionSort.Set(a.prefs, string(s))
			}
		}
	})
}

// foldable is the Pinned or Workspaces section: a label row folding a run
// of Workspace groups.
func (a *App) foldable(c *ui.Context, label, key string, groups []sessions.Group, busy map[string]activity.Activity, fn func()) {
	open := !a.foldedWorkspaces().Has(a.prefs, key)
	// Only a folded header counts its busy Sessions; the Workspaces section
	// holds nearly every Session, too many to gather on every frame.
	var list []sessions.Summary
	if !open {
		for _, g := range groups {
			list = append(list, g.Sessions...)
		}
	}
	a.sectionHeader(c, label, "", open, list, busy, func() { a.foldedWorkspaces().Toggle(a.prefs, key) }, nil)
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
		color := t.MutedForeground
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
				if n, ok := activity.CountBusy(list, busy, a.sidebar.subagentsRunning); ok {
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
	name := g.Label
	if g.Scratch {
		name = L("Recents")
	}
	open := loose || !a.foldedWorkspaces().Has(a.prefs, g.Key)
	pinnedWS := a.pinnedWorkspaces().Has(a.prefs, g.Key)
	pinnedIDs := set(prefs.PinnedSessions.Get(a.prefs))
	newHere := func() {
		if g.Scratch {
			a.Go(Route{Name: "new", Scratch: true})
		} else {
			a.Go(Route{Name: "new", Workspace: g.Path})
		}
	}
	toggle := func() { a.foldedWorkspaces().Toggle(a.prefs, g.Key) }
	newButton := func(shown bool) {
		b := ui.ButtonBase(c).Label(L("New session in %s", name)).Tooltip(L("New session in %s", name)).
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
				if m.Item(L("Move up")).Disabled(at == 0).Chosen() {
					a.manualWorkspaces().Set(a.prefs, sessions.MoveWorkspace(shown, g.Key, shown[at-1], false))
				}
				if m.Item(L("Move down")).Disabled(at == len(shown)-1).Chosen() {
					a.manualWorkspaces().Set(a.prefs, sessions.MoveWorkspace(shown, g.Key, shown[at+1], true))
				}
				m.Separator()
			}
		}
		if !g.Scratch {
			label := L("Pin workspace")
			if pinnedWS {
				label = L("Unpin workspace")
			}
			if m.Item(label).Chosen() {
				a.pinnedWorkspaces().Toggle(a.prefs, g.Key)
			}
		}
		if m.Item(L("New session here")).Chosen() {
			newHere()
		}
	}

	section := ui.Column(c.Key(g.Key)).Role(ui.RoleGroup).Label(name).Margin(0, 0, k.Px(12), 0)
	section.Children(func() {
		switch {
		case loose:
		case g.Scratch:
			wrap := ui.Column(c).ContextMenu(menu)
			wrap.Children(func() {
				a.sectionHeader(c, name, L("Sessions without a workspace"), open, g.Sessions, busy, toggle, newButton)
			})
			wrap.Opacity(1)
		default:
			h := ui.Row(c).Role(ui.RoleHeading).Height(k.Px(k.Dense(28, 32))).Radius(k.Px(8)).ContextMenu(menu)
			hovered := h.Hovered()
			if hovered {
				h.Background(t.SidebarAccent)
			}
			h.Children(func() {
				b := ui.ButtonBase(c).Grow(1).MinWidth(0).FillHeight().PaddingX(k.Px(8)).Gap(k.Px(8)).Justify(ui.Start).Radius(k.Px(8)).
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
					k.Text(c, g.Label, k.Dense(12, 13), k.Dense(18, 19.5)).FontWeight(500).TextColor(t.SidebarForeground).SingleLine().Shrink(1)
					if !open {
						if n, ok := activity.CountBusy(g.Sessions, busy, a.sidebar.subagentsRunning); ok {
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
			b.Children(func() { k.Text(c, L("Show %d older", hidden), 11, 16.5).TextColor(color) })
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
	return 12
}

// sessionRow is one Session in the sidebar: title, then whether it holds an
// unsent draft, what it is doing or its message count, and how long ago it
// changed.
func (a *App) sessionRow(c *ui.Context, s sessions.Summary, flat, selected bool, doing activity.Activity, pinned bool) {
	k, t := a.kit, a.kit.T
	if a.sidebar.renaming == s.SessionID {
		a.renameField(c, s, flat)
		return
	}
	a.sidebar.numbered = append(a.sidebar.numbered, s.SessionID)
	n := len(a.sidebar.numbered)
	b := ui.ButtonBase(c.Key(s.SessionID)).Label(s.Title).Tooltip(s.Title).Column().AlignItems(ui.Stretch).Justify(ui.Start).
		Gap(k.Px(2)).Padding(k.Px(6), k.Px(8), k.Px(6), k.Px(indent(flat))).Radius(k.Px(6)).Cursor(ui.CursorPointer)
	kit.Selected(b, selected)
	switch {
	case selected:
		b.Background(t.SidebarAccent)
	case b.Hovered():
		b.Background(t.SidebarAccent.Alpha(0.5))
	}
	acc, elsewhere := a.elsewhere.accounts[s.SessionID]
	b.Children(func() {
		ui.Row(c).Gap(k.Px(6)).Children(func() {
			titleColor := t.Foreground
			if elsewhere {
				titleColor = t.MutedForeground
			}
			title := k.Text(c, s.Title, k.Dense(12, 13), k.Dense(18, 19.5)).TextColor(titleColor).SingleLine().Grow(1).Shrink(1)
			if elsewhere {
				a.elsewhereMark(c, acc)
			}
			if a.unread[s.SessionID] {
				title.FontWeight(500)
				kit.Dot(c, k.Px(6), t.Info).Role(ui.RoleImage).Label(L("Unread"))
			}
			if pinned {
				k.Icon(c, "pin", 12, t.Foreground).Opacity(0.7).Label(L("Pinned"))
			}
			if s.ArchivedAt != "" {
				k.Icon(c, "archive", 12, t.Foreground).Opacity(0.7).Label(L("Archived"))
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
			if !selected && a.drafts.Has(s.SessionID) {
				ui.Row(c).Role(ui.RoleStatus).Label(L("Draft")).Gap(k.Px(4)).Shrink(0).Children(func() {
					k.Icon(c, "pencil", 12, t.Foreground)
					k.Text(c, L("Draft"), 11, 16.5).FontWeight(500).TextColor(t.Foreground).SingleLine()
				})
			}
			switch {
			case doing == activity.NeedsInput:
				status("circle-alert", t.Attention, L("Needs input"), false)
			case doing == activity.Compacting:
				status("", t.Info, L("Compacting"), true)
			case a.sidebar.subagentsRunning[s.SessionID] > 0:
				status("", t.Info, countL(a.sidebar.subagentsRunning[s.SessionID], l10n.N("%d subagent running"), l10n.N("%d subagents running")), true)
			case doing == activity.Working:
				status("", t.Info, L("Working"), true)
			case a.jobsRunning(s.SessionID) > 0:
				status("", t.Info, countL(a.jobsRunning(s.SessionID), l10n.N("%d background task running"), l10n.N("%d background tasks running")), true)
			case s.Worktree != nil && s.Worktree.Branch != "":
				label := L("Worktree on %s", s.Worktree.Branch)
				if s.Worktree.Ephemeral() {
					label = L("Ephemeral worktree on %s", s.Worktree.Branch)
				}
				ui.Row(c).Role(ui.RoleImage).Label(label).Tooltip(label).Gap(k.Px(4)).Shrink(1).MinWidth(0).Children(func() {
					k.Icon(c, "git-branch", 12, t.MutedForeground)
					k.Text(c, s.Worktree.Branch, 11, 16.5).TextColor(t.MutedForeground).SingleLine()
				})
			case s.MessagesCount != nil:
				ui.Row(c).Gap(k.Px(4)).Shrink(1).MinWidth(0).Children(func() {
					k.Icon(c, "message-square", 12, t.MutedForeground)
					k.Text(c, plural(*s.MessagesCount, L("message"), L("messages")), k.Dense(10, 11), k.Dense(15, 16.5)).TextColor(t.SidebarMutedForeground).SingleLine()
				})
			}
			ui.Spacer(c)
			if n <= 9 && c.Modifiers() == ui.Cmd {
				k.Text(c, shortcut(sessionCommand(n)), 11, k.Dense(15, 16.5)).TextColor(t.Foreground).FontFeatures("tnum").Shrink(0)
			} else {
				k.Text(c, relativeTime(time.Unix(s.UpdatedAt, 0), a.cfg.Now()), k.Dense(10, 11), k.Dense(15, 16.5)).TextColor(t.SidebarMutedForeground).FontFeatures("tnum").Shrink(0)
			}
		})
	})
	if b.Clicked() {
		a.Go(Route{Name: "session", SessionID: s.SessionID})
	}
	if elsewhere {
		// The Daemon runs as another account and cannot act on it.
		b.ContextMenu(func(m *ui.Menu) {
			if m.Item(L("Switch to %s", accountWho(acc))).Chosen() {
				a.askSwitchFor(s.SessionID)
			}
			m.Separator()
			if m.Item(L("Copy session ID")).Chosen() {
				c.WriteClipboard(s.SessionID)
			}
		})
		return
	}
	b.ContextMenu(func(m *ui.Menu) {
		if m.Item(L("Rename")).Chosen() {
			a.sidebar.renaming, a.sidebar.rename = s.SessionID, s.Title
		}
		pin := L("Pin")
		if pinned {
			pin = L("Unpin")
		}
		if m.Item(pin).Chosen() {
			prefs.PinnedSessions.Toggle(a.prefs, s.SessionID)
		}
		m.Separator()
		if m.Item(L("Copy session ID")).Chosen() {
			c.WriteClipboard(s.SessionID)
		}
		if m.Item(L("Copy session details")).Chosen() {
			a.copySessionDetails(s)
		}
		m.Separator()
		archive := L("Archive")
		if s.ArchivedAt != "" {
			archive = L("Unarchive")
		}
		if m.Item(archive).Chosen() {
			if s.ArchivedAt == "" && s.Worktree.Ephemeral() {
				a.askArchive(s)
			} else {
				go a.toggleArchive(s)
			}
		}
	})
}

// renameField is a row's title as a field: Enter saves, Escape keeps the title.
func (a *App) renameField(c *ui.Context, s sessions.Summary, flat bool) {
	k, t := a.kit, a.kit.T
	ui.Row(c.Key(s.SessionID)).Radius(k.Px(10)).Background(t.SidebarAccent).Padding(k.Px(6), k.Px(8), k.Px(6), k.Px(indent(flat))).Children(func() {
		in := ui.TextInputBase(c, &a.sidebar.rename).Label(L("Session title")).AutoFocus().Grow(1).Height(k.Px(32)).PaddingX(k.Px(6)).
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
		a.reloadList()
	}
}

func (a *App) toggleArchive(s sessions.Summary) { _ = a.archive(s) }

// archive archives the Session, or unarchives an archived one. A Scratch
// Session's conversation goes with its folder, through the Gateway.
func (a *App) archive(s sessions.Summary) error {
	cl, err := a.ctl.Client()
	if err != nil {
		return err
	}
	a.mu.Lock()
	listed := append([]sessions.Summary(nil), a.listed...)
	a.mu.Unlock()
	var scratch archive.Scratch = noScratch{}
	if a.cfg.Gateway != nil {
		scratch = a.cfg.Gateway
	}
	if s.ArchivedAt != "" {
		err = archive.Unarchive(a.ctx, cl, scratch, listed, s)
	} else {
		err = archive.Conversation(a.ctx, cl, scratch, listed, s)
		if err == nil {
			a.cfg.Update(func() {
				if a.route.SessionID == s.SessionID {
					prefs.LastSessionID.Set(a.prefs, "")
					a.Go(Route{Name: "home"})
				}
			})
		}
	}
	a.reloadList()
	return err
}

// noScratch is a window without a Gateway: it has no folders to move.
type noScratch struct{}

func (noScratch) TrashScratch(context.Context, string) error   { return nil }
func (noScratch) RestoreScratch(context.Context, string) error { return nil }

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
