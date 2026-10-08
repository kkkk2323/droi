<coding_guidelines>
# Repository Guidelines

Droi is a thin Client of the Droid Daemon. Read `CONTEXT.md` for the vocabulary
(Daemon, Desktop Shell, Gateway, Client, Pairing Token, Fake Daemon, ...) and
`docs/adr/` for the decisions behind the layout. Use those names in code,
comments, commits and issues.

## What is maintained

Only two apps are maintained: the native app (`apps/native`, Go on MyGo, macOS and
Windows) and the Phone App (`apps/mobile`), with what they use: `packages/droid-sdk-go`,
`packages/daemon-layer`, `tests/fake-daemon` and `tests/mobile`.

The Electron Desktop Shell and the web Client (`apps/desktop`, except its `package.json`
version, which stays the release version) and their suites (`tests/web`, `tests/electron`,
`tests/live` and the PR workflow's `smoke` job) are deprecated (`apps/desktop/README.md`).
Releases no longer carry an update for the Electron app. Do not add features to them, fix them or run their suites. They stay in the
repository until they are removed, so a change to shared code must still pass `pnpm check`
and `pnpm test` there; keep such fixes to what makes them compile.

## Project Structure

```
apps/
├── native/     # The desktop app: Go on MyGo (macOS, Windows); see apps/native/README.md
│   ├── internal/app/   # The window: views and their tests against the Fake Daemon
│   ├── internal/host/  # Host: settings, Daemon supervisor, sign-in, CLI login, Runtime Overlay
│   └── internal/...    # transcript, gateway (Remote Access), memorywork (Memory), ...
├── mobile/     # Phone App: Expo SDK 57 iPhone Client (expo-router under src/app,
│               # screens under src/screens, hardware behind src/platform with
│               # *.web.ts stand-ins for the tests); DEVICE-CHECKLIST.md
└── desktop/    # Deprecated: Electron Desktop Shell and web Client (package "droi"; its
                # version is the release version, which the Phone App reads too)
packages/
├── droid-sdk-go/  # The Go SDK of the Daemon's protocol: client, Session store, controller, Fake Daemon
└── daemon-layer/  # @droi/daemon-layer: the Phone App's daemon layer (connection, Session
                   # state hooks, Gateway contract, local preferences); no DOM or Electron
tests/          # @droi/tests: the Playwright suites, one config per folder
├── fake-daemon/   # The Fake Daemon: the Phone App's suite and the native app's view tests use it
├── mobile/        # The Phone App's web build against the Fake Daemon
├── web/           # Deprecated: the web Client in a browser
├── electron/      # Deprecated: smoke suite against the Desktop Shell
└── live/          # Deprecated: the web Client and the Electron Gateway against a real Daemon
scripts/        # install:phone (install:mac builds the deprecated Electron app)
docs/           # ADRs, agent docs, the README screenshot
```

The repository is a pnpm workspace plus a Go workspace (`go.work`); every command below runs
from the root. The root holds only workspace tooling (oxlint, oxfmt, vitest, TypeScript);
each app declares its own dependencies. TypeScript unit tests sit next to the code as
`*.test.ts` / `*.test.tsx` and run with vitest from the root; Go tests are `*_test.go`.

## Development Commands

| Command | Description |
|---------|-------------|
| `go run ./apps/native` | Run the native app (needs `droid` on PATH) |
| `go test ./apps/native/... ./packages/droid-sdk-go/...` | Go unit tests and the native app's view tests |
| `go vet ./apps/native/... ./packages/droid-sdk-go/...` | Also with `GOOS=windows` when the change touches Windows |
| `go tool -modfile=apps/native/go.mod mygo build apps/native` | Build the native app (`.app` and `.dmg`; `-platform windows/amd64` for Windows) |
| `pnpm test` | Run vitest once |
| `pnpm test:e2e:phone` | Export the Phone App for web and run Playwright against it (Fake Daemon) |
| `pnpm install:phone` | Build a signed Release of the Phone App and install it on the connected iPhone |
| `pnpm typecheck` | TypeScript validation (root + Desktop Shell node/web + Phone App + tests) |
| `pnpm lint` / `pnpm lint:fix` | oxlint |
| `pnpm format` / `pnpm format:check` | oxfmt |
| `pnpm check` | format check + lint + typecheck |

Deprecated, for the Electron app only: `pnpm dev`, `pnpm dev:client`, `pnpm build`,
`pnpm build:mac`, `pnpm install:mac`, `pnpm test:e2e`, `pnpm test:smoke`, `pnpm test:live`.

## Validation Workflow

Before committing run `pnpm check && pnpm test`, and for Go changes `gofmt -l`,
`go vet` and `go test` over `./apps/native/... ./packages/droid-sdk-go/...` (as the PR
workflow does). Run `pnpm test:e2e:phone` when the Phone App, the shared daemon layer or the
Fake Daemon changed. For what only an iPhone can show, walk `apps/mobile/DEVICE-CHECKLIST.md`.

The Phone App is pinned to Expo SDK 57 (ADR 0006). Expo changes its APIs every
SDK: read the versioned docs (`https://docs.expo.dev/versions/v57.0.0/`) or the
installed types before using an Expo module, and add Expo packages with
`npx expo install` from `apps/mobile` so the versions match the SDK.

## Code Style

- Go: `gofmt`; no cgo (MyGo builds with `CGO_ENABLED=0`); platform code in `_windows.go` /
  `_other.go` (or `_darwin.go`) pairs
- TypeScript strict, `verbatimModuleSyntax`, `noUncheckedIndexedAccess`; the shared layer is
  imported as `@droi/daemon-layer/<module>` and uses relative imports inside itself
- Geist Sans for UI, Geist Mono for code (the native app vendors them as TTF in `apps/native/assets/fonts`)
- File naming: kebab-case for components, camelCase for utilities
- Git: Conventional Commits

## Testing

- The native app has no end-to-end suite that opens a window. Its view tests run `App.View`
  in MyGo's `ui.NewTester` (no window; click, type and read by text or label) against the
  real Fake Daemon, through the real SDK, so they cover everything but the window and the
  operating system. MyGo's own GUI tests (`internal/e2e`) cover only the framework. Check
  windows, menus, installers and Windows behavior by hand, or drive the app with Cua Driver.
- Reference screens: `DROI_NATIVE_SHOTS=/tmp/shots go test ./apps/native/internal/app -v`
  writes each screen and its pixel diff against the recorded reference
- README screenshot: `DROI_README_SHOTS=1 go test ./apps/native/internal/app -run TestReadmeScreenshots`
  draws the native app against the Fake Daemon into `docs/screenshot.png` (skipped in a normal run)
- Prefer `getByRole` / `getByLabel` selectors in Playwright; add `data-testid` only when no accessible name fits
- E2E tests never need a Factory API key; they run against the Fake Daemon
- Live tests against a real Daemon, through the local droid-proxy (`dp`) with a cheap model:
  `FACTORY_API_KEY=$(grep -m1 '^fk-' ~/.config/dp/keys.txt) FACTORY_API_BASE_URL=$(dp status | awk '/baseURL/ {print $2}') go test ./apps/native/internal/host ./apps/native/internal/gateway -run Live -v`
  (`DROI_LIVE_MODEL` defaults to `glm-5.3-flash`; the Daemon runs in a throwaway HOME so the key need not own this computer's Factory registration)

## Factory login

The native app hands its sign-in to the `droid` CLI (ADR 0015, `apps/native/internal/host/cli_login.go`):
the two share one login, which the Daemon runs as. (The deprecated Electron Desktop Shell
used the CLI's device flow instead, ADR 0005.)

## Factory API base URL

The native app passes `FACTORY_API_BASE_URL` to the Daemon when Settings has a
"Factory API base URL" (or its own environment sets it). Point it at droid-proxy
(`dp status` → baseURL) to route the Daemon's Factory traffic through the proxy.
</coding_guidelines>
