# apps/desktop (deprecated)

The Electron Desktop Shell and the web Client. They are no longer maintained:
the native app (`apps/native`) replaces them, and the Phone App (`apps/mobile`)
is the other maintained Client.

Do not add features here, fix bugs here or run the suites that test this code
(`tests/web`, `tests/electron`, `tests/live`). The code stays until it is removed,
so it must still pass `pnpm check` and `pnpm test`.

`package.json`'s `version` is still the release version: the Phone App and the
Fake Daemon read it, and each release bumps it with `apps/native/mygo.json`.
