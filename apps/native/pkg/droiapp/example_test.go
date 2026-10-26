package droiapp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/brand"
	"github.com/kkkk2323/droi/apps/native/pkg/dashkit"
	"github.com/kkkk2323/droi/apps/native/pkg/droiapp"
	"github.com/kkkk2323/droi/apps/native/pkg/extension"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/mcpserver"
	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
)

// acmeKey signs the person in with Acme's identity provider and asks Acme's
// service for their Factory API key.
func acmeKey(ctx context.Context) (string, error) { return "", ctx.Err() }

// A company's build of Droi is a main package of its own, with its own
// mygo.json (name, identifier, update key): Droi's app under its brand,
// with a remote MCP server, a tool server inside its executable, a page in
// the sidebar and a table for its query tool's results.
func ExampleRun() {
	const tools = "acme-tools"
	acme := extension.Extension{
		Name: "acme",
		Overlay: func(h *host.Host) host.OverlayOptions {
			servers := map[string]host.MCPServer{
				"acme-db": {Config: map[string]any{"type": "http", "url": "https://mcp.acme.internal/db"}, Approved: true, Autonomy: "low"},
			}
			if exe := h.Executable(); exe != "" {
				servers[tools] = host.MCPServer{Config: map[string]any{"type": "stdio", "command": exe, "args": []string{tools}}, Approved: true}
			}
			return host.OverlayOptions{MCPServers: servers, BlockOnMCPLoad: true}
		},
		SystemPrompt: func() string { return "Use acme-db's query tool for questions about orders." },
		Commands: map[string]func([]string) int{
			tools: func([]string) int {
				err := mcpserver.Serve(os.Stdin, os.Stdout, mcpserver.Server{
					Name: tools, Title: "Acme tools", Version: "1.0.0",
					Tools: json.RawMessage(`[{"name":"ping","description":"Answers pong.","inputSchema":{"type":"object"}}]`),
					Call: func(name string, args map[string]any, sessionID string) (mcpserver.Result, error) {
						return mcpserver.Text("pong", false), nil
					},
				})
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 1
				}
				return 0
			},
		},
		Pages: []extension.Page{{ID: "orders", Label: "Orders", Icon: "list-filter", Draw: func(c *ui.Context, env extension.Env) {
			look := dashkit.New(env.Kit, c)
			ui.Column(c).Fill().Padding(look.Px(24)).Gap(look.Px(16)).Children(func() {
				look.MetricStrip(c, []dashkit.MetricView{{Title: "Orders today", Value: "1,284"}, {Title: "Refunds", Value: "12", Tone: dashkit.ToneWarning}})
			})
		}}},
		ToolViews: map[string]sessionview.ToolView{
			"acme-db___query": {Open: true, Detail: func(c *ui.Context, t sessionview.ToolContext) {
				for _, line := range strings.Split(t.Result.Text, "\n") {
					t.Kit.Text(c, line, 12, 18)
				}
			}},
		},
		Chinese: map[string]string{"Orders": "订单"},
		// People sign in with Acme's own identity provider, which hands out
		// their Factory API key, instead of with Factory.
		SignIn: func(c *ui.Context, env extension.Env) {
			ui.Column(c).Fill().Center().Gap(env.Kit.Px(16)).Children(func() {
				env.Kit.Text(c, "Sign in to Acme Agent", 20, 28)
				if env.Kit.SmallButton(c, kit.Primary, "Sign in with Acme SSO", "", false).Clicked() {
					go func() {
						if key, err := acmeKey(env.Ctx); err == nil {
							_ = env.Host.SetAPIKey(key)
						}
					}()
				}
			})
		},
	}
	droiapp.Run(droiapp.Options{
		DevVersion: "0.1.0",
		Brand:      brand.Brand{Name: "Acme Agent", ID: "acme-agent", Port: 41517},
		Extensions: []extension.Extension{acme},
		// Its own Sessions and login, apart from the droid CLI's, and no
		// Factory sign-in in Settings.
		OwnFactoryHome: true,
		HiddenSettings: []string{"account"},
	})
}
