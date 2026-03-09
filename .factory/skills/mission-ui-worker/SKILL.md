---
name: mission-ui-worker
description: Implements Mission renderer routing, MissionPage, Mission Control panels, and Mission-specific interaction UX in the Electron app
---

# Mission UI Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the WORK PROCEDURE.

## When to Use This Skill

Use this skill for features that modify:

- `src/renderer/src/router.tsx`
- Mission session entry UI, sidebar/session routing, and MissionPage
- Mission Control panels, status bar, feature queue, timeline, handoff cards
- Mission-specific PermissionCard/InputBar/status copy
- Mission-oriented renderer tests and Mission `data-testid` hooks

## Work Procedure

1. **Read the feature, contract, and testing constraints first.** Review `mission.md`, `validation-contract.md`, mission `AGENTS.md`, `.factory/library/user-testing.md`, and the relevant OpenSpec files.

2. **Identify every required test hook and visible state.** Before editing, list:
   - required `data-testid` values
   - route transitions that must be preserved
   - visible Mission states/copy differences (running, paused, daemon failure, killed by user, validator-injected running)

3. **Write failing tests first (red).** Prefer focused renderer/integration tests for the exact UI behavior being added. If a feature lacks a good test file, create one. Add failing assertions before changing the UI code.

4. **Implement the UI in small slices.** Reuse existing chat shell components instead of duplicating behavior. Preserve normal-session behavior while making Mission sessions route to `/mission` and render Mission-specific controls.

5. **After each slice, run the narrowest relevant checks.** Typical commands include:
   - the specific test file(s) you changed
   - `pnpm typecheck`

6. **Perform manual Electron sanity checks for every feature that changes visible Mission behavior.** When the feature invokes a Mission flow, you must:
   - use the `Mission-GUI-TEST` project
   - for any `agent-browser` testing flow, use Custom -> `CCH-GPT-5.4`
   - stay on Electron with isolated `DROID_APP_DATA_DIR`

7. **Capture route and state evidence, not just screenshots.** For routing/toggle/cooldown features, record the user action, resulting route/view, and the Mission state that triggered the transition.

8. **Verify boundary-sensitive copy.** Explicitly check the differences between:
   - normal chat vs Mission chat
   - user pause vs daemon failure vs user-killed worker
   - optional `start_mission_run` permission present vs absent

9. **Return a precise handoff.** Include exact test ids added/changed, manual UI checks performed, and any remaining UI state that still needs another feature.

## Example Handoff

```json
{
  "salientSummary": "Added `/mission` routing, MissionPage Chat/Mission Control toggle, Mission status bar, and Mission-specific sidebar/session navigation. Manual Electron checks confirmed Mission sessions open on `/mission`, normal sessions stay on `/`, and Chat/Mission Control switching preserves the same session.",
  "whatWasImplemented": "Updated the renderer route tree, SessionConfigPage, sidebar/session navigation, and MissionPage shell so Mission sessions are created and reopened on `/mission`, expose the required Mission test ids, preserve the existing chat shell in Mission Chat view, and support Chat/Mission Control toggling with state-driven auto-switch semantics.",
  "whatWasLeftUndone": "Feature queue/timeline/handoff panel content is still pending in a separate Mission Control feature.",
  "verification": {
    "commandsRun": [
      {
        "command": "node --test --experimental-strip-types test/missionRouting.test.ts",
        "exitCode": 0,
        "observation": "Mission route guard, sidebar routing, and session entry behavior pass."
      },
      {
        "command": "pnpm typecheck",
        "exitCode": 0,
        "observation": "Renderer route and component changes typecheck successfully."
      }
    ],
    "interactiveChecks": [
      {
        "action": "Launched Electron dev app with isolated data dir, selected `Mission-GUI-TEST`, chose Custom -> `CCH-GPT-5.4`, and created a Mission session",
        "observed": "The session opened on `/mission` and exposed the Mission toggle plus Mission status bar."
      },
      {
        "action": "Toggled between Chat and Mission Control for the same Mission session",
        "observed": "The active session id stayed constant and the existing chat shell remained visible in Mission Chat view."
      }
    ]
  },
  "tests": {
    "added": [
      {
        "file": "test/missionRouting.test.ts",
        "cases": [
          {
            "name": "mission sessions route to /mission while normal sessions route to /",
            "verifies": "Mission routing and route guards behave correctly for both session kinds."
          },
          {
            "name": "manual Mission view toggle preserves the active session",
            "verifies": "Chat and Mission Control are alternate views of the same orchestrator session."
          }
        ]
      }
    ]
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- The required UI depends on backend/session data that is not yet exposed in renderer state.
- You cannot complete the feature without changing Web/LAN scope.
- A required Mission E2E sanity check would need the OS project picker or a non-approved model/project.
- Permission or daemon behavior contradicts the contract and you cannot determine which UI copy/state is correct.
