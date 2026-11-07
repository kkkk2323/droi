# Droi

Droi is a desktop, iPhone and mobile-web front end for a coding agent. Since 2.0 it runs that agent itself: the Kernel, Droi's own process built on Pi Durable (ADR 0038), and Droi presents and controls the Conversations the Kernel owns. Factory's models reach the Kernel through the Host's LLM Proxy (ADR 0039).

## Language

### Processes

**Kernel**:
The agent process Droi builds and ships (`apps/kernel`): TypeScript on Pi Durable with Droi's own coding tools, owning every Conversation, its entries, one SQLite database, and the file and process work of a turn. It is the Host's sidecar, one process for every Conversation on the computer, and Droi speaks to no other agent process.
_Avoid_: daemon, backend, server, engine, exec runner

**Host**:
The process on a computer that starts and watches over the Kernel, runs the Gateway, holds the Key Pool and the LLM Proxy every Factory request goes through, keeps that computer's Memory, its Automations and its Workstreams, and creates the Worktrees a Conversation runs in. The Desktop Shell embeds one; on a computer without a Desktop Shell (a headless Linux box) it runs on its own as `droi host`, the same Droi program without a window, kept alive by the system's service manager. Every computer a Client can reach has exactly one Host.
_Avoid_: server, node, station, backend, agent runner

**Key Pool**:
The Factory API keys Droi keeps for the Factory requests of every Conversation, one of them current. In Automatic mode Droi moves to another key when the current one runs out; in Manual mode it keeps the one chosen. A key's Factory plan (Pro, Plus, Max, Ultra) sets the size of its quota, and the Usage tab counts a Max key's percent as ten of a Pro key's. Kept in Droi's own data, apart from droid-proxy's.
_Avoid_: key list, key rotation, accounts, login

**LLM Proxy**:
The Key Pool's local proxy the Kernel sends its Factory traffic through: pi-ai is pointed at it as a provider, and it stamps the current key on every request. It keeps droid's request shape, because Factory's endpoint expects it: droid's first line, Pi's own sentences rewritten, `413` answered by dropping old attachments, `403` explained, the key's organisation and region honoured (ADR 0039). It therefore no longer forwards requests unchanged, and it no longer rewrites `/api/feature-flags`. It records requests, their events and each key's quota for Settings → Usage; it also keeps prompt caches warm. Not the Gateway, and not droid-proxy.
_Avoid_: dp, gateway, MITM

**Desktop Shell**:
The installed desktop application. It embeds a Host and opens a window for the Local Client. It holds no conversation state.
_Avoid_: main process, backend, app

**Brand**:
What names the app built from Droi's packages: its name, the short id of its folders, the Gateway's port and its mark. Droi's own brand is Droi; another app runs `droiapp.Run` with its own (ADR 0029). The Gateway answers `/meta` as Droi whatever the brand.
_Avoid_: white label, skin, theme

**Extension**:
What an app built on Droi adds to it through `droiapp.Run`'s seams or, since 2.0, by adding a Kernel extension: MCP servers, words for the System Prompt Addition, tools, subcommands, pages, Settings sections, and how tool calls and json-render elements show (ADRs 0029, 0038). It plugs into seams Droi has; it does not patch Droi.
_Avoid_: plugin, add-on, module
**Gateway**:
The part of the Host that lets a Client reach the Kernel. It checks the Pairing Token or the Local Token and passes the Kernel's protocol through without touching a frame; the credential is not its business any more (ADR 0038). It creates and trashes Scratch Workspaces, and answers a Client's requests to restart the Kernel or update the Host.
_Avoid_: proxy, API server, Hono server, web server

### Clients

**Client**:
A Droi user interface. Every Client speaks only the Kernel's protocol, reaches the Kernel through a Host's Gateway, and keeps no conversation state of its own. There are two: the Desktop Shell window and the Phone App.
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
A Host a Client has been paired with and remembers: one Gateway address, the Relay address when its pairing link names one, the Pairing Token, and the computer's name and id as the Gateway reports them. A Client reaches it at the Gateway address when that answers within a moment, and through the Relay otherwise. Pairing the same computer again updates it rather than adding another. A Client is connected to one Paired Computer at a time. The Desktop Shell's own Host is the first Paired Computer of its Local Client and cannot be forgotten.
_Avoid_: host (that is the process; this is a Client's record of one), daemon profile, server, device

**Remote Access**:
The Host setting that decides whether the Gateway accepts Remote Clients at all, on the computer's network addresses and through the Relay. Off by default in a Desktop Shell; always on in a Host running on its own, which has no Local Client.
_Avoid_: LAN mode, web mode, mobile mode, `DROID_WEB_ENABLED`

**Relay**:
A server both a Host and a Remote Client dial out to, so that a phone away from the computer's network reaches its Gateway (ADR 0034). The Host keeps one socket open to it while Remote Access is on and registers its Relay Id; a phone addresses the Host as `<Relay>/c/<Relay Id>`, and the Gateway still checks the Pairing Token. `droi-relay` runs one; it speaks alma-relay's protocol. Its **relay address**, `https://<Relay>/#key=<registration key>`, is what a Host is given; the key never reaches a phone.
_Avoid_: tunnel, proxy, Ponte, droid's Remote Control (Factory's relay, which Droi does not use)

