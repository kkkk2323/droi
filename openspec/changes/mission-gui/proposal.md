## Why

Droi 目前只支持普通的单轮/多轮对话式 session。Droid CLI 0.65.0 引入了 Missions 模式（Orchestrator + Worker + Validators），可以将大任务拆成 feature 队列自动执行，并在 milestone 边界注入验证闸门。当前没有 GUI 支持 Missions，用户无法在 Droi Electron 应用中创建、监控、暂停/恢复 mission，也无法可视化 feature 队列和 worker 进度。

## What Changes

- 新增 Mission 模式入口：在 SessionConfigPage 中增加 Normal/Mission 模式切换，选择 Mission 后以 `decompSessionType: "orchestrator"` + `interactionMode: "agi"` 创建 session
- 新增 MissionPage 页面：包含 Chat 视图与 Mission Control 视图的切换（类似 CLI 的 Ctrl+G），共享同一个 orchestrator session
- Mission Control 面板：展示 Feature 队列（纯展示）、Mission 状态、Progress Timeline、Handoff 摘要
- 双通道数据源：通过 `mission_*` notifications 实时更新 + `fs.watch` 监听 `~/.factory/missions/<baseSessionId>/` 磁盘文件作为权威数据源
- Mission 操作：Pause（interrupt_session）、Kill Worker（kill_worker_session）
- PermissionCard 增强：支持 `propose_mission` 和 `start_mission_run` 确认类型渲染
- InputBar 状态感知：Mission running 时禁用输入并提示"暂停后可发消息"
- Sidebar 标记：Mission session 带图标标记，点击跳转 `/mission` 路由
- 历史 Mission 恢复：通过 `load_session` + missionDir 磁盘读取恢复 mission 状态
- 所有新组件添加 `data-testid` 属性以支持 E2E 测试

## Capabilities

### New Capabilities
- `mission-session`: Mission orchestrator session 的创建、初始化、模式切换（stream-jsonrpc `decompSessionType: "orchestrator"`）
- `mission-control-ui`: Mission Control 视图，包含 Feature 队列展示、状态指示、Progress Timeline、Handoff 卡片、操作按钮（Pause/Kill）
- `mission-data-sync`: 双通道数据同步（notification channel + missionDir disk watcher），包含 reconciler 合并策略和崩溃恢复
- `mission-page-routing`: MissionPage 路由和 Chat/MissionControl 视图切换逻辑，包含基于 mission state 的自动切换

### Modified Capabilities
- (none)

## Impact

- **Renderer 层**：新增 MissionPage、MissionControlPanel 等组件；扩展 Zustand store 的 SessionBuffer 增加 mission 状态字段；新增 missionReducer；路由增加 `/mission`
- **Backend 层**：新增 MissionDirWatcher；DroidJsonRpcSession 增加 orchestrator 创建参数支持；新增 Mission 相关 IPC handlers
- **Shared 层**：protocol.ts 增加 Mission 相关类型（MissionState、Feature、ProgressEntry、Handoff 等）
- **Preload 层**：IPC bridge 增加 Mission 相关方法
- **依赖**：可能需要 `chokidar` 用于可靠的文件监听（需确认项目是否已安装）
