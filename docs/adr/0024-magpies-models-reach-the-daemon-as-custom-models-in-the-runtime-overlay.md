---
status: accepted
---

# Magpie's models reach the Daemon as Custom Models in the Runtime Overlay

Magpie ([yetone/magpie](https://github.com/yetone/magpie)) is a local gateway on `127.0.0.1:3425` that runs coding agents on the models of many providers behind one endpoint: it speaks OpenAI Chat Completions and Responses, Anthropic Messages and Gemini, translates between them, and answers the model list at `/v1/models`. Droi could not use it. The Daemon calls Factory's API, which no gateway speaks unless it speaks Factory's protocol, and `FACTORY_API_BASE_URL` only moves that traffic (the droid-proxy case); pointing it at Magpie fails. A Magpie model was therefore invisible to every Droi Session, and to the model picker.

What droid does support is its own: `customModels` in `~/.factory/settings.json`, each with a `model`, a `baseUrl`, an `apiKey` and a `provider` dialect, and `--settings <file>`, which merges a settings file for one process only. The Host already passes the second as the Runtime Overlay (ADR 0010), for Memory.

We decided:

- **The Host reads the gateway's model list and writes nothing else to it.** One `GET /v1/models` a minute at most, on a background read; a read that fails keeps the last list, so a restart of Magpie does not empty the picker. Droi sends Magpie no request for a Session: the Daemon does.
- **The models reach the Daemon through the Runtime Overlay, as `customModels`.** The overlay is now written when Memory or Magpie is on, and holds Memory's part only when Memory is. `~/.factory/settings.json` is not written: the droid CLI and the Factory App keep their own models, and droid merges the overlay's entries with a user's own, so those stay too.
- **The ids belong to Droi.** An entry is named `custom:magpie-<provider>-<model>-<hash>`: `custom:` because droid resolves a custom model only by an id that starts with it (an entry without it is listed under "Available custom models" and then ignored, and the Session runs another model without saying so), the provider because it names the company behind the model, and a short digest of the gateway's own id because it makes the id depend on nothing else. Left to droid, an entry is named after its display name and its place in the list (`custom:<displayName>-<index>`): renaming a model or reordering the catalog would then move every Session on it to another model. The list is sorted for the picker's sake only; the ids do not need it.
- **A Magpie model is an ordinary Custom Model.** It speaks `generic-chat-completion-api` (the gateway translates), carries the gateway's `max_output_tokens`, writes `noImageSupport` from the gateway's modalities (`false` when they include images, because droid treats a custom model without the field as one that cannot take them), and has no Factory multiplier, which the picker already handles. The Daemon reports the id, not the gateway's `provider/model` name, and every one of them says `generic-chat-completion-api`, so the picker reads the company out of that id for the row's mark ("codex" is OpenAI, "grok" xAI, "workbuddy" with a `glm` model Zhipu).
- **The picker keeps Magpie's models in a tab of their own.** The brand tabs, Favorites and the list the picker opens on hold the Daemon's own models only; the Magpie tab, between Favorites and the brands, holds the gateway's. A gateway offers models under the names the Daemon already has, and the two sets side by side read as one crowd of near-duplicates: the tab is what tells a Session's model apart from the gateway's copy of it. The search still reaches both lists, since a query is the user asking for them; a starred Magpie model stays in Favorites, where the user put it.
- **Settings → Advanced has a Magpie section**: on or off, the gateway's address (default `http://127.0.0.1:3425`, or `MAGPIE_BASE_URL`), the gateway's state and how many models it answered with, and Refresh. Turning it on or off restarts the Daemon, since the overlay is read at start. Refresh reads the list and restarts it only when the models differ from the ones the running Daemon was given: the catalog the window shows may already be newer than that, since the section reads a stale one in the background as it draws.

## Considered Options

- **Write `~/.factory/settings.json` directly.** That is what Magpie does for the agents it knows. Rejected: Droi would share a file with the droid CLI and the Factory App, and turning Magpie off could not take its entries out without guessing which ones were its own.
- **Point `FACTORY_API_BASE_URL` at the gateway.** Rejected: the Daemon speaks Factory's protocol, and Magpie speaks four other ones. This is why droid-proxy works and Magpie cannot.
- **Have Droi call the models itself.** Rejected: Droi has no model client. Sessions, tools, Prompts and compaction are the Daemon's (ADR 0001, ADR 0004), and a second path would be a second agent.
- **Wait for Magpie to list droid among its agents.** Rejected: it does not today, and the feature would then depend on another project editing the CLI's settings, which Droi would have to detect and explain.
- **Add one entry per model the user picks, in Settings.** Left out for now: the gateway's list changes on its own, and a picker that follows it is less work to keep right than a list to curate.

## Consequences

- The overlay carries the gateway's models only while Magpie is on; the CLI, and a `droid` run in a terminal, never see them.
- Magpie has to run for a Session that uses its models. Droi says it is unreachable in Settings; a turn that fails says the model did.
- A model that leaves the gateway's list leaves the picker, and a Session that had it keeps an id the Daemon may no longer resolve. An id itself never changes: it follows the gateway's model name, not its display name, its place in the list or the models beside it.
- The `custom:` prefix is droid's rule, not Droi's: should a droid release change it, every Magpie id changes with it, and Sessions on those models have to pick again.
- Magpie's routing groups, its quota and its own usage are not shown. The picker shows the models in a tab of their own, the Daemon runs them.
- The Phone App gets the models for free: it reads the Daemon's list.
- `BuildRuntimeOverlayWith` joins the public API (ADR 0022); `BuildRuntimeOverlay` keeps its signature for the apps that call it.
