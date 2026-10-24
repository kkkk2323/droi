---
status: accepted
---

# Droi runs as a library under another app's brand, with extensions

A company wants its own build of Droi: everything Droi does, under its own name, plus its private MCP servers and tools, ways to query its data, and dashboards. WeCom Droi (ADR 0022) reuses Droi's packages but draws a window of its own; a company build wants Droi's window as it is, and every Droi release with it, so redrawing it, or forking the repository, is the wrong cost. Go does not let another module import `apps/native/internal`, and until now Droi's window was put together in `package main`, which no module can import.

We decided:

- **The app is a library: `pkg/droiapp`.** What `main.go` did (the Host, the Gateway, the window, the menus, the menu bar item, the subcommands) is `droiapp.Run(Options)`. Droi's `main.go` is one call with its `devVersion`; a company's is one call with its brand and extensions. `internal/` stays internal: `pkg/droiapp`, which may import it, is the one door.
- **A brand names the app: `pkg/brand`.** Its name, the short id of its folders (`~/.<id>/chats`, `$XDG_CONFIG_HOME/<id>` for `<app> host`), the Gateway's preferred port and its mark. Droi's words keep the word Droi; `l10n.SetAppName` makes it read as the brand's name, so neither the catalog nor `zh.go` changes. The other app's identity, data folder, signing key and update channel come from its own `mygo.json`, which MyGo reads at build time.
- **The Gateway still answers `/meta` as "Droi".** That is its protocol's name, not the app's: the Phone App and `gateway.FetchMeta` pair only with a Gateway that answers so, and a company build is paired with the same Phone App. Two apps on one computer take two ports.
- **An extension is a struct of optional parts: `pkg/extension`.** Each part plugs into a seam that Droi itself uses, so it costs Droi nothing when empty:
  - `Overlay`: MCP servers (pre-approved and at a chosen autonomy, as Memory's), hooks, environment and models, added to the Runtime Overlay at each Daemon start (`host.Config.Overlay`, `host.OverlayOptions`).
  - `SystemPrompt`: appended after Settings' System Prompt Addition (`host.Config.AppendSystemPrompt`).
  - `Commands`: subcommands of the app's executable, so an MCP server ships inside it as Memory's does; `pkg/mcpserver` is the stdio protocol the Memory Server uses.
  - `Pages`: sidebar rows under Automations that open a page in the main panel; `SettingsTabs`: Settings sections in Droi's groups or one of their own. Both draw with an `extension.Env` (the Kit, the Controller, the Host, preferences).
  - `ToolViews` and `Components`: how a tool's calls and a json-render element type of its own show in a Session (`sessionview.RegisterToolView`, `sessionview.RegisterComponent`), for query results as tables and charts.
  - `Icons` (`icons.Register`) and `Chinese` (`l10n.Add`).
- **Dashboards draw with `pkg/dashkit`,** the Usage tab's cards, metrics, meters, controls and charts, moved out of `internal/app`.
- `droi host` takes the same brand and the extensions' Overlay and SystemPrompt.

## Considered Options

- **Fork the repository.** All of Droi at once, but every release is a merge, and the merge grows with every change the company makes.
- **Move `internal/app` to `pkg/`.** A company could then build its window from Droi's parts, but the window's 15,000 lines would become an API, every change to them a possible break.
- **Plugins loaded at run time.** Go's plugins do not build without cgo, which MyGo's builds leave out.

## Consequences

- `pkg/droiapp`, `pkg/extension`, `pkg/brand`, `pkg/mcpserver` and `pkg/dashkit` are an API, tagged with each release as ADR 0022's packages are.
- Registrations (`RegisterToolView`, `RegisterComponent`, `icons.Register`, `l10n.Add`, `brand.Set`) are process-wide and happen in `droiapp.Run`, before anything is drawn.
- A new page, section or tool view needs no change to Droi; a new kind of seam does, and is added here first.
- Key Pool, Patched Droid and Magpie stay Droi's; a company build that wants none of them leaves them off in Settings.
