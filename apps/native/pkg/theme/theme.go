// Package theme holds the design tokens the native views paint with: the
// colours of the three themes (after ADR 0025), Tailwind's type scale and
// radii, and the vendored faces, JetBrains Mono for code and Geist for the
// Font setting's own choice.
package theme

import (
	"sync"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/assets"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// Name is a theme the user picks in Settings, as lib/theme.ts names them.
type Name string

const (
	Light          Name = "light"
	Dark           Name = "dark"
	SolarizedLight Name = "solarized-light"
)

var Names = []Name{Light, Dark, SolarizedLight}

var Labels = map[Name]string{Light: l10n.N("Light"), Dark: l10n.N("Dark"), SolarizedLight: "Solarized Light+"}

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
	// Tint is the one accent hue the controls that show state paint with: a
	// switch that is on, a field with the focus. Everything else stays
	// neutral, as the reference does.
	Tint ui.Color
	// SidebarMutedForeground is muted text inside the sidebar, which
	// Solarized Light+ sets apart from the conversation's.
	SidebarMutedForeground ui.Color
}

// ok is CSS's oklch(l c h) as its nearest sRGB color. A color beyond sRGB
// has MyGo draw the window into float16 drawables on a Display P3 screen,
// twice the memory of 8-bit ones (about 50 MB for a large window), for
// status colors that differ from their nearest sRGB ones by a ΔE under 2.2.
func ok(l, c, h float32) ui.Color {
	x := ui.Oklch(l, c, h)
	return ui.Color{R: x.R, G: x.G, B: x.B, A: x.A}
}

// ink is a hex color of the reference palette (Synara's: Codex's neutral
// scale), rgba a translucent step of it for fills and hairlines.
func ink(s string) ui.Color { return ui.Hex(s) }

func rgba(r, g, b uint8, a float32) ui.Color { return ui.RGBA(r, g, b, a) }

// The themes follow Synara (Emanuele-web04/synara): a near-white page in
// light and a near-black one in dark, surfaces one hairline apart, borders
// and fills as low-alpha steps of the ink rather than separate greys, and a
// single blue tint for the controls that show state (a switch, a focused
// field).
var light = Tokens{
	Name:                   Light,
	Background:             ink("#fcfcfc"),
	Foreground:             ink("#262626"),
	Card:                   ink("#ffffff"),
	CardForeground:         ink("#262626"),
	Popover:                ink("#ffffff"),
	PopoverForeground:      ink("#262626"),
	Primary:                ink("#171717"),
	PrimaryForeground:      ink("#ffffff"),
	Secondary:              rgba(0, 0, 0, 0.03),
	SecondaryForeground:    ink("#262626"),
	Muted:                  rgba(0, 0, 0, 0.04),
	MutedForeground:        ink("#6f6f6f"),
	Accent:                 rgba(0, 0, 0, 0.04),
	AccentForeground:       ink("#262626"),
	Destructive:            ink("#ef4444"),
	DestructiveForeground:  ink("#c81e1e"),
	Border:                 rgba(0, 0, 0, 0.06),
	Input:                  rgba(0, 0, 0, 0.08),
	Ring:                   ink("#a3a3a3"),
	Sidebar:                ink("#ffffff"),
	SidebarForeground:      ink("#262626"),
	SidebarPrimary:         ink("#171717"),
	SidebarPrimaryFg:       ink("#ffffff"),
	SidebarAccent:          rgba(0, 0, 0, 0.045),
	SidebarAccentFg:        ink("#171717"),
	SidebarBorder:          rgba(0, 0, 0, 0.06),
	SidebarRing:            ink("#a3a3a3"),
	Code:                   ink("#262626"),
	CodeBg:                 rgba(0, 0, 0, 0.045),
	Success:                ink("#10b981"),
	Attention:              ink("#f59e0b"),
	AttentionForeground:    ink("#b45309"),
	Info:                   ink("#3b82f6"),
	InfoForeground:         ink("#1d4ed8"),
	Added:                  ink("#00a240"),
	AddedForeground:        ink("#0a7a34"),
	Removed:                ink("#e02e2a"),
	RemovedForeground:      ink("#b3211e"),
	Highlight:              rgba(250, 204, 21, 0.35),
	Selection:              rgba(0, 105, 204, 0.18),
	SidebarMutedForeground: ink("#8a8a8a"),
	Tint:                   ink("#0169cc"),
}

var dark = Tokens{
	Name:                   Dark,
	Dark:                   true,
	Background:             ink("#0e0e0e"),
	Foreground:             ink("#f5f5f5"),
	Card:                   ink("#141414"),
	CardForeground:         ink("#f5f5f5"),
	Popover:                ink("#171717"),
	PopoverForeground:      ink("#f5f5f5"),
	Primary:                ink("#f5f5f5"),
	PrimaryForeground:      ink("#171717"),
	Secondary:              rgba(255, 255, 255, 0.05),
	SecondaryForeground:    ink("#f5f5f5"),
	Muted:                  rgba(255, 255, 255, 0.05),
	MutedForeground:        ink("#9a9a9a"),
	Accent:                 rgba(255, 255, 255, 0.05),
	AccentForeground:       ink("#f5f5f5"),
	Destructive:            ink("#f05454"),
	DestructiveForeground:  ink("#f87575"),
	Border:                 rgba(255, 255, 255, 0.08),
	Input:                  rgba(255, 255, 255, 0.1),
	Ring:                   ink("#6b6b6b"),
	Sidebar:                ink("#131313"),
	SidebarForeground:      ink("#f5f5f5"),
	SidebarPrimary:         ink("#f5f5f5"),
	SidebarPrimaryFg:       ink("#171717"),
	SidebarAccent:          rgba(255, 255, 255, 0.06),
	SidebarAccentFg:        ink("#ffffff"),
	SidebarBorder:          rgba(255, 255, 255, 0.07),
	SidebarRing:            ink("#6b6b6b"),
	Code:                   ink("#f5f5f5"),
	CodeBg:                 rgba(255, 255, 255, 0.06),
	Success:                ink("#34d399"),
	Attention:              ink("#fbbf24"),
	AttentionForeground:    ink("#fcd34d"),
	Info:                   ink("#60a5fa"),
	InfoForeground:         ink("#93c5fd"),
	Added:                  ink("#40c977"),
	AddedForeground:        ink("#5ef0a4"),
	Removed:                ink("#fa423e"),
	RemovedForeground:      ink("#ff8a86"),
	Highlight:              rgba(251, 191, 36, 0.22),
	Selection:              rgba(51, 156, 255, 0.28),
	SidebarMutedForeground: ink("#8f8f8f"),
	Tint:                   ink("#339cff"),
}

