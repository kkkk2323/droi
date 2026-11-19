package sessionview

import (
	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

// wakeEntry is what woke the Session while no one spoke to it, where it
// woke: one quiet row saying what woke it, which opens to what the Daemon
// told Droid and the calls a background Script made meanwhile.
func (v *View) wakeEntry(c *ui.Context, e *transcript.Entry) {
	k, t := v.look()
	b := e.Blocks[0]
	label, detail := transcript.WakeLabel(b)
	icon := "braces"
	if n := len(b.Notices); n > 0 {
		icon = "bell"
		if b.Notices[n-1].Kind == transcript.CommandEnded {
			icon = "terminal"
		}
	}
	fg := t.MutedForeground
	if transcript.WakeFailed(b) {
		fg = t.DestructiveForeground
	}
	when := transcript.FormatTimestamp(e.CreatedAt, v.env.Now())
	line := func(chevron func()) {
		k.Icon(c, icon, 14, fg)
		k.Text(c, label, 12.5, 18.75).FontWeight(500).TextColor(fg).Shrink(0)
		k.Text(c, detail, 11.5, 17.25).Font(k.Mono).TextColor(t.MutedForeground).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		k.Text(c, when, 11, 16.5).TextColor(t.MutedForeground).FontFeatures("tnum").Shrink(0)
		if chevron != nil {
			chevron()
		}
	}
	v.column(c).Role(ui.RoleGroup).Label(label).Padding(k.Px(12), k.Px(24), k.Px(4), k.Px(24)).Children(func() {
		if !wakeHasDetail(b) {
			ui.Row(c).FillWidth().Height(k.Px(28)).Gap(k.Px(8)).PaddingX(k.Px(6)).Margin(0, 0, 0, -k.Px(6)).Children(func() { line(nil) })
			return
		}
		open := v.flag("wake:"+e.ID, false)
		name := label
		if detail != "" {
			name += ": " + detail
		}
		p := v.rowTrigger(c, open, name)
		hover := p.Trigger.Hovered() || p.Trigger.Focused()
		p.Trigger.Children(func() {
			line(func() {
				ch := k.Icon(c, "chevron-right", 14, t.MutedForeground).Rotate(90 * p.Progress())
				if !hover && !*open && p.Progress() == 0 {
					ch.Opacity(0)
				}
			})
		})
		p.Panel(func() { v.wakeDetail(c, b) })
	})
}

func wakeHasDetail(b transcript.Block) bool {
	if len(b.Calls) > 0 {
		return true
	}
	for _, n := range b.Notices {
		if n.Text != "" {
			return true
		}
	}
	return false
}

// wakeDetail is what the Daemon told Droid, and the calls the Script made.
func (v *View) wakeDetail(c *ui.Context, b transcript.Block) {
	k, t := v.look()
	ui.Column(c).Gap(k.Px(6)).Margin(k.Px(4), 0, k.Px(4), 0).Children(func() {
		for _, n := range b.Notices {
			if n.Text == "" {
				continue
			}
			label := l10n.L("Notification")
			if n.Kind == transcript.CommandEnded {
				label = l10n.L("Output")
			}
			ui.Column(c.Key(n.ID)).Role(ui.RoleGroup).Label(label).Radius(k.Px(6)).Border(1, t.Border).Background(t.Card.Alpha(0.6)).Clip().Children(func() {
				ui.Scroll(c).MaxHeight(k.Px(240)).Children(func() {
					k.Text(c, n.Text, 11.5, 20).Font(k.Mono).TextColor(t.MutedForeground).Selectable().Padding(k.Px(6), k.Px(10))
				})
			})
		}
		if len(b.Calls) == 0 {
			return
		}
		ui.Column(c).Margin(0, 0, 0, k.Px(6)).BorderWidth(0, 0, 0, 1).BorderColor(t.Border).Padding(0, 0, 0, k.Px(12)).Gap(k.Px(2)).Children(func() {
			for _, call := range b.Calls {
				ui.Column(c.Key(call.Use.ID)).Children(func() { v.toolRow(c, call) })
			}
		})
	})
}
