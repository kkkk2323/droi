# droid exec `stream-jsonrpc`：notification（`droid.session_notification`）结构与示例（仅以 droid exec 实测为准）

本文以 **`droid exec --input-format stream-jsonrpc --output-format stream-jsonrpc` 的 stdout** 为唯一事实来源，整理在 **Droid CLI v0.57.14** 下可观测到的：

* JSON-RPC envelope（request/response/notification）
* client → server 可调用方法
* server → client 的 request（权限确认 / AskUser 问卷）
* `droid.session_notification` 的 `params.notification` 结构与返回示例
* 常见报错（通过故意传入错误参数触发）

> 实测环境：droid `0.57.14`；示例 model 使用 `kimi-k2.5`；`jsonrpc: "2.0"`；`factoryApiVersion: "1.0.0"`。

---

## 1) 启动方式（CLI）

`stream-jsonrpc` 本质是 **JSONL（每行一个 JSON）** 形式承载 JSON-RPC 2.0。

```bash
~/.local/bin/droid exec \
  --model kimi-k2.5 \
  --input-format stream-jsonrpc \
  --output-format stream-jsonrpc \
  --cwd <workspace>
```

可选：`--auto low|medium|high`（影响权限策略，仍然可能收到 `droid.request_permission`）。

---

## 2) JSON-RPC envelope（实测字段）

### 2.1 Request（client → server）

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"1","method":"...","params":{}}
```

### 2.2 Response（server → client）

成功（多数 method 返回空对象）：

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":"2","result":{}}
```

失败（注意：实测中多种错误会返回 `id: null`，无法按请求 `id` 做精确匹配）：

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":null,"error":{"code":-32600,"message":"Invalid request format"}}
```

### 2.3 Notification（server → client）

实测可观测到的 notification method 为：

* `method: "droid.session_notification"`

其业务载荷统一放在：`params.notification`。

---

## 3) client → server methods（已实测可用）

### 3.1 `droid.initialize_session`

**request（jsonc，省略了部分 `availableModels`）**：

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

**response（节选）**：

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

**与“plan/spec 模式”相关的实测现象**：当 `initialize_session` **不传** `autonomyLevel/reasoningEffort` 时，response 的 `result.settings.autonomyLevel` 可能会是 `"spec"`（即以规划/规范模式启动）。

### 3.2 `droid.load_session`

加载已有会话，恢复历史上下文。需在 `initialize_session` 之后调用。

**request**：

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"2","method":"droid.load_session","params":{"sessionId":"a3179cea-cbc4-404f-aa54-5ba7e82d23b5"}}
```

**response**（返回完整历史消息、设置、可用模型等，结构与 `initialize_session` response 相同）：

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

调用后当前进程的会话状态切换为旧会话。`sessionId` 必须是服务端生成的 UUID（由 `initialize_session` response 的 `result.sessionId` 提供）。

详细说明见 **7.2 会话恢复**。

### 3.3 `droid.add_user_message`

**request**：

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"2","method":"droid.add_user_message","params":{"text":"只回复 OK。"}}
```

**response**（立即 ACK，具体输出走 notification 流）：

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":"2","result":{}}
```

典型输出会伴随：

* `create_message`（记录 user/assistant 消息快照）
* `assistant_text_delta`（assistant 文本流式增量）
* `droid_working_state_changed`（状态机）

### 3.4 `droid.update_session_settings`

**request（示例：切换推理强度）**：

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"3","method":"droid.update_session_settings","params":{"reasoningEffort":"xhigh"}}
```

**response**：

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":"3","result":{}}
```

若传入非法字段/值（例如 `autonomyLevel: "bogus-level"`），实测会收到 `-32600 Invalid request format`（且 `id: null`）。

### 3.5 `droid.interrupt_session`

**request**：

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"4","method":"droid.interrupt_session","params":{}}
```

**response**：

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":"4","result":{}}
```

---

## 4) server → client request（权限/问卷；已实测可观测）

### 4.1 `droid.request_permission`

当 Droid 需要工具权限确认时，会发出 JSON-RPC **request**：

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

