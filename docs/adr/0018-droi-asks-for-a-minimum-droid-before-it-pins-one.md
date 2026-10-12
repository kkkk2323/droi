---
status: accepted
---

# Droi asks for a minimum droid before it pins one

ADR 0013 decided that a release would download and run a Pinned Droid. It was never built: the native Host starts the Daemon from whatever `droid` it finds (the `droidPath` setting, then `PATH`, then the installer's folders). What we actually feared was an old droid under a new Droi: a Daemon that lacks what the Client calls. A Daemon the Host starts already updates its own droid at startup (`runConditionalUpdate` in droid 0.238.0), so most computers follow the latest droid. The ones that stay behind are those where droid cannot update itself: an npm install, a folder droid cannot write to, an organization that turned updates off, Airgap Mode, or a `droidPath` that points at an old copy.

We decided that each release names a **Minimum Droid** (`MinDroid` in `apps/native/internal/host/droid.go`, first 0.233.0, the build the Go SDK was checked against). Before every start the Host reads `droid --version`. When it is older, the Host does not start the Daemon, and the window says which version it needs and offers "Update droid". That runs `droid update` without `FACTORY_DROID_AUTO_UPDATE_ENABLED`: a Daemon sets it to `false` for its Sessions' commands, and with it `droid update` refuses as if droid came from npm. Once the version is at least the minimum, the Host starts the Daemon; otherwise it shows the last line droid printed.

The Pinned Droid stays as ADR 0013 describes it, for when an update of droid breaks a Droi already released, or when many users have no droid CLI at all.

## Considered options

- **The Pinned Droid now** (ADR 0013). Deferred: new models would wait for a Droi release, each release would have to choose a droid and regenerate the SDK against it, and the Daemon would differ from the CLI the user runs over the same `~/.factory`.
- **Bundling droid in the installer.** Rejected again: 250 MB or more per platform.
- **Only reporting the version.** Rejected: the Daemon would start and fail later in ways that do not name the cause.

## Consequences

- Raise `MinDroid` in the change that first uses something only a newer Daemon offers.
- A droid newer than the SDK still runs; the SDK drops the notifications it cannot parse, and Droi's own updates keep that gap short.
- `droid update` is a manual update: it ignores the user's `FACTORY_DROID_AUTO_UPDATE_ENABLED=false`, but organization controls and Airgap Mode still stop it, and the window shows what droid said.
