package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// Permission is a pending request of the agent to run tools.
type Permission struct {
	RequestID string
	// SessionID is the Session whose worker asked.
	SessionID string
	// AssociatedSessionIDs are the Sessions that show the prompt: SessionID
	// and, for a subagent, the Sessions above it.
	AssociatedSessionIDs []string
	ToolUses             []protocol.ToolConfirmationInfo
	Options              []protocol.ToolConfirmationListItem
	// Restored: the prompt came with daemon.load_session, not as a request.
	Restored   bool
	ReceivedAt time.Time
}

// AskUser is a pending set of questions of the agent.
type AskUser struct {
	RequestID  string
	SessionID  string
	ToolCallID string
	Questions  []protocol.AskUserQuestion
	Restored   bool
	ReceivedAt time.Time
}

// PermissionAnswer answers a Permission.
type PermissionAnswer struct {
	SelectedOption    protocol.ToolConfirmationOutcome
	Comment           string
	EditedSpecContent string
}

// prompts is the Controller's prompt state, guarded by Controller.mu.
type prompts struct {
	perms map[string]*Permission
	asks  map[string]*AskUser
	// Answers given while their Session's worker was gone, replayed when the
	// Daemon asks again: permissions by tool use id, AskUser answers by
	// Session and tool call id.
	bufPerm map[string]bufferedPermission
	bufAsk  map[string]map[string]protocol.AskUserResult
	// inactive Sessions have no live worker: answers wait for a load.
	inactive map[string]bool
	// concurrent Sessions have shown more than one permission at once; one
	// being answered does not mean the rest are stale.
	concurrent map[string]bool
}

type bufferedPermission struct {
	sessionID string
	answer    PermissionAnswer
}

func newPrompts() prompts {
	return prompts{
		perms:      map[string]*Permission{},
		asks:       map[string]*AskUser{},
		bufPerm:    map[string]bufferedPermission{},
		bufAsk:     map[string]map[string]protocol.AskUserResult{},
		inactive:   map[string]bool{},
		concurrent: map[string]bool{},
	}
}

// reply is an answer to send once Controller.mu is released.
type reply struct {
	id     string
	result any
}

func firstToolUseID(tu []protocol.ToolConfirmationInfo) string {
	if len(tu) == 0 {
		return ""
	}
	return tu[0].ToolUse.ID
}

func permissionResponse(sessionID string, a PermissionAnswer) protocol.RequestPermissionResponse {
	return protocol.RequestPermissionResponse{
		SessionID:         sessionID,
		SelectedOption:    a.SelectedOption,
		Comment:           a.Comment,
		EditedSpecContent: a.EditedSpecContent,
	}
}

func askUserResponse(sessionID string, r protocol.AskUserResult) protocol.AskUserResponse {
	if r.Answers == nil {
		r.Answers = []protocol.AskUserCollectedAnswer{}
	}
	return protocol.AskUserResponse{SessionID: sessionID, Cancelled: r.Cancelled, Answers: r.Answers}
}

func (p *prompts) sessionPermissions(sessionID string) []*Permission {
	var out []*Permission
	for _, perm := range p.perms {
		if slices.Contains(perm.AssociatedSessionIDs, sessionID) {
			out = append(out, perm)
		}
	}
	return out
}

func (p *prompts) sessionAsks(sessionID string) []*AskUser {
	var out []*AskUser
	for _, a := range p.asks {
		if a.SessionID == sessionID {
			out = append(out, a)
		}
	}
	return out
}

func (p *prompts) hasPending(sessionID string) bool {
	return len(p.sessionPermissions(sessionID)) > 0 || len(p.sessionAsks(sessionID)) > 0
}

