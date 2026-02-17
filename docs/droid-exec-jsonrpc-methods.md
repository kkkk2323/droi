# droid exec `stream-jsonrpc`: notification (`droid.session_notification`) structure and examples (based solely on `droid exec` observations)

This document organizes the observable behaviors under **Droid CLI v0.57.14** from the stdout of **`droid exec --input-format stream-jsonrpc --output-format stream-jsonrpc`**:

* JSON-RPC envelope (request/response/notification)
* client → server callable methods
* server → client requests (permission confirmation / AskUser questionnaire)
* `droid.session_notification` `params.notification` structure and return examples
* Common errors (triggered by intentionally passing incorrect parameters)

> Tested environment: droid `0.57.14`; example model `kimi-k2.5`; `jsonrpc: "2.0"`; `factoryApiVersion: "1.0.0"`.

---

## 1) Startup method (CLI)

`stream-jsonrpc` is essentially **JSONL (one JSON per line)** carrying JSON-RPC 2.0.

```bash
~/.local/bin/droid exec \
  --model kimi-k2.5 \
  --input-format stream-jsonrpc \
  --output-format stream-jsonrpc \
  --cwd <workspace>
```

Optional: `--auto low|medium|high` (affects permission policy; may still receive `droid.request_permission`).

---

## 2) JSON-RPC envelope (observed fields)

### 2.1 Request (client → server)

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"1","method":"...","params":{}}
```

### 2.2 Response (server → client)

Success (most methods return empty object):

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":"2","result":{}}
```

Failure (note: many errors return `id: null`, making precise matching by request `id` impossible):

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":null,"error":{"code":-32600,"message":"Invalid request format"}}
```

### 2.3 Notification (server → client)

The only observed notification method is:

* `method: "droid.session_notification"`

Its business payload is placed in: `params.notification`.

---

## 3) client → server methods (verified working)

### 3.1 `droid.initialize_session`

**request (jsonc, partial `availableModels` omitted)**:

```jsonc
{
  "jsonrpc": "2.0",
  "factoryApiVersion": "1.0.0",
  "type": "request",
  "id": "1",
  "method": "droid.initialize_session",
  "params": {
    "machineId": "machine-probe",
    "cwd": "/path/to/workspace",
    "sessionId": "probe-...",
    "modelId": "gpt-5.1",
    "autonomyLevel": "auto-low",
    "reasoningEffort": "none"
  }
}
```

**response (excerpt)**:

```jsonc
{
  "type": "response",
  "id": "1",
  "result": {
    "sessionId": "probe-...",
    "session": { "messages": [] },
    "settings": {
      "modelId": "gpt-5.1",
      "reasoningEffort": "none",
      "autonomyLevel": "auto-low",
      "specModeReasoningEffort": "none"
    },
    "availableModels": [ /* ... */ ],
    "gitRepo": { "owner": "...", "repoName": "..." }
  }
}
```

**Observed phenomenon related to "plan/spec mode"**: When `initialize_session` does **not** pass `autonomyLevel/reasoningEffort`, the response's `result.settings.autonomyLevel` may be `"spec"` (i.e., starts in plan/spec mode).

### 3.2 `droid.load_session`

Load an existing session to restore historical context. Must be called after `initialize_session`.

**request**:

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"2","method":"droid.load_session","params":{"sessionId":"a3179cea-cbc4-404f-aa54-5ba7e82d23b5"}}
```

**response** (returns complete history messages, settings, available models, etc.; same structure as `initialize_session` response):

```jsonc
{
  "type": "response",
  "id": "2",
  "result": {
    "session": {
      "messages": [
        { "id": "...", "role": "user", "content": [{ "type": "text", "text": "..." }], "parentId": "root" },
        { "id": "...", "role": "assistant", "content": [{ "type": "text", "text": "OK" }], "parentId": "..." }
      ]
    },
    "settings": { "modelId": "kimi-k2.5", "autonomyLevel": "auto-low", /* ... */ },
    "availableModels": [ /* ... */ ],
    "isAgentLoopInProgress": false,
    "gitRepo": { "repoName": "..." },
    "cwd": "/path/to/workspace"
  }
}
```

After calling, the current process's session state switches to the old session. `sessionId` must be a server-generated UUID (provided by `initialize_session` response's `result.sessionId`).

