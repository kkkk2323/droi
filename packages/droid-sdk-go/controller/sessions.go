package controller

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/session"
)

const (
	initializeAttempts       = 2
	initializeAttemptTimeout = time.Minute
	selfResumeAttempts       = 3
	selfResumeDelay          = 500 * time.Millisecond
	selfResumeMaxDelay       = 4 * time.Second
)

// tracked is a Session the Controller loaded or created.
type tracked struct {
	// params are the spawn options the Session was loaded with, reused
	// when the Controller loads it again.
	params     protocol.LoadSessionParams
	loaded     bool
	everLoaded bool
	// reload: load again once reconnected.
	reload  bool
	loading *loadCall
}

type loadCall struct {
	done chan struct{}
	// res is nil for daemon.initialize_session.
	res *protocol.LoadSessionResult
	err error
}

// Methods that do not need a live worker, so a call does not first load
// the Session (the TS SDK's SKIP_ENSURE_LOADED).
var skipEnsureLoaded = map[string]bool{
	protocol.MethodLoadSession:            true,
	protocol.MethodInitializeSession:      true,
	protocol.MethodCloseSession:           true,
	protocol.MethodInterruptSession:       true,
	"daemon.archive_session":              true,
	"daemon.unarchive_session":            true,
	"daemon.rename_session":               true,
	"daemon.create_terminal":              true,
	"daemon.write_terminal_data":          true,
	"daemon.resize_terminal":              true,
	"daemon.close_terminal":               true,
	"daemon.list_terminals":               true,
	"daemon.list_available_plugins":       true,
	"daemon.list_installed_plugins":       true,
	"daemon.install_plugin":               true,
	"daemon.uninstall_plugin":             true,
	"daemon.set_plugin_enabled":           true,
	"daemon.update_plugin":                true,
	"daemon.list_marketplaces":            true,
	"daemon.add_marketplace":              true,
	"daemon.remove_marketplace":           true,
	"daemon.update_marketplace":           true,
	"daemon.get_automation_visual":        true,
	"daemon.get_workspace_file_content":   true,
	"daemon.write_workspace_file_content": true,
	"daemon.list_crons":                   true,
	"daemon.create_cron":                  true,
	"daemon.update_cron":                  true,
	"daemon.delete_cron":                  true,
	"daemon.hold_session_crons":           true,
	"daemon.resume_session_crons":         true,
}

// beforeCall loads, before a session-scoped call, a Session the Controller
// loaded before and that is not loaded now (its worker timed out, or the
// connection was replaced).
func (c *Controller) beforeCall(ctx context.Context, method, sessionID string) error {
	if sessionID == "" || skipEnsureLoaded[method] {
		return nil
	}
	c.mu.Lock()
	t := c.sessions[sessionID]
	if t == nil || t.loaded || !t.everLoaded {
		c.mu.Unlock()
		return nil
	}
	lc, params := t.loading, t.params
	c.mu.Unlock()
	if lc != nil {
		return lc.wait(ctx)
	}
	_, err := c.LoadSession(ctx, params)
	return err
}