// addPermission records a permission; a repeat of a pending one only
// updates it.
func (p *prompts) addPermission(perm *Permission, out []Event) []Event {
	if old, ok := p.perms[perm.RequestID]; ok {
		for _, id := range old.AssociatedSessionIDs {
			if !slices.Contains(perm.AssociatedSessionIDs, id) {
				perm.AssociatedSessionIDs = append(perm.AssociatedSessionIDs, id)
			}
		}
		p.perms[perm.RequestID] = perm
		return out
	}
	for id, old := range p.perms {
		for _, tu := range perm.ToolUses {
			if slices.ContainsFunc(old.ToolUses, func(o protocol.ToolConfirmationInfo) bool { return o.ToolUse.ID == tu.ToolUse.ID }) {
				out = p.dropPermission(id, "", out)
				break
			}
		}
	}
	p.perms[perm.RequestID] = perm
	for _, s := range perm.AssociatedSessionIDs {
		if pending := p.sessionPermissions(s); len(pending) > 1 {
			for _, q := range pending {
				for _, a := range q.AssociatedSessionIDs {
					p.concurrent[a] = true
				}
			}
		}
	}
	return append(out, PermissionRequested{Permission: *perm})
}

// dropPermission removes a pending permission, answered with option (empty:
// dropped unanswered).
func (p *prompts) dropPermission(id string, option protocol.ToolConfirmationOutcome, out []Event) []Event {
	perm, ok := p.perms[id]
	if !ok {
		return out
	}
	delete(p.perms, id)
	for _, s := range perm.AssociatedSessionIDs {
		if p.concurrent[s] && !p.hasPending(s) {
			delete(p.concurrent, s)
		}
	}
	return append(out, PermissionResolved{RequestID: id, SessionID: perm.SessionID, SelectedOption: option})
}

func (p *prompts) addAskUser(a *AskUser, out []Event) []Event {
	if _, ok := p.asks[a.RequestID]; ok {
		p.asks[a.RequestID] = a
		return out
	}
	for id, old := range p.asks {
		if old.SessionID == a.SessionID && old.ToolCallID == a.ToolCallID {
			out = p.dropAskUser(id, nil, out)
		}
	}
	p.asks[a.RequestID] = a
	return append(out, AskUserRequested{AskUser: *a})
}

func (p *prompts) dropAskUser(id string, result *protocol.AskUserResult, out []Event) []Event {
	a, ok := p.asks[id]
	if !ok {
		return out
	}
	delete(p.asks, id)
	return append(out, AskUserResolved{RequestID: id, SessionID: a.SessionID, Result: result})
}

// clearSession drops a Session's prompts; with keepRelayed, permissions
// also shown on other Sessions stay.
func (p *prompts) clearSession(sessionID string, keepRelayed bool, out []Event) []Event {
	for id, perm := range p.perms {
		if perm.SessionID != sessionID {
			continue
		}
		if keepRelayed && slices.ContainsFunc(perm.AssociatedSessionIDs, func(s string) bool { return s != sessionID }) {
			continue
		}
		out = p.dropPermission(id, "", out)
	}
	for _, a := range p.sessionAsks(sessionID) {
		out = p.dropAskUser(a.RequestID, nil, out)
	}
	delete(p.concurrent, sessionID)
	return out
}

func (p *prompts) forgetSession(sessionID string, out []Event) []Event {
	out = p.clearSession(sessionID, false, out)
	for id, b := range p.bufPerm {
		if b.sessionID == sessionID {
			delete(p.bufPerm, id)
		}
	}
	delete(p.bufAsk, sessionID)
	delete(p.inactive, sessionID)
	return out
}

// markPromptSessionsInactive marks the Sessions with pending prompts
// inactive, as their connection is gone.
func (p *prompts) markPromptSessionsInactive() {
	for _, perm := range p.perms {
		p.inactive[perm.SessionID] = true
	}
	for _, a := range p.asks {
		p.inactive[a.SessionID] = true
	}
}

// clearStale drops the prompt of a Session whose agent moved on without it
// being answered here. With several prompts out at once, one being answered
// says nothing of the others, so they stay.
func (p *prompts) clearStale(sessionID string, out []Event) []Event {
	if p.inactive[sessionID] {
		return out
	}
	perms, asks := len(p.sessionPermissions(sessionID)), len(p.sessionAsks(sessionID))
	switch {
	case perms == 0 && asks == 0:
		delete(p.concurrent, sessionID)
	case perms > 1 || asks > 1:
		p.concurrent[sessionID] = true
	case p.concurrent[sessionID]:
	default:
		out = p.clearSession(sessionID, true, out)
	}
	return out
}