See **7.2 Session Resume** for details.

### 3.3 `droid.add_user_message`

**request**:

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"2","method":"droid.add_user_message","params":{"text":"Just reply OK."}}
```

**response** (immediate ACK; actual output goes through notification stream):

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":"2","result":{}}
```

Typical output is accompanied by:

* `create_message` (records user/assistant message snapshots)
* `assistant_text_delta` (assistant text streaming increments)
* `droid_working_state_changed` (state machine)

### 3.4 `droid.update_session_settings`

**request (example: switching reasoning effort)**:

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"3","method":"droid.update_session_settings","params":{"reasoningEffort":"xhigh"}}
```

**response**:

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":"3","result":{}}
```

If illegal fields/values are passed (e.g., `autonomyLevel: "bogus-level"`), observed error is `-32600 Invalid request format` (with `id: null`).

### 3.5 `droid.interrupt_session`

**request**:

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"4","method":"droid.interrupt_session","params":{}}
```

**response**:

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":"4","result":{}}
```

---

## 4) server → client request (permissions/questionnaire; verified observable)

### 4.1 `droid.request_permission`

When Droid needs tool permission confirmation, it sends a JSON-RPC **request**:

```jsonc
{
  "type": "request",
  "method": "droid.request_permission",
  "id": "1887395f-fcc3-4703-ac35-349cccca8a1f",
  "params": {
    "toolUses": [
      {
        "toolUse": {
          "type": "tool_use",
          "id": "call_bn6NsLqIO1ofhyKmSHzSQcKL",
          "name": "Execute",
          "input": {
            "command": "echo 'hello' > /tmp/droid-perm-test-...txt",
            "timeout": 60,
            "riskLevelReason": "...",
            "riskLevel": "medium"
          }
        },
        "confirmationType": "exec",
        "details": {
          "type": "exec",
          "fullCommand": "echo 'hello' > /tmp/droid-perm-test-...txt",
          "command": "echo",
          "extractedCommands": ["echo"],
          "impactLevel": "medium"
        }
      }
    ],
    "options": [
      { "label": "Yes, allow", "value": "proceed_once" },
      { "label": "Yes, and always allow...", "value": "proceed_always" },
      { "label": "No, cancel", "value": "cancel" }
    ]
  }
}
```

The client must return a **response** with the selected option in `result.selectedOption`:

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":"1887395f-fcc3-4703-ac35-349cccca8a1f","result":{"selectedOption":"proceed_once"}}
```

After confirmation, the notification stream shows `permission_resolved`:

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"notification","method":"droid.session_notification","params":{"notification":{"type":"permission_resolved","requestId":"1887395f-fcc3-4703-ac35-349cccca8a1f","toolUseIds":["call_bn6NsLqIO1ofhyKmSHzSQcKL"],"selectedOption":"proceed_once"}}}
```

### 4.2 `droid.ask_user`

When Droid needs questionnaire interaction, it sends a JSON-RPC **request**:

```json
{"type":"request","jsonrpc":"2.0","factoryApiVersion":"1.0.0","method":"droid.ask_user","id":"52e74dee-c6a9-4325-ab2a-e304c3b2f818","params":{"toolCallId":"call_dkqxtnKTeUCDhJs0actksoW1","questions":[{"index":1,"topic":"Color","question":"Which color do you want?","options":["Red","Blue"]"]}}
```

