package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

// narrowWidth is the web Client's md breakpoint in CSS pixels: below it the
// sidebar is a drawer and the conversation takes the whole window.
const narrowWidth = 768

// isNarrow compares in CSS pixels, as the media query does: zooming in
// narrows the page.
func (a *App) isNarrow(c *ui.Context) bool {
	w, _ := c.Size()
	zoom := float32(prefs.Zoom.Get(a.prefs))
	if zoom <= 0 {
		zoom = 1
	}
	return w > 0 && w/zoom < narrowWidth
}

// sidebarShown is whether the sidebar stands beside the page; never while
// narrow, where it is the drawer.
func (a *App) sidebarShown() bool { return !a.narrow && prefs.SidebarVisible.Get(a.prefs) }

// openSessionsButton is the narrow header's button that opens the drawer,
// clear of the traffic lights.
func (a *App) openSessionsButton(c *ui.Context) {
	k := a.kit
	if a.cfg.InsetTop {
		ui.Box(c).Width(64).Shrink(0)
	}
	if k.IconButton(c, "panel-left", L("Open sessions"), 28).Expanded(a.drawerOpen).Clicked() {
		a.drawerOpen = true
	}
}

// drawer is the sidebar from the left edge over a dimmed page.
func (a *App) drawer(c *ui.Context, sidebar func()) {
	t := a.kit.T
	w, _ := c.Size()
	ui.DialogBase(c, &a.drawerOpen, func(backdrop, panel *ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.3))
		panel.Label(L("Sessions")).Absolute().Left(0).Top(0).Bottom(0).Width(min(w*0.85, a.kit.Px(288))).
			Background(t.Sidebar).TextColor(t.Foreground).Clip().Shadow(0, a.kit.Px(20), a.kit.Px(25), -a.kit.Px(5), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).Fill().Children(sidebar)
	})
}
