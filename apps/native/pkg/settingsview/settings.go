// Package settingsview draws the parts of Settings both Droi and another
// app built on these packages show: the General section's appearance (the
// language, theme, font and text size), the Session defaults the Daemon
// keeps, and the model picker they use.
package settingsview

import (
	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/defaults"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/theme"
)

// Env is what the sections need from their app.
type Env struct {
	// Kit is what to draw with, read as a section is built.
	Kit func() *kit.Kit
	// Prefs keeps the appearance and the default tool mode, on this device.
	Prefs *prefs.Store
	// Update runs fn on the UI thread and draws; it is called from any
	// goroutine.
	Update func(fn func())
	// Defaults are the Session Defaults the section reads and saves.
	Defaults *Defaults
	// Failed shows why a save failed, on the UI thread; nil shows nothing.
	Failed func(msg string)
	// PickFolder asks for a folder off the UI thread, "" for none. Nil
	// leaves the spec folder without its Custom… choice, as for another
	// computer's Daemon, which cannot use a folder picked here.
	PickFolder func(title string) string
}

// Settings is the sections' state, kept from frame to frame: which selects
// are open, and the model picker's.
type Settings struct {
	env    Env
	open   map[string]*bool
	picker PickerState
}

// New makes the sections' state.
func New(env Env) *Settings {
	if env.Update == nil {
		env.Update = func(fn func()) { fn() }
	}
	return &Settings{env: env, open: map[string]*bool{}}
}

// Flag is the open state kept under key, for the app's own selects and
// pickers on the same page too.
func (s *Settings) Flag(key string) *bool {
	if s.open[key] == nil {
		s.open[key] = new(bool)
	}
	return s.open[key]
}

// Picker is the model picker's state, which the page's pickers share: one
// is open at a time.
func (s *Settings) Picker() *PickerState { return &s.picker }

// Select is a settings row's Select.
func (s *Settings) Select(c *ui.Context, label, value string, options []kit.Option, disabled bool) (string, bool) {
	return s.env.Kit().FieldSelect(c, label, value, options, disabled)
}

// SaveDefaults shows dv patched with p at once and has the Daemon save p.
func (s *Settings) SaveDefaults(dv defaults.View, p defaults.Patch) {
	s.env.Defaults.Save(dv, p, func(err error) {
		if s.env.Failed != nil {
			s.env.Failed(l10n.L("Session defaults did not load or save: %s", err))
		}
	})
}

// General is the General section's Appearance group: the language, theme,
// font, text size and density, kept in Env.Prefs. apply is called once one changed,
// to draw with it.
func (s *Settings) General(c *ui.Context, apply func()) {
	k, store := s.env.Kit(), s.env.Prefs
	themes := make([]kit.Option, len(theme.Names))
	for i, n := range theme.Names {
		themes[i] = kit.Option{Value: string(n), Label: theme.Labels[n]}
	}
	fonts := []kit.Option{{Value: string(theme.FontSystem), Label: l10n.L("System")}, {Value: string(theme.FontGeist), Label: "Geist"}}
	sizes := []kit.Option{{Value: "small", Label: l10n.L("Small")}, {Value: "default", Label: l10n.L("Default")}, {Value: "large", Label: l10n.L("Large")}, {Value: "largest", Label: l10n.L("Largest")}}
	pick := func(p prefs.String, label string, opts []kit.Option) func() {
		return func() {
			if next, ok := s.Select(c, label, p.Get(store), opts, false); ok {
				p.Set(store, next)
				if apply != nil {
					apply()
				}
			}
		}
	}
	densities := []kit.Option{{Value: string(theme.DensityComfortable), Label: l10n.L("Comfortable")}, {Value: string(theme.DensityCompact), Label: l10n.L("Compact")}}
	languages := []kit.Option{{Value: "", Label: l10n.L("System")}, {Value: string(l10n.English), Label: "English"}, {Value: string(l10n.Chinese), Label: "简体中文"}}
	k.SettingGroup(c, l10n.L("Appearance"), "",
		func() {
			k.SettingRow(c, l10n.L("Language"), l10n.L("System follows this computer's language."), pick(prefs.Language, l10n.N("Language"), languages), nil)
		},
		func() {
			k.SettingRow(c, l10n.L("Theme"), l10n.L("Light is the default. Kept on this device."), pick(prefs.Theme, l10n.N("Theme"), themes), nil)
		},
		func() {
			k.SettingRow(c, l10n.L("Font"), l10n.L("System uses your OS font (San Francisco on macOS, Segoe UI on Windows). Geist is built into Droi."), pick(prefs.Font, l10n.N("Font"), fonts), nil)
		},
		func() {
			k.SettingRow(c, l10n.L("Text size"), l10n.L("Scales every label, message and code block together. ⌘= and ⌘- zoom on top of this."), pick(prefs.TextSize, l10n.N("Text size"), sizes), nil)
		},
		func() {
			k.SettingRow(c, l10n.L("Density"), l10n.L("Comfortable has larger text and controls. Compact fits more on the screen."), pick(prefs.Density, l10n.N("Density"), densities), nil)
		},
	)
}
