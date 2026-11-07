---
status: accepted
---

# A brand's app runs on the Kernel through the same Extension seams

ADR 0029 let an app other than Droi run Droi's window under its own name, adding what it needs through one `Extension`. Two apps use it: **Anlan Droi** (a company build on Codeup, locked to Droi 1.90.0, its own 0.14.0 released 2026-11-03) and **WeCom Droi** (locked to 1.55.0, untouched since 15 October). ADR 0038 replaces the process those seams were shaped around, and those seams were shaped around it precisely: the MCP server and the hooks go into a `--settings` overlay, the System Prompt Addition is text the Gateway puts into a frame, the sign-in page hands the Host an API key, and a page draws from the Daemon's session controller. Delete the overlay and the frames and all of that has nowhere to land.

The one real case is Anlan Droi, so the seams are judged by what it actually uses. It imports `brand`, `defaults`, `droiapp`, `extension`, `host`, `kit`, `l10n`, `keypool`, `mcpserver` and `sessionview` (its `theme` and `transcript` imports are in tests only), and its `Extension` fills in six of the thirteen fields: `Start`, `SignIn`, `SettingsTabs` (one Account section), `Overlay` (one stdio MCP server run as a subcommand of its own executable, `BlockOnMCPLoad`, no hooks, no environment, no custom models), `SystemPrompt` (OpsLens's guide to the tools), `Commands` (`anlan-mcp` and the legacy name), `ToolViews` (`anlan___db_query`) and `Chinese`. It sets `OwnFactoryHome`, `HiddenSettings`, `DroidDownloads` and `Presets`, and touches no `Pages`, `Components` or `Icons`. Its pages read `Env.Kit`, `Env.Host`, `Env.Ctx` and `Env.Update`, never `Env.Controller`.

We decided that **`droiapp.Run` and the `Extension` stay, and each seam lands on a Kernel counterpart**:

| Seam | On the Kernel |
|---|---|
| `Overlay.MCPServers` | The Kernel's MCP entries. A server the app's own executable runs is still `{command: h.Executable(), args: [a command of Commands]}`; the Kernel spawns it as a child and speaks MCP over stdio, as the Daemon did. `BlockOnMCPLoad` keeps its meaning: a Conversation's first run waits for its servers. |
| `Overlay.Env` | The environment the Kernel gives the tools it runs. |
| `Overlay.CustomModels` | Gone. A model that is not Factory's is a pi-ai provider (ADR 0038), so an Extension contributes providers, not `customModels`. |
| `Overlay.Hooks` | **Not mapped, and this is the one seam that is lost.** A droid hook is a command string run at an event; the Kernel has no event of that shape to run one at. Its equivalent is a Kernel extension (TypeScript, `apps/kernel/src/extensions/`) named by path, which runs inside the Kernel and can wrap a tool, add a section, or observe a request. An app that needs one must ship one, and `Extension` gains a field that names the paths. No downstream app uses hooks today, so nothing waits on this; it is written down so nobody assumes the seam survived. |
| `SystemPrompt` | A Kernel extension `section`: the text is added to every Conversation's prompt, after Settings' own, and never replaces the Kernel's. |
| `SignIn` | Unchanged. It fills the window while this computer has no Factory credential, and `Host.SetAPIKey` is what ends that: the Host writes the key to the Key Pool (ADR 0039), so "no credential" now means "an empty pool", not "no login". |
| `Commands` | Unchanged: subcommands of the app's executable, dispatched by `droiapp` before any window opens. The executable an MCP server names is still this. |
| `Pages`, `SettingsTabs`, `Components`, `Icons`, `Chinese` | Unchanged, and drawn by the same Go code: `Env.Kit`, `Env.Host`, `Env.Prefs`, `Env.Ctx` and `Env.Update` keep their meaning. |
| `ToolViews` | The type is unchanged; the key is the Kernel's name for the tool, so an MCP server's `db_query` is keyed by whatever the Kernel calls it (the protocol names an MCP tool in M3, and this ADR requires that name be predictable from the server and tool names). |
| `Start` | Unchanged: it runs on the Host's goroutine once the Host starts, and it is where an app keeps the credential it handed over current. |
| `Env.Controller` | Becomes the Kernel's client instead of the Daemon's session controller. Nothing in either downstream app reads it today. |
| `droiapp.Options.OwnFactoryHome`, `.DroidDownloads` | Gone with the Daemon: there is no `~/.factory` home to move and no droid build to mirror. |
| `droiapp.Options.HiddenSettings`, `.Brand`, `.DevVersion` | Unchanged. `Presets` keeps the model, the effort and the compaction limit, and loses the Script tool mode, which only meant which tool ran a program. |

Two more things follow, and both are the user's decision:

- **A downstream app is not migrated in 2.0.** Anlan Droi stays on Droi 1.90.0 and WeCom Droi on 1.55.0, each running the droid Daemon it was built for, until it is upgraded on its own afterwards. 2.0 keeps the seams so that upgrade is a port, not a rewrite.
- **Droi's public packages may break in 2.0.** `github.com/kkkk2323/droi/apps/native` becomes `github.com/kkkk2323/droi/apps/native/v2`, the tags `apps/native/v2.x.y`, and the `CHANGELOG` says what an app must change. `extension`, `droiapp`, `brand`, `defaults`, `kit`, `l10n`, `sessionview`, `keypool` and `mcpserver` stay public; `host` keeps what a downstream app needs of it (Key Pool, data folder, executable, Model tools) and loses the Daemon's parts.

## Considered options

- **Drop `droiapp.Run` and the Extension in 2.0, and let an app fork Droi.** Rejected: Anlan Droi is a shipped company product, and forking turns every Droi fix into a merge it has to do by hand.
- **Keep the Runtime Overlay alive only for Extensions.** Rejected: it is a settings file for a program that no longer exists; the seams above cover what an Extension actually puts in it.
- **Map hooks by running them from a Kernel extension.** Rejected as the default: a hook is a shell command with droid's own event and payload shapes, and pretending to support it would make an app's hook run in a place it was never written for. An app that needs the behaviour ships a Kernel extension.
- **Migrate Anlan Droi inside 2.0.** Rejected by the user: it would tie a company release to Droi's biggest change, and 2.0 has enough on it.

## Consequences

- **The go.mod of a downstream app moves to `/v2`**, and its `Extension` literal loses `Overlay.CustomModels`, `Overlay.Hooks` (if used), `OwnFactoryHome` and `DroidDownloads`, and gains the extension-path field if it ships a Kernel extension.
- **Both downstream apps stay on their pins until they are upgraded**, which is safe because they carry their own copy of the droid-side Droi they build against.
- **`ToolViews` keys depend on an M3 decision** about how the Kernel names an MCP tool. Until it is made, an Extension's key is a thing to check at upgrade time, not to guess.
- **`Env.Controller` changes type.** An Extension that draws from it gets a compile error, which is the right way to find out; neither downstream app does.

## What this ADR does not decide

- **How the Kernel names an MCP tool**, which the `ToolViews` and the model's tool list both depend on (M3).
- **When Anlan Droi is upgraded**, and what its 0.15.0 changes beyond the port itself (its own repository and release).
