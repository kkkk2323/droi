## 1.85.0 - 2026-10-31

### Added

- Droid's queued questions (droid 1.1) sit above the composer, which stays usable: you can keep typing, and Escape still stops the turn. A queued question counts as needing input only once Droid rests, and its answers show in the transcript as one card. You can fold a question or dismiss it.
- The background tasks list has Stop for each command droid runs in the background (droid 1.0 or newer), also for a Session on another computer. The list also shows a command whose start is older than the messages Droi loaded.
- While droid retries a model request, the session's activity row says "Retrying the model request (attempt N)…", so a retry no longer looks like a hang. This happens, for example, when the Key Pool moves to another key.
- A Session started from an approved spec shows the plan as a folded "Approved plan" card instead of the long first message.

### Changed

- Droi's Memory Server is now named `memory`. Sessions from before still show their `droi-memory` calls. A `memory` server in your own `mcp.json` stays yours.
- When Droi stops or restarts the Daemon, it waits up to 12 seconds (before, 3) so that droid can save its work. On Windows Droi asks the Daemon to stop instead of ending it at once.
- A custom model whose id names no company shows the brand of the model it behaves like.
- The Phone App runs on Factory's droid SDK 0.10.0. Opening a Session on the phone no longer cancels a permission request that the phone cannot read: the request waits for a Client that can answer it.

### Fixed

- After the connection to the Daemon drops, a queued message is no longer lost or sent twice. Droi keeps it and checks the Daemon's queue when it connects again. Messages show in the order the Daemon stored them.
- A WaitForScript row says "stopped" when the model stopped a Script and "checked" when it only checked on one. Before, it always said "continued".
- A subagent's card shows that it ended as soon as droid 1.1 says so in the parent Session.
## 1.84.0 - 2026-10-30

### Changed

- The Key Pool runs Factory's own droid, unchanged, instead of a patched one (ADR 0033). Droi downloads Factory's newest droid (or copies yours when it is the same build), checks on this computer that its key hook works in it, and starts the Daemon with the hook. A new droid works the day Factory ships it: there are no recipes to wait for. If you used the Key Pool before, Droi moves to the new droid on its own the first time it starts.
- The Usage tab knows each key's Factory plan. A key shows Pro, Plus, Max or Ultra next to it, and a Plus key's quota counts as 5 Pro keys', a Max key's as 10 and an Ultra key's as 100: in the totals, the averages, the credits, how long the pool lasts and how many waiting keys start a day.

### Fixed

- A model request can no longer be billed to your signed-in account while the Key Pool is on. If the key hook did not load, the Daemon stops and says why, and the Key Pool's proxy refuses any model request that does not carry one of its keys.
- When Factory rejects a Key Pool key, droid no longer says that your login has expired. In Automatic mode Droi moves to another key and the request is retried; in Manual mode the session says which key was rejected.
- On Windows, switching keys while a session runs no longer fails now and then with "Access is denied".

## 1.83.0 - 2026-10-30

### Added

- An app built on Droi can show a shorter name in the sidebar's top row than its full name (ADR 0029). The window title, the menus and the texts keep the full name. Droi itself looks as before.

## 1.82.0 - 2026-10-30

### Added

- An app built on Droi can give single tools of its MCP servers an autonomy level of their own, over the server's (ADR 0029): a server whose tools run without asking can still have a Session ask before each call of one of them, such as a tool that runs a deploy. Droi itself looks and works as before.
## 1.81.1 - 2026-10-30

### Fixed

- Opening an archived Session no longer shows "droid: session not found", including a Memory Session opened from Settings → Memory. droid 1.0 refuses to load an archived Session, so Droi now unarchives it, loads it and archives it again: the Session opens and stays archived.
- A Memory Session no longer runs a skill that its material names. When a Session or the Memory Summary mentioned a skill as `/name`, such as `/retro`, droid 1.0 activated that skill in the Memory Session and told the model to follow it.
## 1.81.0 - 2026-10-30

### Changed

- Droi runs on MyGo 0.7.3. On Windows, a window that is hidden or off screen no longer holds the app up for a second on each frame, and resizing a window no longer shows its content magnified and cut off. On macOS, an update keeps a single Dock icon, Cmd-Q always quits, and a dialog opened by a click shows at once. Spinners and pulses redraw only themselves.
- Droi's own requests follow the proxy set in the system's settings, PAC files included, as a browser does: downloads of droid and the Patched Droid's recipes, the Factory sign-in and usage, the Key Pool's usage reads and the Patched Droid's model requests. Before, only `HTTP_PROXY` and `HTTPS_PROXY` reached them. A proxy set in those variables still wins.
- The Autonomy select and every select in Settings open as menus. Up and Down move through the options, Enter chooses one, Escape closes the menu, and typing a letter jumps to an option. A dot marks the current choice.
- The pulses move 30 times a second. On a Mac with a ProMotion display, the display then refreshes less often while nothing else moves.
- The release's files have new names: `droi-<version>.dmg` for macOS and `droi-setup-<version>-amd64.exe` for Windows. Installed apps update as before.
## 1.80.0 - 2026-10-30

### Changed

- The model picker opens on your favorites when you have starred models the Daemon offers. Click the star in the picker's rail to see every model. A Session on a Magpie model still opens on the Magpie tab.

### Fixed

- The sidebar's search no longer lists Droi's own Memory Sessions, such as "Memory: update the Memory Summary". The Daemon's search found them in their messages.
- The search box's icons sit in the middle of the box at both densities. At Comfortable they were 2 pixels too high.
## 1.79.0 - 2026-10-30

### Added

- An app built on Droi can preset what new Sessions start with: Session Defaults such as the model, the reasoning effort and the compaction limit, and the tool mode (ADR 0029). The window gives them to the computer's Daemon the first time it connects, once for each version of the presets, so what you change in Settings afterwards stays. Droi itself sets none.

### Fixed

- With no Workspace at all, as on a new install or an app built on Droi with a Factory home of its own, the New session page starts without a project instead of waiting for one to be chosen, so you can type at once.

## 1.78.0 - 2026-10-30

### Added

- An app built on Droi can start background work of its own once the Host has started, in the window and in `<app> host` alike, such as keeping the Factory API key it handed the Host current. It can download the Patched Droid's recipes and Factory's droid builds from a mirror of its own, for networks where GitHub or Factory's downloads are slow; the recipes are still checked against Droi's key. An app that puts keys of its own in the Key Pool can find them again by the id the pool gives them (ADR 0029). Droi itself looks and works as before.

## 1.77.0 - 2026-10-29

### Added

- The sidebar can list Sessions by time. Sort → View → By time shows one list: the Sessions that are working or have a result you have not seen under Right now, then Today, Yesterday and Earlier, each row naming its Workspace. By workspace stays the default. The Phone App lists the Sessions the way the computer's window does.

### Changed

