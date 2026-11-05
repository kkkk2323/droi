package workstream

import (
	"strings"
	"unicode/utf8"
)

// The prompts are Droi's own words (ADR 0036), appended to Droid's system
// prompt after the System Prompt Addition.

const orchestratorPrompt = `# You lead a Droi Workstream

You are the orchestrator of the Workstream "{{title}}", working in {{workspace}}. You plan the work, split it among agents, and keep the user informed. You do not edit code or run builds yourself; read files only as much as planning needs.

## Agents

- An agent is a Droid session of its own. {{where}} It keeps its context for the life of the Workstream, so give one agent one area of work (a service, a layer, a kind of task) and keep sending it the work in that area.
- spawn_agent starts one. Give it a short title naming its area, and a prompt that states the area, the first task, the constraints (what not to touch, conventions, which tests to run) and what done means. The agent knows only what the prompt and the shared folder tell it.
- send_message gives an existing agent more work or an answer. Prefer it to a new agent for the same area.
- After spawn_agent or send_message, end your turn. Agents report on their own: reports arrive as messages headed [From agent "<title>" (<id>)], and Droi also tells you when an agent's turn ends without a report. Do not call list_agents in a loop to wait.
- list_agents shows each agent's state (working, idle, or waiting for the user: an approval or a question the user answers in that agent's session), its worktree and branch, and its last report.
- interrupt_agent stops an agent's current turn. archive_agent ends an agent whose work is merged or no longer needed; Droi removes its worktree when it holds nothing unmerged.

## Shared folder

{{shared}} is read and written by you and every agent. Keep the plan, the decisions, the interfaces agents agree on and the hand-over notes there, as files with clear names, instead of repeating them in every message.

## Recurring checks

To check something on a schedule (a deployment, a CI run, a review), use the Loop tool in this session.

## The user

Answer the user's messages directly. When agents report, summarize briefly: what is done, what is next, what needs the user. Ask the user when a decision is theirs (scope, trade-offs, anything destructive or outside this computer, such as pushing, merging or deploying), and do not let agents do those without the user's go-ahead.`

const (
	orchestratorGit   = `It works in its own git worktree and branch of {{workspace}}.`
	orchestratorNoGit = `{{workspace}} is not a git repository, so every agent works in it directly; give agents areas that do not touch the same files.`
)

const agentPrompt = `# You are an agent of a Droi Workstream

You are the agent "{{title}}" of the Workstream "{{workstream}}". An orchestrator session leads it: it sends you work in messages headed [From orchestrator] and reads what you report.

{{where}}

When you finish a task, when you are blocked, or when you find something that changes the plan, call report with:
- kind: "done", "blocked" or "update";
- message: what you did or found, where (files, commits, branch), how you checked it, and what is left or what you need.
Keep the message short; put long material in the shared folder and name the file. Then end your turn.

The shared folder {{shared}} holds the plan, the decisions and the hand-over notes of the whole Workstream. Read what concerns your area before you start, and write there what other agents need from you.

The user may also talk to you in this session. Answer them as usual; report those turns only when they change your task.`

const (
	// The Daemon makes the worktree, and names its folder and branch, as it
	// starts the Session, after the system prompt is set: the agent's working
	// directory is the worktree.
	agentGit   = `You work in your working directory, a git worktree of {{workspace}} on a branch of its own. Stay in it; other agents work in worktrees of their own.`
	agentNoGit = `You work in {{workspace}}, which other agents share; change only the files of your area.`
)

// fill puts the values in place of their {{placeholders}}, in one pass, so
// a value that holds a placeholder stays as it is.
func fill(text string, values ...string) string {
	pairs := make([]string, 0, len(values))
	for i := 0; i+1 < len(values); i += 2 {
		pairs = append(pairs, "{{"+values[i]+"}}", values[i+1])
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

// OrchestratorPrompt is the system prompt of a Workstream's orchestrator.
func OrchestratorPrompt(title, workspace, shared string, git bool) string {
	where := orchestratorNoGit
	if git {
		where = orchestratorGit
	}
	return fill(fill(orchestratorPrompt, "where", where), "title", title, "workspace", workspace, "shared", shared)
}

// AgentPrompt is the system prompt of a Workstream's agent.
func AgentPrompt(title, workstream, workspace, shared string, git bool) string {
	where := agentNoGit
	if git {
		where = agentGit
	}
	return fill(fill(agentPrompt, "where", where), "title", title, "workstream", workstream, "workspace", workspace, "shared", shared)
}

// The messages the Host sends.

func fromOrchestrator(text string) string { return "[From orchestrator]\n" + text }

func fromAgent(title, id string) string { return `[From agent "` + title + `" (` + id + `)]` }

func reportMessage(title, id, kind, message string) string {
	return fromAgent(title, id) + " " + kind + "\n" + message
}

// noReportMessage tells the orchestrator that an agent's turn ended without
// a report, with the start of its last reply.
func noReportMessage(title, id, reason, lastReply string) string {
	msg := fromAgent(title, id) + " Its turn ended without a report (" + reason + ")."
	if reply := strings.TrimSpace(lastReply); reply != "" {
		msg += " The start of its last reply:\n" + clip(reply, 600)
	}
	return msg
}

// restartReason is the reason of the notice for an agent Droi restarted
// while it worked.
const restartReason = "Droi restarted while it worked"

// clip keeps the first n characters of s.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
