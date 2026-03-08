## Why

Droi 当前只把 session 当作普通 chat session 处理，主链路依赖 `autoLevel -> interactionMode(spec/auto)` 的隐式映射。Droid CLI 0.65.0 的 Missions 需要显式的 `interactionMode: "agi"` 与 `decompSessionType: "orchestrator"`，并且进入 Mission 后该 session 不能再退回普通模式。当前 GUI 没有办法创建、恢复、监控或控制这类 session，也不能展示 `missionDir` 中的 feature 队列、progress log 与 handoff 数据。

本变更要补齐的不是单纯一个页面，而是让 Droi 的 session 模型能正确表达 Mission session，并在 Electron GUI 中稳定驱动 Mission 生命周期。

## What Changes

- 在 SessionConfigPage 中新增独立的 `sessionKind: normal | mission` 选择，不再复用现有 `workspaceMode: local | new-worktree`
- 为 session 引入显式协议字段：`interactionMode`、`autonomyLevel`、`decompSessionType`、`isMission`，Mission session 创建后这些字段必须被锁定并在后续 send/load/save 流程中保留
- 新增 Electron-only 的 `/mission` 页面，在同一 orchestrator session 上切换 Chat / Mission Control 两个视图
- Mission Control 展示 Feature Queue、Mission 状态、Progress Timeline、Handoff 摘要，以及 Pause / Kill Worker 操作
- 通过 Electron main/preload IPC 建立 Mission 磁盘同步：`mission_*` notifications 用于低延迟更新，`~/.factory/missions/<baseSessionId>/` 磁盘文件作为恢复与校正来源
- Mission 运行中禁用 InputBar；当 state 进入 `paused` 或 `orchestrator_turn` 时，用户通过普通 chat 输入继续推进 mission，而不是依赖单独的 Resume RPC
- PermissionCard、Sidebar、Session 恢复流程增加 Mission 感知，并为新增 UI 提供 `data-testid`

## Capabilities

### New Capabilities
- `mission-session`: 显式 Mission session 建模、初始化、保存、恢复，以及“Mission session 不可降级”为普通 session 的约束
- `mission-control-ui`: Mission Control 视图，包含 Feature Queue、状态、Timeline、Handoff 与操作按钮
- `mission-data-sync`: `mission_*` notification 与 missionDir 磁盘状态的双通道同步、合并与崩溃恢复
- `mission-page-routing`: `/mission` 路由、Mission session 导航、Chat/Mission Control 视图切换

### Modified Capabilities
- (none)

## Impact

- **Renderer 层**：调整 new-session 状态模型，引入 `sessionKind` 与 Mission 专用状态；新增 MissionPage 与 Mission 控件；让 Mission 页面复用现有对话外壳能力
- **Backend 层**：扩展 `DroidJsonRpcSession` / manager 的显式协议参数传递与 Mission guard；新增 `missionDir` reader/watcher 与 Mission IPC handlers
- **Shared / Preload 层**：为显式 session 协议字段、Mission 数据类型和 Electron IPC bridge 增加类型定义
- **Scope**：本变更只覆盖 Electron 桌面端；现有 Web/LAN 路径不纳入本次实现
- **依赖**：不新增文件监听依赖，沿用 Node.js 原生 `fs.watch` + poll fallback
