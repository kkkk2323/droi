// Package hostsession is the Host's own Sessions: the ones it starts and
// follows itself on its Daemon, such as Automation runs (ADR 0035) and
// Workstream Sessions (ADR 0036). One Runner keeps one SDK Controller
// connected from the Host straight to its Daemon, as Memory dials it (ADR
// 0011), not through the Gateway: the Runner adds the System Prompt Addition
// itself. The Host never answers a Session's prompts; Clients do.
package hostsession

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/controller"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"
)

// Config is what a Runner needs from the Host.
type Config struct {
	// ResolveURL is the Daemon's address, waiting until it runs
	// (host.Host.WaitForDaemonURL).
	ResolveURL func(ctx context.Context) (string, error)
	// Credential authenticates the connection and spawns the Sessions'
	// workers (host.Host.Credential).
	Credential func(ctx context.Context) (*droid.Credential, error)
	// HasCredential, optional, reports that there is a credential to connect
	// with (host.Host.HasCredential); without one the Runner waits for
	// OnChange instead of trying.
	HasCredential func() bool
	// SystemPromptAddition, optional, is added after Droid's system prompt
	// of every Session the Runner opens, as the Gateway adds it to a
	// Client's (host.Host.SystemPromptAddition).
	SystemPromptAddition func() string
	// OnChange, optional, hears of a change that may let a failed connect
	// succeed, such as a sign-in or a restarted Daemon (host.Host.OnChange).
	OnChange func(fn func()) (unsubscribe func())
	// Log, optional, hears what went wrong.
	Log func(string)

	// maxReconnectAttempts is the Controller's; 0 is 1000. Tests set -1 to
	// leave reconnecting to the Runner.
	maxReconnectAttempts int
	// reloadRetry is the wait before a failed reload is tried again; 0 is a
	// minute.
	reloadRetry time.Duration
	// turnOutcome is daemon.get_session_turn_outcome; nil asks the Daemon.
	turnOutcome func(ctx context.Context, p protocol.GetSessionTurnOutcomeParams) (*protocol.DaemonGetSessionTurnOutcomeResult, error)
}

// Spec is a Session the Runner opens.
type Spec struct {
	Title string
	// Cwd is where the Session runs, or the repository whose worktree it
	// gets with Worktree.
	Cwd             string
	ModelID         string
	ReasoningEffort protocol.ReasoningEffort
	AutonomyLevel   protocol.AutonomyLevel
	// SkipPermissions runs every tool without asking
	// (skipPermissionsUnsafe), as the Daemon's own Automation runs did.
	SkipPermissions bool
	Tags            []protocol.SessionTag
	// SystemPrompt follows the System Prompt Addition, a blank line between.
	SystemPrompt string
	// Prompt is the first user message; "" sends none.
	Prompt string
	// Worktree runs the Session in a worktree of Cwd of its own, kept out of
	// the Daemon's clean-up of ephemeral ones.
	Worktree bool
	// WorktreeNewBranch, with Worktree, is the new branch the worktree gets,
	// cut from Cwd's HEAD; the Daemon refuses one that exists.
	WorktreeNewBranch string
	// DisabledTools are disabledToolIds: tool ids, llm ids and MCP selectors
	// (hosttools.Selector).
	DisabledTools            []string
	DisableInactivityTimeout bool
}

// Opened is a Session as the Daemon started it: the settings it answered.
type Opened struct {
	SessionID       string
	WorktreePath    string
	WorktreeBranch  string
	ModelID         string
	ReasoningEffort string
}

// Event is what Subscribe hears: TurnEnded, WorkingChanged or
// SettingsUpdated, of a Session the Runner follows.
type Event interface{ hostSessionEvent() }

// TurnEnded is an agent turn's end. A turn whose worker stopped without
// ending it ends with Reason process_exit and Error WorkerStopped, once the
// Runner sees the Session idle again. One a load finds over ends as the
// Daemon recorded it (daemon.get_session_turn_outcome), or, without its
// record, with Error LostTouch, as does one whose Session no reload could
// load; a followed Session the Daemon no longer has, with Error SessionGone.
type TurnEnded struct {
	SessionID string
	Reason    protocol.AgentTurnCompletionReason
	// Error is the turn's first error notification, "" when it had none.
	Error string
	// LastReply is the text of the turn's last assistant message: "" when
	// none followed the last user message that carried text.
	LastReply string
}

