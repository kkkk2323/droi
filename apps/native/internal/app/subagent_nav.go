package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/l10n"
	"github.com/kkkk2323/droi/apps/native/internal/sessions"
	"github.com/kkkk2323/droi/apps/native/internal/subagents"
)

// subagentNav is what the header knows of the Session's place among
// subagents: the Sessions above it (for a subagent), the subagents of the
// same caller, and those it called itself.
type subagentNav struct {
	trail    []subagents.SessionRef
	siblings []sessions.Summary
	called   []sessions.Summary
	runs     map[string]subagents.Run
}

func (a *App) subagentNavOf(listed []sessions.Summary, sel *sessions.Summary, id string) subagentNav {
	var n subagentNav
	// A subagent just started is in the Store before the list has it.
	if (sel == nil || sel.CallingSessionID == "") && a.ctl != nil {
		if h := a.ctl.Store().Session(id); h != nil {
			if caller, toolUse := h.CallingSession(); caller != "" {
				s := sessions.Summary{SessionID: id, Title: h.Title()}
				if sel != nil {
					s = *sel
				}
				s.CallingSessionID, s.CallingToolUseID = caller, toolUse
				sel = &s
			}
		}
	}
	if sel == nil {
		return n
	}
	if sel.CallingSessionID != "" {
		n.trail = subagents.CallerTrail(listed, id, sel.CallingSessionID)
		n.siblings = subagents.Siblings(listed, sel.CallingSessionID)
	}
	callers := []string{id}
	for _, s := range sessions.ContinuationChain(listed, *sel) {
		callers = append(callers, s.SessionID)
	}
	n.called = subagents.Of(listed, callers)
	if len(n.siblings)+len(n.called) > 0 {
		n.runs = a.subagentRuns(listed)
	}
	return n
}

// subagentMark is a subagent's state in a menu row: a spinner while it
// runs, a check once done, a dashed circle otherwise.
func (a *App) subagentMark(c *ui.Context, run subagents.Run, ok bool) {
	k, t := a.kit, a.kit.T
	switch {
	case ok && subagents.IsRunning(run.Status):
		k.Spinner(c, 14, t.Info).Role(ui.RoleImage).Label(L("Running"))
	case ok && run.Status == subagents.Completed:
		k.Icon(c, "check", 14, t.Success).Role(ui.RoleImage).Label(L("Completed"))
	default:
		k.Icon(c, "circle-dashed", 14, t.MutedForeground)
	}
}

// subagentItem is a SubagentRow inside a menu: its state, title and age.
func (a *App) subagentItem(c *ui.Context, open *bool, s sessions.Summary, run subagents.Run, ok, checked bool) bool {
	k, t := a.kit, a.kit.T
	b := ui.ButtonBase(c.Key(s.SessionID)).Role(ui.RoleMenuItem).Label(s.Title).Checked(checked).Gap(k.Px(8)).
		Padding(k.Px(6), k.Px(8)).Radius(k.Px(6)).Justify(ui.Start).Cursor(ui.CursorPointer)
	color := t.PopoverForeground
	if b.Hovered() || b.Focused() {
		b.Background(t.Accent)
		color = t.AccentForeground
	}
	b.Children(func() {
		a.subagentMark(c, run, ok)
		k.Text(c, trimOr(s.Title, L("Untitled session")), 14, 20).TextColor(color).SingleLine().Grow(1).MinWidth(0)
		k.Text(c, relativeTime(time.Unix(s.UpdatedAt, 0), a.cfg.Now()), 12, 16).TextColor(t.MutedForeground).FontFeatures("tnum").Shrink(0)
		if checked {
			k.Icon(c, "check", 14, color)
		}
	})
	if b.Clicked() {
		*open = false
		return true
	}
	return false
}

func (a *App) subagentPopup(c *ui.Context, anchor ui.Element, open *bool, end bool, fn func()) {
	k, t := a.kit, a.kit.T
	ui.PopoverBase(c, anchor, open, func(p ui.Element) {
		w, h := c.Size()
		if end {
			p.AttachTo(anchor, ui.AnchorBottomRight, ui.AnchorTopRight)
		} else {
			p.AttachTo(anchor, ui.AnchorBottomLeft, ui.AnchorTopLeft)
		}
		p.Role(ui.RoleMenu).Label(L("Subagents")).Margin(k.Px(6), 0, 0, 0).Width(min(k.Px(320), w-k.Px(16))).MaxHeight(min(k.Px(384), h-k.Px(60))).Clip().
			Padding(k.Px(4)).Radius(k.Px(8)).Border(1, t.Border).Background(t.Popover).TextColor(t.PopoverForeground).
			Shadow(0, k.Px(10), k.Px(15), -k.Px(3), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).FillWidth().Children(fn)
	})
}

