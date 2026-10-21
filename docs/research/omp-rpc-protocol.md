# OMP RPC Protocol Research (for Go host integration)

Research date: 2026-10-21  
Local binary probed: `/Users/xlong/.local/bin/omp` → **omp/18.12.5**  
Protocol nature: **custom JSONL over stdio** (not JSON-RPC 2.0)

## Sources (primary)

| Source | URL / path | Role |
| --- | --- | --- |
| Canonical RPC reference | https://raw.githubusercontent.com/can1357/oh-my-pi/main/docs/rpc.md | Full wire contract |
| Host-facing RPC guide | https://omp.sh/docs/rpc | Lifecycle, checklist, public tables |
| Approval modes | https://raw.githubusercontent.com/can1357/oh-my-pi/main/docs/approval-mode.md | Tool approval policy |
| Host-facing SDK page | https://omp.sh/docs/sdk | TS in-process SDK vs RPC choice |
| Sessions | https://omp.sh/docs/sessions | Storage, resume, profiles |
| Go client README | https://raw.githubusercontent.com/can1357/oh-my-pi/main/sdk/go/omp-rpc/README.md | Go SDK API surface |
| Go module | https://github.com/can1357/oh-my-pi/tree/main/sdk/go/omp-rpc | Exists; `module github.com/can1357/oh-my-pi/sdk/go/omp-rpc`, `go 1.23` |
| Local CLI help | `omp --help` / `omp --version` | Launch flags |
| Local RPC probe | `omp --mode rpc --no-session …` | Ready / negotiate / `get_state` shapes |

Implementation pointers named by upstream docs (not fetched line-by-line here):

- `packages/coding-agent/src/modes/rpc/rpc-mode.ts`
- `packages/coding-agent/src/modes/rpc/rpc-types.ts`
- `packages/coding-agent/src/modes/rpc/wire` (+ `bun run gen:rpc`)
- `sdk/go/omp-rpc/wire.go` (generated)

---

## 1. Launching RPC / RPC-UI

### Commands

```bash
# Headless JSONL server (recommended default for hosts without tool UI)
omp --mode rpc [regular CLI options]

# Same wire protocol; marks session UI-capable and bridges tool/extension UI
omp --mode rpc-ui [regular CLI options]

# Typical host spawn for ephemeral work
omp --mode rpc --no-session

# Host without extension UI chrome; tool dialogs still off under plain rpc
omp --mode rpc --no-ui

# Answer tool dialogs (ask/select/…) while keeping extensions headless
omp --mode rpc-ui --no-ui
```

From local `omp --help` (18.12.5):

- `--mode=<value>`: `text` (default), `json`, `rpc`, or `rpc-ui`
- `--no-ui`: with `rpc`/`rpc-ui`, run extensions headless (`rpc-ui` tool UI remains enabled)
- `--no-session`: do not save session (ephemeral; cannot resume)
- `--session-dir=<value>`: directory for session storage and lookup
- `-c, --continue` / `-r, --resume=<value>` / `--restore=<value>`
- `--model`, `--cwd`, `--profile`, `--config` (repeatable), `--add-dir` (repeatable)
- `--auto-approve` / `--approval-mode=always-ask|always-ask-cached|write|yolo`
- `--tools`, `--no-tools`, `--thinking`, provider keys via env, etc.

### RPC-mode behavior notes (docs)

- `@file` CLI args are **rejected** in RPC mode; send files/images in the prompt payload instead.
- CLI RPC sets `PI_NO_TITLE=1` (no auto title model call). `rpc-ui` also sets `PI_NO_PTY=1`.
- stdin is claimed before extension discovery; one non-empty JSONL object per line.
- First stdout frame is `ready`, then commands are processed.
- Close stdin to shut down: pending UI/tool/URI requests rejected, accepted commands drained, session disposed, exit `0` (persistence failure still latched → exit `1` after `notice`).
- Keep stderr separate; stdout is **only** the JSON protocol.

### Local probe (18.12.5)

```json
{"type":"ready","protocolVersion":1,"supportedProtocolVersions":[1,2],"maxFrameBytes":1048576,"maxReassembledFrameBytes":67108864}
```

After:

```json
{"id":"p1","type":"negotiate_protocol","protocolVersion":2}
```

→

```json
{"id":"p1","type":"response","command":"negotiate_protocol","success":true,"data":{"protocolVersion":2}}
```

---

## 2. Transport and framing

### Physical frames

