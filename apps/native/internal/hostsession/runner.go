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
	// SystemPromptAddition, optional, is added after Droid's system prompt
	// of every Session the Runner opens, as the Gateway adds it to a
	// Client's (host.Host.SystemPromptAddition).
	SystemPromptAddition func() string
	// OnChange, optional, hears of a change that may let a failed connect
	// succeed, such as a sign-in or a restarted Daemon (host.Host.OnChange).
	OnChange func(fn func()) (unsubscribe func())
	// Log, optional, hears what went wrong.
	Log func(string)
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

// Event is what Subscribe hears: TurnEnded or WorkingChanged, of a Session
// the Runner follows.
type Event interface{ hostSessionEvent() }

// TurnEnded is an agent turn's end.
type TurnEnded struct {
	SessionID string
	Reason    protocol.AgentTurnCompletionReason
	// Error is the turn's first error notification, "" when it had none.
	Error string
	// LastReply is the text of the Session's last assistant message.
	LastReply string
}

// WorkingChanged is a Session's new working state.
type WorkingChanged struct {
	SessionID string
	State     protocol.DroidWorkingState
}

func (TurnEnded) hostSessionEvent()      {}
func (WorkingChanged) hostSessionEvent() {}

// Runner starts and follows the Host's Sessions; safe for use from any
// goroutine.
type Runner struct {
	cfg Config
	ctl *controller.Controller

	mu        sync.Mutex
	following map[string]bool
	// firstError is the first error notification of each Session's turn.
	firstError map[string]string
	subs       map[int]func(Event)
	next       int
}

// New makes a Runner; Start connects it.
func New(cfg Config) *Runner {
	r := &Runner{cfg: cfg, following: map[string]bool{}, firstError: map[string]string{}, subs: map[int]func(Event){}}
	r.ctl = controller.New(controller.Config{
		ResolveURL: cfg.ResolveURL,
		Credential: cfg.Credential,
		Caller:     "droi-native",
		// Start keeps trying, with its own backoff.
		MaxConnectAttempts: 1,
		// The Daemon restarts on its own (the Host's Supervisor); keep trying.
		MaxReconnectAttempts: 1000,
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
	var delay time.Duration
	for ctx.Err() == nil {
		st := r.ctl.Status()
		if !st.Connected && !st.Reconnecting {
			if err := r.ctl.Connect(ctx); err != nil && ctx.Err() == nil {
				r.log("connect: " + err.Error())
				delay = retryDelay(delay)
			} else {
				delay = 0
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
	}
	if s.DisableInactivityTimeout {
		p.DisableInactivityTimeout = &yes
	}
	// Followed before it starts, so no notification of its first turn is
	// missed.
	r.follow(p.SessionID, true)
	res, err := r.ctl.InitializeSession(ctx, p)
	if err != nil {
		r.follow(p.SessionID, false)
		return Opened{}, err
	}
	if res.SessionID != p.SessionID {
		r.follow(p.SessionID, false)
		r.follow(res.SessionID, true)
	}
	o := Opened{SessionID: res.SessionID, ModelID: res.Settings.ModelID, ReasoningEffort: string(res.Settings.ReasoningEffort)}
	if w := res.Worktree; w != nil {
		o.WorktreePath, o.WorktreeBranch = w.Path, w.Branch
	}
	if s.Prompt != "" {
		if _, err := r.ctl.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: o.SessionID, Text: s.Prompt}); err != nil {
			return o, err
		}
	}
	return o, nil
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
// restarted.
func (r *Runner) Load(ctx context.Context, sessionID string) error {
	r.follow(sessionID, true)
	if _, err := r.ctl.LoadSession(ctx, protocol.LoadSessionParams{SessionID: sessionID}); err != nil {
		r.follow(sessionID, false)
		return err
	}
	return nil
}

// Send sends a Session a user message; one sent while it works joins the
// running turn. A Session the Runner does not follow is loaded first.
func (r *Runner) Send(ctx context.Context, sessionID, text string) error {
	if !r.Following(sessionID) {
		if err := r.Load(ctx, sessionID); err != nil {
			return err
		}
	}
	_, err := r.ctl.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: sessionID, Text: text})
	return err
}

// Interrupt stops a Session's turn.
func (r *Runner) Interrupt(ctx context.Context, sessionID string) error {
	return r.ctl.InterruptSession(ctx, sessionID)
}

// idleWait bounds how long Archive waits for an interrupted turn to end.
const idleWait = 10 * time.Second

// Archive archives a Session, interrupting it first when it works: the
// Daemon refuses to archive a working Session. It then closes it, since an
// archived Session cannot be loaded again, and stops following it.
func (r *Runner) Archive(ctx context.Context, sessionID string) error {
	cl, err := r.ctl.Client()
	if err != nil {
		return err
	}
	if r.working(sessionID) {
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
	return r.following[sessionID]
}

// Subscribe calls fn with every Event, in order, on a goroutine of the
// Runner's own; fn may call the Runner. The returned function unsubscribes.
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

func (r *Runner) follow(sessionID string, on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if on {
		r.following[sessionID] = true
	} else {
		delete(r.following, sessionID)
		delete(r.firstError, sessionID)
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
// follows.
func (r *Runner) see(e controller.Event) {
	n, ok := e.(controller.SessionNotification)
	if !ok {
		return
	}
	r.mu.Lock()
	if !r.following[n.SessionID] {
		r.mu.Unlock()
		return
	}
	var out Event
	switch v := n.Value.(type) {
	case *protocol.ErrorNotification:
		if r.firstError[n.SessionID] == "" {
			r.firstError[n.SessionID] = v.Message
		}
	case *protocol.DroidWorkingStateChangedNotification:
		out = WorkingChanged{SessionID: n.SessionID, State: v.NewState}
	case *protocol.AgentTurnCompletedNotification:
		out = TurnEnded{SessionID: n.SessionID, Reason: v.Reason, Error: r.firstError[n.SessionID]}
		delete(r.firstError, n.SessionID)
	}
	subs := make([]func(Event), 0, len(r.subs))
	for _, fn := range r.subs {
		subs = append(subs, fn)
	}
	r.mu.Unlock()
	if out == nil {
		return
	}
	if t, ok := out.(TurnEnded); ok {
		// The store took the turn's messages before the Controller sent this.
		t.LastReply = lastReply(r.Session(n.SessionID))
		out = t
	}
	for _, fn := range subs {
		fn(out)
	}
}

// lastReply is the text of a Session's last assistant message that has any.
func lastReply(s *session.Session) string {
	if s == nil {
		return ""
	}
	msgs := s.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role != protocol.MessageRoleAssistant {
			continue
		}
		var text strings.Builder
		for _, b := range m.Content {
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
