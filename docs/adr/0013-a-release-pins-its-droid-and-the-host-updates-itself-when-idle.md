---
status: accepted
---

# A release pins its `droid`, and the Host updates itself when idle

Droi speaks the Daemon protocol through a pinned `@factory/droid-sdk`, while the Daemon's protocol version comes from whichever `droid` binary is on the computer's PATH, a binary that updates itself. On one computer the two drifted quietly: the SDK in this repo declares protocol 1.201.1 and the installed droid answers with a newer one, and the SDK's answer to a mismatch is to log and drop the notifications it cannot parse. With several Hosts (ADR 0012) the drift multiplies, and a Client that only ever spoke to one Daemon now meets three.

We decided a Droi release names one **Pinned Droid**. The Host downloads that exact build from Factory's versioned release path (`downloads.factory.ai/factory-cli/releases/<version>/<platform>/<arch>/droid`, checksum beside it) into its own data directory, starts the Daemon from it, and disables droid's self-update for that copy. The Host's version, its Pinned Droid and its bundled Client and SDK are thereby fixed together at release time; the droid CLI the user runs in a terminal is a separate install that may be newer. When the download fails the Host falls back to the PATH `droid` and says so in `/meta`, so an offline box still works. An explicit `droidPath` setting skips the pin for development.

Updating follows multica's daemon: a Host on its own checks the npm registry on a timer (hourly, releases only, on by default), installs the new version with the npm that installed it, and re-executes into it; a Client can ask for the same through the Gateway, and `droi-host update` does it by hand. Every restart, of the Daemon or the Host, waits until no Session has a turn in progress (Memory Sessions included) and records why it is waiting in `/meta`; a forced restart exists for a wedged Daemon and lists what it will interrupt. The Desktop Shell's embedded Host is updated by the Shell's own updater and refuses remote update requests, since a desktop update relaunches a window the user is looking at; it does accept remote Daemon restarts.

## Considered Options

- **Follow the PATH `droid` and only report versions.** The status quo with a banner. Rejected: the user would have to keep three things aligned by hand on every computer, and the SDK hides mismatches rather than failing.
- **Bundle `droid` inside the Droi package.** Same alignment, but a 240 MB binary per platform in every npm and DMG artifact, and Factory already serves versioned builds.
- **Update on a version mismatch the Client notices.** Puts the decision on the least informed party; the Host knows whether it is idle.
- **Restart at once when a new binary appears.** Simplest, and multica did not do it either: a running turn is the one thing a restart must not interrupt.

## Consequences

- Releasing Droi means choosing a droid version and running the E2E suites and the live test against it; the release notes name it.
- The Host's data directory holds one droid build per pinned version, old ones removed after a successful switch.
- `/meta` reports `hostVersion`, `droidVersion`, whether the Pinned Droid or a fallback is running, the Daemon state and any pending restart with its reason. A Client compares `hostVersion` with its own and offers "Update this computer" (or "update the app" when the Client is the older one); it never refuses to connect over a version difference.
- Linux CPUs without AVX2 need Factory's `-baseline` build; the Host picks it the way Factory's installer does.
- Droi's own `FACTORY_DROID_AUTO_UPDATE_ENABLED` handling must be verified against the installed droid before shipping: the variable exists, its exact semantics were not read from source.
