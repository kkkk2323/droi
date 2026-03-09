---
name: mission-verification-worker
description: Expands Mission automated coverage, integration flows, and E2E-readiness for the Electron Mission GUI
---

# Mission Verification Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the WORK PROCEDURE.

## When to Use This Skill

Use this skill for features that primarily add or stabilize:

- Mission-focused automated tests
- restore/recovery/integration coverage
- Mission-specific test hooks and stable UI selectors
- E2E readiness for Electron validation flows

## Work Procedure

1. **Read the contract and testing library first.** Review `validation-contract.md`, mission `AGENTS.md`, `.factory/library/user-testing.md`, and `.factory/services.yaml` before deciding what to test.

2. **Translate assertions into tests deliberately.** Group related assertions into the smallest reliable test files. Prefer extending existing tests before creating broad new suites.

3. **Write failing tests first (red).** Add the failing Mission assertions before touching implementation code or test hooks.

4. **Stabilize the test surface only as needed.** If you add `data-testid` hooks or minor harness support, keep those changes tightly scoped to the assertions under test.

5. **Run targeted automated coverage iteratively.** Use the narrowest commands that prove the changed assertions, then widen to the relevant suite.

6. **For any Mission-invoking manual/E2E verification, obey the hard constraints:**
   - project: `Mission-GUI-TEST`
   - model: `gemini-3-flash-preview`
   - Electron surface only
   - isolated copied `DROID_APP_DATA_DIR`

7. **Collect evidence for the full flow, not just pass/fail.** For end-to-end Mission flows, record:
   - project and model used
   - route transitions
   - key Mission states
   - whether the flow involved validator injection, pause, daemon failure, or kill worker

8. **Run the feature’s final validation set before handoff.** For verification-heavy features, this typically includes:
   - targeted Mission test files
   - `pnpm test`
   - `pnpm typecheck`

9. **Return a handoff that makes gaps obvious.** If any assertion variant remains unproven (for example, user pause covered but daemon failure not covered), state that explicitly rather than implying the full contract area is complete.

## Example Handoff

```json
{
  "salientSummary": "Expanded Mission integration coverage for create/run/validator injection, pause/continue, daemon-failure recovery, and kill-worker flows. Added stable Mission test ids and confirmed Mission-invoking Electron checks use `Mission-GUI-TEST` plus `gemini-3-flash-preview` with an isolated copied data directory.",
  "whatWasImplemented": "Extended Mission-focused automated tests across interaction mode hot switching, session persistence, notification mapping, watcher/recovery paths, and Electron integration scenarios. Added stable Mission selectors needed by E2E and documented the constrained Electron validation path for Mission flows.",
  "whatWasLeftUndone": "I did not modify unrelated UI polish outside the assertions covered by the new integration/E2E tests.",
  "verification": {
    "commandsRun": [
      {
        "command": "node --test --experimental-strip-types test/interactionModeHotSwitch.test.ts test/sessionStore.test.ts test/rpcNotificationMapping.test.ts",
        "exitCode": 0,
        "observation": "Mission protocol persistence, restore precedence, and notification mapping scenarios pass."
      },
      {
        "command": "pnpm test",
        "exitCode": 0,
        "observation": "Full Node test suite passes with the Mission additions."
      },
      {
        "command": "pnpm typecheck",
        "exitCode": 0,
        "observation": "Type contracts remain valid after Mission test-surface changes."
      }
    ],
    "interactiveChecks": [
      {
        "action": "Launched the Electron dev app with `DROID_APP_DATA_DIR=/tmp/droi-mission-e2e`, selected `Mission-GUI-TEST`, and chose `gemini-3-flash-preview` for a new Mission flow",
        "observed": "Mission E2E started on the Electron surface without using Web/LAN mode or the OS project picker."
      },
      {
        "action": "Ran the kill-worker and restart/reselection sanity flows",
        "observed": "The paused Mission remained inspectable, reused the same Mission session context, and recovered through Mission entry points."
      }
    ]
  },
  "tests": {
    "added": [
      {
        "file": "test/missionIntegration.test.ts",
        "cases": [
          {
            "name": "create -> run -> validator injection -> completion",
            "verifies": "Mission stays active through validator injection and completes only at the final Mission state."
          },
          {
            "name": "daemon failure and user pause both resume through normal chat continuation",
            "verifies": "Paused Mission flows recover without recreating the session."
          },
          {
            "name": "kill worker transitions to mission_paused with user-kill semantics",
            "verifies": "Kill Worker does not look like infrastructure failure and keeps the Mission inspectable."
          }
        ]
      }
    ]
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- The needed assertions cannot be made reliable without broader product changes outside the feature scope.
- A Mission-invoking E2E path would require violating the project/model/data-dir constraints.
- You cannot make the test surface stable enough to prove the targeted assertions.
- External daemon/auth/runtime issues prevent reliable validation and cannot be fixed locally.
