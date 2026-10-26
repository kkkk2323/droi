package app

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/extension"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// extensionEnv is what an app's own Page or Settings section draws with.
func (a *App) extensionEnv() extension.Env {
	return extension.Env{
		Kit:         a.kit,
		Controller:  a.ctl,
		Host:        a.cfg.Host,
		Prefs:       a.prefs,
		Ctx:         a.ctx,
		Update:      a.cfg.Update,
		OpenSession: a.OpenSession,
	}
}

// extensionPage is the app's own Page id, nil when there is none.
func (a *App) extensionPage(id string) *extension.Page {
	for i := range a.cfg.Pages {
		if a.cfg.Pages[i].ID == id {
			return &a.cfg.Pages[i]
		}
	}
	return nil
}

// extensionPageView is an app's own Page in the main panel, under the
// window's top strip.
func (a *App) extensionPageView(c *ui.Context, p *extension.Page) {
	k := a.kit
	ui.Column(c).Role(ui.RoleGroup).Label(l10n.T(p.Label)).Fill().Children(func() {
		ui.Row(c).Height(k.Px(44)).Shrink(0).PaddingX(k.Px(8)).AlignItems(ui.Center).DragWindow().Children(func() {
			if a.narrow {
				a.openSessionsButton(c)
			}
		})
		ui.Column(c).Grow(1).MinHeight(0).MinWidth(0).Children(func() {
			if p.Draw != nil {
				p.Draw(c, a.extensionEnv())
			}
		})
	})
}

// extensionTab draws the app's own Settings section id.
func (a *App) extensionTab(c *ui.Context, id string) {
	for _, m := range a.cfg.SettingsTabs {
		if m.ID == id && m.Draw != nil {
			m.Draw(c, a.extensionEnv())
			return
		}
	}
}

// settingHidden: the app leaves Droi's Settings section id out. About
// always stays, so Settings is never empty.
func (a *App) settingHidden(id string) bool {
	return id != "about" && slices.Contains(a.cfg.HiddenSettings, id)
}

// droiSettingsTabs are Droi's Settings sections the app keeps.
func (a *App) droiSettingsTabs() []settingsTab {
	if len(a.cfg.HiddenSettings) == 0 {
		return settingsTabs
	}
	var tabs []settingsTab
	for _, tab := range settingsTabs {
		if !a.settingHidden(tab.id) {
			tabs = append(tabs, tab)
		}
	}
	return tabs
}

// withExtensionTabs is Droi's Settings sections with an app's own: each
// after the last of its group, a group of its own last. A section whose id
// is taken is left out.
func withExtensionTabs(droi []settingsTab, more []extension.SettingsTab) []settingsTab {
	if len(more) == 0 {
		return droi
	}
	tabs := append([]settingsTab(nil), droi...)
	for _, m := range more {
		taken := false
		for _, t := range tabs {
			taken = taken || t.id == m.ID
		}
		if taken || m.ID == "" {
			continue
		}
		tab := settingsTab{id: m.ID, label: m.Label, icon: m.Icon, needsHost: m.NeedsHost, group: m.Group}
		at := len(tabs)
		for i, t := range tabs {
			if t.group == m.Group {
				at = i + 1
			}
		}
		tabs = append(tabs[:at], append([]settingsTab{tab}, tabs[at:]...)...)
	}
	return tabs
}
