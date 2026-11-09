---
status: accepted
---

# A brand's app runs on the Kernel: the shell stays, and its agent settings become one `Agent`

ADR 0029 let an app other than Droi run Droi's window under its own name, adding what it needs through one `Extension`. Two apps use it: **Anlan Droi** (a company build on Codeup, locked to Droi 1.90.0, its own 0.14.0 released 2026-11-03) and **WeCom Droi** (locked to 1.55.0, untouched since 15 October). ADR 0038 replaces the process those seams were shaped around, and part of the `Extension` was shaped around it precisely: MCP servers and hooks went into a `--settings` overlay the Daemon took at start, the System Prompt Addition was text the Gateway put into a frame, and a page drew from the Daemon's session controller.

The one real user decides how much of that has to survive. Anlan Droi fills in eight fields — `Name`, `Start`, `SignIn`, `SettingsTabs`, `Overlay`, `SystemPrompt`, `Commands`, `ToolViews` and `Chinese` — and its overlay holds exactly three things: one stdio MCP server run as a subcommand of its own executable, the tool names it may run unasked, and `BlockOnMCPLoad`. It uses **no hooks**, no environment variables and no custom models. Its pages read `Env.Kit`, `Env.Host`, `Env.Ctx` and `Env.Update`, never `Env.Controller`.

So the `Extension` splits in two, and only the second half changes:

**What stays, because it never had anything to do with the agent process:** `Name`, `Start`, `SignIn`, `Commands`, `Pages`, `SettingsTabs`, `ToolViews`, `Components`, `Icons` and `Chinese`, drawn by the same Go code from the same `Env`. `droiapp.Run` and its `Options` stay too, minus the two fields that only existed for the Daemon (`OwnFactoryHome`, `DroidDownloads`). `Env.Controller` becomes the Kernel's client instead of the Daemon's session controller; nothing in either downstream app reads it.

**What becomes one field, `Agent`:**

```go
// Agent is what a brand's app adds to every Conversation: tool servers, the
// tools that run without asking, and the words of its own prompt section.
// The Kernel's MCP extension, its approval policy and its prompt sections
// take them; there is no settings file to write.
type Agent struct {
    MCPServers []AgentMCPServer // stdio or HTTP, by the Kernel's own names
    AlwaysAllow []string        // tool names, "server___tool" for an MCP server's
    Section     string          // added to the prompt, after Settings' own
}
```

- **`MCPServers`** is the Kernel's MCP entries. A server the app's own executable runs is still `{Command: h.Executable(), Args: ["anlan-mcp", ...]}`; the Kernel spawns it as a child and speaks MCP over stdio, as the Daemon did. HTTP servers are a URL and headers. `BlockOnMCPLoad` folds into the server as a flag on it.
- **`AlwaysAllow`** is the fourth answer of ADR 0040 written down by the app instead of by a user's click: a tool named here runs without asking, for every Conversation, on every computer running that app. It is appended to the policy table after the user's own entries, so a user who denies a tool still wins.
- **`Section`** is ADR 0038's extension `section`. Anlan Droi's OpsLens guide lands here, after Settings' own text and never replacing the Kernel's prompt.

**What is deleted:** the hooks mapping, `host.OverlayOptions`, `host.Setting`s-from-a-file thinking and `pkg/host/droidhook`. A droid hook is a shell command run at an event of droid's own; the Kernel has no such event to run it at, and mapping one would put an app's command somewhere it was never written for. An app that wants that behaviour ships a Kernel extension (TypeScript, `apps/kernel/src/extensions/`) and names its path in the `Agent`; the field for that path arrives with the first app that needs one, not before. Neither downstream app uses hooks, so this costs nothing today, and it is written down so nobody assumes the seam survived.

Two more things follow, and both are the user's decision:

- **A downstream app is not migrated in 2.0.** Anlan Droi stays on Droi 1.90.0 and WeCom Droi on 1.55.0, each running the droid Daemon it was built for, until it is upgraded on its own afterwards.
- **Droi's public packages may break in 2.0.** The module becomes `github.com/kkkk2323/droi/apps/native/v2` and the tags `apps/native/v2.x.y`; the `CHANGELOG` says what an app must change. **The rename is the last step before 2.0 merges into `main`**, not an early one: it rewrites every Go import in this repository (265 files, 657 import lines), so done during development it would conflict with `main` on every merge for the whole project, and it changes no behaviour to be worth that. `extension`, `droiapp`, `brand`, `defaults`, `kit`, `l10n`, `sessionview`, `keypool` and `mcpserver` stay public, and `host` keeps what a downstream app needs of it (Key Pool, data folder, executable, model tools) and loses the Daemon's parts.

