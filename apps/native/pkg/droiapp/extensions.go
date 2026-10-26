package droiapp

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/extension"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/icons"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
)

// extensions are the app's Extensions, joined for the Host, the window and
// the command line.
type extensions struct {
	list         []extension.Extension
	pages        []extension.Page
	settingsTabs []extension.SettingsTab
	// signIn is the first extension's sign-in page, nil when none has one.
	signIn func(*ui.Context, extension.Env)
}

func newExtensions(list []extension.Extension) *extensions {
	x := &extensions{list: list}
	for _, e := range list {
		x.pages = append(x.pages, e.Pages...)
		x.settingsTabs = append(x.settingsTabs, e.SettingsTabs...)
		if x.signIn == nil {
			x.signIn = e.SignIn
		}
	}
	return x
}

// register adds the extensions' icons, words, tool views and json-render
// components, before anything is drawn.
func (x *extensions) register() {
	for _, e := range x.list {
		for name, svg := range e.Icons {
			icons.Register(name, svg)
		}
		if len(e.Chinese) > 0 {
			l10n.Add(e.Chinese)
		}
		for name, v := range e.ToolViews {
			sessionview.RegisterToolView(name, v)
		}
		for typ, draw := range e.Components {
			sessionview.RegisterComponent(typ, draw)
		}
	}
}

// command is the extensions' subcommand name, nil when none has it.
func (x *extensions) command(name string) func(args []string) int {
	for _, e := range x.list {
		if cmd := e.Commands[name]; cmd != nil {
			return cmd
		}
	}
	return nil
}

// overlay is what the extensions add to h's Runtime Overlay, the first's
// name kept where two give the same.
func (x *extensions) overlay(h *host.Host) host.OverlayOptions {
	var o host.OverlayOptions
	for _, e := range x.list {
		if e.Overlay != nil {
			o = o.Add(e.Overlay(h))
		}
	}
	return o
}

// systemPrompt is the extensions' additions to the System Prompt Addition,
// a blank line between them.
func (x *extensions) systemPrompt() string {
	var parts []string
	for _, e := range x.list {
		if e.SystemPrompt == nil {
			continue
		}
		if p := strings.TrimSpace(e.SystemPrompt()); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\n\n")
}
