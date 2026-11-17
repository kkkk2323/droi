package app

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/activity"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

const workstreamsSectionKey = "droi:workstreams"

// workstreamsSection is the sidebar's Workstreams: one row for each, which is
// its orchestrator's Session, under a title that folds them and starts a
// new one. Their agents are in the page of the Workstream.
func (a *App) workstreamsSection(c *ui.Context, listed []sessions.Summary, selected string, busy map[string]activity.Activity) {
	if !prefs.ShowWorkstreams.Get(a.prefs) {
		return
	}
	live := a.liveOrchestrators(listed)
	if (a.cfg.Gateway == nil || a.workstreams.older) && len(live) == 0 {
		return
	}
	k := a.kit
	open := !a.foldedWorkspaces().Has(a.prefs, workstreamsSectionKey)
	sorted := a.sortedByUse(live)
	ui.Column(c.Key("workstreams")).Role(ui.RoleGroup).Label(L("Workstreams")).Margin(0, 0, k.Px(8), 0).Children(func() {
		a.workstreamsHeader(c, open, sorted, busy)
		if !open {
			return
		}
		if len(sorted) == 0 {
			if b := a.sidebarRow(c, "plus", L("New Workstream"), false, nil); b.Clicked() {
				a.openNewWorkstream()
			}
			return
		}
		ui.Column(c).Gap(1).Children(func() {
			for _, s := range sorted {
				a.workstreamRow(c, s, s.SessionID == selected, busy[s.SessionID])
			}
		})
	})
}

// liveOrchestrators is the orchestrators of the Workstreams not archived,
// found again only when the list changed: a frame comes 24 times a second
// while a spinner turns.
func (a *App) liveOrchestrators(listed []sessions.Summary) []sessions.Summary {
	m := &a.listMemo
	if !m.leadsSet || m.leadsRev != a.shownRev {
		m.leadsSet, m.leadsRev, m.leads = true, a.shownRev, nil
		for _, s := range sessions.FoldContinued(sessions.WithoutAutomationRuns(sessions.Orchestrators(sessions.MainSessions(listed)))) {
			if s.ArchivedAt == "" {
				m.leads = append(m.leads, s)
			}
		}
	}
	return m.leads
}

// sortedByUse is list with the Session used last first.
func (a *App) sortedByUse(list []sessions.Summary) []sessions.Summary {
	out := append([]sessions.Summary(nil), list...)
	slices.SortStableFunc(out, func(x, y sessions.Summary) int {
		return cmp.Compare(sessions.UsedAt(y, a.sidebar.times), sessions.UsedAt(x, a.sidebar.times))
	})
	return out
}

