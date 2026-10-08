package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/internal/kit"
)

// startingUp is the Daemon coming up: never connected yet, and not given
// up on, as the web Client's isStartingUp.
func startingUp(status controller.Status, everConnected bool) bool {
	return !status.Connected && !everConnected
}

// droidUpdatedCard is the corner card once droid updated itself while the
// Daemon kept running the earlier build. Closing it hides it until the next
// update.
func (a *App) droidUpdatedCard(c *ui.Context) {
	h := a.cfg.Host
	if h == nil || !h.DroidUpdated() {
		a.droidUpdateDismissed = false
		return
	}
	if a.droidUpdateDismissed {
		return
	}
	k, t := a.kit, a.kit.T
	a.cornerCard(c, L("droid update"), "refresh-cw", func() {
		k.Text(c, L("droid was updated"), 14, 20).FontWeight(500)
		k.Text(c, L("Restart the Daemon to use the new version. Working Sessions are interrupted."), 12, 16).TextColor(t.MutedForeground).Margin(k.Px(2), 0, 0, 0)
		if a.smallButton(c, kit.Primary, L("Restart Daemon"), "", false).AlignSelf(ui.Start).Margin(k.Px(8), 0, 0, 0).Clicked() {
			go h.RestartDaemon()
		}
	}, func() { a.droidUpdateDismissed = true })
}

// cornerCards stack the corner cards above the sidebar's footer, whose
// Settings button a card would cover.
func (a *App) cornerCards(c *ui.Context) {
	k := a.kit
	ui.Column(c).Absolute().Left(k.Px(16)).Bottom(k.Px(48)).Gap(k.Px(8)).Children(func() {
		a.droidUpdatedCard(c)
		a.memoryFullCard(c)
		a.updateCard(c)
	})
}

// cornerCard is one card of the corner: an icon, its body and Dismiss.
func (a *App) cornerCard(c *ui.Context, label, icon string, body func(), dismiss func()) {
	k, t := a.kit, a.kit.T
	ui.Row(c).Role(ui.RoleStatus).Label(label).Width(k.Px(288)).AlignItems(ui.Start).Gap(k.Px(10)).
		Padding(k.Px(12)).Radius(k.Px(8)).Border(1, t.Border).Background(t.Popover).TextColor(t.PopoverForeground).
		Shadow(0, k.Px(10), k.Px(15), -k.Px(3), ui.RGBA(0, 0, 0, 0.1)).Children(func() {
		k.Icon(c, icon, 16, t.MutedForeground).Margin(k.Px(2), 0, 0, 0)
		ui.Column(c).Grow(1).MinWidth(0).Children(body)
		if k.IconButton(c, "x", L("Dismiss"), 24).Clicked() {
			dismiss()
		}
	})
}

// startingUpView is the quiet start: a breathing dot and one line where the
// content will appear, instead of a warning for a Daemon not up yet.
func (a *App) startingUpView(c *ui.Context) {
	k, t := a.kit, a.kit.T
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(k.Px(44)).Shrink(0).PaddingX(k.Px(8)).AlignItems(ui.Center).DragWindow().Children(func() {
			if a.narrow {
				a.openSessionsButton(c)
			}
		})
		ui.Row(c).Grow(1).Center().Children(func() {
			ui.Row(c).Role(ui.RoleStatus).Label(L("Starting")).Gap(k.Px(10)).AlignItems(ui.Center).Children(func() {
				wave := float32(pulseWave(c))
				kit.Dot(c, k.Px(8), t.MutedForeground.Alpha(0.4+0.3*wave))
				k.Text(c, L("Starting the Daemon"), 14, 20).TextColor(t.MutedForeground)
			})
		})
	})
}
