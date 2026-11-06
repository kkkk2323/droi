package app

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/defaults"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
)

// newWorkstreamOf is what the Host is asked to start: the name, the folder and
// the symbol; the first message comes from the user in the orchestrator's
// page.
func newWorkstreamOf(name, workspace, symbol string) gateway.NewWorkstream {
	return gateway.NewWorkstream{Title: name, Workspace: workspace, Symbol: symbol}
}

// editWorkstream gives Workstream id a new name or symbol; nil leaves one as it
// is. A Host without edits only renames the orchestrator's Session.
func (a *App) editWorkstream(id string, title, symbol *string) {
	gw := a.cfg.Gateway
	if gw == nil || id == "" {
		return
	}
	if title != nil {
		trimmed := strings.TrimSpace(*title)
		if trimmed == "" {
			title = nil
		} else {
			title = &trimmed
		}
	}
	if title != nil {
		if orch := a.orchestratorSession(id); orch != "" {
			if sel := findSummary(a.listedNow(), orch); sel != nil && sel.Title == *title {
				title = nil
			}
		}
	}
	if title == nil && symbol == nil {
		return
	}
	go func() {
		err := gw.EditWorkstream(a.ctx, gateway.WorkstreamEdit{ID: id, Title: title, Symbol: symbol})
		if errors.Is(err, gateway.ErrNotFound) && title != nil {
			if orch := a.orchestratorSession(id); orch != "" {
				a.renameSession(orch, *title)
			}
			return
		}
		a.cfg.Update(func() {
			if err != nil {
				a.workstreams.noticeAt, a.workstreams.noticeErr, a.workstreams.notice = a.cfg.Now(), true, L("Could not save the workstream: %s", err)
				return
			}
			a.reloadList()
			a.loadWorkstreams(true)
		})
	}()
}

// agentInfo is what a transcript's rows say of the agent whose Session is id:
// its name and what it does or waits for. ok is false for a Session no
// Workstream lists as an agent.
func (a *App) agentInfo(id string) (sessionview.AgentInfo, bool) {
	for _, w := range a.workstreams.list {
		for _, ag := range w.Agents {
			if ag.SessionID != id && a.resolve(ag.SessionID) != a.resolve(id) {
				continue
			}
			group, how := a.agentStatus(ag)
			return sessionview.AgentInfo{
				Title:    ag.Title,
				Activity: a.agentActivity(a.resolve(ag.SessionID), group, how),
				Waiting:  group == "waiting",
			}, true
		}
	}
	return sessionview.AgentInfo{}, false
}

// How far a model is known to run Script.
const (
	scriptUnknown = iota
	scriptYes
	scriptNo
)

// scriptModels is what the Host said of the models that may use Script, and
// when it was asked.
type scriptModels struct {
	list   gateway.ScriptModelList
	have   bool
	at     time.Time
	asking bool
}

// scriptModelsFresh is how long the Host's list of Script models is kept.
const scriptModelsFresh = time.Minute

// loadScriptModels asks the Host which models may use Script, unless it did
// lately; a Host that cannot say leaves every model unknown.
func (a *App) loadScriptModels(force bool) {
	s := &a.workstreams.script
	gw := a.cfg.Gateway
	if gw == nil || s.asking || (!force && !s.at.IsZero() && a.cfg.Now().Sub(s.at) < scriptModelsFresh) {
		return
	}
	s.asking = true
	go func() {
		list, err := gw.ScriptModels(a.ctx)
		a.cfg.Update(func() {
			s.asking, s.at = false, a.cfg.Now()
			s.list, s.have = list, err == nil
		})
	}()
}

// scriptCapable is whether the Host runs tools of model as Script: the
// Host's list, and this computer's Settings when the window is on it. A
// custom model takes its base model's answer from the Daemon, so it is never
// said to be left out.
func (a *App) scriptCapable(model string) int {
	if !a.following() && slices.Contains(prefs.ScriptModels.Get(a.prefs), model) {
		return scriptYes
	}
	s := a.workstreams.script
	if !s.have {
		return scriptUnknown
	}
	allowed, known := s.list.Allows(model)
	switch {
	case allowed:
		return scriptYes
	case known && !strings.HasPrefix(model, "custom:"):
		return scriptNo
	}
	return scriptUnknown
}

// directMark is whether the row of an agent says it can use only Direct tools
// while this device would give it Script, and why.
func (a *App) directMark(ag gateway.WorkstreamAgent) (bool, string) {
	if ag.ToolMode != string(defaults.DirectOnly) || defaults.DefaultToolMode(a.prefs) == defaults.DirectOnly {
		return false, ""
	}
	switch {
	case ag.ScriptCapable != nil && !*ag.ScriptCapable:
		return true, L("This model is not in the Script models, so this agent can use only Direct tools. Add it in Settings, Script models.")
	case ag.ScriptCapable != nil:
		return true, L("Script is allowed for this model now. This agent switches after its model changes; new agents use it.")
	}
	return true, L("This agent can use only Direct tools.")
}
