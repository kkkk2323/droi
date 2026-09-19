---
status: accepted
---

# Client state comes from the SDK's DaemonSessionController, not the connectToDaemon facade

The plan behind ADR 0001 assumed the Client would use `connectToDaemon()` and layer TanStack Query plus a small Zustand store on top. While building the connection layer we found that facade too thin for a Client: `ConnectedDroid` exposes no connection-status events (so no reconnecting banner), and `ConnectedDroidSession` only yields a Session's events to the caller of `stream()`, so a Client that merely has a Session open, which is exactly the phone in ADR 0003, would see nothing. The facade is a wrapper over `DaemonSessionController` and `MultiSessionStateManager`, both exported, and those two are what Factory's own surfaces use.

We decided the Client builds a `DaemonSessionController` directly (`machineType: local`, credential `{ apiKey: GATEWAY_API_KEY_PLACEHOLDER }`, which the Gateway swaps) and reads all conversation state from its `MultiSessionStateManager`: display messages, working state, pending Prompts, token usage. The Client's own state is limited to the connection state machine (`connecting`, `connected`, `reconnecting`, `unpaired`, `unreachable`) and UI concerns such as which Session is selected. React reads the SDK stores through `useSyncExternalStore`.

Two Gateway details follow from this. Browsers hide the HTTP status of a failed WebSocket upgrade, so the Gateway answers a plain `GET /daemon?token=…` with 204 or 401 and the Client checks that before connecting; that is how "pairing failed" is told apart from "Daemon unreachable". And the SDK's reconnection budget is short (3 attempts), so when it gives up the Client keeps polling the pairing check every 2 s and reconnects when the Gateway answers again.

## Considered Options

- **`connectToDaemon()` facade + TanStack Query + Zustand (planned).** Rejected: no connection events, no way to observe a Session another Client is driving, and we would rebuild the reducers the SDK already ships.
- **`DaemonSessionController` + `MultiSessionStateManager` (chosen).** Full event surface, SDK-maintained message reducers, and the same code path Factory's desktop app uses.
- **Raw WebSocket JSON-RPC with our own reducers.** Rejected: that is the hand-rolled SDK the rebuild is meant to delete.

## Consequences

- No TanStack Query or Zustand dependency for conversation data; only for UI state if that ever grows beyond a few `useState`s.
- The Client depends on non-facade SDK exports whose stability is lower than the documented facade. The SDK stays pinned and #94's TypeScript 7 branch is where an upgrade is first tried.
- The Fake Daemon validates outbound messages with SDK-exported result and notification schemas; the Daemon request envelopes are not exported, so inbound frames get an envelope check only. ADR 0002 is amended accordingly.
