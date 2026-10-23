<coding_guidelines>
# Repository Guidelines

Droi is a thin Client of the Droid Daemon. Read `CONTEXT.md` for the vocabulary
(Daemon, Desktop Shell, Gateway, Client, Pairing Token, Fake Daemon, ...) and
`docs/adr/` for the decisions behind the layout. Use those names in code,
comments, commits and issues.

## What is maintained

Only two apps are maintained: the native app (`apps/native`, Go on MyGo, macOS and
Windows) and the Phone App (`apps/mobile`), with what they use: `packages/daemon-layer` and
`tests/mobile`. The Go SDK and the Fake Daemon they use live in their own repository,
[droid-sdk-go](https://github.com/kkkk2323/droid-sdk-go) (ADR 0021).

The Electron Desktop Shell and the web Client (`apps/desktop`) and their suites
(`tests/web`, `tests/electron`, `tests/live`) were removed after 1.59.0; the git history
before that has them, and the ADRs that describe them stay as the record of those decisions.
The release version lives in `apps/native/mygo.json`.

## Project Structure

```
apps/
├── native/     # The desktop app: Go on MyGo (macOS, Windows); see apps/native/README.md
│   ├── internal/app/   # The window: views and their tests against the Fake Daemon
│   ├── pkg/host/       # Host: settings, Daemon supervisor, sign-in, CLI login, Runtime Overlay
│   ├── internal/...    # memorywork (Memory), computer, updates, slash, ...
│   └── pkg/...         # Public, for WeCom Droi (ADR 0022): host, gateway (Remote Access),
│                       # sessionview (a Session's transcript, Prompts, plain composer),
│                       # settingsview (Settings' Appearance, Session defaults, model picker),
│                       # l10n, kit, theme, transcript, ...
└── mobile/     # Phone App: Expo SDK 57 iPhone Client (expo-router under src/app,
                # screens under src/screens, hardware behind src/platform with
                # *.web.ts stand-ins for the tests); DEVICE-CHECKLIST.md
packages/
└── daemon-layer/  # @droi/daemon-layer: the Phone App's daemon layer (connection, Session
                   # state hooks, Gateway contract, local preferences); no DOM or Electron
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

| Command | Description |
|---------|-------------|
| `go run ./apps/native` | Run the native app (needs `droid` on PATH) |
| `go test ./apps/native/...` | Go unit tests and the native app's view tests |
| `go vet ./apps/native/...` | Also with `GOOS=windows` when the change touches Windows |
| `go tool -modfile=apps/native/go.mod mygo build apps/native` | Build the native app (`.app` and `.dmg`; `-platform windows/amd64` for Windows) |
| `pnpm test` | Run vitest once |
| `pnpm test:e2e:phone` | Export the Phone App for web and run Playwright against it (Fake Daemon) |
| `pnpm install:phone` | Build a signed Release of the Phone App and install it on the connected iPhone |
| `pnpm typecheck` | TypeScript validation (root + Phone App + tests) |
| `pnpm lint` / `pnpm lint:fix` | oxlint |
| `pnpm format` / `pnpm format:check` | oxfmt |
| `pnpm check` | format check + lint + typecheck |

## Validation Workflow

Before committing run `pnpm check && pnpm test`, and for Go changes
`"$(go env GOROOT)/bin/gofmt" -l apps/native`, `go vet` and `go test` over
`./apps/native/...` (as the PR workflow does). Format with that gofmt, which is go.mod's
Go: a `gofmt` on PATH from another Go lays out `pkg/l10n/zh.go` differently, and the PR
workflow rejects it (`go test ./apps/native` checks this too). Run
`pnpm test:e2e:phone` when the Phone App, the shared daemon layer or the Fake Daemon
changed. For what only an iPhone can show, walk `apps/mobile/DEVICE-CHECKLIST.md`.

## droid-sdk-go

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
  `FACTORY_API_KEY=$(grep -m1 '^fk-' ~/.config/dp/keys.txt) FACTORY_API_BASE_URL=$(dp status | awk '/baseURL/ {print $2}') go test ./apps/native/pkg/host ./apps/native/pkg/gateway -run Live -v`
  (`DROI_LIVE_MODEL` defaults to `glm-5.3-flash`; the Daemon runs in a throwaway HOME so the key need not own this computer's Factory registration)

## Factory login

The native app hands its sign-in to the `droid` CLI (ADR 0015, `apps/native/pkg/host/cli_login.go`):
the two share one login, which the Daemon runs as. (The removed Electron Desktop Shell
used the CLI's device flow instead, ADR 0005.) More Factory accounts can be added in Settings:
each has its own folder, which the Daemon gets as `FACTORY_HOME_OVERRIDE`, linked to the CLI's
skills, settings and sessions (ADR 0017, `apps/native/pkg/host/accounts.go`).

## Factory API base URL

The native app passes `FACTORY_API_BASE_URL` to the Daemon when Settings has a
"Factory API base URL" (or its own environment sets it). Point it at droid-proxy
(`dp status` → baseURL) to route the Daemon's Factory traffic through the proxy.
</coding_guidelines>