- One JSON object + `\n` per physical frame (v1).
- Physical stdout frame cap: **1 MiB** (`maxFrameBytes: 1048576`), including newline.
- Inbound (host→omp) frames are always **single unchunked** JSONL objects (do not send `rpc_chunk` inbound).
- Protocol v2 (opt-in): oversized **stdout** logical frames become uninterrupted `rpc_chunk` sequences (base64 UTF-8 segments), reassembled up to **64 MiB** (`maxReassembledFrameBytes: 67108864`).

`rpc_chunk` shape:

```json
{
  "type": "rpc_chunk",
  "chunkId": "rpc-1",
  "index": 0,
  "count": 7,
  "byteLength": 1600042,
  "data": "eyJ0eXBlIjoicmVzcG9uc2UiLC4uLn0="
}
```

Client must validate `chunkId`/`index`/`count`/`byteLength`, reject interleaved sequences, concatenate in order, UTF-8 decode, parse one JSON object.

### Correlation

- Every command may carry optional `id?: string`.
- Responses echo `id` when present.
- Match responses by `id`, **not** emission order (`bash`, admitted `prompt`, `predict_word`, `live_start` can complete out of order).
- Malformed JSON → recoverable `{ type:"response", command:"parse", success:false, error:… }` **without** `id`.

### Response envelope

```text
Success: { id?, type: "response", command: <cmd>, success: true, data?: … }
Failure: { id?, type: "response", command: string, success: false, error: string, code?: string }
```

Known machine codes (at least): `session_busy`, `stale_cursor`, `unknown_since` (on paging/history).

---

## 3. Inbound vs outbound frame categories

### Host → omp (stdin)

1. `RpcCommand` (`type` = command name)
2. `extension_ui_response`
3. `host_tool_update` / `host_tool_result`
4. `host_uri_result`

### omp → host (stdout)

1. `ready`
2. `response`
3. Agent/session events (`agent_start`, `message_update`, …)
4. `extension_ui_request`
5. `host_tool_call` / `host_tool_cancel`
6. `host_uri_request` / `host_uri_cancel`
7. `extension_error`
8. `available_commands_update`
9. `prompt_result`
10. `session_settled`
11. Subagent frames (`subagent_lifecycle` / `subagent_progress` / `subagent_event`)
12. Side-question frames (`btw_delta`, `btw_record`)
13. Slash side channels (`command_output`, `session_info_update`, `config_update`)
14. `rpc_frame_error` / `rpc_chunk`
15. Live voice (`live_phase`, `live_levels`, `live_transcript`, `live_end`)
16. `notice`

---

## 4. Command catalog (exact `type` names)

All accept `id?: string`. Canonical list from `docs/rpc.md` (richer than the shortened omp.sh table).

### Protocol

| type | fields | success `data` |
| --- | --- | --- |
| `negotiate_protocol` | `protocolVersion: 2` | `{ protocolVersion: 2 }` |

### Prompting / queue

| type | fields | notes |
| --- | --- | --- |
| `prompt` | `message: string`, `images?: ImageContent[]`, `streamingBehavior?: "steer" \| "followUp"` | Ack on **admission**, not turn end |
| `steer` | `message`, `images?` | Mid-run interrupt path |
| `follow_up` | `message`, `images?` | After current run |
| `remove_queued_message` | `message: string`, `queue: "steering" \| "followUp"` | `data: { removed: boolean, images?, imagesDropped? }` |
| `promote_queued_message` | `message: string` | Follow-up → steering; `data: { promoted: boolean }` |
| `abort` | — | Stop active agent run |
| `abort_and_prompt` | `message`, `images?` | Abort then schedule replacement |
| `abort_and_restore_queue` | — | Abort and restore queued drafts |
| `new_session` | `parentSession?: string` | `data: { cancelled: boolean }` |
| `open_session` | `sessionDir: string`, `provider?`, `modelId?` | Resume/create in dir; see §6 |

While streaming, a second `prompt` **must** set `streamingBehavior` or it fails. Prefer dedicated `steer` / `follow_up`.

### State / host registration

