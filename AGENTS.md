<coding_guidelines>
# Repository Guidelines

Droi is a thin Client of the Droid Daemon. Read `CONTEXT.md` for the vocabulary
(Daemon, Desktop Shell, Gateway, Client, Pairing Token, Fake Daemon, ...) and
`docs/adr/` for the decisions behind the layout. Use those names in code,
comments, commits and issues.

## Project Structure

```
src/
├── main/       # Desktop Shell: Electron main process (Daemon lifecycle, Gateway, window)
├── preload/    # Minimal bridge; conversation data never crosses it
├── renderer/   # Client: React app, runs as Local Client and Remote Client
└── shared/     # Types and pure helpers shared by Desktop Shell and Client
e2e/            # Playwright tests: Client in a browser against the Fake Daemon
legacy/         # Previous implementation, kept for reference only; excluded
                # from build and checks; deleted by the last rebuild ticket
```

Unit tests sit next to the code as `*.test.ts` / `*.test.tsx` and run with vitest.

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
| `pnpm test:smoke` | Electron smoke suite against the built Shell (`pnpm build` first) |
| `pnpm test:live` | One case against a real Daemon; skips unless `FACTORY_API_KEY` is set |
| `pnpm typecheck` | TypeScript validation (node + web) |
| `pnpm lint` / `pnpm lint:fix` | oxlint |
| `pnpm format` / `pnpm format:check` | oxfmt |
| `pnpm check` | format check + lint + typecheck |

## Validation Workflow

Before committing run `pnpm check && pnpm test`. Run `pnpm test:e2e` when the
Client or the Fake Daemon changed. The PR workflow runs all three.

## Code Style

- TypeScript strict, `verbatimModuleSyntax`, `noUncheckedIndexedAccess`
- Client imports use the `@/` alias for `src/renderer/src`
- Tailwind CSS 4 with the CSS variables in `src/renderer/src/styles/global.css`
- Geist Sans for UI, Geist Mono for code (vendored in `src/renderer/src/assets/fonts`)
- Icons: Lucide React
- File naming: kebab-case for components, camelCase for utilities
- Git: Conventional Commits

## Testing

- Prefer `getByRole` / `getByLabel` selectors in Playwright; add `data-testid` only when no accessible name fits
- E2E tests never need a Factory API key; they run against the Fake Daemon
- README screenshot: `DROI_SCREENSHOT=1 pnpm test:e2e --project=desktop e2e/screenshot.spec.ts`
  (skipped in a normal run)
- Live test through the local droid-proxy (`dp`) with a cheap model:
  `FACTORY_API_KEY=$(grep -m1 '^fk-' ~/.config/dp/keys.txt) FACTORY_API_BASE_URL=$(dp status | awk '/baseURL/ {print $2}') pnpm test:live`
  (`DROI_LIVE_MODEL` defaults to `glm-5.3-flash`; the Daemon runs in a throwaway HOME so the key need not own this computer's Factory registration)

## Factory login

The Desktop Shell signs in with Factory using the `droid` CLI's device flow (ADR 0005,
`src/main/factory-auth.ts`). While signed in the Daemon is started without `FACTORY_API_KEY`
and runs as the CLI's login, so `droid login` on this computer must be the same account.

## Factory API base URL

The Desktop Shell passes `FACTORY_API_BASE_URL` to the Daemon when the Settings page has a
"Factory API base URL" (or the Shell's own environment sets it). Point it at droid-proxy
(`dp status` → baseURL) to route the Daemon's Factory traffic through the proxy.
</coding_guidelines>
