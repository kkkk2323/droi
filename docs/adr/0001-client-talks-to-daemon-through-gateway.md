---
status: accepted
---

# Client talks to the Droid Daemon; the Desktop Shell is a thin host with an auth Gateway

Droi previously carried its own JSON-RPC-over-stdio layer, session storage, Git backend, HTTP API and ~90 Electron IPC methods to drive the `droid` CLI. `droid daemon` plus the browser-safe `@factory/droid-sdk` root entrypoint now provide all of that behind one authenticated WebSocket protocol (sessions, transcripts, search, git, workspace, terminals, skills, MCP). We decided the Client connects to the Daemon with `connectToDaemon()` in both the desktop window and a phone browser, and the Desktop Shell shrinks to: spawn the Daemon, open a window, and run a Gateway.

The Gateway exists because the Daemon's only remote credential is a Factory API key, which we refuse to store on a phone. Daemon authentication is an in-band `daemon.authenticate` JSON-RPC message (not an HTTP header), so the Gateway validates a Pairing Token at the WebSocket upgrade, rewrites the `apiKey` in that first message, and then forwards frames verbatim.

## Considered Options

- **Daemon direct, API key on the phone.** Least code; rejected because a LAN peer with the key can drive the computer and the key is a billing credential.
- **`@factory/droid-sdk/node` behind our own Hono/WebSocket server.** Keeps the key local but forces us to keep a protocol layer and a transcript store (the Node entrypoint has no `getMessages`), and loses concurrent sessions, `git.*`, `workspace.*`, `terminals.*`. This is the "build our own SDK" path the rewrite is meant to end.
- **Daemon plus Gateway (chosen).** Same client code everywhere, key stays on the computer, ~200 lines of proxy.

## Consequences

- No Electron IPC for conversation data. The Local Client also goes through the Gateway (preload injects only a local Pairing Token), so there is one Client code path and the API key never enters any renderer.
- The Daemon is a child of the Desktop Shell (`--parent-pid`), loopback only; the Gateway is the only thing that may listen on a LAN interface, and only when Remote Access is switched on. It keeps a permanent loopback listener and adds one listener per LAN interface address while Remote Access is on, so toggling never disturbs the Local Client's bridge and the port stays closed to the network otherwise.
- Droi's own session files under `~/.droid-app` are abandoned; the Daemon's session store is the only source of truth.
- Droi is coupled to the Daemon protocol version shipped with the installed `droid` binary and the pinned `@factory/droid-sdk`.
