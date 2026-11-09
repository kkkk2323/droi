package app

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/v2/internal/slash"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/host"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/l10n"
)

// The Model Sources in the window (ADR 0044): how each stands, and what gives the Kernel models when none has any.

// kernelMode reports that this window's Sessions are the Kernel's: it has a connection, or its Host runs a Kernel
// and is about to hand it one. The Daemon's sign-in and accounts are then not what runs a model.
func (a *App) kernelMode() bool {
	if a.onKernel() {
		return true
	}
	h := a.cfg.Host
	return h != nil && !a.following() && h.KernelEnabled()
}

// slashBuiltins are the builtins a composer offers: on the Kernel without /loop, a timer only the Daemon keeps.
func (a *App) slashBuiltins(items []slash.Item) []slash.Item {
	if !a.kernelMode() {
		return items
	}
	return slices.DeleteFunc(slices.Clone(items), func(i slash.Item) bool { return i.Name == "loop" })
}

// noKernelModels says why a Kernel without models starts nothing, and for each Model Source of this computer what
// gives it some.
func (a *App) noKernelModels() string {
	var fixes []string
	if h := a.cfg.Host; h != nil && !a.following() {
		for _, s := range h.KernelModelSources() {
			if fix := sourceFix(h, s); fix != "" {
				fixes = append(fixes, fix)
			}
		}
	}
	if len(fixes) == 0 {
		fixes = []string{L("Add a Factory key on Settings → Usage, or turn on Magpie in Settings → Advanced.")}
	}
	return L("Droi has no models to run yet.") + " " + strings.Join(fixes, " ")
}

// sourceFix is what gives a source that offers nothing its models, "" when there is nothing to do. Factory is listed
// first, so Magpie's is the other way.
func sourceFix(h *host.Host, s host.ModelSourceStatus) string {
	switch s.ID {
	case host.SourceFactory:
		switch s.State {
		case host.SourceEmpty:
			return L("Add a Factory key on Settings → Usage.")
		case host.SourceUnreachable:
			return L("The Key Pool did not start: %s.", strings.TrimSuffix(s.Err, "."))
		}
	case host.SourceMagpie:
		switch s.State {
		case host.SourceOff:
			return L("Or turn on Magpie in Settings → Advanced.")
		case host.SourceUnreachable:
			return L("Magpie does not answer at %s: start it, then refresh in Settings → Advanced.", h.MagpieBaseURL())
		case host.SourceEmpty:
			return L("Magpie lists no models.")
		}
	}
	return ""
}

// sourceRow is one Model Source in Settings: its pill and what it needs.
func sourceRow(h *host.Host, s host.ModelSourceStatus) (title string, ok bool, pill, desc string) {
	models := countL(s.Models, l10n.N("%d model"), l10n.N("%d models"))
	switch s.ID {
	case host.SourceFactory:
		title = L("Factory")
		switch s.State {
		case host.SourceReady:
			return title, true, models, L("Through the Key Pool's keys.")
		case host.SourceEmpty:
			return title, false, L("No key"), L("Add a Factory key on the Usage tab.")
		default:
			return title, false, L("Not running"), L("The Key Pool did not start: %s.", strings.TrimSuffix(s.Err, "."))
		}
	case host.SourceMagpie:
		title = L("Magpie")
		switch s.State {
		case host.SourceReady:
			return title, true, models, L("From the gateway at %s.", h.MagpieBaseURL())
		case host.SourceOff:
			return title, false, L("Off"), L("Turn on Magpie below to offer its models.")
		case host.SourceEmpty:
			return title, false, L("No models"), L("The gateway at %s lists no models.", h.MagpieBaseURL())
		default:
			desc := L("Nothing answers at %s. Start Magpie, then refresh below.", h.MagpieBaseURL())
			if s.Models > 0 {
				desc += " " + L("The models it listed before stay offered.")
			}
			return title, false, L("Not reachable"), desc
		}
	}
	return s.ID, s.State == host.SourceReady, models, ""
}

// modelSourcesGroup lists the Model Sources of the running Kernel in Settings → Advanced.
func (a *App) modelSourcesGroup(c *ui.Context) {
	h := a.cfg.Host
	sources := h.KernelModelSources()
	if len(sources) == 0 {
		return
	}
	rows := make([]func(), 0, len(sources))
	for _, s := range sources {
		rows = append(rows, func() {
			title, ok, pill, desc := sourceRow(h, s)
			a.settingRow(c, title, desc, func() { a.statusPill(c, ok, pill) }, nil)
		})
	}
	a.settingGroup(c, L("Model sources"), L("Where Droi's models come from. New Sessions can use the models of every source that has some."), rows...)
}
