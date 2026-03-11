---
name: mission-ui-finish-worker
description: Finishes an already-started Mission renderer feature by validating the existing draft, applying minimal follow-up fixes, and committing.
---

# Mission UI Finish Worker

Use this skill only when a Mission UI feature already has substantial uncommitted implementation work in the working tree and prior workers timed out during validation or manual verification.

## Procedure

1. Treat the current working tree as authoritative draft state. Do not restart broad architecture exploration.
2. Read the assigned feature, the claimed validation assertions, and inspect the existing diff before making any changes.
3. Limit edits to the smallest set needed to make the draft pass validators and satisfy the claimed assertions.
4. Prefer validator-first closure:
   - run `pnpm check`
   - run `pnpm test -- --test-concurrency=7`
   - if validators fail, make only the focused fixes required and rerun
5. Perform at most one minimal isolated Electron sanity pass when the final fix changes visible Mission behavior. Reuse Mission-GUI-TEST and Custom -> CCH-GPT-5.4 constraints.
6. If you open agent-browser, always close the specific session before ending the feature.
7. Commit only when validators pass and the working tree is clean aside from the intended feature files.

## Important Constraints

- Do not re-scope the feature.
- Do not add unrelated refactors.
- Do not spend time re-deriving already-implemented behavior from first principles.
- If the draft appears incomplete in a way that cannot be closed quickly, return to orchestrator with a precise list of missing pieces instead of timing out.

## Handoff Expectations

- State whether the draft was already mostly complete when you started.
- List the exact validator failures you fixed.
- List commands run and their exit codes.
- Note whether an Electron sanity pass was needed or intentionally skipped because no final visible changes remained.
