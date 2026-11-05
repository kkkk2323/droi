package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

// The Host's Workstreams (ADR 0036), which every Client lists and starts
// through its Gateway.
const (
	// WorkstreamsPath answers the Workstreams, active and archived, to a GET.
	WorkstreamsPath = "/workstreams"
	// WorkstreamCreatePath starts a Workstream, to a POST of a
	// NewWorkstream; it answers 201 with a WorkstreamCreated.
	WorkstreamCreatePath = "/workstreams/create"
	// WorkstreamSettingsPath changes a Workstream's agent model settings, to
	// a POST of a WorkstreamSettings; it answers 204.
	WorkstreamSettingsPath = "/workstreams/settings"
	// WorkstreamArchivePath archives the Workstream WorkstreamIDQuery
	// names, to a POST; it answers a WorkstreamArchived.
	WorkstreamArchivePath = "/workstreams/archive"
	WorkstreamIDQuery     = "id"
)

// Workstream is a Workstream as the Host keeps it.
type Workstream struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Workspace string `json:"workspace"`
	// SharedFolder is the folder its Sessions share, on the Host's computer.
	SharedFolder   string `json:"sharedFolder"`
	OrchestratorID string `json:"orchestratorId"`
	// AgentModel and AgentReasoningEffort are its agents' settings; "" is
	// the orchestrator's.
	AgentModel           string            `json:"agentModel"`
	AgentReasoningEffort string            `json:"agentReasoningEffort"`
	CreatedAt            string            `json:"createdAt"`
	ArchivedAt           string            `json:"archivedAt,omitempty"`
	Agents               []WorkstreamAgent `json:"agents"`
}

// WorkstreamAgent is one of a Workstream's agents. Whether it works or waits
// on the user, a Client reads from the Daemon.
type WorkstreamAgent struct {
	SessionID string `json:"sessionId"`
	Title     string `json:"title"`
	// WorktreePath and Branch are its worktree, "" when the Workspace is no
	// git repository; BaseBranch is what its branch was cut from.
	WorktreePath string `json:"worktreePath,omitempty"`
	Branch       string `json:"branch,omitempty"`
	BaseBranch   string `json:"baseBranch,omitempty"`
	// Model and ReasoningEffort are the agent model settings it last took
	// up; a model the user picked in its own Session differs from them.
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	CreatedAt       string `json:"createdAt"`
	ArchivedAt      string `json:"archivedAt,omitempty"`
	// Awaiting is set while the orchestrator waits on its report.
	Awaiting   bool              `json:"awaiting"`
	LastReport *WorkstreamReport `json:"lastReport,omitempty"`
}

// WorkstreamReport is what an agent last reported.
type WorkstreamReport struct {
	// Kind is "done", "blocked" or "update".
	Kind string `json:"kind"`
	Text string `json:"text"`
	At   string `json:"at"`
}

// NewWorkstream is a Workstream to start: Goal is the orchestrator's first
// message; Model and ReasoningEffort are the orchestrator's, "" the
// Daemon's defaults.
type NewWorkstream struct {
	Title                string `json:"title"`
	Workspace            string `json:"workspace"`
	Goal                 string `json:"goal"`
	Model                string `json:"model"`
	ReasoningEffort      string `json:"reasoningEffort"`
	AgentModel           string `json:"agentModel"`
	AgentReasoningEffort string `json:"agentReasoningEffort"`
}

// WorkstreamCreated is a started Workstream.
type WorkstreamCreated struct {
	WorkstreamID   string `json:"workstreamId"`
	OrchestratorID string `json:"orchestratorId"`
}

// WorkstreamSettings are a Workstream's new agent model settings.
type WorkstreamSettings struct {
	ID                   string `json:"id"`
	AgentModel           string `json:"agentModel"`
	AgentReasoningEffort string `json:"agentReasoningEffort"`
}

// WorkstreamArchived is an archived Workstream: the worktrees kept because
// they hold changes.
type WorkstreamArchived struct {
	KeptWorktrees []string `json:"keptWorktrees"`
}

// Workstreams is the Host's side of the Workstream endpoints. A
// *RefusedRequest answers 400 with its reason; any other error 500.
type Workstreams interface {
	List(ctx context.Context) ([]Workstream, error)
	Create(ctx context.Context, w NewWorkstream) (WorkstreamCreated, error)
	SetAgentSettings(ctx context.Context, s WorkstreamSettings) error
	Archive(ctx context.Context, id string) (keptWorktrees []string, err error)
}

