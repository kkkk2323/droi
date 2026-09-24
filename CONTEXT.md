# Droi

Droi is a desktop, iPhone and mobile-web front end for the Factory Droid coding agent. It does not run the agent itself; it presents and controls agent conversations that live in the Droid Daemon.

## Language

### Processes

**Daemon**:
The `droid daemon` process that owns every Session, its transcript, and all filesystem and Git operations. Droi never re-implements what the Daemon already does.
_Avoid_: backend, server, engine, exec runner

**Desktop Shell**:
The installed desktop application. It starts the Daemon, opens a window for the Local Client, and hosts the Gateway. It holds no conversation state.
_Avoid_: main process, backend, app

**Gateway**:
The part of the Desktop Shell that lets a Remote Client reach the Daemon. It checks the Pairing Token and supplies the Factory credential (the Shell's Factory login token, or an API key as fallback) so it never leaves the computer. It also adds the Shell's System Prompt Addition to every Session a Client starts.
_Avoid_: proxy, API server, Hono server, web server

### Clients

**Client**:
A Droi user interface. Every Client speaks only the Daemon protocol and keeps no conversation state of its own. There are two: the web Client, which runs as the Local Client and as a Remote Client in a browser, and the Phone App.
_Avoid_: renderer, frontend, web UI

**Local Client**:
The web Client running inside the Desktop Shell window on the same computer as the Daemon.

**Remote Client**:
A Client on another device that reaches the Daemon through the Gateway: the Phone App on an iPhone, or the web Client in a browser anywhere else.
_Avoid_: mobile, web mode, browser mode, LAN mode

**Phone App**:
The native iPhone Client. It is the Remote Client on iOS and takes the browser's place there; the browser Remote Client stays for every other device. It offers what the web Client offers, and nothing the desktop lacks.
_Avoid_: mobile app, iOS app, iOS Client

**Pairing Token**:
The secret a Remote Client presents to the Gateway to prove it was authorised from the Desktop Shell. There is one token, shown as a QR code and link in the Desktop Shell settings; resetting it revokes every Remote Client at once. The Local Client presents a separate per-launch token instead, so a reset never touches the desktop window.
_Avoid_: API key or login token (those are Factory credentials, which Clients never hold), device token

**Paired Computer**:
A Desktop Shell the Phone App has been paired with: one Gateway address, the Pairing Token, and the computer's name and id as the Gateway reports them. Scanning the same computer again updates it rather than adding another. The Phone App is connected to one Paired Computer at a time.
_Avoid_: host, daemon profile, server, device

**Remote Access**:
The Desktop Shell setting that decides whether the Gateway accepts Remote Clients at all. Off by default.
_Avoid_: LAN mode, web mode, mobile mode, `DROID_WEB_ENABLED`

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
Text the Desktop Shell keeps and the Gateway appends to Droid's own system prompt when a Session starts, whichever Client starts it. It never replaces Droid's prompt, and a Session keeps the one it started with.
_Avoid_: system prompt override, custom instructions

**Workspace**:
The directory on the computer that a Session operates in.
_Avoid_: project, project dir, cwd, repo

**Prompt**:
A question the Daemon asks the human mid-turn: a permission request for a tool call, or an ask-user questionnaire. The Daemon sends it to every Client attached to the Session; the first answer wins and the Daemon then tells every Client the Prompt is resolved.
_Avoid_: confirmation, approval dialog, permission request (when the ask-user case is included)
