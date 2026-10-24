// Package dashkit draws dashboards in droid-proxy's look: a neutral page
// with white cards a hairline apart, one blue for the controls that show
// state, and color kept for status. It holds the palette, the surfaces and
// controls (Look), the charts, and the view models they draw, which say
// what to show in words and Tones rather than in colors.
package dashkit

import "github.com/egoist/mygo/ui"

// Palette is droid-proxy's colors, Light or Dark.
type Palette struct {
	Bg, Card, Text, Secondary, Tertiary, Faint, Line, Track, Hover, Selected ui.Color
	SegTrack, SegThumb, SegText                                              ui.Color
	Accent, Green, Warning, Danger                                           ui.Color
}

var (
	Light = Palette{
		Bg: ui.Hex("#f4f4f6"), Card: ui.Hex("#ffffff"), Text: ui.Hex("#1c1c21"),
		Secondary: ui.Hex("#66666e"), Tertiary: ui.Hex("#9a9aa3"), Faint: ui.Hex("#c6c6ce"),
		Line: ui.Hex("#e4e4e9"), Track: ui.Hex("#ececf0"),
		Hover: ui.RGBA(20, 20, 30, 0.045), Selected: ui.RGBA(20, 20, 30, 0.075),
		SegTrack: ui.Hex("#e0e0e6"), SegThumb: ui.Hex("#ffffff"), SegText: ui.Hex("#3f3f46"),
		Accent: ui.Hex("#2f6fe4"), Green: ui.Hex("#189a58"), Warning: ui.Hex("#c2710c"), Danger: ui.Hex("#d64545"),
	}
	Dark = Palette{
		Bg: ui.Hex("#1a1a1e"), Card: ui.Hex("#232327"), Text: ui.Hex("#ededf1"),
		Secondary: ui.Hex("#a3a3ad"), Tertiary: ui.Hex("#74747e"), Faint: ui.Hex("#56565f"),
		Line: ui.Hex("#333339"), Track: ui.Hex("#303036"),
		Hover: ui.RGBA(255, 255, 255, 0.05), Selected: ui.RGBA(255, 255, 255, 0.085),
		SegTrack: ui.Hex("#2a2a2f"), SegThumb: ui.Hex("#4a4a52"), SegText: ui.Hex("#c9c9d1"),
		Accent: ui.Hex("#7ea6ff"), Green: ui.Hex("#6fd99b"), Warning: ui.Hex("#f2b544"), Danger: ui.Hex("#f28b82"),
	}
)

// Tone is a semantic color a Look maps to its palette.
type Tone uint8

const (
	ToneSecondary Tone = iota
	TonePrimary
	ToneTertiary
	ToneAccent
	ToneGreen
	ToneWarning
	ToneDanger
	ToneTrack
)

// PrimaryTone keeps healthy figures in the text color: only problems get
// color.
func PrimaryTone(t Tone) Tone {
	if t == ToneSecondary {
		return TonePrimary
	}
	return t
}

// QuietTone keeps far-off deadlines quiet.
func QuietTone(t Tone) Tone {
	if t == ToneAccent {
		return ToneSecondary
	}
	return t
}

// StrokeIcon is a small stroke icon of droid-proxy's window, standing in
// for an SF Symbol: body is the SVG's shapes on a 24-unit grid.
func StrokeIcon(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

// The icons of droid-proxy's window.
var (
	IconStar        = StrokeIcon(`<path d="M12 3.2l2.6 5.5 6 .8-4.4 4.1 1.1 5.9L12 16.6l-5.3 2.9 1.1-5.9-4.4-4.1 6-.8z" fill="currentColor" stroke="none"/>`)
	IconClock       = StrokeIcon(`<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>`)
	IconRefresh     = StrokeIcon(`<path d="M19.5 12a7.5 7.5 0 1 1-2.2-5.3"/><path d="M19.5 4.5v4h-4"/>`)
	IconPlus        = StrokeIcon(`<path d="M12 5v14M5 12h14"/>`)
	IconSnowflake   = StrokeIcon(`<path d="M12 3v18M4.2 7.5l15.6 9M4.2 16.5l15.6-9M9.5 4.5 12 6l2.5-1.5M9.5 19.5 12 18l2.5 1.5"/>`)
	IconWarning     = StrokeIcon(`<path d="M10.3 4.2a2 2 0 0 1 3.4 0l7.4 12.9A2 2 0 0 1 19.4 20H4.6a2 2 0 0 1-1.7-2.9z"/><path d="M12 9v4.5"/><path d="M12 16.8h.01"/>`)
	IconCheck       = StrokeIcon(`<circle cx="12" cy="12" r="9"/><path d="m8 12.2 2.7 2.7L16.2 9.4"/>`)
	IconOffline     = StrokeIcon(`<path d="M5.6 18.4a9 9 0 0 1 0-12.8M18.4 5.6a9 9 0 0 1 0 12.8M8.5 15.5a5 5 0 0 1 0-7M15.5 8.5a5 5 0 0 1 0 7"/><path d="M4 4l16 16"/>`)
	IconWaveform    = StrokeIcon(`<path d="M4 10v4M8 6v12M12 9v6M16 4v16M20 10v4"/>`)
	IconArrows      = StrokeIcon(`<path d="M7 7h13l-3.5-3.5M17 17H4l3.5 3.5"/>`)
	IconChevron     = StrokeIcon(`<path d="M9 5l7 7-7 7"/>`)
	IconInfo        = StrokeIcon(`<circle cx="12" cy="12" r="8.5"/><path d="M12 11v5"/><path d="M12 8h.01"/>`)
	IconList        = StrokeIcon(`<path d="M9 6.5h11M9 12h11M9 17.5h11M4.5 6.5h.01M4.5 12h.01M4.5 17.5h.01"/>`)
	IconKey         = StrokeIcon(`<circle cx="7.5" cy="15.5" r="4"/><path d="m10.5 12.5 9-9M16 7l3 3M18.5 4.5l2 2"/>`)
	IconChevronDown = StrokeIcon(`<path d="m6 9.5 6 6 6-6"/>`)
	IconTick        = StrokeIcon(`<path d="m5 12.5 4.5 4.5L19 7.5"/>`)
)
