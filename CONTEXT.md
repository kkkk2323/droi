# Droi

Droi is a desktop and mobile-web front end for the Factory Droid coding agent. It does not run the agent itself; it presents and controls agent conversations that live in the Droid Daemon.

## Language

### Processes

**Daemon**:
The `droid daemon` process that owns every Session, its transcript, and all filesystem and Git operations. Droi never re-implements what the Daemon already does.
_Avoid_: backend, server, engine, exec runner

**Desktop Shell**:
The installed desktop application. It starts the Daemon, opens a window for the Local Client, and hosts the Gateway. It holds no conversation state.
_Avoid_: main process, backend, app

**Gateway**:
The part of the Desktop Shell that lets a Remote Client reach the Daemon. It checks the Pairing Token and supplies the Factory API key so the key never leaves the computer.
_Avoid_: proxy, API server, Hono server, web server

### Clients

**Client**:
The single Droi user interface. The same interface runs as a Local Client or a Remote Client; it speaks only the Daemon protocol.
_Avoid_: renderer, frontend, web UI, mobile app

**Local Client**:
The Client running inside the Desktop Shell window on the same computer as the Daemon.

**Remote Client**:
The Client running in a browser on another device (typically a phone) and reaching the Daemon through the Gateway.
_Avoid_: mobile, web mode, browser mode, LAN mode

**Pairing Token**:
The secret a Remote Client presents to the Gateway to prove it was authorised from the Desktop Shell. There is one token, shown as a QR code and link in the Desktop Shell settings; resetting it revokes every Remote Client at once. The Local Client presents a separate per-launch token instead, so a reset never touches the desktop window.
_Avoid_: API key (that is the Factory credential, which Clients never hold), device token

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

**Workspace**:
The directory on the computer that a Session operates in.
_Avoid_: project, project dir, cwd, repo

**Prompt**:
A question the Daemon asks the human mid-turn: a permission request for a tool call, or an ask-user questionnaire. The Daemon sends it to every Client attached to the Session; the first answer wins and the Daemon then tells every Client the Prompt is resolved.
_Avoid_: confirmation, approval dialog, permission request (when the ask-user case is included)