func (lc *loadCall) wait(ctx context.Context) error {
	select {
	case <-lc.done:
		return lc.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Controller) track(sessionID string) *tracked {
	t := c.sessions[sessionID]
	if t == nil {
		t = &tracked{params: protocol.LoadSessionParams{SessionID: sessionID}}
		c.sessions[sessionID] = t
	}
	return t
}

func spawnToken(cred *droid.Credential) string {
	if cred == nil {
		return ""
	}
	if cred.Token != "" {
		return cred.Token
	}
	return cred.APIKey
}

// stash keeps the spawn options of a load for the next one, as the TS SDK's
// DaemonLoadSessionSpawnOptions.
func stash(p protocol.LoadSessionParams) protocol.LoadSessionParams {
	return protocol.LoadSessionParams{
		SessionID:                p.SessionID,
		DisableInactivityTimeout: p.DisableInactivityTimeout,
		DisableBuiltinSkills:     p.DisableBuiltinSkills,
		SkipPermissionsUnsafe:    p.SkipPermissionsUnsafe,
		RuntimeSettingsPath:      p.RuntimeSettingsPath,
		StructuredOutputFormat:   p.StructuredOutputFormat,
		TaskSubagentProcess:      p.TaskSubagentProcess,
	}
}

func withStashed(p, s protocol.LoadSessionParams) protocol.LoadSessionParams {
	if p.DisableInactivityTimeout == nil {
		p.DisableInactivityTimeout = s.DisableInactivityTimeout
	}
	if p.DisableBuiltinSkills == nil {
		p.DisableBuiltinSkills = s.DisableBuiltinSkills
	}
	if p.SkipPermissionsUnsafe == nil {
		p.SkipPermissionsUnsafe = s.SkipPermissionsUnsafe
	}
	if p.RuntimeSettingsPath == "" {
		p.RuntimeSettingsPath = s.RuntimeSettingsPath
	}
	if p.StructuredOutputFormat == nil {
		p.StructuredOutputFormat = s.StructuredOutputFormat
	}
	if p.TaskSubagentProcess == nil {
		p.TaskSubagentProcess = s.TaskSubagentProcess
	}
	return p
}

// LoadSession loads a Session (daemon.load_session): the Controller then
// follows its notifications, restores its pending prompts, and loads it
// again after a reconnect. A load of a Session already being loaded waits
// for that one. Spawn options left unset keep those of the last load.
func (c *Controller) LoadSession(ctx context.Context, p protocol.LoadSessionParams) (*protocol.LoadSessionResult, error) {
	for {
		c.mu.Lock()
		t := c.track(p.SessionID)
		if lc := t.loading; lc != nil {
			c.mu.Unlock()
			if err := lc.wait(ctx); err != nil || lc.res != nil {
				return lc.res, err
			}
			continue
		}
		p = withStashed(p, t.params)
		lc := &loadCall{done: make(chan struct{})}
		t.loading = lc
		cl, cred, gen := c.client, c.cred, c.gen
		c.mu.Unlock()

		lc.res, lc.err = c.load(ctx, cl, cred, gen, t, p)
		close(lc.done)
		return lc.res, lc.err
	}
}

func (c *Controller) load(ctx context.Context, cl *droid.Client, cred *droid.Credential, gen int, t *tracked, p protocol.LoadSessionParams) (*protocol.LoadSessionResult, error) {
	sessionID := p.SessionID
	tok := c.store.BeginLoad(sessionID)
	fail := func(err error) (*protocol.LoadSessionResult, error) {
		c.mu.Lock()
		t.loading = nil
		if !t.everLoaded && c.sessions[sessionID] == t {
			delete(c.sessions, sessionID)
		}
		c.mu.Unlock()
		c.store.MarkNotLoaded(sessionID)
		return nil, err
	}
	if cl == nil {
		return fail(ErrNotConnected)
	}
	p.Token = spawnToken(cred)
	if p.LoadAllMessages == nil {
		p.LoadAllMessages = ptr(true)
	}
	if p.MessageLimit == nil && c.cfg.DefaultMessageLimit > 0 {
		p.MessageLimit = ptr(c.cfg.DefaultMessageLimit)
	}
	res, err := cl.LoadSession(ctx, p)
	if err != nil {
		var rpcErr *droid.RPCError
		if errors.As(err, &rpcErr) && rpcErr.Code == codeEntityNotFound {
			c.mu.Lock()
			t.loading = nil
			delete(c.sessions, sessionID)
			c.mu.Unlock()
			c.store.MarkNotFound(sessionID)
			c.events.push(SessionNotFound{SessionID: sessionID})
			return nil, ErrSessionNotFound
		}
		return fail(err)
	}
	c.mu.Lock()
	t.loading = nil
	t.params = stash(p)
	t.everLoaded = true
	if c.gen != gen {
		// The connection it subscribed went away meanwhile.
		t.reload = true
		c.mu.Unlock()
		c.store.MarkNotLoaded(sessionID)
		return res, nil
	}
	t.loaded, t.reload = true, false
	c.sessions[sessionID] = t
	out, replies := c.prompts.restore(sessionID, res, c.associated, nil)
	pending := c.prompts.hasPending(sessionID)
	c.mu.Unlock()
	var limit int
	if p.MessageLimit != nil {
		limit = int(*p.MessageLimit)
	}
	outcome := c.store.ApplyLoadResult(tok, res, limit)
	c.store.MarkActive(sessionID)
	ws := outcome.ReportedWorkingState
	if pending {
		ws = protocol.DroidWorkingStateWaitingForToolConfirmation
	}
	c.store.ApplyLoadedWorkingState(tok, ws)
	c.push(out)
	c.events.push(SessionLoaded{SessionID: sessionID, Result: res})
	c.sendReplies(cl, replies)
	if len(outcome.Resubmit) > 0 {
		go c.resubmit(cl, sessionID, outcome.Resubmit)
	}
	return res, nil
}

// resubmit sends again the queued messages a load found the Daemon lost;
// those it cannot send stay queued, paused.
func (c *Controller) resubmit(cl *droid.Client, sessionID string, msgs []session.QueuedMessage) {
	ctx, cancel := c.stopContext()
	defer cancel()
	for i, q := range msgs {
		if _, err := cl.AddUserMessage(droid.WithRequestID(ctx, q.RequestID), session.ResubmitParams(q)); err != nil {
			c.log.Warn("droid: resend queued message", "sessionId", sessionID, "err", err)
			if s := c.store.Session(sessionID); s != nil {
				rest := msgs[i:]
				for j := range rest {
					rest[j].Kind = session.KindLocalPausedAfterEsc
				}
				s.RestoreQueuedMessagesToFront(rest)
			}
			return
		}
	}
}

func ptr[T any](v T) *T { return &v }

// InitializeSession creates a Session (daemon.initialize_session), which
// the Controller then follows as one it loaded. MachineID, SessionID and
// Token are filled in when empty; a timed-out attempt is tried once more
// with the same SessionID, which the Daemon dedupes.
func (c *Controller) InitializeSession(ctx context.Context, p protocol.InitializeSessionParams) (*protocol.InitializeSessionResult, error) {
	if p.MachineID == "" {
		p.MachineID = c.cfg.MachineID
	}
	if p.SessionID == "" {
		p.SessionID = uuid.NewString()
	}
	c.mu.Lock()
	cl, cred, gen := c.client, c.cred, c.gen
	t := c.track(p.SessionID)
	lc := &loadCall{done: make(chan struct{})}
	t.loading = lc
	c.mu.Unlock()
	res, err := c.initialize(ctx, cl, cred, p)
	c.mu.Lock()
	t.loading = nil
	if err != nil {
		if !t.everLoaded && c.sessions[p.SessionID] == t {
			delete(c.sessions, p.SessionID)
		}
		c.mu.Unlock()
		lc.err = err
		close(lc.done)
		return nil, err
	}
	sessionID := res.SessionID
	if sessionID != p.SessionID {
		delete(c.sessions, p.SessionID)
	}
	t.params = stash(protocol.LoadSessionParams{
		SessionID:                sessionID,
		DisableInactivityTimeout: p.DisableInactivityTimeout,
		DisableBuiltinSkills:     p.DisableBuiltinSkills,
		RuntimeSettingsPath:      p.RuntimeSettingsPath,
		StructuredOutputFormat:   p.StructuredOutputFormat,
	})
	t.everLoaded = true
	t.loaded = c.gen == gen
	t.reload = !t.loaded
	c.sessions[sessionID] = t
	c.mu.Unlock()
	cwd := p.Cwd
	if p.Worktree != nil && *p.Worktree && res.Worktree != nil && res.Worktree.Path != "" {
		cwd = res.Worktree.Path
	}
	c.store.ApplyInitializeResult(res, cwd, p.Tags)
	close(lc.done)
	c.events.push(SessionInitialized{SessionID: sessionID, Result: res})
	return res, nil
}

func (c *Controller) initialize(ctx context.Context, cl *droid.Client, cred *droid.Credential, p protocol.InitializeSessionParams) (*protocol.InitializeSessionResult, error) {
	if cl == nil {
		return nil, ErrNotConnected
	}
	p.Token = spawnToken(cred)
	var err error
	for attempt := 1; attempt <= initializeAttempts; attempt++ {
		actx, cancel := context.WithTimeout(ctx, initializeAttemptTimeout)
		var res *protocol.InitializeSessionResult
		res, err = cl.InitializeSession(actx, p)
		cancel()
		var te *droid.TimeoutError
		if err == nil || !errors.As(err, &te) || ctx.Err() != nil {
			return res, err
		}
		c.log.Warn("droid: initialize_session timed out", "sessionId", p.SessionID, "attempt", attempt)
	}
	return nil, err
}

// selfResume loads a Session whose worker is gone after an answer to one
// of its prompts was kept, so that the Daemon asks again and gets it.
func (c *Controller) selfResume(sessionID string) {
	c.mu.Lock()
	if c.selfResuming[sessionID] {
		c.mu.Unlock()
		return
	}
	c.selfResuming[sessionID] = true
	params := protocol.LoadSessionParams{SessionID: sessionID}
	if t := c.sessions[sessionID]; t != nil {
		params = t.params
	}
	c.mu.Unlock()
	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.selfResuming, sessionID)
			c.mu.Unlock()
		}()
		ctx, cancel := c.stopContext()
		defer cancel()
		delay := selfResumeDelay
		for attempt := 1; attempt <= selfResumeAttempts; attempt++ {
			_, err := c.LoadSession(ctx, params)
			if err == nil || errors.Is(err, ErrSessionNotFound) {
				return
			}
			c.log.Warn("droid: resume after answer failed", "sessionId", sessionID, "attempt", attempt, "err", err)
			if attempt < selfResumeAttempts && c.sleep(ctx, delay) != nil {
				return
			}
			delay = min(delay*2, selfResumeMaxDelay)
		}
	}()
}

