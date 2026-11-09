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
	"unicode/utf8"

	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/v2/internal/automationwork"
	"github.com/kkkk2323/droi/apps/native/v2/internal/hostsession"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/sessions"
)

// Config is what a Coordinator needs from the Host.
type Config struct {
	// Dir holds the Workstreams' folders: <UserData>/workstreams.
	Dir string
	// SessionsDir is the Daemon's sessions folder, whose transcripts tell
	// which Session was handed off to which; "" follows no handoff.
	SessionsDir string
	// Sessions starts and follows the Workstreams' Sessions.
	Sessions *hostsession.Runner
	// ScriptModels, optional, is where the models that may use Script stand
	// (host.Host.ScriptModelList), for the agents' ScriptCapable.
	ScriptModels func() gateway.ScriptModelList
	Log          func(string)
	Now          func() time.Time
}

// Coordinator keeps the Host's Workstreams: it starts them, carries out
// their tools, and forwards what the orchestrator must hear. Safe for use
// from any goroutine.
type Coordinator struct {
	cfg   Config
	links *lineage

	// branches is held while a new agent's branch is chosen.
	branches sync.Mutex

	// life ends with Close, which waits for work to finish.
	life        context.Context
	end         context.CancelFunc
	work        sync.WaitGroup
	unsubscribe func()

	mu     sync.Mutex
	closed bool
	all    map[string]*Workstream
	// activity is when the Host last saw each Session do something.
	activity map[string]time.Time
	// queue is the end of each agent's line of work (see serial), by agent:
	// its Session changes when it is handed off.
	queue map[*Agent]chan struct{}
	// news hears of an agent's new settings while moveAgent waits for them.
	news map[string]chan struct{}
	// claimed are the branches of agents being started.
	claimed map[string]bool
	// starting are the agents spawn_agent is making Sessions for.
	starting []*starting

	// subsMu guards the subscribers of OnChange, which dirty tells to be
	// called.
	subsMu  sync.Mutex
	subs    map[int]func()
	nextSub int
	dirty   chan struct{}
}

// New makes a Coordinator of the Workstreams in cfg.Dir; Start resumes them.
func New(cfg Config) *Coordinator {
	if cfg.Log == nil {
		cfg.Log = func(string) {}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	c := &Coordinator{
		cfg: cfg, links: newLineage(cfg.SessionsDir), all: loadAll(cfg.Dir, cfg.Log), activity: map[string]time.Time{},
		queue: map[*Agent]chan struct{}{}, news: map[string]chan struct{}{}, claimed: map[string]bool{},
		subs: map[int]func(){}, dirty: make(chan struct{}, 1),
	}
	c.life, c.end = context.WithCancel(context.Background())
	c.unsubscribe = cfg.Sessions.Subscribe(c.hear)
	c.work.Add(1)
	go func() {
		defer c.work.Done()
		c.announce()
	}()
	return c
}

// errClosed is work refused by a closed Coordinator.
var errClosed = errors.New("Droi is quitting")

// Close stops the Coordinator: it takes no more work, ends what runs and
// waits for it.
func (c *Coordinator) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.unsubscribe()
	c.end()
	c.work.Wait()
}

// begin counts a goroutine Close waits for; false once closed. Called
// without c.mu.
func (c *Coordinator) begin() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.work.Add(1)
	return true
}

// Start waits, until ctx ends, for the Runner to reach the Daemon, then
// tells each orchestrator of an agent it waited on that Droi's restart left
// idle.
func (c *Coordinator) Start(ctx context.Context) {
	if !c.begin() {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.life, cancel)
	go func() {
		defer c.work.Done()
		defer stop()
		defer cancel()
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
	c.changed()
}

// update changes w's record under c.mu and saves it.
func (c *Coordinator) update(w *Workstream, fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn()
	c.save(w)
}

// serial runs fn on a goroutine of its own once the work queued before it
// for the agent a has run, so that an agent's turn ends, messages,
// settings and archiving never overlap; done closes when fn has run, or
// once Close has kept it from running.
func (c *Coordinator) serial(a *Agent, fn func()) (done <-chan struct{}) {
	ch := make(chan struct{})
	if !c.begin() {
		close(ch)
		return ch
	}
	c.mu.Lock()
	prev := c.queue[a]
	c.queue[a] = ch
	c.mu.Unlock()
	go func() {
		defer c.work.Done()
		if prev != nil {
			select {
			case <-prev:
			case <-c.life.Done():
			}
		}
		if c.life.Err() == nil {
			fn()
		}
		c.mu.Lock()
		if c.queue[a] == ch {
			delete(c.queue, a)
		}
		c.mu.Unlock()
		close(ch)
	}()
	return ch
}

// inOrder runs fn in the agent a's line of work and waits for it; fn's
// context also ends with Close.
func (c *Coordinator) inOrder(ctx context.Context, a *Agent, fn func(ctx context.Context) error) error {
	err := errClosed
	<-c.serial(a, func() {
		ctx, cancel := context.WithCancel(ctx)
		defer context.AfterFunc(c.life, cancel)()
		defer cancel()
		err = fn(ctx)
	})
	return err
}

// sessionOf is the agent a's Session now.
func (c *Coordinator) sessionOf(a *Agent) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return a.SessionID
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

// archivedTitle is the title of the archived Workstream whose orchestrator
// sessionID was; "" for none.
func (c *Coordinator) archivedTitle(sessionID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, w := range c.all {
		if w.ArchivedAt != "" && sessionID != "" && w.OrchestratorID == sessionID {
			return w.Title
		}
	}
	return ""
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

// checkTitle is title trimmed, or why it cannot be a Workstream's name.
func checkTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return "", refuse("Enter a name.")
	case utf8.RuneCountInString(title) > gateway.MaxWorkstreamTitle:
		return "", refuse("The name is longer than %d characters.", gateway.MaxWorkstreamTitle)
	}
	return title, nil
}

