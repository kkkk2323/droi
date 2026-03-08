## ADDED Requirements

### Requirement: Mission page route
The system SHALL register a `/mission` route that renders MissionPage. The route MUST be accessible via TanStack Router navigation.

#### Scenario: Direct navigation to mission route
- **WHEN** the router navigates to `/mission`
- **THEN** the MissionPage component is rendered within the RootLayout

#### Scenario: Non-mission session on mission route
- **WHEN** the active session is not a Mission session and the route is `/mission`
- **THEN** the system redirects to `/` (ChatPage)

#### Scenario: Mission session on default route
- **WHEN** the active session is a Mission session and the user is routed via session selection
- **THEN** the app navigates to `/mission` instead of leaving the user on `/`

### Requirement: Chat/MissionControl view toggle
MissionPage SHALL provide a toggle to switch between Chat view and Mission Control view. The toggle MUST have `data-testid="mission-view-toggle"`. Both views share the same orchestrator session.

#### Scenario: Toggle from Chat to MissionControl
- **WHEN** user clicks the MissionControl tab/toggle
- **THEN** the view switches from Chat (ChatView + InputBar) to MissionControlPanel
- **AND** the toggle visually indicates MissionControl is active

#### Scenario: Toggle from MissionControl to Chat
- **WHEN** user clicks the Chat tab/toggle
- **THEN** the view switches from MissionControlPanel to Chat (ChatView + InputBar)

### Requirement: Auto-switch based on mission state
The system SHALL automatically switch views based on mission state changes, with a 30-second cooldown after manual user switch.

#### Scenario: Auto-switch to MissionControl on running
- **WHEN** mission state changes to `running`
- **AND** user has not manually switched views in the last 30 seconds
- **THEN** the view automatically switches to MissionControl

#### Scenario: Auto-switch to Chat on orchestrator_turn
- **WHEN** mission state changes to `orchestrator_turn` or `paused`
- **AND** user has not manually switched views in the last 30 seconds
- **THEN** the view automatically switches to Chat

#### Scenario: Manual switch cooldown
- **WHEN** user manually switches views
- **THEN** auto-switch is suppressed for 30 seconds
- **AND** after 30 seconds, auto-switch resumes

### Requirement: Sidebar navigation to MissionPage
The system SHALL navigate to `/mission` when a Mission session is clicked in the sidebar, and to `/` when a normal session is clicked.

#### Scenario: Click Mission session in sidebar
- **WHEN** user clicks a session with `isMission: true` in the sidebar
- **THEN** the router navigates to `/mission`

#### Scenario: Click normal session in sidebar
- **WHEN** user clicks a session without `isMission` flag in the sidebar
- **THEN** the router navigates to `/`

#### Scenario: Click project with latest Mission session
- **WHEN** a project-level navigation path auto-selects a latest session and that session is a Mission session
- **THEN** the router navigates to `/mission`

### Requirement: Mission session creation navigates to MissionPage
The system SHALL navigate to `/mission` after a Mission session is successfully created from SessionConfigPage.

#### Scenario: Mission session created
- **WHEN** user selects Mission session kind in SessionConfigPage and sends the first message
- **AND** the session is successfully created
- **THEN** the router navigates to `/mission`

### Requirement: Mission route preserves existing conversation shell behavior
MissionPage SHALL preserve the existing conversation-shell behavior while switching between Chat and Mission Control views.

#### Scenario: Mission page shows chat-specific UI when Chat view is active
- **WHEN** MissionPage is in Chat view
- **THEN** the user sees the same conversation shell behavior used by ChatPage for that session
- **AND** switching to Mission Control does not create a separate session
