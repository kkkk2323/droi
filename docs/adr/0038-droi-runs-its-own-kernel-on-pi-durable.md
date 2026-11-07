---
status: accepted
---

# Droi runs its own Kernel: Pi Durable replaces the droid Daemon

Droi is a thin Client of `droid daemon`, Factory's agent process. Everything awkward in this repository is a patch over that: the Gateway rewrites protocol frames to inject the credential and the System Prompt Addition (`pkg/gateway/frames.go`), the Host generates a `--settings` overlay the Daemon was never meant to take, a Key Hook is preloaded into a Bun binary to choose an LLM key (ADR 0033), a background process is read out of the text droid writes for the model (`pkg/transcript/background.go`), a queued AskUser question is recognised by a message-id prefix, a Session's Loops are kept alive by holding its worker loaded (ADR 0023), and when the user last wrote in a Session is sniffed from protocol frames into a JSON file (ADR 0032). Each of those exists because the Daemon's protocol is fixed and Droi may not change it, and each gets a new shape whenever Factory changes its side.

The shape also costs. droid runs one worker process per Session at about 290 MB. Measured on one Mac (M4 Pro, 14 cores, 2026-11-07) with one scripted model and a cold start: ten idle Sessions held 2871 MB under droid and 122 MB under Pi Durable, whose marginal Session costs 5.6 MB; idle at start was 251 MB against 71 MB; a hundred `read` tool calls took 0.71 s against 0.40 s. droid's first request carried 12 tools and 41.5 KB, Pi Durable's 2 tools and 3.0 KB. Cold start was not a difference that matters (0.31 s against 0.11 s).

We decided that the agent process is Droi's own, and that it is [Pi Durable](https://www.npmjs.com/package/@earendil-works/pi-durable):

- **One Kernel, no second one.** Droi ships `apps/kernel`, a TypeScript sidecar built on `@earendil-works/pi-durable` pinned at exactly `1.5.0` (its releases break the API in minor versions), with the coding tools ported from `@earendil-works/pi-coding-agent` `1.5.0`. Support for `droid daemon` is deleted, not kept beside it: there is no fallback kernel, no setting that chooses one, and no protocol translation layer. A Session is a Conversation the Kernel owns, from the first 2.0 build.
- **The version comes with the app.** The Kernel is Droi's binary, compiled by Droi's release for each target, so a Pinned Droid, a Minimum Droid and a check of `droid --version` have nothing left to pin or check. Those three concepts go (ADRs 0013, 0018, 0033).
- **The Clients see the Kernel's protocol, not a translation of the Daemon's.** Methods are named after Pi's concepts — `conversation.*`, `submission.*`, `task.*`, `approval.*`, `process.*` — 22 methods in all, and 9 notifications. WebSocket with JSON-RPC 2.0 stays, for two reasons that are not "it is what we have": the Relay already carries a WebSocket byte for byte (`internal/relay/agent.go` forwards HTTP and WebSocket alike, so it needs no change), and every approval and question is answered on the same connection that carried the request. REST with SSE was the alternative; it would have meant redoing the Relay's streaming path and the Phone App's connection layer, about 1.5 person-weeks, neither of which has anything to do with the kernel.
- **A Client sees one thing: `conversation.view`.** A view plus its incremental ops, the way Pi's `watch` hands them over, and a handful of Droi events beside it (a pending approval, a task, a background process). Today's 41 notification types go. Clients consume only `value` in the first version and ignore `ops`: a Window holds a hundred entries and redraws them, and the retire/splice semantics of `ops` are not worth a subtle bug for one redraw.
- **The Kernel owns one SQLite, and Droi's metadata lives in it.** Pi writes its tables under a `pi_` prefix; Droi's list, its full-text index, its policy and its key-value settings sit beside them in the same file, so "when did the user last write here" commits in the same transaction as the entry that changed it. One writer, one connection, for reasons the Windows measurements gave (a second connection blocks the event loop for 5.5 s on a lock wait; a commit costs 15.6 ms on NTFS with WAL).
- **The Gateway stops rewriting frames.** The credential no longer passes through it (it is in the LLM Proxy, ADR 0039), the System Prompt Addition is an extension `section` the Kernel evaluates, and the disabled-tool list is `conversation.configure({modelTools})`. The Gateway keeps what is genuinely its own: the Pairing Token and Local Token, the Relay bridge, the pass-through to the Kernel, and its REST answers for `/meta`, Scratch Workspaces and session files. `pkg/gateway/frames.go` is deleted.
- **Go keeps what is not the agent.** The sidecar does not touch git, Factory credentials or the Memory files, and it holds the only SQLite connection. The Host keeps the Key Pool, the LLM Proxy, Memory, the Automations scheduler, Workstreams and the process table.
- **The Kernel is supervised, never trusted to stay up.** The Host spawns it with a random port and token and a data folder, reads the port from its first stdout line, polls `/health` until it answers, and restarts it on a 1/2/5/10/30 s backoff. A storage failure is fatal: `harness.closed` ends the process and the window says the Kernel failed, because that is the one failure the Kernel cannot work around. On start the Kernel reconciles what a crash left behind: an approval or a question still `pending` becomes `abandoned`, and a `droi.process` entry still `running` whose pid is gone becomes `exited`.
- **Packaging treats the Kernel as its own file with its own version.** `bun build --compile` per target (`--target=bun-darwin-arm64`, `bun-windows-x64`, `bun-linux-x64`, `bun-linux-arm64`), about 61 MB on macOS arm64 and 99.4 MB for Windows when cross-compiled (38 MB gzipped). It ships beside the app binary rather than merged into it, so a Go fix does not resend 38 MB, and its version is `kernel-<piVersion>-<our revision>`.
- **Droi's own capabilities become Kernel extensions.** Memory is four tools (`memory_search`, `memory_read`, `memory_list`, `memory_note`) and a prompt `section`, in the Kernel's own process, with no MCP child process and no start hook (ADR 0010 amended). Skills and MCP Servers are extensions that read `<cwd>/.factory/skills`, `<cwd>/.factory/mcp.json` and the `AGENTS.md` files where they lie, and `~/.droi` once copied from `~/.factory`. codemode is an extension and is on by default, and its nested tool calls are listed inside the card of the call that made them. Workstreams stay Droi's own (ADRs 0036, 0037): their tools are extension tools that call the Host, not an MCP Server Droi carries.
- **A Loop becomes an Automation that targets an existing Conversation.** There is no worker to keep loaded and no `daemon.list_crons`, so ADR 0035's scheduler gains a `target` of `new` or `existing` and is the only schedule Droi runs.
- **The platform decides the shell.** Pi's `NodeExecutionEnv` looks for bash on Windows and finds WSL's, where the working directory, `HOME`, git and ssh keys are all someone else's and every command takes 4–7 s instead of 58 ms. Windows therefore gets `createPowerShellTool()`, and `bash` only when Git Bash is installed, with `shellPath` given explicitly. macOS and Linux keep `bash`.
- **Images are created the way that survives cross-compilation.** The Windows build is cross-compiled on Ubuntu, and there `createNodePhotonImages()` cannot resolve `@silvia-odwyer/photon/photon_rs_bg.wasm` even though the bytes are in the binary. `createPhotonImages(readFileSync(wasmPath))` with the wasm committed next to the source works in every target, and a test reads a PNG in the cross-compiled binary rather than checking that a file exists. The wasm is Apache-2.0 and the installers carry its NOTICE.