| type | fields | success `data` (when specified) |
| --- | --- | --- |
| `get_state` | — | `RpcSessionState` (see below) |
| `set_fast_mode` | `enabled: boolean` | `{ enabled, active }` |
| `set_slow_mode` | `enabled: boolean` | (session slow mode) |
| `goal` | `op: "get"\|"create"\|"resume"\|"pause"\|"drop"`, `objective?`, `token_budget?` | goal payload |
| `set_ask_dialog` | `enabled: boolean` | enables `ask` UI method |
| `get_available_commands` | — | `{ commands: RpcAvailableSlashCommand[] }` |
| `get_entries` | `since?: string` | `{ entries, leafId }`; bad since → `code:"unknown_since"` |
| `get_tree` | — | `{ tree, leafId }` |
| `set_todos` | `phases: TodoPhase[]` | `{ todoPhases }` |
| `set_host_tools` | `tools: RpcHostToolDefinition[]` | `{ toolNames: string[] }` (replace set) |
| `set_host_uri_schemes` | `schemes: RpcHostUriSchemeDefinition[]` | `{ schemes: string[] }` |
| `set_subagent_subscription` | `level: "off"\|"progress"\|"events"` | `{ level }` |
| `set_event_filter` | `events: string[] \| null`, `messageUpdates?: "full"\|"delta"` | filter stdout events |
| `get_subagents` | — | `{ subagents: RpcSubagentSnapshot[] }` |
| `get_subagent_messages` | `subagentId?`, `sessionFile?`, `fromByte?` | incremental transcript |
| `cancel_subagent` | `subagentId: string` | `{ cancelled: boolean }` |
| `steer_subagent` | `subagentId`, `message` | ack when accepted |

### Model / thinking / queues / compaction / retry / bash

| type | fields |
| --- | --- |
| `set_model` | `provider: string`, `modelId: string` → selected `Model` or `Model not found: provider/id` |
| `cycle_model` | — → `{ model, thinkingLevel, isScoped } \| null` |
| `get_available_models` | — → `{ models: Model[] }` (waits discovery) |
| `set_thinking_level` | `level: "inherit"\|"off"\|"minimal"\|"low"\|"medium"\|"high"\|"xhigh"\|"max"` |
| `cycle_thinking_level` | — |
| `get_available_thinking_levels` | — → `{ levels }` (`off` + model efforts; no `inherit`/`auto`) |
| `set_steering_mode` | `mode: "all"\|"one-at-a-time"` (default one-at-a-time) |
| `set_follow_up_mode` | same |
| `set_interrupt_mode` | `mode: "immediate"\|"wait"` (default immediate) |
| `compact` | `customInstructions?` → `CompactionResult` |
| `set_auto_compaction` | `enabled: boolean` |
| `set_cache_warming` | `mode: "off"\|"streaming"\|"idle"` → `{ mode }` |
| `set_auto_retry` | `enabled: boolean` |
| `abort_retry` | — |
| `bash` | `command: string` → `BashResult` (concurrent) |
| `abort_bash` | — |

### Session / messages / login / misc

| type | fields | success `data` |
| --- | --- | --- |
| `get_session_stats` | — | `SessionStats` |
| `export_html` | `outputPath?` | `{ path }` |
| `switch_session` | `sessionPath`, `provider?`, `modelId?` | `{ cancelled }` |
| `branch` | `entryId` | `{ text, cancelled }` |
| `fork` | `entryId?` | `{ cancelled }`; busy → `code:"session_busy"` |
| `get_branch_messages` | — | `{ messages: [{ entryId, text }] }` |
| `get_last_assistant_text` | — | `{ text: string\|null }` |
| `set_session_name` | `name` (non-empty) | — |
| `handoff` | `customInstructions?` | `{ savedPath? } \| null` |
| `get_messages` | — | `{ messages: AgentMessage[] }` (monolithic) |
| `get_messages_page` | `cursor?`, `limit?` (1–256, default 100) | `{ messages, totalMessages, nextCursor? }` |
| `get_login_providers` | — | `{ providers: [{ id, name, available, authenticated }] }` |
| `login` | `providerId` | `{ providerId }`; may emit UI |
| `get_logout_accounts` | `providerId` | `{ accounts: LogoutAccount[] }` |
| `logout` | `providerId`, `credentialId: number` | `{ remainingSource? }` |
| `predict_word` | `text`, `cursor` (UTF-16) | `{ suffix: string\|null }` concurrent |
| `predict_word_feedback` | `text`, `cursor`, `suggestion`, `accepted` | — |
| `btw` | `question`, `recordId?` | `{ record }` + stream frames |
| `btw_cancel` | `recordId?` | `{ cancelled }` |
| `get_btw_history` | — | `{ records }` |
| `live_start` | `voice?`, `instructions?` | `{ voice }` concurrent |
| `live_stop` | — | — |
| `live_mute` | `muted?` | `{ muted }` |

### Shared input objects

**ImageContent**

```json
{
  "type": "image",
  "data": "<base64>",
  "mimeType": "image/png",
  "detail": "auto|low|high|original",
  "providerFile": { "provider": "openai|anthropic|google", "id": "…", "uri": "…", "expiresAt": 0 },
  "url": "https://…"
}
```

