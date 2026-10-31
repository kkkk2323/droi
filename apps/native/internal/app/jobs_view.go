package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

// jobsMenu is the header's count of the Session's background commands
// still running; it opens to them, each to the last lines it wrote while
// the Daemon runs here.
func (v *sessionView) jobsMenu(c *ui.Context) {
	k, t := v.a.kit, v.a.kit.T
	jobs := v.RunningCommands()
	if len(jobs) == 0 {
		v.jobsOpen = false
		return
	}
	label := countL(len(jobs), l10n.N("%d background task running"), l10n.N("%d background tasks running"))
	b := ui.ButtonBase(c).Role(ui.RoleMenuButton).Label(label).Height(k.Px(28)).Gap(k.Px(6)).PaddingX(k.Px(8)).Radius(k.Px(6)).
		Cursor(ui.CursorPointer).Expanded(v.jobsOpen)
	color := t.MutedForeground
	if b.Hovered() || v.jobsOpen {
		b.Background(t.Accent)
		color = t.Foreground
	}
	b.Children(func() {
		k.Spinner(c, 14, t.Info)
		k.Text(c, countL(len(jobs), l10n.N("%d background task"), l10n.N("%d background tasks")), 12, 16).TextColor(color).SingleLine()
		k.Icon(c, "chevron-down", 12, color)
	})
	if b.Clicked() {
		v.jobsOpen = !v.jobsOpen
	}
	ui.PopoverBase(c, b, &v.jobsOpen, func(p ui.Element) {
		p.AttachTo(b, ui.AnchorBottomRight, ui.AnchorTopRight).Margin(k.Px(6), 0, 0, 0).
			Role(ui.RoleDialog).Label(L("Background tasks")).Width(k.Px(440)).Padding(k.Px(4)).Radius(k.Px(8)).
			Border(1, t.Border).Background(t.Popover).TextColor(t.PopoverForeground).
			Shadow(0, k.Px(10), k.Px(15), -k.Px(3), ui.RGBA(0, 0, 0, 0.1))
		ui.Scroll(c).FillWidth().MaxHeight(k.Px(480)).Children(func() { v.jobList(c, jobs) })
	})
}

// jobList is the popover's tasks, each with how long it has run.
func (v *sessionView) jobList(c *ui.Context, jobs []transcript.Job) {
	k, t := v.a.kit, v.a.kit.T
	c.After(time.Second)
	row := func(r ui.Element, j transcript.Job, chevron func()) {
		r.Height(k.Px(30)).Gap(k.Px(8)).Padding(0, k.Px(2), 0, k.Px(8)).Radius(k.Px(6)).Children(func() {
			k.Spinner(c, 12, t.Info)
			k.Text(c, j.Summary, 12.5, 18.75).TextColor(t.Foreground).Tooltip(j.Command).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			k.Text(c, v.JobState(j), 11, 16.5).TextColor(t.MutedForeground).FontFeatures("tnum").SingleLine().Shrink(0)
			if chevron != nil {
				chevron()
			} else {
				ui.Box(c).Size(k.Px(24), k.Px(24)).Shrink(0)
			}
		})
	}
	// stop sits beside the row, not in it: the row may be the button that
	// opens the output.
	stop := func(j transcript.Job) {
		if j.ProcessID == "" {
			return
		}
		if v.Stopping(j) {
			ui.Box(c).Size(k.Px(24), k.Px(24)).Center().Shrink(0).Children(func() { k.Spinner(c, 12, t.MutedForeground) })
			return
		}
		if k.IconButton(c, "square", L("Stop %s", j.Summary), 24).Tooltip(L("Stop")).Clicked() {
			v.StopJob(j)
		}
	}
	ui.Column(c).Role(ui.RoleList).Label(L("Background tasks")).FillWidth().Children(func() {
		for _, j := range jobs {
			c := c.Key(j.ID)
			if !v.ShowsOutput(j) {
				ui.Row(c).FillWidth().Gap(k.Px(2)).Children(func() {
					row(ui.Row(c).Grow(1).Shrink(1).MinWidth(0), j, nil)
					stop(j)
				})
				continue
			}
			open := v.Flag("job:"+j.ID, false)
			var p ui.CollapsibleParts
			ui.Row(c).FillWidth().Gap(k.Px(2)).Children(func() {
				p = ui.CollapsibleBase(c, open)
				tr := p.Trigger.Label(L("Output of %s", j.Summary)).Grow(1).Shrink(1).MinWidth(0).Cursor(ui.CursorPointer)
				if tr.Hovered() {
					tr.Background(t.Accent)
				}
				row(tr, j, func() {
					ui.Box(c).Size(k.Px(24), k.Px(24)).Center().Shrink(0).Children(func() {
						k.Icon(c, "chevron-down", 12, t.MutedForeground).Rotate(180 * p.Progress())
					})
				})
				stop(j)
			})
			p.Panel(func() {
				ui.Column(c).Margin(k.Px(2), 0, k.Px(4), 0).Radius(k.Px(6)).Border(1, t.Border).Background(t.Card.Alpha(0.6)).Clip().
					Children(func() { v.JobOutput(c, j) })
			})
		}
	})
}

// jobsRunning counts a Session's background commands still running, as
// far as a view this window keeps of it knows.
func (a *App) jobsRunning(id string) int {
	if v := a.views[id]; v != nil {
		return len(v.RunningCommands())
	}
	return 0
}