**Relay Id**:
The random id a Host is addressed by on a Relay, kept in its settings. Only its hash reaches the Relay's memory; it is not a secret that lets anyone in, which the Pairing Token is.
_Avoid_: connect code, routing token

### Memory

**Memory**:
What Droi keeps on a computer of the user's earlier Sessions for the agent to draw on in later ones: the Memory Summary, the Session Summaries and the Memory Notes, in one folder. The Kernel knows nothing of it; the Host keeps it, and each Host keeps its own.
_Avoid_: context, knowledge base, history, Project Memory, Global Memory

**Memory Summary**:
The short document every Conversation on a computer with Memory starts with: who the user is, how they like to work, and pointers to the Session Summaries of recent work.
_Avoid_: memory file, profile, MEMORY.md

**Session Summary**:
The record of one earlier Session, written from its entries once the Session has been left for a while.
_Avoid_: rollout summary, recap, extraction

**Memory Note**:
What the user explicitly asked the agent to remember, forget or correct, kept as they asked it until the Memory Summary takes it in.
_Avoid_: memory entry, fact, correction

**Memory Tools**:
The tools and the prompt section the Kernel offers when Memory is on: `memory_search`, `memory_read`, `memory_list` and `memory_note`, with the Memory Summary as a section of every request. They are not an MCP Server and nothing is dialled: the Memory setting is their only switch (ADR 0038).
_Avoid_: memory MCP, memory plugin, memory server

**Memory Session**:
A Conversation the Host opens in the Kernel for Memory work of its own, writing a Session Summary or updating the Memory Summary, with no user in it. Its kind keeps it out of every Client's session list, it has a `submit_result` tool, and it is archived when its turn ends.
_Avoid_: hidden session, background session, consolidation job

### Testing

**Fake Kernel**:
The real Kernel, started by a test with a scripted model in place of a provider. It speaks the Kernel's protocol, so a test needs no Factory key, and it plays a Scenario instead of running an agent.
_Avoid_: mock server, stub, recorder, Fake Daemon

**Scenario**:
The per-test script that tells the Fake Kernel's model what to answer and when.
_Avoid_: fixture data, recording, replay log

### Conversations

**Session**:
One conversation with the agent, owned and persisted by the Kernel and identified by its conversation id. It is what a Client shows, and what the Kernel's protocol calls a Conversation.
_Avoid_: chat, thread, worker, droi session file

**Conversation**:
What the Kernel and its protocol call a Session: its entries, its runs and its id. Droi's Clients and screens still say Session, and both words name the same thing (ADR 0038).
_Avoid_: chat, thread

**Entry**:
One entry of a Conversation: Pi's six kinds (`pi.user`, `pi.assistant`, `pi.system`, `pi.tool-result`, `pi.reset`, `pi.compaction`) and Droi's five (`droi.approval`, `droi.ask`, `droi.subagent`, `droi.process`, `droi.note`). Clients render and page entries; what a model gets is the entries that carry a model, which is why Droi's own five carry none.
_Avoid_: message, block, transcript line

**Run**:
One run of the model in a Conversation: what `conversation.busy` carries and what a Client shows as working. A Conversation is idle between runs.
_Avoid_: turn (the user's word, not the Kernel's), job

**Task**:
Work the Kernel runs beside a Conversation's run: a compaction, a subagent, or work queued after the run. `task.changed` reports it and `task.abort` stops it.
_Avoid_: job, background task (a background process is a `droi.process` entry)

**Submission**:
One input a Client sends a Conversation: what the user typed, a steer into a run that is working, or a continue. The Kernel answers with a `submissionId`, reports its status, and `submission.abort` takes back what has not settled.
_Avoid_: message, request, prompt

**Unread**:
A Session whose last run ended after any Client of its computer last showed it. Showing it on one Client reads it on all of them.
_Avoid_: new, unseen, notification

