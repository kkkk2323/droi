package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// The Host's own Automations (ADR 0035), which every Client lists, edits and
// runs through its Gateway, and the Factory automations the Daemon still
// runs on its computer, which a Client moves to Droi.
const (
	// AutomationsPath answers the Automations, as Automation, to a GET.
	AutomationsPath = "/automations"
	// AutomationRunsPath answers the runs of the Automation
	// AutomationIDQuery names, newest first, to a GET.
	AutomationRunsPath = "/automations/runs"
	// AutomationSavePath creates (an empty ID) or changes an Automation, to
	// a POST of an Automation; it answers the Automation saved.
	AutomationSavePath = "/automations/save"
	// AutomationPausePath pauses or resumes an Automation, to a POST with
	// AutomationIDQuery and AutomationPausedQuery ("true" or "false"); it
	// answers 204.
	AutomationPausePath = "/automations/pause"
	// AutomationRunPath starts a run of an Automation now, to a POST; it
	// answers the AutomationRun.
	AutomationRunPath = "/automations/run"
	// AutomationDeletePath deletes an Automation, to a POST; it answers 204.
	AutomationDeletePath = "/automations/delete"
	// FactoryAutomationsPath answers the Factory automations on the Host's
	// computer, as FactoryAutomation, to a GET.
	FactoryAutomationsPath = "/automations/factory"
	// FactoryAutomationMovePath copies the Factory automation
	// AutomationIDQuery names into Droi's and deletes it from Factory's, to
	// a POST; it answers an AutomationMoved.
	FactoryAutomationMovePath = "/automations/factory/move"

	AutomationIDQuery     = "id"
	AutomationPausedQuery = "paused"
)

// ErrAutomationNotFound is an Automation the Host does not have (any
// more). The Host answers it 410.
var ErrAutomationNotFound = errors.New("no such automation")

// ErrAutomationRunning is a run asked for while one of the Automation
// works. The Host answers it 409.
var ErrAutomationRunning = errors.New("a run of the automation is already working")

// ErrAutomationsElsewhere is a Host whose Automations another Droi on its
// computer runs, from the same data folder. The Host answers it 503.
var ErrAutomationsElsewhere = errors.New("another Droi on this computer runs its automations")

