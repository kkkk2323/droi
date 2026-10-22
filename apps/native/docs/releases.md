# Releases and in-app updates

Droi is the native app. A tag `v<version>` runs `.github/workflows/release.yml`,
which puts in one GitHub Release:

- `Droi <version>.dmg`, one app for both Apple chips (`darwin/universal`), for new installs;
- `droi-<version>-linux-amd64.tar.gz` and `-linux-arm64.tar.gz` (with a `.deb` and the
  `install.sh` each), the program for `droi host` on Linux and its signed update;
- `droi-<version>-darwin-universal.tar.gz`, the signed update, a delta update from the
  previous version (`updates.deltas` in `mygo.json`), and `update-darwin-universal.json`,
  the manifest the app checks.

The workflow builds into a draft and publishes it once every build is in, as the latest
Release: apps read the manifest from `releases/latest/download/`.

## How the app updates itself

`internal/updates` walks the update through the Shell's `UpdateState`: check, download,
restart. It checks 15 seconds after launch, then every hour. By default a release it finds
waits for the user: the corner card offers Update, Details and Skip this version, and once
it is installed, "Restart now" (or About's "Restart to update") starts the new version.
With Settings → About → "Install updates automatically" on, the check installs what it
finds, and the app restarts into it once no Session works or waits for an answer and the
window is in the background, checking every minute.

MyGo's updater does the rest: it downloads the delta for the running version when there is
one (else the whole archive, as an app more than one version behind does), checks the
Ed25519 signature against the public key built into the app, and replaces the bundle. A file that is not signed with the key leaves the app as it
was. Development builds (`go run`) and `DROI_NO_UPDATE_CHECK=1` do not update.

## The signing key

`mygo.json`'s `updates.publicKey` is the public half of the key that signs updates. The secret
half is `mygo-update.key`, which `go tool mygo keygen` wrote to
`~/Library/Application Support/mygo/update-keys` on the computer that made it. It never goes
in the repository. Keep a copy in a password manager: installed apps take only updates signed
with it, so losing it means they no longer update. Give it to the workflow once:

```sh
gh secret set MYGO_UPDATER_PRIVATE_KEY < ~/Library/Application\ Support/mygo/update-keys/mygo-update.key
```

## From the Electron app

The Electron Shell checks for `update-darwin-universal.json` in the latest Release before its
own `latest.json`. Once it is there, its update state is `native`: the About row and the
corner card say Droi is now a native app and link to the Release to download it. It installs
no more code updates of its own. The native app has the same bundle identifier and data
folder, so dropping it into `/Applications` over the old one keeps the Sessions, settings and
Pairing Token.

The first Release with the native app has no delta updates (no version is published before
it); the Electron users install it from the disk image, which the card links to.

## A release

1. Add the version's `## <version> - <date>` section at the top of `CHANGELOG.md` (Added,
   Changed, Fixed, in plain sentences). It becomes the release notes and the update's notes.
2. Run `scripts/release.sh <version>` on main. It bumps the version in the two places a
   test keeps equal (`apps/native/mygo.json` and `devVersion` in `apps/native/main.go`),
   commits them with the changelog as `chore(release): <version>`, pushes main and the tag `v<version>`, and waits until the workflow has published the
   Release (about 6 minutes). Arguments after the version go to `git commit`, such as
   `--trailer "Co-authored-by: …"`.
3. Reinstall the Phone App with `pnpm install:phone`, so that it matches the release.

Push only `v<version>`: once the release is published, the workflow tags
`apps/native/v<version>` itself, the version other apps require of the Go module (ADR 0022),
and fails if that tag already exists. When a run fails only on a flaky test, fix the test on
main and rerun the failed jobs (`gh run rerun <id> --failed`) rather than releasing another
version.
To try a signed build locally:

```sh
MYGO_UPDATER_PRIVATE_KEY="$(cat ~/Library/Application\ Support/mygo/update-keys/mygo-update.key)" \
  go tool -modfile=apps/native/go.mod mygo build apps/native
```