**Time View**:
The Session list as one list by when each Session was last used, under Pinned, Right now (working or Unread), Today, Yesterday and Earlier, in place of the groups by Workspace. The window's Sort menu chooses it, and the Phone App follows the window.
_Avoid_: recent view, Recents (that is the group of Scratch Workspace Sessions), flat list

**Subagent**:
A Conversation the Kernel starts for a subagent task, which the parent's `droi.subagent` entry names along with the tool call that started it. Clients keep it out of the session list and reach it from the Session that called it: the Task call's card, the header's subagent menu, and the trail back.
_Avoid_: child session, sub-session, task session

**Session Defaults**:
What every new Conversation on a computer starts with: model, reasoning, autonomy, plan mode, compaction and subagent models. Droi keeps them in its own settings, in the Kernel's agent shape, and every Client edits them through the Kernel (ADR 0038).
_Avoid_: preferences, global settings, default settings (on their own)

**System Prompt Addition**:
Text the Host keeps and the Kernel adds to its own prompt as a section when a Conversation starts, whichever Client starts it. It never replaces the Kernel's prompt, and a Conversation keeps the one it started with.
_Avoid_: system prompt override, custom instructions

**Custom Model**:
A model the Kernel runs that is not Factory's: a pi-ai provider with its own base URL, key and dialect, kept in Droi's settings rather than in droid's. A Session picks one like any other model, and it has no Factory usage multiplier. Clients show them; only the user and the Host add them.
_Avoid_: BYOK model, external model, third-party model (on its own)

