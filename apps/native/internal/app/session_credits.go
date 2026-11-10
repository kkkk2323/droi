package app

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"
)

type observedTurnCredits struct {
	own, children *float64
	at            time.Time
}

// creditValue keeps a reported zero distinct from a missing field. Do not
// derive Session credits from its current model: it may have used others.
func creditValue(u *protocol.TokenUsage) *float64 {
	if u == nil || u.FactoryCredits == nil {
		return nil
	}
	n := *u.FactoryCredits
	if n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	return &n
}

// creditsMeter reads the Daemon's counters independently of model-speed
// measurements and the Key Pool. Its totals are lifetime Session figures,
// never substituted for a request or a filtered period in Usage.
func (v *sessionView) creditsMeter(c *ui.Context, s *session.Session) {
	u := s.Usage()
	own, inclusive := creditValue(u.Session), creditValue(u.Inclusive)
	total := inclusive
	if total == nil {
		total = own
	}
	turn := v.turnCredits
	if total == nil && turn.own == nil && turn.children == nil {
		return
	}
	k, t := v.a.kit, v.a.kit.T
	label := L("Credits")
	if total != nil {
		label = L("%s Credits", groupDigits(*total))
	}
	b := ui.ButtonBase(c).Label(L("Session credits")).Height(k.Px(24)).PaddingX(k.Px(6)).Radius(k.Px(6)).Shrink(0).
		Cursor(ui.CursorPointer).Expanded(v.creditsOpen)
	color := t.MutedForeground
	if b.Hovered() || v.creditsOpen {
		b.Background(t.Muted)
		color = t.Foreground
	}
	b.Children(func() {
		k.Text(c, label, 12, 16).TextColor(color).FontFeatures("tnum").SingleLine()
	})
	if b.Clicked() {
		v.creditsOpen = !v.creditsOpen
	}
	ui.PopoverBase(c, b, &v.creditsOpen, func(p ui.Element) {
		p.AttachTo(b, ui.AnchorTopRight, ui.AnchorBottomRight).Margin(0, 0, k.Px(6), 0).
			Role(ui.RoleDialog).Label(L("Session credits")).Width(k.Px(300)).Padding(k.Px(10), k.Px(12)).Radius(k.Px(8)).
			Border(1, t.Border).Background(t.Popover).TextColor(t.PopoverForeground).
			Shadow(0, k.Px(10), k.Px(15), -k.Px(3), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).FillWidth().Gap(k.Px(6)).Children(func() {
			row := func(label string, value *float64) {
				if value == nil {
					return
				}
				ui.Row(c).FillWidth().Gap(k.Px(8)).Children(func() {
					k.Text(c, label, 13, 18).SingleLine().Grow(1).MinWidth(0)
					k.Text(c, groupDigits(*value), 13, 18).FontFeatures("tnum").SingleLine().Shrink(0)
				})
			}
			row(L("This Session"), own)
			row(L("Including subagents"), inclusive)
			row(L("Last observed turn"), turn.own)
			row(L("Subagents in that turn"), turn.children)
			if turn.own != nil || turn.children != nil {
				k.Text(c, L("Turn observed %s", turn.at.Local().Format("2006-01-02 15:04:05")), 12, 16).TextColor(t.MutedForeground)
			}
			k.Text(c, L("Reported by the Daemon."), 12, 16).TextColor(t.MutedForeground)
			k.Text(c, L("Session totals cover its full history. Usage shows separate request estimates."), 12, 16).TextColor(t.MutedForeground)
		})
	})
}
