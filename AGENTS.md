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
| `pnpm test` | Run vitest once |
| `pnpm test:e2e` | Run Playwright against the Client dev server |
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
</coding_guidelines>