**TodoPhase / TodoItem**

```json
{
  "name": "phase",
  "tasks": [
    { "content": "…", "status": "pending|in_progress|completed|abandoned|blocked", "blocker": "…" }
  ]
}
```

**RpcHostToolDefinition**

```json
{
  "name": "echo_host",
  "label": "Echo",
  "description": "…",
  "parameters": { "type": "object", "properties": { "message": { "type": "string" } } },
  "hidden": false,
  "loadMode": "essential|discoverable"
}
```

**RpcHostUriSchemeDefinition**

```json
{ "scheme": "notes", "description": "…", "writable": true, "immutable": false }
```

---

## 5. Prompt, stream, tools, abort, state, model (implementation focus)

### 5.1 Prompt lifecycle

**Send:**

```json
{ "id": "req_1", "type": "prompt", "message": "Summarize this repo" }
```

**Ack (admission only):**

```json
{ "id": "req_1", "type": "response", "command": "prompt", "success": true }
```

or local-only completion:

```json
{
  "id": "req_1",
  "type": "response",
  "command": "prompt",
  "success": true,
  "data": { "agentInvoked": false }
}
```

If `data.agentInvoked === false` on the response, the prompt finished locally (slash/extension); **no** `prompt_result` follows.

Otherwise exactly one later completion frame:

```json
{
  "type": "prompt_result",
  "id": "req_1",
  "agentInvoked": true,
  "status": "completed|aborted|error",
  "error": { "message": "…", "provider": "…", "model": "…", "httpStatus": 0, "retryable": false },
  "sessionSettled": true
}
```

**Yield vs settled**

- `prompt_result` = agent **yielded** (or local/pre-dispatch outcome). Background bash/task/eval may still wake the agent.
- `session_settled` = nothing can wake the session again.
- `get_state` exposes `isSettled` and `hasPendingAsyncWork`.

Host rules:

- Present turn answer on `prompt_result` / streamed messages.
- Tear down / recycle sandbox only after `session_settled` or `isSettled: true`.

### 5.2 Streaming events (agent lifecycle)

| type | fields |
| --- | --- |
| `agent_start` | — |
| `turn_start` | — |
| `message_start` | `message: AgentMessage` |
| `message_update` | `message`, `assistantMessageEvent`, often `messageId` |
| `message_end` | `message` |
| `tool_execution_start` | `toolCallId`, `toolName`, `args`, `intent?` |
| `tool_execution_update` | `toolCallId`, `toolName`, `args`, `partialResult` |
| `tool_execution_end` | `toolCallId`, `toolName`, `result`, `isError?` |
| `turn_end` | `message`, `toolResults` |
| `agent_end` | `messages`, `isTerminal?`, `yielded?`, `awaitingAsyncWork?`, `telemetry?`, `coverage?`, optional `messageCount` after compaction |

**AssistantMessageEvent** (`assistantMessageEvent.type`):

- `start`, `text_start`, `text_delta`, `text_end`
- `thinking_start`, `thinking_delta`, `thinking_end`
- `image_end`
- `toolcall_start`, `toolcall_delta`, `toolcall_end` (**spelling: `toolcall_*`, no underscore between tool/call**)
- `done` (`reason: stop|length|toolUse`)
- `error` (`reason: aborted|error`)

Minimal stream sequence (docs):

```json
{ "id": "req_1", "type": "response", "command": "prompt", "success": true }
{ "type": "agent_start" }
{ "type": "message_update", "messageId": "msg-2", "assistantMessageEvent": { "type": "text_delta", "delta": "…" }, "message": { "role": "assistant", "content": [] } }
{ "type": "agent_end", "messages": [], "isTerminal": true, "yielded": true }
{ "type": "prompt_result", "id": "req_1", "agentInvoked": true, "status": "completed", "sessionSettled": true }
{ "type": "session_settled" }
```

Notes:

- Do **not** treat `agent_end.messages` as the full transcript; keep `message_end` stream or page with `get_messages_page`.
- Terminal run: `agent_end` with `isTerminal !== false` (absent field is terminal-compatible).
- Correlate **prompts** via `prompt_result.id`, not via `agent_end`.

### 5.3 Built-in tool execution events vs host tools

**Agent tool calls** appear as `tool_execution_*` session events (args/results already executed inside omp, subject to approval).

**Host-owned tools** (after `set_host_tools`):

```json
{
  "type": "host_tool_call",
  "id": "host_1",
  "toolCallId": "toolu_123",
  "toolName": "echo_host",
  "arguments": { "message": "hello" }
}
```

