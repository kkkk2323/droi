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

// The workbench separates a neutral navigation rail from the reading surface.
// Controls use low-alpha ink and a single blue tint for focus and state.
var light = Tokens{
	Name:                   Light,
	Background:             ui.Hex("#ffffff"),
	Foreground:             ui.Hex("#171717"),
	Card:                   ui.Hex("#ffffff"),
	CardForeground:         ui.Hex("#171717"),
	Popover:                ui.Hex("#ffffff"),
	PopoverForeground:      ui.Hex("#171717"),
	Primary:                ui.Hex("#171717"),
	PrimaryForeground:      ui.Hex("#ffffff"),
	Secondary:              ui.RGBA(0, 0, 0, 0.03),
	SecondaryForeground:    ui.Hex("#171717"),
	Muted:                  ui.RGBA(0, 0, 0, 0.04),
	MutedForeground:        ui.Hex("#636363"),
	Accent:                 ui.RGBA(0, 0, 0, 0.04),
	AccentForeground:       ui.Hex("#171717"),
	Destructive:            ui.Hex("#ef4444"),
	DestructiveForeground:  ui.Hex("#c81e1e"),
	Border:                 ui.RGBA(0, 0, 0, 0.12),
	Input:                  ui.RGBA(0, 0, 0, 0.15),
	Ring:                   ui.Hex("#a3a3a3"),
	Sidebar:                ui.Hex("#ebebeb"),
	SidebarForeground:      ui.Hex("#171717"),
	SidebarPrimary:         ui.Hex("#171717"),
	SidebarPrimaryFg:       ui.Hex("#ffffff"),
	SidebarAccent:          ui.RGBA(0, 0, 0, 0.045),
	SidebarAccentFg:        ui.Hex("#171717"),
	SidebarBorder:          ui.RGBA(0, 0, 0, 0.10),
	SidebarRing:            ui.Hex("#a3a3a3"),
	Code:                   ui.Hex("#af2938"),
	CodeBg:                 ui.RGBA(0, 0, 0, 0.055),
	Success:                ui.Hex("#10b981"),
	Attention:              ui.Hex("#f59e0b"),
	AttentionForeground:    ui.Hex("#b45309"),
	Info:                   ui.Hex("#3b82f6"),
	InfoForeground:         ui.Hex("#1d4ed8"),
	Added:                  ui.Hex("#00a240"),
	AddedForeground:        ui.Hex("#0a7a34"),
	Removed:                ui.Hex("#e02e2a"),
	RemovedForeground:      ui.Hex("#b3211e"),
	Highlight:              ui.RGBA(250, 204, 21, 0.35),
	Selection:              ui.RGBA(0, 105, 204, 0.18),
	SidebarMutedForeground: ui.Hex("#5c5c5c"),
	Tint:                   ui.Hex("#0169cc"),
}

var dark = Tokens{
	Name:                   Dark,
	Dark:                   true,
	Background:             ui.Hex("#18191b"),
	Foreground:             ui.Hex("#f5f5f5"),
	Card:                   ui.Hex("#202124"),
	CardForeground:         ui.Hex("#f5f5f5"),
	Popover:                ui.Hex("#252629"),
	PopoverForeground:      ui.Hex("#f5f5f5"),
	Primary:                ui.Hex("#f5f5f5"),
	PrimaryForeground:      ui.Hex("#171717"),
	Secondary:              ui.RGBA(255, 255, 255, 0.05),
	SecondaryForeground:    ui.Hex("#f5f5f5"),
	Muted:                  ui.RGBA(255, 255, 255, 0.05),
	MutedForeground:        ui.Hex("#9a9a9a"),
	Accent:                 ui.RGBA(255, 255, 255, 0.05),
	AccentForeground:       ui.Hex("#f5f5f5"),
	Destructive:            ui.Hex("#f05454"),
	DestructiveForeground:  ui.Hex("#f87575"),
	Border:                 ui.RGBA(255, 255, 255, 0.12),
	Input:                  ui.RGBA(255, 255, 255, 0.15),
	Ring:                   ui.Hex("#6b6b6b"),
	Sidebar:                ui.Hex("#202124"),
	SidebarForeground:      ui.Hex("#f5f5f5"),
	SidebarPrimary:         ui.Hex("#f5f5f5"),
	SidebarPrimaryFg:       ui.Hex("#171717"),
	SidebarAccent:          ui.RGBA(255, 255, 255, 0.06),
	SidebarAccentFg:        ui.Hex("#ffffff"),
	SidebarBorder:          ui.RGBA(255, 255, 255, 0.10),
	SidebarRing:            ui.Hex("#6b6b6b"),
	Code:                   ui.Hex("#fb9797"),
	CodeBg:                 ui.RGBA(255, 255, 255, 0.06),
	Success:                ui.Hex("#34d399"),
	Attention:              ui.Hex("#fbbf24"),
	AttentionForeground:    ui.Hex("#fcd34d"),
	Info:                   ui.Hex("#60a5fa"),
	InfoForeground:         ui.Hex("#93c5fd"),
	Added:                  ui.Hex("#40c977"),
	AddedForeground:        ui.Hex("#5ef0a4"),
	Removed:                ui.Hex("#fa423e"),
	RemovedForeground:      ui.Hex("#ff8a86"),
	Highlight:              ui.RGBA(251, 191, 36, 0.22),
	Selection:              ui.RGBA(51, 156, 255, 0.28),
	SidebarMutedForeground: ui.Hex("#8f8f8f"),
	Tint:                   ui.Hex("#339cff"),
}