// WorkingChanged is a Session's new working state.
type WorkingChanged struct {
	SessionID string
	State     protocol.DroidWorkingState
}

// SettingsUpdated is a Session's new settings, which its Session already
// holds when the event comes.
type SettingsUpdated struct{ SessionID string }

func (TurnEnded) hostSessionEvent()       {}
func (WorkingChanged) hostSessionEvent()  {}
func (SettingsUpdated) hostSessionEvent() {}

// WorkerStopped is the Error of a TurnEnded whose worker stopped mid-turn.
const WorkerStopped = "the Session's worker stopped"

// LostTouch is the Error of a TurnEnded that ended while the Runner was not
// connected, when the Daemon has no record of how.
const LostTouch = "the Host lost touch with the Daemon while the Session worked"

// SessionGone is the Error of a TurnEnded of a followed Session the Daemon
// no longer has.
const SessionGone = "the Daemon no longer has this Session"

// reloadTries is how many reloads of a followed Session may fail, other
// than for a Session the Daemon does not have, before its turn ends with
// LostTouch.
const reloadTries = 3

// outcomeWait bounds the Daemon's look for a turn's record.
const outcomeWait = 5 * time.Second

// followed is what the Runner keeps of a Session it follows.
type followed struct {
	// inTurn: the Session started working and its turn has not ended.
	inTurn bool
	// firstError is the turn's first error notification.
	firstError string
	// reply is the text of the last assistant message since the last user
	// message that carried text.
	reply string
	// turnID is the message id of the prompt the Runner sent to start the
	// turn, under which the Daemon records its outcome; "" for another.
	turnID string
	// asking: the Daemon is being asked for the turn's outcome.
	asking bool
}

// Runner starts and follows the Host's Sessions; safe for use from any
// goroutine.
type Runner struct {
	cfg Config
	ctl *controller.Controller

	mu        sync.Mutex
	following map[string]*followed
	// hidden are the disabledToolIds each followed Session was opened or
	// loaded with, which a worker the Daemon spawns again does not keep.
	hidden map[string][]string
	subs   map[int]func(Event)
	next   int
}

// New makes a Runner; Start connects it.
func New(cfg Config) *Runner {
	r := &Runner{cfg: cfg, following: map[string]*followed{}, hidden: map[string][]string{}, subs: map[int]func(Event){}}
	reconnects := cfg.maxReconnectAttempts
	if reconnects == 0 {
		// The Daemon restarts on its own (the Host's Supervisor); keep trying.
		reconnects = 1000
	}
	r.ctl = controller.New(controller.Config{
		ResolveURL: cfg.ResolveURL,
		Credential: cfg.Credential,
		Caller:     "droi-native",
		// Start keeps trying, with its own backoff.
		MaxConnectAttempts:   1,
		MaxReconnectAttempts: reconnects,
		DefaultMessageLimit:  50,
	})
	r.ctl.Subscribe(r.see)
	return r
}

// Start keeps the Runner connected to the Daemon until ctx ends, then
// closes it.
func (r *Runner) Start(ctx context.Context) {
	go r.keepConnected(ctx)
}

