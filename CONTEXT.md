# Droi

Droi is a desktop, iPhone and mobile-web front end for the Factory Droid coding agent. It does not run the agent itself; it presents and controls agent conversations that live in the Droid Daemon.

## Language

### Processes

**Daemon**:
The `droid daemon` process that owns every Session, its transcript, and all filesystem and Git operations. Droi never re-implements what the Daemon already does.
_Avoid_: backend, server, engine, exec runner

**Host**:
The process on a computer that starts and watches over the Daemon, runs the Gateway, supplies the Factory credential (normally the droid CLI's own login on that computer, an API key as fallback) and keeps that computer's Memory. The Desktop Shell embeds one; on a computer without a Desktop Shell (a headless Linux box) it runs on its own as `droi host`, the same Droi program without a window, kept alive by the system's service manager. Every computer a Client can reach has exactly one Host.
_Avoid_: server, node, station, backend, agent runner

**Factory Account**:
A Factory login the Host can run its Daemon as. The first is the droid CLI's own, in `~/.factory`; each one added in Droi keeps its login in a folder of its own and shares the CLI's skills, droids, MCP Servers, settings and Sessions. The Daemon runs as one at a time, and switching restarts it.
_Avoid_: profile, key, provider instance

**Minimum Droid**:
The oldest `droid` build a Droi release starts its Daemon from. The Host reads `droid --version` before every start; an older droid does not start, and the window offers to update it with `droid update`.
_Avoid_: required droid, droid version (on its own)

**Pinned Droid**:
The one `droid` build a Droi release would be made for (ADR 0013); not built yet, deferred in favour of the Minimum Droid (ADR 0018). The Host would fetch it and start the Daemon from it, so the Daemon, the Host and every Client of that release speak the same protocol; the droid CLI the user runs from a terminal is a separate install and may be newer. When the Pinned Droid could not be fetched the Host would fall back to the computer's own `droid` and say so.
_Avoid_: bundled droid, vendored CLI, droid version (on its own)

**Patched Droid**:
Factory's own `droid` build, downloaded by the Host and changed in one place by a Recipe so that each LLM request carries a key from the Key Pool, while everything else keeps the droid's own login (ADR 0027). Opt-in; the Daemon runs it instead of the computer's droid. It does not update itself; Droi offers the newer one when a newer Recipe exists.
_Avoid_: modified droid, custom droid, Pinned Droid

**Recipe**:
What patching one droid version takes: the version, the platforms it was verified on, the checksum of the code it changes, where to change it and what to put there. Droi ships some and reads newer signed ones that a scheduled workflow publishes.
_Avoid_: patch file, patch config

**Key Pool**:
The Factory API keys Droi keeps for the Patched Droid's LLM requests, one of them current. In Automatic mode Droi moves to another key when the current one runs out; in Manual mode it keeps the one chosen. Kept in Droi's own data, apart from droid-proxy's.
_Avoid_: key list, key rotation, accounts (a Factory Account is a login, not a key)

**LLM Proxy**:
The Key Pool's local proxy the Patched Droid sends its Factory traffic through. It forwards requests unchanged, except that the reply at `/api/feature-flags` also names the models Settings lets use Script (ADR 0028). It records requests, their events and each key's quota for Settings → Usage; it also keeps prompt caches warm. Not the Gateway, and not droid-proxy.
_Avoid_: dp, gateway, MITM

**Desktop Shell**:
The installed desktop application. It embeds a Host and opens a window for the Local Client. It holds no conversation state.
_Avoid_: main process, backend, app

**Brand**:
What names the app built from Droi's packages: its name, the short id of its folders, the Gateway's port and its mark. Droi's own brand is Droi; another app runs `droiapp.Run` with its own (ADR 0029). The Gateway answers `/meta` as Droi whatever the brand.
_Avoid_: white label, skin, theme

**Extension**:
What an app built on Droi adds to it through `droiapp.Run`: MCP servers and hooks for the Runtime Overlay, words for the System Prompt Addition, subcommands, pages, Settings sections, a sign-in page of its own, and how tool calls and json-render elements show (ADR 0029). It plugs into seams Droi has; it does not patch Droi.
_Avoid_: plugin, add-on, module
**Gateway**:
The part of the Host that lets a Client reach the Daemon. It checks the Pairing Token and supplies the Factory credential (the Host's Factory login token, or an API key as fallback) so it never leaves the computer. It also adds the Host's System Prompt Addition to every Session a Client starts, creates and trashes Scratch Workspaces, and answers a Client's requests to restart the Daemon or update the Host.
_Avoid_: proxy, API server, Hono server, web server

### Clients

**Client**:
A Droi user interface. Every Client speaks only the Daemon protocol, reaches the Daemon through a Host's Gateway, and keeps no conversation state of its own. There are two: the Desktop Shell window and the Phone App.
_Avoid_: renderer, frontend, web UI

**Local Client**:
The Client in the Desktop Shell window, whichever Paired Computer it is connected to; the window follows one Paired Computer at a time, this computer's own Host unless the Computer preference names another. It alone can reach the Desktop Shell itself (settings, system notifications, opening folders in other apps); it reaches its own Host's Gateway with a per-launch token and any other Host's Gateway with that Host's Pairing Token.

**Remote Client**:
A Client that is not the Desktop Shell window of the Host it is connected to: the Phone App on an iPhone. It reaches the Host through the Gateway with the Pairing Token.
_Avoid_: mobile, web mode, browser mode, LAN mode

**Phone App**:
The native iPhone Client. It is the Remote Client, and it offers nothing the desktop lacks.
_Avoid_: mobile app, iOS app, iOS Client

**Pairing Token**:
The secret a Remote Client presents to the Gateway to prove it was authorised on the Host's computer. There is one token per Host, shown as a QR code and link in the Desktop Shell settings or printed by the Host's command line; resetting it revokes every Remote Client at once. The Local Client presents a separate per-launch token to its own Host instead, so a reset never touches the desktop window.
_Avoid_: API key or login token (those are Factory credentials, which Clients never hold), device token

**Paired Computer**:
A Host a Client has been paired with and remembers: one Gateway address, the Pairing Token, and the computer's name and id as the Gateway reports them. Pairing the same computer again updates it rather than adding another. A Client is connected to one Paired Computer at a time. The Desktop Shell's own Host is the first Paired Computer of its Local Client and cannot be forgotten.
_Avoid_: host (that is the process; this is a Client's record of one), daemon profile, server, device

**Remote Access**:
The Host setting that decides whether the Gateway accepts Remote Clients at all. Off by default in a Desktop Shell; always on in a Host running on its own, which has no Local Client.
_Avoid_: LAN mode, web mode, mobile mode, `DROID_WEB_ENABLED`

**Runtime Overlay**:
The settings the Host hands its Daemon at start, which apply to that Daemon alone and never reach the droid CLI's own files.
_Avoid_: settings override, daemon config, `--settings` file

### Memory

**Memory**:
What Droi keeps on a computer of the user's earlier Sessions for Droid to draw on in later ones: the Memory Summary, the Session Summaries and the Memory Notes, in one folder. The Daemon knows nothing of it; the Host keeps it, and each Host keeps its own.
_Avoid_: context, knowledge base, history, Project Memory, Global Memory

**Memory Summary**:
The short document every Session on Droi's Daemon starts with: who the user is, how they like to work, and pointers to the Session Summaries of recent work.
_Avoid_: memory file, profile, MEMORY.md

**Session Summary**:
The record of one earlier Session, written from its transcript once the Session has been left for a while.
_Avoid_: rollout summary, recap, extraction

**Memory Note**:
What the user explicitly asked Droid to remember, forget or correct, kept as they asked it until the Memory Summary takes it in.
_Avoid_: memory entry, fact, correction

**Memory Server**:
The MCP Server Droi carries and attaches only to its own Daemon, through which Droid reads Memory and saves Memory Notes. It is not one of the user's MCP Servers: the Memory setting is its only switch.
_Avoid_: memory MCP, memory plugin, memory tool (a tool is one of the things it offers)

**Memory Session**:
A Session the Host opens on the Daemon for Memory work of its own, writing a Session Summary or updating the Memory Summary, with no user in it. It carries the `droi.memory` tag, which keeps it out of every Client's session list, and is archived when its turn ends.
_Avoid_: hidden session, background session, consolidation job

### Testing

**Fake Daemon**:
A scripted stand-in for the Daemon used by end-to-end tests. It speaks the Daemon protocol but replays a Scenario instead of running an agent.
_Avoid_: mock server, stub, recorder

**Scenario**:
The per-test script that tells the Fake Daemon what to answer and when to emit events.
_Avoid_: fixture data, recording, replay log

### Conversations

**Session**:
One conversation with the agent, owned and persisted by the Daemon and identified by the Daemon's session id.
_Avoid_: chat, thread, worker, droi session file

**Draft Session**:
A Session the New session page opens as soon as it knows the Workspace, so the composer can offer that Workspace's skills and commands before anything is sent. It carries the `droi.draft` tag, which keeps it out of every Client's session list, until the first send takes it over; an abandoned draft is closed, and the Daemon deletes it.
_Avoid_: pre-init session, placeholder session

**Subagent**:
A Session the Daemon starts for a Task tool call; the Daemon lists it with its calling Session and tool call (`callingSessionId`, `callingToolUseId`). Clients keep it out of the session list and reach it from the Session that called it: the Task call's card, the header's subagent menu, and the trail back.
_Avoid_: child session, sub-session, task session

**Session Defaults**:
What every new Session on a computer starts with: model, reasoning, interaction mode, autonomy, spec mode, compaction and subagent models. The Daemon keeps them in `~/.factory/settings.json`, shared with the droid CLI and the Factory App; every Client edits them through the Daemon.
_Avoid_: preferences, global settings, default settings (on their own)

**System Prompt Addition**:
Text the Host keeps and the Gateway appends to Droid's own system prompt when a Session starts, whichever Client starts it. It never replaces Droid's prompt, and a Session keeps the one it started with.
_Avoid_: system prompt override, custom instructions

**Custom Model**:
A model droid runs that is not Factory's, from `customModels` in `~/.factory/settings.json`: a base URL, an API key and a provider dialect, which is droid's own BYOK (bring your own key). A Session picks one like any other model, and it has no Factory usage multiplier. Clients show them; only the user and the Host add them.
_Avoid_: BYOK model, external model, third-party model (on its own)

**Magpie**:
A local gateway (yetone's Magpie, listening on 127.0.0.1:3425) that runs coding agents on the models of many providers behind one endpoint. Droi does not run it and sends it no request of its own apart from the model list: the Host reads that list and hands the models to its Daemon as Custom Models in the Runtime Overlay. The droid CLI's own settings and the CLI itself do not see them.
_Avoid_: proxy, provider, model router (a Router is Factory's), gateway (that is Droi's own)

**Skill**:
A `SKILL.md` folder the Daemon offers Droid, and the user through "/": built into the droid CLI, personal (`~/.factory/skills`), or the Workspace's (`.factory/skills`). The Daemon lists a Session's skills and writes the switches: off across all projects (`~/.factory/settings.json`) or off for one Workspace (`.factory/settings.json`); the organization's switches are read-only. A personal skill is managed by its files. Droi gives each skill one switch: a built-in one writes across all projects, the Workspace's writes for that Workspace.
_Avoid_: plugin, extension, prompt template

**MCP Server**:
A tool server the Daemon connects a Session to, from the user's `~/.factory/mcp.json`, the Workspace's `.factory/mcp.json` or the organization's policy. The Daemon reports each one's connection and tools, switches them, adds and removes the user's own, and runs a sign-in whose page a Client opens and whose callback the Daemon takes on the computer.
_Avoid_: connector (Factory's hosted integrations are something else), tool, integration

**Automation**:
A prompt the Daemon runs on a schedule on its computer, kept in `~/.factory/automations/<id>/` and shared with the droid CLI and the Factory App. Droi lists, runs, pauses, edits and creates the local ones; the Factory App's cloud kinds (Slack, webhook, a droid computer) are not Droi's.
_Avoid_: cron, job, scheduled task, workflow

**Automation Run**:
One run of an Automation: a Session the Daemon starts with the `automation` tag, without asking for permission, and closes once idle. Clients list it on the Automations page, not with the other Sessions.
_Avoid_: job run, execution, automation session

**Loop**:
A prompt the Daemon sends to the same Session on a schedule, as droid's `/loop` and Loop tool make it (a cron of kind `session_prompt`). It fires only while the Session has a worker; Droi shows a Session's Loops above its composer and keeps that worker running for them.
_Avoid_: cron, scheduled loop, Automation (that one starts a new Session each run)

**Workspace**:
The directory on the computer that a Session operates in.
_Avoid_: project, project dir, cwd, repo

**Scratch Workspace**:
A Workspace the Host creates for a Session started without choosing one, for work that belongs to no project. The Sessions that continue it after a compaction share it; archiving the conversation moves it to the Trash. Clients list these Sessions together, under Recents.
_Avoid_: chat, temp dir, draft (a Draft Session is something else)

**Worktree**:
A git worktree the Daemon creates for a Session started with "New worktree": its own directory and a new `droid/<slug>` branch, cut from a base branch of the repository. It is the Session's Workspace, and Clients list the Session under the repository it came from. An ephemeral Worktree is deleted when its Session is archived (or when the Daemon's limit of ephemeral Worktrees is passed); a persistent one stays until the user deletes it in Settings → Worktrees. The Daemon creates and deletes Worktrees; Clients only ask.
_Avoid_: branch (on its own), sandbox, checkout

**Prompt**:
A question the Daemon asks the human mid-turn: a permission request for a tool call, or an ask-user questionnaire. The Daemon sends it to every Client attached to the Session; the first answer wins and the Daemon then tells every Client the Prompt is resolved.
_Avoid_: confirmation, approval dialog, permission request (when the ask-user case is included)