- The list dates a Session by the last message you sent it, from the window or the phone, instead of by the time its file last changed. Opening an old Session, a hook run or a step of a turn no longer moves it, Loops and Automation runs do not move it either, and a Session keeps its place after a restart. The first time it starts, Droi reads from each transcript when you last wrote in it, in the background: about 5,000 Sessions take a few seconds (ADR 0032).
- Inside a group, a Session that is working or waits for an answer comes first, then one with a result you have not seen, then the rest by when you last wrote in them. An old Session with news no longer hides behind "Show older" or among the older Workspaces.
- Unread marks are the computer's, so every Client shows the same ones: a Session you read in the window is read on the phone, and the other way round.
- While the pointer is over the sidebar's list, the rows stay where they are, so a row cannot move just before you click it. They take their new places once the pointer leaves the list.

## 1.76.1 - 2026-10-28

### Fixed

- The Key Pool moves droid onto a key whose Standard month ends within 7 days. Before, droid stayed on the key it was using until one of that key's windows was full, so on a light day another key's month could end with quota left. On one computer, 30.1M credits were at risk this way. Droid now changes to the key whose month ends first, as long as that key's 5-hour window is not nearly full. Otherwise it keeps its key as before.
- Settings → Usage → Expiring counts only the keys it lists in "Use at least … a day to lose none". The figure used to cover every running month up to 30 days out. It showed 64.9M a day next to a single key that needed 8.26M a day.
- When updating the Patched Droid restarts the Daemon, the Memory Summary tries again on the next pass instead of waiting an hour, and a Session Summary that was cut off does not count as a failure.

## 1.76.0 - 2026-10-27

### Changed

- Memory looks after itself. Droi now summarizes each Session once it has been left for six hours and folds those summaries into one short Memory Summary, under 10,000 bytes, that every new Session starts with: who you are, how you like to work, and pointers to recent work by project. Droid saves something itself only when you ask it to remember, forget or correct it. There is nothing to consolidate, no size limit that refuses a write, and no corner card asking you to tidy up (ADR 0031).
- The first time it runs, Memory brings in what the earlier Memory kept: your corrections and preferences as notes that stay until you remove them, and everything else as summaries that age out unless a Session reads them. `memory.sqlite` is left as it was, so an older Droi still finds it.
- Settings → Memory has two models, one that summarizes Sessions and one that keeps the Memory Summary. Both default to Claude Haiku 5.5 instead of GLM 5.3 Flash: compared on real Sessions with Gemini 3.8 Flash, DeepSeek V4.1 Flash and GPT-6 Luna, it kept the most of what you said, and it costs less than GLM 5.3 Flash. It shows the Memory Summary's size and when it was updated, how many Sessions wait, and the last failure, and Update now takes the Sessions left for ten minutes. It lists the latest Memory Sessions, and each opens like any other Session.

### Removed

- Project and Global Memory, topics, categories, the consolidate buttons and Memory's corner cards. Droid's `memory_add`, `memory_replace` and `memory_remove` give way to `memory_note`, `memory_read`, `memory_list` and `memory_search`.

### Fixed

- A Session the Daemon cleaned up after it sat idle keeps its speed and context meters under the composer. They went away until the next message.
- Sending a message to a Session the Daemon cleaned up no longer plays the completion sound before the message goes out. Droi now uses droid-sdk-go 0.3.1.
- The AskUser and permission cards, which stand where the composer was, have the composer's white instead of a faint grey.

## 1.75.0 - 2026-10-26

### Added

- An app built on Droi can sign its people in its own way, such as with the company's single sign-on: its own page takes the place of "Sign in with Factory" until it hands the Host a Factory API key. It can also run the Daemon on a Factory home of its own, so a droid CLI signed in on the same computer does not take the place of that key and the app's Sessions and settings stay apart, and it can leave Droi's Settings sections out, such as Account (ADR 0029). Droi itself looks and works as before.

## 1.74.0 - 2026-10-26

### Added

- Global Memory has topics. A fact about one subject outside any project, such as a service you run, a client, a home network or a hobby, is filed under a topic: Droid finds it by search instead of being shown it in every Session, and every Session starts with one line naming the topics. The first time Memory work runs after this update, Droi sorts the Global Memory into topics once, after a backup of the database, without changing or removing an entry, and a corner card says so. Settings → Memory shows the core, its corrections against their budget, and the largest topics.

### Changed

- A Session is shown the corrections of the Global Memory's core within 3,000 characters of their own, and the Project Memory's within their own 2,000, instead of 2,000 shared. On one heavy user's Memory, all 13 corrections of the core now reach a Session, where 8 of 23 did before.
- Consolidating the Global Memory looks at its core with every category together, then at each topic, so the same fact saved as a preference and as a correction becomes one entry. It never moves a correction or a preference to a project, and moves another entry only to a project the entry names.
- A chat without a project can save what it is about, under a topic.
- Memory Sessions ask the model for low reasoning effort, so a model that defaults to high, such as Gemini 3.8 Flash, answers in seconds instead of thinking for minutes.
## 1.73.4 - 2026-10-26

### Changed

- An automation's page shows only the first four lines of a long prompt, so its runs stay in view. Show full prompt opens the rest, and Show less folds it again.
- The Phone App rounds its corners as the desktop app does: the composer's card is 18px again, cards, bubbles and pictures 12, a tool's detail 10 and a control 8, and every border is a full pixel instead of a mix of hairlines. Its text sizes and touch targets are unchanged.

## 1.73.3 - 2026-10-26

### Fixed

- On some Macs, letters drawn next to a small rounded shape lost pieces, as the composer's MCP label beside its status dot did. Droi now uses MyGo 0.6.4, whose Metal renderer fixes this.
- On macOS, Droi no longer starts the AutoFill helper process, which used CPU and could make scrolling stall.
- In Chinese text, each character counts as its own word: deleting a word, moving by words and double-click selection in the composer no longer take a whole clause.
- On Windows, short labels, such as a four-character Chinese button, are no longer cut off with an ellipsis.

## 1.73.2 - 2026-10-26

### Fixed

- Settings → Usage → Observability estimates Credits for every model the requests went to, not only Opus 5.5, Fable 5.1, Sonnet 5.5 and Haiku 4.5: DeepSeek, GLM, GPT-6, Opus 5, Haiku 5.5, Kimi K3, Gemini, Qwen and Muse Spark have Factory's weights now, so the 4813 calls of the last 30 days that showed as Unpriced have a number. Only image models, which have no chat price, stay unpriced.
- Credits count a Sonnet 5.5 cache read at Factory's current price: its cache-read multiplier is 0.05 of an input token, not 0.1, so its credits fall by about 11% and match the CLI's own count.

## 1.73.1 - 2026-10-25

### Changed

- The reasoning effort names stay in English in every language: Off, Low, Medium, High, Extra high and Max. They are the Daemon's own names for the levels, and the Chinese app showed them translated.

## 1.73.0 - 2026-10-24

### Added