## The protocol

22 methods, and what replaces each of today's calls:

| Method | Answers | Today |
|---|---|---|
| `harness.info` | `{version, providers[], models[], capabilities}` | `daemon.*` capabilities, `/meta` |
| `conversation.create` | `{conversationId, view}` | `daemon.create_session` + `initialize_session` + `load_session` |
| `conversation.list` | `{items[], cursor}` | `daemon.list_sessions` (Droi's own table) |
| `conversation.open` / `.close` | `{view}` / `{}` — attach and detach; the Conversation stays | `load_session` / nothing |
| `conversation.submit` | `{submissionId}` | `daemon.add_user_message` |
| `conversation.abort` / `submission.abort` | `{status}` | `stop_session`, `resolve_queued_user_message` |
| `conversation.configure` | `{agent}` as resolved | `update_session_settings` |
| `conversation.compact` | `{taskId}` | `compact_session` (which returned a new Session) |
| `conversation.fork` | `{conversationId}` | — (new) |
| `conversation.entries` | `{items[], cursor}` | `daemon.get_session_messages`, paged |
| `conversation.usage` | `{models, tools, context}` | `get_context_breakdown` + `get_session_usage` |
| `task.abort` | `{status}` | `stop_session` on a subagent |
| `process.list` / `.stop` | `{processes[]}` / `{status}` | `KillBackgroundProcess`, parsed from text |
| `approval.respond` / `ask.respond` | `{status}` | permission prompts, `ask_user` answers |
| `mcp.list` / `.set` / `.authorize` / `.authorize.cancel` | MCP state | `daemon.*` MCP calls |
| `skills.list` / `.setDisabled` | Skills | `daemon.*` skill calls |
| `index.update` | `{}` — title, archived, kind | `rename_session`, `archive_session`, `droi.*` tags |

Nine rows of notifications, two of them a request and its resolution: `conversation.view`; `conversation.busy` (the run, or none, with a failure); `approval.requested` and `approval.resolved`; `ask.requested` and `ask.resolved`; `task.changed`; `mcp.changed`; `process.changed`; `index.changed`; and `warning`, which carries the Kernel's own reports (a failed write, an extension that threw).

An entry is Pi's six kinds — `pi.user`, `pi.assistant`, `pi.system`, `pi.tool-result`, `pi.reset`, `pi.compaction` — plus five of Droi's: `droi.approval`, `droi.ask`, `droi.subagent`, `droi.process` and `droi.note`. Those five are written with `model` left empty, so they are shown and never sent. `droi.approval` and `droi.ask` replace a Prompt that lived only in notifications; `droi.subagent` replaces `callingSessionId`/`callingToolUseId`; `droi.process` replaces a background process parsed from text; `droi.note` replaces the loose notifications a Session used to get. `droi.spec` is not needed: a plan is a `pi.assistant` entry with a tool call in it.

## Where things live

```text
apps/kernel/                      the sidecar (TypeScript, Bun)
  src/main.ts                     arguments, single-instance lock, SQLite, port, stdout handshake
  src/protocol/                   JSON-RPC frames, method routing, error codes
  src/session/                    Conversations, the attach table, watch → conversation.view
  src/indexer/                    droi_conversations and FTS in the same database
  src/extensions/                 coding, skills, commands, mcp, approval, ask-user, plan,
                                  subagent, memory, codemode, background
  src/storage/bun-sqlite.ts       the SqliteDatabase facade
  src/images.ts                   Photon, with its wasm
  build.ts                        the four --target builds
  protocol/fixtures/              the JSON examples both sides test against
apps/native/pkg/kernel/           the Go client for those 22 methods
apps/native/pkg/host/supervisor.go  spawns, health-checks and restarts the Kernel
packages/daemon-layer/src/kernel/   the Phone App's client, the same protocol
```

## What this deletes

Each of these was written to work around the Daemon, and has nothing to work around any more: `frames.go`'s frame rewriting; the Runtime Overlay (`overlay.go`) and `customModels` in a settings file; the Key Hook and `pkg/host/droidhook`; Droi's droid, Pinned and Minimum Droid (ADRs 0013, 0018, 0033); the Draft Session and its `droi.draft` tag (Skills and commands are files, and `harness.info` lists them); the `droi.*` tags that held Droi's own metadata, which become columns of `droi_conversations`; the compaction continuation chain, since Pi inserts a `pi.compaction` entry in place; the Script models and the `/api/feature-flags` rewrite (ADR 0028), because there is no Script — codemode is an extension and is on by default, its nested tool calls listed inside the outer card; the "keep the worker loaded so its Loops fire" dance (ADR 0023), since Loops join the Automations scheduler; the text parsing of background processes, now a structured `droi.process` entry (ADR 0040); Session Times as a store of their own, now entry timestamps plus one `viewedAt` column; and the Factory login and the multi-account folders (ADRs 0005, 0015, 0017), which ADR 0039 replaces with keys.

Two things stay as they are, deliberately: the Relay (ADR 0034) carries the Kernel's WebSocket with no change, and every Client still reaches a Host only through its Gateway (ADR 0001).

## Considered options

- **Keep `droid daemon` and wrap it better.** Rejected: it keeps every workaround above and still costs 290 MB a Session.
- **Run both kernels and let the user choose.** Rejected by the user: two protocols, two sets of bugs, two test suites, and no way to say which one a Session is in without a flag on every screen.
- **The Pi SDK instead of Pi Durable.** Rejected: the SDK's session manager keeps its state in memory, so a restart loses the Conversations Droi is expected to keep; Pi SDK's marginal Session was 7.2 MB against 5.6 MB.
- **REST with SSE.** Rejected for now: see the transport bullet above. If the Relay is ever retired, this should be reconsidered on its merits.
- **Two SQLite files, one for Pi and one for Droi.** Rejected: Pi's `SqliteStorage` takes a prefix and shares a database, and metadata that commits with its entry cannot be split across two writers.
- **Node or Deno as the runtime.** Rejected: a Node executable alone is 81.6 MB on Windows before the code, so a Bun-compiled binary is no bigger, and Pi leans on `worker_threads` and `bun:sqlite`.

## Consequences

- **2.0 is a breaking release.** Sessions do not carry over by themselves: a one-time importer, which the user drives by choosing which Sessions to bring, is the only path from 1.x (a later ADR covers it). Auto-update pauses once to explain the change.
- **Roughly 14.6 person-weeks**, about 9 weeks on the critical path with three people working in parallel.
- **The Kernel is a new maintenance surface**, and Pi Durable is a dependency Droi does not control. The pins are exact, the conformance suites and protocol fixtures catch a bad upgrade, and the code Droi owns stays in `apps/kernel`.
- **Tests change shape.** The Fake Daemon of today's view tests becomes a real Kernel started with a scripted model (`fakekernel`), so a test still needs no Factory key but can no longer inject a protocol bug that no longer exists. Pi's `fauxProvider` does not fire the request-shaping hook, so tests of the Factory request shape use a real provider against a local fake HTTP server.
- **The Windows build gains Authenticode signing**, for the app and for the Kernel beside it, or SmartScreen warns about "an app starting an unsigned program".

## What this ADR does not decide

- **How Magpie's models reach the model picker.** The Runtime Overlay and `customModels` are gone (ADR 0024 is superseded), and pi-ai providers are configured natively; whether a Magpie model is one of them, and how it is added, is undecided.
- **WeCom Droi** (ADR 0022), which runs its own Daemon and imports `pkg/host`. It keeps working only as long as what it imports does; migrating it to the Kernel is a separate piece of work with its own decision.
- **The Phone App's timing.** It speaks the Kernel protocol like the Window (ADR 0006 amended), and when it is switched over is a matter for the plan, not a different protocol.
