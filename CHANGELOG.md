## 1.8.1 - 2026-09-24

### Fixed

- Tables in replies on the iPhone line their columns up: every cell of a column has the same width, sized to the column's widest text. Before, each row sized its cells on its own, so the columns drifted apart from row to row.

## 1.8.0 - 2026-09-24

### Added

- Settings → Remote Access → Pairing address: a host name the pairing link and QR code use instead of this computer's network address, for a name that also reaches it away from home (Tailscale, or a Surge Ponte name such as `laptop.myhome`).
- `DROI_HTTP_DOMAINS` when building the iPhone app (`DROI_HTTP_DOMAINS=myhome pnpm install:phone`) lets it reach more domains over plain http, next to IP addresses, `.local` and `ts.net`.

### Changed

- The sidebar lists the Workspaces with the most conversations first; Workspaces without any conversation come last. Before, loading an old Session could move its Workspace to the top.

### Fixed

- A pairing that fails on the iPhone says why, and points out a pairing link without its port.

## 1.7.1 - 2026-09-24

### Fixed

- Coming back to a Session you left part way up opens it where you were reading. In a long Session it could land far up the conversation, near the start. On the desktop and the web Client.
- Going back from a subagent opens the calling Session at the Task you opened it from, not near the start.
- "Scroll to latest" reaches the latest message in one click after switching Sessions, instead of stopping part way.
- Tables in replies no longer show a white strip above their header row.

## 1.7.0 - 2026-09-24

### Added

- Settings → Session defaults: what every new Session starts with, as on the Factory App's page. Default model, reasoning level, interaction mode and autonomy; spec mode model, reasoning level and save folder; compaction (automatic, token limit, limits for specific models, compaction model); and subagent autonomy and the light, medium and heavy task models. They are kept by the Daemon in `~/.factory/settings.json`, shared with the droid CLI and the Factory App; settings your organization manages show but cannot be changed. On the desktop, the web Client and the iPhone app (Settings → Session defaults, for the selected computer).
- Text to add to Droid's system prompt, under Session defaults on the desktop. It is appended to Droid's own prompt in every new Session, whichever device starts it; existing Sessions and subagents keep theirs.

### Fixed

- Spinners that appear at different moments, such as a column of running tools, now turn together. On the desktop, the web Client and the iPhone app.
- The Settings page fits a phone-sized browser: its sections sit above the content.

## 1.6.3 - 2026-09-24

### Fixed

- Switching Sessions no longer flashes the transcript part way up the conversation before it jumps to the end. Coming back to a Session opens it where you left it; one you left at the latest message opens at the latest message. "Loading session…" shows only when loading takes a moment. On the desktop and the web Client.

## 1.6.2 - 2026-09-24

### Fixed

- Pasting or dropping a file that is not an image (an HTML page, say) into the message box inserts its full path where the caret is, instead of just the file name or nothing. A path with spaces is quoted; images are still attached. On the desktop.

## 1.6.1 - 2026-09-24

### Changed

- iPhone app: inline code in a reply is a faint wash with warm brown text, instead of a hard grey box that joined across wrapped lines.

### Fixed

- A long path or name in inline code wraps inside the reply instead of giving the transcript a sideways scrollbar. On the desktop and the web Client.

## 1.6.0 - 2026-09-23

### Changed

- Droi uses far less CPU while a Session works or streams. The spinners no longer repaint the window every frame; a streamed reply redraws only the message it adds to, at most about 30 times a second; and scrolling back through a long Session does less work per message. On the desktop, the web Client and the iPhone app.
- iPhone app: built with the React Compiler, as the web Client is, and settled Markdown is parsed once.

### Fixed

- A subagent started from a Session that another program drives (the `droid` CLI, say) shows in the header's subagent menu and leads back to that Session.

## 1.5.0 - 2026-09-23

### Changed

- iPhone app: menus and pickers open as the system's bottom sheet. The dimmed background fades in place while only the sheet slides up, a swipe down or a tap outside closes it, and on iOS 26 it is drawn in Liquid Glass with a quieter title.
- The repository is split into `apps/desktop` (the desktop app and web Client), `apps/mobile` (the iPhone app) and `tests`; the commands are unchanged.

### Fixed

- Leaving a subagent that was started before its Session was compacted returns to the current Session, not an earlier part of the conversation, and the subagent switcher lists all of that Session's subagents.

## 1.4.0 - 2026-09-23

### Added