**Magpie**:
A local gateway (yetone's Magpie, listening on 127.0.0.1:3425) that runs coding agents on the models of many providers behind one endpoint. Droi does not run it. It reached the Daemon as Custom Models in the Runtime Overlay, which 2.0 removes; whether and how its models reach the Kernel is undecided (ADR 0038).
_Avoid_: proxy, provider, model router (a Router is Factory's), gateway (that is Droi's own)

**Skill**:
A `SKILL.md` folder the Kernel offers the agent, and the user through "/": built into the Kernel, personal (`~/.droi/skills`, copied once from `~/.factory/skills`), or the Workspace's (`.factory/skills`, read where it lies). The Kernel lists a Session's skills and keeps the switches: off across all projects, or off for one Workspace. A personal skill is managed by its files. Droi gives each skill one switch: a built-in one switches it for every project, the Workspace's switches it there.
_Avoid_: plugin, extension, prompt template

**MCP Server**:
A tool server the Kernel connects a Conversation to, from Droi's `~/.droi/mcp.json` (copied once from `~/.factory/mcp.json`), the Workspace's `.factory/mcp.json` or the organization's policy. The Kernel reports each one's connection and tools, switches them, adds and removes the user's own, and runs a sign-in whose page a Client opens and whose callback the Kernel takes on the computer.
_Avoid_: connector (Factory's hosted integrations are something else), tool, integration

**Automation**:
A prompt Droi's Host runs on a schedule on its computer, each run in a new Session unless the Automation targets an existing one (ADR 0035, ADR 0038). It is kept in Droi's own data, out of Factory's sync; the window, the Gateway and the agent's own tools create, edit, pause and run it.
_Avoid_: cron, job, scheduled task, workflow

**Factory Automation**:
An Automation kept in `~/.factory/automations/<id>/` and synced to Factory, as the droid CLI and the Factory App make them. Nothing in Droi runs one any more (ADR 0038): Droi lists the local ones and moves one into its own Automations on request, and no longer edits them.
_Avoid_: Automation (on its own), local automation, Daemon automation

**Automation Run**:
One run of an Automation: a Session the Host starts and closes when its turn ends, or, when the Automation targets an existing Session, the input it sends there. Clients list a run in its own Session on the Automations page, not with the other Sessions; a Factory Automation's runs are listed on that Factory Automation's page.
_Avoid_: job run, execution, automation session

**Workstream**:
A long goal the user hands to an Orchestrator, which splits it among Workstream Agents (ADR 0036). It is Droi's own: the Host starts its Sessions, keeps its record, and its tools are the Kernel's extensions. Archiving it archives its Sessions and keeps its Shared Folder.
_Avoid_: mission, project, swarm, team

**Orchestrator**:
The Session that leads a Workstream: it plans, starts Workstream Agents, sends them work and hears their reports; it does not edit code. The user talks to it like any Session. It is the Workstream's page, with tabs for its agents, and appears in the sidebar's Workstreams group, not in the session list (ADR 0037).
_Avoid_: lead, manager, parent session

**Workstream Agent**:
A Conversation an Orchestrator starts for one area of a Workstream's work, in a Worktree of its own when the Workspace is a git repository. It reports back to its Orchestrator. Clients keep it out of the session list and reach it from its Workstream, as a tab of the Orchestrator's page and a row of its panel. Every agent of a Workstream runs on the Workstream's agent model, unless the user changes the model in that agent's own Session.
_Avoid_: worker, subagent (that is a Task call's Session), child session

**Shared Folder**:
A folder the Host keeps for each Workstream, outside the repository, where its Orchestrator and Workstream Agents leave plans, notes and hand-overs for each other.
_Avoid_: scratch dir, notes folder, Scratch Workspace

**Workspace**:
The directory on the computer that a Session operates in.
_Avoid_: project, project dir, cwd, repo

**Scratch Workspace**:
A Workspace the Host creates for a Session started without choosing one, for work that belongs to no project. Archiving the conversation moves it to the Trash. Clients list these Sessions together, under Recents.
_Avoid_: chat, temp dir, draft

**Worktree**:
A git worktree the Host creates for a Session started with "New worktree": its own directory and a new `droid/<slug>` branch, cut from a base branch of the repository. It is the Session's Workspace, and Clients list the Session under the repository it came from. An ephemeral Worktree is deleted when its Session is archived (or when the Daemon's limit of ephemeral Worktrees is passed); a persistent one stays until the user deletes it in Settings → Worktrees. The Host creates and deletes Worktrees, since git stays in Go (ADR 0038); Clients only ask.
_Avoid_: branch (on its own), sandbox, checkout

**Approval**:
A tool call the Kernel stops to ask the human about. The tool writes a `droi.approval` entry, every attached Client gets `approval.requested`, and the first `approval.respond` wins with one of four outcomes: allow once, always allow this tool, always allow this file, or cancel (ADR 0040).
_Avoid_: confirmation, approval dialog, permission prompt, Prompt

**Question**:
An `ask_user` tool call: a questionnaire the Kernel puts to the human. A blocking one waits like an Approval; a queued one (`blocking: false`) lets the run go on, shows above the composer, and takes its answers as the next input (ADR 0040).
_Avoid_: AskUser prompt, non-blocking prompt, async question, deferred question, Prompt

**Autonomy**:
The four levels that decide which tool calls the Kernel stops for: `ask`, `edits`, `safe` and `all`, in place of droid's off, low, medium and high. The Host keeps the table of allowances an answer added, so "always allow" holds across Conversations and is the same setting Settings → Autonomy shows.
_Avoid_: permission mode, approval level, confirmation mode

**Plan Mode**:
A mode a Conversation can be in where the Kernel offers only the reading tools and `submit_plan`: the agent writes a plan instead of making a change, and the user then continues in the same context or starts a new Conversation. It replaces spec mode (ADR 0040).
_Avoid_: spec mode, plan-only session, draft mode

## Retired in 2.0

These things from 1.x have no place any more. They stay here so a reader who meets one in an older ADR, a commit or a Settings page can map it to what replaced it (ADR 0038, ADR 0039).

**Daemon**:
`droid daemon`, Factory's agent process that Droi drove over its protocol. The Kernel replaces it, and there is no second one.
_Avoid_: using the word for the Kernel

**Factory Account**:
A Factory login Droi kept and gave a Daemon of its own, under `FACTORY_HOME_OVERRIDE`. What was several accounts is several keys in the Key Pool.

**Minimum Droid**, **Pinned Droid**, **Droi's droid**, **Key Hook**:
The `droid` builds Droi chose or checked, and the preload that picked their LLM key. Nothing selects a droid build now: the Kernel comes with the app and resolves its own key through the LLM Proxy.

**Runtime Overlay**:
The `--settings` file the Host generated for its Daemon at start. The Kernel takes its configuration as start arguments and as extensions.

**Draft Session**:
A Session the New session page opened before the user had said anything, so the composer could list its Workspace's skills. Skills and commands are files, and `harness.info` lists them before a Conversation exists.

**Session Times**:
The Host's own record of when the user wrote, when a turn ended, and when a Client showed a Session. The Kernel derives the first two from entry timestamps and keeps one `viewedAt` column; Clients still order by them and still mark Sessions Unread.

**Loop**:
A prompt the Daemon sent back into the same Session on a schedule. It is an Automation with `target: existing` now, run by the Host.

**Prompt**, **Queued Question**:
A mid-turn question from the Daemon, whether a permission request or a questionnaire. Split into Approval and Question, which are tool calls the Kernel waits on.

**Memory Server**:
The MCP Server Droi carried for Memory. The Memory Tools replace it, in the Kernel's own process.

**Script**:
droid's tool that wrote and ran a program of tool calls, and the model whitelist in `/api/feature-flags` that decided which models could use it. codemode is the Kernel's extension, on by default, and its nested calls are listed inside the outer card.