// subagentMenu is the header's way into the Session's subagents: how many,
// how many still running.
func (v *sessionView) subagentMenu(c *ui.Context, n subagentNav) {
	a := v.a
	k, t := a.kit, a.kit.T
	if len(n.called) == 0 {
		return
	}
	running := 0
	for _, s := range n.called {
		if r, ok := n.runs[s.SessionID]; ok && subagents.IsRunning(r.Status) {
			running++
		}
	}
	count := countL(len(n.called), l10n.N("%d subagent"), l10n.N("%d subagents"))
	label := count
	if running > 0 {
		label = L("%s, %d running", count, running)
	}
	b := ui.ButtonBase(c).Role(ui.RoleMenuButton).Label(label).Height(k.Px(28)).Gap(k.Px(6)).PaddingX(k.Px(8)).Radius(k.Px(6)).
		Cursor(ui.CursorPointer).Expanded(v.subOpen)
	color := t.MutedForeground
	if b.Hovered() || v.subOpen {
		b.Background(t.Accent)
		color = t.Foreground
	}
	b.Children(func() {
		if running > 0 {
			k.Spinner(c, 14, t.Info)
		} else {
			k.Icon(c, "bot", 14, color)
		}
		k.Text(c, count, 12, 16).TextColor(color).SingleLine()
		k.Icon(c, "chevron-down", 12, color)
	})
	if b.Clicked() {
		v.subOpen = !v.subOpen
	}
	a.subagentPopup(c, b, &v.subOpen, true, func() {
		for _, s := range n.called {
			run, ok := n.runs[s.SessionID]
			if a.subagentItem(c, &v.subOpen, s, run, ok, false) {
				a.Go(Route{Name: "session", SessionID: s.SessionID})
			}
		}
	})
}

// sessionTrail is a subagent's title as a trail: the Sessions above it
// lead back, and the last crumb switches to another subagent of the same
// caller.
func (v *sessionView) sessionTrail(c *ui.Context, n subagentNav, title string) {
	a := v.a
	k, t := a.kit, a.kit.T
	ui.Row(c).Role(ui.RoleGroup).Label(L("Session hierarchy")).MinWidth(0).Gap(k.Px(2)).Children(func() {
		for _, crumb := range n.trail {
			ui.Row(c.Key(crumb.SessionID)).MinWidth(0).Shrink(1).Gap(k.Px(2)).Children(func() {
				b := ui.ButtonBase(c).Role(ui.RoleButton).Label(crumb.Title).MaxWidth(k.Px(192)).Padding(k.Px(2), k.Px(6)).Radius(k.Px(6)).
					Shrink(1).MinWidth(0).Cursor(ui.CursorPointer)
				color := t.MutedForeground
				if b.Hovered() {
					b.Background(t.Accent)
					color = t.Foreground
				}
				b.Children(func() { k.Text(c, crumb.Title, 14, 20).FontWeight(500).TextColor(color).SingleLine() })
				if b.Clicked() {
					a.Go(Route{Name: "session", SessionID: crumb.SessionID})
				}
				k.Icon(c, "chevron-right", 14, t.MutedForeground.Alpha(0.6))
			})
		}
		b := ui.ButtonBase(c).Role(ui.RoleMenuButton).Label(title).Gap(k.Px(4)).Padding(k.Px(2), k.Px(6)).Radius(k.Px(6)).MinWidth(0).
			Cursor(ui.CursorPointer).Expanded(v.trailOpen)
		if b.Hovered() || v.trailOpen {
			b.Background(t.Accent)
		}
		b.Children(func() {
			k.Text(c, title, 14, 20).Role(ui.RoleHeading).FontWeight(500).TextColor(t.Foreground).SingleLine().Shrink(1)
			k.Icon(c, "chevron-down", 14, t.MutedForeground)
		})
		if b.Clicked() {
			v.trailOpen = !v.trailOpen
		}
		a.subagentPopup(c, b, &v.trailOpen, false, func() {
			for _, s := range n.siblings {
				run, ok := n.runs[s.SessionID]
				if a.subagentItem(c, &v.trailOpen, s, run, ok, s.SessionID == v.id) && s.SessionID != v.id {
					a.Go(Route{Name: "session", SessionID: s.SessionID})
				}
			}
		})
	})
}