func (r *Runner) keepConnected(ctx context.Context) {
	defer r.ctl.Close()
	poke, hostPoke := make(chan struct{}, 1), make(chan struct{}, 1)
	wake := func(ch chan struct{}) {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	if r.cfg.OnChange != nil {
		defer r.cfg.OnChange(func() { wake(hostPoke) })()
	}
	defer r.ctl.Subscribe(func(e controller.Event) {
		if _, ok := e.(controller.StatusChanged); ok {
			wake(poke)
		}
	})()
	var (
		delay     time.Duration
		connected bool
		lastErr   string
	)
	for ctx.Err() == nil {
		st := r.ctl.Status()
		signedIn := r.cfg.HasCredential == nil || r.cfg.HasCredential()
		if !st.Connected && !st.Reconnecting && signedIn {
			if err := r.ctl.Connect(ctx); err != nil && ctx.Err() == nil {
				// A Daemon that stays down fails the same way every 30 s.
				if err.Error() != lastErr {
					r.log("connect: " + err.Error())
				}
				lastErr = err.Error()
				delay = retryDelay(delay)
			} else if err == nil {
				if connected {
					// The Controller reloads its Sessions after its own
					// reconnect, not after this one.
					r.reloadFollowed(ctx)
				}
				connected, delay, lastErr = true, 0, ""
			}
		}
		if delay > 0 {
			// A failed Connect changes the status, which would only connect
			// again at once; a change on the Host may help before the wait ends.
			select {
			case <-hostPoke:
			case <-time.After(delay):
			case <-ctx.Done():
			}
			continue
		}
		select {
		case <-poke:
		case <-hostPoke:
		case <-ctx.Done():
		}
	}
}

// reloadFollowed loads every followed Session again on a new connection, so
// their notifications reach it, with the tools it hid.
func (r *Runner) reloadFollowed(ctx context.Context) {
	r.mu.Lock()
	loads := make([]protocol.LoadSessionParams, 0, len(r.following))
	for id := range r.following {
		loads = append(loads, protocol.LoadSessionParams{SessionID: id, DisabledToolIDs: r.hidden[id]})
	}
	r.mu.Unlock()
	for _, p := range loads {
		go r.reload(ctx, p)
	}
}

// reload loads a followed Session again, trying again a while when that
// fails other than for a Session the Daemon does not have (seeGone).
func (r *Runner) reload(ctx context.Context, p protocol.LoadSessionParams) {
	sessionID := p.SessionID
	wait := r.cfg.reloadRetry
	if wait <= 0 {
		wait = time.Minute
	}
	for try := 1; ; try++ {
		_, err := r.ctl.LoadSession(ctx, p)
		if err == nil || errors.Is(err, controller.ErrSessionNotFound) || ctx.Err() != nil {
			return
		}
		r.log("reload " + sessionID + ": " + err.Error())
		if try == reloadTries {
			r.mu.Lock()
			var out Event
			if f := r.following[sessionID]; f != nil && f.inTurn {
				out = f.end(sessionID, protocol.AgentTurnCompletionReasonProcessExit, LostTouch)
			}
			r.mu.Unlock()
			r.publish(out)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if !r.Following(sessionID) {
			return
		}
	}
}

// retryDelay is the wait after a failed connect that followed a wait of
// prev: a second, doubling to half a minute.
func retryDelay(prev time.Duration) time.Duration {
	if prev <= 0 {
		return time.Second
	}
	return min(2*prev, 30*time.Second)
}

func (r *Runner) log(s string) {
	if r.cfg.Log != nil {
		r.cfg.Log(s)
	}
}

// Open starts a Session and sends it the Spec's Prompt. A Session that
// started but did not take the Prompt answers its Opened with the error.
func (r *Runner) Open(ctx context.Context, s Spec) (Opened, error) {
	yes := true
	p := protocol.InitializeSessionParams{
		SessionID:       uuid.NewString(),
		Cwd:             s.Cwd,
		Title:           s.Title,
		ModelID:         s.ModelID,
		ReasoningEffort: s.ReasoningEffort,
		AutonomyLevel:   s.AutonomyLevel,
		Tags:            s.Tags,
		DisabledToolIDs: s.DisabledTools,
	}
	if prompt := r.systemPrompt(s.SystemPrompt); prompt != "" {
		p.SystemPrompt, _ = json.Marshal(struct {
			Type   string `json:"type"`
			Preset string `json:"preset"`
			Append string `json:"append"`
		}{"preset", "droid", prompt})
	}
	if s.SkipPermissions {
		p.SkipPermissionsUnsafe = &yes
	}
	if s.Worktree {
		p.Worktree = &yes
		p.WorktreeLifecycle = protocol.WorktreeLifecyclePersistent
		p.WorktreeNewBranch = s.WorktreeNewBranch
	}
	if s.DisableInactivityTimeout {
		p.DisableInactivityTimeout = &yes
	}
	// Followed before it starts, so no notification of its first turn is
	// missed.
	r.follow(p.SessionID, true)
	r.hide(p.SessionID, s.DisabledTools)
	res, err := r.ctl.InitializeSession(ctx, p)
	if err != nil {
		r.follow(p.SessionID, false)
		return Opened{}, err
	}
	if res.SessionID != p.SessionID {
		r.follow(p.SessionID, false)
		r.follow(res.SessionID, true)
		r.hide(res.SessionID, s.DisabledTools)
	}
	o := Opened{SessionID: res.SessionID, ModelID: res.Settings.ModelID, ReasoningEffort: string(res.Settings.ReasoningEffort)}
	if w := res.Worktree; w != nil {
		o.WorktreePath, o.WorktreeBranch = w.Path, w.Branch
	}
	if s.SkipPermissions {
		// The Controller keeps a Session's spawn options from its loads, not
		// from initialize_session: without this load, the worker a reload
		// spawns would ask for permissions. The Daemon keeps the live worker.
		lp := protocol.LoadSessionParams{SessionID: o.SessionID, SkipPermissionsUnsafe: &yes}
		if s.DisableInactivityTimeout {
			lp.DisableInactivityTimeout = &yes
		}
		if _, err := r.ctl.LoadSession(ctx, lp); err != nil {
			return o, err
		}
	}
	if s.Prompt != "" {
		if err := r.prompt(ctx, o.SessionID, s.Prompt); err != nil {
			return o, err
		}
	}
	return o, nil
}

// prompt sends a Session a user message under an id of the Runner's,
// which names the turn it starts when the Session is idle.
func (r *Runner) prompt(ctx context.Context, sessionID, text string) error {
	id := uuid.NewString()
	r.mu.Lock()
	if f := r.following[sessionID]; f != nil && !f.inTurn {
		f.turnID = id
	}
	r.mu.Unlock()
	_, err := r.ctl.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: sessionID, MessageID: id, Text: text})
	return err
}

