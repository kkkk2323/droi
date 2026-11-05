// Package workstream runs Droi's own Workstreams from the Host (ADR 0036):
// an orchestrator Session that splits a long goal among agent Sessions it
// spawns through the droi-workstream tool server, each in a persistent
// worktree of its own, reporting back to it. The Host starts and follows them
// with its own Runner (hostsession) and keeps their records in Droi's data.
package workstream

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/internal/hostsession"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// Config is what a Coordinator needs from the Host.
type Config struct {
	// Dir holds the Workstreams' folders: <UserData>/workstreams.
	Dir string
	// Sessions starts and follows the Workstreams' Sessions.
	Sessions *hostsession.Runner
	Log      func(string)
	Now      func() time.Time
}

// Coordinator keeps the Host's Workstreams: it starts them, carries out
// their tools, and forwards what the orchestrator must hear. Safe for use
// from any goroutine.
type Coordinator struct {
	cfg Config

	mu  sync.Mutex
	all map[string]*Workstream
	// activity is when the Host last saw each Session do something.
	activity map[string]time.Time
	// asked is the agent settings the Host last asked of each agent, so an
	// effort the Daemon fitted to its model is not asked for again and again.
	asked map[string][2]string
}

// factoryAutomationTools are Factory's Automation tools, which Droi's
// Workstream Sessions do without (ADR 0035).
var factoryAutomationTools = []string{
	"CreateAutomation", "ListAutomations", "ReadAutomation", "EditAutomation",
	"DeleteAutomation", "RunAutomation", "ListAutomationTemplates",
}

