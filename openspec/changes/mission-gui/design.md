## Context

Droi 是一个 Electron 40 + React 19 应用，通过 `droid exec --input-format stream-jsonrpc` 子进程与 Droid CLI 交互。当前架构：

- **Main Process**：`DroidExecManager` → `DroidJsonRpcManager` → `DroidJsonRpcSession`（每个 session 一个 droid exec 子进程）
- **Renderer**：Zustand store（`SessionBuffer` per session）、TanStack Router、shadcn/ui 组件
- **通信**：IPC bridge（preload）连接 main process 与 renderer

Droid CLI 0.65.0 的 Missions 模式通过 stream-jsonrpc 协议暴露给 GUI，关键约束：
- stream-jsonrpc 下 `/enter-mission` 等 slash command 不会被执行，必须在 `initialize_session` 时直接设置 `decompSessionType: "orchestrator"`
- Mission worker 是 factoryd 内部的 session（不是 GUI 管理的子进程）
- Mission 状态通过两个通道获取：notification（实时）和 missionDir 磁盘文件（权威）

## Goals / Non-Goals

**Goals:**
- 用户能在 Droi GUI 中创建、监控、暂停/恢复 Mission
- Mission Control 视图提供 Feature 队列、Progress Timeline、Handoff 摘要、操作按钮
- Chat 视图与 Mission Control 视图可切换（类似 CLI Ctrl+G），共享同一 orchestrator session
- 双通道数据保证崩溃恢复能力
- 所有新组件可通过 agent-browser E2E 测试

**Non-Goals:**
- Worker transcript 完整浏览
- Handoff dismiss 管理
- missionModelSettings 配置面板
- validation-contract / validation-state 可视化
- Mission picker（多历史 mission 间切换 UI）
- Feature 编辑/重排/取消（队列纯展示）

## Decisions

### D1: Mission session 创建方式

**决定**：在 `DroidJsonRpcSession.ensureInitialized` 中增加 `decompSessionType` 和 `interactionMode` 参数透传，由上层调用方决定是否创建 orchestrator session。

**理由**：与现有 session 创建流程保持一致，不引入新的子进程管理路径。Mission session 本质上仍是一个 droid exec 子进程，只是 initialize_session 的参数不同。

**替代方案**：为 Mission 创建独立的 ExecRunner 类 -- 但这会导致大量代码重复，且 Mission 的 RPC 协议与普通 session 完全一致。

### D2: MissionPage 与 ChatPage 的关系

**决定**：MissionPage 是独立页面（路由 `/mission`），内部复用 ChatView 和 InputBar 组件。通过 `viewMode: 'chat' | 'mission-control'` 状态切换两个视图。

**理由**：
- Mission 的 UI 需求（Feature 队列、Progress Timeline、Handoff 卡片）远超 ChatPage 的 scope
- 但 orchestrator 的前几轮对话与普通 Chat 完全一致，复用 ChatView 避免重复
- 独立路由使 sidebar 可以根据 session 类型跳转不同页面

**替代方案**：在 ChatPage 内部做条件渲染 -- 会使已经很大的 ChatPage（150+ 行）更加臃肿。

### D3: 状态管理 -- 扩展 SessionBuffer vs 独立 MissionStore

**决定**：扩展 SessionBuffer 增加 `mission` 字段（包含 state/features/progressLog/handoffs），在 `appReducer.ts` 中增加 `missionReducer` 处理 `mission_*` notification。

**理由**：
- Mission 状态与 session 强绑定（1:1 关系），放在 SessionBuffer 里语义更清晰
- 现有的 notification 处理管道（`handleRpcNotification`）已经按 sessionId 路由，扩展最小侵入
- 磁盘数据也按 sessionId 关联

**替代方案**：独立的 Zustand slice -- 会引入 session 与 mission 状态同步的复杂度。

### D4: 双通道数据合并策略

**决定**：notification channel 优先用于实时 UI 更新，磁盘 channel 作为"校正源"。具体策略：

1. **notification 到达**：立即更新 store（低延迟）
2. **磁盘 poll**（间隔 2s，通过 main process 的 MissionDirWatcher）：
   - 读取 state.json / features.json / progress_log.jsonl / handoffs/
   - 与当前 store 对比，如果磁盘数据的 `updatedAt` 更新 → 覆盖 store
3. **首次加载/恢复**：纯从磁盘读取，不依赖 notification history

**理由**：notification 是增量的、可能乱序/丢失；磁盘是全量的、权威的。双通道互补确保 UI 不会长时间处于错误状态。

**文件监听方式**：使用 Node.js 原生 `fs.watch` + setInterval poll 兜底。不引入 chokidar（项目未安装且 mission 文件数量少，原生 watch 足够）。

### D5: Chat/MissionControl 视图自动切换

**决定**：基于 mission state 变化自动切换视图，但用户手动切换后 30 秒内不自动切。

规则：
- `mission_state_changed → running`：自动切到 Mission Control（Chat 此时冻结）
- `mission_state_changed → orchestrator_turn / paused`：自动切到 Chat（需要用户交互）
- 用户手动切换后设置 30 秒 cooldown

**理由**：与 CLI 的 Ctrl+G 行为对齐，同时避免"抢控制权"的糟糕体验。

### D6: MissionDir 路径获取

**决定**：双重来源 -- 从 `ProposeMission` 的 tool_result notification 中提取 missionDir 路径并记录到 SessionBuffer；恢复时按约定路径 `~/.factory/missions/<baseSessionId>` 查找。

**理由**：tool_result 是最可靠的首次获取来源；约定路径确保即使 notification 丢失也能恢复。

### D7: InputBar 在 Mission running 时的行为

**决定**：Mission state 为 `running` 时，InputBar 显示但禁用输入，placeholder 显示"Mission is running. Pause to send a message."，同时显示 Pause 按钮。

**理由**：对齐 CLI 行为（running 时消息不会被接收）。禁用而非隐藏，让用户知道可以暂停后操作。

## Risks / Trade-offs

- **[Risk] fs.watch 跨平台可靠性** → Mitigation: setInterval poll 兜底（每 2s），fs.watch 仅用作"加速检测"。即使 watch 不触发，poll 也能保证 2s 内同步。

- **[Risk] Mission notification 类型不在 protocol.ts 中** → Mitigation: 使用 `{ type: string } & Record<string, unknown>` 通配类型（已有），在 missionReducer 中按 type 字符串匹配处理。

- **[Risk] missionDir 在 GUI 首次启动时不存在** → Mitigation: MissionDirWatcher 在检测到 missionDir 后才开始监听，之前不报错。

- **[Risk] 大量 progress_log 条目导致渲染性能问题** → Mitigation: ProgressTimeline 使用虚拟化列表或限制展示最近 200 条，旧条目可展开加载。

- **[Trade-off] 不做 Feature 编辑** → 首版只读展示，减少"GUI 直接写 missionDir 文件"带来的一致性风险。后续可通过 orchestrator 对话间接修改。

- **[Trade-off] 不做独立 MissionStore** → 扩展 SessionBuffer 更简单但增加了 appReducer 的复杂度。如果未来 Mission 状态变得非常复杂，可能需要重构为独立 slice。
