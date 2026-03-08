## 1. Shared Session Model & Protocol

- [ ] 1.1 Add Mission types to `src/shared/protocol.ts`: MissionState, MissionFeature, MissionProgressEntry, MissionHandoff, MissionData
- [ ] 1.2 Add explicit session protocol fields to shared types and client API: `isMission`, `sessionKind`, `interactionMode`, `autonomyLevel`, `decompSessionType`
- [ ] 1.3 Extend `SessionMeta`, `SaveSessionRequest`, and `LoadSessionResponse` so Mission sessions can be saved and restored without relying on `autoLevel` inference
- [ ] 1.4 Add typed Mission IPC methods to `DroidClientAPI`: `missionWatchStart`, `missionWatchStop`, `missionReadDir`, `missionKillWorker`, `onMissionDirChanged`

## 2. Backend: Mission Session Plumbing

- [ ] 2.1 Extend `DroidJsonRpcSession.ensureInitialized` to accept and pass explicit `interactionMode` and `decompSessionType`
- [ ] 2.2 Extend `DroidExecManager` / `DroidJsonRpcManager` create-send-update paths to carry explicit Mission session settings instead of deriving them only from `autoLevel`
- [ ] 2.3 Add a Mission guard to `updateSessionSettings` so Mission sessions cannot be downgraded back to normal `spec/auto` settings after creation
- [ ] 2.4 Add `killWorkerSession` support in `DroidJsonRpcSession`, `DroidJsonRpcManager`, and `DroidExecManager`, passing the live `workerSessionId`

## 3. Backend: Electron-only Mission Disk Sync

- [ ] 3.1 Create `src/backend/mission/missionTypes.ts` for parsed disk payloads (`state.json`, `features.json`, `progress_log.jsonl`, `handoffs/`)
- [ ] 3.2 Create `src/backend/mission/missionDirReader.ts` to read and parse missionDir snapshots
- [ ] 3.3 Create `src/backend/mission/missionDirWatcher.ts` using `fs.watch` with a 2s poll fallback
- [ ] 3.4 Support missionDir appearing late by waiting for directory creation before starting file-level monitoring

## 4. Main / Preload IPC

- [ ] 4.1 Register Electron IPC handlers for `mission:watch-start`, `mission:watch-stop`, `mission:read-dir`, and `mission:kill-worker`
- [ ] 4.2 Emit `mission:dir-changed` events from main to renderer with parsed mission disk updates
- [ ] 4.3 Add preload bridge methods and type declarations for all Mission IPC handlers
- [ ] 4.4 Keep the existing Hono / Web client path unchanged for this change

## 5. Renderer State & Persistence

- [ ] 5.1 Add `sessionKind` to the new-session state model without overloading existing `PendingNewSessionMode`
- [ ] 5.2 Extend `SessionBuffer` with explicit session protocol settings plus `mission` state data
- [ ] 5.3 Implement `applyMissionNotification` in `appReducer.ts` for `mission_*` notifications and Mission-specific permission metadata
- [ ] 5.4 Consume `droid.load_session` mission snapshot during restore before watcher-based reconciliation begins
- [ ] 5.5 Implement `applyMissionDiskData` with per-file reconciliation rules: `state.json` by `updatedAt`, `features.json` by snapshot replacement, `progress_log.jsonl` by append/dedupe, `handoffs/` by file merge
- [ ] 5.6 Parse `tool_progress_update` for `StartMissionRun` as an auxiliary real-time state source, but do not make it authoritative over `load_session` or missionDir
- [ ] 5.7 Keep Mission state authoritative when `milestone_validation_triggered` appends validator features after an implementation worker handoff
- [ ] 5.8 Persist and restore Mission session metadata, including Mission protocol settings and missionDir recovery on app start
- [ ] 5.9 Ignore transient generic `settings_updated` values when they conflict with persisted Mission metadata or `droid.load_session`

## 6. Routing, Navigation, and Session Creation UX

- [ ] 6.1 Add `/mission` route to `src/renderer/src/router.tsx` under the existing chat layout
- [ ] 6.2 Redirect non-Mission sessions away from `/mission`
- [ ] 6.3 Update `SessionConfigPage` to show a separate Mission toggle (`data-testid="session-mode-mission"`) while preserving local/new-worktree selection
- [ ] 6.4 Create Mission sessions with `sessionKind: mission`, navigate to `/mission`, and keep normal sessions on `/`
- [ ] 6.5 Update sidebar and project-selection flows so Mission sessions route to `/mission` consistently

