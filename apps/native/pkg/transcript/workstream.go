package transcript

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// WorkstreamServer is the tool server of a Workstream's Sessions, as the
// Daemon prefixes its tools' names.
const WorkstreamServer = "droi-workstream"

// The tools of the Workstream server.
const (
	ToolSpawnAgent     = "spawn_agent"
	ToolSendMessage    = "send_message"
	ToolListAgents     = "list_agents"
	ToolInterruptAgent = "interrupt_agent"
	ToolArchiveAgent   = "archive_agent"
	ToolReport         = "report"
)

// WorkstreamTool is the tool of the Workstream server that call is, ok false
// for any other call.
func WorkstreamTool(c *ToolCall) (tool string, ok bool) {
	server, name := ToolName(c.Use.Name)
	if server != WorkstreamServer {
		return "", false
	}
	switch name {
	case ToolSpawnAgent, ToolSendMessage, ToolListAgents, ToolInterruptAgent, ToolArchiveAgent, ToolReport:
		return name, true
	}
	return "", false
}

// CallState is how far a call went.
type CallState uint8

const (
	CallRunning CallState = iota
	CallDone
	CallFailed
)

// StateOf is how far call went: running until its result came.
func StateOf(c *ToolCall) CallState {
	switch {
	case c.Result == nil:
		return CallRunning
	case c.Result.IsError != nil && *c.Result.IsError:
		return CallFailed
	}
	return CallDone
}

var agentInResult = regexp.MustCompile(`agent "([^"]*)" \(([^)]+)\)`)

// AgentOf is the agent a Workstream call is about, as far as the call tells:
// its Session id and its title. An orchestrator's call names the agent by id
// and the result gives its title; a spawn gives the title and the result the
// id.
func AgentOf(c *ToolCall) (id, title string) {
	id, _ = c.Input["agent_id"].(string)
	title, _ = c.Input["title"].(string)
	if m := agentInResult.FindStringSubmatch(ResultText(c.Result)); m != nil {
		if title == "" {
			title = m[1]
		}
		if id == "" {
			id = m[2]
		}
	}
	return strings.TrimSpace(id), strings.TrimSpace(title)
}

// WorkstreamLine is what a Workstream call's row says: what it did to the
// agent called name. The spawn names no agent in its line: its row carries
// the title.
func WorkstreamLine(c *ToolCall, tool, name string) string {
	pick := func(running, done, failed string) string {
		switch StateOf(c) {
		case CallRunning:
			return fill(running, name)
		case CallFailed:
			return fill(failed, name)
		}
		return fill(done, name)
	}
	switch tool {
	case ToolSpawnAgent:
		return pick(l10n.N("Starting up"), l10n.N("Started"), l10n.N("Failed to start"))
	case ToolSendMessage:
		return pick(l10n.N("Messaging %s"), l10n.N("Messaged %s"), l10n.N("Failed to message %s"))
	case ToolInterruptAgent:
		return pick(l10n.N("Interrupting %s"), l10n.N("Interrupted %s"), l10n.N("Failed to interrupt %s"))
	case ToolArchiveAgent:
		return pick(l10n.N("Archiving %s"), l10n.N("Archived %s"), l10n.N("Failed to archive %s"))
	case ToolListAgents:
		return pick(l10n.N("Listing available agents"), l10n.N("Listed available agents"), l10n.N("Failed to list agents"))
	}
	return pick(l10n.N("Sending report to the orchestrator"), l10n.N("Sent report back to the orchestrator"), l10n.N("Failed to send the report"))
}

// fill is the catalog's text of key with args, which a key without verbs
// leaves out.
func fill(key string, args ...any) string {
	text := l10n.T(key)
	if !strings.Contains(text, "%") {
		return text
	}
	return fmt.Sprintf(text, args...)
}

