package app

import (
	"slices"
	"strings"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// startingAgent is an agent the Host is still starting: its Session is not
// there yet.
type startingAgent struct{ Title, StartedAt string }

// startingOf are the agents the Host says it is starting.
func startingOf(*gateway.Workstream) []startingAgent { return nil }

// lastActivityOf is when the Host last saw anything of the agent, RFC 3339.
func lastActivityOf(gateway.WorkstreamAgent) string { return "" }

// newWorkstreamOf is what the Host is asked to start: the name, the folder and
// the symbol; the first message comes from the user in the orchestrator's
// page.
func newWorkstreamOf(name, workspace, symbol string) gateway.NewWorkstream {
	return gateway.NewWorkstream{Title: name, Workspace: workspace}
}

// renameWorkstream gives Workstream id a new name.
func (a *App) renameWorkstream(id, title string) {
	title = strings.TrimSpace(title)
	orch := a.orchestratorSession(id)
	if orch == "" || title == "" {
		return
	}
	if sel := findSummary(a.listedNow(), orch); sel != nil && sel.Title == title {
		return
	}
	go a.renameSession(orch, title)
}

// How far a model is known to run Script.
const (
	scriptUnknown = iota
	scriptYes
	scriptNo
)

// scriptCapable is whether the Host runs tools of model as Script: Settings'
// list of models, and Factory's, which the Host reads from the Daemon.
func (a *App) scriptCapable(model string) int {
	if slices.Contains(prefs.ScriptModels.Get(a.prefs), model) {
		return scriptYes
	}
	return scriptUnknown
}