// Solarized Light+ in the same material: the page a step brighter than its
// panels, hairlines as low-alpha ink, and the tint the palette's blue.
var solarized = Tokens{
	Name:                   SolarizedLight,
	Background:             ink("#fbf7ea"),
	Foreground:             ink("#4b5c62"),
	Card:                   ink("#f4efe2"),
	CardForeground:         ink("#4b5c62"),
	Popover:                ink("#fdfaf1"),
	PopoverForeground:      ink("#4b5c62"),
	Primary:                ink("#5c6f76"),
	PrimaryForeground:      ink("#fdfaf1"),
	Secondary:              rgba(88, 110, 117, 0.06),
	SecondaryForeground:    ink("#4b5c62"),
	Muted:                  rgba(88, 110, 117, 0.07),
	MutedForeground:        ink("#6b7f88"),
	Accent:                 rgba(88, 110, 117, 0.08),
	AccentForeground:       ink("#40505a"),
	Destructive:            ink("#dc322f"),
	DestructiveForeground:  ink("#b32d2a"),
	Border:                 rgba(88, 110, 117, 0.14),
	Input:                  rgba(88, 110, 117, 0.18),
	Ring:                   ink("#b3a99b"),
	Sidebar:                ink("#f7f2e5"),
	SidebarForeground:      ink("#44555c"),
	SidebarPrimary:         ink("#5c6f76"),
	SidebarPrimaryFg:       ink("#fdfaf1"),
	SidebarAccent:          rgba(88, 110, 117, 0.09),
	SidebarAccentFg:        ink("#3c4c54"),
	SidebarBorder:          rgba(88, 110, 117, 0.14),
	SidebarRing:            ink("#b3a99b"),
	Code:                   ink("#4b5c62"),
	CodeBg:                 rgba(88, 110, 117, 0.08),
	Success:                ink("#859900"),
	Attention:              ink("#b58900"),
	AttentionForeground:    ink("#8f6a00"),
	Info:                   ink("#268bd2"),
	InfoForeground:         ink("#1f6fa8"),
	Added:                  ink("#859900"),
	AddedForeground:        ink("#6a7a00"),
	Removed:                ink("#dc322f"),
	RemovedForeground:      ink("#b32d2a"),
	Highlight:              rgba(181, 137, 0, 0.22),
	Selection:              rgba(38, 139, 210, 0.18),
	SidebarMutedForeground: ink("#7d8d93"),
	Tint:                   ink("#268bd2"),
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

// Font families. The interface face is the system's own (SF Pro on macOS,
// Segoe UI on Windows), as the reference app uses; the code face is the
// vendored JetBrains Mono.
const (
	// SystemUI is the system's interface font, as CSS's system-ui: the
	// text stack resolves it to SF Pro on macOS and Segoe UI on Windows.
	SystemUI = "system-ui"
	// JetBrainsMono is the vendored code face, also for a field that is mono.
	JetBrainsMono = "JetBrains Mono"
	// Geist and GeistMono are still vendored, for the Font setting's choice.
	Geist     = "Geist"
	GeistMono = "Geist Mono"
)

var fontsOnce sync.Once

// RegisterFonts registers the vendored faces, once: JetBrains Mono, and the
// Geist pair the Font setting can still pick.
func RegisterFonts() {
	fontsOnce.Do(func() {
		if err := ui.RegisterFont(assets.JetBrainsMonoVariable, JetBrainsMono); err != nil {
			panic(err)
		}
		if err := ui.RegisterFont(assets.GeistVariable, Geist); err != nil {
			panic(err)
		}
		if err := ui.RegisterFont(assets.GeistMonoVariable, GeistMono); err != nil {
			panic(err)
		}
	})
}

// FontChoice is Settings → Font: the system's interface font, the default,
// or the vendored Geist pair.
type FontChoice string

const (
	// FontSystem is the reference's own: the system's interface font with
	// JetBrains Mono for code.
	FontSystem FontChoice = "system"
	FontGeist  FontChoice = "geist"
)

// Families returns the sans and mono families of a font choice. SystemUI is
// the text stack's own system font, as CSS's system-ui is.
func Families(f FontChoice) (sans, mono string) {
	if f == FontGeist {
		return Geist, GeistMono
	}
	return SystemUI, JetBrainsMono
}

// TextSize is Settings → Text size, which scales every rem.
type TextSize string

var TextSizes = []TextSize{"small", "default", "large", "largest"}

var TextSizeLabels = map[TextSize]string{"small": l10n.N("Small"), "default": l10n.N("Default"), "large": l10n.N("Large"), "largest": l10n.N("Largest")}

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
