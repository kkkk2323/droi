---
status: proposed
---

# Droi runs Workstreams of its own: an Orchestrator and Workstream Agents the Host starts

Factory's Workstreams (Missions 2.0, `mission-v2` in droid) let one Session, the orchestrator, split a long goal among agent Sessions it spawns and keeps. Droi could start one through the Daemon alone (`interactionMode: "mission"` and a `mission-v2` tag), but only for an organization with the `missions_v2` flag. The roster then comes from Factory's backend whenever Cloud Sync is on, every local agent works in the orchestrator's one directory, agents are created inside the Daemon without the Gateway's System Prompt Addition, and Droi can neither see nor change the prompts and behavior. What the user would use it for is narrower than Factory's pitch. Over two months (September 5 to November 5, 2026), 15% of the Sessions the user started spanned two days or more and held 49% of the messages the user wrote; across all of them the user nudged with "check" or "continue" 198 times and handed context over by pasting another Session's id 61 times. Three Sessions at once in one repository happened in 11 of 941 active half-hours. So the value is a long-lived lead, standing helpers that keep their context, being told when something waits, and hand-overs without pasted ids, not many agents at once. A spike on a bare Daemon (2026-11-05, branch `spike/droi-workstream`) ran MCP tools → Host → agent Sessions in Worktrees of their own → reports back, and passed in each of its three recorded runs.

We decided:

- **A Workstream is Droi's.** It is an Orchestrator Session and the Workstream Agents it spawns, all Sessions on one computer's Daemon, and a Shared Folder in Droi's data (`<Droi data>/workstreams/<id>/shared/`) that all of them read and write. A Session's `droi.workstream` tag (`workstreamId`, `role`) says which Workstream it belongs to and as what, so every Client groups them from the Daemon's list.
- **The Orchestrator coordinates through tools and does none of the work.** An MCP Server Droi carries gives it `spawn_agent`, `send_message`, `list_agents` (status, what an agent waits on, last activity, Worktree, last report), `interrupt_agent` and `archive_agent`, and gives a Workstream Agent `report`. Calls go to the Host, which carries them out with the Daemon: as in ADR 0011, the Host, not a tool process, holds the Daemon and the credential. Only Workstream Sessions get these tools.
- **The Host starts each Workstream Agent** as it starts an Automation run (ADR 0035): in a persistent Worktree of its own, since the Daemon deletes ephemeral ones past its limit of 15, with the System Prompt Addition and a prompt that names its Orchestrator, its area and the Shared Folder.
- **The user picks the agents' model and reasoning effort**, one of each per Workstream, as Factory's Workstreams do. They live on the Orchestrator's `droi.workstream` tag (`agentModel`, `agentReasoningEffort`), so every Client shows and changes them, and the picker lists the models the Daemon offers here, this computer's custom models included, since every agent runs on it. Unset, an agent starts on the Orchestrator's model and effort at the moment it is spawned; a model set without an effort takes the Orchestrator's effort, fitted to what the model supports. `spawn_agent` takes no model. When the settings change, the Host moves each agent to them before its next turn, except an agent whose model or effort the user changed in its own Session: that one keeps the user's pick. Each agent's tag records the settings it last took up, which is how the Host tells the two apart.
- **Agents report, and the Host backs them up.** A Workstream Agent calls `report` when its work is done, when it is blocked, and when it finds something that changes the plan. When an agent's turn ends without a report, or fails, the Host sends the Orchestrator a short notice with the start of the agent's last reply. A turn the user started in an agent is not forwarded. Messages to the Orchestrator are headed `[From agent "<title>" (<id>)]`.
- **Recurring checks are Loops** (ADR 0023) that the Orchestrator makes with Droid's own Loop tool. They run on the computer, never reach Factory, and Droi already shows them and keeps their Session's worker running. Droi adds no scheduling of its own for Workstreams.
- **Archiving an agent** interrupts it, archives its Session, and removes its Worktree when the branch is merged or has no changes. A Worktree with changes stays, and the Orchestrator and the user are told.
- **The prompts are Droi's own words.** Factory's mission-v2 prompts are closed source, read from a decompiled build; Droi is public under MIT and copies none of them.
- **Clients.** The desktop has a Workstream page: the Orchestrator's transcript beside its agents, grouped as waiting on you, working and idle. The sidebar keeps agents under their Workstream, and the desktop's alerts already cover an agent that waits. The Phone App shows the same groups and answers an agent's Prompts, away from home through the Relay (ADR 0034).
- **Not now:** Listeners (a webhook that sends work to an Orchestrator, after ADR 0035's webhook trigger), renaming agents, agents on other computers, and pushes to the phone.

## Considered Options

- **Factory's Workstreams through the Daemon.** Rejected for the reasons above: a Beta flag, Factory's backend for the roster, one shared directory, no Gateway, a black box. They stay usable in the Factory App.
- **Task subagents.** Enough for one-off work, which most Sessions are. But a subagent ends with its one answer, the user cannot talk to it, and its result fills the caller's context.
- **Agents report only with a message tool, as Factory's do.** Rejected alone: a model that forgets leaves the Orchestrator waiting, and Factory's own code also reports failed agent turns.
- **The Host forwards every finished turn, as the spike did.** Rejected alone: it also forwards the user's own turns with an agent, and it cannot report in the middle of a turn.
- **A schedule tool of Droi's own.** Rejected: Loops already do it, on the computer.
- **The Orchestrator picks each agent's model**, by tier (Droid's light, medium and heavy subagent models) or by id. Not now: the cost of a Workstream becomes harder to foresee, and ids written into a prompt go stale. A tier can come later as an optional `spawn_agent` parameter without changing the rest.
- **Droid's subagent settings decide.** They belong to Task subagents (a custom droid's `model`, a built-in's tier); a Workstream Agent is a whole Session the user also talks to.

## Consequences

- Token use grows with every agent; with the Key Pool on, all of it goes through the Key Pool.
- Each working agent keeps a worker process, and each agent keeps a Worktree until it is archived; Settings → Worktrees shows any that a crash left behind.
- Memory summarizes the Orchestrator like any Session the user works in, and skips Workstream Agents, whose work reaches the Orchestrator in reports.
- Away from the computer, the phone hears of a waiting agent only while the Phone App is open: pushes are a later decision.
- `CONTEXT.md` gains Workstream, Orchestrator, Workstream Agent and Shared Folder when this lands.

## Open points (before this is accepted)

- Whether the Daemon keeps the MCP servers given in `initialize_session` across `load_session`. If not, the tools go through the Runtime Overlay, and the Host refuses calls from other Sessions, as in the spike.
- Whether an agent at low autonomy is asked before it writes to the Shared Folder outside its Worktree. If so, each Worktree gets a link to it, listed in `.git/info/exclude`.
- Whether the Loop tool is there in an Orchestrator Session that Droi starts.
- The whole path through the Gateway and the Key Pool (the spike dialed the Daemon directly), and one real task on the user's usual model: tokens, time, and the user's review time.
