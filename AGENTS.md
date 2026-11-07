<coding_guidelines>

# Repository Guidelines

Droi is a thin Client of its own agent process. Read `CONTEXT.md` for the vocabulary
(Kernel, Host, Gateway, Client, Pairing Token, Conversation, Approval, ...) and
`docs/adr/` for the decisions behind the layout. Use those names in code,
comments, commits and issues.

**Droi 2.0 replaces the Droid Daemon with the Kernel** (ADR 0038): a TypeScript sidecar in
`apps/kernel` built on `@earendil-works/pi-durable` 1.5.0, with the coding tools ported from
`@earendil-works/pi-coding-agent` 1.5.0. Until that work lands, the 1.x code below still
starts `droid daemon` and the ADRs about it still describe what the code does; where this file
says a thing is being removed, the ADR is the reason. The work branches off `feat/pi-kernel`
and merges there, never into `main`.

## What is maintained

Two apps are maintained: the native app (`apps/native`, Go on MyGo, macOS and
Windows) and the Phone App (`apps/mobile`), with what they use: `packages/daemon-layer` and
`tests/mobile`. The Go SDK and the Fake Daemon they use live in their own repository,
[droid-sdk-go](https://github.com/kkkk2323/droid-sdk-go) (ADR 0021).

A third is being added for 2.0: the Kernel (`apps/kernel`, TypeScript on Bun, macOS, Windows and
Linux). It does not exist yet; ADR 0038 is the decision behind it.

Two apps build on Droi's packages rather than in this repository: **Anlan Droi** (Codeup, on
Droi 1.90.0) and **WeCom Droi** (on 1.55.0). 2.0 keeps `droiapp.Run` and the `Extension` seams so
they can be ported afterwards, and does not migrate them (ADR 0041). A change to a public package
(`droiapp`, `extension`, `brand`, `defaults`, `kit`, `l10n`, `sessionview`, `keypool`,
`mcpserver`) is a change to their build too.

The Electron Desktop Shell and the web Client (`apps/desktop`) and their suites
(`tests/web`, `tests/electron`, `tests/live`) were removed after 1.59.0; the git history
before that has them, and the ADRs that describe them stay as the record of those decisions.
The release version lives in `apps/native/mygo.json`.

## Project Structure

```
apps/
├── kernel/     # 2.0: the Kernel, Droi's own agent process (TypeScript on Bun, ADR 0038)
│   ├── src/protocol/    # the contract: apps/kernel/protocol/README.md + its fixtures
│   ├── src/session/     # Conversations, the attach table, watch → conversation.view
│   ├── src/extensions/  # coding tools, skills, MCP, approval, ask-user, plan, memory, codemode
│   ├── src/storage/     # the bun:sqlite facade (one connection, one database)
│   ├── protocol/fixtures/  # the JSON examples both sides test against
│   └── build.ts         # the four bun build --compile targets
├── native/     # The desktop app: Go on MyGo (macOS, Windows); see apps/native/README.md
│   ├── internal/app/   # The window: views and their tests against the Fake Daemon
│   ├── pkg/kernel/     # 2.0: the Go client for the Kernel's protocol
│   ├── pkg/host/       # Host: settings, the Kernel's supervisor, Key Pool, LLM Proxy, Memory,
│   │                   # Automations, Workstreams
│   ├── internal/...    # memorywork (Memory), computer, updates, slash, ...
│   └── pkg/...         # Public, for WeCom Droi (ADR 0022): host, gateway (Remote Access); and
│                       # sessionview (a Session's entries, Approvals, plain composer),
│                       # settingsview (Settings' Appearance, Session defaults, model picker),
│                       # l10n, kit, theme, transcript, ...; and for apps built on Droi
│                       # (ADR 0029): droiapp (the app), brand, extension, mcpserver, dashkit
└── mobile/     # Phone App: Expo SDK 57 iPhone Client (expo-router under src/app,
                # screens under src/screens, hardware behind src/platform with
                # *.web.ts stand-ins for the tests); DEVICE-CHECKLIST.md
packages/
└── daemon-layer/  # @droi/daemon-layer: the Phone App's daemon layer (connection, Session
                   # state hooks, Gateway contract, local preferences); no DOM or Electron;
                   # src/kernel/ is its client for the Kernel's protocol (2.0, ADR 0038)
tests/          # @droi/tests: the Playwright suites, one config per folder
└── mobile/        # The Phone App's web build against the Fake Daemon
scripts/        # install:phone, release.sh, measure-cpu.py
docs/           # ADRs, agent docs, the README screenshot
```

