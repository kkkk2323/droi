# Mission Runtime

Mission-specific runtime notes that workers should keep in mind while implementing and validating.

---

## Mission directory lifecycle

- `missionDir` may not exist immediately after Mission session creation.
- `handoffs/` usually appears only after the first worker completes.
- validator-related files such as `validation-state.json` may arrive later in the Mission lifecycle.
- Recovery logic must tolerate these files appearing incrementally.

## Authoritative state order

1. Persisted Mission metadata + `droid.load_session`
2. Mission-specific notifications (`mission_*`)
3. missionDir reconciliation
4. `tool_progress_update(StartMissionRun)` as supplemental UI hints only
5. Generic session notifications are non-authoritative for Mission identity

## Completion rules

- Do not infer Mission completion from the first `worker_completed` event.
- Do not infer Mission completion from the presence of a handoff file alone.
- Do not infer Mission completion from the absence of an active worker alone.
- Validator feature injection can keep a Mission in `running` after the first implementation worker finishes.

## Recovery edge cases to preserve

- Stale `currentWorkerSessionId` values must not create false live-worker UI after restart.
- Disk-only recovery should still rebuild a coherent Mission if `droid.load_session` is unavailable or stale.
- Mission restore must not flicker back to a normal session because of transient generic settings updates.

## Worker kill contract

- The renderer/preload/shared contract for killing a Mission worker is `killWorkerSession({ sessionId, workerSessionId })`.
- IPC rejects missing `sessionId` or `workerSessionId`; only show the Kill Worker action when a concrete current worker session id is known.
- The backend uses `sessionId` only to route to the correct managed session. The JSON-RPC request sent to Droid is `droid.kill_worker_session` with payload `{ workerSessionId }`.
- The JSON-RPC session trims `workerSessionId` and no-ops on an empty string, so validation and UI state should prevent empty-worker kill attempts from being presented as actionable.