// checkSymbol is symbol trimmed, or why it cannot be a Workstream's symbol.
func checkSymbol(symbol string) (string, error) {
	symbol = strings.TrimSpace(symbol)
	if !gateway.ValidWorkstreamSymbol(symbol) {
		return "", refuse("The symbol must be lowercase letters, digits and dashes, at most 32 characters.")
	}
	return symbol, nil
}

// Create starts a Workstream: its folders, then its orchestrator in the
// Workspace, which gets the goal as its first message when there is one;
// without, the orchestrator waits for the user's.
func (c *Coordinator) Create(ctx context.Context, in gateway.NewWorkstream) (gateway.WorkstreamCreated, error) {
	title, err := checkTitle(in.Title)
	if err != nil {
		return gateway.WorkstreamCreated{}, err
	}
	symbol, err := checkSymbol(in.Symbol)
	if err != nil {
		return gateway.WorkstreamCreated{}, err
	}
	workspace, goal := strings.TrimSpace(in.Workspace), strings.TrimSpace(in.Goal)
	if workspace == "" {
		return gateway.WorkstreamCreated{}, refuse("Choose a workspace.")
	}
	if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
		return gateway.WorkstreamCreated{}, refuse("%s is not a folder on this computer.", workspace)
	}
	w := &Workstream{
		ID: newID(title), Title: title, Workspace: workspace, Symbol: symbol,
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
		// The Daemon may have started the orchestrator all the same, say
		// when the bound on Create ended the wait for its answer.
		if opened.SessionID != "" {
			if err := c.cfg.Sessions.Archive(context.WithoutCancel(ctx), opened.SessionID); err != nil {
				c.cfg.Log("archiving the orchestrator " + opened.SessionID + " of workstream " + w.ID + ", which did not start: " + err.Error())
			}
		} else {
			c.cfg.Log("the orchestrator of workstream " + w.ID + " did not start (" + err.Error() + "); a Session tagged with the workstream may be left")
		}
		_ = os.RemoveAll(filepath.Join(c.cfg.Dir, w.ID))
		return gateway.WorkstreamCreated{}, err
	}
	c.mu.Lock()
	w.OrchestratorID = opened.SessionID
	c.all[w.ID] = w
	c.save(w)
	c.mu.Unlock()
	// Sent once recorded, so that the orchestrator's first tool call finds
	// it. The Workstream stands without it: the user can type the goal in
	// the orchestrator's Session.
	if goal != "" {
		if err := c.sendGoal(ctx, opened.SessionID, goal); err != nil {
			c.cfg.Log("the orchestrator " + opened.SessionID + " started but did not get the goal: " + err.Error())
		}
	}
	return gateway.WorkstreamCreated{WorkstreamID: w.ID, OrchestratorID: opened.SessionID}, nil
}

// goalRetryWait is the pause before the goal is sent a second time.
const goalRetryWait = time.Second

func (c *Coordinator) sendGoal(ctx context.Context, orch, goal string) error {
	err := c.cfg.Sessions.Send(ctx, orch, goal)
	if err == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return err
	case <-time.After(goalRetryWait):
	}
	return c.cfg.Sessions.Send(ctx, orch, goal)
}

func orchestratorHidden() []string {
	return append(slices.Clone(automationwork.FactoryAutomationTools), hostToolsSelector(ToolReport)...)
}