Optional progress:

```json
{
  "type": "host_tool_update",
  "id": "host_1",
  "partialResult": { "content": [{ "type": "text", "text": "working" }] }
}
```

Complete:

```json
{
  "type": "host_tool_result",
  "id": "host_1",
  "result": { "content": [{ "type": "text", "text": "done" }] }
}
```

Error: set top-level `"isError": true` on `host_tool_result`.  
Cancel from server: `{ "type":"host_tool_cancel", "id":"…", "targetId":"host_1" }`.

### 5.4 Approvals / UI requests

Approvals and dialogs use **`extension_ui_request` / `extension_ui_response`**, not a separate “tool card” frame type.

Outbound:

```json
{
  "type": "extension_ui_request",
  "id": "123",
  "method": "confirm",
  "title": "Confirm",
  "message": "Continue?",
  "timeout": 30000
}
```

**Methods**

| method | request fields | response |
| --- | --- | --- |
| `select` | `title`, `options: string[]`, `optionDetails?: [{description?}]`, `timeout?` | `{ value: string }` |
| `confirm` | `title`, `message`, `timeout?` | `{ confirmed: boolean }` |
| `input` | `title`, `placeholder?`, `timeout?` | `{ value: string }` |
| `editor` | `title`, `prefill?`, `promptStyle?` | `{ value: string }` |
| `ask` | `questions: [{ id, question, header?, options:[{label,description?,preview?}], multi?, recommended? }]`, `timeout?` | `{ answers: [{ id, selectedOptions: string[], customInput? }] }` |
| `cancel` | `targetId` | (server→host: close dialog) |
| `notify` | `message`, `notifyType?: info\|warning\|error` | fire-and-forget |
| `setStatus` | `statusKey`, `statusText?` | fire-and-forget |
| `setWidget` | `widgetKey`, `widgetLines?`, `widgetPlacement?` | fire-and-forget |
| `setTitle` | `title` | suppressed unless `PI_RPC_EMIT_TITLE=1` |
| `set_editor_text` | `text` | fire-and-forget |
| `open_url` | `url`, `launchUrl?`, `instructions?` | fire-and-forget (OAuth) |

Inbound responses:

```json
{ "type": "extension_ui_response", "id": "ui_7", "value": "feature/rpc-host" }
{ "type": "extension_ui_response", "id": "ui_7", "confirmed": true }
{ "type": "extension_ui_response", "id": "ui_7", "cancelled": true, "timedOut": true }
{ "type": "extension_ui_response", "id": "ui_9", "answers": [ { "id": "db", "selectedOptions": [], "customInput": "DuckDB" } ] }
```

Mode boundaries:

| Launch | Extension UI | Tool UI (`ask`, tool dialogs) | Tool **approval** dialogs |
| --- | --- | --- | --- |
| `--mode rpc` | on (unless `--no-ui`) | off | via extension runner when UI available |
| `--mode rpc --no-ui` | headless (defaults) | off | **fail closed** if prompt required |
| `--mode rpc-ui` | on | on | needs UI path; not a separate frame type |
| `--mode rpc-ui --no-ui` | headless | **on** | still fail closed under no-ui for approvals |

Docs: RPC/ACP hosts without an approval dialog receive a **`select`** with the same choices as the TUI (Approve / Approve tool for session / Deny; plus Always… grant variants in `always-ask-cached`).

`set_ask_dialog: true` is required before structured `ask` method frames are emitted.

### 5.5 Abort

```json
{ "id": "a1", "type": "abort" }
{ "id": "a2", "type": "abort_and_prompt", "message": "try a different approach" }
{ "id": "a3", "type": "abort_retry" }
{ "id": "a4", "type": "abort_bash" }
{ "id": "a5", "type": "abort_and_restore_queue" }
```

Host should wait for:

1. `abort` response
2. terminal `agent_end` for the interrupted run
3. `prompt_result` with `status: "aborted"` for any open prompt tickets (session transitions also abort tickets)

`host_tool_cancel` / `host_uri_cancel` / UI `method:"cancel"` cancel the referenced host-side op.

### 5.6 History / state

**`get_state`** → `RpcSessionState`. Local 18.12.5 probe keys included:

`model`, `thinkingLevel`, `isStreaming`, `isCompacting`, `isSettled`, `hasPendingAsyncWork`, `steeringMode`, `followUpMode`, `interruptMode`, `sessionId`, `sessionFile?`, `sessionName?`, `autoCompactionEnabled`, `fastModeEnabled`, `fastModeActive`, `slowModeEnabled`, `slowModeSupported`, `tokensPerSecond`, `messageCount`, `queuedMessageCount`, `queuedMessages`, `todoPhases`, `systemPrompt`, `dumpTools`, `contextUsage`, `goal`

Docs table also lists these fields; newer binaries may add more—preserve unknowns.

**History**

- `get_messages` — full snapshot (prefer under v1 or as fallback).
- `get_messages_page` — preferred under v2; `limit` 1–256 default 100; opaque `nextCursor`; errors `session_busy` / `stale_cursor`.
- `get_entries` / `get_tree` — durable append-history / tree (Pi-compatible command names, OMP-native entry payloads).
- `get_last_assistant_text`, `get_session_stats`, `get_branch_messages`.

### 5.7 Session resume (RPC)

| Mechanism | How |
| --- | --- |
| CLI at spawn | `omp --mode rpc -c` / `-r <id-or-path>` / `--session-dir DIR` / `--restore PATH` |
| Runtime | `open_session` with `sessionDir` (± `provider`/`modelId`) |
| Runtime switch | `switch_session` with `sessionPath` (± model pair) |
| New thread | `new_session` |
| Branch/fork | `branch` / `fork` |

**`open_session` success data:**

```json
{
  "cancelled": false,
  "resumed": true,
  "sessionId": "01a0…",
  "sessionFile": "/srv/threads/t1/2026-….jsonl"
}
```

- `resumed: false` → fresh session in that dir.
- Fails under `--no-session`.
- Restores saved model like `--continue`; if model unrestorable → error, previous session stays active (turn aborted).
- Optional `provider`+`modelId` override like startup `--model`.

### 5.8 Model set

```json
{ "id": "m1", "type": "set_model", "provider": "anthropic", "modelId": "claude-sonnet-4-5" }
```

Success `data` = full **Model** catalog object (`provider`, `id`, plus cost/context/compat metadata—preserve extras).  
Failure:

```json
{
  "id": "m1",
  "type": "response",
  "command": "set_model",
  "success": false,
  "error": "Model not found: provider/model"
}
```

Also: `get_available_models`, `cycle_model`; session event `model_changed` (payload empty—re-`get_state`).

---

## 6. Session storage paths and listing/resume

### Paths (docs + local layout)

| Scope | Typical path |
| --- | --- |
| Default agent dir | `~/.omp/agent` (override `PI_CODING_AGENT_DIR`) |
| Default sessions root | `~/.omp/agent/sessions/` |
| Named profile | `~/.omp/profiles/<name>/agent/sessions/` |
| Per-project bucket | cwd path encoded as directory name, e.g. `-Documents-droi` for `/Users/…/Documents/droi` |
| Session file | `<sessions>/<project>/<timestamp>_<sessionId>.jsonl` |
| Artifacts / subagents | sibling directory same basename as the jsonl |
| Approval remember DB | `approval-cache.db` in agent dir (when using always-ask-cached) |
| Config | `~/.omp/agent/config.yml` |

Local example (this machine):

```text
~/.omp/agent/sessions/-Documents-droi/2026-10-21T03-20-11-032Z_01a15c79-3a18-77ed-be35-8efc47ac865f.jsonl
```

Header lines observed:

```json
{"type":"title","v":1,"title":"","updatedAt":"…"}
{"type":"session","version":3,"id":"01a15c79-3a18-77ed-be35-8efc47ac865f","timestamp":"…","cwd":"/Users/xlong/Documents/droi"}
```

Subagent/child files may set `"parentSession": "<absolute path to parent jsonl>"`.

### How to list / resume

**Outside RPC (CLI / TUI):**

- `omp -c` — continue last for cwd/terminal
- `omp -r` — interactive picker (folder vs all projects via Tab)
- `omp -r <id-prefix|path>`
- `omp --restore <session.jsonl>` — resume and continue unfinished turn/subagents
- `/session info` — authoritative path/id
- Profiles isolate pickers (`--profile` / `OMP_PROFILE`)

**Inside RPC:**

- Spawn with `-r` / `-c` / `--session-dir` / path
- Or `open_session` / `switch_session` after connect
- Discover on-disk files under the sessions root (no dedicated `list_sessions` RPC command in the public catalog); host can scan filesystem or keep its own index
- Read transcript via `get_messages_page` / `get_entries` after bind

**Persistence flags**

- Default: sessions saved under agent dir
- `--no-session`: ephemeral; `open_session`/`fork` persistence features fail
- `--session-dir <dir>`: both save and lookup in that dir (pair with same flag on resume)

