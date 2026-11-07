---
status: accepted
---

# A Conversation's children are one thing, and their tools run in the Kernel

ADR 0036 gave Workstreams an MCP Server Droi carried: `spawn_agent`, `send_message`, `list_agents`, `interrupt_agent`, `archive_agent` and `report`, spoken over stdio to the Host, which carried each call out with the Daemon. It had to be a server because a droid Session can only be given tools through MCP, and the tools had to reach the Host because the Host was the only process that could start another Session. ADR 0036's Workstream Agent was a separate Session with a tag and metadata; ADR 0031's subagent was another one, with `callingSessionId` and `callingToolUseId`; the Automations needed a tool of their own to do anything the window could not. Three mechanisms for one idea, and a fourth process on the machine (Anlan Droi's tool server is the same shape, for the same reason).

The Kernel has children of its own: a Conversation can own another Conversation, and the parent's tool drives it. That is how Pi's subagents work, and it is what ADR 0038 already relies on for `droi.subagent`. So the workaround goes, and the three mechanisms collapse into Pi's one.

We decided:

- **A subagent and a Workstream Agent are the same thing: a child Conversation.** Both are owned by the Conversation that started them, both are created by a tool the Kernel runs in its own process, both can be interrupted by their parent, and both are hidden from the session list by the ownership relation rather than by a tag. What differs is only what a Client draws: a subagent appears as a card inside the tool call that spawned it, and a Workstream Agent appears as a tab of its Orchestrator's page and a row of its panel (ADR 0037, which stands unchanged).
- **The Workstream tools and the Automation's tools are Kernel extension tools, not an MCP Server.** They are `defineTool` definitions the Kernel registers for a Conversation whose `kind` says it is an Orchestrator or a Workstream Agent, chosen per Conversation through the same `modelTools` a Client configures (ADR 0038). Nothing listens on stdio, nothing is pre-approved, and `pkg/mcpserver` stops being the way Droi's own capabilities reach the agent.
- **An Orchestrator's interruption cascades to its children.** `conversation.abort` on an Orchestrator stops the children's runs as well, because a child whose parent is gone has nobody to report to; a child's own abort stops only that child. This is the Kernel's rule, not a Client's, so both Clients get it for free.
- **A Workstream Agent reports by submitting into its Orchestrator's inbox.** `report` is a submission to the parent's Conversation with the agent's id as the `requestId`, so a report that is retried after a dropped connection lands once (the protocol's deduplication, ADR 0038) and an Orchestrator that was idle starts a run with the report as its input. The `droi.subagent` entry keeps naming the relationship; the report itself is an ordinary `pi.user`-shaped input, not a new entry kind, so the Orchestrator's context holds what it was told in the order it was told.
- **A Workstream's own record stays the Host's.** Who the agents are, the Shared Folder, the model each agent runs on, the sidebar row and the archive are still the Host's, because they outlive any one Conversation and a Client asks the Host for them. What the Host no longer does is carry each tool call.

## Why this is the right split

The tool belongs where the Conversation is, because the tool's whole job is to create and drive Conversations: the Kernel owns them, so the Kernel can do it without a round trip through the Host, and without a second process whose only purpose was to be a place for the call to arrive. The Host keeps what is not about a Conversation: the record, the folder, the schedule, the model policy.

It also removes a class of bug. Today an MCP tool call can be refused before it runs (the pre-approval), can fail after it ran, and can be repeated by the model; with a Kernel tool the call is a function call inside one process, its result is the tool result, and a retry is the model's retry with the same `requestId` deduplicating the report.

## Considered options

- **Keep the MCP Server and port it to a Kernel extension that speaks MCP.** Rejected: it is a protocol between two processes that are already one process.
- **Keep two kinds of child (subagent and Workstream Agent).** Rejected: three mechanisms for one idea is what this ADR removes; the difference users care about is where the agent is shown, which is a rendering decision, not a Kernel one.
- **Have the Host keep carrying the calls.** Rejected: it needs the Host to be reachable from the agent's turn, which is exactly what the MCP Server was for.
- **Reports as a new entry kind** (`droi.report`). Rejected: a report is a message to the Orchestrator, and a message already has a shape, an order in the context and a deduplication key. A new kind would need all three again.

## Consequences

- **One fewer process on the machine**, and one fewer MCP server in the MCP panel: Droi's own tools are the Kernel's, not a server the user can switch off.
- **`interrupt_agent` becomes `conversation.abort` on the child**, and `list_agents` reads the children's views. The tools' names stay for the model's sake, because the model was taught them in the Workstream's prompt.
- **A Workstream Agent's run no longer starts with a cold agent**; the child shares the Kernel and its model configuration.
- **ADR 0036's MCP Server goes**, and with it the Host's `spawn_agent` handling and the tag that hid an agent from the session list. ADR 0036's record, Shared Folder, model policy and the Workstreams page stand.
- **The Automations no longer need a tool the window lacks**: an Automation's tool is a Kernel tool too, chosen by its Conversation's kind.
- **Anlan Droi's `anlan-mcp` server is unaffected**: it is a third party's tools, which is what MCP is for. What changes is only Droi's own capabilities.

## What this ADR does not decide

- **The exact tool names a Client shows** for the Workstream tools (the model's names stay; what a card says is a UI question).
- **Whether a child can outlive its parent.** Today a child whose parent is archived is archived with it; a child that reports into an archived Orchestrator is dropped with a warning.
