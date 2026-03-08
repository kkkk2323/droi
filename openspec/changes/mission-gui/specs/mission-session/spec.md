## ADDED Requirements

### Requirement: Session kind is separate from workspace mode
The system SHALL model Mission selection independently from workspace preparation mode. `SessionConfigPage` MUST keep the existing workspace mode choices (`local` / `new-worktree`) and add a separate session-kind selector (`normal` / `mission`). The Mission selector MUST have `data-testid="session-mode-mission"`.

#### Scenario: User selects Mission session kind
- **WHEN** user selects Mission in SessionConfigPage
- **THEN** the UI marks Mission as the active session kind
- **AND** the selected workspace mode remains independently configurable

#### Scenario: User selects workspace mode after Mission
- **WHEN** user switches between `local` and `new-worktree` after choosing Mission
- **THEN** the Mission selection remains active
- **AND** the system does not treat workspace mode changes as a Mission/Normal toggle

### Requirement: Mission sessions use explicit orchestrator settings
The system SHALL create Mission sessions by passing explicit session protocol settings to `droid.initialize_session`: `decompSessionType: "orchestrator"` and `interactionMode: "agi"`. The system MUST NOT rely on `/enter-mission` slash command in stream-jsonrpc mode.

#### Scenario: Mission session is created
- **WHEN** user sends the first message in Mission mode
- **THEN** the backend calls `droid.initialize_session` with `decompSessionType: "orchestrator"` and `interactionMode: "agi"`
- **AND** the returned session is stored with `isMission: true` and `sessionKind: "mission"`

#### Scenario: Session parameters are passed through DroidExecManager
- **WHEN** a Mission session is created
- **THEN** the send/create options include `interactionMode`, `autonomyLevel`, and `decompSessionType`
- **AND** those values are forwarded to `DroidJsonRpcSession.ensureInitialized` without being re-derived only from `autoLevel`

### Requirement: Mission sessions cannot be downgraded
The system SHALL preserve Mission session protocol settings across later sends, loads, and settings updates. A Mission session MUST NOT be converted back to a normal `spec/auto` session by generic update logic.

#### Scenario: User sends another message in an existing Mission session
- **WHEN** a Mission session already exists and the user sends another message
- **THEN** the system reuses the stored Mission protocol settings for that session
- **AND** the send path does not rewrite the session to `interactionMode: "spec"` or `interactionMode: "auto"`

#### Scenario: Generic session settings update targets a Mission session
- **WHEN** renderer or backend performs a generic session settings update for a Mission session
- **THEN** the Mission-specific `interactionMode` and `decompSessionType` remain unchanged
- **AND** unsupported downgrade attempts are ignored or rejected

### Requirement: Mission session metadata persistence
The system SHALL persist enough metadata to restore a Mission session after app restart, including `isMission: true` and the explicit Mission protocol settings.

#### Scenario: Mission session is saved
- **WHEN** a Mission session is saved to disk
- **THEN** the saved record includes `isMission: true`
- **AND** it preserves the Mission session's `interactionMode` and `decompSessionType`

#### Scenario: Mission session is loaded
- **WHEN** a previously saved Mission session is loaded
- **THEN** the system recognizes it as a Mission session
- **AND** it restores the Mission protocol settings without re-inferring them from `autoLevel`
- **AND** it navigates to `/mission`

### Requirement: Mission restore can consume `droid.load_session` snapshot
The system SHALL support Mission recovery using the `droid.load_session` response in addition to local metadata and missionDir state.

#### Scenario: load_session returns Mission snapshot
- **WHEN** renderer or backend loads an existing Mission session via `droid.load_session`
- **THEN** the system reads the returned `decompSessionType`, current settings, and `mission` snapshot if present
- **AND** it uses that snapshot to bootstrap the restored session before missionDir reconciliation completes

#### Scenario: load_session remains Mission-typed across runtime stages
- **WHEN** renderer or backend calls `droid.load_session` while a Mission is `running`, `paused`, in validator-injected `running`, or `completed`
- **THEN** the response continues to identify the session as `decompSessionType: "orchestrator"`
- **AND** the returned settings keep the Mission protocol shape (`interactionMode: "agi"`, Mission autonomy settings)
- **AND** the response includes a `mission` snapshot suitable for UI bootstrap in every stage

#### Scenario: Transient settings updates arrive during Mission init
- **WHEN** initialization emits early `settings_updated` notifications before the final Mission settings settle
- **THEN** the system does not classify the session as non-Mission based on those transient values alone
- **AND** it preserves the explicit Mission session metadata as the source of truth

#### Scenario: settings_updated conflicts with load_session during Mission bootstrap
- **WHEN** early `settings_updated` notifications temporarily report non-Mission values such as `interactionMode: "spec"` or `autonomyLevel: "off"`
- **AND** `droid.load_session` still reports Mission settings and `decompSessionType: "orchestrator"`
- **THEN** the system treats `load_session` + persisted Mission metadata as authoritative
- **AND** it does not downgrade the session classification because of the transient settings notification

### Requirement: Sidebar Mission session indicator
The system SHALL display a visual indicator (icon or badge) on Mission sessions in the sidebar to distinguish them from normal sessions. The sidebar item MUST have `data-testid="session-mission-{sessionId}"`.

#### Scenario: Mission session appears in sidebar
- **WHEN** a Mission session exists in the project
- **THEN** the sidebar shows it with a distinct Mission indicator icon
- **AND** clicking it navigates to `/mission` route instead of `/`
