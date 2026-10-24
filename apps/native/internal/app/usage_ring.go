package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
	"github.com/kkkk2323/droi/apps/native/pkg/dashkit"
)

// keyPoolRing sits beside Settings while the Key Pool serves the Daemon: a
// ring filled to the share of the monthly quota the keys have left, the
// menu bar item's figure, as Synara's rail draws a provider's quota. A click
// opens Settings → Usage.
func (a *App) keyPoolRing(c *ui.Context) {
	if !a.usageTabShown() {
		return
	}
	k, t := a.kit, a.kit.T
	remaining, ok := llmusage.MonthlyRemaining(keyStates(a.poolState()), a.cfg.Now())
	tip := L("Open Usage")
	color := t.Success
	switch {
	case !ok:
		color = t.Border
	case remaining < llmusage.DangerRemaining:
		color = t.Destructive
	case remaining < llmusage.WarningRemaining:
		color = t.Attention
	}
	if ok {
		tip = L("Key Pool: %s of the monthly quota left", llmusage.FormatPercent(remaining))
	}
	size := k.Px(k.Dense(28, 32))
	b := ui.ButtonBase(c).Label(L("Open Usage")).Tooltip(tip).Size(size, size).Radius(k.Px(8)).Shrink(0).Cursor(ui.CursorPointer)
	icon := t.SidebarMutedForeground
	if b.Hovered() {
		b.Background(t.SidebarAccent.Alpha(0.5))
		icon = t.SidebarForeground
	}
	b.Children(func() {
		ring := ui.Box(c).Size(k.Px(20), k.Px(20)).Center()
		ring.Draw(func(p *ui.Painter, r ui.Rect) {
			s := r.W / 20
			dashkit.DrawRing(p, r.X+10*s, r.Y+10*s, 8.75*s, 2.5*s, remaining/100, t.Border, color)
		})
		ring.Children(func() { k.IconStroke(c, "key-round", 1.6, 10, icon) })
	})
	if b.Clicked() {
		a.OpenUsage()
	}
}