// groupable are the tools whose calls one after another are shown as a
// group: the same thing done to several agents.
func groupable(tool string) bool {
	switch tool {
	case ToolSpawnAgent, ToolSendMessage, ToolInterruptAgent, ToolArchiveAgent:
		return true
	}
	return false
}

// WorkstreamRuns cuts calls into the runs of the same groupable Workstream
// tool that follow each other; any other call is a run of its own.
func WorkstreamRuns(calls []*ToolCall) [][]*ToolCall {
	var runs [][]*ToolCall
	last := ""
	for _, c := range calls {
		tool, ok := WorkstreamTool(c)
		if n := len(runs); ok && groupable(tool) && tool == last && n > 0 {
			runs[n-1] = append(runs[n-1], c)
			continue
		}
		runs = append(runs, []*ToolCall{c})
		last = ""
		if ok && groupable(tool) {
			last = tool
		}
	}
	return runs
}

// WorkstreamGroup is the header of a run of calls of one groupable tool
// done to several agents: "Starting 3 agents", "Started 3 agents", "Started 2
// of 3 agents". ok is false for a run of fewer than two or of other calls.
func WorkstreamGroup(run []*ToolCall) (label string, ok bool) {
	if len(run) < 2 {
		return "", false
	}
	tool, ok := WorkstreamTool(run[0])
	if !ok || !groupable(tool) {
		return "", false
	}
	running, failed := false, 0
	for _, c := range run {
		if t, same := WorkstreamTool(c); !same || t != tool {
			return "", false
		}
		switch StateOf(c) {
		case CallRunning:
			running = true
		case CallFailed:
			failed++
		}
	}
	n, done := len(run), len(run)-failed
	verbs := map[string][4]string{
		ToolSpawnAgent: {l10n.N("Starting %d agents"), l10n.N("Started %d agents"), l10n.N("Started %d of %d agents"), l10n.N("Failed to start %d agents")},
		ToolSendMessage: {l10n.N("Messaging %d agents"), l10n.N("Messaged %d agents"), l10n.N("Messaged %d of %d agents"),
			l10n.N("Failed to message %d agents")},
		ToolInterruptAgent: {l10n.N("Interrupting %d agents"), l10n.N("Interrupted %d agents"), l10n.N("Interrupted %d of %d agents"),
			l10n.N("Failed to interrupt %d agents")},
		ToolArchiveAgent: {l10n.N("Archiving %d agents"), l10n.N("Archived %d agents"), l10n.N("Archived %d of %d agents"), l10n.N("Failed to archive %d agents")},
	}[tool]
	switch {
	case running:
		return fill(verbs[0], n), true
	case failed == n:
		return fill(verbs[3], n), true
	case failed > 0:
		return fill(verbs[2], done, n), true
	}
	return fill(verbs[1], n), true
}

// AgentMessage is a message the Host gave the orchestrator for an agent's
// report, or for the end of its turn.
type AgentMessage struct {
	Title, ID string
	// Kind is the report's: done, blocked or update; "" for a note of Droi's.
	Kind string
	Body string
}

// ReadAgentMessage reads a message headed [From agent "<title>" (<id>)].
func ReadAgentMessage(text string) (AgentMessage, bool) {
	const head = `[From agent "`
	rest, ok := strings.CutPrefix(text, head)
	if !ok {
		return AgentMessage{}, false
	}
	title, rest, ok := strings.Cut(rest, `" (`)
	if !ok {
		return AgentMessage{}, false
	}
	id, rest, ok := strings.Cut(rest, ")]")
	if !ok || id == "" {
		return AgentMessage{}, false
	}
	m := AgentMessage{Title: title, ID: id}
	rest = strings.TrimPrefix(rest, " ")
	first, more, hasMore := strings.Cut(rest, "\n")
	switch first {
	case "done", "blocked", "update":
		m.Kind, m.Body = first, strings.TrimSpace(more)
	default:
		m.Body = strings.TrimSpace(rest)
		_ = hasMore
	}
	return m, true
}