// New makes a Coordinator of the Workstreams in cfg.Dir; Start resumes them.
func New(cfg Config) *Coordinator {
	if cfg.Log == nil {
		cfg.Log = func(string) {}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	c := &Coordinator{cfg: cfg, all: loadAll(cfg.Dir, cfg.Log), activity: map[string]time.Time{}, asked: map[string][2]string{}}
	cfg.Sessions.Subscribe(func(e hostsession.Event) { go c.see(e) })
	return c
}

// Start waits, until ctx ends, for the Runner to reach the Daemon, then
// tells each orchestrator of an agent it waited on that Droi's restart left
// idle.
func (c *Coordinator) Start(ctx context.Context) {
	go func() {
		for {
			if _, err := c.cfg.Sessions.Client(); err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
		c.resume(ctx)
	}()
}

func (c *Coordinator) now() string { return c.cfg.Now().UTC().Format(time.RFC3339) }

// save writes w's record; under c.mu.
func (c *Coordinator) save(w *Workstream) {
	if err := save(c.cfg.Dir, w); err != nil {
		c.cfg.Log("saving workstream " + w.ID + ": " + err.Error())
	}
}

// update changes w's record under c.mu and saves it.
func (c *Coordinator) update(w *Workstream, fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn()
	c.save(w)
}

// SharedFolder is a Workstream's Shared Folder.
func (c *Coordinator) SharedFolder(id string) string {
	return filepath.Join(c.cfg.Dir, id, SharedDir)
}

// orchestratorOf is the active Workstream whose orchestrator sessionID is.
func (c *Coordinator) orchestratorOf(sessionID string) *Workstream {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, w := range c.all {
		if w.ArchivedAt == "" && sessionID != "" && w.OrchestratorID == sessionID {
			return w
		}
	}
	return nil
}

// agentOf is the active agent sessionID is, and its Workstream.
func (c *Coordinator) agentOf(sessionID string) (*Workstream, *Agent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, w := range c.all {
		if w.ArchivedAt != "" {
			continue
		}
		for _, a := range w.Agents {
			if a.ArchivedAt == "" && sessionID != "" && a.SessionID == sessionID {
				return w, a
			}
		}
	}
	return nil, nil
}

func refuse(format string, args ...any) error {
	return &gateway.RefusedRequest{Reason: fmt.Sprintf(format, args...)}
}

// Create starts a Workstream: its folders, then its orchestrator in the
// Workspace, which gets the goal as its first message.
func (c *Coordinator) Create(ctx context.Context, in gateway.NewWorkstream) (gateway.WorkstreamCreated, error) {
	title, workspace, goal := strings.TrimSpace(in.Title), strings.TrimSpace(in.Workspace), strings.TrimSpace(in.Goal)
	switch {
	case title == "":
		return gateway.WorkstreamCreated{}, refuse("Enter a name.")
	case workspace == "":
		return gateway.WorkstreamCreated{}, refuse("Choose a workspace.")
	case goal == "":
		return gateway.WorkstreamCreated{}, refuse("Enter the goal.")
	}
	if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
		return gateway.WorkstreamCreated{}, refuse("%s is not a folder on this computer.", workspace)
	}
	w := &Workstream{
		ID: newID(title), Title: title, Workspace: workspace,
		AgentModel: strings.TrimSpace(in.AgentModel), AgentReasoningEffort: strings.TrimSpace(in.AgentReasoningEffort),
		CreatedAt: c.now(), Agents: []*Agent{},
	}
	shared := c.SharedFolder(w.ID)
	if err := os.MkdirAll(shared, 0o777); err != nil {
		return gateway.WorkstreamCreated{}, err
	}
	opened, err := c.cfg.Sessions.Open(ctx, hostsession.Spec{
		Title:           title,
		Cwd:             workspace,
		ModelID:         strings.TrimSpace(in.Model),
		ReasoningEffort: protocol.ReasoningEffort(strings.TrimSpace(in.ReasoningEffort)),
		Tags:            []protocol.SessionTag{orchestratorTag(w)},
		SystemPrompt:    OrchestratorPrompt(title, workspace, shared, isGitRepo(ctx, workspace)),
		DisabledTools:   orchestratorHidden(),
	})
	if err != nil {
		if opened.SessionID == "" {
			_ = os.RemoveAll(filepath.Join(c.cfg.Dir, w.ID))
		}
		return gateway.WorkstreamCreated{}, err
	}
	c.mu.Lock()
	w.OrchestratorID = opened.SessionID
	c.all[w.ID] = w
	c.save(w)
	c.mu.Unlock()
	made := gateway.WorkstreamCreated{WorkstreamID: w.ID, OrchestratorID: opened.SessionID}
	// Recorded first, so that the orchestrator's first tool call finds it.
	if err := c.cfg.Sessions.Send(ctx, opened.SessionID, goal); err != nil {
		return made, fmt.Errorf("the orchestrator started but did not get the goal: %w", err)
	}
	return made, nil
}

func orchestratorHidden() []string {
	return append(slices.Clone(factoryAutomationTools), hostToolsSelector(ToolReport)...)
}

func agentHidden() []string {
	return append(slices.Clone(factoryAutomationTools), hostToolsSelector(orchestratorTools...)...)
}

// orchestratorTag is w's orchestrator's droi.workstream tag; under c.mu or
// before w is shared.
func orchestratorTag(w *Workstream) protocol.SessionTag {
	md := map[string]string{"workstreamId": w.ID, "role": sessions.WorkstreamOrchestrator}
	if w.AgentModel != "" {
		md["agentModel"] = w.AgentModel
	}
	if w.AgentReasoningEffort != "" {
		md["agentReasoningEffort"] = w.AgentReasoningEffort
	}
	return protocol.SessionTag{Name: sessions.WorkstreamTag, Metadata: md}
}

func agentTag(workstreamID, orchestratorID, model, effort string) protocol.SessionTag {
	return protocol.SessionTag{Name: sessions.WorkstreamTag, Metadata: map[string]string{
		"workstreamId": workstreamID, "role": sessions.WorkstreamAgent, "orchestratorId": orchestratorID,
		"agentModel": model, "agentReasoningEffort": effort,
	}}
}

// withTag is tags with tag in place of any of its name.
func withTag(tags []protocol.SessionTag, tag protocol.SessionTag) []protocol.SessionTag {
	out := make([]protocol.SessionTag, 0, len(tags)+1)
	for _, t := range tags {
		if t.Name != tag.Name {
			out = append(out, t)
		}
	}
	return append(out, tag)
}

// List is every Workstream, the newest first.
func (c *Coordinator) List(context.Context) ([]gateway.Workstream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]gateway.Workstream, 0, len(c.all))
	for _, w := range c.all {
		g := gateway.Workstream{
			ID: w.ID, Title: w.Title, Workspace: w.Workspace, SharedFolder: c.SharedFolder(w.ID),
			OrchestratorID: w.OrchestratorID, AgentModel: w.AgentModel, AgentReasoningEffort: w.AgentReasoningEffort,
			CreatedAt: w.CreatedAt, ArchivedAt: w.ArchivedAt, Agents: make([]gateway.WorkstreamAgent, 0, len(w.Agents)),
		}
		for _, a := range w.Agents {
			ga := gateway.WorkstreamAgent{
				SessionID: a.SessionID, Title: a.Title, WorktreePath: a.WorktreePath, Branch: a.Branch, BaseBranch: a.BaseBranch,
				Model: a.Model, ReasoningEffort: a.ReasoningEffort, CreatedAt: a.CreatedAt, ArchivedAt: a.ArchivedAt, Awaiting: a.Awaiting,
			}
			if r := a.LastReport; r != nil {
				ga.LastReport = &gateway.WorkstreamReport{Kind: r.Kind, Text: r.Text, At: r.At}
			}
			g.Agents = append(g.Agents, ga)
		}
		out = append(out, g)
	}
	slices.SortFunc(out, func(a, b gateway.Workstream) int {
		return cmp.Or(cmp.Compare(b.CreatedAt, a.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return out, nil
}

// find is the Workstream id names, refusing one that is not there or is
// archived.
func (c *Coordinator) find(id string) (*Workstream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.all[id]
	switch {
	case w == nil:
		return nil, refuse("There is no workstream %s.", id)
	case w.ArchivedAt != "":
		return nil, refuse("The workstream %s is archived.", w.Title)
	}
	return w, nil
}

// SetAgentSettings changes a Workstream's agent model settings: on the
// orchestrator's tag and the record, and on each idle agent now; a working
// one takes them when its turn ends.
func (c *Coordinator) SetAgentSettings(ctx context.Context, s gateway.WorkstreamSettings) error {
	w, err := c.find(s.ID)
	if err != nil {
		return err
	}
	c.update(w, func() {
		w.AgentModel, w.AgentReasoningEffort = strings.TrimSpace(s.AgentModel), strings.TrimSpace(s.AgentReasoningEffort)
	})
	c.mu.Lock()
	orch, tag := w.OrchestratorID, orchestratorTag(w)
	var agents []string
	for _, a := range w.Agents {
		if a.ArchivedAt == "" {
			agents = append(agents, a.SessionID)
		}
	}
	c.mu.Unlock()
	lead, err := c.follow(ctx, orch)
	if err != nil {
		return err
	}
	if err := c.cfg.Sessions.UpdateSettings(ctx, protocol.UpdateSessionSettingsParams{SessionID: orch, Tags: withTag(lead.Settings().Tags, tag)}); err != nil {
		return err
	}
	for _, id := range agents {
		if err := c.moveAgent(ctx, w, id); err != nil {
			c.cfg.Log("moving agent " + id + " to the workstream's settings: " + err.Error())
		}
	}
	return nil
}

// follow is a Session the Runner follows, loaded first when it does not.
func (c *Coordinator) follow(ctx context.Context, id string) (*session.Session, error) {
	if !c.cfg.Sessions.Following(id) {
		if err := c.cfg.Sessions.Load(ctx, id); err != nil {
			return nil, err
		}
	}
	s := c.cfg.Sessions.Session(id)
	if s == nil {
		return nil, fmt.Errorf("session %s is not loaded", id)
	}
	return s, nil
}

func working(s *session.Session) bool {
	if s == nil {
		return false
	}
	ws := s.WorkingState()
	return ws != "" && ws != protocol.DroidWorkingStateIdle
}

// agentSettings are the model and effort w's agents are to have: the
// user's, else the orchestrator's current ones.
func (c *Coordinator) agentSettings(ctx context.Context, w *Workstream) (model, effort string, err error) {
	c.mu.Lock()
	model, effort, orch := w.AgentModel, w.AgentReasoningEffort, w.OrchestratorID
	c.mu.Unlock()
	if model != "" && effort != "" {
		return model, effort, nil
	}
	lead, err := c.follow(ctx, orch)
	if err != nil {
		return "", "", err
	}
	set := lead.Settings()
	return cmp.Or(model, set.ModelID), cmp.Or(effort, string(set.ReasoningEffort)), nil
}

// settleWait bounds how long the Host waits for the Daemon to report an
// agent's new settings.
const settleWait = 3 * time.Second

// moveAgent moves an idle agent to its Workstream's agent settings when
// they differ from the ones it took up, unless the user switched its model
// or effort in its own Session; the settings it then takes go on its tag.
func (c *Coordinator) moveAgent(ctx context.Context, w *Workstream, id string) error {
	model, effort, err := c.agentSettings(ctx, w)
	if err != nil {
		return err
	}
	s, err := c.follow(ctx, id)
	if err != nil {
		return err
	}
	if working(s) {
		return nil
	}
	c.mu.Lock()
	a := agentIn(w, id)
	if a == nil {
		c.mu.Unlock()
		return nil
	}
	takenModel, takenEffort, asked := a.Model, a.ReasoningEffort, c.asked[id]
	orch := w.OrchestratorID
	c.mu.Unlock()
	if (model == takenModel && effort == takenEffort) || asked == [2]string{model, effort} {
		return nil
	}
	set := s.Settings()
	if set.ModelID != takenModel || string(set.ReasoningEffort) != takenEffort {
		// Switched by hand: the user's pick stays.
		return nil
	}
	if err := c.cfg.Sessions.UpdateSettings(ctx, protocol.UpdateSessionSettingsParams{SessionID: id, ModelID: model, ReasoningEffort: protocol.ReasoningEffort(effort)}); err != nil {
		return err
	}
	deadline := time.Now().Add(settleWait)
	for s.Settings().ModelID != model && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	set = s.Settings()
	tookModel, tookEffort := cmp.Or(set.ModelID, model), cmp.Or(string(set.ReasoningEffort), effort)
	c.mu.Lock()
	c.asked[id] = [2]string{model, effort}
	a.Model, a.ReasoningEffort = tookModel, tookEffort
	c.save(w)
	c.mu.Unlock()
	return c.cfg.Sessions.UpdateSettings(ctx, protocol.UpdateSessionSettingsParams{SessionID: id, Tags: withTag(set.Tags, agentTag(w.ID, orch, tookModel, tookEffort))})
}

func agentIn(w *Workstream, id string) *Agent {
	for _, a := range w.Agents {
		if a.SessionID == id {
			return a
		}
	}
	return nil
}

// spawn starts an agent of w: in a persistent worktree of the Workspace when
// it is a git repository, on the agent settings, at the orchestrator's
// autonomy, with the task as its first message.
func (c *Coordinator) spawn(ctx context.Context, w *Workstream, title, task string) (*Agent, error) {
	model, effort, err := c.agentSettings(ctx, w)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	orch, wsTitle, workspace := w.OrchestratorID, w.Title, w.Workspace
	c.mu.Unlock()
	var autonomy protocol.AutonomyLevel
	if lead := c.cfg.Sessions.Session(orch); lead != nil {
		autonomy = lead.Settings().AutonomyLevel
	}
	repo := isGitRepo(ctx, workspace)
	base := ""
	if repo {
		base = currentBranch(ctx, workspace)
	}
	opened, err := c.cfg.Sessions.Open(ctx, hostsession.Spec{
		Title:           title,
		Cwd:             workspace,
		ModelID:         model,
		ReasoningEffort: protocol.ReasoningEffort(effort),
		AutonomyLevel:   autonomy,
		Tags:            []protocol.SessionTag{agentTag(w.ID, orch, model, effort)},
		SystemPrompt:    AgentPrompt(title, wsTitle, workspace, c.SharedFolder(w.ID), repo),
		Worktree:        repo,
		DisabledTools:   agentHidden(),
	})
	if err != nil {
		if opened.SessionID != "" {
			_ = c.cfg.Sessions.Close(context.WithoutCancel(ctx), opened.SessionID)
		}
		return nil, err
	}
	a := &Agent{
		SessionID: opened.SessionID, Title: title, WorktreePath: opened.WorktreePath, Branch: opened.WorktreeBranch,
		Model: cmp.Or(opened.ModelID, model), ReasoningEffort: cmp.Or(opened.ReasoningEffort, effort),
		CreatedAt: c.now(), Awaiting: true,
	}
	if a.WorktreePath != "" {
		a.BaseBranch = base
	}
	c.mu.Lock()
	w.Agents = append(w.Agents, a)
	c.asked[a.SessionID] = [2]string{model, effort}
	c.activity[a.SessionID] = c.cfg.Now()
	c.save(w)
	c.mu.Unlock()
	if a.Model != model || a.ReasoningEffort != effort {
		if err := c.cfg.Sessions.UpdateSettings(ctx, protocol.UpdateSessionSettingsParams{SessionID: a.SessionID, Tags: []protocol.SessionTag{agentTag(w.ID, orch, a.Model, a.ReasoningEffort)}}); err != nil {
			c.cfg.Log("tagging agent " + a.SessionID + ": " + err.Error())
		}
	}
	// Recorded first, so that a report in its first turn finds it.
	if err := c.cfg.Sessions.Send(ctx, a.SessionID, fromOrchestrator(task)); err != nil {
		c.update(w, func() { a.Awaiting = false })
		return a, err
	}
	return a, nil
}

// message sends an agent more work, moving it to the Workstream's agent
// settings first.
func (c *Coordinator) message(ctx context.Context, w *Workstream, a *Agent, text string) error {
	if err := c.moveAgent(ctx, w, a.SessionID); err != nil {
		c.cfg.Log("moving agent " + a.SessionID + " to the workstream's settings: " + err.Error())
	}
	c.update(w, func() { a.Awaiting, a.ReportedThisTurn = true, false })
	if err := c.cfg.Sessions.Send(ctx, a.SessionID, fromOrchestrator(text)); err != nil {
		c.update(w, func() { a.Awaiting = false })
		return err
	}
	return nil
}

// report forwards an agent's report to its orchestrator.
func (c *Coordinator) report(ctx context.Context, w *Workstream, a *Agent, kind, text string) error {
	c.mu.Lock()
	a.LastReport = &Report{Kind: kind, Text: text, At: c.now()}
	a.ReportedThisTurn = true
	c.activity[a.SessionID] = c.cfg.Now()
	c.save(w)
	orch, title, id := w.OrchestratorID, a.Title, a.SessionID
	c.mu.Unlock()
	return c.cfg.Sessions.Send(ctx, orch, reportMessage(title, id, kind, text))
}

// interrupt stops an agent's turn, which the orchestrator then no longer
// waits on.
func (c *Coordinator) interrupt(ctx context.Context, w *Workstream, a *Agent) error {
	c.update(w, func() { a.Awaiting = false })
	return c.cfg.Sessions.Interrupt(ctx, a.SessionID)
}

// archiveAgent archives an agent: it stops its turn, removes its worktree
// when it holds nothing its base branch does not, which archives the
// Session with it, else archives the Session alone; kept says why a
// worktree stays.
func (c *Coordinator) archiveAgent(ctx context.Context, w *Workstream, a *Agent) (removed bool, kept string, err error) {
	c.update(w, func() { a.Awaiting = false })
	id := a.SessionID
	if s, err := c.follow(ctx, id); err != nil {
		c.cfg.Log("following agent " + id + ": " + err.Error())
	} else if working(s) {
		if err := c.cfg.Sessions.Interrupt(ctx, id); err != nil {
			return false, "", err
		}
		for deadline := time.Now().Add(idleWait); working(s) && time.Now().Before(deadline); {
			select {
			case <-ctx.Done():
				return false, "", ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	c.mu.Lock()
	path, base := a.WorktreePath, a.BaseBranch
	c.mu.Unlock()
	var archivedWith bool
	if path != "" {
		removed, archivedWith, kept = c.removeWorktree(ctx, id, path, base)
	}
	if archivedWith {
		if err := c.cfg.Sessions.Close(ctx, id); err != nil {
			c.cfg.Log("closing agent " + id + ": " + err.Error())
		}
	} else if err := c.cfg.Sessions.Archive(ctx, id); err != nil {
		return removed, kept, err
	}
	c.update(w, func() { a.ArchivedAt = c.now() })
	return removed, kept, nil
}

// idleWait bounds how long archiving waits for an interrupted turn to end.
const idleWait = 10 * time.Second

// removeWorktree has the Daemon remove an agent's worktree, as Settings →
// Worktrees does, when it holds nothing unmerged; the Daemon archives the
// agent's Session with it. kept says why it stays.
func (c *Coordinator) removeWorktree(ctx context.Context, id, path, base string) (removed, archived bool, kept string) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, false, ""
	}
	held, err := unmergedWork(ctx, path, base)
	if err != nil {
		return false, false, "it could not be checked: " + err.Error()
	}
	if held != "" {
		return false, false, held
	}
	cl, err := c.cfg.Sessions.Client()
	if err != nil {
		return false, false, "it could not be removed: " + err.Error()
	}
	no := false
	res, err := cl.CleanupWorktree(ctx, protocol.CleanupWorktreeParams{WorktreePath: path, DeleteLocalBranch: &no, DeleteRemoteBranch: &no, Force: &no})
	switch {
	case err != nil:
		return false, false, "it could not be removed: " + err.Error()
	case !res.WorktreeRemoved:
		return false, slices.Contains(res.ArchivedSessionIDs, id), "the Daemon kept it (" + cmp.Or(string(res.PreservedReason), "no reason given") + ")"
	}
	return true, slices.Contains(res.ArchivedSessionIDs, id), ""
}

// Archive archives a Workstream: every agent as archive_agent does, then the
// orchestrator. The Shared Folder stays. It answers the worktrees kept.
func (c *Coordinator) Archive(ctx context.Context, id string) ([]string, error) {
	w, err := c.find(id)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	var agents []*Agent
	for _, a := range w.Agents {
		if a.ArchivedAt == "" {
			agents = append(agents, a)
		}
	}
	orch := w.OrchestratorID
	c.mu.Unlock()
	kept := []string{}
	for _, a := range agents {
		_, why, err := c.archiveAgent(ctx, w, a)
		if err != nil {
			return kept, fmt.Errorf("archiving agent %q: %w", a.Title, err)
		}
		if why != "" {
			kept = append(kept, a.WorktreePath)
		}
	}
	if _, err := c.follow(ctx, orch); err != nil {
		c.cfg.Log("following orchestrator " + orch + ": " + err.Error())
	}
	if err := c.cfg.Sessions.Archive(ctx, orch); err != nil {
		return kept, fmt.Errorf("archiving the orchestrator: %w", err)
	}
	c.update(w, func() { w.ArchivedAt = c.now() })
	return kept, nil
}

// see follows the Runner's events: an agent's turn end may be one the
// orchestrator waits on, and is when a working agent takes new settings.
func (c *Coordinator) see(e hostsession.Event) {
	switch e := e.(type) {
	case hostsession.WorkingChanged:
		c.mu.Lock()
		c.activity[e.SessionID] = c.cfg.Now()
		c.mu.Unlock()
	case hostsession.TurnEnded:
		w, a := c.agentOf(e.SessionID)
		if a == nil {
			return
		}
		c.mu.Lock()
		c.activity[e.SessionID] = c.cfg.Now()
		awaiting, reported := a.Awaiting, a.ReportedThisTurn
		a.Awaiting, a.ReportedThisTurn = false, false
		c.save(w)
		orch, title := w.OrchestratorID, a.Title
		c.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		failed := e.Reason != protocol.AgentTurnCompletionReasonCompleted
		if awaiting && (!reported || failed) {
			if err := c.cfg.Sessions.Send(ctx, orch, noReportMessage(title, e.SessionID, turnReason(e), e.LastReply)); err != nil {
				c.cfg.Log("telling the orchestrator of agent " + e.SessionID + ": " + err.Error())
			}
		}
		// The working state goes idle just after the turn's end.
		for deadline := time.Now().Add(settleWait); working(c.cfg.Sessions.Session(e.SessionID)) && time.Now().Before(deadline); {
			time.Sleep(20 * time.Millisecond)
		}
		if err := c.moveAgent(ctx, w, e.SessionID); err != nil {
			c.cfg.Log("moving agent " + e.SessionID + " to the workstream's settings: " + err.Error())
		}
	}
}

// turnReason is why a turn ended, as the orchestrator is told.
func turnReason(e hostsession.TurnEnded) string {
	reason := cmp.Or(string(e.Reason), "unknown")
	if e.Error != "" {
		reason += ": " + e.Error
	}
	return reason
}

// resume tells each orchestrator of the agents it waited on that are idle
// now that Droi has restarted; it follows the working ones, whose turn end
// tells it.
func (c *Coordinator) resume(ctx context.Context) {
	type waiting struct {
		w *Workstream
		a *Agent
	}
	var todo []waiting
	c.mu.Lock()
	for _, w := range c.all {
		if w.ArchivedAt != "" {
			continue
		}
		for _, a := range w.Agents {
			if a.ArchivedAt == "" && a.Awaiting {
				todo = append(todo, waiting{w, a})
			}
		}
	}
	c.mu.Unlock()
	for _, t := range todo {
		s, err := c.follow(ctx, t.a.SessionID)
		if err != nil {
			c.cfg.Log("following agent " + t.a.SessionID + ": " + err.Error())
			continue
		}
		if working(s) {
			continue
		}
		c.mu.Lock()
		still := t.a.Awaiting
		t.a.Awaiting, t.a.ReportedThisTurn = false, false
		c.save(t.w)
		orch, title := t.w.OrchestratorID, t.a.Title
		c.mu.Unlock()
		if !still {
			continue
		}
		if err := c.cfg.Sessions.Send(ctx, orch, noReportMessage(title, t.a.SessionID, restartReason, lastReply(s))); err != nil {
			c.cfg.Log("telling the orchestrator of agent " + t.a.SessionID + ": " + err.Error())
		}
	}
}

// lastReply is the text of a Session's last assistant message that has any.
func lastReply(s *session.Session) string {
	msgs := s.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != protocol.MessageRoleAssistant {
			continue
		}
		var text strings.Builder
		for _, b := range msgs[i].Content {
			if v, err := b.Value(); err == nil {
				if tb, ok := v.(*protocol.TextBlock); ok {
					text.WriteString(tb.Text)
				}
			}
		}
		if text.Len() > 0 {
			return text.String()
		}
	}
	return ""
}
