## ADDED Requirements

### Requirement: Mission notification processing
The system SHALL process `mission_*` type notifications from `droid.session_notification` and update the mission state in SessionBuffer accordingly.

#### Scenario: mission_state_changed notification
- **WHEN** a notification with `type: "mission_state_changed"` arrives
- **THEN** the store updates the mission state field to the new state value

#### Scenario: mission_features_changed notification
- **WHEN** a notification with `type: "mission_features_changed"` arrives
- **THEN** the store replaces the feature list with the notification's features array

#### Scenario: mission_progress_entry notification
- **WHEN** a notification with `type: "mission_progress_entry"` arrives
- **THEN** the store appends the progress log entries to the mission's progressLog array

#### Scenario: mission_worker_started notification
- **WHEN** a notification with `type: "mission_worker_started"` arrives
- **THEN** the store updates currentWorkerSessionId in the mission state

#### Scenario: mission_worker_completed notification
- **WHEN** a notification with `type: "mission_worker_completed"` arrives
- **THEN** the store clears currentWorkerSessionId and increments completedFeatures

### Requirement: StartMissionRun progress updates may supplement Mission notifications
The system SHALL support `tool_progress_update` payloads emitted for `StartMissionRun` as an auxiliary state source, without replacing the authoritative Mission notifications and missionDir state.

#### Scenario: StartMissionRun emits status details
- **WHEN** a `tool_progress_update` arrives with `toolName: "StartMissionRun"`
- **THEN** the system may parse its status details for `missionState`, progress counters, current feature, and current worker metadata
- **AND** it treats that data as supplemental UI state rather than the sole source of truth

### Requirement: MissionDir path extraction
The system SHALL extract the missionDir path from the `ProposeMission` tool_result notification and store it in the SessionBuffer. The path follows the pattern `~/.factory/missions/<baseSessionId>`.

#### Scenario: missionDir extracted from tool_result
- **WHEN** a tool_result notification for ProposeMission contains `missionDir` in its content
- **THEN** the system parses the missionDir path and stores it in `sessionBuffer.mission.missionDir`

#### Scenario: missionDir fallback via convention
- **WHEN** missionDir was not captured from notification (e.g., app restarted)
- **THEN** the system attempts to find it at `~/.factory/missions/<sessionId>`

### Requirement: MissionDirWatcher disk monitoring is Electron-only
The system SHALL monitor the missionDir on disk via the Electron main process using `fs.watch` with a 2-second setInterval poll fallback. Changes are sent to renderer via IPC. This change SHALL NOT require Web/LAN server routes.

#### Scenario: Initial load from disk
- **WHEN** a Mission session is activated and missionDir exists on disk
- **THEN** the system reads state.json, features.json, progress_log.jsonl, and handoffs/ directory
- **AND** populates the mission state in SessionBuffer

#### Scenario: Disk poll detects changes
- **WHEN** a file in missionDir changes (detected by poll or fs.watch)
- **THEN** the system reads the changed file and sends updated data to renderer via IPC

#### Scenario: MissionDir does not exist yet
- **WHEN** a Mission session is active but missionDir does not exist on disk
- **THEN** the watcher waits without error and begins monitoring once the directory is created

### Requirement: Dual-channel data reconciliation
The system SHALL reconcile data from notification channel and disk channel using file-specific merge rules. Notifications update the store immediately; disk data acts as the recovery and correction source.

#### Scenario: Notification arrives before disk sync
- **WHEN** a mission_state_changed notification arrives
- **THEN** the store updates immediately (sub-second latency)

#### Scenario: state.json is newer than store
- **WHEN** disk poll reads `state.json` with a newer `updatedAt` than the current store value
- **THEN** the mission state in the store is overwritten with the disk state

#### Scenario: features.json changes
- **WHEN** disk poll reads an updated `features.json`
- **THEN** the feature list in the store is replaced with the latest disk snapshot

#### Scenario: validator features are injected after worker completion
- **WHEN** `progress_log.jsonl` appends `milestone_validation_triggered` and `features.json` gains validator features for the same milestone
- **THEN** the store updates total features and feature ordering from disk
- **AND** it does not infer Mission completion solely from the earlier implementation `worker_completed`

