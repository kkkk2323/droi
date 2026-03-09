# Environment

Environment variables, external dependencies, and setup notes for the Mission GUI mission.

**What belongs here:** Required env vars, external API keys/services, local data-dir expectations, platform-specific notes.
**What does NOT belong here:** Service ports/commands (use `.factory/services.yaml`).

---

## External dependencies

- Reuse the existing local Droid CLI + `droid daemon` / factoryd environment.
- Do not assume Redis or other auxiliary services are part of this mission.
- If daemon auth or external Factory connectivity is broken, return to the orchestrator instead of reconfiguring global user state.

## Mission E2E environment

- Use an isolated copied data directory at `/tmp/droi-mission-e2e` for Mission-invoking Electron E2E.
- `init.sh` seeds `/tmp/droi-mission-e2e/app-state.json` from the user’s existing Droi app-state when available.
- Use `DROID_APP_API_PORT=3002` and `ELECTRON_REMOTE_DEBUGGING_PORT=9222` for isolated Electron runs.

## Off-limits / constraints

- Do not use the existing Droi instance on `127.0.0.1:3001` for Mission validation.
- Do not touch Redis on `127.0.0.1:6379`.
- Mission validation remains Electron-only for this mission.

## User-provided validation constraints

- Any manual or automated E2E flow that invokes Mission must run in the `Mission-GUI-TEST` project.
- Any Mission-invoking E2E flow must use Gemini 3 Flash (`gemini-3-flash-preview`).

- `FACTORY_API_KEY` — Required for Droid CLI authentication
- Node.js + pnpm required
- Electron app — runs as desktop application via `pnpm dev`