// Automation is one of the Host's Automations. Times are RFC 3339.
type Automation struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Prompt string `json:"prompt"`
	// Schedule is five cron fields, read in Timezone; "" is the Host's
	// computer's zone.
	Schedule        string `json:"schedule"`
	Timezone        string `json:"timezone,omitempty"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	// Autonomy is "" to run every tool without asking, or low, medium or
	// high.
	Autonomy string `json:"autonomy,omitempty"`
	// WorkingDirectory is the Workspace its runs work in; "" is Folder.
	WorkingDirectory string `json:"workingDirectory,omitempty"`
	Paused           bool   `json:"paused"`
	// The rest is the Host's to answer; a save ignores it.
	CreatedAt string `json:"createdAt,omitempty"`
	// ForkedFrom is the Factory automation it was moved from.
	ForkedFrom string `json:"forkedFrom,omitempty"`
	// Folder is the Automation's own folder on the Host's computer.
	Folder string `json:"folder,omitempty"`
	// NextRunAt is its next scheduled time, "" when paused or Invalid.
	NextRunAt string         `json:"nextRunAt,omitempty"`
	LastRun   *AutomationRun `json:"lastRun,omitempty"`
	Running   bool           `json:"running"`
	// Invalid is why its HEARTBEAT.md, schedule or time zone cannot be read.
	Invalid string `json:"invalid,omitempty"`
}

// AutomationRun is one run of an Automation.
type AutomationRun struct {
	RunID     string `json:"runId"`
	SessionID string `json:"sessionId,omitempty"`
	// Trigger is "schedule", "manual" or "catch-up".
	Trigger string `json:"trigger"`
	// Status is "running", "succeeded", "failed" or "interrupted".
	Status    string `json:"status"`
	StartedAt string `json:"startedAt,omitempty"`
	EndedAt   string `json:"endedAt,omitempty"`
	Error     string `json:"error,omitempty"`
}

// FactoryAutomation is an automation in Factory's folder that the Daemon
// runs (and syncs to Factory).
type FactoryAutomation struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Prompt string `json:"prompt,omitempty"`
	// Schedule is as the Daemon has it: in UTC, or daily, weekly or
	// monthly.
	Schedule         string `json:"schedule"`
	Model            string `json:"model,omitempty"`
	ReasoningEffort  string `json:"reasoningEffort,omitempty"`
	WorkingDirectory string `json:"workingDirectory,omitempty"`
	Paused           bool   `json:"paused"`
	// Path is its folder on the Host's computer.
	Path string `json:"path,omitempty"`
	// TargetsSession is set for one that posts into an existing Session,
	// which Droi's cannot: it does not move.
	TargetsSession bool `json:"targetsSession,omitempty"`
}

// AutomationMoved is a Factory automation copied into the Host's: the copy,
// and DeleteError when the Daemon did not delete the original, which then
// still runs as well.
type AutomationMoved struct {
	Automation  Automation `json:"automation"`
	DeleteError string     `json:"deleteError,omitempty"`
}

// Automations is the Host's side of the Automation endpoints. A
// *RefusedRequest answers 400 with its reason, ErrAutomationRunning 409,
// ErrAutomationNotFound 410, ErrAutomationsElsewhere 503; any other error
// 500.
type Automations interface {
	List(ctx context.Context) ([]Automation, error)
	Runs(ctx context.Context, id string, limit int) ([]AutomationRun, error)
	Save(ctx context.Context, a Automation) (Automation, error)
	SetPaused(ctx context.Context, id string, paused bool) error
	Run(ctx context.Context, id string) (AutomationRun, error)
	Delete(ctx context.Context, id string) error
	Factory(ctx context.Context) ([]FactoryAutomation, error)
	MoveFactory(ctx context.Context, id string) (AutomationMoved, error)
}

// automationTimeout bounds what an Automation request does on the Host
// (starting a run waits for its Session to open), under the Relay's two
// minutes for an answer.
const automationTimeout = 100 * time.Second

// runsLimit is how many runs AutomationRunsPath answers.
const runsLimit = 50

func (g *Gateway) answerAutomations(w http.ResponseWriter, r *http.Request, endpoint string, q url.Values) (int, any) {
	if g.classify(q.Get(TokenQuery)) == "" {
		return http.StatusUnauthorized, nil
	}
	au := g.opts.Automations
	if au == nil {
		return http.StatusNotFound, nil
	}
	want := http.MethodPost
	if endpoint == AutomationsPath || endpoint == AutomationRunsPath || endpoint == FactoryAutomationsPath {
		want = http.MethodGet
	}
	if r.Method != want {
		return http.StatusMethodNotAllowed, nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), automationTimeout)
	defer cancel()
	id := q.Get(AutomationIDQuery)
	if id == "" && endpoint != AutomationsPath && endpoint != FactoryAutomationsPath && endpoint != AutomationSavePath {
		return http.StatusBadRequest, errorBody{Error: "No automation given"}
	}
	var err error
	switch endpoint {
	case AutomationsPath:
		var list []Automation
		if list, err = au.List(ctx); err == nil {
			return http.StatusOK, emptyIfNil(list)
		}
	case AutomationRunsPath:
		var runs []AutomationRun
		if runs, err = au.Runs(ctx, id, runsLimit); err == nil {
			return http.StatusOK, emptyIfNil(runs)
		}
	case AutomationSavePath:
		var in Automation
		if status, err := readJSON(w, r, &in, 1<<20); err != nil {
			return status, errorBody{Error: err.Error()}
		}
		var saved Automation
		if saved, err = au.Save(ctx, in); err == nil {
			return http.StatusOK, saved
		}
	case AutomationPausePath:
		paused, perr := strconv.ParseBool(q.Get(AutomationPausedQuery))
		if perr != nil {
			return http.StatusBadRequest, errorBody{Error: "paused is not true or false"}
		}
		if err = au.SetPaused(ctx, id, paused); err == nil {
			return http.StatusNoContent, nil
		}
	case AutomationRunPath:
		var run AutomationRun
		if run, err = au.Run(ctx, id); err == nil {
			return http.StatusOK, run
		}
	case AutomationDeletePath:
		if err = au.Delete(ctx, id); err == nil {
			return http.StatusNoContent, nil
		}
	case FactoryAutomationsPath:
		var list []FactoryAutomation
		if list, err = au.Factory(ctx); err == nil {
			return http.StatusOK, emptyIfNil(list)
		}
	case FactoryAutomationMovePath:
		var moved AutomationMoved
		if moved, err = au.MoveFactory(ctx, id); err == nil {
			return http.StatusOK, moved
		}
	}
	var refused *RefusedRequest
	switch {
	case errors.As(err, &refused):
		return http.StatusBadRequest, errorBody{Error: err.Error()}
	case errors.Is(err, ErrAutomationRunning):
		return http.StatusConflict, errorBody{Error: err.Error()}
	case errors.Is(err, ErrAutomationNotFound):
		return http.StatusGone, errorBody{Error: err.Error()}
	case errors.Is(err, ErrAutomationsElsewhere):
		return http.StatusServiceUnavailable, errorBody{Error: err.Error()}
	}
	return http.StatusInternalServerError, errorBody{Error: err.Error()}
}

func emptyIfNil[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}

// Automations are the Gateway's computer's Automations. ErrNotFound is an
// older Host, without them.
func (c *Client) Automations(ctx context.Context) ([]Automation, error) {
	var list []Automation
	err := c.automationCall(ctx, http.MethodGet, AutomationsPath, nil, nil, &list)
	return list, err
}

// AutomationRuns are an Automation's runs, newest first.
func (c *Client) AutomationRuns(ctx context.Context, id string) ([]AutomationRun, error) {
	var runs []AutomationRun
	err := c.automationCall(ctx, http.MethodGet, AutomationRunsPath, url.Values{AutomationIDQuery: {id}}, nil, &runs)
	return runs, err
}

// SaveAutomation creates an Automation (a.ID "") or changes one, and returns
// it as the Host keeps it.
func (c *Client) SaveAutomation(ctx context.Context, a Automation) (Automation, error) {
	var saved Automation
	err := c.automationCall(ctx, http.MethodPost, AutomationSavePath, nil, a, &saved)
	return saved, err
}

// PauseAutomation pauses or resumes an Automation.
func (c *Client) PauseAutomation(ctx context.Context, id string, paused bool) error {
	q := url.Values{AutomationIDQuery: {id}, AutomationPausedQuery: {strconv.FormatBool(paused)}}
	return c.automationCall(ctx, http.MethodPost, AutomationPausePath, q, nil, nil)
}

// RunAutomation starts a run of an Automation now; ErrAutomationRunning
// while one of it works.
func (c *Client) RunAutomation(ctx context.Context, id string) (AutomationRun, error) {
	var run AutomationRun
	err := c.automationCall(ctx, http.MethodPost, AutomationRunPath, url.Values{AutomationIDQuery: {id}}, nil, &run)
	return run, err
}

// DeleteAutomation deletes an Automation; the Sessions of its runs stay.
func (c *Client) DeleteAutomation(ctx context.Context, id string) error {
	return c.automationCall(ctx, http.MethodPost, AutomationDeletePath, url.Values{AutomationIDQuery: {id}}, nil, nil)
}

// FactoryAutomations are the Factory automations on the Gateway's computer.
func (c *Client) FactoryAutomations(ctx context.Context) ([]FactoryAutomation, error) {
	var list []FactoryAutomation
	err := c.automationCall(ctx, http.MethodGet, FactoryAutomationsPath, nil, nil, &list)
	return list, err
}

// MoveFactoryAutomation copies a Factory automation into the Host's and
// deletes it from Factory's.
func (c *Client) MoveFactoryAutomation(ctx context.Context, id string) (AutomationMoved, error) {
	var moved AutomationMoved
	err := c.automationCall(ctx, http.MethodPost, FactoryAutomationMovePath, url.Values{AutomationIDQuery: {id}}, nil, &moved)
	return moved, err
}

// automationCall is an Automation request, which the Host may take up to
// automationTimeout to carry out: a 409 is ErrAutomationRunning, a 410
// ErrAutomationNotFound, a 503 ErrAutomationsElsewhere.
func (c *Client) automationCall(ctx context.Context, method, path string, query url.Values, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, automationTimeout+requestTimeout)
	defer cancel()
	err := c.sendBody(ctx, c.poll, 4<<20, method, path, query, in, out)
	var status *StatusError
	if errors.As(err, &status) {
		switch status.Status {
		case http.StatusConflict:
			return ErrAutomationRunning
		case http.StatusGone:
			return ErrAutomationNotFound
		case http.StatusServiceUnavailable:
			return ErrAutomationsElsewhere
		}
	}
	return err
}
