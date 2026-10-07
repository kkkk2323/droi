## 1.36.0 - 2026-10-07

### Added

- An Automations page, under New session in the sidebar, shows the Daemon's local Automations: their state, schedule in local time, last run and next run. You can run one now, pause or resume it, edit it, delete it, and create a new one. A schedule is Hourly, Daily, Weekdays, Weekly or Custom, with a preview of the cron expression the Daemon gets. A run opens its Session.
- While the Factory App runs, the page warns that its Daemon runs the same Automations, so a run can start twice.

### Changed

- Sessions that an Automation ran no longer show in the sidebar or among the recent Workspaces. Open them from the Automation's runs.

## 1.35.0 - 2026-10-07

### Added

- Droi runs on Windows 10 and 11 (x64). Each Release has a `Droi Setup <version>.exe` that installs for your user, with no administrator rights, and updates itself like the Mac app.
- Until Droi can talk to the Daemon, the window is one sign-in page: Sign in with Factory, or paste a Factory API key. Settings stays reachable from the page.
- A Session can start in a Worktree, in the native app and the Phone App. On the New session page, choose Work locally or New worktree, then whether the Worktree is ephemeral or persistent, its base branch and its setup profile. A switch keeps New worktree as the default on this device. The Daemon creates and deletes the Worktree.
- The Session list shows a Session in a Worktree under its repository, with its branch.
- Archiving a Session in an ephemeral Worktree asks first, because the Daemon then deletes the Worktree.
- Settings → Worktrees (native app) sets the Worktree directory and the limit of ephemeral Worktrees, and lists the managed Worktrees, which you can delete (with their local or origin branch, if you choose).

### Changed

- Sign in with Factory now gives the login to the droid CLI on this computer, as Factory's own desktop app does. The Daemon runs as that login, so signing in works without `droid` signed in first. Signing out in Settings signs the CLI out too.
- The browser opens the Factory sign-in page by itself. Before, Droi showed a code and waited.

### Fixed

- After you sign in or add an API key, the window connects to the Daemon. Before, it stayed on “Starting the Daemon” until Droi restarted.
- The Daemon's notices show in the transcript as Droid's reply, for example “You've reached your 5-hour Droid Core usage limit” or a switch to another model. Before, a turn that ended on such a notice showed nothing, and a reopened Session showed the notice as a message from you.
- Two working Sessions no longer swap places in the sidebar. A Session moves up only when a turn starts.
- A background Task that finished shows Completed, with its tools and time. Before, it showed “Started in background”.
- You can select and copy the text of an AskUser question and of a queued message.
- Removing a queued message (×) removes its row at once.
- json-render tables fill the reading column. Before, a long cell came out one character to a line.

## 1.34.6 - 2026-10-07

### Added

- The end of each reply has a Copy button. It copies everything Droid wrote in that turn as Markdown, without the reasoning and the tool calls.

### Changed

- The end of a reply shows the time and how long the turn ran, without the word “took” (for example, 04:28 PM · 28m 56s).

### Fixed

- Typing / in a Session lists its Skills again after Droi starts. Before, the list asked the Daemon too early, got an error, and showed only the built-in commands for a minute.

## 1.34.5 - 2026-10-07

### Changed

- On the new-session page, the Workspace opens a picker with a search box. Type to filter your recent projects, use the arrow keys and Enter to pick one, or choose Other folder… to open any folder.
- The picker has Don't work in a project (before, None). Then the page asks “What should we work on?”, and a Work in a project button under the message box opens the picker again.

### Fixed

- VoiceOver can reach the Workspace button in the new-session question. Before, the question hid it.

## 1.34.4 - 2026-10-07

### Fixed

- You can select text in Droid's replies and copy it with ⌘C or Edit → Copy. Before, dragging over a paragraph looked like it did nothing, and Copy copied nothing. A selection stays inside one paragraph, list item or code block.
- Droi uses MyGo 0.2.16, which fixes a crash when the window enters full screen and uses less memory for pages you have left.

## 1.34.3 - 2026-10-07

### Fixed