client 需要回一个 **response**，并在 `result.selectedOption` 填入所选项：

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":"1887395f-fcc3-4703-ac35-349cccca8a1f","result":{"selectedOption":"proceed_once"}}
```

确认后，notification 流中会出现 `permission_resolved`：

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"notification","method":"droid.session_notification","params":{"notification":{"type":"permission_resolved","requestId":"1887395f-fcc3-4703-ac35-349cccca8a1f","toolUseIds":["call_bn6NsLqIO1ofhyKmSHzSQcKL"],"selectedOption":"proceed_once"}}}
```

### 4.2 `droid.ask_user`

当 Droid 需要问卷交互时，会发出 JSON-RPC **request**：

```json
{"type":"request","jsonrpc":"2.0","factoryApiVersion":"1.0.0","method":"droid.ask_user","id":"52e74dee-c6a9-4325-ab2a-e304c3b2f818","params":{"toolCallId":"call_dkqxtnKTeUCDhJs0actksoW1","questions":[{"index":1,"topic":"Color","question":"你想选哪个颜色？","options":["Red","Blue"]}]}}
```

client 回包示例：

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":"52e74dee-c6a9-4325-ab2a-e304c3b2f818","result":{"cancelled":false,"answers":[{"index":1,"question":"你想选哪个颜色？","answer":"Red"}]}}
```

注意：实测 `answers: string[]`（如 `["Red"]`）会导致会话停留在 `waiting_for_tool_confirmation`，后续不再继续；请使用上面的对象数组格式。

### 4.3 `droid.request_permission`（ExitSpecMode —— spec 模式退出确认）

当 `initialize_session` 使用 `autonomyLevel: "spec"` 启动会话时，Droid 进入**规划/规范模式（spec mode）**。在此模式下，Droid 只做方案设计不执行任何代码变更。当方案就绪后，Droid 会调用 `ExitSpecMode` 工具，此时 server 会向 client 发出一个特殊的 `droid.request_permission` request，`confirmationType` 为 `"exit_spec_mode"`。

#### ExitSpecMode 工具参数结构

`ExitSpecMode` 的 `toolUse.input` 包含以下字段：

| 字段 | 类型 | 是否必填 | 说明 |
|------|------|----------|------|
| `plan` | `string` | 是 | 规范/方案的完整 Markdown 文本 |
| `title` | `string` | 否 | 规范标题（实测中并非总是出现） |
| `optionNames` | `string[]` | 否 | 当 Droid 提供多个可选方案时出现，数组中每项为方案名称标签 |

#### 场景一：单方案（无 optionNames）

当 Droid 生成单一方案时，`input` 中只有 `plan`：

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
            "plan": "退出 Spec 模式以执行文件创建操作"
          }
        },
        "confirmationType": "exit_spec_mode",
        "details": {
          "type": "exit_spec_mode",
          "plan": "退出 Spec 模式以执行文件创建操作"
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

#### 场景二：多方案（含 optionNames）

当 Droid 提供多个可选方案供用户选择时，`input` 中会多出 `optionNames` 字段：

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
            "plan": "## 日志系统设计方案\n\n### 方案 A：基于文件的日志系统 ...\n\n### 方案 B：基于 stdout 的结构化日志系统 ...",
            "optionNames": [
              "方案 A - 文件日志",
              "方案 B - stdout 日志"
            ]
          }
        },
        "confirmationType": "exit_spec_mode",
        "details": {
          "type": "exit_spec_mode",
          "plan": "## 日志系统设计方案\n\n### 方案 A ...",
          "optionNames": [
            "方案 A - 文件日志",
            "方案 B - stdout 日志"
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

#### client 回包

与普通 `droid.request_permission` 一致，在 `result.selectedOption` 填入所选项：

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"response","id":"db9be483-...","result":{"selectedOption":"proceed_once"}}
```

可选值与含义（实测）：

| value | 含义 |
|-------|------|
| `proceed_once` | 确认方案，进入实现（不自动授权后续工具） |
| `proceed_auto_run_low` | 确认方案 + 自动授权 Low 权限（文件编辑和只读命令） |
| `proceed_auto_run_medium` | 确认方案 + 自动授权 Medium 权限（可逆命令） |
| `proceed_auto_run_high` | 确认方案 + 自动授权 High 权限（所有命令） |
| `cancel` | 拒绝方案，继续迭代规范 |

