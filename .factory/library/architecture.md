# Architecture

Architectural decisions and patterns relevant to the Mission GUI mission.

---

## Mission session model

- Mission is a **session kind**, not a workspace mode.
- Mission sessions must preserve explicit protocol metadata across create/send/update/save/load:
  - `isMission`
  - `sessionKind`
  - `interactionMode`
  - `autonomyLevel`
  - `decompSessionType`
- Mission sessions must not be downgraded by later generic settings updates.

## Renderer state model

- Per-session state lives in the existing `sessionBuffers` / `SessionBuffer` architecture.
- Mission state should extend the session-oriented model instead of creating a parallel global store unless a feature explicitly requires it.
- Mission Chat view must reuse the existing conversation shell behaviors (permission, ask-user, todo, debug trace) while Mission Control adds Mission-specific panels.

## Mission truth sources and precedence

- Bootstrap first from persisted Mission metadata plus `droid.load_session` Mission snapshot.
- Mission-specific notifications are the primary incremental state source.
- `tool_progress_update(StartMissionRun)` is supplemental only; it can enrich UI hints but must not outrank Mission truth sources.
- missionDir disk data is the durable recovery/correction source after bootstrap.
- Generic session updates such as transient `settings_updated` must not visibly downgrade Mission identity.

## Mission disk reconciliation

- `state.json` -> newer state snapshot wins
- `features.json` -> replace with latest feature snapshot
- `progress_log.jsonl` -> append + dedupe
- `handoffs/` -> additive merge
- validator-related files may appear later than the initial Mission directory

## File watching constraints

- Electron-only missionDir watching via Node.js `fs.watch`
- 2-second polling fallback is required
- Do not introduce new watcher dependencies like `chokidar`

## Routing & UI semantics

- Mission sessions open on `/mission`; non-Mission sessions stay on `/`
- MissionPage toggles between Chat and Mission Control for the same orchestrator session
- Auto-switch rules:
  - `running` -> Mission Control
  - `paused` / `orchestrator_turn` -> Chat
  - manual user toggle suppresses auto-switch for 30 seconds
- The first implementation worker completion does **not** mean the Mission is complete; validator injection can keep the Mission running
