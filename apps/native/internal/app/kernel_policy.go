package app

import (
	"context"
	"encoding/json"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/defaults"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/models"
	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
)

// The window's half of the approval policy (design/m3-kernel.md §2): Kernel holds a copy of it and runs nothing
// above "ask" until the first policy.set, so every connection is given it at once. The default level is the
// Session defaults' autonomy; a conversation's own level, and the rules its "always allow" answers added, the Host
// stores (host.ConversationPolicy) from what policy.changed says.

// kernelLevels are the Kernel's levels for the Session defaults' (design/m3-kernel.md §2.2).
var kernelLevels = map[string]string{"off": "ask", "low": "edits", "medium": "safe", "high": "all"}

// kernelLevel is a Session defaults' level in the Kernel's words. One it does not know, "" included, is "ask":
// the level that asks most.
func kernelLevel(level string) string {
	if l, ok := kernelLevels[level]; ok {
		return l
	}
	return "ask"
}

// sessionLevel is a Kernel level in the Session defaults' words, for the picker.
func sessionLevel(level string) string {
	for session, k := range kernelLevels {
		if k == level {
			return session
		}
	}
	return "off"
}

// defaultsWait is how long a new connection waits for the Session defaults before it gives Kernel "ask": the
// defaults come from the Daemon, which may still be starting. Their arrival pushes the level they name.
const defaultsWait = 5 * time.Second

// policyState is what the window knows of the policy. Guarded by mu; push is held across each default-level
// policy.set, so that the last one sent carries the latest level.
type policyState struct {
	mu sync.Mutex
	// own are the conversations' own policies of a window without a Host; with one, the Host's are read each
	// time, since every window of it writes them.
	own map[kernel.ID]host.ConversationPolicy
	// level is the default level gen's connection was given, "" before it was given one.
	level string
	gen   int64
	// agent is the default agent agentGen's connection was given, as sent; "" before it was given one.
	agent    string
	agentGen int64

	push sync.Mutex
}

// ownPolicies are the conversations' own policies; the caller must hold a.policy.mu.
func (a *App) ownPoliciesLocked() map[kernel.ID]host.ConversationPolicy {
	if a.cfg.Host != nil {
		return a.cfg.Host.ConversationPolicies()
	}
	if a.policy.own == nil {
		a.policy.own = map[kernel.ID]host.ConversationPolicy{}
	}
	return a.policy.own
}

// storeOwn changes what conversation id has of its own, and has the Host store it.
func (a *App) storeOwn(id kernel.ID, change func(*host.ConversationPolicy)) {
	a.policy.mu.Lock()
	own := a.ownPoliciesLocked()
	p := own[id]
	p.Rules = slices.Clone(p.Rules)
	change(&p)
	if a.cfg.Host == nil {
		if p.Autonomy == "" && len(p.Rules) == 0 {
			delete(own, id)
		} else {
			own[id] = p
		}
	}
	a.policy.mu.Unlock()
	if a.cfg.Host != nil {
		if err := a.cfg.Host.SetConversationPolicy(id, p); err != nil {
			log.Println("droi: kernel: storing a conversation's approval policy:", err)
		}
	}
	a.redraw()
}