// stopContext is a context that ends with the Controller.
func (c *Controller) stopContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-c.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// reloadSessions loads again, after a reconnect, the Sessions that were
// loaded on the lost connection, so their notifications flow again.
func (c *Controller) reloadSessions() {
	c.mu.Lock()
	var todo []protocol.LoadSessionParams
	for _, t := range c.sessions {
		if t.reload {
			todo = append(todo, t.params)
		}
	}
	c.mu.Unlock()
	for _, p := range todo {
		go func() {
			ctx, cancel := c.stopContext()
			defer cancel()
			if _, err := c.LoadSession(ctx, p); err != nil {
				c.log.Warn("droid: reload after reconnect failed", "sessionId", p.SessionID, "err", err)
			}
		}()
	}
}

// applyNotification applies a session notification to the prompts and the
// state; c.mu is held.
func (c *Controller) applyNotification(sessionID string, v any, p protocol.SessionNotificationParams, out []Event) []Event {
	switch v := v.(type) {
	case *protocol.PermissionResolvedNotification:
		ids := v.ToolUseIDs
		if perm := c.prompts.perms[v.RequestID]; perm != nil {
			ids = nil
			for _, tu := range perm.ToolUses {
				ids = append(ids, tu.ToolUse.ID)
			}
		}
		for _, id := range ids {
			delete(c.prompts.bufPerm, id)
		}
		out = c.prompts.dropPermission(v.RequestID, v.SelectedOption, out)
	case *protocol.SessionInactivityNotification, *protocol.SessionProcessExitedNotification:
		c.prompts.inactive[sessionID] = true
	case *protocol.SessionClosedNotification:
		out = c.prompts.forgetSession(sessionID, out)
		delete(c.sessions, sessionID)
	case *protocol.DroidWorkingStateChangedNotification:
		if v.NewState != protocol.DroidWorkingStateWaitingForToolConfirmation {
			out = c.prompts.clearStale(sessionID, out)
		}
	case *protocol.ChildSessionAvailableNotification:
		if t := c.sessions[v.ChildSessionID]; t == nil {
			go c.hydrateChild(v.ChildSessionID)
		}
	}
	return append(out, SessionNotification{SessionID: sessionID, Value: v, Raw: p.Notification})
}