// systemPrompt is the System Prompt Addition, then spec's, a blank line
// between; "" when both are blank.
func (r *Runner) systemPrompt(spec string) string {
	var parts []string
	if r.cfg.SystemPromptAddition != nil {
		if a := r.cfg.SystemPromptAddition(); strings.TrimSpace(a) != "" {
			parts = append(parts, a)
		}
	}
	if strings.TrimSpace(spec) != "" {
		parts = append(parts, spec)
	}
	return strings.Join(parts, "\n\n")
}

// Load follows a Session that exists, such as one the Host opened before it
// restarted. disabledTools, when given, are the disabledToolIds its worker
// gets, as Spec's DisabledTools: for a Session the Host did not open, such
// as one a Session of its was handed off to.
func (r *Runner) Load(ctx context.Context, sessionID string, disabledTools ...string) error {
	added := r.follow(sessionID, true)
	r.hide(sessionID, disabledTools)
	if _, err := r.ctl.LoadSession(ctx, protocol.LoadSessionParams{SessionID: sessionID, DisabledToolIDs: disabledTools}); err != nil {
		if added {
			r.follow(sessionID, false)
		}
		return err
	}
	if len(disabledTools) == 0 {
		return nil
	}
	// A load that finds the Session's worker running, as a Session just
	// handed off has, reuses it without handing it the load's
	// disabledToolIds (droid 1.4.1). Its settings reach that worker, which
	// saves them for the workers spawned later.
	return r.UpdateSettings(ctx, protocol.UpdateSessionSettingsParams{SessionID: sessionID, DisabledToolIDs: disabledTools})
}

// Send sends a Session a user message; one sent while it works joins the
// running turn. A Session the Runner does not follow is loaded first.
func (r *Runner) Send(ctx context.Context, sessionID, text string) error {
	if err := r.ensureFollowed(ctx, sessionID); err != nil {
		return err
	}
	return r.prompt(ctx, sessionID, text)
}

