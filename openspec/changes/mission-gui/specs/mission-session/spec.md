## ADDED Requirements

### Requirement: Mission mode toggle in SessionConfigPage
The system SHALL display a mode selector (Normal / Mission) in the SessionConfigPage when creating a new session. The toggle MUST be visually distinct and have `data-testid="session-mode-mission"`.

#### Scenario: User selects Mission mode
- **WHEN** user clicks the Mission mode toggle in SessionConfigPage
- **THEN** the mode selector shows Mission as active
- **AND** the underlying session creation parameters are set to `decompSessionType: "orchestrator"` and `interactionMode: "agi"`

#### Scenario: User selects Normal mode (default)
- **WHEN** user opens SessionConfigPage without changing mode
- **THEN** Normal mode is selected by default
- **AND** session creation uses standard parameters (no `decompSessionType`)

### Requirement: Orchestrator session initialization via stream-jsonrpc
The system SHALL create Mission sessions by passing `decompSessionType: "orchestrator"` and `interactionMode: "agi"` to the `droid.initialize_session` RPC method. The system MUST NOT rely on `/enter-mission` slash command in stream-jsonrpc mode.

#### Scenario: Mission session is created
- **WHEN** user sends the first message in Mission mode
- **THEN** the backend calls `droid.initialize_session` with `decompSessionType: "orchestrator"` and `interactionMode: "agi"`
- **AND** the returned sessionId is stored in the session buffer with `isMission: true`

#### Scenario: Session parameters are passed through DroidExecManager
- **WHEN** a Mission session is created
- **THEN** `DroidExecSendOptions` includes `decompSessionType` and the value is forwarded to `DroidJsonRpcSession.ensureInitialized`

### Requirement: Mission session metadata persistence
The system SHALL persist `isMission: true` in the session metadata (SessionMeta) so that Mission sessions can be identified after app restart.

#### Scenario: Mission session is saved
- **WHEN** a Mission session is saved to disk
- **THEN** the SessionMeta includes `isMission: true`

#### Scenario: Mission session is loaded
- **WHEN** a previously saved Mission session is loaded
- **THEN** the system recognizes it as a Mission session and navigates to `/mission` route

### Requirement: Sidebar Mission session indicator
The system SHALL display a visual indicator (icon or badge) on Mission sessions in the sidebar to distinguish them from normal sessions. The sidebar item MUST have `data-testid="session-mission-{sessionId}"`.

#### Scenario: Mission session appears in sidebar
- **WHEN** a Mission session exists in the project
- **THEN** the sidebar shows it with a distinct Mission indicator icon
- **AND** clicking it navigates to `/mission` route instead of `/`
