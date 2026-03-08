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

#### Scenario: progress_log.jsonl appends entries
- **WHEN** disk poll reads new lines from `progress_log.jsonl`
- **THEN** the store appends only unseen progress events
- **AND** previously known events are not duplicated

#### Scenario: handoff files appear
- **WHEN** new files are added under `handoffs/`
- **THEN** the store merges the newly discovered handoff records without clearing already loaded handoffs

### Requirement: Crash recovery from disk
The system SHALL recover mission state purely from missionDir files when the app restarts.

#### Scenario: App restart with existing mission
- **WHEN** the app starts and a Mission session's missionDir exists on disk
- **THEN** the system reads state.json, features.json, progress_log.jsonl, and handoffs/
- **AND** the Mission Control view renders the recovered state correctly

#### Scenario: Worker orphan indication
- **WHEN** state.json shows `currentWorkerSessionId` but the worker is no longer running (orphan)
- **THEN** the UI shows the mission state as recovered from disk without assuming the worker is still active

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
