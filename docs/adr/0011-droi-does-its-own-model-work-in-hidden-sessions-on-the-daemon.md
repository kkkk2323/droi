---
status: accepted
---

# Droi does its own model work in hidden Sessions on the Daemon

Memory needs a model twice without a user in the loop: to consolidate a Project Memory that has grown past its soft limit, and to extract entries from a transcript when a Session ended without writing any (ADR 0010). The obvious tools were a `droid exec` child process, which starts a whole agent in a few seconds and leaves a session record, or a direct HTTP call to droid-proxy, which means reproducing the client signature and provider paths the proxy exists to hide.

We use the Daemon Droi already runs. The Desktop Shell opens a Session with `daemon.initialize_session`, tagged `droi.memory`, with a `title`, `privacyLevel: "private"`, the model chosen in Settings → Memory, the task's prompt as the system prompt `append`, `autoRejectPermissionRequests: true` so it can never wait on a Prompt nobody will answer, and `structuredOutputFormat` so the result is JSON the Shell validates rather than prose it parses. Consolidation works one category slice at a time and the Shell accepts changes only to the ids it sent, so a wrong answer is a rejected slice, not lost data (pi-hermes lost eleven entries this way before adding the same guard). When the turn ends the Shell archives the Session with `daemon.archive_session`.

Clients hide these Sessions by their tag, as they hide Draft Sessions. The Shell drives them, not the Memory Server: the Shell holds the Daemon address and the Factory credential and already owns the Daemon's lifecycle, and it takes the `@factory/droid-sdk` dependency for it.

## Considered Options

- **`droid exec` from a hook or the Memory Server.** Works with no configuration and inherits the proxy through the environment, but every run is a cold agent start and a session record outside Droi's control, and a hook that runs `droid exec` must guard against triggering its own hooks.
- **HTTP to droid-proxy.** Fastest, but the request shape, headers and provider paths are the proxy's internals; Factory rejects clients it does not recognise.
- **The Memory Server drives the hidden Session.** Rejected: it would need the Daemon address and the credential passed through the Runtime Overlay's `env`, one more process holding the token.
- **Delete the Session afterwards.** Not available: the Daemon deletes only empty Draft Sessions on close and has no `delete_session`. Deleting the transcript file behind its back would contradict ADR 0009 and leave the synced copy.

## Consequences

- Hidden Sessions persist under `~/.factory/sessions`, sync to Factory when `cloudSessionSync` is on, and show in the droid CLI and Factory App as archived Sessions titled for what they did.
- The Session's cost follows the model picked in Settings → Memory; the default is a cheap one.
- The Fake Daemon gains a Scenario for a hidden Session so the E2E suites can check the tag, the archive call and the Clients' filtering; the quality of the model's output is a live test's concern.
- Any later Droi feature that needs a model without a user (titles, summaries, triage) has a path.
