---
status: accepted
---

# Droi runs Factory's own droid with a preloaded key hook

ADR 0027's Patched Droid changed one bundled module of Factory's build by a Recipe. Every droid
release needed a new Recipe, a scheduled workflow to make and sign it, an ad-hoc signature on macOS,
and a working Bun bytecode layout; droid 1.0 changes faster than that. droid is a Bun executable, and
Bun reads `BUN_OPTIONS` at start: `--preload=<file>` runs a script before droid's own code, in the
same process, with the same `fetch` and `WebSocket`. Tried on droid 0.70 to 1.0.0 (macOS, Linux and
Windows), with real keys on one Session through title, compaction, a subagent and key switches.

We decided:

- **Droi's droid is Factory's build, unchanged.** The Host asks `downloads.factory.ai/factory-cli/LATEST`
  for the newest version, downloads that build into its data folder and checks Factory's checksum (a
  local droid with the same SHA-256 is copied instead). Nothing is patched or re-signed. It starts
  with `FACTORY_DROID_AUTO_UPDATE_ENABLED=false`; Settings offers a newer one, checked hourly.
- **The Key Hook does what the patch did, from outside.** `pkg/droidhook` embeds `llm-hook.js`;
  the Daemon gets `BUN_OPTIONS="--preload=<hook>"`, `DROI_LLM_KEY_FILE` and `DROI_HOOK_READY`. Only
  inside droid's own executable (`Bun.main` under `$bunfs` or `~BUN`), the hook wraps `fetch` and
  `WebSocket`: a request to `/api/llm/{a,o,g,v1,decision}/` on Factory's hosts (the base URLs and
  `*.factory.ai`, where an account's premium host sends LLM requests), but not `/api/llm/custom/`,
  gets the key file's key. With no key in the file it fails at once; it never falls back to the
  login. Every other request keeps droid's login.
- **Bun's rules are kept exactly.** Bun ignores a `--preload` that follows another option, without
  a word (a preload file that is missing stops droid instead). So the option goes first, ahead of
  any `BUN_OPTIONS` droid would have had; the whole argument is in double quotes (macOS's
  `Application Support` has a space); on Windows the path has forward slashes, since Bun reads a
  backslash in quotes as an escape.
- **Nothing is trusted until it checked in.** Before a version is used, the Host runs it against a
  local receiver with made-up keys (`droidhook.Verify`): the hook checks in, LLM requests carry the
  key file's key, others the login, and without a key file no LLM request goes out. Each Daemon
  start has the hook write its process id to `DROI_HOOK_READY`; a Daemon that did not is stopped
  after its health check, with the reason in the window.
- **The LLM Proxy refuses what the hook did not choose.** A request to `/api/llm/` (but
  `/api/llm/custom/`) whose key is not in the Key Pool gets a 400 from the proxy and never reaches
  Factory, so a hook that failed cannot bill the signed-in account. When Factory answers 401 or 403
  for a pool key, droid would read it as its own login having expired: in Automatic mode the proxy
  moves the key file and answers 429 with `Retry-After-Ms`, else a 400 that names the key.
- **The key file is replaced so Windows allows it.** Windows refuses to rename over a file another
  process has open, which droid has for each request; `droidhook.WritePrivate` retries
  `ERROR_ACCESS_DENIED` and `ERROR_SHARING_VIOLATION` for up to 2 seconds, as Go's own `robustio`
  does.

A Host that ran the Patched Droid (`droidKind: "patched"`) runs Droi's droid: it prepares the newest
build at start and removes the patched builds and the cached Recipes. The Minimum Droid (ADR 0018)
still applies.

## Considered Options

- **Keep the Patched Droid.** Rejected for the reasons above; it also needed a signing key in CI
  and a published release for every droid.
- **Run the user's droid with the hook.** Rejected, as in ADR 0027: droid's own update would
  change the build under a running Key Pool, and the build was never checked with the hook.
- **Swap keys in Droi's proxy.** Still rejected (ADR 0027): the proxy would decide which requests
  change identity. The gate only refuses; it never adds a key.

## Consequences

- Any droid that Bun builds works without a new release of Droi, as long as Factory's LLM routes
  keep their prefixes. A droid that moves them fails `Verify`, and Droi keeps the version it has.
- Every Bun process the Daemon starts, a script an agent runs say, preloads the hook too; outside
  droid's executable it does nothing. A `droid` an agent runs inside a Session uses the Key Pool as
  well, and agents droid 1.0 hosts in the Daemon's own process share its `fetch`.
- The `droid-patch` workflow, its release and `DROI_RECIPE_SIGNING_KEY` are no longer used.
- An app built on Droi that mirrors the downloads keeps `LATEST` next to its releases folder;
  `Options.RecipesURL` is ignored.