#### 确认后的行为（实测）

1. 立即收到 `settings_updated` notification —— Droid 从 spec 模式切换到 normal/auto 模式。
2. 收到 `permission_resolved` notification。
3. 收到 `tool_result` notification（`ExitSpecMode` 工具的结果）。
4. Droid 开始执行实现，后续工具调用按正常 `droid.request_permission` 流程逐一确认（或被 auto_run 级别自动授权）。

#### 与普通 `request_permission` 的区别

| 特征 | 普通工具权限 | ExitSpecMode |
|------|-------------|--------------|
| `confirmationType` | `"exec"` / `"create"` / `"edit"` 等 | `"exit_spec_mode"` |
| `toolUse.name` | `Execute` / `Create` / `Edit` 等 | `ExitSpecMode` |
| `toolUse.input` | 工具参数（command / file_path 等） | `{ plan, optionNames? }` |
| `details.type` | `"exec"` / `"create"` 等 | `"exit_spec_mode"` |
| `details` 额外字段 | 工具相关（fullCommand / filePath 等） | `plan`、`optionNames?` |
| `options` | 通常 3 项（allow / always / cancel） | 5 项（含 3 个 auto_run 级别） |

---

## 5) `droid.session_notification`：已观测到的 notification.type 与结构

下列 `params.notification.type` 均来自实测样本（非“推断”）。

### 5.1 `settings_updated`

```json
{"type":"notification","method":"droid.session_notification","params":{"notification":{"type":"settings_updated","settings":{"modelId":"gpt-5.1"}}}}
```

### 5.2 `session_title_updated`

```json
{"type":"notification","method":"droid.session_notification","params":{"notification":{"type":"session_title_updated","title":"..."}}}
```

### 5.3 `droid_working_state_changed`

实测出现过的 `newState`（节选）：

* `streaming_assistant_message`
* `executing_tool`
* `waiting_for_tool_confirmation`
* `idle`

```json
{"type":"notification","method":"droid.session_notification","params":{"notification":{"type":"droid_working_state_changed","newState":"executing_tool"}}}
```

### 5.4 `create_message`（user / assistant；content 支持 text 与 tool_use）

user 消息：

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

assistant 消息（含 `tool_use` 内容块）：

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

成功示例：

```json
{"type":"notification","method":"droid.session_notification","params":{"notification":{"type":"tool_result","toolUseId":"call_bn6NsLqIO1ofhyKmSHzSQcKL","messageId":"3ffae25b-9ad8-4b42-90d3-64602bfdc7ff","content":"Command completed successfully\n\n[Process exited with code 0]"}}}
```

失败示例（exit code 1）：

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

> 下例 `authUrl` 做了字段级打码（保留结构）。

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

见上文 **4.1**。

---

## 6) 常见报错（通过“故意传错”触发）

### 6.1 `-32700 Invalid JSON-RPC message`

当发送的 JSON 对象不满足 Droid 的 JSON-RPC 基础格式（例如缺少 `factoryApiVersion`）时，实测返回：

```json
{"jsonrpc":"2.0","type":"response","factoryApiVersion":"1.0.0","id":null,"error":{"code":-32700,"message":"Invalid JSON-RPC message"}}
```

### 6.2 `-32600 Invalid request format`

当请求 method/params 不被接受（包括：未知 method、非法 settings 值等）时，实测返回：

```json
{"jsonrpc":"2.0","type":"response","factoryApiVersion":"1.0.0","id":null,"error":{"code":-32600,"message":"Invalid request format"}}
```

---

## 7) 会话恢复（Session Resume）

### 7.1 CLI `--session-id` 参数

`droid exec` 支持 `--session-id <id>` 参数用于继续已有会话：

```bash
# 首次执行（自动创建 session）
droid exec --auto low "Remember code GRAPE-1122"

# 继续已有 session（需要提供 prompt）
droid exec -s <session-id> "what code did I mention?"
```

CLI 帮助文档描述为：
> `Loads conversation history for context but does NOT replay old messages in output`

即恢复后 AI 拥有完整上下文，但不会重新输出历史消息。

### 7.2 `stream-jsonrpc` 模式的会话恢复：`droid.load_session`（实测）

