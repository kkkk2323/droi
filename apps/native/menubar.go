package main

import (
	"log"
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/app"
	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
	"github.com/kkkk2323/droi/apps/native/internal/trayicon"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// menuBar is the Key Pool's menu bar item, on while Settings → Advanced
// says so: an icon with the monthly quota left beside it (macOS), and the
// panel a click opens. Main thread only.
type menuBar struct {
	current func() *app.App
	win     func() *mygo.Window
	tray    *mygo.Tray
	panel   *mygo.Window
	shown   llmusage.MenuBarView
	// failed stops retrying an icon the system refused.
	failed bool
	// w, h are the panel's size, so that Fit resizes it only when it changed.
	w, h int
}

// shell is what the App gets to reach the item.
func (m *menuBar) shell() *app.MenuBar {
	return &app.MenuBar{
		Fit:       m.fit,
		Close:     m.closePanel,
		OpenUsage: m.openUsage,
		Quit:      func() { go mygo.App.Quit() },
	}
}

// sync adds, updates or removes the icon as the App says. Main thread only.
func (m *menuBar) sync() {
	a := m.current()
	if !a.MenuBarShown() {
		m.remove()
		return
	}
	v := a.MenuBarItem()
	if m.tray == nil {
		if m.failed {
			return
		}
		tray, err := mygo.NewTray(mygo.TrayOptions{
			ID:             "droi.keypool",
			Icon:           trayicon.For(string(v.Icon)),
			IconIsTemplate: trayicon.IsTemplate(string(v.Icon)),
			Title:          v.Title,
			ToolTip:        v.ToolTip,
		})
		if err != nil {
			log.Println("droi: menu bar item:", err)
			m.failed = true
			return
		}
		m.tray, m.shown = tray, v
		tray.OnClick(m.toggle)
		tray.OnRightClick(m.popUpMenu)
		return
	}
	if v.Title != m.shown.Title {
		m.tray.SetTitle(v.Title)
	}
	if v.Icon != m.shown.Icon {
		_ = m.tray.SetIcon(trayicon.For(string(v.Icon)), trayicon.IsTemplate(string(v.Icon)))
	}
	if v.ToolTip != m.shown.ToolTip {
		m.tray.SetToolTip(v.ToolTip)
	}
	m.shown = v
}

func (m *menuBar) remove() {
	if m.panel != nil {
		m.panel.Close()
	}
	if m.tray != nil {
		m.tray.Destroy()
		m.tray = nil
	}
	m.failed = false
}

// toggle opens the panel under the icon, or closes it.
func (m *menuBar) toggle() {
	if m.panel != nil {
		m.panel.Close()
		return
	}
	m.w, m.h = app.MenuBarPanelWidth, 320
	m.panel = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      m.tray,
		Placement: mygo.PlacementBottom,
		Gap:       4,
		Width:     m.w,
		Height:    m.h,
		Focusable: true,
		Shadow:    true,
		Popover:   true,
		Content:   ui.View(func(c *ui.Context) { m.current().MenuBarPanel(c) }),
	})
	panel := m.panel
	panel.OnClosed(func() {
		if m.panel == panel {
			m.panel = nil
		}
	})
}

// popUpMenu is the icon's menu, set for this one right-click so that a
// left click keeps opening the panel.
func (m *menuBar) popUpMenu() {
	m.closePanel()
	m.tray.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: l10n.L("Open Usage"), Click: func(*mygo.MenuItem, *mygo.Window) { m.openUsage() }},
		{Label: l10n.L("Refresh"), Click: func(*mygo.MenuItem, *mygo.Window) {
			if w := m.win(); w != nil {
				w.Update(func() { m.current().RefreshKeyPool() })
			}
		}},
		mygo.Separator(),
		quit(),
	}))
	defer m.tray.SetMenu(nil)
	m.tray.PopUpMenu()
}

// fit sizes the panel to what its frame drew; the resize waits for the
// frame to end.
func (m *menuBar) fit(width, height float32) {
	w, h := int(math.Ceil(float64(width))), int(math.Ceil(float64(height)))
	if m.panel == nil || w <= 0 || h <= 0 || (w == m.w && h == m.h) {
		return
	}
	m.w, m.h = w, h
	panel := m.panel
	go panel.SetContentSize(w, h)
}

func (m *menuBar) closePanel() {
	if m.panel != nil {
		m.panel.Close()
	}
}

func (m *menuBar) openUsage() {
	m.closePanel()
	w := m.win()
	if w == nil {
		return
	}
	w.Show()
	w.Focus()
	w.Update(func() { m.current().OpenUsage() })
}

// invalidate redraws the open panel.
func (m *menuBar) invalidate() {
	if p := m.panel; p != nil {
		p.Invalidate()
	}
}

// tick reads the item again every app.MenuBarRefresh: the quota changes
// without the window hearing of it.
func (m *menuBar) tick(update func(func())) {
	for range time.Tick(app.MenuBarRefresh) {
		update(func() {})
	}
}
