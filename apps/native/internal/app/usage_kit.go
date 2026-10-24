package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/dashkit"
)

// usageLook is dashkit's Look, which the Usage tab and the menu bar panel
// draw with: they copy droid-proxy's window, so they use its colors rather
// than the theme's. The Key Pool's sections are its methods.
type usageLook struct{ dashkit.Look }

func (a *App) usageLook(c *ui.Context) usageLook {
	return usageLook{dashkit.New(a.kit, c)}
}

// tokensInfo is the info icon whose tooltip turns credits into each
// model's tokens.
func (u usageLook) tokensInfo(c *ui.Context, tokens string) {
	u.Info(c, L("In tokens"), tokens, 11)
}

func providerColor(class string) ui.Color {
	switch class {
	case "anthropic":
		return ui.Hex("#cc784d")
	case "openai":
		return ui.Hex("#1aa68c")
	case "google":
		return ui.Hex("#4285f5")
	}
	return ui.Hex("#8e8e93")
}
