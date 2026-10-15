---
status: accepted
---

# The Go SDK and the Fake Daemon live in their own repository, droid-sdk-go

A second app is planned beside Droi: a desktop app that starts its own Daemon and serves a WeCom bot, reusing Droi's code. It lives in a repository of its own, and Go does not let another module import `apps/native/internal`, nor a module that `replace`s its way into a sibling folder. The Go SDK (`packages/droid-sdk-go`) was already a module of its own with no dependency on Droi, so it goes first. Its `fakedaemon` package, which the native app's view tests use, ran the TypeScript Fake Daemon (`tests/fake-daemon`) by bundling it from a Droi checkout, so the Fake Daemon goes with it.

We decided:

- **One repository, [droid-sdk-go](https://github.com/kkkk2323/droid-sdk-go), for both.** The module is `github.com/kkkk2323/droid-sdk-go`; the Fake Daemon is the package `@droi/fake-daemon` in its `fake-daemon/` folder. The history of both folders came along.
- **Droi pins a tag of it twice:** `apps/native/go.mod` requires the module, and `tests/package.json` installs the Fake Daemon from the same tag (`github:kkkk2323/droid-sdk-go#<tag>&path:/fake-daemon`). Nothing is published to npm.
- **What it commits is built.** The package carries its compiled `dist/`, so installing it from Git needs no build, and `fakedaemon` embeds the Fake Daemon bundled into one script, so a Go test needs only Node, not a Droi checkout. Its CI fails when either is out of date.
- **The Fake Daemon keeps playing Droi's Gateway.** Clients reach a Daemon through a Gateway, so the Fake Daemon answers the Pairing Token check, `/meta`, Scratch Workspaces and session files. It keeps its own copy of the Gateway's paths and of Droi's patch to `@factory/droid-sdk`'s schemas; the Phone App's suite fails when the copy drifts from `packages/daemon-layer/src/gateway.ts`. `/meta` answers `FakeDaemon.appVersion`, which Droi's fixtures set to the release version.

The Host and the window stay in `apps/native/internal` until the second app needs them; moving them out into public packages is its own decision.

## Considered Options

- **Leave the Fake Daemon in Droi.** droid-sdk-go's own tests would need a Droi checkout, and so would every other module that uses `fakedaemon`.
- **Publish to npm and tag the Go module from Droi (`packages/droid-sdk-go/vX`).** Keeps one repository, but the second app would depend on Droi's release tags and its whole history for one package.

## Consequences

- A change to the SDK or the Fake Daemon is two steps: a tagged release of droid-sdk-go, then moving both pins in Droi. While working on both, `go work use` and a `link:` dependency point at a local checkout.
- Droi's PR workflow no longer checks the SDK; droid-sdk-go's CI does (gofmt, `go vet` for Linux, macOS and Windows, `go test -race`, typecheck).
