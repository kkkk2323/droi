---
status: accepted
---

# Droi adds the models Settings lets use Script

droid 0.249 decides whether a Session may call tools from a Script program by looking its model up in
`configs.script_tool_models.models`, a dynamic config it fetches from `/api/feature-flags`. A model
outside that list is resolved to `direct_only` even when the Session asks for `script_only`, and
`toolExecutionModeOverride` does not help. A newly released model, such as DeepSeek V4.1 Flash
1017, is therefore unusable in Script until Factory adds it, and the Session silently runs in
Direct instead.

We decided:

- **The Key Pool's proxy adds the models Settings lists.** Settings → Session defaults → Tool calls
  has **Script models**, a list kept on this device (`droi.scriptModels`). For a `GET`
  `/api/feature-flags` reply, the proxy appends those IDs to `configs.script_tool_models.models`,
  and leaves every other flag and config byte for byte as Factory sent it.
- **The change is narrow.** It happens only when the reply already carries `script_tool_models`;
  it is idempotent; a reply it cannot read, a non-200, or a body over 4 MiB passes through untouched.
  `droid` picks the list up when the Daemon starts, so the settings row says to restart it.
- **The Key Pool must be on.** Only then does the Daemon send its Factory traffic through Droi's
  proxy (ADR 0027). With the Key Pool off the Daemon talks to Factory directly and the list has no
  effect.

The consequence is that Droi, not Factory, decides that a model may use Script, for the models the
user names. That is deliberate: Script is a way to save context, not a safety property, and a model
Factory has not listed may still handle Script, as DeepSeek V4.1 Flash 1017 was verified to. The
list is per device, so a second computer has its own; once Factory lists a model, its entry here
adds nothing.
