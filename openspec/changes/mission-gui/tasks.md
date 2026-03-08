## 1. Shared Types & Protocol

- [ ] 1.1 Add Mission types to `src/shared/protocol.ts`: MissionState, MissionFeature, MissionProgressEntry, MissionHandoff, MissionData (state/features/progressLog/handoffs/missionDir)
- [ ] 1.2 Add Mission notification types: MissionStateChangedNotification, MissionFeaturesChangedNotification, MissionProgressEntryNotification, MissionWorkerStartedNotification, MissionWorkerCompletedNotification
- [ ] 1.3 Add `isMission` field to SessionMeta interface and SaveSessionRequest
- [ ] 1.4 Add `decompSessionType` and related fields to DroidClientAPI (createSession, exec params)
- [ ] 1.5 Add Mission IPC method types to DroidClientAPI: `missionWatchStart`, `missionWatchStop`, `missionReadDir`, `missionKillWorker`

## 2. Backend: Mission Session Support

- [ ] 2.1 Extend `DroidJsonRpcSession.ensureInitialized` to accept and pass `decompSessionType` to `droid.initialize_session` params
- [ ] 2.2 Extend `DroidExecManager` and `DroidJsonRpcManager` interfaces to support `decompSessionType` in send/createSession options
- [ ] 2.3 Add `killWorkerSession` method to `DroidJsonRpcSession` (sends `droid.kill_worker_session` RPC request)
- [ ] 2.4 Expose `killWorkerSession` through `DroidJsonRpcManager` and `DroidExecManager`

## 3. Backend: MissionDirWatcher

- [ ] 3.1 Create `src/backend/mission/missionTypes.ts` with parsed disk types (MissionDiskState, MissionDiskFeatures, MissionDiskProgressLog, MissionDiskHandoff)
- [ ] 3.2 Create `src/backend/mission/missionDirReader.ts` with functions to read and parse state.json, features.json, progress_log.jsonl, handoffs/ directory
- [ ] 3.3 Create `src/backend/mission/missionDirWatcher.ts` using fs.watch + 2s setInterval poll; emits events when files change; reads and parses changed files
- [ ] 3.4 Handle missionDir not existing yet (watch for directory creation, then start file monitoring)

## 4. Backend: Mission IPC Handlers

- [ ] 4.1 Register `mission:watch-start` IPC handler in `src/main/ipc/registerHandlers.ts` that creates and starts MissionDirWatcher
- [ ] 4.2 Register `mission:watch-stop` IPC handler that stops and disposes MissionDirWatcher
- [ ] 4.3 Register `mission:read-dir` IPC handler that reads missionDir on demand and returns parsed data
- [ ] 4.4 Register `mission:kill-worker` IPC handler that calls DroidExecManager.killWorkerSession
- [ ] 4.5 Emit `mission:dir-changed` events from main to renderer when MissionDirWatcher detects changes

## 5. Preload: IPC Bridge

- [ ] 5.1 Add Mission IPC methods to preload `src/preload/index.ts`: missionWatchStart, missionWatchStop, missionReadDir, missionKillWorker
- [ ] 5.2 Add `onMissionDirChanged` listener bridge for `mission:dir-changed` events
- [ ] 5.3 Update `src/preload/index.d.ts` type declarations

## 6. State Management: Mission Reducer

- [ ] 6.1 Add `mission` field to `SessionBuffer` in `src/renderer/src/state/appReducer.ts` (type: MissionData | null)
- [ ] 6.2 Implement `applyMissionNotification` function in appReducer to handle mission_state_changed, mission_features_changed, mission_progress_entry, mission_worker_started, mission_worker_completed notifications
- [ ] 6.3 Implement missionDir path extraction from ProposeMission tool_result notification (parse content for missionDir field)
- [ ] 6.4 Integrate `applyMissionNotification` into the existing notification handling pipeline (handleRpcNotification)
- [ ] 6.5 Implement `applyMissionDiskData` function for reconciling disk data with store (compare updatedAt timestamps)

## 7. State Management: Mission Hooks & Store

- [ ] 7.1 Create `src/renderer/src/hooks/useMission.ts` with hooks: useMissionState, useMissionFeatures, useMissionProgressLog, useMissionHandoffs, useMissionDir, useIsMissionSession
- [ ] 7.2 Add mission-related selectors to Zustand store exports in `src/renderer/src/store.tsx`
- [ ] 7.3 Add store actions: handleMissionPause, handleMissionKillWorker, handleMissionDirChanged
- [ ] 7.4 Wire up AppInitializer to start MissionDirWatcher when a Mission session becomes active (call missionWatchStart) and stop when session changes