// restore replaces a Session's prompts with those daemon.load_session
// returned, answering at once the ones answered while the Session was
// inactive.
func (p *prompts) restore(sessionID string, res *protocol.LoadSessionResult, assoc func(string, []string) []string, out []Event) ([]Event, []reply) {
	var replies []reply
	keep := map[string]bool{}
	for _, pp := range res.PendingPermissions {
		keep[pp.RequestID] = true
	}
	for _, pa := range res.PendingAskUserRequests {
		keep[pa.RequestID] = true
	}
	for id, perm := range p.perms {
		if perm.SessionID == sessionID && !keep[id] && !slices.ContainsFunc(perm.AssociatedSessionIDs, func(s string) bool { return s != sessionID }) {
			out = p.dropPermission(id, "", out)
		}
	}
	for _, a := range p.sessionAsks(sessionID) {
		if !keep[a.RequestID] {
			out = p.dropAskUser(a.RequestID, nil, out)
		}
	}
	now := time.Now()
	for _, pp := range res.PendingPermissions {
		if b, ok := p.bufPerm[firstToolUseID(pp.ToolUses)]; ok && b.sessionID == sessionID {
			delete(p.bufPerm, firstToolUseID(pp.ToolUses))
			out = p.dropPermission(pp.RequestID, b.answer.SelectedOption, out)
			replies = append(replies, reply{pp.RequestID, permissionResponse(sessionID, b.answer)})
			continue
		}
		out = p.addPermission(&Permission{
			RequestID:            pp.RequestID,
			SessionID:            sessionID,
			AssociatedSessionIDs: assoc(sessionID, slices.Concat(pp.AssociatedSessionIDs, []string{res.CallingSessionID})),
			ToolUses:             pp.ToolUses,
			Options:              pp.Options,
			Restored:             true,
			ReceivedAt:           now,
		}, out)
	}
	for _, pa := range res.PendingAskUserRequests {
		if r, ok := p.bufAsk[sessionID][pa.ToolCallID]; ok {
			delete(p.bufAsk[sessionID], pa.ToolCallID)
			out = p.dropAskUser(pa.RequestID, &r, out)
			replies = append(replies, reply{pa.RequestID, askUserResponse(sessionID, r)})
			continue
		}
		out = p.addAskUser(&AskUser{
			RequestID:  pa.RequestID,
			SessionID:  sessionID,
			ToolCallID: pa.ToolCallID,
			Questions:  pa.Questions,
			Restored:   true,
			ReceivedAt: now,
		}, out)
	}
	delete(p.inactive, sessionID)
	return out, replies
}

// associated returns sessionID, extra and the Sessions above sessionID,
// without repeats or empty ids.
func (c *Controller) associated(sessionID string, extra []string) []string {
	out := []string{sessionID}
	add := func(s string) bool {
		if s == "" || slices.Contains(out, s) {
			return false
		}
		out = append(out, s)
		return true
	}
	for _, s := range extra {
		add(s)
	}
	for cur, depth := sessionID, 0; depth < 10; depth++ {
		var parent string
		if s := c.store.Session(cur); s != nil {
			parent, _ = s.CallingSession()
		}
		if !add(parent) {
			break
		}
		cur = parent
	}
	return out
}