> 实测环境：droid `0.57.14`；macOS (darwin 24.6.0)；model `kimi-k2.5`。
> 因 macOS 无 `timeout` 命令，使用 `后台进程 + sleep + kill` 控制进程生命周期。

#### 7.2.1 关键发现

在 `stream-jsonrpc` 模式下：

* `droid.initialize_session` 只能**创建新会话**，即使传入旧的 `sessionId` 也不会恢复历史上下文（`session.messages` 始终为 `[]`）。
* 恢复会话需要使用 **`droid.load_session`** 方法——先 `initialize_session` 创建新会话，再 `load_session` 加载旧会话上下文。
* 不发 `initialize_session` 直接调用其他方法，会返回错误：`No active session. Call initialize_session or load_session first.`
* `sessionId` 必须是**服务端自动生成的 UUID 格式**（如 `a3179cea-cbc4-404f-aa54-5ba7e82d23b5`），自定义字符串（如 `my-session-1`）虽然 `initialize_session` 不报错，但 `load_session` 无法找到对应会话。

#### 7.2.2 `droid.load_session` 方法

**request**：

```json
{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"2","method":"droid.load_session","params":{"sessionId":"a3179cea-cbc4-404f-aa54-5ba7e82d23b5"}}
```

**response**（成功时返回完整历史消息和设置）：

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

**注意**：`load_session` 返回的 `session.messages` 中，user 消息的 `content` 包含 droid 注入的 `<system-reminder>` 块（环境信息、git 状态等），原始用户文本是最后一个 text content。

#### 7.2.3 正确的会话恢复流程

```
客户端                                       droid exec (stream-jsonrpc)
  │                                                  │
  │── initialize_session (无需旧 sessionId) ────────>│
  │<──── response: 新 sessionId, messages: [] ───────│
  │                                                  │
  │── load_session { sessionId: "旧会话ID" } ───────>│
  │<──── response: messages: [历史消息...] ──────────│
  │                                                  │
  │── add_user_message { text: "继续对话..." } ─────>│
  │<──── notification 流 (AI 能记住之前的上下文) ────│
```

#### 7.2.4 测试脚本（可直接复现）

