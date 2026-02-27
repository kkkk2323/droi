---
name: ui-worker
description: Implements UI fixes in the Electron + React renderer layer
---

# UI Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the WORK PROCEDURE.

## When to Use This Skill

Use for features that modify React components, Zustand store selectors, CSS/Tailwind styling, and scroll behavior in the renderer layer (`src/renderer/src/`).

## Work Procedure

1. **Read the feature description carefully.** Identify all files mentioned and understand the expected behavior.

2. **Read all relevant source files** before making any changes. Understand the current implementation, imports, and patterns used.

3. **Plan your changes.** For each file, identify what needs to change and why. Consider edge cases.

4. **Implement changes incrementally.** Make one logical change at a time. After each change, verify it compiles:
   ```bash
   pnpm typecheck
   ```

5. **Run the full check pipeline:**
   ```bash
   pnpm format && pnpm lint:fix && pnpm check
   ```
   Fix any errors before proceeding.

6. **Manual verification** (where applicable):
   - For scroll behavior: describe what you expect to happen and why your change achieves it
   - For layout changes: describe the DOM structure before and after
   - For state changes: trace the data flow from store to component

7. **Commit your changes** with a conventional commit message.

## Example Handoff

```json
{
  "salientSummary": "Moved New Project button from SidebarContent to SidebarHeader and wrapped project list in ScrollArea. Ran pnpm check (pass). Sidebar header is now fixed, only project list scrolls.",
  "whatWasImplemented": "Restructured app-sidebar.tsx: extracted New Project SidebarMenuItem into SidebarHeader section above SidebarContent. Wrapped the project Collapsible list inside ScrollArea component. Adjusted CSS classes for proper flex layout.",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      { "command": "pnpm typecheck", "exitCode": 0, "observation": "No type errors" },
      { "command": "pnpm check", "exitCode": 0, "observation": "Format, lint, typecheck all pass" }
    ],
    "interactiveChecks": [
      { "action": "Reviewed DOM structure in code", "observed": "SidebarHeader contains New Project, SidebarContent contains ScrollArea with project list, SidebarFooter contains Settings" }
    ]
  },
  "tests": { "added": [] },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- If a required component or import doesn't exist
- If the existing code structure is significantly different from what the feature description assumes
- If pnpm check fails with errors you cannot resolve
- If changes would require modifying files outside the allowed scope (backend, preload, main process)