// pushPolicy gives connection gen the policy: the default level, then each conversation's own.
func (a *App) pushPolicy(c *kernel.Client, gen int64) {
	deadline := time.Now().Add(defaultsWait)
	for a.cfg.Controller != nil && a.defaults.Get() == nil && time.Now().Before(deadline) {
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !a.pushDefault(c, gen) {
		return
	}
	a.policy.mu.Lock()
	own := make(map[kernel.ID]host.ConversationPolicy, len(a.ownPoliciesLocked()))
	for id, p := range a.ownPoliciesLocked() {
		own[id] = p
	}
	a.policy.mu.Unlock()
	for id, p := range own {
		rules := p.Rules
		if rules == nil {
			rules = []kernel.PolicyRule{}
		}
		_, err := c.PolicySet(a.ctx, kernel.PolicySetParams{ID: &id, Policy: kernel.PolicyChange{Autonomy: p.Autonomy, Rules: &rules}})
		switch {
		case kernel.Is(err, kernel.CodeConversationGone):
			// Deleted while the window was away: nothing is left to keep its policy for.
			a.storeOwn(id, func(p *host.ConversationPolicy) { *p = host.ConversationPolicy{} })
		case err != nil:
			log.Printf("droi: kernel: the approval policy of conversation %d: %v", id, err)
		}
	}
}

// pushDefault gives connection gen the default level the Session defaults name now, unless it has it already, and
// reports whether Kernel took it (or had it). Without defaults it is "ask", until they arrive.
func (a *App) pushDefault(c *kernel.Client, gen int64) bool {
	a.policy.push.Lock()
	defer a.policy.push.Unlock()
	level := "ask"
	if dv := a.defaults.Get(); dv != nil {
		level = kernelLevel(dv.AutonomyLevel)
	}
	a.policy.mu.Lock()
	same := a.policy.gen == gen && a.policy.level == level
	a.policy.mu.Unlock()
	if same {
		return true
	}
	none := []kernel.PolicyRule{}
	if _, err := c.PolicySet(a.ctx, kernel.PolicySetParams{Policy: kernel.PolicyChange{Autonomy: level, Rules: &none}}); err != nil {
		// A read-only Kernel runs no tool, and one without approvals runs every tool: neither takes a policy.
		if !kernel.Is(err, kernel.CodeReadOnly) && !kernel.Is(err, kernel.CodeUnsupported) {
			log.Println("droi: kernel: the default approval policy:", err)
		}
		return false
	}
	a.policy.mu.Lock()
	a.policy.level, a.policy.gen = level, gen
	a.policy.mu.Unlock()
	return true
}

// defaultsChanged is the Session defaults' Changed: a new default level and model go to Kernel.
func (a *App) defaultsChanged(defaults.View) {
	a.kernel.mu.Lock()
	c, gen := a.kernel.c, a.kernel.gen
	a.kernel.mu.Unlock()
	if c != nil {
		go a.pushDefault(c, gen)
		go a.pushDefaultAgent(c, gen)
	}
}

// pushDefaultAgent gives connection gen the model the Session defaults name, so a conversation another client makes
// without naming one (a phone's) starts as this window's would. It waits for the defaults and for the models Kernel
// offers, whose arrival pushes it again; a model Kernel does not offer is not given.
func (a *App) pushDefaultAgent(c *kernel.Client, gen int64) {
	dv := a.defaults.Get()
	if dv == nil || dv.ModelID == "" {
		return
	}
	offered := a.kernel.offered()
	i := slices.IndexFunc(offered, func(m kernel.ModelInfo) bool { return m.ID == dv.ModelID })
	if i < 0 {
		return
	}
	agent := kernel.DefaultAgent{Model: kernel.ModelRef{Provider: offered[i].Provider, ModelID: dv.ModelID}}
	if slices.Contains(offered[i].Thinking, dv.ReasoningEffort) {
		agent.ThinkingLevel = dv.ReasoningEffort
	}
	sent, _ := json.Marshal(agent)
	a.policy.push.Lock()
	defer a.policy.push.Unlock()
	a.policy.mu.Lock()
	same := a.policy.agentGen == gen && a.policy.agent == string(sent)
	a.policy.mu.Unlock()
	if same {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	if err := c.HarnessConfigure(ctx, kernel.HarnessConfigureParams{Agent: kernel.Some(agent)}); err != nil {
		if !kernel.Is(err, kernel.CodeReadOnly) && !kernel.Is(err, kernel.CodeUnsupported) {
			log.Println("droi: kernel: the default model:", err)
		}
		return
	}
	a.policy.mu.Lock()
	a.policy.agent, a.policy.agentGen = string(sent), gen
	a.policy.mu.Unlock()
}

// policyChanged takes policy.changed. Only a conversation's rules are stored from it: its level in the snapshot is
// the default's when it has none of its own, and storing that would hold it there when the default changes. The
// default's own is the Session defaults', which the window pushed.
func (a *App) policyChanged(n kernel.PolicyChanged) {
	if n.ID == nil {
		return
	}
	a.storeOwn(*n.ID, func(p *host.ConversationPolicy) { p.Rules = slices.Clone(n.Policy.Rules) })
}

// conversationLevel is the level conversation conv's calls are judged at, in the Kernel's words.
func (a *App) conversationLevel(conv kernel.ID) string {
	a.policy.mu.Lock()
	own := a.ownPoliciesLocked()[conv].Autonomy
	a.policy.mu.Unlock()
	if own != "" {
		return own
	}
	if dv := a.defaults.Get(); dv != nil {
		return kernelLevel(dv.AutonomyLevel)
	}
	return "ask"
}

// setConversationLevel gives conversation conv a level of its own.
func (a *App) setConversationLevel(conv kernel.ID, level string) error {
	c, err := a.kernel.client()
	if err != nil {
		return err
	}
	if _, err := c.PolicySet(a.ctx, kernel.PolicySetParams{ID: &conv, Policy: kernel.PolicyChange{Autonomy: level}}); err != nil {
		return err
	}
	a.storeOwn(conv, func(p *host.ConversationPolicy) { p.Autonomy = level })
	return nil
}

// kernelAutonomy is a Kernel conversation's autonomy picker: the composer's, with the Session defaults' levels.
func (v *sessionView) kernelAutonomy(c *ui.Context, conv kernel.ID) {
	a := v.a
	opts := make([]kit.Option, len(autonomyLevels))
	for i, l := range autonomyLevels {
		opts[i] = kit.Option{Value: l, Label: l10n.T(models.AutonomyLabels[l])}
	}
	pick := a.kit.QuietSelect
	if v.narrowBar {
		pick = a.kit.QuietIconSelect
	}
	if next, ok := pick(c, L("Autonomy"), "shield-check", sessionLevel(a.conversationLevel(conv)), opts); ok {
		go func() {
			if err := a.setConversationLevel(conv, kernelLevel(next)); err != nil {
				v.Fail(err)
			}
		}()
	}
}

// heardApprovals makes a conversation's mark and alert follow the cards its state holds, for a conversation the
// window has open: an open lists cards no approval.requested on this connection announced (a reconnect, a
// conversation opened while it waits), and a reopen drops the ones that no longer wait.
func (a *App) heardApprovals(conv kernel.ID) {
	state := a.sessions.State(conv)
	if state == nil {
		return
	}
	id := a.sessionIDOf(conv)
	shown := map[string]bool{}
	for _, p := range append(state.Approvals(), sessionview.BlockingAsks(state.Asks())...) {
		shown[p.RequestID] = true
	}
	a.mu.Lock()
	var add, drop []string
	for requestID := range shown {
		if !a.approvals[id][requestID] {
			add = append(add, requestID)
		}
	}
	for requestID := range a.approvals[id] {
		if !shown[requestID] {
			drop = append(drop, requestID)
		}
	}
	a.mu.Unlock()
	for _, requestID := range add {
		a.awaitApproval(conv, requestID, true)
	}
	for _, requestID := range drop {
		a.awaitApproval(conv, requestID, false)
	}
}

// awaitApproval follows the approvals each conversation waits on, for its mark in the list and its alert: asked
// adds requestID, and an answer takes it away.
func (a *App) awaitApproval(conv kernel.ID, requestID string, asked bool) {
	id := a.sessionIDOf(conv)
	waiting := string(protocol.DroidWorkingStateWaitingForToolConfirmation)
	a.mu.Lock()
	if a.approvals == nil {
		a.approvals = map[string]map[string]bool{}
	}
	reqs := a.approvals[id]
	if asked {
		if reqs == nil {
			reqs = map[string]bool{}
			a.approvals[id] = reqs
		}
		reqs[requestID] = true
	} else {
		delete(reqs, requestID)
	}
	state := ""
	switch {
	case len(reqs) > 0:
		state = waiting
	case a.busy[id] == waiting:
		state = string(protocol.DroidWorkingStateStreamingAssistantMessage)
	}
	if len(reqs) == 0 {
		delete(a.approvals, id)
	}
	if state != "" {
		a.busy[id] = state
	}
	a.mu.Unlock()
	if state == "" {
		return
	}
	a.redraw()
	if ev := a.kernelAlerts.Update(id, state); ev != "" {
		a.cfg.Update(func() { a.raise(id, "", ev) })
	}
}