- Another app can now be built on Droi: the same window and Host under its own name, port and mark, with MCP servers and hooks of its own for the Daemon, words added to the System Prompt, MCP servers inside its own executable, pages in the sidebar under Automations, Settings sections, and its own way to show a tool's results or a json-render element. It runs Droi's `droiapp.Run` with its brand and extensions, and takes each Droi release with a new tag of the Go module (ADR 0029). Droi itself looks and works as before.

## 1.72.1 - 2026-10-24

### Fixed

- On macOS, Droi keeps Full Disk Access and the other permissions macOS grants it after an update. Every release is now signed with the same certificate, so macOS sees each version as the same app; before, each update was a new app to macOS and lost them. Grant them once more after updating to this version, and they stay from then on.

## 1.72.0 - 2026-10-24
### Added

- Settings → Session defaults → Tool calls has Script models. Factory lists the models whose Sessions may use Script, and a model it does not list yet, such as DeepSeek V4.1 Flash 1017, silently ran in Direct whatever the Session asked for. A model added here can use Script too: with the Key Pool on, Droi adds it to the feature flags droid reads and leaves every other flag as Factory sent it. Restart the Daemon in Advanced to apply it to new Sessions.

## 1.71.0 - 2026-10-24

### Added

- With the Key Pool on, Settings → About has a row for Droi's droid. Check for updates looks for a newer Droi's droid at once, instead of waiting for the hourly check, and says whether it is up to date or why it could not check. Once a newer one is available, the button updates to it; updating restarts the Daemon, which interrupts running sessions.

### Changed

- The Key Pool group in Settings → Advanced no longer has the line under it about which requests use the Key Pool's keys.

## 1.70.1 - 2026-10-23

### Changed

- Settings → Usage → Observability: the trend and the heat map, and the chart of the last 14 days on Keys and quota, have an axis with round values and lines across, and the dates or hours sit under their bars. Pointing at a bar or a cell shows what it holds at once: the trend's card lists each model's part. A line above the trend says what it counts and in what unit; Speed is now Output speed, in token/s.
- The model and type filters above the request list, and the bordered buttons of Settings → Usage, have the same rounded corners and height as the tabs. Chinese text writes Credits with a capital C.

### Fixed

- In the breakdown, a second click on the model or type in force lists every request again. The row in force stays the one the list is filtered by when you switch the period or the grouping; before, it could highlight another model. The page no longer shows the last period's numbers when you switch periods quickly.
- A chosen row of the breakdown or the request list keeps its text readable, and after you close a request's details a click on the same row opens them again.
- The line beside the period says the number of requests in the whole log, not in the period.

## 1.70.0 - 2026-10-23

### Added

- Settings → Usage has an Observability tab in place of Requests. For today, the last 7 or 30 days, or everything, it shows how many model requests the Key Pool sent, how many failed or switched keys, the tokens with the share read from the cache, the credits they cost and the output speed. A chart shows them hour by hour or day by day, split among the five models used most; a heat map shows them by weekday and hour; and a table breaks them down by model, key, type (model request, cache warming, key start) or session. Below, the newest requests can be filtered by model, type or failure, and a click on one shows everything recorded about it. Credits are estimates and count only Opus 5.5, Fable 5.1, Sonnet 5.5 and Haiku 4.5; requests to other models are counted as unpriced. Sessions count only requests that name their session, which older records do not.

## 1.69.0 - 2026-10-23

### Added

- With the Key Pool on, Droi can start waiting keys on a schedule. A key's monthly quota counts from its first request, so a key you add waits until a request uses it. Turn on Settings → Usage → Waiting keys → Start on a schedule, and from 8:00 each day Droi starts up to two waiting keys with a one-token Haiku request, also on days you send none. The section shows how many keys started today and how many still wait, and the event log lists each start. It is off by default; off, a waiting key starts on the next request, as before.

### Changed

- Some Chinese text about the model speed and the Key Pool is shorter and reads more naturally.

## 1.68.1 - 2026-10-23

### Fixed

- With the Key Pool on, the speed under the message box now works for Gemini models too. Before, the Key Pool could not read Gemini's replies: their tokens, time to the first token and speed were missing, and the speed stayed on an old estimate.
- The live speed waits a second after the first token before it shows. Before, the first half second often showed about twice the real speed.

## 1.68.0 - 2026-10-23

### Added

- With the Key Pool on, a ring beside Settings in the sidebar shows how much of the monthly quota the keys have left: green, amber under 30% and red under 10%. Click it to open Settings → Usage.

### Changed

- With the Key Pool on, the speed under the message box shows what the Key Pool measured on each model call, and it updates every half second while the model writes. Click it to see the call under way, the last call and the whole session: the model and key, time to the first token, speed, input tokens with the share read from the prompt cache, output tokens with the reasoning, cache written, and time. An Open Usage link at the bottom opens Settings → Usage. Without the Key Pool, it shows what it showed before.

## 1.67.1 - 2026-10-23

### Fixed

- Clicking the sidebar's search right after you open a session no longer loses the focus to the message box, so what you type goes into the search.

## 1.67.0 - 2026-10-23

### Added

- The Key Pool's quota can show in the menu bar (on Windows, in the notification area): a gauge with the share of quota left, and a click opens a panel with each key's quota, today's use and how long the keys last. Turn it on with Settings → Advanced → Show usage in the menu bar. It is off by default and shows only while the Key Pool is on.

### Changed

- Settings → Usage: the tabs that are not selected are darker, so they read as tabs; the event log names its events in words; and the Today line no longer overlaps its note in a narrow window.

## 1.66.0 - 2026-10-23
### Added

- The Key Pool: model requests can use Factory API keys you add instead of your signed-in account, while every other request keeps using the account. Turn it on with Settings → Advanced → Use the Key Pool. The first time, Droi downloads Droi's droid (about 300 MB), a copy of droid that only it runs; the droid you run yourself is not changed. Turning the Key Pool on or off restarts the background service, which interrupts running sessions. It works on macOS and 64-bit Windows, and not while a Factory API base URL is set.
- While the Key Pool is on, Settings has a Usage tab. It lists the keys with the quota each has left, adds and removes keys, and switches between them, either on its own (when a key runs out, the next request uses another) or by hand. It also shows how much quota the keys use each day, how long they last, and the model requests sent through the pool.
- Droi's droid does not update itself. When a newer one is available, the card in the corner offers to update it.

### Changed

- The update card's buttons are smaller: Update and Details sit side by side, Skip this version is a quiet link below them, and a line under the title says which version is installed.

## 1.65.0 - 2026-10-23

### Changed

- The Phone App's Session list follows the computer it is connected to. It shows the computer's pinned Sessions and Workspaces under Pinned, then the rest of the Workspaces in the order the computer's sidebar shows them (the ones unused for a month behind Show N older workspaces), and Recents last; Automation runs are left out, as on the computer. Pinning or unpinning on the phone pins or unpins on the computer too. Before, the phone kept its own pins and ordered the list on its own, without a Pinned section.
- The Phone App's Session list is on a white page instead of a grey one, with a borderless search box, folder icons on the Workspace headers, and each Session's message count and how long ago it changed on a second line, as in the computer's sidebar.

