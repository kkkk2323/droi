---
status: accepted
---

# Every attached Client sees the turn and may answer prompts; first answer wins

When a Session is open in the Desktop Shell window and on a phone at the same time, both Clients are attached to the same Daemon Session over two connections. We needed to know what the Daemon actually does before designing the Client's prompt UI. The spike in [`0003-spike/`](./0003-spike/) ran a real `droid daemon` (0.223.0, protocol 1.217.0) behind a logging WebSocket proxy with three `connectToDaemon()` connections from `@factory/droid-sdk@0.9.1` on one Session: A created it, B resumed it before the turn, C resumed it mid-turn. A streamed a prompt that ran a shell command (autonomy off, so it asked permission) and then used `ask_user`. Handlers on every connection logged and answered with staggered delays. [`events.log`](./0003-spike/events.txt) is the summarised timeline; [`raw.log`](./0003-spike/raw.txt) is every frame (API key redacted).

What the Daemon does:

- **Every attached connection receives every Session notification** during another connection's turn: `create_message`, `assistant_text_delta`, `tool_call`, `tool_result`, `droid_working_state_changed`, `agent_turn_completed`, and so on. B, which never called `stream()`, saw the whole turn.
- **`daemon.request_permission` and `daemon.ask_user` are broadcast to every attached connection** as JSON-RPC requests with the same request id. A, B and C all received them, regardless of which connection started the turn.
- **The first response wins.** Later responses get a JSON-RPC error `No pending permission found with ID` / `No pending ask-user request found with ID`, and the Daemon sends every connection a `permission_resolved` notification carrying the `requestId`, `toolUseIds` and `selectedOption`.
- **A connection that resumes mid-turn sees the in-flight turn.** `daemon.load_session` returned the transcript so far, `isAgentLoopInProgress: true` and `workingState: "thinking"`, and live notifications followed immediately, including the pending `ask_user` request.
- Any attached connection may start the next turn (`daemon.add_user_message`); the others observe it the same way.

Decision: the Client implements the **both-see-and-answer** behaviour. Each Client renders prompts it receives; when it receives `permission_resolved` (or its own answer is rejected with the "no pending" error) it removes the prompt and shows the resolved outcome. No read-only participant mode is needed.

## Considered Options

- **Both see and answer, first answer wins (chosen).** Matches the Daemon exactly; no extra state.
- **Later Client is read-only until the turn ends.** Would have been the fallback if prompts were routed to one connection. The Daemon does not do this, and enforcing it in the Client would only hide a working feature.

## Consequences

- The Client's prompt state is keyed by the Daemon's `requestId` and is cleared by `permission_resolved` and by the "no pending" error, never only by local answering.
- The Fake Daemon must broadcast prompts to every connection and reply with the "no pending" error to a second answer, so E2E tests cover the phone-and-desktop race.
- Two facts from the same run constrain the Desktop Shell (ticket #83): the Daemon rejects an API key whose user does not own the computer registration in `$HOME/.factory` (`user_mismatch`, "This computer belongs to a different user"), and it accepts the same key when started with `FACTORY_API_KEY` set. The Desktop Shell must start the Daemon with the credential the Gateway injects, and the spec's assumption that the Daemon shares `~/.factory/sessions` with the interactive `droid` CLI only holds when that CLI is logged in as the same user.
- SDK 0.9.1 announces protocol 1.201.1 and the Daemon 1.217.0; the Daemon reports the mismatch in error payloads but serves the connection. Pinning the SDK is safe for now; a hard mismatch would show up as an authentication error, so the Client's connection-state UI must surface `protocol_mismatch` distinctly.