Example client response:

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":"52e74dee-c6a9-4325-ab2a-e304c3b2f818","result":{"cancelled":false,"answers":[{"index":1,"question":"Which color do you want?","answer":"Red"}]}}
```

Note: Observed that `answers: string[]` (e.g., `["Red"]`) causes the session to stay in `waiting_for_tool_confirmation` and not proceed; use the object array format above.

### 4.3 `droid.request_permission` (ExitSpecMode — spec mode exit confirmation)

When `initialize_session` uses `autonomyLevel: "spec"` to start a session, Droid enters **plan/spec mode**. In this mode, Droid only designs plans without executing any code changes. When the plan is ready, Droid calls the `ExitSpecMode` tool, and the server sends a special `droid.request_permission` request to the client with `confirmationType` of `"exit_spec_mode"`.

#### ExitSpecMode tool parameter structure

`ExitSpecMode`'s `toolUse.input` contains the following fields:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `plan` | `string` | Yes | Complete Markdown text of the spec/plan |
| `title` | `string` | No | Spec title (not always present in observations) |
| `optionNames` | `string[]` | No | Appears when Droid provides multiple optional plans; each item is a plan name label |

#### Scenario 1: Single plan (no optionNames)

When Droid generates a single plan, `input` only contains `plan`:

```jsonc
{
  "type": "request",
  "method": "droid.request_permission",
  "id": "db9be483-...",
  "params": {
    "toolUses": [
      {
        "toolUse": {
          "type": "tool_use",
          "id": "functions.ExitSpecMode:1",
          "name": "ExitSpecMode",
          "input": {
            "plan": "Exit Spec mode to perform file creation operations"
          }
        },
        "confirmationType": "exit_spec_mode",
        "details": {
          "type": "exit_spec_mode",
          "plan": "Exit Spec mode to perform file creation operations"
        }
      }
    ],
    "options": [
      { "label": "Proceed with implementation", "value": "proceed_once", "selectedColor": "#E3992A" },
      { "label": "Proceed, and allow file edits and read-only commands (Low)", "value": "proceed_auto_run_low", "selectedColor": "#E3992A" },
      { "label": "Proceed, and allow reversible commands (Medium)", "value": "proceed_auto_run_medium", "selectedColor": "#E3992A" },
      { "label": "Proceed, and allow all commands (High)", "value": "proceed_auto_run_high", "selectedColor": "#E54048" },
      { "label": "No, keep iterating on spec", "value": "cancel", "selectedColor": "#E54048", "selectedPrefix": "✕ " }
    ]
  }
}
```

#### Scenario 2: Multiple plans (with optionNames)

When Droid provides multiple optional plans, `input` includes the `optionNames` field:

```jsonc
{
  "type": "request",
  "method": "droid.request_permission",
  "id": "f8c6b257-...",
  "params": {
    "toolUses": [
      {
        "toolUse": {
          "type": "tool_use",
          "id": "functions.ExitSpecMode:1",
          "name": "ExitSpecMode",
          "input": {
            "plan": "## Logging System Design\n\n### Plan A: File-based logging system ...\n\n### Plan B: stdout-based structured logging system ...",
            "optionNames": [
              "Plan A - File logging",
              "Plan B - stdout logging"
            ]
          }
        },
        "confirmationType": "exit_spec_mode",
        "details": {
          "type": "exit_spec_mode",
          "plan": "## Logging System Design\n\n### Plan A ...",
          "optionNames": [
            "Plan A - File logging",
            "Plan B - stdout logging"
          ]
        }
      }
    ],
    "options": [
      { "label": "Proceed with implementation", "value": "proceed_once", "selectedColor": "#E3992A" },
      { "label": "Proceed, and allow file edits and read-only commands (Low)", "value": "proceed_auto_run_low", "selectedColor": "#E3992A" },
      { "label": "Proceed, and allow reversible commands (Medium)", "value": "proceed_auto_run_medium", "selectedColor": "#E3992A" },
      { "label": "Proceed, and allow all commands (High)", "value": "proceed_auto_run_high", "selectedColor": "#E54048" },
      { "label": "No, keep iterating on spec", "value": "cancel", "selectedColor": "#E54048", "selectedPrefix": "✕ " }
    ]
  }
}
```

#### Client response

Same as regular `droid.request_permission`, fill selected option in `result.selectedOption`:

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":"db9be483-...","result":{"selectedOption":"proceed_once"}}
```

Available values and meanings (verified):

| value | meaning |
|-------|---------|
| `proceed_once` | Confirm plan, enter implementation (do not auto-authorize subsequent tools) |
| `proceed_auto_run_low` | Confirm plan + auto-authorize Low permissions (file edits and read-only commands) |
| `proceed_auto_run_medium` | Confirm plan + auto-authorize Medium permissions (reversible commands) |
| `proceed_auto_run_high` | Confirm plan + auto-authorize High permissions (all commands) |
| `cancel` | Reject plan, continue iterating spec |

#### Confirmed behavior (verified)

