---
status: accepted
---

# Skills and MCP Servers are managed through the open Session's Daemon calls

Users wanted to turn skills off and manage MCP Servers from Droi, as the Factory App's Skills and MCP windows and the droid CLI's `/mcp` allow.

The Daemon already owns both. `daemon.list_skills` and `daemon.set_skill_disabled` read a Session's skills and write the switch to `~/.factory/settings.json` (user level) or the Workspace's `.factory/settings.json` (project level); `daemon.list_mcp_servers`, `toggle_mcp_server`, `add_mcp_server`, `remove_mcp_server`, `list_mcp_tools`, `toggle_mcp_tool`, `list_mcp_registry` and the `authenticate` / `cancel` / `clear` auth calls do the same for MCP Servers in `~/.factory/mcp.json`, with `mcp_status_changed` pushed as connections come and go. The Factory App is a thin client over these calls, with rules for which switches a skill gets (a project skill off for its project, a built-in one across all projects, a personal one managed by its files, the organization's read-only) and which servers may change (not the organization's). Droi follows the same rules in the shared daemon layer (`skills.ts`, `mcp.ts`, `use-skills.ts`, `use-mcp.ts`); the web Client shows both behind a wrench in the Session header, the Phone App in a sheet from the same place.

Every one of these calls takes a Session id: the Daemon answers for that Session's Workspace and applies a change to that Session's connections at once. Droi therefore manages them from the open Session rather than from Settings. The Factory App keeps a hidden pre-initialised Session for its new-session page to make the calls without one; Droi's Draft Session would serve the same way, but a Settings page that needs a Workspace to write project-level switches into would be a Settings page with a hidden Session behind it, and the Session header is where the Workspace is already known.

A sign-in an MCP Server needs is a page the Daemon names (`pendingAuthUrl`, `mcp_auth_required`) and a callback on the computer that the Daemon serves itself; Droi passes no `mcpOAuthCallbackUri` of its own. The Local Client opens the page in the default browser, where the callback works. From a Remote Client or the Phone App the page would send its callback to the wrong machine, so those show the link to copy and open on the computer instead.

## Considered Options

- **A Settings tab with a hidden Session, like the Factory App.** Rejected for now: the project-level switches need a Workspace, and Settings has none; the open Session has both.
- **Droi edits `settings.json` and `mcp.json` itself.** Rejected: the Daemon validates, merges the organization's policy, reconnects servers and tells other Clients; a file edit would do none of that and would not reach a running Session.
- **A Gateway route that opens the sign-in page on the computer for a Remote Client.** Left out: a remote party opening arbitrary pages on the computer is a wide door for a small convenience; copying the link covers it.

## Consequences

- A switch made in Droi shows up in the droid CLI and the Factory App, and in the next Session on the computer; and theirs show up in Droi at the next list (the cache is short-lived and the "/" menu refreshes with it).
- Skills and servers are per Session in the UI but not in effect: user-level switches change every Session on the computer.
- The Fake Daemon answers every call and pushes `mcp_status_changed`, so both suites cover the flows; the real sign-in is checked on the device checklist.
- Older Daemons without these methods fail the list with a method-not-found error, which the panel shows as it is.
