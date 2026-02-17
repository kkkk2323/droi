# Repository Guidelines

## Project Structure

Electron + React + TypeScript desktop application for Droid CLI.

```
src/
├── main/           # Electron main process, IPC handlers
├── preload/        # Secure bridge APIs for renderer
├── renderer/       # React application (UI)
│   ├── components/ # UI components (shadcn/ui style)
│   ├── pages/      # Route pages
│   ├── hooks/      # React hooks
│   ├── lib/        # Utilities
│   └── state/      # Zustand state management
├── server/         # Local HTTP API server (Hono)
├── backend/        # Core business logic
│   ├── droid/      # Droid CLI process management
│   ├── git/        # Git integration
│   ├── keys/       # API key management
│   └── storage/    # Persistence layer
├── shared/         # Cross-process types and protocols
└── test/           # Test files (*.test.ts)
```

## Development Commands

| Command | Description |
|---------|-------------|
| `pnpm dev` | Start Electron in development mode |
| `pnpm dev:mobile` | Enable Web UI for LAN access (port 3001) |
| `pnpm dev:api` | Start API server only |
| `pnpm test` | Run Node.js test suite |
| `pnpm typecheck` | TypeScript validation (node + web) |
| `pnpm build` | Production build for Electron |
| `pnpm build:mac` | Build macOS DMG installer |

## Code Style

- **TypeScript**: Strict mode enabled across all configs
- **Imports**: Use path aliases (`@/components`, `@/lib`, `@/hooks`)
- **UI Components**: Base UI (Vega) + shadcn/ui patterns, located in `src/renderer/src/components/ui/`
- **Icons**: Lucide React
- **Styling**: Tailwind CSS 4.1 with CSS variables

## Testing

- **Framework**: Node.js built-in test runner (`node --test`)
- **Location**: `test/*.test.ts` files
- **TypeScript**: Uses `--experimental-strip-types` flag

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `FACTORY_API_KEY` | Factory API authentication |
| `DROID_WEB_ENABLED` | Enable HTTP server for LAN access |
| `DROID_APP_API_PORT` | API server port (default: 3001) |
| `DROID_TRACE_CHAIN` | Enable debugging traces |

## Conventions

- **File naming**: kebab-case for components (`app-sidebar.tsx`), camelCase for utilities
- **React**: Functional components with hooks, React 19 features
- **State**: Zustand for global state, TanStack Query for server state
- **Git**: Follow Conventional Commits specification
- **API**: Hono for HTTP routes, Zod (if needed) for validation
