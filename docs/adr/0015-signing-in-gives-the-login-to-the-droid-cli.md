---
status: accepted
---

# Signing in gives the login to the droid CLI

The Daemon authenticates a Client by comparing it with its own identity, which it reads from droid's credential store in `~/.factory` (or `FACTORY_API_KEY`). Under ADR 0005 Droi kept the device-flow login in its own settings and sent its token in `daemon.authenticate`, which only worked where `droid` was already signed in as the same account. On a computer without that login (a fresh Windows install, say) every authenticate failed with "Daemon not authenticated", and the window waited forever.

We decided that a sign-in done in Droi is handed to the droid CLI, as Factory's own desktop app does: the Host writes the tokens to the keyfile backend (`auth.v2.file`, encrypted with the key in `auth.v2.key`, creating the key when there is none) and removes the secure ciphertexts (`auth.v2.keyring`, `auth.v2.loginkeychain`) that droid would otherwise read first. Droi then keeps no tokens of its own and reads the login back like any CLI login; the Daemon and the CLI refresh it, which matters because WorkOS rotates refresh tokens. A login an earlier Droi kept for itself moves to the CLI at startup when the CLI has none. Signing out in Droi removes the CLI's ciphertexts and keeps their keys, as `droid` itself does.

## Considered options

- **Tell the user to sign in to droid first.** Rejected: a second app and a terminal before the first Session, and the sign-in button in Droi would keep failing.
- **Only an API key and the CLI's login.** Rejected: the device flow is the login most people have.

## Consequences

- Droi and the droid CLI share one login per computer; signing in or out in either changes both.
- Droi writes droid's credential format, which is not a published contract. The writer sits next to the reader in `apps/native/internal/host/cli_login.go`, with tests that read back what it writes.
- The Daemon restarts after a sign-in, and the window reconnects whenever a credential appears, so no relaunch is needed.
