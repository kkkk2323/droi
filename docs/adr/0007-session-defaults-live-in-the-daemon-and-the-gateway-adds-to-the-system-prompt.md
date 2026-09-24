---
status: accepted
---

# Session Defaults live in the Daemon; the Gateway adds to the system prompt

Users wanted to choose what new Sessions start with, as the Factory App's Settings → Session Defaults page allows, and to add their own instructions to Droid's system prompt.

The Daemon already owns Session Defaults. `daemon.get_default_settings` reads them (with the model list, the autonomy levels the organization allows, and which keys it manages) and `daemon.update_session_defaults` writes them to `~/.factory/settings.json`, which the droid CLI and the Factory App read too. The Factory App's page is a thin client over these two calls. So is Droi's: the shared daemon layer reads and patches them (`session-defaults.ts`, `use-session-defaults.ts`), the web Client shows them under Settings → Session defaults and the Phone App on its own screen, over a connection to the selected computer. Two details of the Daemon shape the patches: it replaces `subagentModelSettings` as a whole, so a change to one tier sends every tier, and a key sent as `null` is cleared.

The Daemon has no setting for the system prompt. It takes one per Session, only at `daemon.initialize_session`: `systemPrompt` is either a string that replaces Droid's prompt or `{type: "preset", preset: "droid", append}`, which keeps Droid's prompt and appends text after it. The System Prompt Addition therefore lives in the Desktop Shell's settings, and the Gateway adds it as `append` to every `daemon.initialize_session` a Client sends, unless the Client chose a system prompt itself.

## Considered Options

- **Each Client stores the text and sends it.** Rejected: three places to keep in step, and a phone would not see what was set on the desktop.
- **Launch the Daemon with `FACTORY_APPEND_SYSTEM_PROMPT`.** Works, but a change would need a Daemon restart, which stops every running turn.
- **Gateway adds it at Session start (chosen).** Every Client gets it, a change applies to the next Session without a restart, and the Gateway already rewrites `initialize_session` for the credential.
- **Offer replacing Droid's prompt.** Left out: a full replacement drops Droid's tool and safety guidance; appending covers the need.

## Consequences

- The Gateway now changes more than credentials: `daemon.initialize_session` frames gain `systemPrompt` when the Shell has an addition. Other frames still pass byte-identical.
- The addition applies to Sessions a Client starts. Subagents and Sessions the Daemon starts on its own (a `/compact` handoff) do not go through the Gateway and keep Droid's prompt.
- Only the Local Client can edit the addition, like every other Shell setting; Session Defaults work from every Client.
- The live test sends an addition and checks the reply follows it, so a protocol change shows up there.
