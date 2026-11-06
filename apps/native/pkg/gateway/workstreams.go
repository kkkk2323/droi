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
	// WorkstreamEditPath renames a Workstream or changes its symbol, to a POST
	// of a WorkstreamEdit; it answers 204. A Host whose Workstreams do not
	// implement WorkstreamEditor answers 404.
	WorkstreamEditPath = "/workstreams/edit"
	WorkstreamIDQuery  = "id"
)

// MaxWorkstreamTitle is the longest name a Workstream may have, in
// characters.
const MaxWorkstreamTitle = 120

// maxWorkstreamSymbol is the longest symbol name a Workstream may have.
const maxWorkstreamSymbol = 32

// ValidWorkstreamSymbol reports that s can be a Workstream's symbol: the name
// of an icon, lowercase letters, digits and dashes; "" is the Client's own.
func ValidWorkstreamSymbol(s string) bool {
	if len(s) > maxWorkstreamSymbol {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// Workstream is a Workstream as the Host keeps it.
type Workstream struct {
	ID string `json:"id"`
	// Title is its name: its orchestrator's Session title as it stands now.
	Title     string `json:"title"`
	Workspace string `json:"workspace"`
	// Symbol is the name of its icon, also on its orchestrator's tag; ""
	// leaves the icon to the Client.
	Symbol string `json:"symbol,omitempty"`
	// SharedFolder is the folder its Sessions share, on the Host's computer.
	SharedFolder   string `json:"sharedFolder"`
	OrchestratorID string `json:"orchestratorId"`
	// OrchestratorToolMode and OrchestratorScriptCapable are ToolMode and
	// ScriptCapable of its orchestrator.
	OrchestratorToolMode      string `json:"orchestratorToolMode,omitempty"`
	OrchestratorScriptCapable *bool  `json:"orchestratorScriptCapable,omitempty"`
	// Starting are the agents being started: their Sessions are not there
	// yet. They are not kept across a restart of Droi.
	Starting []WorkstreamStarting `json:"starting,omitempty"`
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
	// ToolMode is the tool execution mode the Daemon resolved for the agent,
	// "direct_only" or "script_only"; "" when the Host does not know. droid
	// decides again only at the first turn after the agent's model changes,
	// so it may be the mode of ToolModeModel rather than of Model.
	ToolMode      string `json:"toolMode,omitempty"`
	ToolModeModel string `json:"toolModeModel,omitempty"`
	// ScriptCapable is whether the model the agent has now may use Script;
	// nil when that is not known.
	ScriptCapable *bool `json:"scriptCapable,omitempty"`
	// LastActivityAt is when the Host last saw the agent's Session do
	// something, "" when it has not.
	LastActivityAt string `json:"lastActivityAt,omitempty"`
}

// WorkstreamStarting is an agent the orchestrator asked for, whose Session
// the Host is still making.
type WorkstreamStarting struct {
	Title     string `json:"title"`
	StartedAt string `json:"startedAt"`
}

// WorkstreamReport is what an agent last reported.
type WorkstreamReport struct {
	// Kind is "done", "blocked" or "update".
	Kind string `json:"kind"`
	Text string `json:"text"`
	At   string `json:"at"`
}

// NewWorkstream is a Workstream to start: Title is at most
// MaxWorkstreamTitle characters; Goal is the orchestrator's first message,
// "" leaving it to the user; Symbol the name of its icon; Model and
// ReasoningEffort are the orchestrator's, "" the Daemon's defaults.
type NewWorkstream struct {
	Title                string `json:"title"`
	Workspace            string `json:"workspace"`
	Goal                 string `json:"goal"`
	Symbol               string `json:"symbol,omitempty"`
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

// WorkstreamEdit is a change of a Workstream's name or symbol; a nil field
// stays as it is, and a Symbol of "" clears it.
type WorkstreamEdit struct {
	ID     string  `json:"id"`
	Title  *string `json:"title,omitempty"`
	Symbol *string `json:"symbol,omitempty"`
}

// WorkstreamEditor is the part of a Host's Workstreams that edits one; it
// is separate from Workstreams so that an older implementation still fits.
type WorkstreamEditor interface {
	Edit(ctx context.Context, e WorkstreamEdit) error
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
// starting Sessions, or archiving every agent. With a request's own time on
// top, a Client's wait stays inside the Relay's 120 s for a Host to answer.
// A var for the tests.
var workstreamTimeout = 100 * time.Second

// ErrWorkstreamArchiveUnfinished is an archive the Host's bound cut short,
// which WorkstreamArchivePath answers with 504: archiving the Workstream
// again goes on where it stopped.
var ErrWorkstreamArchiveUnfinished = errors.New("Archiving took too long, and some agents are not archived yet. Archive the workstream again to finish.")

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
	case WorkstreamEditPath:
		editor, ok := ws.(WorkstreamEditor)
		if !ok {
			return http.StatusNotFound, nil
		}
		var in WorkstreamEdit
		if status, err := readJSON(w, r, &in, maxWorkstreamBody); err != nil {
			return status, errorBody{Error: err.Error()}
		}
		if in.ID == "" {
			return http.StatusBadRequest, errorBody{Error: "No workstream given"}
		}
		if err = editor.Edit(ctx, in); err == nil {
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
		if errors.Is(ctx.Err(), context.DeadlineExceeded) && r.Context().Err() == nil {
			return http.StatusGatewayTimeout, errorBody{Error: ErrWorkstreamArchiveUnfinished.Error()}
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

// EditWorkstream renames a Workstream or changes its symbol. ErrNotFound is
// an older Host.
func (c *Client) EditWorkstream(ctx context.Context, e WorkstreamEdit) error {
	return c.long(ctx, http.MethodPost, WorkstreamEditPath, nil, e, nil)
}

// ArchiveWorkstream archives a Workstream's agents and orchestrator; it
// returns the worktrees kept because they hold changes.
// ErrWorkstreamArchiveUnfinished is an archive that took too long.
func (c *Client) ArchiveWorkstream(ctx context.Context, id string) ([]string, error) {
	var done WorkstreamArchived
	err := c.long(ctx, http.MethodPost, WorkstreamArchivePath, url.Values{WorkstreamIDQuery: {id}}, nil, &done)
	if status := (*StatusError)(nil); errors.As(err, &status) && status.Status == http.StatusGatewayTimeout {
		return nil, ErrWorkstreamArchiveUnfinished
	}
	return done.KeptWorktrees, err
}

// long is call for a request the Host may take longer than requestTimeout
// to carry out, with a JSON body in when it is not nil.
func (c *Client) long(ctx context.Context, method, path string, query url.Values, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, workstreamTimeout+requestTimeout)
	defer cancel()
	return c.sendBody(ctx, c.poll, 1<<20, method, path, query, in, out)
}