func (r *Runner) ensureFollowed(ctx context.Context, sessionID string) error {
	if r.Following(sessionID) {
		return nil
	}
	return r.Load(ctx, sessionID)
}

// Interrupt stops a Session's turn.
func (r *Runner) Interrupt(ctx context.Context, sessionID string) error {
	return r.ctl.InterruptSession(ctx, sessionID)
}

// idleWait bounds how long StopTurn waits for an interrupted turn to end.
const idleWait = 10 * time.Second

// StopTurn interrupts a Session's turn when it works and waits, up to
// idleWait, for the turn to end. A Session the Runner does not follow is
// loaded first, to know whether it works.
func (r *Runner) StopTurn(ctx context.Context, sessionID string) error {
	if err := r.ensureFollowed(ctx, sessionID); err != nil {
		return err
	}
	if !r.working(sessionID) {
		return nil
	}
	if err := r.Interrupt(ctx, sessionID); err != nil {
		return err
	}
	deadline := time.Now().Add(idleWait)
	for r.working(sessionID) && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return nil
}

// IsSessionNotFound reports an error of a Session the Daemon does not know,
// such as an archived one, which cannot be loaded again.
func IsSessionNotFound(err error) bool {
	var rpc *droid.RPCError
	return errors.Is(err, controller.ErrSessionNotFound) || errors.As(err, &rpc) && rpc.Code == codeEntityNotFound
}

// codeEntityNotFound is the Daemon's JSON-RPC code for an unknown entity.
const codeEntityNotFound = -32004

// Archive archives a Session, interrupting it first when it works: the
// Daemon refuses to archive a working Session. Archive then closes it,
// since an archived Session cannot be loaded again, and stops following it.
func (r *Runner) Archive(ctx context.Context, sessionID string) error {
	cl, err := r.ctl.Client()
	if err != nil {
		return err
	}
	if err := r.StopTurn(ctx, sessionID); err != nil {
		return err
	}
	if _, err := cl.ArchiveSession(ctx, protocol.ArchiveSessionParams{SessionID: sessionID}); err != nil {
		return err
	}
	if err := r.ctl.CloseSession(ctx, protocol.CloseSessionParams{SessionID: sessionID}); err != nil {
		r.log("close an archived Session: " + err.Error())
	}
	r.forget(sessionID)
	return nil
}

// Close closes a Session in the Daemon and stops following it; it stays
// listed, and Load follows it again.
func (r *Runner) Close(ctx context.Context, sessionID string) error {
	err := r.ctl.CloseSession(ctx, protocol.CloseSessionParams{SessionID: sessionID})
	r.forget(sessionID)
	return err
}

// Unfollow stops following a Session, which the Daemon keeps; Load follows
// it again.
func (r *Runner) Unfollow(sessionID string) { r.forget(sessionID) }

// UpdateSettings changes a Session's settings, such as its model or tags.
func (r *Runner) UpdateSettings(ctx context.Context, p protocol.UpdateSessionSettingsParams) error {
	cl, err := r.ctl.Client()
	if err != nil {
		return err
	}
	_, err = cl.UpdateSessionSettings(ctx, p)
	return err
}

// Client is the Runner's connection to the Daemon, for the calls the
// Runner does not wrap.
func (r *Runner) Client() (*droid.Client, error) { return r.ctl.Client() }

// Session is the state of a Session the Runner follows, nil for another.
func (r *Runner) Session(sessionID string) *session.Session {
	return r.ctl.Store().Session(sessionID)
}

// Pending counts the permission requests and the questions a Session waits
// on a Client to answer.
func (r *Runner) Pending(sessionID string) (permissions, questions int) {
	return len(r.ctl.PendingPermissions(sessionID)), len(r.ctl.PendingAskUsers(sessionID))
}

// Following reports that the Runner follows a Session.
func (r *Runner) Following(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.following[sessionID] != nil
}

