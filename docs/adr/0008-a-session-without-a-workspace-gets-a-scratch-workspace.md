---
status: accepted
---

# A Session started without a Workspace gets a Scratch Workspace the Gateway creates

Users want to talk to Droid without working next to a project, the way a ChatGPT conversation stands on its own. Every Session the Daemon runs has a Workspace, and the Daemon has no call that creates a directory, so Droi makes one: the New session page offers "None" as a Workspace, and a Session started that way runs in a Scratch Workspace, a fresh folder under the Scratch folder (`~/.droi/chats` unless the Desktop Shell's settings say otherwise).

The Gateway answers three HTTP requests of Droi's own, authenticated with the same token as the Daemon socket: one creates a folder under the Scratch folder and returns its path, one moves such a folder to the Trash, and one recreates it. The Client then starts the Session in that path as in any Workspace, tagged `droi.scratch`, so the Draft Session, skills and commands work unchanged. Archiving a Scratch Session archives the whole conversation (the Sessions a compaction chained to it) and moves the folder to the Trash; the transcripts stay with the Daemon, and unarchiving recreates an empty folder first so the conversation opens again.

## Considered Options

- **The system temp directory.** macOS cleans it, and a Session whose Workspace vanished can no longer be loaded, continued or compacted.
- **One shared folder for every such Session.** Simplest, but files from different conversations mix and one conversation's leftovers steer the next.
- **The Gateway rewrites `daemon.initialize_session` without a `cwd`.** Works for the first Session but not for the Draft Session, which needs the path before anything is sent.
- **Requests of Droi's own on the Daemon socket.** The SDK's controller only sends the Daemon's methods; plain HTTP next to `/meta` needs nothing from it and works the same from the Phone App.
- **Keep the folder on archive and offer a separate delete.** The Daemon cannot delete a Session, so a deleted conversation would linger as an archived Session pointing at nothing.

## Consequences

- A `/compact` handoff continues in the same folder and inherits the tag, so a Scratch Workspace can hold several Sessions of one conversation.
- The Gateway only touches folders directly inside the Scratch folder and refuses any other path. Trashing an empty folder removes it instead, so a Client discarding its Draft Session cleans up the folder it made; a folder with anything in it only ever goes to the Trash. A sweep at start was rejected: a conversation that never wrote a file has an empty folder too, and the Shell cannot tell it from an abandoned one. A Client that quits mid-draft leaves an empty folder behind.
- The Scratch folder is a Desktop Shell setting; only the Local Client edits it. Changing it applies to new Sessions, and existing folders stay where they are.
- Droid keeps its full tools in a Scratch Workspace. It is a clean starting directory, not a sandbox. The Daemon reports a fresh folder as untrusted but starts Sessions there, as it does for any Workspace Droi opens.
- The Fake Daemon answers the three requests, so the suites cover the flow.
