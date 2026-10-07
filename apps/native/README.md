# Droi native (MyGo)

The Desktop Shell and its window rewritten in Go on [MyGo](https://github.com/egoist/mygo)'s
native UI: no Electron, no webview, no Node at run time. It looks the same as
the web Client (`apps/desktop/src/renderer`) running as the Local Client, pixel
for pixel where it can.

What it leaves out on purpose (first version):

- the Gateway and the web Client it serves: no Remote Access, no pairing, no
  phone; the window talks to the Daemon directly through
  `packages/droid-sdk-go`, and the Host supplies the credential and the
  System Prompt Addition the Gateway used to add to frames;
- Memory (Runtime Overlay, Memory Server, hook, Memory Sessions).

## Layout

| Path                                                            | What                                                                                                    |
| --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `main.go`                                                       | the app: window, menus, the Host                                                                        |
| `assets/`                                                       | the vendored Geist fonts, embedded                                                                      |
| `internal/theme/`                                               | the design tokens of `global.css`: the three themes, fonts, text sizes                                  |
| `internal/icons/`                                               | the Lucide icons the web Client imports (`scripts/gen-icons.mjs`)                                       |
| `internal/host/`                                                | the Host: Shell settings, Daemon supervisor, Factory sign-in, the droid CLI's login, Scratch Workspaces |
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
- [ ] Phase 6: done: Settings, Skills/MCP, Open in, copy Session; still to do: the git changes
      button, alert sounds and notifications when a turn ends or waits, the subagent menu and trail
- [x] Phase 7: view tests against the Fake Daemon (`internal/app/app_test.go`) with a pixel
      comparison per reference screen

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

Settings leaves out Account, Advanced and the Memory and Remote Access sections when there is no
Host (the tests); the app has all but Memory and Remote Access.
