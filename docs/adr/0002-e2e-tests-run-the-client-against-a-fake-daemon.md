---
status: accepted
---

# E2E tests run the real Client against a scripted Fake Daemon

Because the Client's only external dependency is a WebSocket URL (ADR 0001), end-to-end tests do not need a real `droid daemon` or a Factory API key. The primary Playwright suite launches the Client in Chromium against a Fake Daemon: a small `ws` server that implements the subset of the daemon JSON-RPC protocol the Client uses, driven by a per-test scenario, and validates every inbound and outbound message with the zod schemas exported by `@factory/droid-sdk`. Schema drift after an SDK upgrade therefore fails the suite instead of surfacing in production.

Three layers, in decreasing weight:

1. **Client × Fake Daemon** (Chromium, every PR, seconds): the regression net; all product behaviour is specified here.
2. **Desktop Shell smoke** (`_electron.launch()`, macOS runner, ≤5 cases): window opens, Gateway listens, pairing QR renders, Local Client connects through the Gateway.
3. **Live** (only when `FACTORY_API_KEY` is set, one case): create a session, send one prompt, receive a reply. Exists to detect real protocol changes.

## Considered Options

- **Record-and-replay real daemon traffic.** Rejected: recordings are unreadable as test intent, couple tests to real timing, and go stale wholesale on any protocol change.
- **Test only through the real daemon.** Rejected: needs a billing credential in CI, is slow and non-deterministic, and cannot script edge cases such as a daemon dropping mid-turn.

## Consequences

- Scenarios are hand-written and must be kept small and intention-revealing; the Fake Daemon is a test fixture, not a daemon reimplementation.
- Selectors prefer `getByRole`/`getByLabel`; `data-testid` is reserved for elements without a semantic role. This makes accessibility a tested property.
- Unit tests (vitest) cover the Gateway's upgrade check and `daemon.authenticate` rewrite, and the Client's streaming reducer. Nothing else in the Desktop Shell warrants unit tests.