func agentHidden() []string {
	return append(slices.Clone(automationwork.FactoryAutomationTools), hostToolsSelector(orchestratorTools...)...)
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
	if w.Symbol != "" {
		md["symbol"] = w.Symbol
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

// tagsOf are the tags of a Session the Runner follows; none for another.
func (c *Coordinator) tagsOf(id string) []protocol.SessionTag {
	if s := c.cfg.Sessions.Session(id); s != nil {
		return s.Settings().Tags
	}
	return nil
}

// List is every Workstream, the newest first, with the Sessions their
// Sessions were handed off to.
func (c *Coordinator) List(ctx context.Context) ([]gateway.Workstream, error) {
	c.followHandoffs(ctx)
	var scripts gateway.ScriptModelList
	if c.cfg.ScriptModels != nil {
		scripts = c.cfg.ScriptModels()
	}
	type row struct {
		w        Workstream
		agents   []Agent
		activity map[string]time.Time
		starting []gateway.WorkstreamStarting
	}
	now := c.cfg.Now()
	c.mu.Lock()
	rows := make([]row, 0, len(c.all))
	for _, w := range c.all {
		r := row{w: *w, activity: map[string]time.Time{}}
		for _, a := range w.Agents {
			r.agents = append(r.agents, *a)
			r.activity[a.SessionID] = c.activity[a.SessionID]
		}
		for _, s := range c.starting {
			if s.workstream == w.ID && now.Sub(s.at) < startingMax {
				r.starting = append(r.starting, gateway.WorkstreamStarting{Title: s.title, StartedAt: s.at.UTC().Format(time.RFC3339)})
			}
		}
		rows = append(rows, r)
	}
	c.mu.Unlock()
	out := make([]gateway.Workstream, 0, len(rows))
	for _, r := range rows {
		w := r.w
		g := gateway.Workstream{
			ID: w.ID, Title: w.Title, Workspace: w.Workspace, Symbol: w.Symbol, SharedFolder: c.SharedFolder(w.ID),
			OrchestratorID: w.OrchestratorID, AgentModel: w.AgentModel, AgentReasoningEffort: w.AgentReasoningEffort,
			CreatedAt: w.CreatedAt, ArchivedAt: w.ArchivedAt, Agents: make([]gateway.WorkstreamAgent, 0, len(r.agents)),
			Starting: r.starting,
		}
		if s := c.cfg.Sessions.Session(w.OrchestratorID); s != nil && w.ArchivedAt == "" {
			// The user may have renamed the orchestrator's Session.
			if title := s.Title(); title != "" {
				g.Title = title
			}
		}
		if w.ArchivedAt == "" {
			mode, _ := c.toolModeOf(w.OrchestratorID, "", "")
			g.OrchestratorToolMode = mode
			g.OrchestratorScriptCapable = scriptCapable(scripts, c.modelOf(w.OrchestratorID, ""))
		}
		for _, a := range r.agents {
			ga := gateway.WorkstreamAgent{
				SessionID: a.SessionID, Title: a.Title, WorktreePath: a.WorktreePath, Branch: a.Branch, BaseBranch: a.BaseBranch,
				Model: a.Model, ReasoningEffort: a.ReasoningEffort, CreatedAt: a.CreatedAt, ArchivedAt: a.ArchivedAt, Awaiting: a.Awaiting,
			}
			if r := a.LastReport; r != nil {
				ga.LastReport = &gateway.WorkstreamReport{Kind: r.Kind, Text: r.Text, At: r.At}
			}
			if a.ArchivedAt == "" {
				ga.ToolMode, ga.ToolModeModel = c.toolModeOf(a.SessionID, a.ToolMode, a.ToolModeModel)
				ga.ScriptCapable = scriptCapable(scripts, c.modelOf(a.SessionID, a.Model))
				if t := c.lastActive(a, r.activity[a.SessionID]); !t.IsZero() {
					ga.LastActivityAt = t.UTC().Format(time.RFC3339)
				}
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

// toolModeOf is the tool execution mode of the Session id and the model droid
// resolved it for: from its settings file, which has the mode droid chose at
// the last turn's boundary, else the one recorded at its start.
func (c *Coordinator) toolModeOf(id, recorded, recordedModel string) (mode, model string) {
	if s, ok := c.links.sessionSettings(id); ok && s.ToolMode != "" {
		return s.ToolMode, s.ToolModeModel
	}
	return recorded, recordedModel
}

// modelOf is the model the Session id has now: the Session's own, as the
// Runner holds it, else the settings file's, else recorded.
func (c *Coordinator) modelOf(id, recorded string) string {
	if s := c.cfg.Sessions.Session(id); s != nil {
		if m := s.Settings().ModelID; m != "" {
			return m
		}
	}
	if s, ok := c.links.sessionSettings(id); ok && s.Model != "" {
		return s.Model
	}
	return recorded
}

// scriptCapable is whether model may use Script on the computer's Daemon;
// nil when the list is not known and does not have it.
func scriptCapable(list gateway.ScriptModelList, model string) *bool {
	if model == "" {
		return nil
	}
	allowed, known := list.Allows(model)
	if !allowed && !known {
		return nil
	}
	return &allowed
}

// lastActive is when the agent a's Session last did something the Host saw,
// seen being when it last heard of it.
func (c *Coordinator) lastActive(a Agent, seen time.Time) time.Time {
	last := seen
	if s := c.cfg.Sessions.Session(a.SessionID); s != nil && c.cfg.Sessions.Following(a.SessionID) {
		if t := s.WorkingStateChangedAt(); t.After(last) {
			last = t
		}
	}
	for _, at := range []string{a.CreatedAt, reportAt(a.LastReport)} {
		if t, err := time.Parse(time.RFC3339, at); err == nil && t.After(last) {
			last = t
		}
	}
	return last
}

// followHandoffs takes over the Sessions the active Workstreams' Sessions
// were handed off to.
func (c *Coordinator) followHandoffs(ctx context.Context) {
	type link struct {
		w *Workstream
		a *Agent
	}
	var links []link
	c.mu.Lock()
	for _, w := range c.all {
		if w.ArchivedAt != "" {
			continue
		}
		links = append(links, link{w: w})
		for _, a := range w.Agents {
			if a.ArchivedAt == "" {
				links = append(links, link{w, a})
			}
		}
	}
	c.mu.Unlock()
	for _, l := range links {
		c.newest(ctx, l.w, l.a)
	}
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
// orchestrator's tag and the record. Each agent is to take them, with ""
// resolved against the orchestrator's settings now: an idle one now, a
// working one when its turn ends.
func (c *Coordinator) SetAgentSettings(ctx context.Context, s gateway.WorkstreamSettings) error {
	w, err := c.find(s.ID)
	if err != nil {
		return err
	}
	c.update(w, func() {
		w.AgentModel, w.AgentReasoningEffort = strings.TrimSpace(s.AgentModel), strings.TrimSpace(s.AgentReasoningEffort)
	})
	orch := c.newest(ctx, w, nil)
	c.mu.Lock()
	tag := orchestratorTag(w)
	c.mu.Unlock()
	if _, err := c.follow(ctx, orch, orchestratorHidden()); err != nil {
		return err
	}
	if err := c.cfg.Sessions.UpdateSettings(ctx, protocol.UpdateSessionSettingsParams{SessionID: orch, Tags: withTag(c.tagsOf(orch), tag)}); err != nil {
		return err
	}
	model, effort, err := c.agentSettings(ctx, w)
	if err != nil {
		return err
	}
	var agents []*Agent
	c.mu.Lock()
	for _, a := range w.Agents {
		if a.ArchivedAt == "" {
			a.PendingModel, a.PendingReasoningEffort = model, effort
			agents = append(agents, a)
		}
	}
	c.save(w)
	c.mu.Unlock()
	for _, a := range agents {
		if err := c.inOrder(ctx, a, func(ctx context.Context) error { return c.moveAgent(ctx, w, a) }); err != nil {
			c.cfg.Log("moving agent " + a.Title + " to the workstream's settings: " + err.Error())
		}
	}
	return nil
}

// Edit renames a Workstream or changes its symbol. The name is its
// orchestrator's Session title, which Clients list it by, and is also on the
// record; the symbol is on the record and the orchestrator's tag. The
// prompts of the Sessions started already keep the old name.
func (c *Coordinator) Edit(ctx context.Context, e gateway.WorkstreamEdit) error {
	w, err := c.find(e.ID)
	if err != nil {
		return err
	}
	var title, symbol string
	if e.Title != nil {
		if title, err = checkTitle(*e.Title); err != nil {
			return err
		}
	}
	if e.Symbol != nil {
		if symbol, err = checkSymbol(*e.Symbol); err != nil {
			return err
		}
	}
	orch := c.newest(ctx, w, nil)
	if e.Title != nil {
		cl, err := c.cfg.Sessions.Client()
		if err != nil {
			return err
		}
		if _, err := cl.RenameSession(ctx, protocol.RenameSessionParams{SessionID: orch, Title: title}); err != nil {
			return err
		}
	}
	c.update(w, func() {
		if e.Title != nil {
			w.Title = title
		}
		if e.Symbol != nil {
			w.Symbol = symbol
		}
	})
	if e.Symbol == nil {
		return nil
	}
	c.mu.Lock()
	tag := orchestratorTag(w)
	c.mu.Unlock()
	if _, err := c.follow(ctx, orch, orchestratorHidden()); err != nil {
		return err
	}
	return c.cfg.Sessions.UpdateSettings(ctx, protocol.UpdateSessionSettingsParams{SessionID: orch, Tags: withTag(c.tagsOf(orch), tag)})
}

// follow is a Session the Runner follows, loaded first when it does not
// with hidden, the tools its role does without: a Session handed off hides
// none of its own.
func (c *Coordinator) follow(ctx context.Context, id string, hidden []string) (*session.Session, error) {
	if !c.cfg.Sessions.Following(id) {
		if err := c.cfg.Sessions.Load(ctx, id, hidden...); err != nil {
			return nil, err
		}
	}
	s := c.cfg.Sessions.Session(id)
	if s == nil {
		return nil, fmt.Errorf("session %s is not loaded", id)
	}
	return s, nil
}

// send sends a Session a user message, loading it first as follow does.
func (c *Coordinator) send(ctx context.Context, id string, hidden []string, text string) error {
	if _, err := c.follow(ctx, id, hidden); err != nil {
		return err
	}
	return c.cfg.Sessions.Send(ctx, id, text)
}

func working(s *session.Session) bool {
	if s == nil {
		return false
	}
	ws := s.WorkingState()
	return ws != "" && ws != protocol.DroidWorkingStateIdle
}

// agentSettings are the model and effort w's agents are to have now: the
// user's, else the orchestrator's current ones.
func (c *Coordinator) agentSettings(ctx context.Context, w *Workstream) (model, effort string, err error) {
	c.mu.Lock()
	model, effort = w.AgentModel, w.AgentReasoningEffort
	c.mu.Unlock()
	if model != "" && effort != "" {
		return model, effort, nil
	}
	orch, err := c.follow(ctx, c.newest(ctx, w, nil), orchestratorHidden())
	if err != nil {
		return "", "", err
	}
	set := orch.Settings()
	return cmp.Or(model, set.ModelID), cmp.Or(effort, string(set.ReasoningEffort)), nil
}

// settleWait bounds how long the Host waits for the Daemon to report an
// agent's new settings, and for an agent to go idle after its turn ends.
const settleWait = 3 * time.Second

// moveAgent moves an idle agent to the settings it is to take, unless the
// user switched its model or effort in its own Session; the settings it then
// takes go on its record and tag. Run in the agent's line of work.
func (c *Coordinator) moveAgent(ctx context.Context, w *Workstream, a *Agent) error {
	id := c.newest(ctx, w, a)
	c.mu.Lock()
	if a.PendingModel == "" && a.PendingReasoningEffort == "" {
		c.mu.Unlock()
		return nil
	}
	model, effort, takenModel, takenEffort, orch := a.PendingModel, a.PendingReasoningEffort, a.Model, a.ReasoningEffort, w.OrchestratorID
	c.mu.Unlock()
	s, err := c.follow(ctx, id, agentHidden())
	if err != nil {
		return err
	}
	if working(s) {
		return nil
	}
	drop := func() { c.update(w, func() { a.PendingModel, a.PendingReasoningEffort = "", "" }) }
	if set := s.Settings(); set.ModelID != takenModel || string(set.ReasoningEffort) != takenEffort {
		// Switched by hand: the user's pick stays.
		drop()
		return nil
	}
	if model == takenModel && effort == takenEffort {
		drop()
		return nil
	}
	news := make(chan struct{}, 1)
	c.mu.Lock()
	c.news[id] = news
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.news, id)
		c.mu.Unlock()
	}()
	if err := c.cfg.Sessions.UpdateSettings(ctx, protocol.UpdateSessionSettingsParams{SessionID: id, ModelID: model, ReasoningEffort: protocol.ReasoningEffort(effort)}); err != nil {
		return err
	}
	tookModel, tookEffort := settled(ctx, func() (string, string) {
		set := s.Settings()
		return set.ModelID, string(set.ReasoningEffort)
	}, news, takenModel, takenEffort, model, effort, settleWait)
	tookModel, tookEffort = cmp.Or(tookModel, model), cmp.Or(tookEffort, effort)
	c.update(w, func() {
		a.Model, a.ReasoningEffort = tookModel, tookEffort
		a.PendingModel, a.PendingReasoningEffort = "", ""
	})
	return c.cfg.Sessions.UpdateSettings(ctx, protocol.UpdateSessionSettingsParams{SessionID: id, Tags: withTag(s.Settings().Tags, agentTag(w.ID, orch, tookModel, tookEffort))})
}

// settled waits, up to timeout, until current holds model and
// effort, or news of settings other than wasModel and wasEffort, which it
// held before they were asked for, comes; it answers what current holds
// then, which may be an effort the Daemon fitted to the model.
func settled(ctx context.Context, current func() (string, string), news <-chan struct{}, wasModel, wasEffort, model, effort string, timeout time.Duration) (string, string) {
	deadline := time.After(timeout)
	for {
		if m, e := current(); m == model && e == effort {
			return m, e
		}
		select {
		case <-news:
			// News of the settings as they were is of an earlier change.
			if m, e := current(); m != wasModel || e != wasEffort {
				return m, e
			}
		case <-deadline:
			return current()
		case <-ctx.Done():
			return current()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func agentIn(w *Workstream, id string) *Agent {
	for _, a := range w.Agents {
		if a.SessionID == id {
			return a
		}
	}
	return nil
}

// claimBranch is the branch of a new agent titled title in the repository
// of dir: droid/<slug>, numbered when the repository or another agent has
// it. release frees the claim once the agent is recorded or failed to start.
func (c *Coordinator) claimBranch(ctx context.Context, dir, title string) (name string, release func()) {
	c.branches.Lock()
	defer c.branches.Unlock()
	base := "droid/" + cmp.Or(slug(title), "agent")
	for n := 1; ; n++ {
		name = base
		if n > 1 {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		if !c.branchHeld(name) && !hasBranch(ctx, dir, name) {
			break
		}
	}
	c.mu.Lock()
	c.claimed[name] = true
	c.mu.Unlock()
	return name, func() {
		c.mu.Lock()
		delete(c.claimed, name)
		c.mu.Unlock()
	}
}

// branchHeld reports that an agent has the branch name or is being started
// on it.
func (c *Coordinator) branchHeld(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.claimed[name] {
		return true
	}
	for _, w := range c.all {
		for _, a := range w.Agents {
			if a.ArchivedAt == "" && a.Branch == name {
				return true
			}
		}
	}
	return false
}

// spawn starts an agent of w: in a persistent worktree of the Workspace on
// a branch of its own when it is a git repository, on the agent settings,
// at the orchestrator's autonomy, with the task as its first message.
func (c *Coordinator) spawn(ctx context.Context, w *Workstream, title, task string) (*Agent, error) {
	model, effort, err := c.agentSettings(ctx, w)
	if err != nil {
		return nil, err
	}
	orch := c.newest(ctx, w, nil)
	c.mu.Lock()
	wsTitle, workspace := w.Title, w.Workspace
	c.mu.Unlock()
	var autonomy protocol.AutonomyLevel
	if s := c.cfg.Sessions.Session(orch); s != nil {
		autonomy = s.Settings().AutonomyLevel
	}
	at := Place{Workspace: workspace}
	repo := isGitRepo(ctx, workspace)
	base := ""
	if repo {
		base = currentBranch(ctx, workspace)
		at.Repository, at.Subfolder = repositoryOf(ctx, workspace)
		var release func()
		at.Branch, release = c.claimBranch(ctx, workspace, title)
		defer release()
	}
	started := c.startAgent(w.ID, title)
	defer started()
	opened, err := c.cfg.Sessions.Open(ctx, hostsession.Spec{
		Title:             title,
		Cwd:               workspace,
		ModelID:           model,
		ReasoningEffort:   protocol.ReasoningEffort(effort),
		AutonomyLevel:     autonomy,
		Tags:              []protocol.SessionTag{agentTag(w.ID, orch, model, effort)},
		SystemPrompt:      AgentPrompt(title, wsTitle, c.SharedFolder(w.ID), at),
		Worktree:          repo,
		WorktreeNewBranch: at.Branch,
		DisabledTools:     agentHidden(),
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
		ToolMode: opened.ToolMode, ToolModeModel: cmp.Or(opened.ModelID, model),
	}
	if a.WorktreePath != "" {
		a.BaseBranch = base
	}
	// Once recorded, a is shared: a handoff may change it.
	id, took, tookEffort := a.SessionID, a.Model, a.ReasoningEffort
	c.mu.Lock()
	w.Agents = append(w.Agents, a)
	c.activity[id] = c.cfg.Now()
	c.save(w)
	c.mu.Unlock()
	started()
	if took != model || tookEffort != effort {
		tags := withTag(c.tagsOf(id), agentTag(w.ID, orch, took, tookEffort))
		if err := c.cfg.Sessions.UpdateSettings(ctx, protocol.UpdateSessionSettingsParams{SessionID: id, Tags: tags}); err != nil {
			c.cfg.Log("tagging agent " + id + ": " + err.Error())
		}
	}
	// Recorded first, so that a report in its first turn finds it.
	if err := c.cfg.Sessions.Send(ctx, id, fromOrchestrator(task)); err != nil {
		c.update(w, func() { a.Awaiting = false })
		return a, err
	}
	return a, nil
}

// message sends an agent more work, moving it to the settings it is to take
// first. Run in the agent's line of work.
func (c *Coordinator) message(ctx context.Context, w *Workstream, a *Agent, text string) error {
	id := c.newest(ctx, w, a)
	if err := c.moveAgent(ctx, w, a); err != nil {
		c.cfg.Log("moving agent " + id + " to the workstream's settings: " + err.Error())
	}
	c.update(w, func() { a.Awaiting, a.ReportedSinceMessage = true, false })
	if err := c.send(ctx, id, agentHidden(), fromOrchestrator(text)); err != nil {
		c.update(w, func() { a.Awaiting = false })
		return err
	}
	return nil
}

// report forwards an agent's report to its orchestrator.
func (c *Coordinator) report(ctx context.Context, w *Workstream, a *Agent, kind, text string) error {
	c.mu.Lock()
	a.LastReport = &Report{Kind: kind, Text: text, At: c.now()}
	a.ReportedSinceMessage = true
	c.activity[a.SessionID] = c.cfg.Now()
	c.save(w)
	title, id := a.Title, a.SessionID
	c.mu.Unlock()
	return c.send(ctx, c.newest(ctx, w, nil), orchestratorHidden(), reportMessage(title, id, kind, text))
}

// interrupt stops an agent's turn, which the orchestrator then no longer
// waits on. Run in the agent's line of work.
func (c *Coordinator) interrupt(ctx context.Context, w *Workstream, a *Agent) error {
	c.update(w, func() { a.Awaiting = false })
	return c.cfg.Sessions.Interrupt(ctx, c.newest(ctx, w, a))
}

// archiveAgent archives an agent: it stops its turn, removes its worktree
// and branch when they hold nothing its base branch does not, which
// archives the Session with them, else archives the Session alone, then
// the Sessions it was handed off from; kept says why a worktree stays. A
// Session archived already, say from a Client, counts as archived. Run in
// the agent's line of work.
func (c *Coordinator) archiveAgent(ctx context.Context, w *Workstream, a *Agent) (removed bool, kept string, err error) {
	gone, err := c.stopAgent(ctx, w, a)
	if err != nil {
		return false, "", err
	}
	id := c.sessionOf(a)
	c.mu.Lock()
	path, base := a.WorktreePath, a.BaseBranch
	c.mu.Unlock()
	var archivedWith bool
	if path != "" {
		removed, archivedWith, kept = c.removeWorktree(ctx, id, path, base)
	}
	switch {
	case archivedWith || gone:
		if c.cfg.Sessions.Following(id) {
			if err := c.cfg.Sessions.Close(ctx, id); err != nil {
				c.cfg.Log("closing agent " + id + ": " + err.Error())
			}
		}
	default:
		if err := c.cfg.Sessions.Archive(ctx, id); err != nil && !hostsession.IsSessionNotFound(err) {
			return removed, kept, err
		}
	}
	c.mu.Lock()
	earlier := slices.Clone(a.PreviousSessionIDs)
	c.mu.Unlock()
	if err := c.archiveLinks(ctx, earlier); err != nil {
		return removed, kept, err
	}
	c.update(w, func() { a.ArchivedAt = c.now() })
	return removed, kept, nil
}

// stopAgent stops an agent's turn, whose end the orchestrator then no
// longer waits on; gone reports a Session the Daemon no longer has. Run in
// the agent's line of work, so that the end of the stopped turn comes
// after it.
func (c *Coordinator) stopAgent(ctx context.Context, w *Workstream, a *Agent) (gone bool, err error) {
	err = c.cfg.Sessions.StopTurn(ctx, c.newest(ctx, w, a))
	gone = hostsession.IsSessionNotFound(err)
	if err != nil && !gone {
		return false, err
	}
	c.update(w, func() { a.Awaiting = false })
	return gone, nil
}

// archiveLinks archives the Sessions a Session was handed off from; one
// archived already counts as archived.
func (c *Coordinator) archiveLinks(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if err := c.cfg.Sessions.Archive(ctx, id); err != nil && !hostsession.IsSessionNotFound(err) {
			return fmt.Errorf("archiving %s, which was handed off: %w", id, err)
		}
	}
	return nil
}

// removeWorktree has the Daemon remove an agent's worktree and its branch,
// as Settings → Worktrees does, when they hold nothing unmerged; the Daemon
// archives the agent's Session with them. kept says why they stay.
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
	yes, no := true, false
	res, err := cl.CleanupWorktree(ctx, protocol.CleanupWorktreeParams{WorktreePath: path, DeleteLocalBranch: &yes, DeleteRemoteBranch: &no})
	switch {
	case err != nil:
		return false, false, "it could not be removed: " + err.Error()
	case !res.WorktreeRemoved:
		return false, slices.Contains(res.ArchivedSessionIDs, id), "the Daemon kept it (" + cmp.Or(string(res.PreservedReason), "no reason given") + ")"
	}
	return true, slices.Contains(res.ArchivedSessionIDs, id), ""
}

// Archive archives a Workstream: every agent as archive_agent does, then the
// orchestrator. The Shared Folder stays. It answers the worktrees kept. One
// cut short goes on, when asked again, with what it had not archived.
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
	c.mu.Unlock()
	// Every working agent is stopped at once, so that archiving many of them
	// fits in the time a Gateway request has. One that does not stop is
	// still waited on, should the archive stop before it.
	var wg sync.WaitGroup
	for _, a := range agents {
		wg.Go(func() {
			err := c.inOrder(ctx, a, func(ctx context.Context) error {
				_, err := c.stopAgent(ctx, w, a)
				return err
			})
			if err != nil {
				c.cfg.Log("stopping agent " + a.Title + ": " + err.Error())
			}
		})
	}
	wg.Wait()
	kept := []string{}
	for _, a := range agents {
		var why string
		err := c.inOrder(ctx, a, func(ctx context.Context) (err error) {
			_, why, err = c.archiveAgent(ctx, w, a)
			return err
		})
		if err != nil {
			return kept, fmt.Errorf("archiving agent %q: %w", a.Title, err)
		}
		if why != "" {
			kept = append(kept, a.WorktreePath)
		}
	}
	if err := c.cfg.Sessions.Archive(ctx, c.newest(ctx, w, nil)); err != nil && !hostsession.IsSessionNotFound(err) {
		return kept, fmt.Errorf("archiving the orchestrator: %w", err)
	}
	c.mu.Lock()
	earlier := slices.Clone(w.PreviousOrchestratorIDs)
	c.mu.Unlock()
	if err := c.archiveLinks(ctx, earlier); err != nil {
		return kept, fmt.Errorf("archiving the orchestrator: %w", err)
	}
	c.update(w, func() { w.ArchivedAt = c.now() })
	return kept, nil
}

// hear takes the Runner's events on its event goroutine, which it must not
// hold up: an agent's turn end waits in the agent's line of work.
func (c *Coordinator) hear(e hostsession.Event) {
	switch e := e.(type) {
	case hostsession.WorkingChanged:
		c.mu.Lock()
		c.activity[e.SessionID] = c.cfg.Now()
		member := c.memberLocked(e.SessionID)
		c.mu.Unlock()
		if member {
			c.changed()
		}
	case hostsession.SettingsUpdated:
		c.mu.Lock()
		news := c.news[e.SessionID]
		member := c.memberLocked(e.SessionID)
		c.mu.Unlock()
		if member {
			// droid's new tool mode for a changed model comes this way.
			c.changed()
		}
		if news != nil {
			select {
			case news <- struct{}{}:
			default:
			}
		}
	case hostsession.TurnEnded:
		if _, a := c.agentOf(e.SessionID); a != nil {
			c.serial(a, func() { c.turnEnded(e) })
		}
	}
}

// turnEnded tells the orchestrator of the end of an agent's turn it waited
// on, unless the agent reported since the last message and the turn
// completed; an agent working again, on a message that came as the turn
// ended, is still waited on. An idle agent then takes new settings.
func (c *Coordinator) turnEnded(e hostsession.TurnEnded) {
	w, a := c.agentOf(e.SessionID)
	if a == nil {
		return
	}
	if e.Error == hostsession.SessionGone {
		c.agentGone(w, a, e.SessionID)
		return
	}
	c.mu.Lock()
	c.activity[e.SessionID] = c.cfg.Now()
	c.mu.Unlock()
	for deadline := time.Now().Add(settleWait); working(c.cfg.Sessions.Session(e.SessionID)) && time.Now().Before(deadline); {
		select {
		case <-c.life.Done():
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
	if working(c.cfg.Sessions.Session(e.SessionID)) {
		return
	}
	c.mu.Lock()
	awaiting, reported := a.Awaiting, a.ReportedSinceMessage
	a.Awaiting = false
	c.save(w)
	title := a.Title
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(c.life, time.Minute)
	defer cancel()
	failed := e.Reason != protocol.AgentTurnCompletionReasonCompleted
	if msg := endNotice(title, e.SessionID, turnReason(e), failed, reported, e.LastReply); awaiting && msg != "" {
		if err := c.send(ctx, c.newest(ctx, w, nil), orchestratorHidden(), msg); err != nil {
			c.cfg.Log("telling the orchestrator of agent " + e.SessionID + ": " + err.Error())
		}
	}
	if err := c.moveAgent(ctx, w, a); err != nil {
		c.cfg.Log("moving agent " + e.SessionID + " to the workstream's settings: " + err.Error())
	}
}

// agentGone records an agent whose Session id the Daemon no longer has,
// such as one archived from a Client, as archived, as resumeAgent does: its
// turn ended with no reply to tell of. One handed off from id goes on in
// the Session it was handed off to.
func (c *Coordinator) agentGone(w *Workstream, a *Agent, id string) {
	ctx, cancel := context.WithTimeout(c.life, time.Minute)
	defer cancel()
	if c.newest(ctx, w, a) != id {
		return
	}
	c.update(w, func() { a.Awaiting, a.ArchivedAt = false, c.now() })
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
		_ = c.inOrder(ctx, t.a, func(ctx context.Context) error {
			c.resumeAgent(ctx, t.w, t.a)
			return nil
		})
	}
}

// resumeAgent sends the restart notice of one agent the orchestrator waited
// on, unless it works or is gone: archived from a Client meanwhile, it is
// recorded archived.
func (c *Coordinator) resumeAgent(ctx context.Context, w *Workstream, a *Agent) {
	id := c.newest(ctx, w, a)
	s, err := c.follow(ctx, id, agentHidden())
	switch {
	case hostsession.IsSessionNotFound(err):
		c.update(w, func() { a.Awaiting, a.ArchivedAt = false, c.now() })
		return
	case err != nil:
		c.cfg.Log("following agent " + id + ": " + err.Error())
		s = nil
	case working(s):
		return
	}
	c.mu.Lock()
	still, reported := a.Awaiting, a.ReportedSinceMessage
	a.Awaiting = false
	c.save(w)
	title := a.Title
	c.mu.Unlock()
	if !still {
		return
	}
	orch := c.newest(ctx, w, nil)
	reply := ""
	if s != nil {
		reply = lastReply(s)
	}
	if err := c.send(ctx, orch, orchestratorHidden(), endNotice(title, id, restartReason, true, reported, reply)); err != nil {
		c.cfg.Log("telling the orchestrator of agent " + id + ": " + err.Error())
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
