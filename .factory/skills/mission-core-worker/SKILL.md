---
name: mission-core-worker
description: Implements Mission protocol, backend/session plumbing, Electron IPC, store reconciliation, and restore logic for Mission GUI
---

# Mission Core Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the WORK PROCEDURE.

## When to Use This Skill

Use this skill for features that modify:

- `src/shared/` protocol and Mission types
- backend JSON-RPC/session management under `src/backend/`
- Electron main/preload Mission IPC
- session persistence/storage logic
- renderer Mission state/reducer/restore logic that is primarily data/recovery oriented rather than visual UI
- focused tests for protocol, persistence, watcher, reducer, and recovery behavior

## Work Procedure

1. **Read the feature plus the mission contract first.** Review `mission.md`, `validation-contract.md`, mission `AGENTS.md`, the repo root `AGENTS.md`, and the relevant OpenSpec files before touching code.

2. **Map the state boundaries before editing.** Identify:
   - the canonical Mission metadata fields involved
   - which source of truth is authoritative (`load_session`, Mission notifications, missionDir, or persisted session metadata)
   - which normal-session paths must remain backward compatible

3. **Write failing tests first (red).** Add or extend the narrowest test files that prove the feature's intended behavior. Prefer existing files such as:
   - `test/interactionModeHotSwitch.test.ts`
   - `test/sessionStore.test.ts`
   - `test/rpcNotificationMapping.test.ts`
   - new focused Mission watcher/recovery tests if needed

4. **Run the new/updated tests and capture the failing state** before implementation.

5. **Implement the minimum code to make the tests pass (green).** Keep changes aligned with existing abstractions; do not create parallel Mission-only infrastructure if the existing session pipeline can be extended safely.

6. **Verify precedence and edge cases explicitly.** For Mission core features, always check:
   - Mission sessions are not downgraded by generic settings updates
   - Mission restore/bootstrap does not flicker to a normal session
   - validator injection does not imply Mission completion
   - stale worker metadata does not create false running UI/state

7. **Run focused validators, then repo-safe validation for touched surfaces.** At minimum run:
   - targeted tests you changed
   - `pnpm typecheck`
   - any additional targeted test file(s) needed to cover adjacent Mission recovery behavior

8. **Do one sanity check that crosses the code boundary.** If the feature affects visible Mission behavior, perform a lightweight end-to-end or restore sanity check (for example, replay a restore path or launch the app and confirm the relevant Mission state appears). Record exactly what you checked.

9. **Return a concrete handoff.** Be explicit about what state source wins, what files changed, what tests were added, and what edge cases remain risky.

## Example Handoff

```json
{
  "salientSummary": "Added explicit Mission protocol metadata to create/save/load flows, blocked downgrade attempts on later settings updates, and wired kill-worker plumbing through the backend/session manager stack. Targeted Mission persistence tests now pass and a paused Mission restore no longer flickers back to a normal session.",
  "whatWasImplemented": "Updated shared protocol/session types plus backend JSON-RPC session plumbing so Mission sessions persist `isMission`, `sessionKind`, `interactionMode`, `autonomyLevel`, and `decompSessionType`, preserve those fields across later sends/settings updates, and expose `kill_worker_session` through the manager/runner layers. Extended persistence/reducer tests to cover Mission restore precedence and non-downgrade behavior.",
  "whatWasLeftUndone": "I did not implement MissionPage UI or sidebar Mission indicators; those remain for the UI worker features.",
  "verification": {
    "commandsRun": [
      {
        "command": "node --test --experimental-strip-types test/interactionModeHotSwitch.test.ts",
        "exitCode": 0,
        "observation": "Mission-specific create/update behavior passes, including non-downgrade checks."
      },
      {
        "command": "node --test --experimental-strip-types test/sessionStore.test.ts",
        "exitCode": 0,
        "observation": "Mission metadata now saves and reloads without inferring from autoLevel alone."
      },
      {
        "command": "pnpm typecheck",
        "exitCode": 0,
        "observation": "Shared, backend, preload, and renderer type contracts remain valid."
      }
    ],
    "interactiveChecks": [
      {
        "action": "Replayed a paused Mission restore path after the code changes",
        "observed": "The restored session stayed Mission-typed and used Mission metadata instead of transient generic settings."
      }
    ]
  },
  "tests": {
    "added": [
      {
        "file": "test/sessionStore.test.ts",
        "cases": [
          {
            "name": "persists explicit Mission metadata across save/load",
            "verifies": "Mission sessions reload with `interactionMode`, `autonomyLevel`, and `decompSessionType` intact."
          }
        ]
      },
      {
        "file": "test/rpcNotificationMapping.test.ts",
        "cases": [
          {
            "name": "generic settings_updated cannot downgrade Mission state",
            "verifies": "Mission state sources outrank transient generic settings during bootstrap/recovery."
          }
        ]
      }
    ]
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- The feature depends on undocumented Mission RPC/tool payloads you cannot confirm from current code/spec.
- A needed runtime behavior would require changing Web/LAN mission scope or violating the Electron-only boundary.
- Existing daemon/session behavior contradicts the contract and you cannot determine the intended precedence.
- You cannot validate a restore/watcher edge because required local runtime inputs are unavailable or external auth is broken.
