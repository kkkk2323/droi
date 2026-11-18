<coding_guidelines>
# Repository Guidelines

Droi is a thin Client of the Droid Daemon. Read `CONTEXT.md` for the vocabulary
(Daemon, Desktop Shell, Gateway, Client, Pairing Token, Fake Daemon, ...) and
`docs/adr/` for the decisions behind the layout. Use those names in code,
comments, commits and issues.

## What is maintained

Only two apps are maintained: the native app (`apps/native`, Go on MyGo, macOS and
Windows) and the Phone App (`apps/native/phone`, the same window on an iPhone, ADR 0040).
The Go SDK and the Fake Daemon they use live in their own repository,
[droid-sdk-go](https://github.com/kkkk2323/droid-sdk-go) (ADR 0021).

The Electron Desktop Shell and the web Client (`apps/desktop`) and their suites
(`tests/web`, `tests/electron`, `tests/live`) were removed after 1.59.0; the git history
before that has them, and the ADRs that describe them stay as the record of those decisions.
The release version lives in `apps/native/mygo.json`, and the Phone App keeps the same one in
its own.

## Project Structure

```
apps/
├── native/     # The desktop app: Go on MyGo (macOS, Windows); see apps/native/README.md
│   ├── internal/app/   # The window: views and their tests against the Fake Daemon
│   ├── pkg/host/       # Host: settings, Daemon supervisor, sign-in, CLI login, Runtime Overlay
│   ├── internal/...    # memorywork (Memory), computer, updates, slash, ...
│   ├── phone/          # The Phone App: the same window on an iPhone, drawn by MyGo (ADR 0040):
│   │                   # main.go, the pairing page, phonekit's Swift plugin; DEVICE-CHECKLIST.md
│   └── pkg/...         # Public, for WeCom Droi (ADR 0022): host, gateway (Remote Access),
│                       # sessionview (a Session's transcript, Prompts, plain composer),
│                       # settingsview (Settings' Appearance, Session defaults, model picker),
│                       # l10n, kit, theme, transcript, ...; and for apps built on Droi
│                       # (ADR 0029): droiapp (the app), brand, extension, mcpserver, dashkit
scripts/        # install-phone.sh, release.sh, measure-cpu.py
docs/           # ADRs, agent docs, the README screenshot
```

The repository is a pnpm workspace plus a Go workspace (`go.work`); every command below runs
from the root. pnpm carries only the JavaScript tooling (oxlint, oxfmt, and lucide-static for
`apps/native/scripts/gen-icons.mjs`); the apps are Go, and their tests are `*_test.go` beside
the code.

## Development Commands

| Command | Description |
|---------|-------------|
| `go run ./apps/native` | Run the native app (needs `droid` on PATH) |
| `go test ./apps/native/...` | Go unit tests and the view tests of both windows |
| `go vet ./apps/native/...` | Once each with `GOOS=darwin`, `windows` and `linux`, as the PR workflow does |
| `go tool -modfile=apps/native/go.mod mygo build apps/native` | Build the native app (`.app` and `.dmg`; `-platform windows/amd64` for Windows) |
| `(cd apps/native && go tool mygo build -platform ios/simulator phone)` | Build the Phone App for the iOS simulator |
| `pnpm install:phone` | Build the Phone App and install it on the connected iPhone |
| `pnpm lint` / `pnpm lint:fix` | oxlint |
| `pnpm format` / `pnpm format:check` | oxfmt |
| `pnpm check` | format check + lint |

## Validation Workflow

Before committing run `pnpm check`, and for Go changes
`"$(go env GOROOT)/bin/gofmt" -l apps/native`, `go vet` for darwin, windows and linux,
and `go test` over `./apps/native/...` plus `go test -cpu 1 ./apps/native/internal/app`
(as the PR workflow does: on one CPU a view test that races the Fake Daemon's answers fails
there, not in a Release; the workflow runs the view tests in shards of that binary, which
cover the same tests). Format with that gofmt, which is go.mod's
Go: a `gofmt` on PATH from another Go lays out `pkg/l10n/zh.go` differently, and the PR
workflow rejects it (`go test ./apps/native` checks this too). The Phone App's views run in
the same binary (`apps/native/internal/app/phone_test.go`), and the PR workflow builds the app
for the iOS simulator (`go tool mygo build -platform ios/simulator phone`, from
`apps/native`). For what only an iPhone can show, walk `apps/native/phone/DEVICE-CHECKLIST.md`.

What must never break is code, not prose: `.claude/settings.json` and `.claude/hooks/` keep
keys out of reach and out of commits, refuse force pushes, pushes and commits to main, branch
switches in the main checkout and committed local pins, and run the checks above before a
Claude Code turn may end. `.claude/hooks/test.sh <main checkout> <worktree>` checks the hooks.

## droid-sdk-go

The Go SDK (`github.com/kkkk2323/droid-sdk-go`) and the Fake Daemon (which the SDK's
`fakedaemon` package embeds) come from
[droid-sdk-go](https://github.com/kkkk2323/droid-sdk-go) at a tag. To change either, change
that repository (its README says how to rebuild the Fake Daemon), tag a release, then move the
pin here to it: `apps/native/go.mod` (`go get github.com/kkkk2323/droid-sdk-go@<tag>` from
`apps/native`). While working on both, `go work use <checkout>` uses a local checkout; do not
commit it.

## Releases and performance

- Releasing: `scripts/release.sh <version>`, after the changelog; it bumps the native app's and
  the Phone App's versions. Steps in `apps/native/docs/releases.md`.
- CPU, memory or frame cost: how to measure and profile is in `apps/native/docs/performance.md`.

## Code Style

- Go: `gofmt`; no cgo in the desktop app (MyGo builds it with `CGO_ENABLED=0`), while the iOS
  Phone App links its Go with Xcode's clang (mygo.json's `ios` section); platform code in
  `_windows.go` / `_other.go` (or `_darwin.go`) pairs
- JavaScript: the few `.mjs` scripts (the icons, droid's model export); `pnpm format` and
  `pnpm lint` cover them
- The system's interface font for UI and JetBrains Mono for code (vendored as TTF in
  `apps/native/assets/fonts`; Geist stays for Settings → Font, ADR 0025). Sizes, radii and
  control heights come from `pkg/theme` and `pkg/kit`, and so do colours: `theme.Tokens`, or
  `dashkit.Palette` in a dashboard. Do not hard-code a colour in a view; add a token. A size
  that differs between Settings → Density's Compact and Comfortable is `k.Dense(compact, comfortable)`
  (ADR 0026).
- File naming: `snake_case` for Go files, kebab-case for the JavaScript ones
- Git: Conventional Commits

## Testing

- The view tests run `App.View` in MyGo's `ui.NewTester` (no window; click,
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
- The Phone App's views are the same `App.View` with `Config.Phone`, on a 402×874 screen
  (`apps/native/internal/app/phone_test.go`), so `go test ./apps/native/...` covers them.
- On a simulator the app opens on its pairing page: `DROI_PHONE_DEV=1 go test
  ./apps/native/internal/app -run TestPhoneDevGateway -v -timeout 0` serves the README scenario
  through a Gateway at `http://127.0.0.1:41499`, which it pairs with as
  `http://127.0.0.1:41499/#pair=phone-dev-token`. To drive the window itself (macOS, Xcode):
  `(cd apps/native && GOFLAGS=-tags=e2e go tool mygo build -debug -platform ios/simulator phone)`,
  then `SIMCTL_CHILD_MYGO_AUTOMATION=/tmp/droi-phone.sock xcrun simctl launch booted
  com.kkkk2323.droi` and `mygo-automation open --socket /tmp/droi-phone.sock` (`go install
  github.com/egoist/mygo/plugins/automation/cmd/mygo-automation` from `apps/native`).
- E2E tests never need a Factory API key; they run against the Fake Daemon
- Live tests against a real Daemon, through the local droid-proxy (`dp`) with a cheap model.
  The key comes from the user: an agent never reads `~/.config/dp/keys.txt`; it asks for one.
  `FACTORY_API_KEY=<key> FACTORY_API_BASE_URL=$(dp status | awk '/baseURL/ {print $2}') go test ./apps/native/pkg/host ./apps/native/pkg/gateway ./apps/native/internal/hostrun -run Live -v -timeout 30m`
  (`DROI_LIVE_MODEL` defaults to `glm-5.3-flash`; the Daemon runs in a throwaway HOME so the key need not own this computer's Factory registration).
  `internal/hostrun`'s run the Host's Automations and Workstreams through its Gateway on Droi's droid
  (the newest under `~/Library/Application Support/Droi/droid/`; `DROID_PATH` picks another), macOS and Linux only

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
