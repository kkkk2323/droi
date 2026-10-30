package app

import (
	"log"

	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// applyPresets gives this computer's Daemon and this device the app's
// Presets, once for each Version. A save that fails is tried again on the
// next connection, before the Version is kept.
func (a *App) applyPresets() {
	p := a.cfg.Presets
	if p == nil || a.following() || int(prefs.PresetsVersion.Get(a.prefs)) >= p.Version {
		return
	}
	// A reconnect while the first try runs would send them twice.
	if !a.presetting.TryLock() {
		return
	}
	defer a.presetting.Unlock()
	if params := p.Session.Params(); len(params) > 0 {
		cl, err := a.ctl.Client()
		if err == nil {
			err = cl.Call(a.ctx, "daemon.update_session_defaults", params, nil)
		}
		if err != nil {
			log.Println("droi: presets:", err)
			return
		}
		a.defaults.Reload()
	}
	if p.ToolMode != "" {
		prefs.DefaultToolMode.Set(a.prefs, string(p.ToolMode))
	}
	prefs.PresetsVersion.Set(a.prefs, float64(p.Version))
	a.redraw()
}
