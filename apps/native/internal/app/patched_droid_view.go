package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/keypool"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// patchedDroidGroup is Settings → Advanced's Patched Droid (ADR 0027): the
// droid the Daemon runs, and the Key Pool its LLM requests are sent with.
func (a *App) patchedDroidGroup(c *ui.Context) {
	h := a.cfg.Host
	h.RefreshPatchRecipes()
	p := h.PatchedDroidState()
	rows := []func(){func() { a.patchedDroidRow(c, p) }}
	if p.Enabled || a.keyPool().Len() > 0 {
		rows = append(rows, func() { a.keyPoolRow(c, p) })
	}
	a.settingGroup(c, L("Patched Droid"),
		L("Droi downloads Factory's own droid of a version it has a recipe for, checks its SHA-256, and patches the one place that reads the LLM key, so each request can be sent with a key from the Key Pool. Your Factory login and everything else about droid stay as they are."),
		rows...)
}

func (a *App) patchedDroidRow(c *ui.Context, p host.PatchedDroid) {
	h := a.cfg.Host
	k, t := a.kit, a.kit.T
	desc := L("No recipe fits this computer yet, so Droi cannot patch droid here.")
	switch {
	case p.Enabled && p.Version != "":
		desc = L("The Daemon runs the Patched Droid, droid %s.", p.Version)
	case p.Supported && p.Latest != "":
		desc = L("Droi can patch droid %s for this computer. The Daemon restarts, which stops working sessions.", p.Latest)
	case p.Supported:
		desc = L("Droi can patch droid for this computer.")
	}
	label := L("Use the Patched Droid")
	a.settingRow(c, L("Patched Droid"), desc, func() {
		if p.Preparing {
			k.Spinner(c, 16, t.MutedForeground)
			return
		}
		if a.toggle(c, label, p.Enabled, !p.Supported) {
			if p.Enabled {
				h.DisablePatchedDroid()
			} else {
				h.EnablePatchedDroid()
			}
		}
	}, func() {
		if p.Preparing {
			a.patchProgress(c, p)
		}
		if p.Err != "" {
			k.Text(c, p.Err, 12, 18).Role(ui.RoleStatus).TextColor(t.DestructiveForeground).Margin(k.Px(8), 0, 0, 0)
		}
		if !p.UpdateAvailable() {
			return
		}
		ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Margin(k.Px(8), 0, 0, 0).Children(func() {
			k.Text(c, L("droid %s has a recipe; this Patched Droid is %s.", p.Latest, p.Version), 12, 18).TextColor(t.MutedForeground).Shrink(1).MinWidth(0)
			if a.smallButton(c, kit.Outline, L("Update"), "refresh-cw", p.Preparing).Label(L("Update the Patched Droid")).Clicked() {
				h.UpdatePatchedDroid()
			}
		})
	})
}

// patchProgress is the download and patch under way: a bar of the bytes
// fetched, or a line while the size is unknown.
func (a *App) patchProgress(c *ui.Context, p host.PatchedDroid) {
	k, t := a.kit, a.kit.T
	if p.Total <= 0 {
		k.Text(c, L("Preparing the Patched Droid…"), 12, 18).Role(ui.RoleStatus).TextColor(t.MutedForeground).Margin(k.Px(8), 0, 0, 0)
		return
	}
	share := float32(float64(p.Done) / float64(p.Total) * 100)
	ui.Column(c).Role(ui.RoleMeter).Label(L("Downloading droid: %s of %s", llmusage.FormatBytes(p.Done), llmusage.FormatBytes(p.Total))).
		Gap(k.Px(6)).Margin(k.Px(8), 0, 0, 0).Children(func() {
		k.Text(c, L("Downloading droid: %s of %s", llmusage.FormatBytes(p.Done), llmusage.FormatBytes(p.Total)), 12, 18).TextColor(t.MutedForeground)
		ui.Box(c).FillWidth().Height(k.Px(4)).Radius(k.Px(2)).Background(t.Foreground.Alpha(0.08)).Clip().Children(func() {
			fill := ui.Box(c).FillHeight().Radius(k.Px(2)).Background(t.Primary)
			fill.WidthPercent(min(fill.Animate("patch", share, 240*time.Millisecond), 100))
		})
	})
}

// keyPoolRow is the Key Pool in Settings: how many keys it holds, which one
// droid is sending with, and the way to the Usage page, where keys are
// added and switched.
func (a *App) keyPoolRow(c *ui.Context, p host.PatchedDroid) {
	k, t := a.kit, a.kit.T
	st := a.poolState()
	desc := L("The Patched Droid sends every LLM request with a key from the Key Pool, whatever account Droi is signed in as. Add and switch keys on the Usage page.")
	if len(st.Keys) == 0 {
		desc = L("The Key Pool is empty. The Patched Droid cannot send a request without a key: add one on the Usage page.")
	}
	a.settingRow(c, L("Key Pool"), desc, func() {
		a.statusPill(c, len(st.Keys) > 0, countL(len(st.Keys), l10n.N("%d key"), l10n.N("%d keys")))
		if a.smallButton(c, kit.Outline, L("Open Usage"), "", false).Clicked() {
			a.route.Tab = "usage"
		}
	}, func() {
		if len(st.Keys) == 0 {
			return
		}
		current := L("No key is in use yet.")
		for _, key := range st.Keys {
			if key.Current {
				current = L("In use: %s", key.Masked)
			}
		}
		mode := L("Keys switch automatically when one runs out.")
		if st.Mode == keypool.Manual {
			mode = L("Manual: droid keeps the key you chose.")
		}
		k.Text(c, current+" · "+mode, 12, 18).TextColor(t.MutedForeground).Margin(k.Px(8), 0, 0, 0)
	})
}
