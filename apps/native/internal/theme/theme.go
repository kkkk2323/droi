// Package theme holds the web Client's design tokens (apps/desktop/src/
// renderer/src/styles/global.css) for the native views: the colours of the
// three themes, Tailwind's type scale and radii, and the vendored Geist fonts.
package theme

import (
	"sync"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/assets"
)

// Name is a theme the user picks in Settings, as lib/theme.ts names them.
type Name string

const (
	Light          Name = "light"
	Dark           Name = "dark"
	SolarizedLight Name = "solarized-light"
)

var Names = []Name{Light, Dark, SolarizedLight}

var Labels = map[Name]string{Light: "Light", Dark: "Dark", SolarizedLight: "Solarized Light+"}

// Tokens are the CSS custom properties of one theme.
type Tokens struct {
	Name Name
	Dark bool

	Background, Foreground             ui.Color
	Card, CardForeground               ui.Color
	Popover, PopoverForeground         ui.Color
	Primary, PrimaryForeground         ui.Color
	Secondary, SecondaryForeground     ui.Color
	Muted, MutedForeground             ui.Color
	Accent, AccentForeground           ui.Color
	Destructive, DestructiveForeground ui.Color
	Border, Input, Ring                ui.Color
	Sidebar, SidebarForeground         ui.Color
	SidebarPrimary, SidebarPrimaryFg   ui.Color
	SidebarAccent, SidebarAccentFg     ui.Color
	SidebarBorder, SidebarRing         ui.Color
	Code, CodeBg                       ui.Color
	Success                            ui.Color
	Attention, AttentionForeground     ui.Color
	Info, InfoForeground               ui.Color
	Added, AddedForeground             ui.Color
	Removed, RemovedForeground         ui.Color
	Highlight                          ui.Color
	Selection                          ui.Color
	// SidebarMutedForeground is muted text inside the sidebar, which
	// Solarized Light+ sets apart from the conversation's.
	SidebarMutedForeground ui.Color
}

func ok(l, c, h float32) ui.Color { return ui.Oklch(l, c, h) }

var light = Tokens{
	Name:                   Light,
	Background:             ok(1, 0, 0),
	Foreground:             ok(0.2, 0, 0),
	Card:                   ok(0.955, 0, 0),
	CardForeground:         ok(0.2, 0, 0),
	Popover:                ok(1, 0, 0),
	PopoverForeground:      ok(0.2, 0, 0),
	Primary:                ok(0.2, 0, 0),
	PrimaryForeground:      ok(0.985, 0, 0),
	Secondary:              ok(0.94, 0, 0),
	SecondaryForeground:    ok(0.2, 0, 0),
	Muted:                  ok(0.955, 0, 0),
	MutedForeground:        ok(0.5, 0, 0),
	Accent:                 ok(0.925, 0, 0),
	AccentForeground:       ok(0.2, 0, 0),
	Destructive:            ok(0.577, 0.245, 27.325),
	DestructiveForeground:  ok(0.5, 0.22, 27),
	Border:                 ok(0.9, 0, 0),
	Input:                  ok(0.88, 0, 0),
	Ring:                   ok(0.6, 0, 0),
	Sidebar:                ok(0.955, 0, 0),
	SidebarForeground:      ok(0.3, 0, 0),
	SidebarPrimary:         ok(0.2, 0, 0),
	SidebarPrimaryFg:       ok(0.985, 0, 0),
	SidebarAccent:          ok(0.905, 0, 0),
	SidebarAccentFg:        ok(0.15, 0, 0),
	SidebarBorder:          ok(0.9, 0, 0),
	SidebarRing:            ok(0.6, 0, 0),
	Code:                   ok(0.5, 0.17, 20),
	CodeBg:                 ok(0.955, 0, 0),
	Success:                ok(0.596, 0.145, 163.225),
	Attention:              ok(0.666, 0.179, 58.318),
	AttentionForeground:    ok(0.555, 0.163, 48.998),
	Info:                   ok(0.588, 0.158, 241.966),
	InfoForeground:         ok(0.5, 0.134, 242.749),
	Added:                  ok(0.596, 0.145, 163.225),
	AddedForeground:        ok(0.432, 0.095, 166.913),
	Removed:                ok(0.586, 0.253, 17.585),
	RemovedForeground:      ok(0.455, 0.188, 13.697),
	Highlight:              ok(0.924, 0.12, 95.746).Alpha(0.7),
	Selection:              ui.RGBA(0, 120, 215, 0.25),
	SidebarMutedForeground: ok(0.5, 0, 0),
}