- Subagents no longer crowd the Session list. A Session that handed work to a subagent (a Task call) shows it as a card in the transcript: the subagent and its task, whether it is running, finished or failed, how many tools it used and for how long, an Open button, and the prompt and report under Details. The header lists the Session's subagents; inside a subagent the title leads back to the Session that called it and switches to its other subagents. The calling Session's row says how many subagents are running, and subagents raise no alerts of their own. On the desktop, the web Client and the iPhone app.
- The model pickers show Factory's usage multiplier for each model (for example 1.6×); custom models show none.
- A picked `/` command or skill shows as a tag in front of the message, so it is clear it will run; Backspace at the start or the tag's × removes it.

### Changed

- Back from Settings returns to the Session (or page) it was opened from, instead of New session.
- iPhone app: brand marks in the model picker and on the model button, the desktop's spinner and activity marks instead of iOS's, a full-point composer border, and more room around the composer's footer.

## 1.3.0 - 2026-09-23

### Added

- Droi for iPhone: a native iPhone app that is another Client of the Droi on your computers. Install it from this repository with `pnpm install:phone` while the iPhone is connected; it is signed with your own Apple account, and a free account's signature lasts seven days, so run the command again to renew it (Settings shows the days left).
- Pair it by scanning the QR code in Droi → Settings → Remote Access or by pasting the pairing link. Several computers can be paired and switched from the drawer; the phone recognises a computer after its address changes, and keeps the Pairing Tokens in the iPhone keychain.
- It offers what the web Client offers: the Session list by Workspace with Working, Needs input and unread marks; the transcript with Markdown, tool rows, diffs and reasoning; the composer with photos and the camera, queued messages, the task list, the context meter, `/` commands and skills and per-Session drafts; permission and ask-user Prompts; New session; the Git changes in the header; pin, fold, archive and rename; the model, reasoning effort and autonomy.
- While the app is open, a Session finishing or starting to wait for an answer plays a short sound and a haptic, unless it is the Session on screen. Sounds follow the silent switch, and Settings turns the sound and the haptic on or off per event. Settings also picks the theme (system, light or dark) and the text size.
- In the background the app lets go of its connection; back in the foreground it reconnects and reloads the open Session, keeping the transcript and any typed text on screen.

### Changed

- The Gateway's `/meta` gives the computer's name and a stable id, which the iPhone app uses to recognise a computer.
- A Session continued after several `/compact`s can show every Session before it: each "Show earlier messages" adds the next earlier one, back to the first.
- Starting a New session in a typed folder no longer leaves a hidden draft Session open on the Daemon.

## 1.2.0 - 2026-09-23

### Changed

