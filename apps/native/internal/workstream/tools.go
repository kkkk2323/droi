package workstream

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/hosttools"
	"github.com/kkkk2323/droi/apps/native/pkg/mcpserver"
)

// ServerName is the tool server of Workstream Sessions, carried through the
// Host tool bridge.
const ServerName = "droi-workstream"

// The tools: the orchestrator's, and an agent's report.
const (
	ToolSpawnAgent     = "spawn_agent"
	ToolSendMessage    = "send_message"
	ToolListAgents     = "list_agents"
	ToolInterruptAgent = "interrupt_agent"
	ToolArchiveAgent   = "archive_agent"
	ToolReport         = "report"
)

var orchestratorTools = []string{ToolSpawnAgent, ToolSendMessage, ToolListAgents, ToolInterruptAgent, ToolArchiveAgent}

func hostToolsSelector(tools ...string) []string { return hosttools.Selector(ServerName, tools...) }

var agentID = map[string]any{"type": "string", "description": "The agent's session id, from spawn_agent or list_agents."}

func object(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// toolList is the tools/list answer's "tools".
var toolList = []map[string]any{
	{
		"name":        ToolSpawnAgent,
		"description": "Start an agent: a session of its own, in its own git worktree when the workspace is a git repository. It gets the prompt as its first message. Its report arrives later as a message in this session.",
		"inputSchema": object(map[string]any{
			"title":  map[string]any{"type": "string", "description": "A short title naming the agent's area of work."},
			"prompt": map[string]any{"type": "string", "description": "The area, the first task, the constraints and what done means."},
		}, "title", "prompt"),
	},
	{
		"name":        ToolSendMessage,
		"description": "Send an agent more work or an answer. Its report arrives later as a message in this session.",
		"inputSchema": object(map[string]any{
			"agent_id": agentID,
			"message":  map[string]any{"type": "string", "description": "The work or the answer."},
		}, "agent_id", "message"),
	},
	{
		"name":        ToolListAgents,
		"description": "List this workstream's agents: state, last activity, worktree, branch and last report.",
		"inputSchema": object(map[string]any{
			"include_archived": map[string]any{"type": "boolean", "description": "Also list archived agents."},
		}),
	},
	{
		"name":        ToolInterruptAgent,
		"description": "Stop an agent's current turn.",
		"inputSchema": object(map[string]any{"agent_id": agentID}, "agent_id"),
	},
	{
		"name":        ToolArchiveAgent,
		"description": "Archive an agent whose work is merged or no longer needed. Droi removes its worktree when it holds nothing unmerged, and says so when it keeps it.",
		"inputSchema": object(map[string]any{"agent_id": agentID}, "agent_id"),
	},
	{
		"name":        ToolReport,
		"description": "Report to the orchestrator that leads you: when a task is done, when you are blocked, or when you find something that changes the plan.",
		"inputSchema": object(map[string]any{
			"kind":    map[string]any{"type": "string", "enum": []string{"done", "blocked", "update"}, "description": "done, blocked or update."},
			"message": map[string]any{"type": "string", "description": "What you did or found, where (files, commits, branch), how you checked it, and what is left or what you need."},
		}, "kind", "message"),
	},
}

// Server is the droi-workstream tool server for the Host tool bridge. Its
// tools run without asking: they only reach the Workstream's own Sessions.
func (c *Coordinator) Server() hosttools.Server {
	tools, _ := json.Marshal(toolList)
	return hosttools.Server{
		Tools:    func() json.RawMessage { return tools },
		Call:     c.Call,
		Autonomy: string(protocol.AutonomyLevelLow),
	}
}

// Call runs one tool for the Session sessionID, refusing a Session the
// tool is not for.
func (c *Coordinator) Call(ctx context.Context, tool string, args map[string]any, sessionID string) (mcpserver.Result, error) {
	if tool == ToolReport {
		w, a := c.agentOf(sessionID)
		if a == nil {
			return mcpserver.Result{}, fmt.Errorf("Only an agent of a Droi workstream can call %s.", tool)
		}
		kind, text := str(args, "kind"), str(args, "message")
		if kind != "done" && kind != "blocked" && kind != "update" {
			return mcpserver.Result{}, fmt.Errorf("kind must be done, blocked or update.")
		}
		if text == "" {
			return mcpserver.Result{}, fmt.Errorf("message is empty.")
		}
		if err := c.report(ctx, w, a, kind, text); err != nil {
			return mcpserver.Result{}, fmt.Errorf("The report did not reach the orchestrator: %v", err)
		}
		return mcpserver.Text("The orchestrator has your report.", false), nil
	}
	known := false
	for _, t := range orchestratorTools {
		known = known || t == tool
	}
	if !known {
		return mcpserver.Result{}, fmt.Errorf("No tool named %s.", tool)
	}
	w := c.orchestratorOf(sessionID)
	if w == nil {
		return mcpserver.Result{}, fmt.Errorf("Only the orchestrator of a Droi workstream can call %s.", tool)
	}
	switch tool {
	case ToolSpawnAgent:
		title, prompt := str(args, "title"), str(args, "prompt")
		if title == "" || prompt == "" {
			return mcpserver.Result{}, fmt.Errorf("spawn_agent needs a title and a prompt.")
		}
		a, err := c.spawn(ctx, w, title, prompt)
		if err != nil && a != nil {
			return mcpserver.Result{}, fmt.Errorf("Started agent %q (%s), but it did not get its task: %v. Send it with send_message.", a.Title, a.SessionID, err)
		}
		if err != nil {
			return mcpserver.Result{}, fmt.Errorf("The agent was not started: %v", err)
		}
		where := "It works in the workspace " + w.Workspace + "."
		if a.WorktreePath != "" {
			where = "It works in the worktree " + a.WorktreePath + " on the branch " + a.Branch + "."
		}
		return mcpserver.Text(fmt.Sprintf("Started agent %q (%s). %s Its report arrives as a message in this session; end your turn now.", a.Title, a.SessionID, where), false), nil
	case ToolListAgents:
		all, _ := args["include_archived"].(bool)
		return c.listAgents(ctx, w, all)
	}
	a, err := c.agentOfWorkstream(w, str(args, "agent_id"))
	if err != nil {
		return mcpserver.Result{}, err
	}
	switch tool {
	case ToolSendMessage:
		text := str(args, "message")
		if text == "" {
			return mcpserver.Result{}, fmt.Errorf("message is empty.")
		}
		if err := c.message(ctx, w, a, text); err != nil {
			return mcpserver.Result{}, fmt.Errorf("The message did not reach the agent: %v", err)
		}
		return mcpserver.Text(fmt.Sprintf("Sent to agent %q (%s). Its report arrives as a message in this session; end your turn now.", a.Title, a.SessionID), false), nil
	case ToolInterruptAgent:
		if err := c.interrupt(ctx, w, a); err != nil {
			return mcpserver.Result{}, fmt.Errorf("The agent was not interrupted: %v", err)
		}
		return mcpserver.Text(fmt.Sprintf("Interrupted agent %q (%s).", a.Title, a.SessionID), false), nil
	default:
		removed, kept, err := c.archiveAgent(ctx, w, a)
		if err != nil {
			return mcpserver.Result{}, fmt.Errorf("The agent was not archived: %v", err)
		}
		msg := fmt.Sprintf("Archived agent %q (%s).", a.Title, a.SessionID)
		switch {
		case removed:
			msg += " Removed its worktree " + a.WorktreePath + "."
		case kept != "":
			msg += fmt.Sprintf(" Kept its worktree %s on the branch %s: %s. Tell the user.", a.WorktreePath, a.Branch, kept)
		}
		return mcpserver.Text(msg, false), nil
	}
}

func str(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return strings.TrimSpace(s)
}

// agentOfWorkstream is w's active agent id names.
func (c *Coordinator) agentOfWorkstream(w *Workstream, id string) (*Agent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := agentIn(w, id)
	switch {
	case id == "":
		return nil, fmt.Errorf("agent_id is empty.")
	case a == nil:
		return nil, fmt.Errorf("This workstream has no agent %s.", id)
	case a.ArchivedAt != "":
		return nil, fmt.Errorf("Agent %q (%s) is archived.", a.Title, id)
	}
	return a, nil
}

type listedReport struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	At      string `json:"at"`
}

