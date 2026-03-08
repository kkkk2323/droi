---
name: droi-e2e-test
description: |
  End-to-end UI testing for the Droi Electron app using agent-browser via CDP.
  Use this skill when the user asks to test the Droi desktop application, run E2E tests,
  automate UI interactions, verify session management, or validate UI changes after development.
  Triggers: "test the app", "E2E test", "UI test", "agent-browser test", "test session creation",
  "test session deletion", "verify the UI works", "run automated tests".
---

# Droi E2E Testing with agent-browser

This skill guides you through safely testing the Droi Electron application using agent-browser
connected via Chrome DevTools Protocol (CDP). It covers launching the app, connecting, navigating
the UI, and performing session CRUD operations.

## Architecture Context

Droi is an Electron 40 + React 19 app with:
- **Main process**: `src/main/index.ts` - creates BrowserWindow, registers IPC, starts API server
- **Renderer**: React SPA with TanStack Router, Zustand state, shadcn/ui components
- **Key UI areas**: Sidebar (projects/sessions), ChatPage (messages), InputBar (model select, send)

The app has `data-testid` attributes on key interactive elements and supports a
`ELECTRON_REMOTE_DEBUGGING_PORT` environment variable for enabling CDP.

## Step 1: Launch the App with CDP

```bash
cd <project-root>
ELECTRON_REMOTE_DEBUGGING_PORT=9222 pnpm dev
```

Or use the shorthand script:
```bash
pnpm dev:test
```

Wait ~10 seconds for the app to start, then verify CDP is available:
```bash
agent-browser connect 9222
```

## Step 2: Wait for App Initialization

The app has a multi-step async initialization (version check, API key loading, state restoration,
session loading). Do NOT interact with the app until initialization completes.

Check for the `data-app-ready` attribute on `<body>`:
```bash
agent-browser eval 'document.body.hasAttribute("data-app-ready")'
```

If it returns `false`, wait and retry:
```bash
agent-browser wait 3000
agent-browser eval 'document.body.hasAttribute("data-app-ready")'
```

## Step 3: Understand the data-testid Map

### Static testids (always the same)
| testid | Element | Notes |
|--------|---------|-------|
| `sidebar-add-project` | "New Project" button | Opens native file dialog (cannot be automated) |
| `sidebar-settings` | "Settings" button | Navigates to settings page |
| `sidebar-trigger` | Sidebar toggle | Show/hide sidebar |
| `chat-input` | Message textarea | The main input field |
| `chat-send` | Send button | Disabled when input is empty |
| `chat-cancel` | Cancel/stop button | Only visible when a session is running |
| `chat-force-cancel` | Force stop button | Only visible during cancellation |
| `model-select-trigger` | Model dropdown trigger | Opens model selection popup |
| `session-mode-local` | "Work from Local" option | In new session config page |
| `session-mode-new-worktree` | "New WorkTree" option | In new session config page |

### Dynamic testids (include entity IDs for uniqueness)
| Pattern | Element | Notes |
|---------|---------|-------|
| `project-{displayName}` | Project row in sidebar | e.g. `project-New project` |
| `new-session-{displayName}` | "+" button per project | e.g. `new-session-New project` |
| `session-{sessionId}` | Session row in sidebar | UUID-based, unique |
| `session-menu-{sessionId}` | Session "..." menu trigger | opacity:0 by default, visible on hover |
| `session-pin-{sessionId}` | Pin/Unpin menu item | Inside session dropdown menu |
| `session-delete-{sessionId}` | Delete menu item | Inside session dropdown menu |
| `confirm-delete-session-{sessionId}` | Confirm delete button | In delete confirmation dialog |
| `cancel-delete-session-{sessionId}` | Cancel delete button | In delete confirmation dialog |

## Critical Safety Rules

### RULE 1: Always use session-ID-specific testids for destructive operations

**NEVER** use `querySelector("[data-testid='session-delete']")` without the session ID suffix.
The DOM contains one delete menu item per session - `querySelector` returns the FIRST match in
DOM order, which is typically the first project's first session, NOT the session you intended.

```javascript
// WRONG - will delete the wrong session!
document.querySelector("[data-testid='session-delete']")?.click()

// CORRECT - targets exactly the right session
document.querySelector("[data-testid='session-delete-{sessionId}']")?.click()
```

The same applies to `confirm-delete-session-*` and `cancel-delete-session-*`.

### RULE 2: Discover session IDs before operating on them

Before performing any session operation, query the DOM to find the exact session ID:

```bash
agent-browser eval 'JSON.stringify(
  Array.from(document.querySelectorAll("[data-testid^=\"session-menu-\"]"))
    .map(el => ({
      id: el.getAttribute("data-testid").replace("session-menu-", ""),
      title: el.closest("[class*=\"group/session\"]")
        ?.querySelector("[data-testid^=\"session-\"]")
        ?.textContent?.substring(0, 60)
    }))
)'
```

This returns a JSON array of `{id, title}` pairs. Use the `id` to construct safe testid selectors.

### RULE 3: Only operate on the designated test project

If the user specifies a project (e.g., "New project"), ONLY create/delete sessions within that
project. Never touch sessions in other projects.

