package droiapp

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/extension"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/icons"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
)

func TestExtensionsJoin(t *testing.T) {
	ran := ""
	a := extension.Extension{
		Name: "a",
		Overlay: func(*host.Host) host.OverlayOptions {
			return host.OverlayOptions{MCPServers: map[string]host.MCPServer{"db": {Autonomy: "low"}}}
		},
		SystemPrompt: func() string { return "Use db for data. " },
		Commands:     map[string]func([]string) int{"db-server": func(args []string) int { ran = args[0]; return 3 }},
		Pages:        []extension.Page{{ID: "p1"}},
	}
	b := extension.Extension{
		Name: "b",
		Overlay: func(*host.Host) host.OverlayOptions {
			return host.OverlayOptions{MCPServers: map[string]host.MCPServer{"db": {}, "wiki": {}}}
		},
		SystemPrompt: func() string { return "  " },
		Pages:        []extension.Page{{ID: "p2"}},
		SettingsTabs: []extension.SettingsTab{{ID: "s"}},
	}
	signedIn := ""
	c := extension.Extension{
		SystemPrompt: func() string { return "Wiki has the runbooks." },
		SignIn:       func(*ui.Context, extension.Env) { signedIn = "c" },
	}
	d := extension.Extension{SignIn: func(*ui.Context, extension.Env) { signedIn = "d" }}
	x := newExtensions([]extension.Extension{a, b, c, d})
	o := x.overlay(nil)
	if len(o.MCPServers) != 2 || o.MCPServers["db"].Autonomy != "low" {
		t.Errorf("overlay %+v", o)
	}
	if got := x.systemPrompt(); got != "Use db for data.\n\nWiki has the runbooks." {
		t.Errorf("prompt %q", got)
	}
	if cmd := x.command("db-server"); cmd == nil || cmd([]string{"x"}) != 3 || ran != "x" {
		t.Error("db-server")
	}
	if x.command("host") != nil || x.command("nope") != nil {
		t.Error("a command no extension has")
	}
	if len(x.pages) != 2 || x.pages[1].ID != "p2" || len(x.settingsTabs) != 1 {
		t.Errorf("pages %v, tabs %v", x.pages, x.settingsTabs)
	}
	if x.signIn == nil {
		t.Fatal("no sign-in page")
	}
	if x.signIn(nil, extension.Env{}); signedIn != "c" {
		t.Errorf("sign-in page of %q, want the first's", signedIn)
	}
	if newExtensions(nil).signIn != nil {
		t.Error("a sign-in page without an extension")
	}
}

func TestExtensionsRegister(t *testing.T) {
	defer l10n.Set("", "en-US")
	newExtensions([]extension.Extension{{
		Icons:      map[string][]byte{"droiapp-test-db": []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 4h16"/></svg>`)},
		Chinese:    map[string]string{"Orders board (droiapp test)": "订单看板"},
		ToolViews:  map[string]sessionview.ToolView{"droiapp-test___q": {Open: true}},
		Components: map[string]sessionview.Component{"DroiappTest": func(*ui.Context, *kit.Kit, map[string]any, func()) {}},
	}}).register()
	if icons.Get("droiapp-test-db") == nil {
		t.Error("icon")
	}
	l10n.Set("zh-Hans", "")
	if got := l10n.T("Orders board (droiapp test)"); got != "订单看板" {
		t.Errorf("words %q", got)
	}
}

// The extensions' Agents join into what the Kernel is given: servers by name, the first's kept; every tool that runs
// without asking, once; sections in order, a blank line between them.
func TestExtensionsAgentsJoin(t *testing.T) {
	a := extension.Extension{Agent: func(*host.Host) extension.Agent {
		return extension.Agent{
			MCPServers:  []extension.AgentMCPServer{{Name: "db", Command: "/bin/db", Args: []string{"serve"}, BlockOnLoad: true}},
			AlwaysAllow: []string{"db___query"},
			Section:     " Use db for data. ",
		}
	}}
	b := extension.Extension{Agent: func(*host.Host) extension.Agent {
		return extension.Agent{
			MCPServers:  []extension.AgentMCPServer{{Name: "db", URL: "https://other"}, {Name: "wiki", URL: "https://wiki", Headers: map[string]string{"X": "1"}}, {}},
			AlwaysAllow: []string{"db___query", "wiki___read", ""},
		}
	}}
	c := extension.Extension{Agent: func(*host.Host) extension.Agent { return extension.Agent{Section: "Wiki has the runbooks."} }}
	got := newExtensions([]extension.Extension{a, {}, b, c}).agent(nil)
	if len(got.MCPServers) != 2 || got.MCPServers["db"].Command != "/bin/db" || !got.MCPServers["db"].BlockOnLoad ||
		got.MCPServers["wiki"].URL != "https://wiki" || got.MCPServers["wiki"].Headers["X"] != "1" {
		t.Errorf("servers %+v", got.MCPServers)
	}
	if len(got.AlwaysAllow) != 2 || got.AlwaysAllow[0] != "db___query" || got.AlwaysAllow[1] != "wiki___read" {
		t.Errorf("always allow %v", got.AlwaysAllow)
	}
	if got.Section != "Use db for data.\n\nWiki has the runbooks." {
		t.Errorf("section %q", got.Section)
	}
	if !newExtensions(nil).agent(nil).Empty() {
		t.Error("an agent without an extension")
	}
}
