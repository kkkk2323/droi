---
status: accepted
---

# Droi exports its Host and Session view for WeCom Droi

WeCom Droi is a second desktop app (Go on MyGo, macOS and Linux with a desktop) in a private repository, `kkkk2323/wecom-droi`. It starts its own Daemon and serves a WeCom bot: each WeCom post is a Session, and its window shows the posts, a Session's transcript, and lets a person answer permission requests and AskUser questions or write into the Session. It replaces the Chat Bridge that lived in Droi (removed in 1.53.0, kept on `archive/chat-bridge`). Go does not let another module import `apps/native/internal`, so what it reuses has to become public. The Go SDK and the Fake Daemon already moved out (ADR 0021).

We decided:

- **Public packages stay in this repository, under `apps/native/pkg/`.** Droi's own tests and reference screens keep covering them. Each Droi release also tags the module as `apps/native/vX.Y.Z`, which WeCom Droi requires.
- **What becomes public:** the Host (Daemon supervisor, settings, sign-in, Credential), the Gateway, `l10n` (with a way for another app to add its own translations), the UI kit (kit, theme, icons, brands) and the data packages a transcript needs (transcript, jsonrender, subagents, sessions, models, defaults, prefs, attachments, compaction), and a Session view: the transcript with tool calls and Markdown, the permission and AskUser cards, sending, and a plain-text composer. Droi keeps its own composer, with its slash menu, attachments and settings bar, and sends and shows the Prompts through the Session view. Settings' General and Session defaults become components both apps draw.
- **Not public:** the sidebar, the New session page, worktrees, Git changes, search, the turn rail, Automations, computer switching, Remote Access settings, updates, Memory and `hostrun`. WeCom Droi draws its own post list in place of the sidebar.
- **WeCom Droi keeps its own Factory data.** The Host gets a Factory home of its own for the Daemon (`FACTORY_HOME_OVERRIDE`) whose sessions are not linked back to `~/.factory`, and its sign-in is written there, not to the CLI's login. Its Sessions do not show in Droi, signing out of one app does not sign out the other, and Automations do not run twice.
- **Its Gateway listens on a random loopback port, without Remote Access**, so it never takes Droi's port 41417. The Host gets parameters for what is Droi's today: the default Scratch folder and the app name `/meta` answers.

Memory comes to WeCom Droi later, as its own decision.

## Considered Options

- **A second app in this repository.** No refactor at all, but the bot's code and its releases would live in Droi's public repository.
- **A separate UI repository, like droid-sdk-go.** Cleaner dependencies, but every change to the Session view would be a release there and a pin in Droi, and Droi's view tests would stop covering it directly.
- **Share `~/.factory` with Droi and the CLI.** Nothing to add to the Host, but bot Sessions would fill Droi's list, one app's sign-out would sign out the other, and both Daemons would run every Automation.

## Consequences

- The packages under `apps/native/pkg/` are an API: a change there can break WeCom Droi, which finds out when it moves to the next tag.
- Droi's view tests that read a Session view's fields (composer text, errors, images) read them through accessors.
- `l10n`'s catalog test still checks Droi's strings; WeCom Droi checks its own.
