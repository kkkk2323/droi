## Context

Droi 是一个 Electron 40 + React 19 应用，通过 `droid exec --input-format stream-jsonrpc` 子进程与 Droid CLI 交互。当前架构：

- **Main Process**：`DroidExecManager` → `DroidJsonRpcManager` → `DroidJsonRpcSession`（每个 session 一个 droid exec 子进程）
- **Renderer**：Zustand store（`SessionBuffer` per session）、TanStack Router、shadcn/ui 组件
- **通信**：IPC bridge（preload）连接 main process 与 renderer

Droid CLI 0.65.0 的 Missions 模式通过 stream-jsonrpc 协议暴露给 GUI，关键约束：
- stream-jsonrpc 下 `/enter-mission` 等 slash command 不会被执行，必须在 `initialize_session` 时直接设置 `decompSessionType: "orchestrator"`
- Mission worker 是 factoryd 内部的 session（不是 GUI 管理的子进程）
- Mission 状态通过两个通道获取：notification（实时）和 missionDir 磁盘文件（权威）
- 当前 Droi 发送链路仍然依赖 `autoLevel -> interactionMode(spec/auto)` 的推导，这不足以表达 Mission 需要的 `interactionMode: "agi"`

## Goals / Non-Goals

**Goals:**
- 用户能在 Droi Electron GUI 中创建、监控、暂停 Mission，并在 `paused` / `orchestrator_turn` 时通过普通对话继续推进 Mission
- Mission Control 视图提供 Feature 队列、Progress Timeline、Handoff 摘要、操作按钮
- Chat 视图与 Mission Control 视图可切换（类似 CLI Ctrl+G），共享同一 orchestrator session
- 双通道数据保证崩溃恢复能力
- 所有新组件可通过 agent-browser E2E 测试

**Non-Goals:**
- Web / LAN UI 支持
- Worker transcript 完整浏览
- Handoff dismiss 管理
- missionModelSettings 配置面板
- validation-contract / validation-state 可视化
- Mission picker（多历史 mission 间切换 UI）
- Feature 编辑/重排/取消（队列纯展示）

## Decisions

### D1: Session kind 必须独立于 workspace mode

**决定**：在当前 `PendingNewSessionMode = 'local' | 'new-worktree'` 之外，引入独立的 `sessionKind = 'normal' | 'mission'`。Mission 选择只决定 session 协议和页面路由，不影响 workspace 创建方式。

**理由**：当前 `mode` 已经承担 “本地目录 / 新 worktree” 语义，如果再把 Normal/Mission 混进去，会造成状态模型和 UI 概念混淆。

**替代方案**：复用现有 `mode` 字段 -- 会让 `SessionConfigPage`、store 和 workspace bootstrap 的职责变得不清晰。

### D2: Mission session 使用显式协议字段，且创建后不可降级

**决定**：为 session 引入显式协议字段并贯穿 `createSession`、`exec/send`、`updateSessionSettings`、`saveSession`、`loadSession`：

- `interactionMode`
- `autonomyLevel`
- `decompSessionType`
- `isMission`

Mission session 固定使用：

- `interactionMode: "agi"`
- `decompSessionType: "orchestrator"`

一旦 session 被标记为 Mission，后续任何发送或设置更新都不得把它回写为普通 `spec/auto` session。

**理由**：仅靠 `autoLevel` 无法稳定表达 Mission 所需的 `agi/orchestrator` 组合，而且当前通用发送链路会在后续消息中再次调用 `update_session_settings`。如果不把 Mission 协议字段提升为一等状态，UI 虽然禁止“切回普通模式”，底层仍可能把 Mission session 当作普通 session 处理。

**替代方案**：继续依赖 `autoLevel -> interactionMode` 推导 -- 这无法表达 `agi`，也无法满足 “Mission session 不可降级” 的产品约束。

### D3: Mission 实现范围限定为 Electron-only

**决定**：Mission 支持只覆盖 Electron 渲染层、preload 和 main process，不扩展当前 Hono/Web 路径。

**理由**：用户已明确本次不支持 Web；同时 `missionDir` 监听、桌面会话恢复和 Electron IPC 更贴合当前落地目标。

**替代方案**：同步改造 Hono / browser client -- 价值存在，但会显著扩大范围，不适合当前 change。

### D4: MissionPage 复用完整对话壳，而不是只复用 ChatView/InputBar

**决定**：MissionPage 是独立页面（路由 `/mission`），但其 Chat 视图必须复用当前 ChatPage 的完整对话壳能力，包括：

- `ChatView`
- `InputBar`
- `PermissionCard`
- `AskUserCard`
- `TodoPanel`
- `DebugTracePanel`

页面内部使用 `viewMode: 'chat' | 'mission-control'` 进行切换。

**理由**：
- Mission 的 UI 需求（Feature 队列、Progress Timeline、Handoff 卡片）远超 ChatPage 的 scope
- 但 orchestrator 的对话、权限请求、ask-user、todo 与 debug 行为仍然与普通 chat 高度重叠
- 独立路由使 sidebar 可以根据 session 类型跳转不同页面

**替代方案**：只复用 `ChatView + InputBar` -- 容易遗漏 Mission 期间同样关键的 permission / ask-user / todo 交互。

