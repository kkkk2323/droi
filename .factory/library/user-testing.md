# User Testing

Testing surface, setup steps, and isolation rules for the Mission GUI mission.

---

## Primary testing surface

- **Application type:** Electron desktop app
- **Mission validation surface:** Electron only
- **Dev command:** `DROID_APP_DATA_DIR=/tmp/droi-mission-e2e DROID_APP_API_PORT=3002 ELECTRON_REMOTE_DEBUGGING_PORT=9222 pnpm dev:test`
- **Renderer URL (health only):** `http://127.0.0.1:5173`
- **CDP port for automation:** `9222`
- **Local API fallback used by the Electron run:** `3002`

## Required isolation rules

- Any Mission-invoking manual or automated E2E must use the pre-created project **`Mission-GUI-TEST`**.
- Any Mission-invoking **agent-browser** manual or automated E2E must use **Custom -> `CCH-GPT-5.4`**.
- Use a copied temporary `DROID_APP_DATA_DIR` rooted at `/tmp/droi-mission-e2e`.
- Do **not** validate Mission flows through Web/LAN mode.
- Do **not** use the existing Droi instance on `127.0.0.1:3001`.
- Do **not** depend on OS-level directory picker dialogs as part of the Mission validation flow.

## Setup steps for Mission E2E

1. Ensure `/tmp/droi-mission-e2e/app-state.json` exists.
   - `init.sh` seeds this automatically when possible from the user’s current Droi app-state.
   - If a fresh copy is needed, copy `~/Library/Application Support/droi/app-state.json` to `/tmp/droi-mission-e2e/app-state.json`.
2. Start the isolated Electron dev app:
   - `DROID_APP_DATA_DIR=/tmp/droi-mission-e2e DROID_APP_API_PORT=3002 ELECTRON_REMOTE_DEBUGGING_PORT=9222 pnpm dev:test`
3. Connect automation with `agent-browser connect 9222`.
4. Wait for `document.body.hasAttribute("data-app-ready") === true`.
5. Select the `Mission-GUI-TEST` project.
6. For any new Mission flow exercised with `agent-browser`, explicitly choose `Custom -> CCH-GPT-5.4` before the first Mission message.

## Recommended automation flow

- Use `agent-browser` for Electron UI interaction and screenshots.
- Prefer annotated screenshots for UI proof.
- Re-snapshot after route changes, toggles, dialogs, or progress-state transitions.
- Reuse a single browser session for each validation run.

## Key Mission checks

- Session creation and route split: Mission -> `/mission`, normal -> `/`
- Mission Chat / Mission Control toggle and 30-second manual override cooldown
- Feature queue ordering and validator feature injection
- Timeline append/dedupe behavior and handoff persistence
- Pause, daemon failure, daemon retry, and kill-worker distinctions
- Restore/reselection into existing Mission sessions after restart

## Known quirks

- `start_mission_run` permission may appear in some environments and be absent in others; both are valid and must be handled.
- Mission directories and handoff/validation files may appear later than the initial Mission startup.
- `droid.load_session` is more trustworthy than transient generic `settings_updated` for Mission identity.
- Some recovery checks are best validated with targeted automated tests plus Electron sanity checks rather than pure browser-only assertions.