## 8. Routing

- [ ] 8.1 Add `/mission` route to `src/renderer/src/router.tsx` with MissionPage component
- [ ] 8.2 Add route to chatLayoutRoute children (sharing RootLayout with sidebar)
- [ ] 8.3 Update sidebar session click handler to navigate to `/mission` for Mission sessions and `/` for normal sessions

## 9. UI: SessionConfigPage Mission Toggle

- [ ] 9.1 Add Mission mode toggle to `src/renderer/src/components/SessionConfigPage.tsx` with `data-testid="session-mode-mission"`
- [ ] 9.2 Pass `isMission` flag through new session creation flow (pendingNewSession → createSession → DroidExecManager)
- [ ] 9.3 Navigate to `/mission` after Mission session creation

## 10. UI: MissionPage

- [ ] 10.1 Create `src/renderer/src/pages/MissionPage.tsx` with Chat/MissionControl view toggle (`data-testid="mission-view-toggle"`)
- [ ] 10.2 Implement Chat view: render ChatView + InputBar (reuse from ChatPage), disable InputBar when mission state is `running`
- [ ] 10.3 Implement MissionControl view placeholder (renders MissionControlPanel)
- [ ] 10.4 Implement auto-switch logic: running → MissionControl, orchestrator_turn/paused → Chat, with 30s manual-switch cooldown
- [ ] 10.5 Add MissionStatusBar at bottom (always visible in both views, `data-testid="mission-statusbar"`)

## 11. UI: MissionControlPanel

- [ ] 11.1 Create `src/renderer/src/components/mission/MissionControlPanel.tsx` as container for all Mission Control sub-components
- [ ] 11.2 Create `src/renderer/src/components/mission/FeatureQueue.tsx` with `data-testid="mission-feature-queue"`, render features with status icons and `data-testid="mission-feature-{featureId}"`
- [ ] 11.3 Create `src/renderer/src/components/mission/MissionStatusIndicator.tsx` with `data-testid="mission-status"`, show state badge + completed/total counter
- [ ] 11.4 Create `src/renderer/src/components/mission/ProgressTimeline.tsx` with `data-testid="mission-progress-timeline"`, render progress_log entries chronologically
- [ ] 11.5 Create `src/renderer/src/components/mission/HandoffCard.tsx` with `data-testid="mission-handoff-{featureId}"`, show salientSummary, successState, whatWasImplemented
- [ ] 11.6 Create `src/renderer/src/components/mission/MissionActions.tsx` with Pause (`data-testid="mission-pause"`) and Kill Worker (`data-testid="mission-kill-worker"`) buttons

## 12. UI: PermissionCard Enhancements

- [ ] 12.1 Update `src/renderer/src/components/PermissionCard.tsx` to detect `propose_mission` confirmationType and render mission-specific accept/cancel UI
- [ ] 12.2 Update PermissionCard to detect `start_mission_run` confirmationType and render "Start Mission Run" / "Cancel" options

## 13. UI: Sidebar Mission Indicator

- [ ] 13.1 Update `src/renderer/src/components/app-sidebar.tsx` to show Mission icon/badge on sessions with `isMission: true`
- [ ] 13.2 Add `data-testid="session-mission-{sessionId}"` to Mission session items
- [ ] 13.3 Update session click handler to route to `/mission` for Mission sessions

## 14. Session Persistence

- [ ] 14.1 Update `saveSession` to persist `isMission` flag in SessionMeta
- [ ] 14.2 Update `loadSession` to restore `isMission` flag and trigger missionDir recovery
- [ ] 14.3 On app start, for Mission sessions with existing missionDir, read disk state and populate mission data

## 15. Integration & Testing

- [ ] 15.1 Verify full Mission flow: create session → propose → accept → start run → watch progress → pause → resume → complete
- [ ] 15.2 Verify crash recovery: close app during running mission → reopen → mission state restored from disk
- [ ] 15.3 Verify all data-testid attributes are present for E2E testing
- [ ] 15.4 Run existing lint and typecheck (`pnpm lint`, `pnpm typecheck`) and fix any issues
- [ ] 15.5 Run existing tests and fix any regressions
