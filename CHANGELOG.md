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