1. Immediately receive `settings_updated` notification — Droid switches from spec mode to normal/auto mode.
2. Receive `permission_resolved` notification.
3. Receive `tool_result` notification (`ExitSpecMode` tool result).
4. Droid begins implementation execution; subsequent tool calls follow normal `droid.request_permission` flow for confirmation (or auto-authorized by auto_run level).

#### Differences from regular `request_permission`

| Characteristic | Regular tool permission | ExitSpecMode |
|--------------|------------------------|--------------|
| `confirmationType` | `"exec"` / `"create"` / `"edit"` etc. | `"exit_spec_mode"` |
| `toolUse.name` | `Execute` / `Create` / `Edit` etc. | `ExitSpecMode` |
| `toolUse.input` | Tool parameters (command / file_path etc.) | `{ plan, optionNames? }` |
| `details.type` | `"exec"` / `"create"` etc. | `"exit_spec_mode"` |
| `details` extra fields | Tool-related (fullCommand / filePath etc.) | `plan`, `optionNames?` |
| `options` | Usually 3 items (allow / always / cancel) | 5 items (includes 3 auto_run levels) |

---

## 5) `droid.session_notification`: observed notification.type and structures

The following `params.notification.type` values are from actual observations (not "inferred").

### 5.1 `settings_updated`

```json
{"type":"notification","method":"droid.session_notification","params":{"notification":{"type":"settings_updated","settings":{"modelId":"gpt-5.1"}}}}
```

### 5.2 `session_title_updated`

```json
{"type":"notification","method":"droid.session_notification","params":{"notification":{"type":"session_title_updated","title":"..."}}}
```

### 5.3 `droid_working_state_changed`

Observed `newState` values (partial list):

* `streaming_assistant_message`
* `executing_tool`
* `waiting_for_tool_confirmation`
* `idle`

```json
{"type":"notification","method":"droid.session_notification","params":{"notification":{"type":"droid_working_state_changed","newState":"executing_tool"}}}
```

### 5.4 `create_message` (user / assistant; content supports text and tool_use)

User message:

```jsonc
{
  "type": "notification",
  "method": "droid.session_notification",
  "params": {
    "notification": {
      "type": "create_message",
      "message": {
        "id": "...",
        "role": "user",
        "content": [{"type":"text","text":"..."}],
        "parentId": "root"
      }
    }
  }
}
```

Assistant message (with `tool_use` content block):

```jsonc
{
  "type": "notification",
  "method": "droid.session_notification",
  "params": {
    "notification": {
      "type": "create_message",
      "message": {
        "id": "72e934ca-f675-43b3-a64a-f696bb532c28",
        "role": "assistant",
        "content": [
          {
            "type": "tool_use",
            "id": "call_yebcxAJ0LWypjQq2j4TWNQF2",
            "name": "Execute",
            "input": {"command": "pwd", "timeout": 60, "riskLevel": "low", "riskLevelReason": "..."}
          }
        ],
        "parentId": "..."
      }
    }
  }
}
```

### 5.5 `assistant_text_delta`

```json
{"type":"notification","method":"droid.session_notification","params":{"notification":{"type":"assistant_text_delta","messageId":"8a2bbdfe-a5a5-45d4-9a47-e52daeb55690","blockIndex":0,"textDelta":"OK"}}}
```

### 5.6 `tool_progress_update`

```jsonc
{
  "type": "notification",
  "method": "droid.session_notification",
  "params": {
    "notification": {
      "type": "tool_progress_update",
      "toolUseId": "call_bn6NsLqIO1ofhyKmSHzSQcKL",
      "toolName": "Execute",
      "update": {"type": "status", "text": "...", "timestamp": 1771239439693}
    }
  }
}
```

### 5.7 `tool_result`

Success example:

```json
{"type":"notification","method":"droid.session_notification","params":{"notification":{"type":"tool_result","toolUseId":"call_bn6NsLqIO1ofhyKmSHzSQcKL","messageId":"3ffae25b-9ad8-4b42-90d3-64602bfdc7ff","content":"Command completed successfully\n\n[Process exited with code 0]"}}}
```

Failure example (exit code 1):

