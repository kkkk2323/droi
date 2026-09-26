<coding_guidelines>
# Repository Guidelines

Droi is a thin Client of the Droid Daemon. Read `CONTEXT.md` for the vocabulary
(Daemon, Desktop Shell, Gateway, Client, Pairing Token, Fake Daemon, ...) and
`docs/adr/` for the decisions behind the layout. Use those names in code,
comments, commits and issues.

## Project Structure

```
apps/
├── desktop/    # Desktop Shell and web Client (package "droi"; its version is the
│   │           # release version, which the Phone App reads too)
│   ├── src/main/      # Desktop Shell: Electron main process (Daemon lifecycle, Gateway, window)
│   ├── src/preload/   # Minimal bridge; conversation data never crosses it
│   ├── src/renderer/  # web Client: React app, runs as Local Client and Remote Client
│   ├── src/shared/    # Types and pure helpers shared by Desktop Shell and Client
│   └── resources/     # App icons; electron-builder config lives in package.json
└── mobile/     # Phone App: Expo SDK 57 iPhone Client (expo-router under src/app,
                # screens under src/screens, hardware behind src/platform with
                # *.web.ts stand-ins for the tests); DEVICE-CHECKLIST.md
packages/
└── daemon-layer/  # @droi/daemon-layer: the Clients' shared daemon layer (connection,
                   # Session state hooks, Gateway contract, local preferences); no DOM
                   # or Electron, the host supplies storage, focus and sound
tests/          # @droi/tests: the Playwright suites, one config per folder
├── fake-daemon/   # The Fake Daemon both Fake Daemon suites share
├── web/           # Client in a browser against the Fake Daemon
├── mobile/        # The Phone App's web build against the Fake Daemon
├── electron/      # Smoke suite against the built Desktop Shell
└── live/          # One case against a real Daemon
scripts/        # install:mac and install:phone
docs/           # ADRs, agent docs, the README screenshot
```

The repository is a pnpm workspace; every command below runs from the root and
covers the packages too. The root holds only workspace tooling (oxlint, oxfmt,
vitest, TypeScript); each app declares its own dependencies. Unit tests sit
next to the code as `*.test.ts` / `*.test.tsx` and run with vitest from the root.

## Development Commands

| Command | Description |
|---------|-------------|
| `pnpm dev` | Start the Desktop Shell in development mode |
| `pnpm dev:client` | Serve only the Client in a browser (vite) |
| `pnpm build` | Production build (electron-vite) |
| `pnpm build:mac` | Build the macOS DMG |
| `pnpm install:mac` | Build for this Mac's architecture only and replace `/Applications/Droi.app` (quits, swaps, relaunches) |
| `pnpm test` | Run vitest once |
| `pnpm test:e2e` | Run Playwright against the Client dev server (Fake Daemon) |
| `pnpm test:e2e:phone` | Export the Phone App for web and run Playwright against it (Fake Daemon) |
| `pnpm install:phone` | Build a signed Release of the Phone App and install it on the connected iPhone |
| `pnpm test:smoke` | Electron smoke suite against the built Shell (`pnpm build` first) |
| `pnpm test:live` | One case against a real Daemon; skips unless `FACTORY_API_KEY` is set |
| `pnpm typecheck` | TypeScript validation (root + Desktop Shell node/web + Phone App + tests) |
| `pnpm lint` / `pnpm lint:fix` | oxlint |
| `pnpm format` / `pnpm format:check` | oxfmt |
| `pnpm check` | format check + lint + typecheck |

## Validation Workflow

Before committing run `pnpm check && pnpm test`. Run `pnpm test:e2e` when the
web Client, the shared daemon layer or the Fake Daemon changed, and
`pnpm test:e2e:phone` when the Phone App, the shared daemon layer or the Fake
Daemon changed. The PR workflow runs all of them. For what only an iPhone can
show, walk `apps/mobile/DEVICE-CHECKLIST.md`.

The Phone App is pinned to Expo SDK 57 (ADR 0006). Expo changes its APIs every
SDK: read the versioned docs (`https://docs.expo.dev/versions/v57.0.0/`) or the
installed types before using an Expo module, and add Expo packages with
`npx expo install` from `apps/mobile` so the versions match the SDK.

## Code Style

- TypeScript strict, `verbatimModuleSyntax`, `noUncheckedIndexedAccess`
- Client imports use the `@/` alias for `apps/desktop/src/renderer/src`; the shared layer is imported
  as `@droi/daemon-layer/<module>` and uses relative imports inside itself
- Tailwind CSS 4 with the CSS variables in `apps/desktop/src/renderer/src/styles/global.css`
- Geist Sans for UI, Geist Mono for code (vendored in `apps/desktop/src/renderer/src/assets/fonts`)
- Icons: Lucide React
- File naming: kebab-case for components, camelCase for utilities
- Git: Conventional Commits

## Testing

- Prefer `getByRole` / `getByLabel` selectors in Playwright; add `data-testid` only when no accessible name fits
- E2E tests never need a Factory API key; they run against the Fake Daemon
- README screenshot: `DROI_SCREENSHOT=1 pnpm test:e2e --project=desktop screenshot.spec.ts`
  writes `docs/screenshot.png` (skipped in a normal run)
- Live test through the local droid-proxy (`dp`) with a cheap model:
  `FACTORY_API_KEY=$(grep -m1 '^fk-' ~/.config/dp/keys.txt) FACTORY_API_BASE_URL=$(dp status | awk '/baseURL/ {print $2}') pnpm test:live`
  (`DROI_LIVE_MODEL` defaults to `glm-5.3-flash`; the Daemon runs in a throwaway HOME so the key need not own this computer's Factory registration)

## Factory login

The Desktop Shell signs in with Factory using the `droid` CLI's device flow (ADR 0005,
`apps/desktop/src/main/factory-auth.ts`). While signed in the Daemon is started without `FACTORY_API_KEY`
and runs as the CLI's login, so `droid login` on this computer must be the same account.

## Factory API base URL

The Desktop Shell passes `FACTORY_API_BASE_URL` to the Daemon when the Settings page has a
"Factory API base URL" (or the Shell's own environment sets it). Point it at droid-proxy
(`dp status` → baseURL) to route the Daemon's Factory traffic through the proxy.
</coding_guidelines>
