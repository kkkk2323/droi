---
status: accepted
---

# A Workstream is its Orchestrator's page: tabs for its agents and a panel beside them

_Replaces the desktop's part of ADR 0036's Clients: its Workstreams page and the page for each Workstream go. The Host, the tools, the tags and the Phone App stand._

ADR 0036 gave the desktop a Workstreams page that listed every Workstream, and a page for each, which held its facts and its agents. In use the Orchestrator's own Session, where the user does most of the talking, was one click away from where its agents were, a list of agents with their last reports read as states (an agent that had reported "done" and was working again still looked finished), and the sidebar was slow to show that an agent had started or waited. Factory's own app 1.5.0 puts the Orchestrator's Session first, with the agents beside it, and the user already knows that layout.

We decided:

- **A Workstream is its Orchestrator's Session.** The sidebar has a Workstreams group of its own (Beta) above the Workspaces: one row for each Workstream, with its symbol, its name, when it was last used, and a line for its agents (how many wait on the user, how many work). The group's plus button starts one. The Orchestrator leaves the session list; a Workstream Agent's Session opened from anywhere opens in its Workstream's page.
- **The page has a tab bar.** The first tab is the Orchestrator and cannot close; each agent the user opens is a tab after it, with a button that closes it. The tabs and whether the panel is out of sight are kept for each Workstream in the preferences.
- **The panel beside it, which the user can drag wider, has two tabs.** Agents are grouped as waiting on you, working, idle and archived. A row says what the agent waits for or does, and, when it neither waits nor works, when it last did anything. It does not say what the agent last reported: a report is a message to the Orchestrator, not a state. Files lists the Shared Folder. An agent the Host is starting has a row of its own.
- **Over the composer** the page says which agent has waited longest, with a button to open it, and counts the agents at work, which opens the panel. The Orchestrator's composer carries the Orchestrator's mark.
- **A Workstream starts from a symbol, a name and a folder.** There is no goal to write and no model to pick: the user says what to do in the Orchestrator's Session, which greets them. The models, the name and the symbol are in the Workstream's settings, and its Orchestrator's model changes like any Session's, from its next turn.
- **The Workstream tools read as what they did** in the transcript, in the Orchestrator's and in an agent's, also when a Script made the calls: an agent started, a message sent, the agents listed, an agent's report. A run of the same call is a group ("Started 2 of 3 agents"), and a message from an agent is a card that opens to its report.
- **An agent that can use only Direct tools wears a mark** when this device does not ask for Direct, which says why and, in the settings, offers Script for the model on this computer (ADR 0028).

## Considered Options

- **Keep the Workstreams page and add the Orchestrator beside it.** Rejected: two places for one thing, and the list had little to say that the sidebar's group does not.
- **The agents' Sessions in the sidebar under the Workstream.** Rejected: it fills the sidebar for every agent and takes the agents out of the Orchestrator's page, where the user reads their reports.

## Consequences

- The Workstreams page's routes are gone. A Workstream is a Session route; the page decides what it shows from the Session's tag.
- The sidebar's status for a Workstream and the list of agents come from the Host's records and the Daemon's list of opened Sessions, read again when the Host says they changed (and when a Session this window did not know works), not on a timer alone.
