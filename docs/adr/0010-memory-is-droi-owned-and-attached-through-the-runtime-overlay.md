---
status: accepted
---

# Memory is Droi's own capability, attached to its Daemon through the Runtime Overlay

Users wanted Droid to remember across Sessions: what they prefer, what a Workspace's conventions are, what went wrong before. The Daemon has no such feature. Existing tools (claude-mem, pi-hermes-memory, memem, pmb) all converge on the same shape, a local store plus lifecycle hooks, but each is bound to another agent's plugin layout and none targets the Daemon.

We make Memory a Droi feature that ships with the Desktop Shell and applies only to Droi's Daemon. `droid daemon --settings <path>` takes a Runtime Overlay: a settings file merged for that process alone, which accepts `mcp.mcpServers`, `mcp.persistentPermissions`, `hooks` and general keys such as `blockOnMcpLoad` and `mcpAutonomyOverrides` (droid 0.229.0, `RuntimeSettingsOverlay.ts`). The Shell writes that file when the Memory setting is on and passes it at spawn; the droid CLI's `~/.factory/mcp.json` and `hooks.json` are never touched, and a Daemon started without the overlay has no Memory at all. Turning the setting off or on restarts the Daemon, the same path a credential change already takes.

The overlay attaches two things. The **Memory Server** is a stdio MCP Server bundled with the Shell and run by Electron's own Node (`ELECTRON_RUN_AS_NODE`), so it needs no system Node and no native module: `node:sqlite` with FTS5 (trigram, with a `LIKE` fallback for queries under three characters) is built into Electron 44. It offers `memory_search`, `memory_list`, `memory_add`, `memory_replace` and `memory_remove`; the two readers carry `readOnlyHint`, and the server is pre-approved through `persistentPermissions` so a Session never stops to ask. **Hooks** do what the model cannot be trusted to remember on its own: `SessionStart` (also after a compaction) injects a short policy on when to search plus the `correction` entries, which are the ones that must not be missed; `UserPromptSubmit` detects a correction in the user's words and nudges every ten turns; `PreCompact` and `SessionEnd` fall back to extracting from the transcript when a Session of six or more turns wrote nothing (ADR 0011).

Droid writes Memory itself, in the Session, with the tools. That is the agent-native half of the design and the reason the Memory Server exists at all: the model in the conversation knows why something happened and can replace or remove an entry that is no longer true, which no after-the-fact summariser can. The hooks are the deterministic half: recall and saving are salience problems, not obedience problems, and every tool we studied that gave the model a memory tool also added nudges and a flush.

Memory is stored in SQLite under the Shell's user data, one Project Memory per Workspace (keyed by the Workspace path) and one Global Memory. SQLite is the source of truth because every Session runs its own Memory Server process and they write concurrently; a read-only Markdown export sits beside it for people and Obsidian. Entries carry an 8-character id, a category (failure, correction, insight, preference, convention, tool-quirk) and a day. Sizes follow a heavy user's real pi-hermes data (a 245k-character project): 200k characters per Project Memory is the point at which Settings suggests consolidation, 300k the point at which writes are refused; Global Memory is 40k and 60k; the always-injected `correction` slice is 20 entries or 2,000 characters.

## Considered Options

- **Install an existing plugin (claude-mem, memem).** Rejected: their hook scripts assume Claude Code's paths and transcript format, claude-mem needs Bun, a resident worker and Chroma, and none can be scoped to one Daemon.
- **Capture every tool call and compress it (claude-mem).** Rejected: high volume, low signal, append-only, and a second model call per session by design.
- **Extract from the transcript at session end only.** Rejected as the primary path: the summariser sees what happened but not why, and can never correct a stale entry. Kept as the fallback.
- **Hooks in `~/.factory/hooks.json`, or a droid plugin.** Rejected: both apply to the CLI too, which the user did not want, and both put Droi's files in the CLI's configuration.
- **Memory in the Client or the Gateway.** Rejected: Clients keep no conversation state (ADR 0001), the Gateway sees only `initialize_session` and not compaction handoffs (ADR 0007), and a Remote Client would have to send transcripts to the Shell. Hooks run where the transcript already is.
- **Inject the whole Memory at session start.** Rejected once the cap was allowed to grow past a few thousand characters; pi-hermes moved to policy-only for the same reason. The `correction` slice stays injected because a missed prohibition is the one failure the user notices.
- **Markdown as the source of truth with a rebuilt index.** Rejected: concurrent Memory Server processes would need their own locking, and hand edits would need a watcher and a forgiving parser. SQLite has all of it.

## Consequences

- Sessions started from the droid CLI neither read nor write Memory. The Memory Server and hook code are portable if that ever changes.
- The Phone App gets Memory for free: it talks to the same Daemon. Only the Local Client can edit the Memory settings, like every other Shell setting.
- The Memory Server appears in the MCP panel beside the user's servers, marked as Droi's and without a switch; the Memory setting is its only switch.
- `blockOnMcpLoad` is on for Droi's Daemon so the first turn cannot start before the Memory Server is connected.
- The Memory Server learns the calling Session only from the id the Daemon puts in each call's `_meta` (`assemblySessionId`, the Session id itself) and reads that Session's Workspace and tags from the files the Daemon keeps under `~/.factory/sessions`. Subagents see the same tools and hooks and may write. A Memory Session (ADR 0011) runs on the same Daemon, so the hooks stay silent for it and the server refuses its calls.
- A Workspace moved or renamed leaves its Project Memory behind under the old path.
- A Scratch Session (ADR 0008) has no Project Memory: its folder is made for it and trashed with it, so nothing keyed by that path would ever be found again. The server refuses a `project` write there and steers facts about the user or their environment to Global Memory, a `project` read answers that there is none, the hook injects only Global corrections, and an extraction from such a Session keeps only its `global` entries. The Session is known by its `droi.scratch` tag, which a compaction's child inherits.
- Consolidation is manual, from Settings → Memory, and runs in a hidden Session (ADR 0011).