#### Scenario: progress_log.jsonl appends entries
- **WHEN** disk poll reads new lines from `progress_log.jsonl`
- **THEN** the store appends only unseen progress events
- **AND** previously known events are not duplicated

#### Scenario: handoff files appear
- **WHEN** new files are added under `handoffs/`
- **THEN** the store merges the newly discovered handoff records without clearing already loaded handoffs

#### Scenario: implementation handoff exists while mission keeps running
- **WHEN** a handoff file for a completed implementation feature is present but Mission state remains `running`
- **THEN** the handoff stays in store
- **AND** later validator-driven feature updates do not clear it

### Requirement: Crash recovery from disk
The system SHALL recover mission state purely from missionDir files when the app restarts.

#### Scenario: App restart with existing mission
- **WHEN** the app starts and a Mission session's missionDir exists on disk
- **THEN** the system reads state.json, features.json, progress_log.jsonl, and handoffs/
- **AND** the Mission Control view renders the recovered state correctly

#### Scenario: Worker orphan indication
- **WHEN** state.json shows `currentWorkerSessionId` but the worker is no longer running (orphan)
- **THEN** the UI shows the mission state as recovered from disk without assuming the worker is still active

### Requirement: `droid.load_session` snapshot participates in recovery
The system SHALL consume Mission snapshot data returned by `droid.load_session` when available, then reconcile it with missionDir state.

#### Scenario: load_session returns paused Mission snapshot
- **WHEN** `droid.load_session` returns a Mission snapshot containing `mission.state`, `mission.features`, and `mission.progressLog`
- **THEN** the renderer uses that snapshot to render the restored Mission immediately
- **AND** missionDir polling/watch continues to reconcile any newer disk state

#### Scenario: load_session and missionDir disagree
- **WHEN** `droid.load_session` returns Mission data that differs from the current missionDir files
- **THEN** the system prefers the more complete or newer view during bootstrap
- **AND** the missionDir watcher becomes the long-lived correction source afterward

### Requirement: Daemon failure is modeled as a supported Mission state transition
The system SHALL handle Mission runs that fail before any worker successfully starts, including daemon / factoryd failures that return control to the orchestrator and ultimately pause the Mission.

#### Scenario: worker fails before a workerSessionId exists
- **WHEN** progress log records `worker_failed` due to daemon or spawn failure and there is no active `workerSessionId`
- **THEN** the system preserves that failure event in Mission state
- **AND** it does not assume `mission_worker_started` must have occurred first

#### Scenario: Mission becomes paused after daemon failure
- **WHEN** the Mission receives `mission_state_changed: orchestrator_turn` followed by `mission_state_changed: paused` after a daemon-related failure
- **THEN** the renderer keeps both the failure context and the final paused state visible to the user

#### Scenario: Mission becomes paused after user kills a worker
- **WHEN** progress log records `worker_failed` with reason `Killed by user` followed by `mission_paused`
- **THEN** the renderer preserves that user-kill reason in Mission state
- **AND** it does not classify the event as daemon failure or generic infrastructure loss

### Requirement: Mission IPC handlers
The system SHALL expose IPC handlers for mission-related operations: `mission:watch-start`, `mission:watch-stop`, `mission:read-dir`, `mission:kill-worker`.

#### Scenario: Start watching missionDir
- **WHEN** renderer sends `mission:watch-start` with a missionDir path
- **THEN** main process starts MissionDirWatcher and sends updates via `mission:dir-changed` IPC events

#### Scenario: Stop watching missionDir
- **WHEN** renderer sends `mission:watch-stop`
- **THEN** main process stops the MissionDirWatcher and releases file handles

#### Scenario: Read missionDir on demand
- **WHEN** renderer sends `mission:read-dir` with a missionDir path
- **THEN** main process reads all mission files and returns parsed state/features/progress/handoffs

#### Scenario: Web path remains unchanged
- **WHEN** this change is implemented
- **THEN** the existing Hono / browser transport does not gain Mission disk-watch routes as part of this scope