// workstreamTimeout bounds what a Workstream request does on the Host:
// starting Sessions, or archiving every agent.
const workstreamTimeout = 2 * time.Minute

// maxWorkstreamBody bounds a request's JSON.
const maxWorkstreamBody = 1 << 20

func (g *Gateway) answerWorkstreams(w http.ResponseWriter, r *http.Request, endpoint string, q url.Values) (int, any) {
	if g.classify(q.Get(TokenQuery)) == "" {
		return http.StatusUnauthorized, nil
	}
	ws := g.opts.Workstreams
	if ws == nil {
		return http.StatusNotFound, nil
	}
	want := http.MethodPost
	if endpoint == WorkstreamsPath {
		want = http.MethodGet
	}
	if r.Method != want {
		return http.StatusMethodNotAllowed, nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), workstreamTimeout)
	defer cancel()
	var err error
	switch endpoint {
	case WorkstreamsPath:
		var list []Workstream
		if list, err = ws.List(ctx); err == nil {
			if list == nil {
				list = []Workstream{}
			}
			for i := range list {
				if list[i].Agents == nil {
					list[i].Agents = []WorkstreamAgent{}
				}
			}
			return http.StatusOK, list
		}
	case WorkstreamCreatePath:
		var in NewWorkstream
		if status, err := readJSON(w, r, &in, maxWorkstreamBody); err != nil {
			return status, errorBody{Error: err.Error()}
		}
		var made WorkstreamCreated
		if made, err = ws.Create(ctx, in); err == nil {
			return http.StatusCreated, made
		}
	case WorkstreamSettingsPath:
		var in WorkstreamSettings
		if status, err := readJSON(w, r, &in, maxWorkstreamBody); err != nil {
			return status, errorBody{Error: err.Error()}
		}
		if in.ID == "" {
			return http.StatusBadRequest, errorBody{Error: "No workstream given"}
		}
		if err = ws.SetAgentSettings(ctx, in); err == nil {
			return http.StatusNoContent, nil
		}
	case WorkstreamArchivePath:
		id := q.Get(WorkstreamIDQuery)
		if id == "" {
			return http.StatusBadRequest, errorBody{Error: "No workstream given"}
		}
		var kept []string
		if kept, err = ws.Archive(ctx, id); err == nil {
			if kept == nil {
				kept = []string{}
			}
			return http.StatusOK, WorkstreamArchived{KeptWorktrees: kept}
		}
	}
	var refused *RefusedRequest
	if errors.As(err, &refused) {
		return http.StatusBadRequest, errorBody{Error: err.Error()}
	}
	return http.StatusInternalServerError, errorBody{Error: err.Error()}
}

// Workstreams are the Gateway's computer's Workstreams. ErrNotFound is an
// older Host, without them.
func (c *Client) Workstreams(ctx context.Context) ([]Workstream, error) {
	var list []Workstream
	err := c.call(ctx, http.MethodGet, WorkstreamsPath, nil, &list)
	return list, err
}

// CreateWorkstream starts a Workstream on the Gateway's computer: its
// orchestrator gets the goal as its first message.
func (c *Client) CreateWorkstream(ctx context.Context, w NewWorkstream) (WorkstreamCreated, error) {
	var made WorkstreamCreated
	err := c.long(ctx, http.MethodPost, WorkstreamCreatePath, nil, w, &made)
	return made, err
}

// SetWorkstreamAgentSettings changes a Workstream's agent model and
// reasoning effort; agents switch before their next turn.
func (c *Client) SetWorkstreamAgentSettings(ctx context.Context, s WorkstreamSettings) error {
	return c.long(ctx, http.MethodPost, WorkstreamSettingsPath, nil, s, nil)
}

// ArchiveWorkstream archives a Workstream's agents and orchestrator; it
// returns the worktrees kept because they hold changes.
func (c *Client) ArchiveWorkstream(ctx context.Context, id string) ([]string, error) {
	var done WorkstreamArchived
	err := c.long(ctx, http.MethodPost, WorkstreamArchivePath, url.Values{WorkstreamIDQuery: {id}}, nil, &done)
	return done.KeptWorktrees, err
}

// long is call for a request the Host may take longer than requestTimeout
// to carry out, with a JSON body in when it is not nil.
func (c *Client) long(ctx context.Context, method, path string, query url.Values, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, workstreamTimeout+requestTimeout)
	defer cancel()
	return c.sendBody(ctx, c.poll, 1<<20, method, path, query, in, out)
}