- ⌘V pastes an image from the clipboard into the message box, on the new-session page and in a Session. Before, the Edit menu took the key and pasted only text, so an image paste did nothing.
- Hiding or showing the sidebar slides it again, as in the Electron app. Before, the conversation jumped to its new width at once.
- The new-session page shows the logo and the question in the middle of the window again. Before, they sat at the top.

### Changed

- The Session header no longer has the Copy session info button. Right-click a Session in the sidebar to copy its ID or details.

## 1.34.2 - 2026-10-07

### Fixed

- The new-session page's model picker has the Tools row again (Direct, Both, Script), as in the Electron app. Before, 1.34.0 showed the mode only as text, so a Session could start only in the default mode from Settings.

## 1.34.1 - 2026-10-07

### Fixed

- An image you paste or drop on the new-session page shows above the message box, with a button to remove it. Before, 1.34.0 attached it without showing it, so the paste looked like it did nothing.

## 1.34.0 - 2026-10-07

### Changed

- Droi on the Mac is a native app now. It is written in Go instead of running in Electron: on our test Mac it shows the Session list in under a second instead of about three, and uses 103 MB of memory instead of 526 MB when idle. It looks and works as before, and keeps your Sessions, settings, alert sounds, Memory and paired phones, as it uses the same data folder. The Electron app's update shows a card that links to the download; drag the new Droi into Applications to replace the old one.
- Droi updates itself: it checks soon after starting and then every hour, downloads only what changed when it can, checks the update's signature, and restarts once no Session is working and the window is in the background. Settings → About can check, download and restart at once.
- Release downloads have the native app only; the browser Client is gone. The phone keeps working: pair it in Settings → Remote Access as before.

### Fixed

- Memory no longer fails at the first start when its database does not exist yet. Before, the Memory Server and the hook could open the new database at the same moment and one of them gave up.

## 1.33.5 - 2026-10-05

### Fixed

- The message box takes your typing again after a Session sat idle for a long time. Before, once the Daemon had let the idle Session go, clicking the message box did nothing until you switched to another Session and back. Now the message you send loads the Session again first. On the desktop, in the browser and on the phone.

## 1.33.4 - 2026-10-04

### Fixed

- On a busy computer, loading previous messages keeps the message you are reading where it was. Before, it could move down by about a row once the earlier messages took their real heights. On the desktop and in the browser.

## 1.33.3 - 2026-10-04

### Fixed

- The model picker under the message box stays usable after previous messages load. Before, scrolling up in a long Session or jumping to an earlier message from the rail turned it grey, showing the model's id, until you started a new Session. On the desktop, in the browser and on the phone.

## 1.33.2 - 2026-10-04

### Fixed

- Loading previous messages keeps the message you are reading where it was again. In 1.33.1 it could move down by about a row. On the desktop and in the browser.

## 1.33.1 - 2026-10-04

### Fixed

- The rail of your messages has a mark for every message you sent, also after a restart. A Session opens with its latest 400 messages, and in a long run of tool calls those can hold one of yours or none, so the rail lost its older marks or did not show. It now reads your messages alone from the Daemon; clicking a mark from before the loaded messages loads the way there, its mark pulsing meanwhile, and jumps to it. On the desktop and in the browser.
- A subagent started in the background shows as running at once: its Task card says Running instead of Started in background, and the calling Session's row in the sidebar says how many subagents are running. Before, both waited until the subagent finished or you switched Sessions. On the desktop, in the browser and on the phone.

## 1.33.0 - 2026-10-03

### Added

- A rail along the transcript's right edge has a mark for every message you sent in the Session. Hovering or focusing a mark previews the message and the start of Droid's reply; clicking it jumps there, and the mark of the turn you are reading stands out as you scroll. The arrow keys, Home and End move between marks. It shows once a Session has two of your messages and the window has room beside the reading column. On the desktop and in the browser.

## 1.32.0 - 2026-10-03

### Added