The repository is a pnpm workspace plus a Go workspace (`go.work`); every command below runs
from the root. The root holds only workspace tooling (oxlint, oxfmt, vitest, TypeScript);
each app declares its own dependencies. TypeScript unit tests sit next to the code as
`*.test.ts` / `*.test.tsx` and run with vitest from the root; Go tests are `*_test.go`.

## Development Commands

| Command                                                      | Description                                                                                                             |
| ------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------- |
| `go run ./apps/native`                                       | Run the native app (1.x: needs `droid` on PATH; 2.0: builds and starts the Kernel)                                      |
| `pnpm test:kernel`                                           | The Kernel's own suite, under Bun: the storage conformance, the protocol, conversations, crash recovery, images, search |
| `pnpm build:kernel`                                          | Compile the Kernel for every target into `apps/native/assets/kernel/` (not committed)                                   |
| `pnpm smoke:kernel <executable>`                             | Start a built Kernel, `read` a PNG, assert an image comes back (the cross-compile check)                                |
| `pnpm typecheck:kernel` / `pnpm lint:kernel`                 | The Kernel's own type check and lint                                                                                    |
| `go test ./apps/native/...`                                  | Go unit tests and the native app's view tests                                                                           |
| `go vet ./apps/native/...`                                   | Also with `GOOS=windows` when the change touches Windows                                                                |
| `go tool -modfile=apps/native/go.mod mygo build apps/native` | Build the native app (`.app` and `.dmg`; `-platform windows/amd64` for Windows)                                         |
| `pnpm test`                                                  | Run vitest once                                                                                                         |
| `pnpm test:e2e:phone`                                        | Export the Phone App for web and run Playwright against it (Fake Daemon)                                                |
| `pnpm install:phone`                                         | Build a signed Release of the Phone App and install it on the connected iPhone                                          |
| `pnpm typecheck`                                             | TypeScript validation (root + Phone App + tests)                                                                        |
| `pnpm lint` / `pnpm lint:fix`                                | oxlint                                                                                                                  |
| `pnpm format` / `pnpm format:check`                          | oxfmt                                                                                                                   |
| `pnpm check`                                                 | format check + lint + typecheck                                                                                         |

## Validation Workflow

Before committing run `pnpm check && pnpm test`, and for Kernel changes `pnpm test:kernel`
(it runs Pi's storage and environment conformance suites, which must stay green: a facade that
passes them is the only reason to trust `bun:sqlite` here). For Go changes
`"$(go env GOROOT)/bin/gofmt" -l apps/native`, `go vet` for darwin, windows and linux,
and `go test` over `./apps/native/...` plus `go test -cpu 1 ./apps/native/internal/app`
(as the PR workflow does: on one CPU a view test that races the Fake Daemon's answers fails
there, not in a Release; the workflow runs the view tests in shards of that binary, which
cover the same tests). Format with that gofmt, which is go.mod's
Go: a `gofmt` on PATH from another Go lays out `pkg/l10n/zh.go` differently, and the PR
workflow rejects it (`go test ./apps/native` checks this too). Run
`pnpm test:e2e:phone` when the Phone App, the shared daemon layer or the Fake Daemon
changed. For what only an iPhone can show, walk `apps/mobile/DEVICE-CHECKLIST.md`.

## droid-sdk-go

_2.0 replaces the Go SDK's client with `apps/native/pkg/kernel` and the Fake Daemon with a real
Kernel started by a test (ADR 0038). The repository, its tag pins and its other consumer stand;
what Droi no longer needs from it is the Daemon protocol._