```json
{"type":"notification","method":"droid.session_notification","params":{"notification":{"type":"tool_result","toolUseId":"call_pBVrZ7Yu9CmyipHlA4ZSJJ5m","messageId":"b19a859c-c29e-4646-b168-b1adb937e917","content":"Error: Command failed (exit code: 1)\nls: /path/does/not/exist: No such file or directory\n\n\n[Process exited with code 1]"}}}
```

### 5.8 `session_token_usage_changed`

```jsonc
{
  "type": "notification",
  "method": "droid.session_notification",
  "params": {
    "notification": {
      "type": "session_token_usage_changed",
      "sessionId": "probe4-...",
      "tokenUsage": {
        "inputTokens": 15117,
        "outputTokens": 11,
        "cacheCreationTokens": 0,
        "cacheReadTokens": 0,
        "thinkingTokens": 0
      }
    }
  }
}
```

### 5.9 `mcp_status_changed`

```jsonc
{
  "type": "notification",
  "method": "droid.session_notification",
  "params": {
    "notification": {
      "type": "mcp_status_changed",
      "servers": [
        {"name":"linear","source":"user","isManaged":false,"status":"connecting","serverType":"http","hasAuthTokens":true},
        {"name":"apikit","source":"user","isManaged":false,"status":"connecting","serverType":"http","hasAuthTokens":false}
      ],
      "summary": {"total": 2, "connected": 0, "connecting": 2, "failed": 0, "disabled": 0}
    }
  }
}
```

### 5.10 `mcp_auth_required`

> The `authUrl` in the example below is field-level masked (structure preserved).

```jsonc
{
  "type": "notification",
  "method": "droid.session_notification",
  "params": {
    "notification": {
      "type": "mcp_auth_required",
      "serverName": "linear",
      "authUrl": "https://mcp.linear.app/authorize?...&redirect_uri=http%3A%2F%2Flocalhost%3A54622%2Fcallback&...",
      "message": "Authentication required for linear"
    }
  }
}
```

### 5.11 `permission_resolved`

See **4.1** above.

---

## 6) Common errors (triggered by "intentional mistakes")

### 6.1 `-32700 Invalid JSON-RPC message`

When the sent JSON object doesn't satisfy Droid's JSON-RPC base format (e.g., missing `factoryApiVersion`), returns:

```json
{"jsonrpc":"2.0","type":"response","factoryApiVersion":"1.0.0","id":null,"error":{"code":-32700,"message":"Invalid JSON-RPC message"}}
```

### 6.2 `-32600 Invalid request format`

When the request method/params are not accepted (including: unknown method, illegal settings values, etc.), returns:

```json
{"jsonrpc":"2.0","type":"response","factoryApiVersion":"1.0.0","id":null,"error":{"code":-32600,"message":"Invalid request format"}}
```

---

## 7) Session Resume

### 7.1 CLI `--session-id` parameter

`droid exec` supports `--session-id <id>` for continuing existing sessions:

```bash
# First execution (auto-creates session)
droid exec --auto low "Remember code GRAPE-1122"

# Continue existing session (need to provide prompt)
droid exec -s <session-id> "what code did I mention?"
```

CLI help describes:
> `Loads conversation history for context but does NOT replay old messages in output`

i.e., after resuming, AI has full context but won't re-output historical messages.

### 7.2 `stream-jsonrpc` mode session resume: `droid.load_session` (verified)

> Tested environment: droid `0.57.14`; macOS (darwin 24.6.0); model `kimi-k2.5`.
> Since macOS lacks `timeout` command, use `background process + sleep + kill` to control process lifecycle.

#### 7.2.1 Key findings

In `stream-jsonrpc` mode:

* `droid.initialize_session` can only **create new sessions**; even when passing an old `sessionId`, it won't restore historical context (`session.messages` is always `[]`).
* To resume a session, use **`droid.load_session`** method — first `initialize_session` to create a new session, then `load_session` to load the old session context.
* Calling other methods without `initialize_session` returns error: `No active session. Call initialize_session or load_session first.`
* `sessionId` must be a **server-auto-generated UUID format** (e.g., `a3179cea-cbc4-404f-aa54-5ba7e82d23b5`); custom strings (e.g., `my-session-1`) won't cause errors in `initialize_session`, but `load_session` cannot find the corresponding session.

#### 7.2.2 `droid.load_session` method