func (c *Controller) onPermissionRequest(gen int, r droid.Request) {
	cl := c.current(gen)
	if cl == nil {
		return
	}
	var p protocol.RequestPermissionParams
	if err := json.Unmarshal(r.Params, &p); err != nil {
		c.log.Warn("droid: undecodable permission request", "err", err)
		return
	}
	var out []Event
	var replies []reply
	c.mu.Lock()
	if b, ok := c.prompts.bufPerm[firstToolUseID(p.ToolUses)]; ok && b.sessionID == p.SessionID {
		delete(c.prompts.bufPerm, firstToolUseID(p.ToolUses))
		replies = append(replies, reply{r.ID, permissionResponse(p.SessionID, b.answer)})
	} else {
		out = c.prompts.addPermission(&Permission{
			RequestID:            r.ID,
			SessionID:            p.SessionID,
			AssociatedSessionIDs: c.associated(p.SessionID, p.AssociatedSessionIDs),
			ToolUses:             p.ToolUses,
			Options:              p.Options,
			ReceivedAt:           time.Now(),
		}, out)
	}
	c.mu.Unlock()
	c.push(out)
	c.sendReplies(cl, replies)
}

func (c *Controller) onAskUserRequest(gen int, r droid.Request) {
	cl := c.current(gen)
	if cl == nil {
		return
	}
	var p protocol.AskUserParams
	if err := json.Unmarshal(r.Params, &p); err != nil {
		c.log.Warn("droid: undecodable AskUser request", "err", err)
		return
	}
	var out []Event
	var replies []reply
	c.mu.Lock()
	if res, ok := c.prompts.bufAsk[p.SessionID][p.ToolCallID]; ok {
		delete(c.prompts.bufAsk[p.SessionID], p.ToolCallID)
		replies = append(replies, reply{r.ID, askUserResponse(p.SessionID, res)})
	} else {
		out = c.prompts.addAskUser(&AskUser{
			RequestID:  r.ID,
			SessionID:  p.SessionID,
			ToolCallID: p.ToolCallID,
			Questions:  p.Questions,
			ReceivedAt: time.Now(),
		}, out)
	}
	c.mu.Unlock()
	if len(replies) == 0 {
		if s := c.store.Session(p.SessionID); s != nil {
			s.SetWorkingState(protocol.DroidWorkingStateWaitingForToolConfirmation)
		}
	}
	c.push(out)
	c.sendReplies(cl, replies)
}

func (c *Controller) push(out []Event) {
	for _, e := range out {
		c.events.push(e)
	}
}

// sendReplies sends answers off the read goroutine, which must not block.
func (c *Controller) sendReplies(cl *droid.Client, replies []reply) {
	if len(replies) == 0 {
		return
	}
	go func() {
		for _, r := range replies {
			if err := cl.Respond(context.Background(), r.id, r.result); err != nil {
				c.log.Warn("droid: replay prompt answer", "requestId", r.id, "err", err)
			}
		}
	}()
}

// PendingPermissions returns the pending permissions shown on a Session
// (all of them for ""), oldest first.
func (c *Controller) PendingPermissions(sessionID string) []Permission {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Permission
	for _, p := range c.prompts.perms {
		if sessionID == "" || slices.Contains(p.AssociatedSessionIDs, sessionID) {
			out = append(out, *p)
		}
	}
	slices.SortFunc(out, func(a, b Permission) int { return a.ReceivedAt.Compare(b.ReceivedAt) })
	return out
}

// PendingAskUsers returns the pending AskUser prompts of a Session (all of
// them for ""), oldest first.
func (c *Controller) PendingAskUsers(sessionID string) []AskUser {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []AskUser
	for _, a := range c.prompts.asks {
		if sessionID == "" || a.SessionID == sessionID {
			out = append(out, *a)
		}
	}
	slices.SortFunc(out, func(a, b AskUser) int { return a.ReceivedAt.Compare(b.ReceivedAt) })
	return out
}

