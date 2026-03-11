## 0.22.0 - 2026-03-11

### Features
- Implement Mission mode support with session management and UI enhancements
- Add Mission GUI (Mission Control, Mission Page, Worker List Panel)
- Integrate OpenSpec prompts and skills for change management (propose, explore, apply, archive)
- Enhance InputBar with readonly model support and mission settings
- Implement mission model settings management and UI integration
- Add support for paused worker sessions and runtime logging
- Surface mission control queue, timeline, and handoffs
- Add mission-specific backend services (watcher, reader, ipc bridge)
- Standardize Mission GUI testing with validation harnesses and proof recordings

### Refactor
- Streamline follow-up handling and mission state inference
- Refactor feature selection and handoff display in Mission Control Panel
- Enhance mission state handling with completion checks and notification updates
- Remove restartSessionWithActiveKey and related legacy logic

### Fixes
- Preserver mission permission option semantics
- Stabilize recovered handoff keys
- Preserve base mission dir fallback across restarts
- Await mission watcher teardown during cleanup

## 0.21.0 - 2026-03-08

### Features
- Add remote debugging support and enhance UI testability with data-testid attributes
- Integrate dropdown menu for file changes display in FilesChangedBadge
- Standardise UI components with design system buttons and refined styling
- Enhance permission options display with primary and advanced options
- Streamline project addition UI and improve messaging for no projects in AppSidebar
- Add design system skills (critique, distill, normalize, teach-impeccable)
- Add .factory mission infrastructure and validation skills

### Refactor
- Extract session components and improve sidebar modularity
- Remove disabled state and related styles for commit button in GitActionsButton
- Extract EditorIcon component for cleaner code in OpenInEditorButton

### Style
- Refine active scale transitions for buttons in InputBar and PermissionCard
- Update text-muted-foreground styles and hover effects for various components
- Adjust max-width for branch display in WorktreeIndicator
- Enhance sidebar padding transitions and layout responsiveness

## 0.20.0 - 2026-03-06

### Features
- Enhance workspace management and persistence

## 0.19.1 - 2026-03-06

### Features
- Add support for GPT-5.4 model

## 0.19.0 - 2026-03-04

### Features
- Use configured base branch as default for PR in CommitWizard

### Refactor
- Refine UI interactions and simplify SessionConfigPage

## 0.18.1 - 2026-03-02

### Features
- Add reasoning effort support for commit message generation

## 0.18.0 - 2026-03-02

### Features
- Update model reasoning levels for Claude Sonnet and Gemini 3.1

## 0.17.0 - 2026-02-28

### Features
- Use custom audio asset for attention beep

### Fixes
- Make SelectTrigger full width

## 0.16.0 - 2026-02-27

### Features
- Show AlertCircle icon and play beep for sessions needing user attention

### Fixes
- Ensure chat footer content is fully visible after appearing
- Pin New Project button above scrollable sidebar project list
- Adjust padding for new project sidebar slot

## 0.15.2 - 2026-02-26

### Fixes
- Conditionally render session indicators based on pending new session state

### Style
- Format code for improved readability and consistency across multiple files

## 0.15.1 - 2026-02-25

### Fixes
- Refine chat UI and assistant message duration tracking
- Improve project rename dialog behavior and state management

## 0.15.0 - 2026-02-25

### Features
- Implement project display names and renaming functionality
- Implement granular session working states and duration indicator
- Implement spec review interaction and session management
- Migrate editor icons from emojis to React components

### Fixes
- Improve session sidebar and navigation reliability

### Chore
- Cleanup outdated documentation
- Exclude local configuration files in .gitignore

## 0.14.0 - 2026-02-24

### Features
- Implement update notification system

### Fixes
- Update interactionMode without restarting session

## 0.13.1 - 2026-02-24

### Fixes
- Hot-switch interactionMode when switching to spec

### CI
- Split ASAR artifact job

## 0.13.0 - 2026-02-24

### Features
- Implement in-app ASAR updater and settings UI

### CI
- Update release workflow to support ASAR hot updates

## 0.12.1 - 2026-02-24

### Fixes
- Improve chat scroll stability and session switching

## 0.12.0 - 2026-02-24

### Features
- Implement usage-based spillover for API key selection

## 0.11.0 - 2026-02-24

### Features
- Add interaction mode support across the Droid session lifecycle
- Improve droid PATH resolution and Task tool UI summaries

## 0.10.0 - 2026-02-23

### Features
- Integrate oxlint and oxfmt for linting and formatting
- Update linting rules and clean up unused imports across components

### Refactor
- Improve code readability and maintainability

## 0.9.0 - 2026-02-23

### Features
- Implement session pinning and smooth sidebar scrolling

### Fixes
- Display last message time in session sidebar items

## 0.8.1 - 2026-02-23

### Fixes
- Auto-scroll chat to bottom when new messages arrive

### Refactor
- Simplify GitActionsButton and remove inline push logic

## 0.8.0 - 2026-02-23

### Features
- Add framer-motion and geist font support with UI enhancements

### Fixes
- Add artifactName to electron-builder config so x64 DMG is correctly named (by @copilot-swe-agent)

### Style
- Use theme-aware color tokens for consistency
- Refine component colors using theme tokens for consistency

## 0.7.1 - 2026-02-23

### Refactor
- Refine UI indicators and optimize sidebar rendering
- Extract AppInitializer and project helpers from store

### CI
- Parallelize ARM64 and x64 DMG builds across separate runners

## 0.7.0 - 2026-02-22

### Features
- Automatically pull branch after switching workspace

## 0.6.1 - 2026-02-22

### Fixes
- Ensure new branches do not automatically track upstream
- Enhance scanRoot to correctly handle symlinks and improve directory entry handling
- Fix gh-release changelog extraction to use exact string matching and merge duplicate categories

## 0.6.0 - 2026-02-22

### Features
- Add compatibility tests and update type checking configuration
- Add debug logs for session notifications
- Add app version display in settings

### Refactor
- Extract ModelSelect component to reduce duplication

### Style
- Add custom scrollbar and bottom padding to ChatView

## 0.5.0 - 2026-02-20

### Features
- Implement chat virtualization and refactor interaction components
- Implement LAN access setting for web UI

### Fixes
- Remove redundant key prop in renderItem and fix double space in AskUserCard className

## 0.4.1 - 2026-02-20

### Features
- Add Claude 4.6 Sonnet, Gemini 3.1 Pro and update Opus 4.6 Fast multiplier

### Fixes
- Resolve react hook violation and refactor draft handling

## 0.4.0 - 2026-02-18

### Features
- Implement session configuration and worktree branch management
- Refactor session bootstrap UI and integrate workspace prep status

## 0.3.2 - 2026-02-18

### Fixes
- Correct electron-builder architecture flags in release workflow

## 0.3.1 - 2026-02-18

### Fixes
- Correct electron-builder command syntax in release workflow

## 0.3.0 - 2026-02-18

### Features
- Track remote branches in workspace manager and refine sidebar UI

### Fixes
- Add artifact download step before release
- Improve changelog extraction and clean up workflow

### CI
- Split mac builds and automate changelog extraction

## 0.2.0 - 2026-02-18

### Features
- Enhance session management in runDroidAndCaptureAssistantText and add tests for session-id handling
- Add support for branch-derived titles in session store and update dependencies

### Fixes
- Correct typo in .gitignore for attachment exclusion
- Add note for unverified app installation on macOS
