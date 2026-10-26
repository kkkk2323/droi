package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/extension"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
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

// An app's own sign-in page takes the place of Droi's while the Host has
// nothing to authenticate with, Settings still in reach; the key it hands
// the Host lets the app in.
func TestAnExtensionsSignInPageTakesDroisPlace(t *testing.T) {
	h0 := testHost(t)
	var drawn extension.Env
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) {
		cfg.Host = h0
		cfg.SignIn = func(c *ui.Context, env extension.Env) {
			drawn = env
			env.Kit.Text(c, "Sign in with Acme", 20, 28)
			if env.Kit.SmallButton(c, kit.Primary, "Use Acme's key", "", false).Clicked() {
				if err := env.Host.SetAPIKey("fk-acme"); err != nil {
					t.Error(err)
				}
			}
		}
	})
	h.until("the app's sign-in page", func() bool { return h.hasText("Sign in with Acme") })
	if h.hasText("Sign in to Droi") || h.hasText("New session") {
		t.Fatalf("Droi's sign-in page or the sessions show: %q", h.tt.Texts())
	}
	if drawn.Host != h0 || drawn.Kit == nil {
		t.Errorf("env %+v", drawn)
	}
	if _, ok := h.tt.Find("Settings"); !ok {
		t.Error("Settings is out of reach")
	}
	h.click("Use Acme's key")
	h.until("the app", func() bool { return !h.hasText("Sign in with Acme") && h.hasText("New session") })
	if got := h0.Settings.APIKey(); got != "fk-acme" {
		t.Fatalf("stored key %q", got)
	}
}

// Droi's Settings sections an app hides are gone, a link to one opens the
// first section shown, About stays, and an app's own section may take a
// hidden one's id.
func TestHiddenSettingsSectionsAreLeftOut(t *testing.T) {
	h0 := testHost(t)
	h0.Settings.Update(func(s *host.Settings) { s.APIKey = host.OptString("fk-x") })
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) {
		cfg.Host = h0
		cfg.HiddenSettings = []string{"account", "advanced", "about"}
		cfg.SettingsTabs = []extension.SettingsTab{{ID: "account", Label: "Acme account", Icon: "user-round", Group: "Personal", Draw: func(c *ui.Context, env extension.Env) {
			env.Kit.Text(c, "Signed in as dev@acme.test", 14, 20)
		}}}
	})
	h.until("the app", func() bool { return h.hasText("New session") })
	h.a.Go(Route{Name: "settings", Tab: "advanced"})
	h.until("Settings", func() bool { return h.hasText("Acme account") })
	if h.hasText("droid executable") {
		t.Error("the link to Advanced opened it")
	}
	for _, gone := range []string{"Advanced", "Factory API key"} {
		if _, ok := h.tt.Find(gone); ok {
			t.Errorf("%q shows", gone)
		}
	}
	if _, ok := h.tt.Find("About"); !ok {
		t.Error("About is gone")
	}
	h.click("Acme account")
	h.until("the app's section", func() bool { return h.hasText("Signed in as dev@acme.test") })
}
