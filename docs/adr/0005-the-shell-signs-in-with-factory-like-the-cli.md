---
status: accepted
---

# The Desktop Shell signs in with Factory the way the `droid` CLI does

Until now the Gateway authenticated every Client connection with a Factory API key (ADR 0003), and the Desktop Shell handed the same key to the Daemon through `FACTORY_API_KEY`. Two things broke that model in practice.

First, the Daemon enforces organisation ownership on Sessions. Session files under `~/.factory/sessions` carry the org of the identity the Daemon runs as; `update_session_settings`, `rename_session` and friends go through `assertSessionAccessibleForOrganization`, which compares that org with the org of the *connection's* credential and answers `SessionNotFound` on a mismatch. A key from another account (for example one from a droid-proxy key pool) therefore connects fine, creates Sessions fine, and then cannot change their model.

Second, the intended deployment runs the Daemon's LLM traffic through droid-proxy (`dp`), which rotates keys itself on `/api/llm/...` and passes every other request through. `dp` starts `droid` **without** `FACTORY_API_KEY`: droid uses its normal login for identity, and only the LLM calls carry rotated keys. Droi should sit in the same position.

We decided the Desktop Shell gets a "Sign in with Factory" of its own that is byte-for-byte the CLI's login: the OAuth device flow against WorkOS User Management (`/authorize/device`, then polling `/authenticate` with the device-code grant, refreshing with the refresh-token grant) using the production `droid` CLI client id. The resulting access token is what the Daemon accepts as `token` in `daemon.authenticate`, and `/api/cli/whoami` maps it to Factory's own user and org ids. Tokens live in the encrypted settings file (safeStorage), are refreshed a minute before expiry, and never reach a Client: the Client keeps sending the Gateway placeholder, and the Gateway now rewrites `apiKey: <placeholder>` into `token: <access token>` (the SDK also sends the placeholder as `token` in `initialize_session` / `load_session`, which is filled the same way). Credential lookup is asynchronous, so the Gateway forwards a Client's frames through an ordered promise chain.

While signed in, the Shell starts the Daemon **without** `FACTORY_API_KEY`, so the Daemon runs as the `droid` CLI's login on this computer, exactly like `dp`. That makes the CLI login the identity Sessions belong to; the Settings page reads `~/.factory/host.json` and warns when the Droi login and the CLI login are different users, or when the CLI is not logged in at all. An API key (stored or `FACTORY_API_KEY`) remains the fallback for headless use: without a login it is injected for Clients and handed to the Daemon as before.

## Considered Options

- **Reuse the CLI's stored credentials directly (`auth.v2.*`).** Rejected: the files are encrypted with a key held in the OS keychain in a Factory-private format that changes between versions, and reading them can trigger keychain prompts.
- **Own device-flow login with the CLI's WorkOS client id (chosen).** Same identity system and same token type the Daemon already verifies; no undocumented storage to decode.
- **Same-account API key.** Works and is kept as the fallback, but the product goal was no API key at all.
- **SDK `trusted` authentication mode.** Only exists for in-process / IPC transports; the Gateway bridges WebSockets, and the Daemon is a separate process by design (ADR 0001).
- **Key rotation inside Droi.** Rejected: droid-proxy already does it, including usage probing, pool cooldowns and 402/429 retries; Droi stays a thin Client and points `FACTORY_API_BASE_URL` at it.

## Consequences

- `daemon.authenticate` frames from Clients carry `token` instead of `apiKey` whenever the Shell is signed in. The Fake Daemon accepts either.
- The WorkOS client id and endpoints are copied from the CLI binary and are not a published contract. They sit in one module (`src/main/factory-auth.ts`) with unit tests against a fake WorkOS, so a change shows up as one failing file.
- Signing in or out restarts the Daemon, because its environment (with or without `FACTORY_API_KEY`) is read at spawn.
- The `~/.droid-app` API key of the Factory desktop app is no longer relevant to Droi.