// workstreamsHeader is the Workstreams' title row: the name, the Beta mark,
// and the button that starts one.
func (a *App) workstreamsHeader(c *ui.Context, open bool, list []sessions.Summary, busy map[string]activity.Activity) {
	k, t := a.kit, a.kit.T
	row := ui.Row(c).Height(k.Px(28)).Margin(k.Px(4), 0, 0, 0).Radius(k.Px(10))
	hovered := row.Hovered()
	if hovered {
		row.Background(t.SidebarAccent.Alpha(0.6))
	}
	row.Children(func() {
		b := ui.ButtonBase(c).Label(L("Workstreams")).Grow(1).FillHeight().PaddingX(k.Px(8)).Gap(k.Px(6)).Justify(ui.Start).Radius(k.Px(10)).Expanded(open).Cursor(ui.CursorPointer)
		color := t.MutedForeground
		if hovered {
			color = t.Foreground
		}
		b.Children(func() {
			k.Text(c, L("Workstreams"), 11, 16.5).FontWeight(500).TextColor(color).SingleLine()
			ui.Row(c).Height(k.Px(16)).PaddingX(k.Px(5)).AlignItems(ui.Center).Radius(k.Px(4)).Border(1, t.SidebarBorder).Children(func() {
				k.Text(c, L("Beta"), 10, 14).FontWeight(500).TextColor(color)
			})
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
			a.foldedWorkspaces().Toggle(a.prefs, workstreamsSectionKey)
		}
		add := ui.ButtonBase(c).Label(L("New Workstream")).Tooltip(L("New Workstream")).Size(k.Px(24), k.Px(24)).Radius(k.Px(8)).Margin(0, k.Px(4), 0, 0).Cursor(ui.CursorPointer)
		addColor := t.MutedForeground
		if add.Hovered() {
			add.Background(t.SidebarAccent)
			addColor = t.Foreground
		}
		add.Children(func() { k.Icon(c, "plus", 14, addColor) })
		if add.Clicked() {
			a.openNewWorkstream()
		}
	})
}

// workstreamRow is a Workstream in the sidebar: its symbol, its name and when
// it was last used, and under them how many of its agents wait on the user
// or work.
func (a *App) workstreamRow(c *ui.Context, s sessions.Summary, selected bool, doing activity.Activity) {
	k, t := a.kit, a.kit.T
	id, _, _ := sessions.Workstream(s.Tags)
	if id != "" && a.workstreams.renaming == id {
		a.workstreamRenameField(c, s, id)
		return
	}
	a.sidebar.numbered = append(a.sidebar.numbered, s.SessionID)
	n := len(a.sidebar.numbered)
	canEdit := !a.following() && a.cfg.Gateway != nil && !a.workstreams.older && id != ""
	sym := sessions.WorkstreamSymbol(s.Tags)
	symOpen := canEdit && a.workstreams.symbolFor == id

	row := ui.Row(c.Key(s.SessionID)).Padding(k.Px(6), k.Px(8), k.Px(6), k.Px(4)).Gap(k.Px(4)).AlignItems(ui.Start).Radius(k.Px(6))
	switch {
	case selected:
		row.Background(t.SidebarAccent)
	case row.Hovered():
		row.Background(t.SidebarAccent.Alpha(0.5))
	}
	row.Children(func() {
		symBtn := ui.ButtonBase(c).Label(L("Change symbol for %s", s.Title)).Tooltip(L("Workstream symbol")).
			Expanded(symOpen).Disabled(!canEdit).Size(k.Px(22), k.Px(22)).Radius(k.Px(6)).Center()
		if canEdit {
			symBtn.Cursor(ui.CursorPointer)
			if symOpen || symBtn.Hovered() {
				symBtn.Background(t.SidebarAccent)
			}
		}
		symBtn.Children(func() {
			k.IconStroke(c, sym, 1.6, 14, t.SidebarForeground).Role(ui.RoleImage).Label(L("Workstream"))
		})
		if canEdit && symBtn.Clicked() {
			if symOpen {
				a.workstreams.symbolFor = ""
			} else {
				a.workstreams.symbolFor = id
			}
		}
		if symOpen {
			open := true
			ui.PopoverBase(c, symBtn, &open, func(p ui.Element) {
				a.symbolGrid(c, p, sym, func(picked string) {
					open = false
					a.workstreams.symbolFor = ""
					if picked != sym {
						a.editWorkstream(id, nil, &picked)
					}
				})
			})
			if !open {
				a.workstreams.symbolFor = ""
			}
		}

		b := ui.ButtonBase(c).Label(s.Title).Tooltip(s.Title).Column().AlignItems(ui.Stretch).Justify(ui.Start).
			Grow(1).MinWidth(0).Gap(k.Px(2)).Cursor(ui.CursorPointer)
		kit.Selected(b, selected)
		b.Children(func() {
			ui.Row(c).Gap(k.Px(6)).AlignItems(ui.Center).MinHeight(k.Px(22)).Children(func() {
				title := k.Text(c, s.Title, k.Dense(12, 13), k.Dense(18, 19.5)).TextColor(t.Foreground).SingleLine().Grow(1).Shrink(1)
				if a.sidebar.unread[s.SessionID] {
					title.FontWeight(500)
					kit.Dot(c, k.Px(6), t.Info).Role(ui.RoleImage).Label(L("Unread"))
				}
				if n <= 9 && c.Modifiers() == ui.Cmd {
					k.Text(c, shortcut(sessionCommand(n)), 11, k.Dense(15, 16.5)).TextColor(t.Foreground).FontFeatures("tnum").Shrink(0)
				} else {
					k.Text(c, relativeTime(time.UnixMilli(sessions.UsedAt(s, a.sidebar.times)), a.cfg.Now()), k.Dense(10, 11), k.Dense(15, 16.5)).
						TextColor(t.SidebarMutedForeground).FontFeatures("tnum").Shrink(0)
				}
			})
			a.rowStatus(c, s, selected, doing, 8)
		})
		if b.Clicked() {
			a.Go(Route{Name: "session", SessionID: s.SessionID})
		}
	})
	row.ContextMenu(func(m *ui.Menu) {
		if canEdit && m.Item(L("Rename")).Chosen() {
			a.workstreams.renaming, a.workstreams.rename = id, s.Title
		}
		if m.Item(L("Workstream settings")).Chosen() {
			a.openWorkstreamSettings(id)
		}
		if m.Item(L("Archive Workstream…")).Chosen() {
			a.workstreams.archiveFor = id
		}
		m.Separator()
		if m.Item(L("Copy session ID")).Chosen() {
			c.WriteClipboard(s.SessionID)
		}
	})
}

// workstreamRenameField edits a Workstream's title in place: Enter saves, Escape cancels.
func (a *App) workstreamRenameField(c *ui.Context, s sessions.Summary, id string) {
	k, t := a.kit, a.kit.T
	ui.Row(c.Key(s.SessionID)).Radius(k.Px(8)).Background(t.SidebarAccent).Padding(k.Px(6), k.Px(8), k.Px(6), k.Px(8)).Children(func() {
		in := ui.TextInputBase(c, &a.workstreams.rename).Label(L("Workstream name")).AutoFocus().Grow(1).Height(k.Px(30)).PaddingX(k.Px(6)).
			Radius(k.Px(6)).Border(1, t.Border).Background(t.Background).FontSize(k.Px(13)).TextColor(t.Foreground)
		done := func(save bool) {
			title := strings.TrimSpace(a.workstreams.rename)
			a.workstreams.renaming = ""
			if save && title != "" && title != s.Title {
				a.editWorkstream(id, &title, nil)
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

// rowStatus is the line under a Workstream's name: how many of its agents
// wait on the user or work, else what the orchestrator itself does. It
// draws nothing when there is nothing to say.
func (a *App) rowStatus(c *ui.Context, s sessions.Summary, selected bool, doing activity.Activity, indent float32) {
	k, t := a.kit, a.kit.T
	status := func(icon string, color ui.Color, label string, spin bool) {
		ui.Row(c).Role(ui.RoleStatus).Label(label).Gap(k.Px(4)).Margin(0, 0, 0, k.Px(indent-8)).Shrink(1).MinWidth(0).Children(func() {
			if spin {
				k.Spinner(c, 12, color)
			} else {
				k.Icon(c, icon, 12, color)
			}
			k.Text(c, label, 11, 16.5).TextColor(color).SingleLine()
		})
	}
	lifted, waiting, isLifted := a.liftedStatus(s.SessionID)
	if _, ok := a.workstreams.lifted[s.SessionID]; ok && !isLifted {
		// An orchestrator's own state wins over its agents'.
		doing = a.workstreams.busy[s.SessionID]
	}
	switch {
	case isLifted && waiting:
		status("circle-alert", t.Attention, lifted, false)
	case isLifted:
		status("", t.Info, lifted, true)
	case doing == activity.NeedsInput:
		status("circle-alert", t.Attention, L("Needs input"), false)
	case doing == activity.Compacting:
		status("", t.Info, L("Compacting"), true)
	case doing == activity.Working:
		status("", t.Info, L("Working"), true)
	case a.jobsRunning(s.SessionID) > 0:
		status("", t.Info, countL(a.jobsRunning(s.SessionID), "%d background task running", "%d background tasks running"), true)
	}
}