- When Droid saves a Memory Entry, the answer lists up to three entries that may already say the same thing, from the same Memory and from the other scope (Global Memory for a Project write, the Workspace's Project Memory for a Global one), so Droid can merge or replace the old entry instead of keeping both. On the desktop, in the browser and on the phone.
- Memory keeps a log of every Memory Server call: the tool, the query, and the entries it found, wrote or showed as similar. Settings → Memory shows, for each Memory, its searches since the log began, how many found nothing, and how many entries no search has returned yet. On the desktop.

## 1.31.1 - 2026-10-03

### Fixed

- A Session started with Workspace: None no longer shows its folder (named like `2026-10-02-5c4802`) as a Workspace in the sidebar when the Daemon lists it without its tags. It stays under Recents, and the folder is left out of the recent Workspaces in New session. On the desktop, in the browser and on the phone.

## 1.31.0 - 2026-10-03

### Added

- Tool calls has a third choice, Both: the new Session's model may call tools directly or through Script, as it sees fit. It sits between Direct and Script in the model picker, on the phone's Tool calls sheet and in Session defaults. On the desktop, in the browser and on the phone.

## 1.30.0 - 2026-10-03

### Added

- A new Session can call tools directly or through Script: the choice sits under the reasoning effort in the model picker (on the phone, a Tool calls button next to it). Settings → Session defaults → Tool calls sets what new Sessions start with on this device: Follow droid settings, Direct or Script. A Session keeps the mode it starts with, since the Daemon cannot change it later. On the desktop, in the browser and on the phone.

## 1.29.0 - 2026-10-03

### Changed

- A pinned Session moves to the Pinned section at the top of the sidebar, before the pinned Workspaces, instead of only going first in its own Workspace or in Recents, where Recents sits at the bottom and the pin was easy to miss. Unpinned, it goes back. On the desktop and in the browser.

### Fixed

- Closing a tool row after scrolling part of the way down no longer throws the transcript up; the latest output stays in view. On the desktop and in the browser.
- A Session that starts working moves up to the top of its Workspace in the sidebar, with its time, instead of keeping the place and time from the last time the list was read. On the desktop, in the browser and on the phone.

## 1.28.0 - 2026-10-02

### Added

- A Session's id, or its details for another Session to read, can be copied from the Session's context menu in the sidebar and from a button in the Session header. The details list the title, the Session id, the Workspace and the path of the transcript file on the computer. On the desktop and in the browser.

## 1.27.0 - 2026-10-02

### Added

- Math in a reply is drawn as a formula instead of shown as TeX: a block between `$$` and inline math between single `$`. TeX that cannot be read is shown as its source, and a formula wider than the column scrolls on its own. On the desktop, in the browser and on the phone.

### Fixed

- A reply that mentions `<json-render>` in inline code no longer shows everything after it as a JSON code block. On the desktop, in the browser and on the phone.

## 1.26.0 - 2026-10-02

### Added

- A Script, the Daemon's tool that runs a small program calling other tools, shows as one row with the calls it made listed under it, instead of a row of escaped JavaScript beside them. It opens to the program with line numbers, its inputs, and what Droid read back, with the run's figures and its log file below. A run that takes longer than a minute says it is still running, and where Droid waits for it the calls it made meanwhile appear. A Script's permission card lists each call it will make by line, with the command and its impact, and shows the program on request. On the desktop, in the browser and on the phone.
- Rich output in a reply (`<json-render>`: tables, status lines, progress bars, charts, cards, timelines) is drawn in place instead of showing as raw JSON. On the desktop, in the browser and on the phone.

### Fixed

- A Session no longer waits forever when a Script asks for permission. The request never reached Droi, so the Session showed "Needs input" with no card to answer. On the desktop, in the browser and on the phone.

## 1.25.0 - 2026-10-01

### Changed

- The desktop app checks for a new release every hour, not only once after launch, so a release published while Droi stays open shows its card in the bottom-left corner within the hour. A check waits while an update is offered, downloading or waiting for a restart. On the desktop.

## 1.24.0 - 2026-10-01

### Changed

- The phone opens on the Session list instead of the last Session with the list in a drawer. Tapping a Session opens it on top, and a swipe from the left edge or the back button returns to the list, as in other iPhone apps. The list's title shows the computer and its connection and switches computers; New session and Settings sit at the top right. A subagent opens on top of its calling Session, so going back returns to the caller. The app still reopens the Session it last showed, with the list one swipe back, and a computer that cannot be reached keeps its last known list below the notice. On the phone.

## 1.23.0 - 2026-10-01

### Changed

- Memory search answers with the entries that hold every word first, and falls back to any word only when no entry holds them all; a quoted phrase is kept whole. Before, any word matched from the start, so a long query filled its ten results with entries sharing one common word. A one-character word (用, a) beside others is ignored instead of pulling in nearly every entry. On the desktop, in the browser and on the phone.

## 1.22.0 - 2026-10-01

### Changed

- A chat without a project (a Scratch Session) keeps no Project Memory. Its folder is made for the Session and trashed with it, so a fact saved there was never found again. Droid is told at the start that only Global Memory applies, a `project` save is refused with a pointer to `global` for facts about you or your computer, and the extraction from a long chat keeps only its Global entries. On the desktop, in the browser and on the phone.

## 1.21.1 - 2026-09-30

### Fixed

- While a turn runs, its tool calls show again. A Session whose model runs through a provider Droi did not know yet (Azure Anthropic) showed only a column of reasoning, and its tool calls appeared only after reopening the Session. On the desktop and the phone.

## 1.21.0 - 2026-09-30

### Changed

- Settings are grouped by what they are for, each group one card. The Factory API key sits with the sign-in under Account, and it reads "Not needed" while you are signed in. The Daemon tab is now Advanced: Daemon status and the droid executable, the Factory API base URL, and the system prompt addition and Scratch folder, which apply only to Droi and so left Session defaults. Version and updates have their own About tab, which the update card's Details opens. Notifications are grouped by event (finished, needs input), Remote Access shows pairing before the pairing address, Memory says when nothing is remembered yet, and every field's button reads Save. On the desktop.
- The phone's Settings follow the desktop's order: computers, appearance, the Session list, Session defaults, alerts, About. On the phone.

### Fixed

- After the Daemon tab, the Remote Access tab no longer shows a leftover "Factory API base URL" row. On the desktop.
- Account no longer says the droid CLI is not logged in while it shows you signed in with that login, and About no longer claims nothing leaves the computer. On the desktop.

## 1.20.2 - 2026-09-30

### Fixed

- Sending from New session on the phone opens the new Session and sends the first message again. Before, the page stayed where it was with a read-only composer, because the Daemon lists a Session only after its first message and the phone waited for it to be listed. On the phone.

## 1.20.1 - 2026-09-29

### Fixed

- A turn's time sits under its reply and says how long the turn took, counted from the prompt that started it (`23:13 · took 4m 12s`). A message sent while the turn ran no longer puts a time under the tool calls before it. On the desktop, in the browser and on the phone.
- The Daemon's record of a hook run (a user message with no content, one per prompt once Memory is on) was drawn as an empty bubble and split the reply around it; it is skipped now. On the desktop, in the browser and on the phone.

## 1.20.0 - 2026-09-29

### Changed

- A tool call from an MCP server shows the server as a muted prefix and the tool by its own name, instead of the Daemon's `server___tool` string, with a plug icon. A call with no command, file or query to show lists its first few inputs as `key: value` (`scope: project · category: insight`) rather than nothing; a Skill load names the skill. On the desktop, in the browser and on the phone.

## 1.19.0 - 2026-09-29

### Added

- Memory (Beta): Droid can remember across Sessions on Droi's own Daemon. It is off until switched on under Settings › Memory, which restarts the Daemon after a warning. Each Workspace has a Project Memory and there is one Global Memory, kept in a SQLite database under Droi's user data. Droid searches and saves them through Droi's own `droi-memory` MCP server, and a new Session starts with the Workspace's and Global Memory's corrections. Writes that look like secrets are refused. Beside the database, each Memory has a read-only Markdown copy. On the desktop.
- When a long Session saved nothing, Droi extracts what is worth keeping once it compacts or ends, in a hidden Session on the model chosen under Settings › Memory. From the same page you can consolidate a Memory, open the Memory folder and reset the prompts to their defaults. The page lists every Memory with its entries, size and last consolidation, and a corner card says when a Project Memory grows past its soft limit. On the desktop.
- The MCP panel shows `droi-memory` as Droi's own server, without a switch, and says where Memory is switched on. On the desktop, in the browser and on the phone.
- A Session can be renamed from its context menu in the Session list. The row turns into a title field; Enter or leaving the field saves the title, and Escape keeps the old one. On the desktop and in the browser.

## 1.18.1 - 2026-09-29

### Fixed

- A `/compact` that finishes while you are reading another Session no longer switches the view back to the compacted one. After a handoff from an older Daemon the view still moves to the new Session, but only if you are still on the one being compacted. On the desktop, in the browser and on the phone.

## 1.18.0 - 2026-09-29

### Added

- When the droid CLI updates itself, a card in the bottom-left corner says so and offers to restart the Daemon, which still runs the earlier version. Restarting from the card or from Settings › Session defaults clears it; closing it hides it until droid updates again. The Desktop Shell notices the new droid within about 10 seconds while it runs. On the desktop.

### Fixed

- The corner cards sit above the sidebar footer instead of covering its Settings button.

## 1.17.0 - 2026-09-28

### Added

- A Solarized Light+ theme (github.com/ryanolsonx/vscode-solarized-theme), chosen under Settings › Theme beside Light and Dark. It puts the theme's VS Code workbench colours on Droi's surfaces: the editor's cream behind the conversation, the sidebar's parchment behind the Session list, the unfocused list selection on the selected row, and VS Code's own light greys for the sidebar text. The conversation's text is Solarized's base01. On the desktop, in the browser and on the phone.
- Code blocks are highlighted: github-light and github-dark under the Light and Dark themes, the theme's own colours under Solarized Light+. On the desktop and in the browser.

### Changed

- Status colours (success, attention, working, added, removed, highlight) come from the theme instead of fixed greens, ambers and reds, so every theme colours them its own way.
- Text keeps the platform's font smoothing instead of the thinner antialiased rendering, matching VS Code and the rest of macOS. On the desktop and in the browser.

## 1.16.0 - 2026-09-28

### Added

- A sort menu beside the Session list's search box orders the workspaces by most sessions (as before), recent activity, name or by hand, and the sessions in each group, Recents too, newest first or by creation. By hand starts from the order on screen; a workspace is dragged into place or moved up and down from its context menu. The Daemon lists no creation time, so each device keeps the time it first saw a session, never later than its last change. The choices are kept per device. On the desktop and in the browser.
- A folded workspace, Recents, Pinned or Workspaces section shows how many of its sessions are working and how many wait for an answer.

### Changed

- The reasoning effort is set in the model picker: the button reads "GPT-5 High", and the picker ends in a row naming every level of the chosen model. Changing it leaves the picker open; picking a model closes it. The separate effort dropdown is gone from the composer. On the desktop and in the browser.
- The workspace folder icon in the Session list is smaller and lighter, and shows open when the group is.

### Fixed

- On macOS the window's traffic lights sit level with the sidebar toggle and the page headers instead of about 3pt higher.
- Closing the skills and MCP dialog no longer collapses it to its header during the fade.

## 1.15.2 - 2026-09-28

### Changed

- On the desktop, "Other folder…" on the New session page opens the system's folder dialog instead of asking for a typed path. The folder picked becomes the workspace, ready for the first message; cancelling keeps the one that was there. In a browser or on the phone, which cannot open the computer's folders, the path is still typed.

## 1.15.1 - 2026-09-28

### Changed

- Adding an MCP server opens a view of its own: a search box over Factory's catalogue, where one click adds an entry as it is. An entry whose command holds a value to fill in first (such as an API key) says "Set up" and opens the form filled in, with its note. A link at the foot opens the form for a server by hand, and Back leads from the form to the catalogue and on to the list. On the desktop, in the browser and on the phone.

### Fixed

- The command, arguments, URL and headers fields of the add-server form no longer collapse to a sliver on the desktop.

## 1.15.0 - 2026-09-28

### Added

- A picture a tool handed back (a Read of an image file, a screenshot) shows in the tool row's detail when it is opened, from the result itself. On the desktop, in the browser and on the phone.
- The Session list says "Compacting" while a conversation's context is being summarised, for an automatic compaction and for a `/compact` started here. When a `/compact` finishes it counts as a completion (sound, notification and unread mark as configured), and the footer under the composer says for a few seconds how many messages were summarised.

### Changed

- On the desktop, skills and MCP servers moved from the conversation's header to the composer's footer row, next to the model, reasoning and autonomy pickers: one button reading "Skills 96/98 · MCP 1" (skills on / total, a connection dot for the servers) whose hover text lists the servers and their state.
- Each skill is a switch instead of an Enabled/Disabled menu: a project skill switches for its project, a built-in one across all projects. Personal skills still have none; one turned off by the organization, in its own file or at the other level shows locked with the reason. On the desktop, in the browser and on the phone.

## 1.14.0 - 2026-09-28

### Added

- A wrench in a conversation's header opens its skills and MCP servers. Skills are grouped by where they live, with the switches the Daemon allows: a project skill off for that project, a built-in one across all projects, one the organization turned off read-only. A skill switched off leaves the "/" menu at once.
- MCP servers show their connection and their tools, switch on and off, tool by tool too, and can be removed; new ones come from Factory's catalogue or a form for a command, an HTTP or an SSE server. A server that needs signing in offers the page to open; from the phone or a remote browser the link is copied to open on the computer, where the Daemon takes the callback.
- Changes are written where the droid CLI and the Factory App read them (`~/.factory/settings.json`, `~/.factory/mcp.json`, the workspace's `.factory/`), so they show up there and in the next conversation.

### Fixed

- On the phone, opening a tool row, a reasoning fold or a subagent's details no longer flickers: the row grew a frame before the scroll that kept its header in place. The same fix stops output streaming within half a second of a tap from being scrolled twice.

## 1.13.2 - 2026-09-28

### Changed

- On the phone, a conversation opens on its latest message at once. Before, it drew from the first message and scrolled down through all of them, which flashed the whole conversation past on the way.
- On the phone, the transcript keeps what you are reading where it is while the reply goes on below, including an earlier part of the same turn, and the keyboard no longer moves it. Older messages come in above as you scroll up.
- On the phone, a long reply stays smooth while it streams: only its unfinished end is parsed again on each update, instead of the whole reply.

### Fixed

- A conversation reopened part way up could land at the end instead of where it was left, on a slow machine.

## 1.13.1 - 2026-09-27

### Changed

- Hovering a workspace header in the sidebar greys the whole row, the new-session button included, as Notion does; the button darkens a little more under the pointer. The Pinned, Workspaces and Recents headers do the same.
- A pinned workspace no longer shows a pin after its name; it already sits under Pinned.

## 1.13.0 - 2026-09-27

### Added

- A search box in the sidebar (and the phone's drawer) searches every session on the computer through the Daemon, two characters and up. The hits take the place of the workspace groups, each with the matching passage highlighted under its title.
- The sidebar lists the newest 100 sessions and offers "Load older sessions" at the bottom for the next 100, instead of stopping at the first 100 for good.
- A session working for another client, say one driven from the phone, shows as Working or Needs input in the sidebar on the computer too, without being opened there first.
- Escape in a conversation interrupts the running turn, as in the Factory App.
- Cancelling a turn keeps the messages that were queued behind it, marked Paused. A pencil puts one back into the composer to edit and send again; Remove drops it.
- Settings → Daemon shows what the Daemon is doing: running with its port, starting, or not running with the reason, the next attempt and the end of its log. The Daemon's output now goes to `logs/daemon.log` next to Droi's settings, kept to a megabyte, and "Show log file" reveals it.

### Fixed

- A screenshot the agent retakes under the same name shows its new picture when the conversation is opened again; the old one stayed before.
- A paused queued message could not be removed: Remove asked the Daemon, which no longer knew it.
- The Daemon counts as running once it answers `GET /health`, not as soon as its port opens, so a Daemon whose RPC server never came up is restarted instead of being waited on.

### Changed

- On macOS and Linux the Daemon watches a pipe from Droi (`--liveness-fd`) and ends the moment Droi does, instead of polling Droi's process id.

## 1.12.2 - 2026-09-27

### Changed

- A long conversation opens with its last 400 messages, and "Load previous messages" at the top brings in the ones before, 100 at a time, the way the Factory App does. The row being read stays where it is while they arrive. This replaces 1.12.1's fixed 2,000-message load.

## 1.12.1 - 2026-09-27

### Fixed

- A long conversation opens whole again. The Daemon sends only the last 100 messages of a session unless asked for more, so after a restart, or on the phone, everything before that point was missing and the transcript could not be scrolled further up. Droi now asks for the last 2,000 messages and says so at the top when a session is longer still.

## 1.12.0 - 2026-09-27

### Added

- Pinned, Workspaces and Recents in the desktop and browser sidebar are sections that fold as a whole, like a single workspace does. Recents has the same header as the other two, with its sessions listed straight under it, instead of a folder row.

### Fixed

- An image the agent writes into its reply as a path on the computer, such as a screenshot it took, is shown in the conversation. It read "Image not available" before. Droi reads the file through the Daemon, so it works on the desktop and on a phone or browser alike; a file that is not there shows its path instead.

## 1.11.2 - 2026-09-27

### Fixed

- The Settings page's left navigation is as wide as the session sidebar, so the content no longer jumps sideways when switching between Settings and a conversation.

## 1.11.1 - 2026-09-26

### Fixed

- The conversation is centred in the window, in line with the message box. Once a conversation was long enough to scroll, the hidden scrollbar took room on the right only, so the messages sat a little to the left, which showed most with the sidebar hidden. The column also no longer shifts sideways when the scrollbar appears.

## 1.11.0 - 2026-09-26

### Changed

- A new icon: a warm off-white tile with a geometric D and one green dot, in place of the dark tile with the glowing green D. The Mac app, the browser tab and the iPhone app use it, and the New session page shows the same mark. On the iPhone the icon now fills the whole square and iOS rounds it, instead of a rounded tile with a margin. macOS may keep showing the old icon in the Dock for a while after updating.

## 1.10.3 - 2026-09-26

### Fixed

- A session compacted with `/compact` stays in the sidebar. droid 0.228 compacts in place, keeping the same session, and Droi marked that session as continuing itself, so it disappeared from the sidebar once you switched away and could not be opened again. Sessions already affected show up again after the update.

## 1.10.2 - 2026-09-26

### Fixed

- Settings → Session defaults → Compaction model saves a model newer than Droi's copy of the droid SDK knows, such as DeepSeek V4.1 Flash. Before, the choice never reached the Daemon and the page went back to the stored model; a compaction model set that way elsewhere (for example in the droid CLI) kept the whole Session defaults page from loading.

## 1.10.1 - 2026-09-26

### Changed

- Sessions in the iPhone's session list are taller rows (44pt, Apple's minimum touch target) with larger titles, so they are easier to tap and read.

## 1.10.0 - 2026-09-26

### Added

- New session → Workspace: None starts a session that is not next to any project, the way a ChatGPT conversation stands on its own. Droi makes it a folder of its own in `~/.droi/chats` (Settings → Session defaults → Scratch folder), and these sessions are listed together under Recents at the bottom of the sidebar and the iPhone's session list. With no recent workspace yet, None is the default. Archiving one moves its folder to the Trash; unarchiving brings the folder back.

### Changed

- Droi for iPhone is built on Expo SDK 57 (React Native 0.86) with Xcode 27 and the iOS 27 SDK, so it installs on this year's iPhones.

### Fixed

- The spinners on the iPhone keep turning. Each time one was replaced by another it swept a shorter arc, and after a few seconds of tool calls they stood still.
- A long file name in the Git changes list is cut short instead of running over the line counts and pushing the list sideways.
- `pnpm install:phone` picks the connected iPhone rather than a simulator, which Xcode 27 lists as paired too.

## 1.9.0 - 2026-09-26

### Added

- Settings → Session defaults says when droid has updated itself since the Daemon started, with a Restart Daemon button. Until the restart the Daemon keeps the earlier droid and its model list, so newly released models were missing from Session defaults and the New session page although the chat already offered them.

### Changed

- Every model setting in Session defaults (default model, spec mode, compaction, the per-model compaction limits and the subagent task models) uses the chat's model picker, with brand filters, search, favourites and usage multipliers. "Same as main", "Inherit" and "Current model" stay pinned at the top of the list.

## 1.8.2 - 2026-09-24

### Changed

- Inline code in replies on the iPhone is monospaced and tinted, without a grey box behind it. The iPhone cannot round or pad that box as the desktop does, so it only hugged the letters.

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