// hydrateChild loads a subagent's Session so that its progress shows.
func (c *Controller) hydrateChild(sessionID string) {
	ctx, cancel := c.stopContext()
	defer cancel()
	if _, err := c.LoadSession(ctx, protocol.LoadSessionParams{SessionID: sessionID}); err != nil {
		c.log.Warn("droid: load subagent session", "sessionId", sessionID, "err", err)
	}
}

// AddUserMessage sends a message to a Session. When the agent is busy, the
// Daemon holds the message; the state then shows it as queued until it
// starts.
func (c *Controller) AddUserMessage(ctx context.Context, p protocol.AddUserMessageParams) (*protocol.AddUserMessageResult, error) {
	cl, err := c.Client()
	if err != nil {
		return nil, err
	}
	// A caller's id is kept: an optimistic submit waits for the
	// create_message that echoes it.
	id := droid.RequestIDOf(ctx)
	if id == "" {
		id = uuid.NewString()
		ctx = droid.WithRequestID(ctx, id)
	}
	if p.SkipAgentLoop == nil || !*p.SkipAgentLoop {
		// On the read goroutine, so the notifications after the response
		// are not yet applied, as in the TS SDK's event loop.
		ctx = droid.WithResponseHook(ctx, func(result json.RawMessage) { c.afterAddUserMessage(p, id, result) })
	}
	return cl.AddUserMessage(ctx, p)
}