// RespondToPermission answers a pending permission. When the Session's
// worker is gone (inactive, or the connection is down) the answer is kept
// and sent when the Session is loaded again, which the Controller then does.
func (c *Controller) RespondToPermission(ctx context.Context, requestID string, a PermissionAnswer) error {
	c.mu.Lock()
	p, ok := c.prompts.perms[requestID]
	if !ok {
		c.mu.Unlock()
		return ErrPromptNotFound
	}
	if !slices.ContainsFunc(p.Options, func(o protocol.ToolConfirmationListItem) bool { return o.Value == a.SelectedOption }) {
		c.mu.Unlock()
		return fmt.Errorf("droid: option %q is not one of the permission's options", a.SelectedOption)
	}
	sessionID, toolUseID := p.SessionID, firstToolUseID(p.ToolUses)
	cl := c.client
	if cl == nil || c.prompts.inactive[sessionID] {
		out := c.bufferPermission(requestID, sessionID, toolUseID, a)
		c.mu.Unlock()
		c.push(out)
		if cl != nil {
			c.selfResume(sessionID)
		}
		return nil
	}
	c.mu.Unlock()
	if err := cl.Respond(ctx, requestID, permissionResponse(sessionID, a)); err != nil {
		if cl.Err() == nil {
			return err
		}
		c.mu.Lock()
		out := c.bufferPermission(requestID, sessionID, toolUseID, a)
		c.mu.Unlock()
		c.push(out)
		return nil
	}
	c.mu.Lock()
	out := c.prompts.dropPermission(requestID, a.SelectedOption, nil)
	c.mu.Unlock()
	c.push(out)
	return nil
}

func (c *Controller) bufferPermission(requestID, sessionID, toolUseID string, a PermissionAnswer) []Event {
	if toolUseID != "" {
		c.prompts.bufPerm[toolUseID] = bufferedPermission{sessionID: sessionID, answer: a}
	} else {
		c.log.Warn("droid: permission without tool use cannot wait for its Session", "requestId", requestID)
	}
	return c.prompts.dropPermission(requestID, a.SelectedOption, nil)
}

// RespondToAskUser answers a pending AskUser prompt: every question once,
// or Cancelled. Like RespondToPermission it keeps the answer for a Session
// whose worker is gone.
func (c *Controller) RespondToAskUser(ctx context.Context, requestID string, r protocol.AskUserResult) error {
	c.mu.Lock()
	a, ok := c.prompts.asks[requestID]
	if !ok {
		c.mu.Unlock()
		return ErrPromptNotFound
	}
	if err := checkAnswers(a, r); err != nil {
		c.mu.Unlock()
		return err
	}
	sessionID, toolCallID := a.SessionID, a.ToolCallID
	buffer := func() []Event {
		if c.prompts.bufAsk[sessionID] == nil {
			c.prompts.bufAsk[sessionID] = map[string]protocol.AskUserResult{}
		}
		c.prompts.bufAsk[sessionID][toolCallID] = r
		return c.prompts.dropAskUser(requestID, &r, nil)
	}
	cl := c.client
	if cl == nil || c.prompts.inactive[sessionID] {
		out := buffer()
		c.mu.Unlock()
		c.push(out)
		if cl != nil {
			c.selfResume(sessionID)
		}
		return nil
	}
	c.mu.Unlock()
	if err := cl.Respond(ctx, requestID, askUserResponse(sessionID, r)); err != nil {
		if cl.Err() == nil {
			return err
		}
		c.mu.Lock()
		out := buffer()
		c.mu.Unlock()
		c.push(out)
		return nil
	}
	c.mu.Lock()
	out := c.prompts.dropAskUser(requestID, &r, nil)
	c.mu.Unlock()
	c.push(out)
	return nil
}

func checkAnswers(a *AskUser, r protocol.AskUserResult) error {
	if r.Cancelled != nil && *r.Cancelled {
		return nil
	}
	seen := map[float64]bool{}
	for _, ans := range r.Answers {
		if !slices.ContainsFunc(a.Questions, func(q protocol.AskUserQuestion) bool { return q.Index == ans.Index }) {
			return fmt.Errorf("droid: answer to unknown question %v", ans.Index)
		}
		if seen[ans.Index] {
			return fmt.Errorf("droid: two answers to question %v", ans.Index)
		}
		seen[ans.Index] = true
	}
	for _, q := range a.Questions {
		if !seen[q.Index] {
			return fmt.Errorf("droid: question %v not answered", q.Index)
		}
	}
	return nil
}
