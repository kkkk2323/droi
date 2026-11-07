---
status: accepted
---

# Approvals, questions, plans and background work are tools the Kernel can wait on

Four things a run has to do from inside a turn — ask permission, ask the user a question, hand over a plan, and leave a program running — were all things the droid Daemon did its own way. Permissions and AskUser were protocol events with their own state machine: twelve `confirmationType`s in the Client, a "first answer wins" race between connected Clients, a `resolve_queued_user_message` call, and a queued question recognised by an `ask-user-answer:` message-id prefix. A background process was not a thing at all: Droi read the text droid wrote for the model and matched `Background process started (PID: N)` and `execute-completion:` (`pkg/transcript/background.go`). Spec mode was a Daemon mode beside the conversation.

The Kernel has one mechanism that covers all four: a tool call that does not return yet. A spike ran it (`spike-approval.ts`): a tool appends an entry, publishes a request, `await`s a promise held in memory, and resumes when the answer arrives over the protocol. Another killed the process mid-wait (`spike-crash.ts`): after `resume()` the tool comes back `interrupted`, no task survives, and the entry is still `pending`. So the answer's survival is ours to handle, and it is the only thing a crash leaves behind.

We decided:

- **Permission is a wrapper around the tools that change the computer**, not a protocol event. `read` never asks; `bash`, `edit` and `write` ask. The wrapper consults a **policy table** the Host owns and the Kernel caches: `allow`, `deny`, or `ask`, by tool, by command pattern, by path. The table arrives with `harness.info` and is refreshed when Settings changes it.
- **Four answers, and four levels of autonomy.** *Allow once*, *always allow this tool*, *always allow this file*, *cancel*, and the levels `ask`, `edits`, `safe`, `all` replace today's off/low/medium/high. "Always" is written by the Host, which owns the table, so it holds across Conversations and is the same setting Settings → Autonomy shows.
- **An approval is an entry and a request.** The wrapper writes a `droi.approval` entry with `status: "pending"`, publishes `approval.requested` to every attached Client, and returns when `approval.respond` names the outcome. A cancellation returns a blocked result to the model rather than stopping the run. No twelve-way `confirmationType`, and no per-type detail payload: the card is rendered from the tool's name and arguments (a command, a diff, a server and tool name).
- **A question is a tool too.** `ask_user` takes a list of questions; with `blocking: true` it waits like an approval, and with `blocking: false` it appends a `droi.ask` entry, returns "queued", and lets the run continue — the answers arrive as the next input, and the entry says what state the question is in. That removes the message-id convention and the "is the Session idle and does it hold a queued question" derivation: the entry carries its own state.
- **A background process is structured.** A `bash_background` tool spawns a detached process group, writes its output to `UserData/kernel/processes/<id>.log`, appends a `droi.process` entry (pid, command, start, exit code, log path), and returns at once with the pid, so the run continues instead of waiting. A watcher appends new output to the entry every two seconds and records the exit code; `process.stop` kills the group with `SIGTERM`, then `SIGKILL` after five seconds, and `process.list` reads the entries. The text parsing and `sessionview/jobs.go`'s branch for it are deleted. A two-second in-process timer is used rather than Pi's `sleepUntil` task, which exists for waiting until a known time and would fill the task table with polls.
- **Plan mode is a tool.** A plan mode extension exposes read-only tools plus `submit_plan`; the model submits a plan instead of making the change, the user reads it over the composer and chooses to continue with the same context or start a new Conversation, and the plan stays in the transcript as an assistant entry with a tool call. There is a `plan.ts` extension and no spec mode in the Kernel, no spec-mode protocol call, and no `droi.spec` entry kind.
- **Starting up, the Kernel reconciles what a crash left.** One pass over the database marks every `droi.approval` and `droi.ask` still `pending` as `abandoned`, and every `droi.process` still `running` whose pid does not answer `kill(pid, 0)` as `exited` with an unknown code. This is the one piece of state the Kernel cannot recover by itself, and it is about thirty lines.

## Considered options

- **Keep approvals as protocol events with their own state machine.** Rejected: it is a second mechanism for the same thing, and it forces the Client to model a permission separately from the tool call it belongs to.
- **Copy droid's twelve `confirmationType`s**, including the ones that stopped a Session or started a new one. Rejected: the four outcomes above cover what users actually choose, and the one case that started a new Session is plan mode's "new Conversation".
- **Ask for permission through a Pi hook instead of a tool wrapper.** Rejected: the hook fires per tool call without the args the policy needs in a stable shape, and the wrapper is also where the tool's own error and its entry live.
- **Keep polling the background process with `sleepUntil`, or keep parsing droid's text.** Rejected: the task table is for durable work, and parsing prose was the thing this replaces.
- **Answer an approval on the Conversation that started it only.** Rejected: a Session open in the Window and on the phone should race exactly as it does today, and `approval.requested` goes to every attached Client.

## Consequences

- **A subagent's approval belongs to the subagent's Conversation.** It is not mirrored into the parent's; a Client shows it under the parent because it can see the `droi.subagent` relationship, as it does today. ADR 0003's "every attached Client sees it and the first answer wins" still holds, per Conversation.
- **MCP tools ask too, generically.** A wrapper decides by server and tool name; there is no per-server confirmation shape to maintain.
- **The policy table is one place**, shared with Settings → Autonomy, and a Client that changes it changes it for every Conversation on that computer.
- **`bash_background` output is a log file the entry points at**, so a log that grows past what the transcript should hold does not grow the entry — the entry carries the tail.