---

## 7. Approval modes

Configure `tools.approvalMode` or CLI:

```bash
omp --mode rpc --approval-mode write
omp --mode rpc --auto-approve          # forces yolo
# --yolo also forces yolo even if --approval-mode is set
```

| Mode | Auto-approves | Prompts for |
| --- | --- | --- |
| `always-ask` | `read` | `write`, `exec` |
| `always-ask-cached` | `read` without file targets + granted paths | file reads, `write`, `exec` |
| `write` | `read`, `write` | `exec` |
| `yolo` (**default**) | `read`, `write`, `exec` | none |

Tiers on tools: `read` | `write` | `exec` (missing → `exec`). MCP tools declare `write`.

Per-tool user overrides:

```yaml
tools:
  approvalMode: write
  approval:
    bash: prompt
    read: allow
    mcp__filesystem_delete: deny
```

Resolution summary:

1. Tool `approval(args)` → tier/policy
2. Tool/user `deny` always denies
3. `yolo` allows unless explicit prompt/deny policy
4. Non-yolo: `override: true` (e.g. critical bash) prompts unless tool `policy: allow`
5. Else mode-by-tier; cached grants in `always-ask-cached`

**RPC host implications**

- Without interactive UI (`--no-ui` or host ignoring UI frames), a call that still needs a prompt **fails closed** (no silent allow).
- Hosts get approval as `extension_ui_request` `select` with full details + choices (Approve / session allow / Deny; Always… variants for grants).
- Subagents run headless under `yolo`; parent `task` approval is the boundary; unresolved `prompt` rejects.
- Provider `pendingSafetyChecks` always force interactive prompt; without UI → fail closed.

---

## 8. Go SDK: `github.com/can1357/oh-my-pi/sdk/go/omp-rpc`

### Existence

**Yes.** Lives in the oh-my-pi monorepo under `sdk/go/omp-rpc/`.

```go
module github.com/can1357/oh-my-pi/sdk/go/omp-rpc

go 1.23
```

Package name: **`omprpc`**. Standard library only.

> Note: `proxy.golang.org` timed out in this environment (`TLS handshake timeout`). Source is confirmed on GitHub raw/API. Prefer:
>
> ```bash
> go get github.com/can1357/oh-my-pi/sdk/go/omp-rpc@main
> # or pin a commit / release tag once published to the module proxy
> ```
>
> Because the module path is nested in a monorepo, ensure the repo tags/versions Go expects, or use a commit hash / `replace` to a checkout.

### Layout / what it provides

| File | Role |
| --- | --- |
| `wire.go` | **Generated** types, codecs, per-command params, `Commands` methods |
| `runtime.go` | `Transport`, `Commands`, `UnknownNotification`, `Ptr` |
| `client.go` | `Client`: correlation, v2 negotiate, frame delivery |
| `frame.go` | `rpc_chunk` reassembly |
| `prompt.go` | `PromptAndWait`, paged `GetMessages` |
| `hosttools.go` | Host tool registration/dispatch |
| `hosturis.go` | Host URI schemes |
| `process*.go` | Process group lifecycle on Unix |

### API highlights

```go
cmd := exec.Command("omp", "--mode", "rpc", "--no-session")
client, err := omprpc.Start(ctx, cmd, omprpc.WithHostTools(echo))
defer client.Close()

// drain frames
for frame := range client.Frames() { /* MessageUpdateEvent, ExtensionUiRequest, … */ }

turn, err := client.PromptAndWait(ctx, omprpc.PromptCommand{Message: "say hi"}, time.Minute)
messages, err := client.GetMessages(ctx)
```

- `Start(ctx, *exec.Cmd, opts…)` or `NewClient(ctx, stdout, stdin, opts…)`
- Auto-negotiates protocol **v2** when ready advertises standard limits
- `Prompt` = ack only; `PromptAndWait` waits for that id’s `prompt_result`
- Serves `host_tool_*` / `host_uri_*` internally when registered
- Failed commands → `*omprpc.CommandError{Command, Message, Code}`
- `Close`: cancel host work, close stdin, SIGTERM process group, then SIGKILL (Unix)
- Must drain `Frames()`; unknown types → `UnknownNotification`

Also generated: one typed method per wire command on `Commands`.

### Related SDKs

- TypeScript in-process: `@oh-my-pi/pi-coding-agent` (`createAgentSession`) — **not** the same as RPC; Bun ≥ 1.3.14
- TypeScript RPC helper: `rpc-client.ts` inside coding-agent package
- Python: `omp-rpc` / `omp_rpc.RpcClient`
- Rust: `sdk/rust/omp-rpc`