### D5: 状态管理继续扩展 SessionBuffer，但保存完整 Mission 状态

**决定**：扩展 SessionBuffer 增加 `mission` 字段（包含 state/features/progressLog/handoffs），在 `appReducer.ts` 中增加 `missionReducer` 处理 `mission_*` notification。

**理由**：
- Mission 状态与 session 强绑定（1:1 关系），放在 SessionBuffer 里语义更清晰
- 现有的 notification 处理管道（`handleRpcNotification`）已经按 sessionId 路由，扩展最小侵入
- 磁盘数据也按 sessionId 关联

**替代方案**：独立的 Zustand slice -- 会引入 session 与 mission 状态同步的复杂度。

### D6: 双通道数据合并按文件类型分别定义规则

**决定**：notification channel 继续用于低延迟 UI，磁盘 channel 作为权威恢复源；但合并规则不能只靠一个通用 `updatedAt`。具体规则：

1. **notification 到达**：立即更新 store（低延迟）
2. **磁盘 poll**（间隔 2s，通过 main process 的 MissionDirWatcher）：
   - `state.json`：按 `updatedAt` 覆盖 mission state
   - `features.json`：按文件快照整体替换 feature 列表
   - `progress_log.jsonl`：按事件 identity 追加去重，不能整表覆盖
   - `handoffs/`：按文件名或 handoff id 合并，保留已读取条目
3. **首次加载/恢复**：纯从磁盘读取，不依赖 notification history

**理由**：Mission 文件并不是统一的单对象 schema；如果简单用一个 `updatedAt` 决定全量覆盖，会丢失 append-only 日志和多文件 handoff 数据。

**文件监听方式**：使用 Node.js 原生 `fs.watch` + setInterval poll 兜底。不引入 chokidar（项目未安装且 mission 文件数量少，原生 watch 足够）。

### D7: Pause 后的“继续执行”通过普通对话驱动，不定义单独 Resume RPC

**决定**：本次只提供明确的 `Pause` 与 `Kill Worker` 操作；Mission 在 `paused` 或 `orchestrator_turn` 状态下，由用户在 Chat 视图继续发送消息推进后续流程。

**理由**：根据现有 mission 文档与实测行为，恢复通常表现为 orchestrator 在收到后续消息后再次调用 `start_mission_run`，而不是一个独立、稳定的 Resume RPC。

**替代方案**：设计一个单独的 Resume 按钮和 RPC -- 产品语义看似简单，但容易与实际协议行为脱节。

### D8: Chat/MissionControl 视图自动切换

**决定**：基于 mission state 变化自动切换视图，但用户手动切换后 30 秒内不自动切。

规则：
- `mission_state_changed → running`：自动切到 Mission Control（Chat 此时冻结）
- `mission_state_changed → orchestrator_turn / paused`：自动切到 Chat（需要用户交互）
- 用户手动切换后设置 30 秒 cooldown

**理由**：与 CLI 的 Ctrl+G 行为对齐，同时避免"抢控制权"的糟糕体验。

### D9: MissionDir 路径获取

**决定**：双重来源 -- 从 `ProposeMission` 的 tool_result notification 中提取 missionDir 路径并记录到 SessionBuffer；恢复时按约定路径 `~/.factory/missions/<baseSessionId>` 查找。

**理由**：tool_result 是最可靠的首次获取来源；约定路径确保即使 notification 丢失也能恢复。

### D10: InputBar 在 Mission running 时的行为

**决定**：Mission state 为 `running` 时，InputBar 显示但禁用输入，placeholder 显示"Mission is running. Pause to send a message."，同时显示 Pause 按钮。

**理由**：对齐 CLI 行为（running 时消息不会被接收）。禁用而非隐藏，让用户知道可以暂停后操作。

## Risks / Trade-offs

- **[Risk] fs.watch 跨平台可靠性** → Mitigation: setInterval poll 兜底（每 2s），fs.watch 仅用作"加速检测"。即使 watch 不触发，poll 也能保证 2s 内同步。

- **[Risk] Mission session 与普通 session 的设置流混用** → Mitigation: 在 store、shared protocol、backend manager 三层保存显式 Mission 协议字段，并在 `updateSessionSettings` 上增加 Mission guard。

- **[Risk] Mission notification 类型不在 protocol.ts 中** → Mitigation: 为常用 `mission_*` 通知补充显式类型，同时保留兼容性兜底。

- **[Risk] missionDir 在 GUI 首次启动时不存在** → Mitigation: MissionDirWatcher 在检测到 missionDir 后才开始监听，之前不报错。

- **[Risk] 大量 progress_log 条目导致渲染性能问题** → Mitigation: ProgressTimeline 使用虚拟化列表或限制展示最近 200 条，旧条目可展开加载。

- **[Trade-off] Electron-only** → 降低实现复杂度，但 Web/LAN 用户无法使用 Mission；后续如有需求再单独提 change。

- **[Trade-off] 不做 Feature 编辑** → 首版只读展示，减少"GUI 直接写 missionDir 文件"带来的一致性风险。后续可通过 orchestrator 对话间接修改。

- **[Trade-off] 不做独立 MissionStore** → 扩展 SessionBuffer 更简单但增加了 appReducer 的复杂度。如果未来 Mission 状态变得非常复杂，可能需要重构为独立 slice。
