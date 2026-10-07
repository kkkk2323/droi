# Droi native (MyGo)

The Desktop Shell and its window rewritten in Go on [MyGo](https://github.com/egoist/mygo)'s
native UI: no Electron, no webview, no Node at run time. It looks the same as
the web Client (`apps/desktop/src/renderer`) running as the Local Client, pixel
for pixel where it can.

It replaces the Electron app: same bundle identifier (`com.droi.app`), name
and data folder (`~/Library/Application Support/Droi`), so the Shell
settings, the Pairing Token and Memory carry over. The window talks to the
Daemon directly through `packages/droid-sdk-go`; the Host supplies the
credential and the System Prompt Addition the Gateway adds for other Clients.

How it performs next to the Electron app: [docs/performance.md](docs/performance.md). How
it is released and updates itself: [docs/releases.md](docs/releases.md).

## Layout

| Path                                                            | What                                                                                                    |
| --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `main.go`                                                       | the app: window, menus, the Host                                                                        |
| `assets/`                                                       | the vendored Geist fonts, embedded                                                                      |
| `internal/theme/`                                               | the design tokens of `global.css`: the three themes, fonts, text sizes                                  |
| `internal/icons/`                                               | the Lucide icons the web Client imports (`scripts/gen-icons.mjs`)                                       |
| `internal/host/`                                                | the Host: Shell settings, Daemon supervisor, Factory sign-in, the droid CLI's login, Scratch Workspaces |
| `internal/memory/`                                              | Memory's store (SQLite, the Electron app's schema), Memory Server, hook, Markdown export                |
| `internal/memorywork/`                                          | Memory Sessions: consolidation, extraction, prompts, what Settings → Memory shows                       |
| `internal/gateway/`                                             | the Gateway for paired phones: proxy to the Daemon, pairing link, Scratch and session-file endpoints   |
| `internal/updates/`                                             | the in-app update: check, download, restart (MyGo's signed updates)                                     |
| `internal/prefs/`                                               | the Client's local preferences (localStorage's place)                                                   |
| `internal/transcript/`                                          | messages to transcript entries, tool calls, Script runs, json-render (daemon-layer port)                |
| `internal/sessions/`                                            | the Session list: grouping, sorting, pins, activity (daemon-layer port)                                 |
| `internal/models/`, `defaults/`, `skills/`, `mcp/`, `slash/`, … | models, Session Defaults, skills, MCP, slash items and the rest of the daemon layer, one package each   |
| `internal/md/`                                                  | streaming Markdown to blocks                                                                            |
| `internal/highlight/`                                           | code colours as Shiki gives them                                                                        |
| `internal/kit/`                                                 | the shadcn/Base UI look as MyGo elements: buttons, menus, selects, switches, dialogs                    |
| `internal/app/`                                                 | the views: shell, sidebar, Session, composer, prompts, settings                                         |
| `testdata/reference/`                                           | the web Client's screens at 2x with computed styles, recorded by `tests/web/native-reference.spec.ts`   |

## Commands (from the repository root)

```sh
go run ./apps/native                 # run it (needs droid on PATH)
go test ./apps/native/...            # unit tests, view tests, Fake Daemon tests
go tool -modfile=apps/native/go.mod mygo build apps/native   # .app and .dmg
DROI_NATIVE_REF=1 pnpm test:e2e --project=desktop native-reference.spec.ts   # re-record the reference
node apps/native/scripts/gen-icons.mjs   # after the web Client imports a new icon
```

## Status

Tracked in this file while the port is under way.

- [x] Phase 0: SDK in the repo (`packages/droid-sdk-go`), module, tokens, fonts, icons
- [x] Phase 1: reference screens recorded
- [x] Phase 2: Host (settings, supervisor, sign-in, CLI login, Scratch)
- [x] Phase 3: shell, sidebar, search, routes, connection, `main.go`
- [x] Phase 4: Session view: transcript, Markdown, code, tools, thinking, subagent cards, todos, Scripts
- [x] Phase 5: composer, model picker, attachments, slash, queue, prompts, New session
- [x] Phase 6: Settings, Skills/MCP, git changes, Open in, copy Session, alerts (sound, desktop
      notification, unread mark), the subagent menu and trail
- [x] Phase 7: view tests against the Fake Daemon (`internal/app/app_test.go`) with a pixel
      comparison per reference screen
- [x] Phase 8: replaces the Electron app: identity and data folder; zoom, pasted images, the
      turn rail, custom alert sounds, the sign-in banner, "droid was updated", the start-up screen
- [x] Phase 9: in-app update (signed delta updates, restart when idle); the Electron app's last
      update points at the native one
- [x] Phase 10: Memory (store, Memory Server, hook, Memory Sessions, Settings → Memory), on the
      Electron app's data; the app's own binary is the Memory Server (`Droi memory-server`) and
      the hook (`Droi memory-hook`)
- [x] Phase 11: Remote Access and pairing for the Phone App (Gateway, Settings → Remote Access);
      no web Client in the Gateway

### How close it is

`DROI_NATIVE_SHOTS=/tmp/shots go test ./apps/native/internal/app -v` writes each screen and a
diff (red: a channel off by more than 24 of 255) next to the share of pixels that differ.
Most of what is left is glyph edges, as MyGo and Chromium antialias text differently.

| Screen                                                       | Differs                   |
| ------------------------------------------------------------ | ------------------------- |
| session (light / dark / Solarized)                           | 2.0% / 1.9% / 2.0%        |
| session, tool call open                                      | 1.8%                      |
| sidebar hidden                                               | 1.8%                      |
| New session                                                  | 2.0%                      |
| Settings: General / Session defaults / Notifications / About | 1.7% / 2.5% / 2.0% / 2.0% |
| permission / AskUser                                         | 1.2% / 0.9%               |

Settings leaves out Account, Memory, Remote Access and Advanced when there is no Host (the
tests); the app has them all.

Live tests against a real Daemon, through droid-proxy, skip without a key:

```sh
FACTORY_API_KEY=$(grep -m1 '^fk-' ~/.config/dp/keys.txt) FACTORY_API_BASE_URL=$(dp status | awk '/baseURL/ {print $2}') \
  go test ./apps/native/internal/host ./apps/native/internal/gateway -run Live -v
```