**request**:

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"2","method":"droid.load_session","params":{"sessionId":"a3179cea-cbc4-404f-aa54-5ba7e82d23b5"}}
```

**response** (success returns complete history messages and settings):

```jsonc
{
  "type": "response",
  "id": "2",
  "result": {
    "session": {
      "messages": [
        {
          "id": "f5a14b7d-...",
          "role": "user",
          "content": [
            { "type": "text", "text": "<system-reminder>...</system-reminder>" },
            { "type": "text", "text": "The password is DOLPHIN-2288. Just reply OK." }
          ],
          "createdAt": 1771250960382,
          "updatedAt": 1771250960382,
          "parentId": "root"
        },
        {
          "id": "2ead7df5-...",
          "role": "assistant",
          "content": [{ "type": "text", "text": "OK" }],
          "parentId": "f5a14b7d-...",
          "createdAt": 1771250963471,
          "updatedAt": 1771250963471
        }
      ]
    },
    "settings": {
      "modelId": "kimi-k2.5",
      "reasoningEffort": "off",
      "autonomyLevel": "auto-low",
      "specModeReasoningEffort": "none"
    },
    "availableModels": [ /* ... */ ],
    "isAgentLoopInProgress": false,
    "gitRepo": { "repoName": "..." },
    "cwd": "/path/to/workspace"
  }
}
```

**Note**: In `load_session` response's `session.messages[]`, user messages' `content` includes droid-injected `<system-reminder>` blocks (environment info, git status, etc.); the original user text is the last text content.

#### 7.2.3 Correct session resume flow

```
Client                                       droid exec (stream-jsonrpc)
  │                                                  │
  │── initialize_session (no old sessionId needed) ──>│
  │<──── response: new sessionId, messages: [] ──────│
  │                                                  │
  │── load_session { sessionId: "old-session-id" } ──>│
  │<──── response: messages: [history messages...] ──│
  │                                                  │
  │── add_user_message { text: "continue..." } ─────>│
  │<──── notification stream (AI remembers context) ─│
```

#### 7.2.4 Test script (directly reproducible)

```bash
#!/bin/bash
DROID=~/.local/bin/droid
CWD=/tmp/droid-resume-clean   # needs to be a git repo
mkdir -p "$CWD" && cd "$CWD" && git init && echo test > README.md && git add . && git commit -m init

# ========== Turn 1: establish session, let AI remember password ==========
echo "=== TURN 1 ==="
(
  # Don't pass sessionId, let server auto-generate UUID
  printf '{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"1","method":"droid.initialize_session","params":{"machineId":"m1","cwd":"%s","modelId":"kimi-k2.5","autonomyLevel":"auto-low"}}\n' "$CWD"
  sleep 5
  printf '{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"2","method":"droid.add_user_message","params":{"text":"The password is DOLPHIN-2288. Just reply OK."}}\n'
  sleep 20
) | "$DROID" exec --input-format stream-jsonrpc --output-format stream-jsonrpc --cwd "$CWD" 2>/dev/null > /tmp/t1.jsonl &
PID1=$!; sleep 28; kill $PID1 2>/dev/null; wait $PID1 2>/dev/null