var dark = Tokens{
	Name:                   Dark,
	Dark:                   true,
	Background:             ok(0.2, 0, 0),
	Foreground:             ok(0.95, 0, 0),
	Card:                   ok(0.25, 0, 0),
	CardForeground:         ok(0.95, 0, 0),
	Popover:                ok(0.23, 0, 0),
	PopoverForeground:      ok(0.95, 0, 0),
	Primary:                ok(0.95, 0, 0),
	PrimaryForeground:      ok(0.2, 0, 0),
	Secondary:              ok(0.28, 0, 0),
	SecondaryForeground:    ok(0.95, 0, 0),
	Muted:                  ok(0.25, 0, 0),
	MutedForeground:        ok(0.68, 0, 0),
	Accent:                 ok(0.3, 0, 0),
	AccentForeground:       ok(0.95, 0, 0),
	Destructive:            ok(0.396, 0.141, 25.723),
	DestructiveForeground:  ok(0.7, 0.19, 25),
	Border:                 ok(0.3, 0, 0),
	Input:                  ok(0.32, 0, 0),
	Ring:                   ok(0.5, 0, 0),
	Sidebar:                ok(0.165, 0, 0),
	SidebarForeground:      ok(0.8, 0, 0),
	SidebarPrimary:         ok(0.95, 0, 0),
	SidebarPrimaryFg:       ok(0.2, 0, 0),
	SidebarAccent:          ok(0.26, 0, 0),
	SidebarAccentFg:        ok(0.97, 0, 0),
	SidebarBorder:          ok(0.26, 0, 0),
	SidebarRing:            ok(0.5, 0, 0),
	Code:                   ok(0.78, 0.12, 20),
	CodeBg:                 ok(0.27, 0, 0),
	Success:                ok(0.765, 0.177, 163.223),
	Attention:              ok(0.828, 0.189, 84.429),
	AttentionForeground:    ok(0.924, 0.12, 95.746),
	Info:                   ok(0.746, 0.16, 232.661),
	InfoForeground:         ok(0.828, 0.111, 230.318),
	Added:                  ok(0.765, 0.177, 163.223),
	AddedForeground:        ok(0.905, 0.093, 164.15),
	Removed:                ok(0.712, 0.194, 13.428),
	RemovedForeground:      ok(0.892, 0.058, 10.001),
	Highlight:              ok(0.769, 0.188, 70.08).Alpha(0.3),
	Selection:              ui.RGBA(100, 150, 255, 0.35),
	SidebarMutedForeground: ok(0.68, 0, 0),
}

var solarized = Tokens{
	Name:                   SolarizedLight,
	Background:             ui.Hex("#fdf6e3"),
	Foreground:             ui.Hex("#586e75"),
	Card:                   ui.Hex("#eee8d5"),
	CardForeground:         ui.Hex("#586e75"),
	Popover:                ui.Hex("#eee8d5"),
	PopoverForeground:      ui.Hex("#586e75"),
	Primary:                ui.Hex("#ac9d57"),
	PrimaryForeground:      ui.Hex("#ffffff"),
	Secondary:              ui.Hex("#eee8d5"),
	SecondaryForeground:    ui.Hex("#586e75"),
	Muted:                  ui.Hex("#eee8d5"),
	MutedForeground:        ui.Hex("#657b83"),
	Accent:                 ui.Hex("#ddd6c1"),
	AccentForeground:       ui.Hex("#586e75"),
	Destructive:            ui.Hex("#dc322f"),
	DestructiveForeground:  ui.Hex("#dc322f"),
	Border:                 ui.Hex("#ddd6c1"),
	Input:                  ui.Hex("#ddd6c1"),
	Ring:                   ui.Hex("#d3af86"),
	Sidebar:                ui.Hex("#eee8d5"),
	SidebarForeground:      ui.Hex("#616161"),
	SidebarPrimary:         ui.Hex("#ac9d57"),
	SidebarPrimaryFg:       ui.Hex("#ffffff"),
	SidebarAccent:          ui.Hex("#d1cbb8"),
	SidebarAccentFg:        ui.Hex("#616161"),
	SidebarBorder:          ui.Hex("#ddd6c1"),
	SidebarRing:            ui.Hex("#d3af86"),
	Code:                   ui.Hex("#2aa198"),
	CodeBg:                 ui.Hex("#eee8d5"),
	Success:                ui.Hex("#859900"),
	Attention:              ui.Hex("#b58900"),
	AttentionForeground:    ui.Hex("#b58900"),
	Info:                   ui.Hex("#268bd2"),
	InfoForeground:         ui.Hex("#268bd2"),
	Added:                  ui.Hex("#859900"),
	AddedForeground:        ui.Hex("#219186"),
	Removed:                ui.Hex("#dc322f"),
	RemovedForeground:      ui.Hex("#dc322f"),
	Highlight:              ui.RGBA(0xb5, 0x89, 0x00, 0x40/255.0),
	Selection:              ui.Hex("#ccc4b0"),
	SidebarMutedForeground: ui.Hex("#717171"),
}

// Of returns the tokens of a theme; an unknown name is Light.
func Of(n Name) *Tokens {
	switch n {
	case Dark:
		return &dark
	case SolarizedLight:
		return &solarized
	}
	return &light
}

// Parse reads a stored theme name, Light for anything else.
func Parse(s string) Name {
	for _, n := range Names {
		if string(n) == s {
			return n
		}
	}
	return Light
}

// Font families, as the CSS's --font-sans-choice and --font-mono-choice.
const (
	Geist     = "Geist"
	GeistMono = "Geist Mono"
)

var fontsOnce sync.Once

// RegisterFonts registers the vendored Geist pair, once.
func RegisterFonts() {
	fontsOnce.Do(func() {
		if err := ui.RegisterFont(assets.GeistVariable, Geist); err != nil {
			panic(err)
		}
		if err := ui.RegisterFont(assets.GeistMonoVariable, GeistMono); err != nil {
			panic(err)
		}
	})
}

// FontChoice is Settings → Font: the vendored Geist pair or the system's.
type FontChoice string

const (
	FontGeist  FontChoice = "geist"
	FontSystem FontChoice = "system"
)

// Families returns the sans and mono families of a font choice. "" is the
// system's own sans, as -apple-system is in the CSS.
func Families(f FontChoice) (sans, mono string) {
	if f == FontSystem {
		return "", "Menlo, monospace"
	}
	return Geist, GeistMono
}

// TextSize is Settings → Text size, which scales every rem.
type TextSize string

var TextSizes = []TextSize{"small", "default", "large", "largest"}

var TextSizeLabels = map[TextSize]string{"small": "Small", "default": "Default", "large": "Large", "largest": "Largest"}

// Scale is how much a text size scales the Client's rem, relative to 16px.
func (s TextSize) Scale() float32 {
	switch s {
	case "small":
		return 0.90625
	case "large":
		return 1.09375
	case "largest":
		return 1.1875
	}
	return 1
}
