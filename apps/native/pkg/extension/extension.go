// Package extension is what an app built on Droi adds to it: MCP servers
// and hooks for the Daemon, subcommands of its executable, pages beside the
// Sessions, Settings sections, and ways to show tool calls and json-render
// elements. droiapp.Run takes a list of them.
package extension

import (
	"context"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
)

// Extension is one app's additions. Every field is optional.
type Extension struct {
	// Name is the extension's, for logs.
	Name string
	// Overlay is what it adds to the Runtime Overlay, read each time the
	// Daemon starts: MCP servers, hooks, environment and models. A server
	// the app's own executable runs is {"type": "stdio", "command":
	// h.Executable(), "args": [a command of Commands]}; h.Executable() is ""
	// under go run. h.RestartDaemon() applies a change.
	Overlay func(h *host.Host) host.OverlayOptions
	// SystemPrompt is added to the System Prompt Addition at every Session
	// start, after the one Settings holds: how to use its tools, say.
	SystemPrompt func() string
	// Commands are subcommands of the app's executable, by name, run in
	// place of the window with what follows the name; each returns the exit
	// code. Droi's own (memory-server, memory-hook, host) cannot be taken.
	Commands map[string]func(args []string) int
	// Pages are shown in the sidebar under Automations, in this order.
	Pages []Page
	// SettingsTabs are Settings sections, after the sections of their group.
	SettingsTabs []SettingsTab
	// ToolViews show the calls of tools by name, as the Daemon names them
	// ("corp-db___query" for the query tool of the MCP server corp-db).
	ToolViews map[string]sessionview.ToolView
	// Components draw json-render element types of their own, by type. The
	// model learns of them from SystemPrompt.
	Components map[string]sessionview.Component
	// Icons are SVG icons drawn as Lucide's, by name, for Pages,
	// SettingsTabs and ToolViews (package icons).
	Icons map[string][]byte
	// Chinese are the Chinese words of its own texts, by English key (l10n.Add).
	Chinese map[string]string
}

// Page is a page of the main panel, opened from the sidebar.
type Page struct {
	// ID tells the page from the others; Label and Icon (a name of package
	// icons) are its sidebar row.
	ID, Label, Icon string
	// Draw builds the page under the window's top strip, every frame; it
	// fills the panel and scrolls what it needs to.
	Draw func(c *ui.Context, env Env)
}

// SettingsTab is a section of Settings.
type SettingsTab struct {
	// ID tells the section from the others, Droi's included; Label and Icon
	// (a name of package icons) are its row. Group is the heading it goes
	// under: one of Droi's ("Personal", "Sessions", "Devices", "System") or
	// a heading of its own, which goes last.
	ID, Label, Icon, Group string
	// NeedsHost leaves the section out while the window shows another
	// computer, whose Host is not this app's.
	NeedsHost bool
	// Draw builds the section under its title, every frame; kit.Kit's
	// SettingGroup and SettingRow draw Droi's rows.
	Draw func(c *ui.Context, env Env)
}

// Env is what a Page or a SettingsTab draws with.
type Env struct {
	Kit *kit.Kit
	// Controller is the window's to the Daemon it shows.
	Controller *controller.Controller
	// Host is this computer's; nil while the window shows another computer.
	Host  *host.Host
	Prefs *prefs.Store
	// Ctx ends when the window's insides are built again or the app quits.
	Ctx context.Context
	// Update runs fn on the main thread and draws: for work done on another
	// goroutine.
	Update func(fn func())
	// OpenSession shows a Session.
	OpenSession func(id string)
}