# Extract server-generated sessionId (UUID format)
OLD_SID=$(python3 -c "
import json
for line in open('/tmp/t1.jsonl'):
    line=line.strip()
    if not line: continue
    try:
        d=json.loads(line)
        sid=d.get('result',{}).get('sessionId','')
        if sid: print(sid); break
    except: pass
")
echo "Old sessionId: $OLD_SID"
# ——— droid process has exited ———

# ========== Turn 2: new process + load_session resume ==========
echo ""
echo "=== TURN 2 (load_session resume) ==="
(
  # 1. initialize_session (create new session, don't pass old sessionId)
  printf '{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"1","method":"droid.initialize_session","params":{"machineId":"m1","cwd":"%s","modelId":"kimi-k2.5","autonomyLevel":"auto-low"}}\n' "$CWD"
  sleep 3
  # 2. load_session (load old session context)
  printf '{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"2","method":"droid.load_session","params":{"sessionId":"%s"}}\n' "$OLD_SID"
  sleep 5
  # 3. Ask AI if it remembers the password
  printf '{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"3","method":"droid.add_user_message","params":{"text":"What password did I tell you? Reply ONLY the password."}}\n'
  sleep 20
) | "$DROID" exec --input-format stream-jsonrpc --output-format stream-jsonrpc --cwd "$CWD" 2>/dev/null > /tmp/t2.jsonl &
PID2=$!; sleep 30; kill $PID2 2>/dev/null; wait $PID2 2>/dev/null

# ========== Verification ==========
python3 -c "
import json, sys
for line in open('/tmp/t2.jsonl'):
    line=line.strip()
    if not line: continue
    try:
        d=json.loads(line)
        # load_session returns historical message count
        r=d.get('result',{})
        if r.get('session') and d.get('id')=='2':
            msgs=r['session'].get('messages',[])
            print(f'load_session -> {len(msgs)} history messages')
        # AI reply
        n=d.get('params',{}).get('notification',{})
        if n.get('type')=='assistant_text_delta':
            sys.stdout.write(n.get('textDelta',''))
    except: pass
print()
"

grep -q 'DOLPHIN' /tmp/t2.jsonl \
  && echo 'RESULT: SUCCESS - AI remembered the password!' \
  || echo 'RESULT: FAILED'
```

#### 7.2.5 Actual output

**Turn 1**: `initialize_session` without `sessionId`, server generates `a3179cea-cbc4-404f-aa54-5ba7e82d23b5`. AI replies `OK`.

**Turn 2**:

```
load_session -> 2 history messages        # user + assistant
AI reply: DOLPHIN-2288                    # successfully recalled password!
RESULT: SUCCESS - AI remembered the password!
```

`load_session` response returned 2 historical messages (user's password message + assistant's OK reply); subsequent `add_user_message` AI successfully recalled the password based on context.

#### 7.2.6 Incorrect resume methods comparison (also verified)

The following methods **cannot restore context**:

| Method | Context restored | Notes |
|--------|------------------|-------|
| `initialize_session` with old `sessionId` (server UUID) | **No** | `session.messages` always empty |
| `initialize_session` with custom `sessionId` (non-UUID) | **No** | Same as above, and `load_session` can't find it |
| CLI `exec -s <sessionId>` (text mode) | **Unknown** | Actual exit code 1, no output (needs TTY interactive mode) |
| `initialize_session` + `load_session` | **Yes** | Correct method, see above |

#### 7.2.7 Additional notes

* `load_session` overwrites the current process's session state — after calling, current session switches to the old session.
* `load_session` response's `session.messages[].content` includes droid-injected `<system-reminder>` blocks (environment info, git status, CLI tool versions, etc.); client doesn't need additional processing (droid automatically manages system context).
* `load_session` response structure is the same as `initialize_session` response (includes `settings`, `availableModels`, `gitRepo`, etc.), can be used to update client state.

### 7.3 Reusing same `sessionId` with different API Keys (verified: reusable)

> Purpose: Verify whether the **same server UUID `sessionId`** can still reuse historical context via `droid.load_session` under **different `FACTORY_API_KEY`** (to determine if App API KEY rotation breaks session recovery).

#### 7.3.1 Conclusion

In **`stream-jsonrpc`** mode:

* A server UUID `sessionId` created with `KEY1` can still `load_session` successfully after switching to `KEY2`.
* When `KEY2` continues the conversation, AI can correctly recall the random token written in the `KEY1` session.

i.e., **same sessionId + different API Key can still reuse session context** (at least when both keys can access the same account/tenant resources).

#### 7.3.2 Reproduction script

Note: Current droid CLI `0.57.14` doesn't support `--mode`, use `--model` instead (`--mode` will prompt `Did you mean --model?`).

```bash
# Don't put keys in scripts/repos; inject via environment variables
export KEY1='...'
export KEY2='...'

python3 scripts/test-droid-session-apikey-reuse.py
```

Script output includes:

* `turn1.sessionId <uuid>`
* `turn2.load_session.ok true/false`
* `token.found_in_turn2 True/False`

---

## 8) Client implementation notes (based on samples)

* **Notifications may duplicate**: The same `create_message` / `tool_result` / `droid_working_state_changed` appeared repeatedly in samples; clients need to implement idempotency/deduplication.
* **Don't rely on error response `id`**: `-32600/-32700` commonly returns `id: null` in observations.