// afterAddUserMessage shows a message the Daemon took as streaming when the
// agent was idle, else as queued. An acknowledged one is done when its
// create_message arrives, which the store applies.
func (c *Controller) afterAddUserMessage(p protocol.AddUserMessageParams, id string, result json.RawMessage) {
	var ack struct {
		Accepted bool `json:"accepted"`
	}
	if json.Unmarshal(result, &ack) == nil && ack.Accepted {
		return
	}
	if s := c.store.Session(p.SessionID); s != nil {
		if p.QueuePlacement != protocol.QueuePlacementEndOfLoop && s.WorkingState() == protocol.DroidWorkingStateIdle {
			s.SetWorkingState(protocol.DroidWorkingStateStreamingAssistantMessage)
		} else {
			// The store drops it once its create_message arrives, whichever
			// comes first.
			s.QueueUserMessages(session.QueuedMessage{
				RequestID: id,
				Content:   session.UserContent(p.Text, p.Content, p.Images, p.Files),
				Kind:      session.KindForPlacement(p.QueuePlacement),
				CreatedAt: time.Now(),
			})
		}
	}
}

// InterruptSession stops a Session's agent; its pending prompts are
// dropped (AskUser ones answered as cancelled) and the message being
// streamed is discarded.
func (c *Controller) InterruptSession(ctx context.Context, sessionID string) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	if _, err := cl.InterruptSession(ctx, protocol.InterruptSessionParams{SessionID: sessionID}); err != nil {
		return err
	}
	c.mu.Lock()
	var out []Event
	for id, perm := range c.prompts.perms {
		if perm.SessionID == sessionID {
			out = c.prompts.dropPermission(id, "", out)
		}
	}
	for _, a := range c.prompts.sessionAsks(sessionID) {
		out = c.prompts.dropAskUser(a.RequestID, &protocol.AskUserResult{Cancelled: ptr(true), Answers: []protocol.AskUserCollectedAnswer{}}, out)
	}
	c.mu.Unlock()
	if s := c.store.Session(sessionID); s != nil {
		s.PauseDaemonQueuedMessagesAfterEsc("")
	}
	c.push(out)
	return nil
}

// Forget drops what this Client holds of a Session it no longer shows: its
// state in the Store, and its reload after a reconnect. The Daemon keeps
// the Session, and LoadSession reads it again. A Session being loaded is
// kept.
func (c *Controller) Forget(sessionID string) {
	c.mu.Lock()
	t := c.sessions[sessionID]
	if t != nil && t.loading != nil {
		c.mu.Unlock()
		return
	}
	delete(c.sessions, sessionID)
	c.mu.Unlock()
	c.store.RemoveSession(sessionID)
}

// CloseSession closes a Session; the Controller stops following it.
func (c *Controller) CloseSession(ctx context.Context, p protocol.CloseSessionParams) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	if _, err := cl.CloseSession(ctx, p); err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.sessions, p.SessionID)
	c.mu.Unlock()
	return nil
}