type listedAgent struct {
	AgentID      string        `json:"agent_id"`
	Title        string        `json:"title"`
	Status       string        `json:"status"`
	LastActivity string        `json:"last_activity"`
	Worktree     string        `json:"worktree,omitempty"`
	Branch       string        `json:"branch,omitempty"`
	LastReport   *listedReport `json:"last_report,omitempty"`
}

// listAgents answers w's agents as JSON: their states from the Runner's
// store for the ones it follows, else from the Daemon's opened Sessions.
func (c *Coordinator) listAgents(ctx context.Context, w *Workstream, archived bool) (mcpserver.Result, error) {
	c.mu.Lock()
	var agents []Agent
	for _, a := range w.Agents {
		if archived || a.ArchivedAt == "" {
			agents = append(agents, *a)
		}
	}
	activity := map[string]time.Time{}
	for _, a := range agents {
		activity[a.SessionID] = c.activity[a.SessionID]
	}
	c.mu.Unlock()
	var opened map[string]protocol.DaemonOpenedSessionInfo
	out := make([]listedAgent, 0, len(agents))
	for _, a := range agents {
		var (
			state              protocol.DroidWorkingState
			permissions, asked int
		)
		last := activity[a.SessionID]
		if s := c.cfg.Sessions.Session(a.SessionID); s != nil && c.cfg.Sessions.Following(a.SessionID) {
			state = s.WorkingState()
			permissions, asked = c.cfg.Sessions.Pending(a.SessionID)
			if t := s.WorkingStateChangedAt(); t.After(last) {
				last = t
			}
		} else if a.ArchivedAt == "" {
			if opened == nil {
				opened = c.openedSessions(ctx)
			}
			if o, ok := opened[a.SessionID]; ok {
				state = o.WorkingState
				if o.PendingPermissionRequestCount != nil {
					permissions = int(*o.PendingPermissionRequestCount)
				}
				if o.PendingAskUserRequestCount != nil {
					asked = int(*o.PendingAskUserRequestCount)
				}
			}
		}
		status := "idle"
		switch {
		case a.ArchivedAt != "":
			status = "archived"
		case permissions > 0 || state == protocol.DroidWorkingStateWaitingForToolConfirmation:
			status = "waiting on you: approval"
		case asked > 0:
			status = "waiting on you: question"
		case state != "" && state != protocol.DroidWorkingStateIdle:
			status = "working"
		}
		for _, at := range []string{a.CreatedAt, reportAt(a.LastReport)} {
			if t, err := time.Parse(time.RFC3339, at); err == nil && t.After(last) {
				last = t
			}
		}
		l := listedAgent{AgentID: a.SessionID, Title: a.Title, Status: status, Worktree: a.WorktreePath, Branch: a.Branch}
		if !last.IsZero() {
			l.LastActivity = last.UTC().Format(time.RFC3339)
		}
		if r := a.LastReport; r != nil {
			l.LastReport = &listedReport{Kind: r.Kind, Message: clip(r.Text, 300), At: r.At}
		}
		out = append(out, l)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return mcpserver.Result{}, err
	}
	return mcpserver.Text(string(b), false), nil
}

func reportAt(r *Report) string {
	if r == nil {
		return ""
	}
	return r.At
}

// openedSessions are the Sessions the Daemon has open, by id; none when it
// does not answer.
func (c *Coordinator) openedSessions(ctx context.Context) map[string]protocol.DaemonOpenedSessionInfo {
	out := map[string]protocol.DaemonOpenedSessionInfo{}
	cl, err := c.cfg.Sessions.Client()
	if err != nil {
		return out
	}
	res, err := cl.ListOpenedSessions(ctx, protocol.ListOpenedSessionsParams{})
	if err != nil {
		c.cfg.Log("listing the opened sessions: " + err.Error())
		return out
	}
	for _, s := range res.Sessions {
		out[s.SessionID] = s
	}
	return out
}
