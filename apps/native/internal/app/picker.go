package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/models"
	"github.com/kkkk2323/droi/apps/native/pkg/settingsview"
)

// autonomyLevels are the levels the composer offers, in order.
var autonomyLevels = []string{"off", "low", "medium", "high"}

// pickerState is a model picker's own state while it is open.
type pickerState = settingsview.PickerState

// pickerToolMode is the picker's Tools row, for a Session about to start.
type pickerToolMode = settingsview.ToolModeChoice

// settingsBar is the SessionSettingsBar: the model with its reasoning
// effort, and the autonomy level.
func (v *sessionView) settingsBar(c *ui.Context, s *session.Session) {
	a := v.a
	cs := &v.composer
	set := s.Settings()
	choices := models.ToChoices(s.AvailableModels())
	update := func(p protocol.UpdateSessionSettingsParams) {
		p.SessionID = v.id
		go func() {
			cl, err := a.ctl.Client()
			if err == nil {
				_, err = cl.UpdateSessionSettings(a.ctx, p)
			}
			if err != nil {
				v.Fail(err)
			}
		}()
	}
	a.modelPicker(c, &cs.modelOpen, &cs.picker, choices, set.ModelID, string(set.ReasoningEffort),
		func(id string) { update(protocol.UpdateSessionSettingsParams{ModelID: id}) },
		func(e string) {
			update(protocol.UpdateSessionSettingsParams{ReasoningEffort: protocol.ReasoningEffort(e)})
		}, nil)
	// A Mission's Sessions have modes of their own.
	if mode := set.InteractionMode; mode == "" || mode == protocol.DroidInteractionModeAuto || mode == protocol.DroidInteractionModeSpec {
		spec := mode == protocol.DroidInteractionModeSpec
		if specToggle(c, a.kit, spec) {
			next := protocol.DroidInteractionModeSpec
			if spec {
				next = protocol.DroidInteractionModeAuto
			}
			update(protocol.UpdateSessionSettingsParams{InteractionMode: next, AutonomyLevel: set.AutonomyLevel})
		}
	}
	opts := make([]kit.Option, len(autonomyLevels))
	for i, l := range autonomyLevels {
		opts[i] = kit.Option{Value: l, Label: l10n.T(models.AutonomyLabels[l])}
	}
	if next, ok := a.kit.QuietSelect(c, L("Autonomy"), "shield-check", string(set.AutonomyLevel), opts); ok {
		update(protocol.UpdateSessionSettingsParams{AutonomyLevel: protocol.AutonomyLevel(next)})
	}
}

// specToggle is the Spec mode switch beside the composer's settings: in
// Spec mode Droid plans with the user before it changes anything. It
// reports a click.
func specToggle(c *ui.Context, k *kit.Kit, on bool) bool {
	t := k.T
	b := k.QuietTrigger(c, L("Spec mode"), false).Role(ui.RoleToggleButton).Checked(on).Tooltip(L("Plan with Droid before it changes anything"))
	fg := t.Foreground.Alpha(0.75)
	if b.Hovered() {
		fg = t.Foreground
	}
	if on {
		fg = t.InfoForeground
		b.Background(t.Info.Alpha(0.12))
	}
	b.Children(func() {
		k.Icon(c, "list-checks", 14, fg)
		k.Text(c, "Spec", 13, 19.5).TextColor(fg).SingleLine()
	})
	return b.Clicked()
}

func (a *App) modelPicker(c *ui.Context, open *bool, st *pickerState, choices []models.Choice, value, effort string, onModel, onEffort func(string), tools *pickerToolMode) {
	settingsview.ModelPicker(c, a.kit, a.prefs, open, st, choices, value, effort, onModel, onEffort, tools)
}

func (a *App) modelField(c *ui.Context, label string, disabled bool, open *bool, st *pickerState, choices []models.Choice, value string, onModel func(string)) {
	settingsview.ModelField(c, a.kit, a.prefs, label, disabled, open, st, choices, value, onModel)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
