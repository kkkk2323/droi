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
2. If this is a re-run, clear stale isolated Electron listeners before restarting:
   - Use the `.factory/services.yaml` `electron-dev.stop` command to clean up ports `9222`, `5173`, and `3002` before the next launch.
3. Start the isolated Electron dev app:
   - `DROID_APP_DATA_DIR=/tmp/droi-mission-e2e DROID_APP_API_PORT=3002 ELECTRON_REMOTE_DEBUGGING_PORT=9222 pnpm dev:test`
4. Connect automation with `agent-browser connect 9222`.
5. Wait for `document.body.hasAttribute("data-app-ready") === true`.
6. Select the `Mission-GUI-TEST` project.
7. For any new Mission flow exercised with `agent-browser`, explicitly choose `Custom -> CCH-GPT-5.4` before the first Mission message.

## Recommended automation flow

- Use `agent-browser` for Electron UI interaction and screenshots.
- Prefer annotated screenshots for UI proof.
- Re-snapshot after route changes, toggles, dialogs, or progress-state transitions.
- Reuse a single browser session for each validation run.

## Flow Validator Guidance: Electron Mission GUI

- Treat the Electron Mission GUI as a **single-user surface** for this repo snapshot. Do **not** run multiple Mission browser flows against the same live app instance in parallel.
- For this milestone, validator groups must run **sequentially** unless each group launches its own isolated Electron app with a unique `DROID_APP_DATA_DIR`, CDP port, and API port.
- Stay inside the pre-created `Mission-GUI-TEST` project and avoid touching other projects or sessions that do not belong to your assigned namespace.
- Use your assigned namespace in any new Mission prompt text or session naming so resulting sessions can be identified and cleaned up if needed.
- Do not delete or modify sessions created by another validator group.
- When verifying routing, use the real sidebar/session entry points instead of manually editing the URL bar.
- When verifying persisted Mission identity, prefer evidence from the live UI plus the isolated temp data written under your assigned `DROID_APP_DATA_DIR`; do not inspect or modify the user's real application data.
- Do not rely on Web/LAN mode, OS-native project pickers, or the existing Droi instance on port `3001`.

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
- The shared live `Mission-GUI-TEST` validator-phase Mission session should be treated as read-only during user testing; destructive Pause/Kill Worker flows and optional Mission-permission permutations are safer to validate with the copied Mission snapshot or isolated in-memory renderer-state injection.
- This Electron build uses TanStack memory-history, so `/mission` versus `/` proof should come from router state plus Mission DOM markers rather than the browser address bar.
- Repeated isolated Electron validation runs can fail if stale listeners are still bound to `9222`, `5173`, or `3002`; run the manifest stop command before restarting the app.
- In the Electron surface, direct automation clicks on the visible send button can be flaky; targeting the real `[data-testid="chat-send"]` element is more reliable when the UI is visibly ready.