## What Anlan Droi changes when it upgrades

It is the measuring stick, so it is written out here; the plan for it is in `phased-plan.md` §7.1, after 2.0.

| Now | Then |
|---|---|
| `Overlay: a.toolsOverlay` returning `host.OverlayOptions{MCPServers: {...}, BlockOnMCPLoad: true}` (`internal/anlan/tools.go:72`) | `Agent: a.agent` returning `extension.Agent{MCPServers: ..., AlwaysAllow: s.Quiet, Section: a.toolsPrompt()}` — the file it changes most, and the only one with real work in it |
| `SystemPrompt: a.toolsPrompt` | folded into `Agent.Section` |
| `Overlay`'s `Quiet` list (the tools OpsLens marks not to ask about) | `Agent.AlwaysAllow` |
| `SignIn` calling `h.SetAPIKey` and `keypool` add/remove (`internal/anlan/sync.go`) | the same calls against the 2.0 Key Pool API, whose shape ADR 0039 settles; `SetAPIKey` still means "the pool now has this key" |
| `Commands`, `ToolViews`, `SettingsTabs`, `Chinese`, `Start` | unchanged, except `ToolViews`' key becomes the Kernel's name for the tool |
| `OwnFactoryHome`, `DroidDownloads`, `Presets.ToolMode` | removed; `Presets` keeps model, effort and compaction limit |
| `scripts/mirror-droid.sh` | no longer read by anything |
| its `droidhook` import, if any | deleted with the package |

## Considered options

- **Drop `droiapp.Run` and the `Extension` in 2.0, and let an app fork Droi.** Rejected: Anlan Droi is a shipped company product, and forking turns every Droi fix into a merge done by hand.
- **Keep `Overlay` and `SystemPrompt` as they are and give them Kernel meanings.** Rejected: an overlay is a settings file for a program that no longer exists, and the three things an app actually puts in it are an MCP list, a permission list and a prompt section, which are three Kernel features, not one file.
- **Map hooks by running them from a Kernel extension.** Rejected: pretending to support a hook would run an app's command in a place it was never written for.
- **Migrate Anlan Droi inside 2.0.** Rejected by the user: it would tie a company release to Droi's biggest change.

## Consequences

- **The `Extension` literal of a downstream app changes in exactly two fields** (`Overlay` and `SystemPrompt` become `Agent`) plus whatever `Env.Controller`'s new type breaks, which is the right way for an app to find out.
- **An app can no longer contribute droid settings, environment variables or custom models.** It contributes Kernel features instead: MCP servers, allowances and prompt text. A model that is not Factory's is a pi-ai provider (ADR 0038), configured by the app or the user, not by an overlay entry.
- **`AlwaysAllow` is an app-level allowance, not a user setting**, so it is shown in Settings → Autonomy as one of the entries and can be revoked there for that computer.
- **Both downstream apps stay on their pins until upgraded**, which is safe because each carries the copy of the droid-side Droi it builds against.

## What this ADR does not decide

- **How the Kernel names an MCP tool**, which `AlwaysAllow` and `ToolViews` both depend on (M3).
- **Whether an `Agent` may name a Kernel extension's path.** The field arrives with the first app that needs one; neither does today.
- **When Anlan Droi is upgraded**, and what its 0.15.0 contains beyond the port itself (its own repository and release).

## Where it stands (M4 4Q, 2026-11-08)

- `extension.Agent` exists beside `Overlay` and `SystemPrompt`, which keep reaching the Daemon until it is removed (M5); `droiapp` joins every extension's `Agent` and the Host passes it to each Kernel launch in `DROI_KERNEL_AGENT`. An app that gives no `Section` has its `SystemPrompt` in the Kernel's prompt instead, so it runs on the Kernel before it is ported.
- The Kernel names an MCP tool `<server>___<tool>` (`apps/kernel/src/mcp/servers.ts`), the name `ToolViews` already used, so their keys do not change.
- The app's servers replace a user's or a repository's of the same name. `AlwaysAllow` is judged after the user's rules and never stored as theirs. Kernel rules only allow, so "a user who denies a tool still wins" has nothing to deny with yet, and revoking an app's allowance in Settings → Autonomy comes with the Kernel's Autonomy page in M5.
- `Env.Kernel` gives a page the window's Kernel connection; `Env.Controller` stays until M5. `OwnFactoryHome`, `DroidDownloads` and `Presets.ToolMode` go with the Daemon in M5.
- Hooks are not mapped (the `Agent` Godoc says so).
