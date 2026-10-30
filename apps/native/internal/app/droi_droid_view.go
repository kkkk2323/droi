package app

import (
	"runtime"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/keypool"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// droiDroidGroup is Settings → Advanced's Droi's droid (ADR 0033): the droid
// the Daemon runs, and the Key Pool its LLM requests are sent with.
func (a *App) droiDroidGroup(c *ui.Context) {
	h := a.cfg.Host
	h.RefreshDroiDroid()
	p := h.DroiDroidState()
	rows := []func(){func() { a.droiDroidRow(c, p) }}
	if p.Enabled || a.keyPool().Len() > 0 {
		rows = append(rows, func() { a.keyPoolRow(c, p) })
	}
	if p.Enabled && a.cfg.MenuBar != nil {
		rows = append(rows, func() { a.menuBarRow(c) })
	}
	a.settingGroup(c, L("Key Pool"), "", rows...)
}

func (a *App) droiDroidRow(c *ui.Context, p host.DroiDroid) {
	h := a.cfg.Host
	k, t := a.kit, a.kit.T
	desc := L("The Key Pool is not available on this computer yet. It works on macOS and 64-bit Windows for now.")
	switch {
	case p.Enabled && p.Version != "":
		desc = L("The Daemon runs Droi's droid %s. Turning this off restarts the Daemon and interrupts running sessions.", p.Version)
	case p.Supported && p.Latest != "":
		desc = L("Turning this on prepares Droi's droid %s (the first time, a download of about 300 MB) and restarts the Daemon, which interrupts running sessions.", p.Latest)
	case p.Supported:
		desc = L("Turning this on prepares Droi's droid (the first time, a download of about 300 MB) and restarts the Daemon, which interrupts running sessions.")
	}
	label := L("Use the Key Pool")
	a.settingRow(c, label, desc, func() {
		if p.Preparing {
			k.Spinner(c, 16, t.MutedForeground)
			return
		}
		if a.toggle(c, label, p.Enabled, !p.Supported) {
			if p.Enabled {
				h.DisableDroiDroid()
			} else {
				h.EnableDroiDroid()
			}
		}
	}, func() {
		if p.Preparing {
			a.droiDroidProgress(c, p)
		}
		if p.Err != "" {
			k.Text(c, p.Err, 12, 18).Role(ui.RoleStatus).TextColor(t.DestructiveForeground).Margin(k.Px(8), 0, 0, 0)
		}
		if p.UpdateAvailable() {
			ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Margin(k.Px(8), 0, 0, 0).Children(func() {
				k.Text(c, L("Droi's droid %s is available; you have %s. Updating restarts the Daemon.", p.Latest, p.Version), 12, 18).TextColor(t.MutedForeground).Shrink(1).MinWidth(0)
				if a.smallButton(c, kit.Outline, L("Update"), "refresh-cw", p.Preparing).Label(L("Update Droi's droid")).Clicked() {
					h.UpdateDroiDroid()
				}
			})
		}
	})
}

// droiDroidAboutRow is About's row for Droi's droid while the Key Pool is
// on: Check for updates asks Factory for its newest droid at once, without
// waiting for the hourly check, and Update moves to the newer one.
func (a *App) droiDroidAboutRow(c *ui.Context, p host.DroiDroid) {
	h := a.cfg.Host
	k, t := a.kit, a.kit.T
	desc := ""
	if p.UpdateAvailable() {
		desc = L("Updating restarts the Daemon, which interrupts running sessions.")
	}
	a.settingRow(c, L("Droi's droid %s", p.Version), desc, func() {
		ui.Row(c).Gap(k.Px(12)).AlignItems(ui.Center).Children(func() {
			status, color := "", t.MutedForeground
			switch {
			case p.Preparing:
			case p.Checking:
				status = L("Checking…")
			case p.CheckErr != "":
				status, color = p.CheckErr, t.DestructiveForeground
			case p.UpdateAvailable():
				status = L("%s is available", p.Latest)
			case p.Checked:
				status = L("Up to date")
			}
			if status != "" {
				k.Text(c, status, 12, 16).Role(ui.RoleStatus).TextColor(color).MaxWidth(k.Px(260)).MaxLines(2)
			}
			if p.UpdateAvailable() {
				if a.smallButton(c, kit.Primary, L("Update to %s", p.Latest), "arrow-down-to-line", p.Preparing).Label(L("Update Droi's droid")).Clicked() {
					h.UpdateDroiDroid()
				}
				return
			}
			if a.smallButton(c, kit.Outline, L("Check for updates"), "", p.Checking || p.Preparing).Label(L("Check for Droi's droid updates")).Clicked() {
				h.CheckDroiDroid()
			}
		})
	}, func() {
		if p.Preparing {
			a.droiDroidProgress(c, p)
		}
		if p.Err != "" {
			k.Text(c, p.Err, 12, 18).Role(ui.RoleStatus).TextColor(t.DestructiveForeground).Margin(k.Px(8), 0, 0, 0)
		}
	})
}

// droiDroidProgress is the download and check under way: a bar of the bytes
// fetched, or a line while the size is unknown.
func (a *App) droiDroidProgress(c *ui.Context, p host.DroiDroid) {
	k, t := a.kit, a.kit.T
	if p.Total <= 0 {
		k.Text(c, L("Preparing Droi's droid…"), 12, 18).Role(ui.RoleStatus).TextColor(t.MutedForeground).Margin(k.Px(8), 0, 0, 0)
		return
	}
	share := float32(float64(p.Done) / float64(p.Total) * 100)
	ui.Column(c).Role(ui.RoleMeter).Label(L("Downloading droid: %s of %s", llmusage.FormatBytes(p.Done), llmusage.FormatBytes(p.Total))).
		Gap(k.Px(6)).Margin(k.Px(8), 0, 0, 0).Children(func() {
		k.Text(c, L("Downloading droid: %s of %s", llmusage.FormatBytes(p.Done), llmusage.FormatBytes(p.Total)), 12, 18).TextColor(t.MutedForeground)
		ui.Box(c).FillWidth().Height(k.Px(4)).Radius(k.Px(2)).Background(t.Foreground.Alpha(0.08)).Clip().Children(func() {
			fill := ui.Box(c).FillHeight().Radius(k.Px(2)).Background(t.Primary)
			fill.WidthPercent(min(fill.Animate("droid", share, 240*time.Millisecond), 100))
		})
	})
}

// menuBarRow turns the Key Pool's menu bar item on and off.
func (a *App) menuBarRow(c *ui.Context) {
	label := L("Show usage in the menu bar")
	desc := L("The menu bar shows how much monthly quota the keys have left. Click it to see today's use, the Standard and Core pools, and each key.")
	if runtime.GOOS == "windows" {
		label = L("Show usage in the notification area")
		desc = L("The notification area of the taskbar shows the Key Pool's icon. Click it to see the monthly quota left, today's use, the Standard and Core pools, and each key.")
	}
	a.settingRow(c, label, desc, func() {
		on := prefs.MenuBarUsage.Get(a.prefs)
		if a.toggle(c, label, on, false) {
			prefs.MenuBarUsage.Set(a.prefs, !on)
		}
	}, nil)
}

// keyPoolRow is the Key Pool in Settings: how many keys it holds, which one
// droid is sending with, and the way to the Usage page, where keys are
// added and switched.
func (a *App) keyPoolRow(c *ui.Context, p host.DroiDroid) {
	k, t := a.kit, a.kit.T
	st := a.poolState()
	desc := L("Add and switch keys, and see their quota, on the Usage tab.")
	if len(st.Keys) == 0 {
		desc = L("The Key Pool has no key yet. The Daemon starts once you add one on the Usage tab.")
	}
	a.settingRow(c, L("Keys"), desc, func() {
		a.statusPill(c, len(st.Keys) > 0, countL(len(st.Keys), l10n.N("%d key"), l10n.N("%d keys")))
		if a.smallButton(c, kit.Outline, L("Open Usage"), "", false).Clicked() {
			a.route.Tab = "usage"
		}
	}, func() {
		if len(st.Keys) == 0 {
			return
		}
		current := L("No key in use yet")
		for _, key := range st.Keys {
			if key.Current {
				current = L("In use: %s", key.Masked)
			}
		}
		mode := L("Switches keys automatically")
		if st.Mode == keypool.Manual {
			mode = L("Keeps the key you chose")
		}
		k.Text(c, current+" · "+mode, 12, 18).TextColor(t.MutedForeground).Margin(k.Px(8), 0, 0, 0)
	})
}