// Subscribe calls fn with every Event, in order, on the Controller's event
// goroutine, which all subscribers share (a turn end the Runner asked the
// Daemon for, or gave up reloading for, comes on one of the Runner's own):
// fn may call the Runner, but a slow fn holds up every later event, so long
// work belongs on a goroutine of the subscriber's own. The returned
// function unsubscribes.
func (r *Runner) Subscribe(fn func(Event)) (unsubscribe func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.next
	r.next++
	r.subs[id] = fn
	return func() {
		r.mu.Lock()
		delete(r.subs, id)
		r.mu.Unlock()
	}
}

// follow starts or stops following a Session; it reports whether it
// started.
func (r *Runner) follow(sessionID string, on bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !on {
		delete(r.following, sessionID)
		delete(r.hidden, sessionID)
		return false
	}
	if r.following[sessionID] != nil {
		return false
	}
	r.following[sessionID] = &followed{}
	return true
}

// hide records the disabledToolIds a followed Session was opened or loaded
// with; none keeps those recorded before.
func (r *Runner) hide(sessionID string, disabledTools []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(disabledTools) > 0 && r.following[sessionID] != nil {
		r.hidden[sessionID] = slices.Clone(disabledTools)
	}
}

func (r *Runner) forget(sessionID string) {
	r.follow(sessionID, false)
	r.ctl.Forget(sessionID)
}

func (r *Runner) working(sessionID string) bool {
	s := r.Session(sessionID)
	return s != nil && s.WorkingState() != "" && s.WorkingState() != protocol.DroidWorkingStateIdle
}

// see turns the Controller's events into the Runner's, for the Sessions it
// follows. It runs on the Controller's event goroutine, so what it keeps of
// a turn follows the notifications in their order.
func (r *Runner) see(e controller.Event) {
	switch e := e.(type) {
	case controller.SessionNotification:
		r.publish(r.seeNotification(e))
	case controller.SessionLoaded:
		r.publish(r.seeLoaded(e.SessionID))
	case controller.SessionNotFound:
		out, f := r.seeGone(e.SessionID)
		r.publish(out)
		// Followed until its end is heard, so that no subscriber that asks
		// meanwhile takes it for a Session lost without one.
		r.mu.Lock()
		if f != nil && r.following[e.SessionID] == f {
			delete(r.following, e.SessionID)
			delete(r.hidden, e.SessionID)
		}
		r.mu.Unlock()
	}
}

// publish gives the subscribers out, unless nil.
func (r *Runner) publish(out Event) {
	if out == nil {
		return
	}
	r.mu.Lock()
	subs := make([]func(Event), 0, len(r.subs))
	for _, fn := range r.subs {
		subs = append(subs, fn)
	}
	r.mu.Unlock()
	for _, fn := range subs {
		fn(out)
	}
}

func (r *Runner) seeNotification(n controller.SessionNotification) Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.following[n.SessionID]
	if f == nil {
		return nil
	}
	switch v := n.Value.(type) {
	case *protocol.ErrorNotification:
		// An error between turns is not the next turn's.
		if f.inTurn && f.firstError == "" {
			f.firstError = v.Message
		}
	case *protocol.CreateMessageNotification:
		if text := textOf(v.Message.Content); text != "" {
			switch v.Message.Role {
			case protocol.MessageRoleAssistant:
				f.reply = text
			case protocol.MessageRoleUser:
				f.reply = ""
			}
		}
	case *protocol.DroidWorkingStateChangedNotification:
		if v.NewState != protocol.DroidWorkingStateIdle {
			f.inTurn = true
		}
		return WorkingChanged{SessionID: n.SessionID, State: v.NewState}
	case *protocol.AgentTurnCompletedNotification:
		return f.end(n.SessionID, v.Reason, f.firstError)
	case *protocol.SettingsUpdatedNotification:
		return SettingsUpdated{SessionID: n.SessionID}
	case *protocol.SessionProcessExitedNotification:
		// A worker kept for a prompt still owes the user an answer: the
		// store keeps the Session waiting, and the turn goes on after it.
		if f.inTurn && !r.working(n.SessionID) {
			return f.end(n.SessionID, protocol.AgentTurnCompletionReasonProcessExit, WorkerStopped)
		}
	}
	return nil
}