- The home view is the New session page; the "Select a session" placeholder is gone. Launch still reopens the last Session, and archiving the open Session or leaving Settings lands on New session.
- `/` on the New session page lists the Workspace's skills and commands before the first message is sent, and the page shows Droi's own mark.
- An ask-user question can be cancelled with its Cancel button or Escape; the Droid is told the question was dismissed.
- The sidebar shows "Needs input" for a Session waiting on a question or permission, and marks a Session that finished or started waiting while you looked elsewhere. The desktop app plays a sound (Factory's own, a bell, or a file you pick) and can show a notification that opens the Session; Settings → Notifications picks the sound per event and whether it plays always, only while Droi is focused, or only while it is not.
- The Session header has an "Open in" button that opens the Workspace in an installed editor, Finder or terminal (VS Code, Cursor, Zed, Xcode, Android Studio, Terminal, iTerm2, Ghostty, kitty, Warp) and remembers the last one picked. Desktop app only.
- The header's Git changes follow a turn's edits as they happen, and pick up edits made outside Droi within 15 seconds, without reopening the Session.
- Earlier messages stay in the transcript after the Daemon compacts a long Session's context, and a long Session opens with its whole history instead of the last 30 messages.
- Opening a tool row or reasoning block near the bottom keeps it where it was clicked instead of jumping to the end.
- A diff's line numbers stay readable when it is scrolled sideways, and a Create row shows the new file's content.
- A long link or word in a sent message wraps inside its bubble.
- More than two queued messages fold into one row that opens to the list, and finished tasks in the task list show a green check.
- The Kimi mark in the model picker is visible on the light theme.

## 1.1.2 - 2026-09-21

### Changed

- While the Desktop Shell is still starting the Daemon, the Client shows a quiet "Starting the Daemon" wait in place of the content; the warning banner is kept for a connection that was lost, or a Daemon that has not answered after 20 seconds.

## 1.1.1 - 2026-09-21

### Changed

- An available, downloading or installed update shows as a small card in the bottom-left corner (close it to hide that step), instead of a line under the header.

## 1.1.0 - 2026-09-21

### Changed

- The Desktop Shell reuses the `droid` CLI's login from `~/.factory` (the same login the Daemon runs as), so opening Droi needs no sign-in of its own. Sign in with Factory in Settings only to use a different account; an API key remains the fallback.

## 1.0.0 - 2026-09-20

### Upgrading

- Droi 1.0 is a rebuild: a thin Client of `droid daemon` with a Desktop Shell and a phone Client. A 0.x install cannot update in place; download the DMG once, and from 1.0 on updates arrive in the app.

### Changed

- Rebuilt as a thin Client of `droid daemon`: the Desktop Shell starts and supervises the Daemon and hosts a Gateway; the Client (desktop window and phone browser) speaks only the Daemon protocol through `@factory/droid-sdk`.
- Sign in with Factory through the `droid` CLI's device flow; an API key remains the fallback for automation.
- Redesigned after Waku: collapsible sidebar grouped by Workspace with pins, folds and context menus; a model picker with brands and favorites; quiet tool clusters with success and failure marks, Edit results as line diffs, reasoning folded away.
- Composer with pasted or picked images, queued messages, task list, context meter, `/` commands and skills, `/compact` handing off to a child Session, drafts kept per Session.
- Prompts (permissions and ask-user questions) take the composer's place; every attached Client sees and answers them, first answer wins.
- Transcript opens on the latest message, follows streamed output, and offers a way back down.
- Session header shows the Git branch with uncommitted counts and the changed files.
- Settings: theme, font (Geist or the system face), text size, `droid` path, Factory API base URL, Remote Access with pairing QR code and link.

### Added

- Phone access through pairing (QR code or pasted link) and Remote Access, with a PWA manifest and iOS safe-area handling.
- In-app update: the Shell fetches the latest Release's `app.asar`, verifies its SHA-256 and swaps it in; Settings → About checks and installs, a banner offers the restart.
- End-to-end suite against a Fake Daemon, an Electron smoke suite, and a live test against a real Daemon.

### Removed

- Droi's own JSON-RPC layer, session files under `~/.droid-app`, API key rotation and proxy, Hono API, Mission GUI, diagnostics and trace-chain, PostHog.

## 0.33.2 - 2026-04-25

### Fixes
- Improve key selection logic for spillover threshold and tie-breaking in `selectActiveKey`

## 0.33.1 - 2026-04-10

### Fixes
- Handle malformed `<{...}>` JSON render blocks in MarkdownRenderer

## 0.33.0 - 2026-04-10

### Features
- Add support for `<json-render>` tags in MarkdownRenderer

### Refactor
- Simplify `<json-render>` parsing by removing streaming logic

## 0.32.1 - 2026-04-10

### Tests
- Update mission and session tests for dynamic model catalog

## 0.32.0 - 2026-04-10

### Features
- Implement model catalog and dynamic model selection

## 0.31.1 - 2026-03-26

### Fixes
- Roll back the broken `0.31.0` release to the stable `0.30.1` code state

## 0.30.1 - 2026-03-19

### Refactor
- Update workspace manager to use workspaceDir and improve session state

## 0.30.0 - 2026-03-18

### Features
- Implement `getGitWorkspaceLookupDir` and improve session workspace handling

## 0.29.3 - 2026-03-18

### Fixes
- Adjust ChatPage layout for TodoPanel and AnimatePresence
- Refine ChatPage layout and input animation key

## 0.29.2 - 2026-03-18

### Tests
- Simplify mission directory watcher event assertion

## 0.29.1 - 2026-03-18

### Refactor
- Replace `@lobehub/ui` with `streamdown` for markdown rendering

## 0.29.0 - 2026-03-18

### Features
- Enhance key management with session bindings and usage tracking
- Add ApplyPatch support with detailed diff view and target path extraction

## 0.28.0 - 2026-03-16

### Features
- Remove deprecated model options and update the reasoning effort map
- Enhance tool execution UI with duration tracking and progress details

## 0.27.0 - 2026-03-13

### Features
- Integrate telemetry service and enable usage analytics
- Simplify telemetry events and enhance startup metrics tracking

## 0.26.0 - 2026-03-13

### Features
- Enhance session management and startup metrics tracking

## 0.25.0 - 2026-03-12

### Features
- Implement session restoration and merging logic for live updates

### Refactor
- Externalize TodoPanel and GitActionsButton state management

## 0.24.0 - 2026-03-12

### Features
- Enhance workspace handling with local workspace support

### Fixes
- Adjust TodoPanel padding for better layout

### Refactor
- Move and simplify the add-project button in the sidebar

## 0.23.0 - 2026-03-11

### Features
- Enhance mission model handling and runtime selection logic
- Enhance Mission Control feature details and state management

## 0.22.1 - 2026-03-11

### Refactor
- Improve code readability by formatting and simplifying conditional expressions

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