## 1.64.2 - 2026-10-22

### Changed

- Droi asks before it updates. When it finds a new version, the card in the corner offers Update, Details and Skip this version, and nothing downloads until you choose Update; once the update is installed, Droi waits for you to click Restart now. Before, it downloaded every update on its own and restarted into it while you were away. To have it do that again, turn on Settings → About → Install updates automatically. Versions before this one still install it on their own.

## 1.64.1 - 2026-10-22

### Changed

- The open session in the sidebar is a grey row again, a shade darker than in 1.63, instead of the white row with a shadow 1.64.0 gave it, which stood out too much. A row under the pointer is a lighter grey, so the two read apart.

## 1.64.0 - 2026-10-22

### Added

- The sidebar's search finds workspaces too. Workspaces whose name has the words you type are listed first, with their sessions, as the sidebar shows them; the sessions the search found follow. Before, it found only sessions.

### Changed

- The sidebar is easier to read. Workspace names are dark instead of grey and a little larger, section titles such as "Workspaces" are no longer faded, and the sidebar is a lighter grey. The open session is a white row with a soft shadow instead of a faint grey one.
- The Reasoning and Tools choices in the model picker, and the choices in Automations' schedule, show clearly which one is picked: the picked one is a white face with a shadow on a darker groove. Before, the two were nearly the same colour.

## 1.63.2 - 2026-10-22

### Fixed

- Signing in with a Factory account that has an organization works even when the browser sign-in hands Droi a login without it. Before, Factory and the background service turned such a login down ("User not affiliated with an organization"), Settings said usage was unavailable with status 401, and signing out and in again did not help. Droi now asks Factory for the account's organization and signs in to it, as droid does; adding an account does the same.
- Droi records the account's Factory organization for droid where droid expects it. Before, it recorded the sign-in service's id there, and Factory answered usage requests that carried it with 403.
- When the background service turns the login down, Droi waits before trying again, a second at first and up to half a minute. Before, it tried thousands of times a second, and droid's log grew by about 140 MB a minute.

## 1.63.1 - 2026-10-22

### Changed

- Simplified Chinese and English wording in Settings, Memory, Automations, MCP servers and connection notices is shorter and more direct, and uses consistent terms across the desktop and Phone apps. On the New session page and in Hourly automations, the Chinese lines around the workspace and minute pickers read as one sentence again.

## 1.63.0 - 2026-10-22

### Changed

- Opening another signed-in account's session opens it under the account in use, with no switch. Droi moves the session, with its subagents and the sessions before and after its compactions, to the account in use; opened later under the first account, it moves back. droid opens a session only under the organization it belongs to, so a moved session belongs to the new one, and with cloud session sync on its later turns sync there. When a session cannot be moved, for example because its transcript is not on this computer, Droi says why and offers the switch as before.

### Fixed

- On the New session page, a command or skill picked from the `/` menu shows as a tag in front of the message, as in a session. Before, it stayed plain text in the message.
## 1.62.2 - 2026-10-21

### Changed

- While Droid thinks, the activity row shows five bars rising and falling in a wave. Before, it was an orbit turning around a dot, which looked like the spinners of a running tool.
## 1.62.1 - 2026-10-21

### Changed

- The switch between computers is now the Droi row at the top of the sidebar. It names the computer the window shows; click it to switch or to manage computers. Before, it sat in the title bar beside the sidebar button. It is in the sidebar of a narrow window too.

### Fixed

- Settings' list of sections is as wide as the sidebar, so opening Settings and going back no longer changes its width. Before, the two differed when Text size or zoom was not the default.
- The spinners on screen turn together. Before, each started its turn when it appeared, so a running tool, its group and the activity row pointed different ways.
## 1.62.0 - 2026-10-21

### Changed

- Droi now runs on MyGo 0.6. The window redraws only what changed and lays out again only what moved, so a Session at work costs less to draw, and on a Mac a window that is hidden or covered draws nothing. The mouse wheel scrolls smoothly.
- The activity row under a conversation shows at a glance what Droid is doing: an orbit while it thinks, dots that light up in turn while it answers, a turning arc while a tool runs, a ripple while it waits for your approval and folding arcs while it compacts. The other spinners, and the waits for the Daemon, another computer and the browser sign-in, use MyGo's loading indicators too.

### Fixed

- On Windows, the menu shortcuts work in the window. Before, Ctrl+N, Ctrl+,, Ctrl+B, Ctrl+J and zoom did nothing.
- On Windows, opening Droi while it already runs brings its window to the front.
- Starting a Session from New session no longer opens a second, unused draft Session and Scratch folder just before the Session shows.

## 1.61.1 - 2026-10-21

### Fixed

- The choices on Droid's question card look like controls again. Each option has a border, a radio button or a check box in front, and turns blue once picked; the field for your own answer has a border too. Before, they were white on the white card and read as plain text.
- Other boxes that had blended into the white since 1.60 stand out again: a permission's command, a Script's calls, the question counter, the note field on a Spec, the facts in the worktree dialog and the note on another account's session. "Keep planning" on a Spec is a framed button.

## 1.61.0 - 2026-10-21

### Added

- Settings → General → Density. Comfortable, the default, brings back 1.59's sizes: 14px text, a 15px conversation and 32px rows and buttons. Compact keeps 1.60's smaller ones, to fit more on the screen. Text size still scales either.

### Changed

- Text, borders and fields are easier to read again. Text is darker, borders and the edges of fields, the composer, the todo bar and menus are about as strong as in 1.59, and inline code has its colour back, in Light, Dark and Solarized Light+. On the phone too.

### Fixed

- Holding ⌘ no longer moves the Sessions in the sidebar: the ⌘1…⌘9 numbers take the place of the times without making a row taller.

## 1.60.0 - 2026-10-21

### Changed