```bash
#!/bin/bash
DROID=~/.local/bin/droid
CWD=/tmp/droid-resume-clean   # 需要是 git 仓库
mkdir -p "$CWD" && cd "$CWD" && git init && echo test > README.md && git add . && git commit -m init

# ========== Turn 1：建立会话，让 AI 记住密码 ==========
echo "=== TURN 1 ==="
(
  # 不传 sessionId，让服务端自动生成 UUID
  printf '{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"1","method":"droid.initialize_session","params":{"machineId":"m1","cwd":"%s","modelId":"kimi-k2.5","autonomyLevel":"auto-low"}}\n' "$CWD"
  sleep 5
  printf '{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"2","method":"droid.add_user_message","params":{"text":"The password is DOLPHIN-2288. Just reply OK."}}\n'
  sleep 20
) | "$DROID" exec --input-format stream-jsonrpc --output-format stream-jsonrpc --cwd "$CWD" 2>/dev/null > /tmp/t1.jsonl &
PID1=$!; sleep 28; kill $PID1 2>/dev/null; wait $PID1 2>/dev/null

# 提取服务端生成的 sessionId（UUID 格式）
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
# ——— 此时 droid 进程已退出 ———

# ========== Turn 2：新进程 + load_session 恢复 ==========
echo ""
echo "=== TURN 2 (load_session resume) ==="
(
  # 1. initialize_session（创建新会话，不传旧 sessionId）
  printf '{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"1","method":"droid.initialize_session","params":{"machineId":"m1","cwd":"%s","modelId":"kimi-k2.5","autonomyLevel":"auto-low"}}\n' "$CWD"
  sleep 3
  # 2. load_session（加载旧会话上下文）
  printf '{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"2","method":"droid.load_session","params":{"sessionId":"%s"}}\n' "$OLD_SID"
  sleep 5
  # 3. 询问 AI 是否记得密码
  printf '{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"3","method":"droid.add_user_message","params":{"text":"What password did I tell you? Reply ONLY the password."}}\n'
  sleep 20
) | "$DROID" exec --input-format stream-jsonrpc --output-format stream-jsonrpc --cwd "$CWD" 2>/dev/null > /tmp/t2.jsonl &
PID2=$!; sleep 30; kill $PID2 2>/dev/null; wait $PID2 2>/dev/null

# ========== 验证 ==========
python3 -c "
import json, sys
for line in open('/tmp/t2.jsonl'):
    line=line.strip()
    if not line: continue
    try:
        d=json.loads(line)
        # load_session 返回的历史消息数
        r=d.get('result',{})
        if r.get('session') and d.get('id')=='2':
            msgs=r['session'].get('messages',[])
            print(f'load_session -> {len(msgs)} history messages')
        # AI 回复
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

#### 7.2.5 实测输出

**Turn 1**：`initialize_session` 不传 `sessionId`，服务端生成 `a3179cea-cbc4-404f-aa54-5ba7e82d23b5`。AI 回复 `OK`。

**Turn 2**：

```
load_session -> 2 history messages        # user + assistant
AI reply: DOLPHIN-2288                    # 成功回忆密码！
RESULT: SUCCESS - AI remembered the password!
```

`load_session` response 中返回了 2 条历史消息（user 的密码消息 + assistant 的 OK 回复），后续 `add_user_message` 时 AI 成功基于上下文回忆出密码。

#### 7.2.6 错误的恢复方式对比（同样经过实测）

以下方式均**无法恢复上下文**：

| 方法 | 是否恢复上下文 | 说明 |
|------|---------------|------|
| `initialize_session` 传入旧 `sessionId`（服务端 UUID） | **否** | `session.messages` 始终为空 |
| `initialize_session` 传入自定义 `sessionId`（非 UUID） | **否** | 同上，且 `load_session` 也找不到该会话 |
| CLI `exec -s <sessionId>`（text 模式） | **未知** | 实测 exit code 1，无输出（需要 TTY 交互模式） |
| `initialize_session` + `load_session` | **是** | 正确方式，见上文 |

#### 7.2.7 补充说明

* `load_session` 会覆盖当前进程的会话状态——调用后当前 session 切换为旧会话。
* `load_session` response 中的 `session.messages[].content` 包含 droid 注入的 `<system-reminder>` 块（环境信息、git 状态、命令行工具版本等），客户端无需额外处理（droid 会自动管理系统上下文）。
* `load_session` response 的结构与 `initialize_session` response 相同（含 `settings`、`availableModels`、`gitRepo` 等），可以用来更新客户端状态。

### 7.3 不同 API Key 下复用同一 `sessionId`（实测：可复用）

> 目的：验证 **同一个服务端 UUID `sessionId`** 在 **不同 `FACTORY_API_KEY`** 下，是否还能通过 `droid.load_session` 复用历史上下文（从而判断 App 里 API KEY 轮换是否会破坏会话恢复）。

#### 7.3.1 结论

在 **`stream-jsonrpc`** 模式下：

* 使用 `KEY1` 创建会话得到的服务端 UUID `sessionId`，在切换到 `KEY2` 后仍可 `load_session` 成功。
* `KEY2` 随后继续对话时，AI 能正确回忆 `KEY1` 会话中写入的随机 token。

即：**相同 sessionId + 不同 API Key 仍可复用会话上下文**（至少在两把 key 均能访问同一账户/租户资源的前提下）。

#### 7.3.2 复现脚本

说明：当前 droid CLI `0.57.14` 不支持 `--mode`，应使用 `--model`（`--mode` 会提示 `Did you mean --model?`）。

```bash
# 不要把 key 写进脚本/仓库；通过环境变量注入
export KEY1='...'
export KEY2='...'

python3 scripts/test-droid-session-apikey-reuse.py
```

脚本输出会包含：

* `turn1.sessionId <uuid>`
* `turn2.load_session.ok true/false`
* `token.found_in_turn2 True/False`

---

## 8) 客户端实现注意点（基于样本）

* **通知可能重复**：同一个 `create_message` / `tool_result` / `droid_working_state_changed` 在样本中出现过重复发送；客户端需做幂等/去重。
* **不要依赖 error response 的 `id`**：`-32600/-32700` 实测常见 `id: null`。