// Solarized Light+ in the same material: the page a step brighter than its
// panels, hairlines as low-alpha ink, and the tint the palette's blue.
var solarized = Tokens{
	Name:                   SolarizedLight,
	Background:             ui.Hex("#fbf7ea"),
	Foreground:             ui.Hex("#4b5c62"),
	Card:                   ui.Hex("#f4efe2"),
	CardForeground:         ui.Hex("#4b5c62"),
	Popover:                ui.Hex("#fdfaf1"),
	PopoverForeground:      ui.Hex("#4b5c62"),
	Primary:                ui.Hex("#5c6f76"),
	PrimaryForeground:      ui.Hex("#fdfaf1"),
	Secondary:              ui.RGBA(88, 110, 117, 0.06),
	SecondaryForeground:    ui.Hex("#4b5c62"),
	Muted:                  ui.RGBA(88, 110, 117, 0.07),
	MutedForeground:        ui.Hex("#5b6e76"),
	Accent:                 ui.RGBA(88, 110, 117, 0.08),
	AccentForeground:       ui.Hex("#40505a"),
	Destructive:            ui.Hex("#dc322f"),
	DestructiveForeground:  ui.Hex("#b32d2a"),
	Border:                 ui.RGBA(88, 110, 117, 0.14),
	Input:                  ui.RGBA(88, 110, 117, 0.18),
	Ring:                   ui.Hex("#b3a99b"),
	Sidebar:                ui.Hex("#eee8d8"),
	SidebarForeground:      ui.Hex("#44555c"),
	SidebarPrimary:         ui.Hex("#5c6f76"),
	SidebarPrimaryFg:       ui.Hex("#fdfaf1"),
	SidebarAccent:          ui.RGBA(88, 110, 117, 0.09),
	SidebarAccentFg:        ui.Hex("#3c4c54"),
	SidebarBorder:          ui.RGBA(88, 110, 117, 0.14),
	SidebarRing:            ui.Hex("#b3a99b"),
	Code:                   ui.Hex("#1f7f78"),
	CodeBg:                 ui.RGBA(88, 110, 117, 0.08),
	Success:                ui.Hex("#859900"),
	Attention:              ui.Hex("#b58900"),
	AttentionForeground:    ui.Hex("#8f6a00"),
	Info:                   ui.Hex("#268bd2"),
	InfoForeground:         ui.Hex("#1f6fa8"),
	Added:                  ui.Hex("#859900"),
	AddedForeground:        ui.Hex("#6a7a00"),
	Removed:                ui.Hex("#dc322f"),
	RemovedForeground:      ui.Hex("#b32d2a"),
	Highlight:              ui.RGBA(181, 137, 0, 0.22),
	Selection:              ui.RGBA(38, 139, 210, 0.18),
	SidebarMutedForeground: ui.Hex("#5b6e76"),
	Tint:                   ui.Hex("#268bd2"),
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

// Density is Settings → Density. Comfortable, the default, keeps the text
// and control sizes Droi had before ADR 0025; Compact keeps the smaller
// ones that ADR brought in, to fit more on the screen.
type Density string

const (
	DensityComfortable Density = "comfortable"
	DensityCompact     Density = "compact"
)

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
