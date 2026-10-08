---
status: accepted
---

# Several Factory accounts run the Daemon from folders of their own

Some people have more than one Factory account (a personal one and a work one, or one per organization) and want to move between them without signing out and in again. The Daemon runs as one login, which it reads from droid's credential store in `~/.factory` (ADR 0015), so the Host can only offer another account by starting the Daemon on another credential store.

We decided that each account added in Droi gets a folder of its own under the Host's data (`accounts/<id>`), and the Host starts the Daemon with `FACTORY_HOME_OVERRIDE` set to it: droid takes that variable for the home folder and keeps the login, logs, cache and state in `<folder>/.factory`, leaving `HOME` alone. The droid CLI's own `~/.factory` stays the first account and is untouched. Before every start the Host links the added account's `skills`, `droids`, `commands`, `hooks`, `plugins`, `mcp.json`, `settings.json` and `sessions` to the CLI's, so every account sees one set of them and one Session list, and copies `AGENTS.md`, which droid ignores when it is a link. droid writes its settings through `atomically`, which follows the link, so a change made under one account reaches all of them. Adding an account runs the device flow on its own and writes the login into the new folder's keyfile backend; it does not switch to it. Switching restarts the Daemon, and Droi asks first when a Session is working, since the restart stops it.

Each account's row shows its Standard usage (the 5-hour, weekly and monthly windows of `/api/billing/limits`), and the account in use says when it is near its limit, so the user knows when to switch; Droi never switches by itself. Droi asks the way droid asks: through the Factory API base URL the Daemon gets (so a local droid-proxy carries it), with the account's own token and droid's client headers (`X-Factory-Client: cli`, `X-Client-Version` from `droid --version`, `X-Factory-Org-Id`, the `Bun/x.y.z` user agent named in the droid binary). The droid CLI and the Daemon refresh their own logins; an added account that is not in use has no one else to refresh it, so Droi renews its expired login itself and writes it back to its folder.

## Considered options

- **A proxy that rotates the credential in front of Factory** (as the local droid-proxy does with API keys). Rejected for Droi: it means answering for Factory's endpoints and keeping keys, which is the user's own tool and not something Droi should ship.
- **A separate `HOME` per account** (as Synara does). Rejected: the Daemon's tools and the user's shell would see another home; `FACTORY_HOME_OVERRIDE` moves only droid's folder.
- **Copying the shared files into each folder.** Rejected: the copies drift; links keep one source.
- **Switching on its own when an account runs out.** Rejected: a switch restarts the Daemon and stops the Sessions it runs; the user decides when.
- **Asking Factory directly from the Host.** Rejected: requests that leave by another route than the Daemon's (no proxy, another egress) are refused with 403, so usage follows the Daemon's base URL.

## Consequences

- The droid CLI's account still shares its login with the CLI (ADR 0015); an added account's sign-out touches only its folder. Droi refreshes only an added account's login, and only while it is not in use, so it never races the CLI's or the Daemon's rotation.
- On Windows a link needs Developer Mode, so the Host falls back to a junction for a folder and a hard link for a file; a file droid replaces by rename then stops being shared for that account.
- Prompt caching is not tied to the account, so a switch costs only the fresh Daemon's first turn.
- Removing an account unlinks the shared entries first and then deletes the folder, so nothing it shared is lost.