- Droi now follows [Synara](https://github.com/Emanuele-web04/synara)'s workbench layout: a grey navigation rail beside a white reading surface, charcoal surfaces in Dark, a compact sidebar with clearer Workspace and Session hierarchy, and an inset composer. New session groups its question and input in the centre. Settings uses flat groups with subtle separators instead of filled cards. The Phone App carries the same surface hierarchy, grouped lists and inset composer while keeping its 15px body text and touch targets.
- The interface is set in the system's own font (San Francisco on a Mac, Segoe UI on Windows) and code in JetBrains Mono, which Droi now bundles. Users who had never picked a font move from Geist to the system face; Settings → Font still offers Geist. On the phone too.
- Solarized Light+ keeps its palette but is drawn in the new material: the cream page, the parchment sidebar, base01 text and Solarized's blue on the switches.
- Magpie's models have a tab of their own in the model picker, between Favorites and the brands. They share their names with the models the Daemon already offers, so the list the picker opens on, the brands' tabs and Favorites now show the Daemon's own models only. The search still reaches both lists, and a starred Magpie model stays in Favorites.

### Fixed

- A picture sent to a Magpie model that takes images now reaches the model. Before, droid dropped it with "this model does not support image inputs", because the Runtime Overlay left `noImageSupport` off and droid reads a custom model without that field as one that cannot take images.

## 1.59.0 - 2026-10-20

### Added

- Magpie models: with [Magpie](https://usemagpie.ai) running on this computer, turn on "Offer Magpie's models" in Settings → Advanced, and its models join the model picker, grouped by the company behind each model. Droi hands them to this computer's Daemon only; the `droid` CLI's settings are not changed, so the CLI and the Factory App keep their own models. The section shows whether the gateway answers and how many models it has. "Refresh" reads the list again and restarts the Daemon only when the models changed. The gateway's address defaults to `http://127.0.0.1:3425`, or `MAGPIE_BASE_URL`.
## 1.58.1 - 2026-10-20

### Fixed

- Background tasks count only the commands Droid left running in the background. A Script that is still going is no longer counted in the top-right corner or the sidebar; its row says "Still running" until it ends.
## 1.58.0 - 2026-10-20

### Added

- Background tasks show while they run. When Droid starts a command in the background, or a Script keeps running after its first answer, the top-right corner of the session counts them. Click the count to see each task and how long it has run. A command opens to the last 20 lines of its output while the Daemon runs on this computer.
- A background task's tool row says it is running in the background, or how it ended ("Finished", or the exit code when it failed) and how long it took. The exit code shows once the session is opened again.
- The sidebar marks a session that is idle but still has background tasks running, such as "1 background task running".
## 1.57.2 - 2026-10-20

### Changed

- Settings shows each account's usage as a small ring per window (5 hours, this week, this month) with the share left; hovering a ring says when it resets. "Details" opens each window's bar, share left and reset time.
- Core usage, once an account draws on it, shows its 5-hour, weekly and monthly windows too, instead of only its fullest one.

### Fixed

- Switching to an account of another organization no longer empties the sidebar of the other account's sessions. droid lists and opens only the sessions of the account in use, so Droi keeps each account's list and shows the others' sessions beside the live ones, muted and marked with their account. Opening one offers to switch to that account (asking first while sessions work) and opens it once the switch is done.

## 1.57.1 - 2026-10-20

### Changed

- Settings shows each account's usage as one line per window (5 hours, this week, this month): a bar of what is left, the share left, and when it resets, such as "Resets in 2h 15m" or "Resets in 3 days (Oct 24)". A window that has not started says "Not started". Core usage shows as a line of its own once the account uses it.
- An account's plan shows as a small badge beside its name. The renewal date is gone; a plan's end shows only within a week of it, or once it has ended.
- The account you share with droid in the terminal no longer says "droid CLI". With more than one account it says "Shared with droid in the terminal", and an organization shows only when two accounts have the same email.
- Settings' sections are grouped: Personal, Sessions, Devices and System. "Added to the system prompt" and "Scratch folder" moved from Advanced to Session defaults.
- In Chinese, Droi says 后台服务 instead of 守护进程 and 压缩上下文 instead of Compact, and several Settings descriptions are shorter and plainer.

### Fixed

- A Session default picked in Settings no longer jumps back to its old value now and then, which made picking that old value again do nothing.

## 1.57.0 - 2026-10-20

### Added

- Spec mode: Droid plans with you before it changes anything. Turn on "Spec" beside the message box, or on the New session page before you start. When the plan is ready, it shows in the conversation, and a review card takes the place of the message box: add a note for Droid, pick the autonomy the work runs at, then "Proceed", or "Keep planning" to send the plan back (your note then goes to Droid as your next message). With "New session" on, the work starts in a new session with the plan, and Droi opens it. Plans in earlier sessions show the same way, with how they were answered.
- Mermaid flowcharts and sequence diagrams in Droid's replies and plans are drawn as diagrams, in the app's colors. "Show source" shows the code, and a wide diagram scrolls sideways. Other diagram types, and a diagram with a mistake in it, stay as code.
- A long paste, of 25 lines or 4,000 characters, waits as a card next to the images instead of going into the message box, and goes out after your message. In the conversation it shows as the same card, which opens on a click. A card can go back into the message box unless one of its lines is over 10,000 characters. Drafts keep the cards, and editing a queued message brings them back.

### Fixed

- Pasting a long log or JSON no longer hangs the window. Before, a paste of 100 KB on one line took almost a second, and so did every key pressed after it.

## 1.56.13 - 2026-10-20

### Added

- Settings shows when each account's plan renews or ends: each account row says "<plan> · Renews <date>" or "Ends <date>". Within a week of the end it also shows the days left, and a plan that has ended is shown in red.

### Changed

- A permission request's answers are now one row of buttons, like a question's: "No, cancel" on the left, and on the right "Yes, and always allow …" and "Yes, allow", the main button. Before, each answer filled a row of its own.

### Fixed

- The "Both" tool mode is gone from the native app and the Phone App, because droid no longer accepts it: a session started with Both failed with "Invalid request params". Direct and Script remain. A saved Both default now reads as "Follow droid settings".
## 1.56.12 - 2026-10-20

### Added

- A permission request can be folded: click the top line of its card to hide the command and the buttons and read the conversation above it, and click it again to show them.

### Fixed

- A permission request with a long command, such as a long shell script, no longer pushes its buttons below the window. The command scrolls in its own box, so "Yes, allow" and "No, cancel" stay in reach.
## 1.56.11 - 2026-10-18

### Fixed

- On the New session page without a project ("Don't work in a project"), "/" now offers your personal and built-in skills, as it does in a project. Before, it offered only /loop. The session you start keeps the new folder the page made for it; if you leave the page without sending, the empty folder is removed.

## 1.56.10 - 2026-10-18

### Added

- An image in Droid's reply, such as a screenshot it took, can be clicked to see it enlarged, as an image attached to a message can. A click or Esc closes it.

## 1.56.9 - 2026-10-17

### Added

- A running subagent's card shows what it is doing: the tool it uses right now, such as "Read src/strings.ts", and how many tools it has used so far. The card also shows the subagent's model and, when Droid asked for one, the task size (light, medium or heavy).
- A running subagent can be stopped from its card, or from the subagents menu at the top of the session, with the small square button. Stopping the session that started it does not stop a subagent that runs in the background, so this is the way to stop one.
- TaskOutput and TaskStop lines in a session name the subagent they are about, and their arrow opens it.

### Fixed

- When Droid gives a subagent more work (resume), the new card reads "Resume Worker" (or the subagent's name), shows the run and opens its session from the start. The card of the earlier run stays done instead of showing it running again.
- Droi works out a session's subagent cards, and the subagents it has heard of before they are listed, only when they change, not on every frame it draws.

## 1.56.8 - 2026-10-16

### Added

- A question from Droid can be folded: click the top line of its card to hide the answers and read the conversation above it, and click it again to show them. What you picked stays picked.
- A session you left with an unsent message shows "Draft" with a pencil in the sidebar, until you send it. The session you are looking at does not, as its message box shows the text.

### Changed

- Every queued message can now be edited, not only one paused after Esc. The pencil takes it out of the queue and puts its text and images back in the message box. If the message went out in the meantime, it stays sent and is not put back, so it is never sent twice.

## 1.56.7 - 2026-10-16

### Changed

- The status colors (working blue, success green, attention orange, error red, and the added and removed lines of a diff) now stay within sRGB. On a Mac's own screen they are a touch less vivid, by less than most eyes can tell, and the window takes about half the graphics memory: about 50 MB less for a large window.

### Fixed

- Droi uses much less memory and some less CPU while a session works. It no longer reads every message of the open session again each time one changes, only the ones that changed. With a long working session open, Droi went from about 18 MB to about 7 MB of new memory a second, and from 57 to 25 memory cleanups a minute.
- With a pinned session, the sidebar no longer goes through every one of your sessions each time it is drawn, and Droi no longer copies the whole session list each time the window is drawn.
- On the Automations page, after Run now, Pause or Resume, the page shows the new state right away. Before, it could keep showing the old one until its next refresh.

## 1.56.6 - 2026-10-16

### Changed

- Droi now runs on MyGo 0.5.5.

### Fixed

- Droi uses about half as much CPU while a session works. Spinners, pulsing dots, the typing cursor and the bouncing dots now only paint the window again as they move, without working out the whole window each time, and they stop asking for frames when they are scrolled out of view. On a Mac with 4751 sessions and a working session open, Droi went from about 33% to about 17% of one core.

## 1.56.5 - 2026-10-16

### Fixed

- Droi uses less CPU while a session works with that session open. Its spinners, pulses and bouncing dots now move together at 24 frames a second, where before they drew about 38. Droi also no longer works out a session's subagents, or reads your sidebar settings, again on every frame. On a Mac with 4751 sessions, Droi went from about 43% to about 33% of one core while a session worked.

## 1.56.4 - 2026-10-16

### Fixed

- Droi uses much less CPU while a session works when you have thousands of sessions. The sidebar no longer reads when each session was first seen, or copies the whole session list, on every frame it draws. On Windows, with 4751 sessions, Droi went from about 15% to about 8% of one core while a session worked.

## 1.56.3 - 2026-10-15

### Fixed

- In Settings, Session defaults, the spec mode model, the summary model and the Subagent task models are picked in the same model picker as the default model, with search, brands and favorites. Its first row keeps the choice of no model of their own: Same as main, Current model or Inherit.

## 1.56.2 - 2026-10-15

### Fixed

- Typing `/` on the New session page offers `/loop`, also without a project. A `/loop` that cannot run stays on the page with the reason, instead of starting an empty session.

## 1.56.1 - 2026-10-15

### Fixed

- A session whose loops were deleted or paused while Droi was not connected to Droid lets its Droid process close again once Droi reconnects. Before, Droi kept it running until Droid restarted.
- While Droi shows another computer, it no longer keeps that computer's sessions running for their loops, because that computer's Droid keeps running after Droi closes. Droi still starts those loops again when it connects. A loop 30 minutes or more apart there waits once its session has been idle for 30 minutes.

## 1.56.0 - 2026-10-15

### Added

- Loops: a prompt Droid sends to the same session again on an interval, as droid's `/loop`. A session's loops show above its composer, with how often each runs and when it runs next; you can pause, resume or delete each one. `/loop 10m check the build` (or `/loop check the build every 10m`) starts one, `/loop stop` deletes them. Loops run while Droi runs: Droi keeps the session ready for them, and starts them again after Droi or Droid restarts.

### Changed

- Global Memory keeps to facts about you that hold in every project. When Droid saves to Global Memory, Droi reminds it to save a project's facts in that project's Memory instead, and in a chat without a project to leave out what the chat is about.
- Consolidating Global Memory moves an entry about one project to that project's Memory and removes entries about a passing topic. Consolidating any Memory also removes one-off details, such as the result of one task.
- Droi's Memory prompts update themselves when you have not edited them. Before, a prompt copied into the Memory folder stayed as it was first copied.

## 1.55.0 - 2026-10-15

### Changed

- Nothing changes in the app. Its Host, Gateway and Session view became public Go packages under `apps/native/pkg`, for another app built on them, and each release now also tags the Go module (`apps/native/v<version>`).
- The Go SDK and the Fake Daemon moved to their own repository, [droid-sdk-go](https://github.com/kkkk2323/droid-sdk-go).

## 1.54.0 - 2026-10-15

### Changed

- Droi runs on MyGo 0.5.1, and its program is about a quarter smaller.
- Replies, reasoning, a subagent's report and the release notes are drawn by MyGo's Markdown view. A reply that is still streaming shows half-written formatting as what it will become, and its words fade in (not with Reduce Motion). Code blocks show their language and a Copy button, without line numbers or a Download button; a table wider than the column scrolls sideways; a picture shows at its own size, at most as wide as the column, and as its description when it cannot be read.
- In Chinese, MyGo's own menus and controls (Undo, Cut, Copy, …) are in Chinese too.
- Enter sends everything typed, even when it comes right after the last key.
- The **Open in** menu takes the apps' icons from the system, and shows them on Windows too.

## 1.53.0 - 2026-10-14

### Removed

- The Chat Bridge for WeCom (企业微信) is gone, with Settings › Chat Bridge, the `chatBridge` block of `droi host`'s settings (now ignored) and the robot mark on Chat Sessions. Sessions it started stay as ordinary Sessions. Remembered computers and switching between them are unchanged.

## 1.52.0 - 2026-10-14

### Changed

- In a WeCom Channel, once a post has a Chat Session, later replies under it reach Droid without an @ of the robot, as in a Slack thread Factory works in; only an @ starts a Chat Session. Droid stays quiet when a reply is not meant for it, and `!new` in a reply starts over. Replies from people not on the allowlist, edits and deletions send nothing.

## 1.51.0 - 2026-10-14

### Added

- Once Droi knows another computer, the top left of the sidebar names the computer the window shows. Click it to switch to this computer or any remembered one, or to open Settings › Computer.

## 1.50.0 - 2026-10-14

### Added

- Droi remembers every computer you followed. Settings › Computer lists this computer and each remembered one; one click switches to it, with no pairing link to paste again. "Use this computer" goes back without forgetting the others, and each one can be forgotten. When a remembered computer cannot be reached, for instance because its Pairing Token was reset, Droi offers to paste a new link, forget it or use this computer.

### Changed

- Switching to another computer, or back to this one, no longer restarts Droi: the window stays open and shows the other computer's Sessions at once. A computer followed with an earlier version is kept.

## 1.49.0 - 2026-10-14

### Added

- `droi host --port N` sets the port its Gateway listens on, 41417 by default, so it can run next to another Droi Host or in a container whose published port differs. The pairing link names that port.

## 1.48.0 - 2026-10-14

### Added

- The Chat Bridge lets your team hand Droid work from WeCom (企业微信). Connect a WeCom smart robot in Settings › Chat Bridge (Bot ID, Secret, Workspace, who may talk to it, Autonomy level and model), or in the `chatBridge` block of `droi host`'s settings file; it needs no public address. @ the robot in a WeCom Channel post, or write to it directly, and Droid works in a Chat Session for that post or conversation: it reads the post and its replies, searches the Channel, opens attachments, reacts with an emoji and replies, with files and @mentions, when it decides to. Permission requests show as a button card in a direct message and as numbered options in a Channel; the first answer wins, in WeCom or in Droi. Chat Sessions carry a mark in the sidebar and the Session header, so you can follow or take over one from the window or the phone. The native README explains how to create the robot and add it to a Channel.

### Fixed

- Opening a Session again no longer moves it to the top of the Session list.

## 1.47.0 - 2026-10-14

### Added

- `droi host` runs Droi without a window on Linux, for a server or a box you reach over the network. It starts the Daemon, the Gateway and Memory, prints the pairing link and a QR code, and signs in with that computer's `droid login` (kept in a file or in the system keyring) or with `FACTORY_API_KEY`. Each Release now has `droi-<version>-linux-amd64.tar.gz` and `-linux-arm64.tar.gz`; the native README explains the download, an example systemd unit and how to reset the Pairing Token.
- The window can follow another computer. In Settings › Computer, paste the pairing link of a computer that runs Droi or `droi host`; Droi checks it and starts again on that computer, showing its Sessions, Session defaults, skills, MCP Servers, Automations and Worktrees. While it follows another computer, this computer's Account, Memory, Remote Access and Advanced settings and the actions that need this computer's disk are hidden, only pictures can be attached, and pins, folded Workspaces and manual order are kept apart for each computer. This computer keeps running for paired phones and Automations. "Use this computer" in the same place switches back.

### Changed

- The window now reaches this computer's Daemon through Droi's own Gateway, as the Phone App does, whether Remote Access is on or off. Nothing needs setting up: paired phones stay paired, the Gateway keeps its port and the System Prompt Addition still applies once.

### Fixed

- Archiving a Session that works in a Scratch Workspace now moves its folder to the Trash, as Settings says, and unarchiving it brings the folder back. A folder still used by another Session that continues the same Scratch Workspace stays until the last of them is archived.

## 1.46.2 - 2026-10-14

### Fixed

- A running Automation now shows under "Running automations" in the sidebar within a few seconds of starting. Before, when the Daemon reported the run before it listed it, the sidebar missed the run until something else made Droi read the Session list again.

## 1.46.1 - 2026-10-13

### Fixed

- A subagent started in the background now shows the same state everywhere: in its Task card, in the header's subagent menu and in the sidebar. Before, the sidebar could list it as a Session of its own, or not count it as running, until the Session list was read again.

## 1.46.0 - 2026-10-13

### Added

- Typing "/" on the New session page now lists the project's skills and commands, as it does in a Session. Pick one with the arrow keys and Tab or Enter, then type the rest of the message.

### Fixed

- A subagent started in the background now counts in its Session's subagent menu at once, instead of only after the Session list was read again.
- In a subagent's Session, clicking the title no longer opens an empty menu when no other subagent of the same Session exists.

## 1.45.3 - 2026-10-13

### Fixed

- Dragging a picture out of an app that keeps its files to itself, such as WeChat, no longer shows a bare "operation not permitted" error. macOS does not let Droi open such a file, so the message box now says so and how to add the picture: save it to a folder such as Downloads first and add it from there, or give Droi Full Disk Access in System Settings › Privacy & Security.

## 1.45.2 - 2026-10-13

### Fixed

- Opening a Session you had not looked at for a while no longer leaves its message box greyed out until you switch to another Session and back. This happened when you had opened several other Sessions in between and the Daemon had stopped the Session while it was idle.

## 1.45.1 - 2026-10-13

### Fixed

- A Session left open for more than 30 minutes, for example while the screen was locked, no longer greys out its message box. The Daemon still stops an idle Session to free memory, but now the next message starts it again before sending, so you no longer need to switch to another Session and back.

## 1.45.0 - 2026-10-12

### Added

- Droi now needs droid 0.233.0 or newer. When the droid on this computer is older, Droi does not start the Daemon from it: the window says which version it needs and which one it found, and **Update droid** updates it and starts the Daemon. If droid cannot update itself (for example an npm install, or updates turned off by your organization), the window shows what droid said.

## 1.44.5 - 2026-10-12

### Fixed

- A subagent resumed for another task no longer shows as running after it finished, in the header's subagent menu and in the sidebar.

## 1.44.4 - 2026-10-12

### Fixed

- The composer keeps the keyboard focus when a pasted image, an error or the queued messages show above the text, when a picked command or skill is removed with Backspace, and after a permission or question is answered, so you can go on typing without clicking it again.
- Enter while an input method composes text, such as Pinyin before a candidate is chosen, no longer risks sending the message.

## 1.44.3 - 2026-10-11

### Fixed

- Picking a command or skill from the composer's suggestions keeps the keyboard focus in the composer, so you can go on typing without clicking it again.

## 1.44.2 - 2026-10-11

### Changed

- The Windows installer is now named `Droi Setup <version> amd64.exe`.
- Droi runs on MyGo 0.4.2. Tooltips show next to what they describe instead of at the pointer, and a notice such as **Copied the reply** goes away on time after a dialog closes.

## 1.44.1 - 2026-10-10

### Fixed

- A file dragged onto the composer from an app that hands macOS file references (`/.file/id=…`) instead of paths, such as a PDF, is added by its path instead of failing with **not a directory**.

## 1.44.0 - 2026-10-10

### Added

- A session whose subagents are running shows it in the sidebar: a spinner and **N subagents running** in place of its message count. A folded group counts it among its working sessions too.

### Fixed

- The sidebar's search finds sessions it missed. A title with the words shows at once, before the Daemon answers. The message text of the last 30 days is searched first, and every session only when that found fewer than 20: with thousands of sessions the Daemon reads only a few hundred of them, so a short query like a two-character Chinese word found nothing.

## 1.43.0 - 2026-10-10

### Added

- The session title has a **…** menu, shown when you point at the title. It renames, pins or unpins, copies the session ID or the session details, and archives the session, as the session's menu in the sidebar does. Rename moved there from the pencil.

## 1.42.0 - 2026-10-10

### Added

- Settings → Session defaults has the settings the Electron app had and the native app was missing:
  - **Limits for specific models**, under Compaction: give a model its own token limit, change it or remove it.
  - **Save folder**, under Spec mode: specs go to your home folder, the project's folder, or a folder you pick.
  - **Task models**, under Subagents: the model and reasoning level for Subagents started with light, medium or heavy complexity.

## 1.41.1 - 2026-10-09

### Changed

- The speed in the footer under the composer is the session's average, in tokens a second, as in DeepSeek Harness. The last model call's speed is in the details its click opens.

## 1.41.0 - 2026-10-09

### Added

- The footer under the composer shows how fast the model answered last, in tokens a second. Click it for the details: that call's wait for the first token and its output tokens, and for the whole session the number of model calls, the average wait and speed, and the time spent in the model and in tools. Droi times what it sees stream, and keeps the figures when you restart it, so a session's numbers start with its next reply.

## 1.40.0 - 2026-10-09

### Added

- The Workspace picker on the New session page shows each Workspace's path under its name. It lists your five most recent Workspaces; **Show N more** lists the rest, and the search looks through all of them.
- Workspaces with no session in the last 30 days wait behind **Show N older workspaces** at the end of the sidebar. Pinned Workspaces, and the one of the session you have open, always show.

### Fixed

- The sidebar and the Workspace picker list every Workspace again. Droi read only your newest 100 sessions, so a project you had not used for a while was missing until you clicked **Load older sessions**. Droi now reads all of them in the background, and that button is gone.
- Two Workspaces with the same folder name read apart: `~/dev/tmp` and `/private/tmp` show as "tmp (dev)" and "tmp (private)", in the sidebar, the picker and the New session question.

## 1.39.2 - 2026-10-08

### Changed

- The native app runs on MyGo 0.3.3. A click or a shortcut that comes right after an edit, before the window draws again, now sees the edit.

### Fixed

- In a reply's numbered list with blank lines between its items, each number sits on its item's first line again. Before, it sat a few pixels above the text; bullets and task boxes in such lists were off the same way.

## 1.39.1 - 2026-10-08

### Changed

- The native app runs on MyGo 0.3.0. A control kept from an earlier frame can no longer stand for another one: development builds and the view tests stop at such a use, and a release build ignores it.

### Fixed

- ⌘1…⌘9 (Ctrl+1…Ctrl+9 on Windows) open the sidebar's first Sessions again. The shortcuts sat on hidden menu items, and neither macOS nor Windows runs the shortcut of a hidden item, so the keys did nothing.

## 1.39.0 - 2026-10-08

### Added

- Settings → Account lists several Factory accounts. Add one with **Add account** (the same browser sign-in), then **Switch** to it; each keeps its own login, and all of them share your skills, droids, MCP servers, settings and sessions, so a Session started under one account goes on under another. Switching restarts the Daemon, and Droi asks first while a Session is working.
- Each account shows its Standard usage for the 5-hour, weekly and monthly windows, and the account in use says when it is close to its limit. Droi asks Factory through the same Factory API base URL as the Daemon.

### Fixed

- A stored Factory API key no longer reaches the Daemon while you are signed in. droid lets the key override the login, so the Daemon ran as the key's account.
- With `FACTORY_HOME_OVERRIDE` set, Droi and Memory read droid's folder where droid keeps it (`<override>/.factory`).

## 1.38.4 - 2026-10-08

### Fixed

- A file copied in the Finder and pasted into the composer goes in as its path, and a picture file is attached as the picture. Before, Droi attached the file's icon instead, so Droid saw an MP4 icon and not the video.
- A file dropped on the New session page that is not a picture goes in as its path. Before, Droi dropped it without a word.

## 1.38.3 - 2026-10-08

### Fixed

- An image Droid writes into its reply as a path on the computer, such as a screenshot it took, shows in the conversation again. Droi reads the file through the Daemon, so it shows on the phone too; while it loads a grey box holds its place, and a file that is not there shows its path. Before, the native app showed every such image as [image].

## 1.38.2 - 2026-10-08

### Fixed

- A Session whose view Droi dropped also leaves the Droid SDK's store, with the subagents it called, once nothing in it works or waits for an answer; opening it again loads it again. Before, every Session opened kept its messages until Droi quit.
- The composer and the Skills panel keep each skill's name and description only, not the whole skill the Daemon sends: with many skills, each open Session held several MB of skill bodies.
- Switching through 12 real Sessions, Droi's Go heap now stays at about 37 MB instead of 66 MB.

### Added

- `DROI_PPROF=127.0.0.1:6061` serves Go's profiles, to see what holds Droi's memory.

## 1.38.1 - 2026-10-08

### Fixed

- Droi no longer keeps every opened Session in memory until it quits: only the last 3 Sessions opened keep their transcript, parsed Markdown and decoded pictures, and a picture not drawn for a while is decoded again when it comes back. In a test, opening 6 Sessions with a screenshot each now stays at about 47 MB instead of growing to 109 MB.

## 1.38.0 - 2026-10-08

### Added

- A click on a picture in a conversation shows it enlarged; a click or Escape closes it.
- The sidebar's Automations row shows a spinner and a count while Automation runs work, and a Running automations group lists those runs' Sessions. The Automations page marks the running Automations and runs too.

### Fixed

- The model picker's rail of providers scrolls, so every provider can be reached.
- A subagent's Session that the Session list does not have yet still leads back to its caller in its header.
- A background Task's card shows Running, then Completed, as the subagent works, without opening its Session first.
- The Delete button of the delete-automation dialog is solid red, as the delete-worktree dialog's, and Reset pairing token's text is red instead of black.

## 1.37.1 - 2026-10-08

### Changed

- The Chinese UI keeps programming terms in English: Subagent, Script, Reasoning, Skill, Autonomy, Token, Prompt, Diff, Compact, and the modes Auto, Direct and Both.

## 1.37.0 - 2026-10-08

### Added

- Simplified Chinese. Droi follows this computer's language; Settings → General → Language picks English or 简体中文 instead.
- A drag selects across a reply's paragraphs, code blocks and lists, and copies them as one text, without list numbers or the code's line numbers.
- A right-click on a reply offers Copy, Select All, Copy as Markdown and Quote in message.
- The composers take images, links and text dropped from other apps, such as a picture dragged from a browser.
- Shortcuts (Ctrl on Windows): ⌘N for a new Session, ⌘, for Settings, ⌘B for the sidebar, ⌘J to scroll to the latest message, and ⌘1 to ⌘9 for the sidebar's first Sessions, which show their numbers while ⌘ is held. The menu bar lists them, and the buttons' tooltips tell them.
- The update shows its release notes in Settings → About, and its card offers Skip this version: the background check then does not install that version.

### Changed

- The Electron app gets no more updates, starting with 1.36.0. Install the native Droi from the Release page instead; it keeps your Sessions and settings.
- The composer grows up to 8 lines before it scrolls.
- A Session working in the background no longer rebuilds the open transcript on each of its events.

### Fixed

- `/compact` on a long Session no longer fails after 30 seconds with "context deadline exceeded": Droi waits 240 seconds, as the Droid SDK does. Cleaning up a worktree, a semantic diff and file transfers wait as long as the SDK too.
- An older read of the Session list no longer replaces a newer one.
- While the Daemon does not come up, the window says why (droid was not found, the Daemon did not start, it did not accept the login, or Droi could not connect) and offers Restart Daemon and Settings, instead of "Starting the Daemon" for ever.
- A Session in a git worktree uses its repository's Project Memory instead of an empty one of its own. Memory saved under a worktree that still exists moves into the repository's.

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