## 7. Mission Page & Controls

- [ ] 7.1 Create `src/renderer/src/pages/MissionPage.tsx` with Chat / Mission Control toggle (`data-testid="mission-view-toggle"`)
- [ ] 7.2 Reuse the existing conversation shell behaviors on MissionPage: chat transcript, permission UI, ask-user UI, todo panel, debug panel, and InputBar handling
- [ ] 7.3 Disable InputBar while Mission state is `running`, show the Mission-specific placeholder, and surface Pause inline or adjacent
- [ ] 7.4 Add auto-switch logic: `running -> mission-control`, `paused/orchestrator_turn -> chat`, with a 30s manual override cooldown
- [ ] 7.5 Add a bottom Mission status bar (`data-testid="mission-statusbar"`) that remains visible in both views
- [ ] 7.6 Surface daemon/factoryd failure `systemMessage` and explain whether the Mission paused automatically or needs a manual retry / app restart
- [ ] 7.7 Keep completed worker handoffs visible while validator features are injected and Mission remains `running`

## 8. Mission Control UI

- [ ] 8.1 Create `MissionControlPanel` and subcomponents for Feature Queue, Mission Status, Progress Timeline, Handoff Cards, and Mission Actions
- [ ] 8.2 Render validator features distinctly and keep feature order aligned with `features.json`
- [ ] 8.3 Use the already-installed virtualization approach where needed for large progress logs
- [ ] 8.4 Add `data-testid` hooks for all Mission UI elements needed by E2E coverage

## 9. Permission & Continue Semantics

- [ ] 9.1 Update `PermissionCard` to render Mission-specific labels for `propose_mission` and `start_mission_run`
- [ ] 9.2 Preserve enough permission metadata in renderer state so Mission-specific UI does not have to parse raw request payloads repeatedly
- [ ] 9.3 Treat `start_mission_run` permission as optional / conditional instead of a guaranteed pre-run step
- [ ] 9.4 Treat Mission “resume” as normal chat continuation when state is `paused` or `orchestrator_turn`; do not add a separate Resume RPC button in this change
- [ ] 9.5 Distinguish user-initiated Pause from daemon-failure-driven `paused` state in UI copy and control availability
- [ ] 9.6 Distinguish `kill_worker_session` (`worker_failed` with reason like `Killed by user`) from daemon failures in timeline and status copy

## 10. Testing & Validation

- [ ] 10.0 Run Mission GUI manual/E2E validation only inside the pre-created Droi project named `Mission-GUI-TEST`, avoiding OS-level project-picker dialogs during Mission test flows
- [ ] 10.1 Extend `test/interactionModeHotSwitch.test.ts` to verify Mission sessions retain `agi/orchestrator` settings across subsequent sends and updates
- [ ] 10.2 Extend `test/sessionStore.test.ts` to verify Mission session persistence and restore
- [ ] 10.3 Extend `test/rpcNotificationMapping.test.ts` for Mission notification mapping, `tool_progress_update`, and reconciliation behavior
- [ ] 10.4 Add focused tests for `missionDirReader` / `missionDirWatcher`
- [ ] 10.5 Add restore coverage for `droid.load_session` returning a paused Mission snapshot
- [ ] 10.6 Add restore coverage for `droid.load_session` returning stable Mission settings across `running`, validator-injected `running`, `paused`, and `completed`
- [ ] 10.7 Add coverage that transient generic `settings_updated` values do not downgrade Mission session classification during bootstrap
- [ ] 10.8 Add focused watcher tests for late `missionDir` creation, late `handoffs/` creation, and pause-only updates touching `state.json` + `progress_log.jsonl`
- [ ] 10.9 Add Electron-surface integration coverage for create → propose → accept → run → worker_completed → validator injection → validation completion
- [ ] 10.10 Add Electron-surface integration coverage for create → propose → accept → run → pause/daemon-failure → continue → complete
- [ ] 10.11 Add coverage for `kill_worker_session` producing `worker_failed(reason = "Killed by user") -> mission_paused`
- [ ] 10.12 Run `pnpm lint`, `pnpm typecheck`, and `pnpm test`
