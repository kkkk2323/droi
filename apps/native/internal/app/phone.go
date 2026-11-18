package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
	"github.com/kkkk2323/droi/apps/native/pkg/theme"
)

// PrefsKit is the Kit the preferences in p ask for, and sets the language
// they and locale pick: what the Phone App draws its pairing page with
// before it has an App.
func PrefsKit(p *prefs.Store, locale string) *kit.Kit {
	k := kit.New(theme.Parse(prefs.Theme.Get(p)), theme.FontChoice(prefs.Font.Get(p)), theme.TextSize(prefs.TextSize.Get(p)), prefs.Zoom.Get(p))
	k.Compact = theme.Density(prefs.Density.Get(p)) == theme.DensityCompact
	l10n.Set(prefs.Language.Get(p), locale)
	return k
}

// ComputerError is err, from computer.Verify, as the window tells it.
func ComputerError(err error) string { return computerError(err) }

// phoneList is a phone's first page, the Session list, which a Session or
// the New session page is pushed over (ADR 0014): the sidebar, filling the
// screen, under the connection's banner.
func (a *App) phoneList(c *ui.Context, status controller.Status, listed []sessions.Summary, busy map[string]string, listErr string, listDone bool) {
	t := a.kit.T
	a.mu.Lock()
	starting := startingUp(status, a.everConnected)
	a.mu.Unlock()
	ui.Column(c).Fill().Background(t.Sidebar).TextColor(t.Foreground).Children(func() {
		if starting && status.Failure != nil {
			a.followingStartView(c, status)
			return
		}
		a.banner(c, status)
		ui.Column(c).Grow(1).MinHeight(0).Children(func() {
			a.sidebarView(c, listed, busy, listErr, listDone)
		})
	})
	a.newWorkstreamDialog(c)
	a.workstreamSettingsDialog(c)
	a.archiveWorkstreamDialog(c)
}

// pickImages asks for images to attach, off the UI thread: the app's own
// picker, else the system's file dialog.
func (a *App) pickImages() ([]string, error) {
	if a.cfg.PickImages != nil {
		return a.cfg.PickImages()
	}
	return mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:    L("Choose images"),
		Multiple: true,
		Filters:  []mygo.FileFilter{{Name: L("Images"), Extensions: []string{"png", "jpg", "jpeg", "gif", "webp"}}},
	})
}

// phoneEdgeBack lets a finger from the screen's left edge go back to the
// Session list, as UIKit's stacks do: past a third of the way, or flung.
func (a *App) phoneEdgeBack(c *ui.Context) {
	k := a.kit
	w, _ := c.Size()
	ui.Overlay(c, func() {
		edge := ui.Box(c).Absolute().Left(0).Top(k.Px(56)).Bottom(0).Width(k.Px(16)).Label(L("Back to sessions"))
		dx, _, held := edge.Dragged()
		switch {
		case held:
			a.edgeSwipe += dx
		case a.edgeSwipe != 0:
			if a.edgeSwipe > w/3 {
				a.Go(Route{Name: "home"})
			}
			a.edgeSwipe = 0
		}
	})
}
