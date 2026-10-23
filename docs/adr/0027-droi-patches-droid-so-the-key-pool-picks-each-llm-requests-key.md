---
status: accepted
---

# Droi patches droid so the Key Pool picks each LLM request's key

ADR 0005 left key rotation to droid-proxy (`dp`): Droi pointed `FACTORY_API_BASE_URL` at it, and
`dp` rewrote the `Authorization` of every LLM request. That needs a second program installed and
running, and a proxy that tells LLM calls from the rest of droid's Factory traffic by path and swaps
credentials in flight. Users who only run Droi wanted the same pool of keys, with its usage page,
inside Droi.

We decided Droi can run the Daemon from a **Patched Droid**, opt-in from Settings → Advanced:

- **The patch changes one place.** A **Recipe** names a droid version, the SHA-256 of the bundled
  module built from `src/llm-proxy/utils.ts`, an anchor in it and a few lines to inject before the
  anchor. Those lines make the headers of an LLM request carry the key read from the file named by
  `DROI_LLM_KEY_FILE`. With no key file, or an empty one, the request fails with a non-retryable
  error; it never falls back to the signed-in account. Every other request keeps droid's own login,
  so Sessions still belong to the Factory Account the Daemon runs as.
- **Droi patches Factory's own build, never the user's droid.** The Host downloads the exact version
  a Recipe names from `downloads.factory.ai/factory-cli/releases/…` and checks Factory's checksum (a
  local droid is copied instead only when its SHA-256 is the same). It applies the Recipe, signs the
  result ad hoc on macOS, and runs it against a local receiver with a fake key before it replaces
  anything. The Patched Droid lives in Droi's data folder and starts with
  `FACTORY_DROID_AUTO_UPDATE_ENABLED=false`, since a self-update would put Factory's file back;
  Settings offers an update when a newer Recipe exists.
- **Recipes are data, published and signed.** Droi embeds the Recipes it was released with and reads
  newer ones from the `droid-patch` release of this repository, signed with an ed25519 key whose
  public half is in the app. A scheduled workflow makes and verifies the Recipe of each new droid on
  macOS arm64 and amd64 and opens an issue when the patch no longer fits.
- **The Key Pool and its LLM Proxy only observe.** The keys live in Droi's own data folder (mode
  0600), not in droid-proxy's files. The Key Pool writes the current key into the key file; the
  Daemon's `FACTORY_API_BASE_URL` points at the Key Pool's local LLM Proxy, which forwards every
  request unchanged and records requests, events and each key's quota for the Usage tab in Settings.
  In Automatic mode, when a key runs out the proxy moves the key file to another key and answers
  droid's 402 or 429 with a 429 and `Retry-After-Ms: 500`: droid retries a 429 but not a 402, so the
  retry goes out with the new key. In Manual mode the key stays and the error reaches the Session.
  Cache warming from droid-proxy runs in the same proxy.

The Minimum Droid (ADR 0018) still applies: a Recipe for an older droid is not used. This is not
ADR 0013's Pinned Droid: the version follows the newest Recipe, not the Droi release, and the
official droid stays the default.

## Considered Options

- **Keep using droid-proxy.** Still possible through the Factory API base URL, and the Key Pool is
  off unless the Patched Droid is. Rejected as the only way for the reasons above.
- **Rewrite `Authorization` in Droi's own proxy, as `dp` does.** Rejected: the proxy would hold the
  decision of which requests may change identity. With the patch, droid sends the right key itself
  and a proxy that fails or forwards the wrong path cannot move a request onto another account.
- **Patch the droid the user installed.** Rejected: the CLI the user runs would change, and droid's
  own update would undo it.
- **Bundle a patched droid with Droi.** Rejected again (ADR 0013, 0018): 250 MB or more per
  platform, and a new droid would wait for a Droi release.

## Consequences

- Each droid version needs a Recipe. Until the workflow publishes one, Droi keeps the newest Patched
  Droid it has. Windows and Linux have no verified Recipe yet.
- The workflow needs the `DROI_RECIPE_SIGNING_KEY` secret; a Recipe that does not verify against the
  embedded public key is ignored.
- A droid that moves or renames `src/llm-proxy/utils.ts` breaks the Recipe generator, not the
  Patched Droid already in use.
