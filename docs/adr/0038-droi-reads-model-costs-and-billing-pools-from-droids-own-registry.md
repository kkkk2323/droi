---
status: accepted
---

# Droi reads model costs and billing pools from droid's own registry

The Observe page estimates each request's Credits from per-model weights, and the Key Pool routes
a request to a key's Standard or Core pool by its model. Both came from tables in Droi's code:
a model Factory added (Gemini 4 Carbon, Kimi K3.1 Pro, ...) showed as unpriced, or was kept off Core,
until a Droi release added a row. The Daemon's `list_models` gives only the input multiplier, and
Factory's responses give tokens, not Credits. Droi's droid (ADR 0033), the build that sends the
requests, carries the whole registry: each model's multipliers, its promotions and its billing pool.

We decided:

- **Droi asks the build, once per build.** `droidhook.Store.Models` runs `droid --version` with
  `model-export.mjs` preloaded through `BUN_OPTIONS`, the way the Key Hook loads, and keeps what it
  writes as `models.json` next to the build. The export runs in that short process only: the
  Daemon's droid never loads it, so droid's own start-up order is not touched. No request leaves
  the computer.
- **The registry is found by its shape, not its name.** Bun bundles droid into modules whose file
  and export names change with every build. The export reads the modules beside `Bun.main`, imports
  those holding `cost:{tokenMultiplier:`, and takes the largest exported object whose values have an
  `id` and a numeric `cost.tokenMultiplier`. The output multipliers by provider, which the module
  keeps to itself, are read from their literal; built-in defaults stand in when they are not found.
- **An export is used only when it looks whole.** Fewer than 30 models, or an error, and Droi keeps
  its own tables (`StandardTokenWeights`, `coreModelIDs`), as before.
- **What it reads replaces the tables.** A request is priced at the multiplier droid would have used
  at that request's time, its promotion included; droid's Core models route to Core, and the
  built-in Core list is no longer consulted.

## Considered Options

- **Parse the registry out of the executable's bytes.** It worked for multipliers, but promotions
  are often variables (`promotions:[ti]`) that only running the code resolves.
- **The Daemon's `tokenMultiplier` with guessed output and cache weights.** Gemini 4 Carbon's output
  would have been 20% off, and no billing pool.
- **A table Droi publishes and downloads.** Still a table someone keeps up to date, and late for a
  droid that was already updated.

## Consequences

- A new model is priced, with its promotions, and routed to its pool as soon as Droi runs a droid
  that offers it, with no Droi release.
- The export depends on droid keeping its registry as exported objects with `id` and `cost`. A droid
  that changes that falls back to the built-in tables; `TestExportModelsFromDroid` and
  `TestRealDroidMatchesTheTable` show it on a computer that has Droi's droid.
- Request estimates now include promotions; the Session footer's Credits, which the Daemon reports,
  already did.