For a **Go desktop host**, prefer **process RPC** (`omprpc` or hand-rolled JSONL) over the Bun TS SDK.

---

## 9. Host implementation checklist (Go)

1. Spawn `omp --mode rpc` (add `--no-session` or stable `--session-dir` / profile as needed); separate stderr.
2. Wait for `ready`; send `negotiate_protocol` v2; implement chunk reassembly (or use `omprpc`).
3. Unique string ids; correlate `response` / `prompt_result` by id.
4. Continuous stdout reader; never block the reader on one request while UI/tool/URI frames arrive.
5. Track prompt **admission** separately from **completion** (`agentInvoked` / `prompt_result` / `session_settled`).
6. Stream UI: handle `message_update.assistantMessageEvent.text_delta` (and thinking/toolcall if shown).
7. Handle `extension_ui_request` (approvals as `select`, login `open_url`/`input`, optional `ask` after `set_ask_dialog`).
8. Optional: `set_host_tools` / `set_host_uri_schemes` and answer host_* frames.
9. Abort path: `abort` + wait terminal `agent_end` + open `prompt_result`.
10. History: `get_messages_page` under v2; handle `session_busy` / `stale_cursor`.
11. Model: `get_available_models` / `set_model` with `provider` + `modelId`.
12. Approval policy: pick `--approval-mode` at spawn or config; if not yolo, implement UI or accept fail-closed.
13. Shutdown: close stdin, keep reading until EOF, wait process exit.
14. Forward-compat: ignore unknown event types; preserve unknown message fields when persisting.

### Suggested minimal argv for Droi-like host

```bash
omp --mode rpc-ui --no-ui \
  --cwd <workspace> \
  --session-dir <host-managed-dir> \  # or default store
  --approval-mode write \             # or always-ask / yolo per product
  --model <provider/id>               # optional
```

Use `rpc-ui --no-ui` if the host will answer tool dialogs/`ask` but not extension chrome. Use plain `rpc --no-ui` if the host only needs login URL + yolo/auto-approved tools.

---

## 10. Agent message / content shapes (summary)

**Roles:** `user`, `developer`, `assistant`, `toolResult`, `bashExecution`, `pythonExecution`, `custom`, `hookMessage`, `fileMention`, `branchSummary`, `compactionSummary`, …

**Content blocks:** `text`, `thinking`, `redactedThinking`, `image`, `toolCall`, `fallback`, `anthropicServerTool`, …

**Assistant usage / stopReason:**  
`stopReason`: `stop` | `length` | `toolUse` | `error` | `aborted`  
`usage`: input/output/cache/total/cost/…

Preserve unknown roles/blocks when replaying.

---

## 11. Gaps / caveats for implementers

- Public omp.sh RPC page is slightly **behind** github `docs/rpc.md` on some commands (`open_session`, `fork`, `set_event_filter`, `prompt_result.status`, `session_settled`, logout, btw, live, queue edit, etc.). Prefer **github rpc.md + generated wire schema** as canonical.
- No first-class RPC `list_sessions` in the documented command catalog; listing is filesystem/CLI picker.
- Go module may need commit pin until proxy-visible version tags are confirmed.
- Local binary **18.12.5** matched ready/negotiate/`get_state` docs; regenerate/align with `wire.go` when bumping omp.
- Approval prompts under RPC are UI `select` options—exact option **strings** may evolve; prefer matching documented labels and handling cancel/timeout.

---

## 12. Quick reference: most important wire names

```text
Commands:  negotiate_protocol, prompt, steer, follow_up, abort, abort_and_prompt,
           get_state, get_messages, get_messages_page, set_model, get_available_models,
           open_session, switch_session, new_session, fork, branch,
           set_host_tools, set_host_uri_schemes, set_ask_dialog, set_subagent_subscription

Events:    ready, response, rpc_chunk, prompt_result, session_settled,
           agent_start, agent_end, message_start, message_update, message_end,
           tool_execution_start, tool_execution_update, tool_execution_end,
           extension_ui_request, extension_ui_response,
           host_tool_call, host_tool_update, host_tool_result, host_tool_cancel,
           host_uri_request, host_uri_result, host_uri_cancel,
           available_commands_update, notice, extension_error

UI methods: select, confirm, input, editor, ask, cancel, notify, setStatus,
            setWidget, setTitle, set_editor_text, open_url

Approval modes: always-ask | always-ask-cached | write | yolo
```
