## ADDED Requirements

### Requirement: Feature Queue panel
The system SHALL display a Feature Queue panel showing all features from `features.json` with their current status. The panel MUST have `data-testid="mission-feature-queue"`.

#### Scenario: Features are displayed with correct status
- **WHEN** MissionControlPanel is visible and features.json contains features
- **THEN** each feature is rendered with its id, description, and status (pending/in_progress/completed/cancelled)
- **AND** the current in_progress feature is visually highlighted
- **AND** each feature item has `data-testid="mission-feature-{featureId}"`

#### Scenario: Feature order reflects move-to-bottom
- **WHEN** a feature completes and is moved to the bottom of features.json
- **THEN** the Feature Queue UI updates to reflect the new order within 2 seconds

#### Scenario: Validator features are visually distinct
- **WHEN** features with skillName `scrutiny-validator` or `user-testing-validator` appear in the queue
- **THEN** they are rendered with a distinct visual style (different icon/badge) indicating they are validation features

### Requirement: Mission status indicator
The system SHALL display the current Mission state (initializing/running/paused/orchestrator_turn/completed/awaiting_input) with a status badge. The indicator MUST have `data-testid="mission-status"`.

#### Scenario: State changes are reflected in real-time
- **WHEN** a `mission_state_changed` notification arrives
- **THEN** the status indicator updates to show the new state within 500ms

#### Scenario: Progress counter
- **WHEN** Mission is in any active state
- **THEN** the status indicator shows completed/total feature count (e.g., "2/6 completed")

### Requirement: Progress Timeline
The system SHALL display a timeline of progress_log events (mission_run_started, worker_started, worker_completed, milestone_validation_triggered, etc.). The timeline MUST have `data-testid="mission-progress-timeline"`.

#### Scenario: Events are displayed chronologically
- **WHEN** progress_log contains entries
- **THEN** events are displayed in chronological order with timestamps and event type labels

#### Scenario: New events appear in real-time
- **WHEN** a `mission_progress_entry` notification arrives
- **THEN** the new event is appended to the timeline within 500ms

### Requirement: Handoff card display
The system SHALL display a summary card for each completed worker's handoff. The card shows salientSummary, successState, whatWasImplemented, and verification results.

#### Scenario: Handoff appears after worker completion
- **WHEN** a worker completes (worker_completed event)
- **THEN** a Handoff card appears showing the handoff summary
- **AND** the card has `data-testid="mission-handoff-{featureId}"`

#### Scenario: Handoff data sourced from disk
- **WHEN** GUI restarts and missionDir contains handoff files
- **THEN** the system reads `handoffs/*.json` and renders Handoff cards for all completed features

### Requirement: Pause action
The system SHALL provide a Pause button that calls `droid.interrupt_session` on the orchestrator session. The button MUST have `data-testid="mission-pause"`.

#### Scenario: User pauses a running mission
- **WHEN** mission state is `running` and user clicks Pause
- **THEN** the system calls `droid.interrupt_session`
- **AND** mission state transitions to `paused`

#### Scenario: Pause button visibility
- **WHEN** mission state is `running`
- **THEN** Pause button is visible and enabled
- **WHEN** mission state is NOT `running`
- **THEN** Pause button is hidden or disabled

### Requirement: Kill Worker action
The system SHALL provide a Kill Worker button that sends `droid.kill_worker_session` for the current worker. The button MUST have `data-testid="mission-kill-worker"`.

#### Scenario: User kills current worker
- **WHEN** a worker is actively running and user clicks Kill Worker
- **THEN** the system sends the kill command for the current worker session
- **AND** the UI shows a brief confirmation

#### Scenario: Kill Worker button visibility
- **WHEN** mission state is `running` and a worker session is active
- **THEN** Kill Worker button is visible
- **WHEN** no worker is active
- **THEN** Kill Worker button is hidden

### Requirement: Mission PermissionCard enhancements
The system SHALL render specialized permission cards for `propose_mission` and `start_mission_run` confirmation types with descriptive labels.

#### Scenario: ProposeMission permission
- **WHEN** a `droid.request_permission` arrives with `confirmationType: "propose_mission"`
- **THEN** the PermissionCard shows "Accept Mission Proposal" / "Cancel" options with mission-specific styling

#### Scenario: StartMissionRun permission
- **WHEN** a `droid.request_permission` arrives with `confirmationType: "start_mission_run"`
- **THEN** the PermissionCard shows "Start Mission Run" / "Cancel" options

### Requirement: Mission page reuses existing conversation shell
The system SHALL keep Mission chat interactions consistent with the existing chat experience by preserving permission prompts, ask-user prompts, todo visibility, and debug trace behavior on MissionPage.

#### Scenario: Mission session requires permission
- **WHEN** a Mission session has a pending permission request
- **THEN** MissionPage renders the PermissionCard flow instead of bypassing it

#### Scenario: Mission session asks the user a question
- **WHEN** a Mission session has a pending ask-user request
- **THEN** MissionPage renders the ask-user UI using the same interaction pattern as normal chat sessions

### Requirement: InputBar mission-aware behavior
The system SHALL disable the InputBar when mission state is `running` and display a contextual message.

#### Scenario: InputBar disabled during running
- **WHEN** mission state is `running` and user is on Chat view
- **THEN** InputBar is disabled with placeholder "Mission is running. Pause to send a message."
- **AND** a Pause button is shown inline or adjacent

#### Scenario: InputBar enabled when paused
- **WHEN** mission state is `paused` or `orchestrator_turn`
- **THEN** InputBar is enabled and user can type and send messages

### Requirement: Continue semantics use normal chat input
The system SHALL let the user continue a paused Mission by sending a normal chat message once the Mission returns to `paused` or `orchestrator_turn`. The system SHALL NOT require a dedicated Resume RPC control in this change.

#### Scenario: User continues a paused Mission
- **WHEN** mission state is `paused` and user sends a chat message such as `continue`
- **THEN** the message is sent through the existing orchestrator session
- **AND** the system allows the orchestrator to decide whether to call `start_mission_run` again

### Requirement: Mission status bar (always visible)
The system SHALL display a compact status bar at the bottom of MissionPage showing mission state, current feature, and worker info. The bar MUST be visible in both Chat and MissionControl views. It MUST have `data-testid="mission-statusbar"`.

#### Scenario: Status bar content
- **WHEN** mission is active
- **THEN** status bar shows: mission state, feature progress (e.g., "2/6"), current worker session id (truncated)
