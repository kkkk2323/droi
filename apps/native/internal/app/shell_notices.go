package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
)

// startingUp is the Daemon coming up: never connected yet, and not given
// up on, as the web Client's isStartingUp.
func startingUp(status controller.Status, everConnected bool) bool {
	return !status.Connected && !everConnected
}

// startupProblem is why the window still waits for its first connection:
// a Daemon that does not start, or one the window cannot connect to. Empty
// while the Daemon is only coming up.
func startupProblem(daemon host.DaemonState, status controller.Status) (title, detail string) {
	switch {
	case daemon.DroidMissing():
		return "droid was not found", "Install the droid CLI, or set the path to droid in Settings."
	case daemon.Reason != "":
		return "The Daemon did not start", "Last attempt: " + daemon.Reason + "."
	case status.Failure != nil && status.Failure.Reason == controller.ReasonAuthRejected:
		return "The Daemon did not accept the login", "Sign out and sign in again in Settings, then restart the Daemon. (" + status.Failure.Error() + ")"
	case status.Failure != nil:
		return "Droi could not connect to the Daemon", status.Failure.Error()
	}
	return "", ""
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
func (a *App) startingUpView(c *ui.Context, status controller.Status) {
	k, t := a.kit, a.kit.T
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(k.Px(44)).Shrink(0).PaddingX(k.Px(8)).AlignItems(ui.Center).DragWindow().Children(func() {
			if a.narrow {
				a.openSessionsButton(c)
			}
		})
		var title, detail string
		if h := a.cfg.Host; h != nil {
			title, detail = startupProblem(h.Daemon.State(), status)
		}
		if title != "" {
			a.startupProblemView(c, title, detail)
			return
		}
		ui.Row(c).Grow(1).Center().Children(func() {
			ui.Row(c).Role(ui.RoleStatus).Label(L("Starting")).Gap(k.Px(10)).AlignItems(ui.Center).Children(func() {
				wave := float32(pulseWave(c))
				kit.Dot(c, k.Px(8), t.MutedForeground.Alpha(0.4+0.3*wave))
				k.Text(c, L("Starting the Daemon"), 14, 20).TextColor(t.MutedForeground)
			})
		})
	})
}

// startupProblemView says why Droid is not there yet, with what can fix it.
func (a *App) startupProblemView(c *ui.Context, title, detail string) {
	h := a.cfg.Host
	k, t := a.kit, a.kit.T
	ui.Column(c).Grow(1).Center().PaddingX(k.Px(24)).Children(func() {
		ui.Column(c).Role(ui.RoleStatus).Label(title).FillWidth().MaxWidth(k.Px(420)).AlignItems(ui.Center).Gap(k.Px(8)).Children(func() {
			k.Icon(c, "circle-alert", 20, t.Attention)
			k.Text(c, title, 14, 20).FontWeight(500).TextAlign(ui.Center)
			k.Text(c, detail, 13, 18).TextColor(t.MutedForeground).TextAlign(ui.Center)
			ui.Row(c).Gap(k.Px(8)).Margin(k.Px(8), 0, 0, 0).Children(func() {
				if a.smallButton(c, kit.Outline, "Restart Daemon", "refresh-cw", false).Clicked() {
					go h.RestartDaemon()
				}
				if a.smallButton(c, kit.Ghost, "Settings", "settings", false).Clicked() {
					a.Go(Route{Name: "settings", Tab: "advanced"})
				}
			})
		})
	})
}