To verify you're in the right project, check the project name:
```bash
agent-browser eval 'document.querySelector("[data-testid=\"project-New project\"]")?.textContent'
```

### RULE 4: Handle hover-only elements via JavaScript

Some elements (session menu "...", project menu) are `opacity: 0` and only visible on CSS
`:hover`. The `agent-browser hover` command may not reliably trigger CSS hover states in CDP.

Use JavaScript to force-show and click these elements:
```bash
agent-browser eval 'const el = document.querySelector("[data-testid=\"session-menu-{id}\"]"); el.style.opacity = "1"; el.click()'
```

### RULE 5: Check animation state before interacting

Session items use framer-motion animations. Each session `motion.div` has a
`data-animation-state` attribute that transitions from `"animating"` to `"idle"`.

Wait for animations to complete before clicking:
```bash
agent-browser eval 'document.querySelector("[data-animation-state=\"animating\"]") === null'
```

## Common Test Flows

### Create a New Session

```bash
# 1. Click the "+" button for the target project (using data-testid)
agent-browser eval 'document.querySelector("[data-testid=\"new-session-New project\"]").click()'

# 2. Wait for SessionConfigPage to appear
agent-browser wait 1000
agent-browser screenshot step-new-session.png

# 3. Verify "Work from Local" is selected (default)
# The session-mode-local button should be visible

# 4. Select model (e.g., minimax-m2.5)
agent-browser snapshot -i  # find the model combobox ref
agent-browser click @<model-combobox-ref>
agent-browser snapshot -i  # find the model option
agent-browser click @<minimax-option-ref>

# 5. Type a message and send
agent-browser fill "[data-testid='chat-input']" "Test message"
agent-browser eval 'document.querySelector("[data-testid=\"chat-send\"]").click()'

# 6. Wait for session to be created and AI to respond
agent-browser wait 5000
agent-browser screenshot step-session-created.png
```

### Delete a Session

```bash
# 1. Discover the session ID
agent-browser eval 'JSON.stringify(Array.from(document.querySelectorAll("[data-testid^=\"session-menu-\"]")).map(el => ({id: el.getAttribute("data-testid").replace("session-menu-",""), title: el.closest("[class*=\"group/session\"]")?.querySelector("[data-testid^=\"session-\"]")?.textContent?.substring(0,60)})))'

# 2. Open session menu (force opacity for hover-only elements)
agent-browser eval 'const el = document.querySelector("[data-testid=\"session-menu-{SESSION_ID}\"]"); el.style.opacity = "1"; el.click()'

# 3. Click Delete in the dropdown (use session-specific testid!)
agent-browser eval 'document.querySelector("[data-testid=\"session-delete-{SESSION_ID}\"]")?.click()'

# 4. Confirm deletion in the dialog (use session-specific testid!)
agent-browser eval 'document.querySelector("[data-testid=\"confirm-delete-session-{SESSION_ID}\"]")?.click()'

# 5. Verify session is removed
agent-browser wait 2000
agent-browser screenshot step-session-deleted.png
```

### Switch Model

```bash
# The model selector is a combobox. Use snapshot to find its ref, then click to open.
agent-browser snapshot -i | grep combobox
# Click the first combobox (model select)
agent-browser click @<ref>
# Find the target model in the dropdown options
agent-browser snapshot -i | grep -i "minimax\|m2.5"
# Click the option
agent-browser click @<ref>
```

## Troubleshooting

### "Connection refused" on port 9222
The app wasn't launched with `ELECTRON_REMOTE_DEBUGGING_PORT=9222`. Kill all Electron processes
and relaunch:
```bash
lsof -i :9222 -t | xargs kill 2>/dev/null
lsof -i :5173 -t | xargs kill 2>/dev/null
ELECTRON_REMOTE_DEBUGGING_PORT=9222 pnpm dev
```

### snapshot shows refs but click times out
The element might be outside the visible viewport. Scroll it into view first:
```bash
agent-browser scrollintoview @<ref>
agent-browser click @<ref>
```

Or use JavaScript `eval` to bypass viewport requirements:
```bash
agent-browser eval 'document.querySelector("[data-testid=\"...\"]").click()'
```

### Clicked the wrong session / wrong project
This almost certainly means you used a non-unique selector. Go back to Rule 1 and Rule 2 above.
Always use session-ID-specific testids for any operation that modifies data.

### Native dialog blocks automation
`sidebar-add-project` triggers Electron's native `dialog.showOpenDialog`. This cannot be
automated via CDP. If the test requires adding a new project, this step must be done manually
or bypassed through the Zustand store:
```bash
agent-browser eval 'window.__zustand_store?.getState()?.handleSetProjectDir("/path/to/project")'
```
(Note: this depends on store exposure which may not be available.)

### Dropdown menu doesn't appear after click
Some dropdown menus require CSS hover state to be visible. Use the JavaScript force-opacity
pattern from Rule 4 above.

## Cleanup

After testing, kill the dev process:
```bash
kill <PID> 2>/dev/null
lsof -i :9222 -t | xargs kill 2>/dev/null
lsof -i :5173 -t | xargs kill 2>/dev/null
```
