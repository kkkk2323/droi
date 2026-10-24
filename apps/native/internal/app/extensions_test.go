package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/extension"
)

// An app's own page is a sidebar row under Automations that opens it in
// the main panel, with what it draws.
func TestAnExtensionsPageOpensFromTheSidebar(t *testing.T) {
	var drawn extension.Env
	h := newHarnessWith(t, sessionsScenario(), "", func(cfg *Config) {
		cfg.Pages = []extension.Page{{ID: "orders", Label: "Orders board", Icon: "list-filter", Draw: func(c *ui.Context, env extension.Env) {
			drawn = env
			env.Kit.Text(c, "42 orders today", 14, 20)
		}}}
	})
	h.until("the page's row", func() bool { _, ok := h.tt.Find("Orders board"); return ok })
	if h.hasText("42 orders today") {
		t.Fatal("the page shows before it is opened")
	}
	h.click("Orders board")
	h.until("the page", func() bool { return h.hasText("42 orders today") })
	if r := h.a.Route(); r.Name != "page" || r.Page != "orders" {
		t.Errorf("route %+v", r)
	}
	if drawn.Kit == nil || drawn.Controller == nil || drawn.Prefs == nil || drawn.OpenSession == nil || drawn.Ctx == nil {
		t.Errorf("env %+v", drawn)
	}
}

// An app's own Settings section goes after its group's, or last under a
// heading of its own, and draws under its title.
func TestAnExtensionsSettingsTabShowsInItsGroup(t *testing.T) {
	h := newHarnessWith(t, sessionsScenario(), "", func(cfg *Config) {
		cfg.SettingsTabs = []extension.SettingsTab{
			{ID: "corp-data", Label: "Data sources", Icon: "list-filter", Group: "Company", Draw: func(c *ui.Context, env extension.Env) {
				env.Kit.Text(c, "corp-db is connected", 14, 20)
			}},
			{ID: "general", Label: "Taken", Icon: "list-filter", Group: "Personal"},
		}
	})
	h.a.Go(Route{Name: "settings", Tab: "corp-data"})
	h.until("the section", func() bool { return h.hasText("corp-db is connected") })
	if !h.hasText("Company") || !h.hasText("Data sources") {
		t.Errorf("no heading or row: %q", h.tt.Texts())
	}
	if h.hasText("Taken") {
		t.Error("a section took Droi's id")
	}
	tabs := withExtensionTabs(settingsTabs, []extension.SettingsTab{{ID: "x", Group: "Sessions"}, {ID: "y", Group: "Company"}})
	at := map[string]int{}
	for i, tab := range tabs {
		at[tab.id] = i
	}
	if at["x"] != at["worktrees"]+1 || at["y"] != len(tabs)-1 {
		t.Errorf("order %v", at)
	}
}