The Go SDK (`github.com/kkkk2323/droid-sdk-go`) and the Fake Daemon
(`@droi/fake-daemon`, which the Go SDK's `fakedaemon` package embeds) come from
[droid-sdk-go](https://github.com/kkkk2323/droid-sdk-go) at a tag. To change either, change
that repository (its README says how to rebuild the Fake Daemon), tag a release, then move
both pins here to it: `apps/native/go.mod` (`go get github.com/kkkk2323/droid-sdk-go@<tag>`
from `apps/native`) and `@droi/fake-daemon` in `tests/package.json`. While working on both,
`go work use <checkout>` and `link:<checkout>/fake-daemon` use a local checkout; do not
commit them.

The Phone App is pinned to Expo SDK 57 (ADR 0006). Expo changes its APIs every
SDK: read the versioned docs (`https://docs.expo.dev/versions/v57.0.0/`) or the
installed types before using an Expo module, and add Expo packages with
`npx expo install` from `apps/mobile` so the versions match the SDK.

## Releases and performance

- Releasing: `scripts/release.sh <version>`, after the changelog; steps in `apps/native/docs/releases.md`.
- CPU, memory or frame cost: how to measure and profile is in `apps/native/docs/performance.md`.

## Code Style

- Go: `gofmt`; no cgo (MyGo builds with `CGO_ENABLED=0`); platform code in `_windows.go` /
  `_other.go` (or `_darwin.go`) pairs
- TypeScript strict, `verbatimModuleSyntax`, `noUncheckedIndexedAccess`; the shared layer is
  imported as `@droi/daemon-layer/<module>` and uses relative imports inside itself
- The system's interface font for UI and JetBrains Mono for code (vendored as TTF in
  `apps/native/assets/fonts`; Geist stays for Settings → Font, ADR 0025). Sizes, radii and
  control heights come from `pkg/theme` and `pkg/kit`: use them, do not hard-code a colour. A size
  that differs between Settings → Density's Compact and Comfortable is `k.Dense(compact, comfortable)`
  (ADR 0026).
- File naming: kebab-case for components, camelCase for utilities
- Git: Conventional Commits

## Testing

- The native app's view tests run `App.View` in MyGo's `ui.NewTester` (no window; click,
  type and read by text or label) against the real Fake Daemon, through the real SDK, so they
  cover everything but the window and the operating system. One test opens the window:
  `DROI_E2E=1 go test ./apps/native -run TestTheWindow -v` (macOS, a desktop session) builds
  the app with `-tags e2e`, which links MyGo's automation plugin, and drives the menu bar, a key
  and Quit through its socket, with no Daemon. Check installers and Windows behavior by hand,
  or drive the app with Cua Driver.
- Reference screens: `DROI_NATIVE_SHOTS=/tmp/shots go test ./apps/native/internal/app -v`
  writes each screen and its pixel diff against the recorded reference
- README screenshot: `DROI_README_SHOTS=1 go test ./apps/native/internal/app -run TestReadmeScreenshots`
  draws the native app against the Fake Daemon into `docs/screenshot.png` (skipped in a normal run)
- Prefer `getByRole` / `getByLabel` selectors in Playwright; add `data-testid` only when no accessible name fits
- E2E tests never need a Factory API key; they run against the Fake Daemon
- Live tests against a real Daemon, through the local droid-proxy (`dp`) with a cheap model:
  `FACTORY_API_KEY=$(grep -m1 '^fk-' ~/.config/dp/keys.txt) FACTORY_API_BASE_URL=$(dp status | awk '/baseURL/ {print $2}') go test ./apps/native/pkg/host ./apps/native/pkg/gateway ./apps/native/internal/hostrun -run Live -v -timeout 30m`
  (`DROI_LIVE_MODEL` defaults to `glm-5.3-flash`; the Daemon runs in a throwaway HOME so the key need not own this computer's Factory registration).
  `internal/hostrun`'s run the Host's Automations and Workstreams through its Gateway on Droi's droid
  (`~/Library/Application Support/Droi/droid/1.4.1/droid`; `DROID_PATH` picks another), macOS and Linux only

## Factory login

**2.0 has no sign-in and no Factory Accounts** (ADR 0039). A user pastes an `fk-` key in
Settings → Key Pool, and the Host's LLM Proxy stamps it on each Factory request; there is no device
flow, no `~/.factory` credential store to share, and no per-account folder. What the 1.x code does
today, and what 2.0 deletes, is: the native app hands its sign-in to the `droid` CLI (ADR 0015,
`apps/native/pkg/host/cli_login.go`), the two share one login which the Daemon runs as, and more
Factory accounts keep a folder of their own given to the Daemon as `FACTORY_HOME_OVERRIDE`
(ADR 0017, `apps/native/pkg/host/accounts.go`).

## Factory API base URL

In 1.x the native app passes `FACTORY_API_BASE_URL` to the Daemon when Settings has a
"Factory API base URL" (or its own environment sets it); pointed at droid-proxy (`dp status` →
baseURL) it routes the Daemon's Factory traffic through the proxy.

In 2.0 the Kernel's Factory traffic always goes through the Host's own LLM Proxy, whose address
is what the Kernel's Factory provider is pointed at (ADR 0039). The setting keeps its meaning for
the upstream the LLM Proxy talks to, and droid-proxy stays a way to send that upstream elsewhere.
</coding_guidelines>