// seeLoaded ends a turn that a load, such as the reload after a Daemon
// restart, finds over without its end having come: as the Daemon recorded
// the turn of the Runner's prompt, which it asks for on a goroutine of its
// own, else with LostTouch.
func (r *Runner) seeLoaded(sessionID string) Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.following[sessionID]
	if f == nil {
		return nil
	}
	if r.working(sessionID) {
		f.inTurn = true
		return nil
	}
	switch {
	case !f.inTurn || f.asking:
		return nil
	case f.turnID != "":
		f.asking = true
		go r.askOutcome(sessionID, f, f.turnID)
		return nil
	}
	return f.end(sessionID, protocol.AgentTurnCompletionReasonProcessExit, LostTouch)
}

// askOutcome ends f's turn turnID as the Daemon recorded it, else with
// LostTouch, unless it ended meanwhile.
func (r *Runner) askOutcome(sessionID string, f *followed, turnID string) {
	reason, final, ok := r.turnOutcome(sessionID, turnID)
	r.mu.Lock()
	var out Event
	if r.following[sessionID] == f && f.inTurn && f.turnID == turnID {
		if !ok {
			out = f.end(sessionID, protocol.AgentTurnCompletionReasonProcessExit, LostTouch)
		} else {
			if final != "" {
				f.reply = final
			}
			out = f.end(sessionID, reason, f.firstError)
		}
	}
	r.mu.Unlock()
	r.publish(out)
}

// turnOutcome is the reason the Daemon recorded for a Session's turn, with
// its final output; not ok when it has no such record or cannot say.
func (r *Runner) turnOutcome(sessionID, turnID string) (reason protocol.AgentTurnCompletionReason, final string, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), outcomeWait+10*time.Second)
	defer cancel()
	ask := r.cfg.turnOutcome
	if ask == nil {
		cl, err := r.ctl.Client()
		if err != nil {
			return "", "", false
		}
		ask = cl.GetSessionTurnOutcome
	}
	res, err := ask(ctx, protocol.GetSessionTurnOutcomeParams{SessionID: sessionID, TurnID: turnID, TimeoutMs: float64(outcomeWait.Milliseconds())})
	if err != nil {
		r.log("the outcome of " + sessionID + ": " + err.Error())
		return "", "", false
	}
	v, err := res.Value()
	got, isAvailable := v.(*protocol.DaemonGetSessionTurnOutcomeResultAvailable)
	if err != nil || !isAvailable || got.TurnID != turnID {
		return "", "", false
	}
	var o struct {
		TurnID string                             `json:"turnId"`
		Reason protocol.AgentTurnCompletionReason `json:"reason"`
	}
	if json.Unmarshal(got.Outcome.Raw, &o) != nil || o.TurnID != turnID || o.Reason == "" {
		return "", "", false
	}
	return o.Reason, got.FinalOutput, true
}

// seeGone ends a followed Session the Daemon no longer has; see stops
// following it once the end is heard. It is the one place that ends a turn
// for a Session gone, and an Automation run's Session must not be
// unfollowed otherwise (an Unfollow): its end would never come.
func (r *Runner) seeGone(sessionID string) (Event, *followed) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.following[sessionID]
	if f == nil {
		return nil, nil
	}
	return f.end(sessionID, protocol.AgentTurnCompletionReasonProcessExit, SessionGone), f
}

// end is the TurnEnded of f's turn; the next turn starts afresh.
func (f *followed) end(sessionID string, reason protocol.AgentTurnCompletionReason, failure string) TurnEnded {
	t := TurnEnded{SessionID: sessionID, Reason: reason, Error: failure, LastReply: f.reply}
	*f = followed{}
	return t
}

// textOf is the text of a message's text blocks.
func textOf(blocks []protocol.ContentBlock) string {
	var text strings.Builder
	for _, b := range blocks {
		if v, err := b.Value(); err == nil {
			if tb, ok := v.(*protocol.TextBlock); ok {
				text.WriteString(tb.Text)
			}
		}
	}
	return text.String()
}
