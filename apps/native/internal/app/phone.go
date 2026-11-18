package app

import (
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/theme"
)

// PrefsKit is the Kit the preferences in p ask for, and sets the language
// they and locale pick: what the Phone App draws its pairing page with
// before it has an App.
func PrefsKit(p *prefs.Store, locale string) *kit.Kit {
	k := kit.New(theme.Parse(prefs.Theme.Get(p)), theme.FontChoice(prefs.Font.Get(p)), theme.TextSize(prefs.TextSize.Get(p)), prefs.Zoom.Get(p))
	k.Compact = theme.Density(prefs.Density.Get(p)) == theme.DensityCompact
	l10n.Set(prefs.Language.Get(p), locale)
	return k
}

// ComputerError is err, from computer.Verify, as the window tells it.
func ComputerError(err error) string { return computerError(err) }
