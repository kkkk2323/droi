package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
)

// usagePalette is droid-proxy's: a neutral page with white cards a hairline
// apart and one blue for the controls that show state; color is kept for
// status. The Usage tab copies droid-proxy's window, so it draws with its
// colors rather than the theme's.
type usagePalette struct {
	bg, card, text, secondary, tertiary, faint, line, track, hover, selected ui.Color
	segTrack, segThumb                                                       ui.Color
	accent, green, warning, danger                                           ui.Color
}

var (
	usageLight = usagePalette{
		bg: ui.Hex("#f4f4f6"), card: ui.Hex("#ffffff"), text: ui.Hex("#1c1c21"),
		secondary: ui.Hex("#66666e"), tertiary: ui.Hex("#9a9aa3"), faint: ui.Hex("#c6c6ce"),
		line: ui.Hex("#e4e4e9"), track: ui.Hex("#ececf0"),
		hover: ui.RGBA(20, 20, 30, 0.045), selected: ui.RGBA(20, 20, 30, 0.075),
		segTrack: ui.Hex("#e8e8ed"), segThumb: ui.Hex("#ffffff"),
		accent: ui.Hex("#2f6fe4"), green: ui.Hex("#189a58"), warning: ui.Hex("#c2710c"), danger: ui.Hex("#d64545"),
	}
	usageDark = usagePalette{
		bg: ui.Hex("#1a1a1e"), card: ui.Hex("#232327"), text: ui.Hex("#ededf1"),
		secondary: ui.Hex("#a3a3ad"), tertiary: ui.Hex("#74747e"), faint: ui.Hex("#56565f"),
		line: ui.Hex("#333339"), track: ui.Hex("#303036"),
		hover: ui.RGBA(255, 255, 255, 0.05), selected: ui.RGBA(255, 255, 255, 0.085),
		segTrack: ui.Hex("#2a2a2f"), segThumb: ui.Hex("#45454c"),
		accent: ui.Hex("#7ea6ff"), green: ui.Hex("#6fd99b"), warning: ui.Hex("#f2b544"), danger: ui.Hex("#f28b82"),
	}
)

// Small stroke icons of droid-proxy's window, standing in for SF Symbols.
func usageIcon(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

var (
	iconStar      = usageIcon(`<path d="M12 3.2l2.6 5.5 6 .8-4.4 4.1 1.1 5.9L12 16.6l-5.3 2.9 1.1-5.9-4.4-4.1 6-.8z" fill="currentColor" stroke="none"/>`)
	iconClock     = usageIcon(`<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>`)
	iconRefresh   = usageIcon(`<path d="M19.5 12a7.5 7.5 0 1 1-2.2-5.3"/><path d="M19.5 4.5v4h-4"/>`)
	iconPlus      = usageIcon(`<path d="M12 5v14M5 12h14"/>`)
	iconSnowflake = usageIcon(`<path d="M12 3v18M4.2 7.5l15.6 9M4.2 16.5l15.6-9M9.5 4.5 12 6l2.5-1.5M9.5 19.5 12 18l2.5 1.5"/>`)
	iconWarning   = usageIcon(`<path d="M10.3 4.2a2 2 0 0 1 3.4 0l7.4 12.9A2 2 0 0 1 19.4 20H4.6a2 2 0 0 1-1.7-2.9z"/><path d="M12 9v4.5"/><path d="M12 16.8h.01"/>`)
	iconCheck     = usageIcon(`<circle cx="12" cy="12" r="9"/><path d="m8 12.2 2.7 2.7L16.2 9.4"/>`)
	iconOffline   = usageIcon(`<path d="M5.6 18.4a9 9 0 0 1 0-12.8M18.4 5.6a9 9 0 0 1 0 12.8M8.5 15.5a5 5 0 0 1 0-7M15.5 8.5a5 5 0 0 1 0 7"/><path d="M4 4l16 16"/>`)
	iconWaveform  = usageIcon(`<path d="M4 10v4M8 6v12M12 9v6M16 4v16M20 10v4"/>`)
	iconArrows    = usageIcon(`<path d="M7 7h13l-3.5-3.5M17 17H4l3.5 3.5"/>`)
	iconChevron   = usageIcon(`<path d="M9 5l7 7-7 7"/>`)
	iconInfo      = usageIcon(`<circle cx="12" cy="12" r="8.5"/><path d="M12 11v5"/><path d="M12 8h.01"/>`)
	iconList      = usageIcon(`<path d="M9 6.5h11M9 12h11M9 17.5h11M4.5 6.5h.01M4.5 12h.01M4.5 17.5h.01"/>`)
	iconKey       = usageIcon(`<circle cx="7.5" cy="15.5" r="4"/><path d="m10.5 12.5 9-9M16 7l3 3M18.5 4.5l2 2"/>`)
)

// usageLook draws droid-proxy's controls and surfaces, at the kit's text
// size.
type usageLook struct {
	k *kit.Kit
	p *usagePalette
}

func (a *App) usageLook(c *ui.Context) usageLook {
	p := &usageLight
	if c.Theme().Dark {
		p = &usageDark
	}
	return usageLook{k: a.kit, p: p}
}

func (u usageLook) px(v float32) float32 { return u.k.Px(v) }

// themed returns MyGo's theme set to the palette, for the widgets used as
// they are: tables, text inputs, dialogs, spinners and scroll bars.
func (u usageLook) themed(base *ui.Theme) *ui.Theme {
	t, p := *base, u.p
	t.Background, t.Surface = p.card, p.card
	t.SurfaceHover, t.SurfacePressed = p.hover, p.selected
	t.Border = p.line
	t.Text, t.TextMuted = p.text, p.secondary
	t.Accent, t.Danger, t.Warning, t.Success = p.accent, p.danger, p.warning, p.green
	t.Radius = u.px(7)
	t.FontSize = u.px(13)
	return &t
}

func (u usageLook) tone(t llmusage.Tone) ui.Color {
	p := u.p
	switch t {
	case llmusage.TonePrimary:
		return p.text
	case llmusage.ToneTertiary:
		return p.tertiary
	case llmusage.ToneAccent:
		return p.accent
	case llmusage.ToneGreen:
		return p.green
	case llmusage.ToneWarning:
		return p.warning
	case llmusage.ToneDanger:
		return p.danger
	case llmusage.ToneTrack:
		return p.track
	}
	return p.secondary
}

// primaryTone keeps healthy figures in the text color: only problems get
// color.
func primaryTone(t llmusage.Tone) llmusage.Tone {
	if t == llmusage.ToneSecondary {
		return llmusage.TonePrimary
	}
	return t
}

// quietTone keeps far-off deadlines quiet.
func quietTone(t llmusage.Tone) llmusage.Tone {
	if t == llmusage.ToneAccent {
		return llmusage.ToneSecondary
	}
	return t
}

func (u usageLook) text(c *ui.Context, s string, size float32) ui.Element {
	return ui.Text(c, s).FontSize(u.px(size))
}

func (u usageLook) icon(c *ui.Context, s *ui.SVG, size float32, color ui.Color) ui.Element {
	return ui.Icon(c, s).Size(u.px(size), u.px(size)).Shrink(0).TextColor(color)
}

// card is a white surface a hairline from the page.
func (u usageLook) card(c *ui.Context) ui.Element {
	return ui.Column(c).Radius(u.px(12)).Background(u.p.card).Border(1, u.p.line).Clip()
}

// hairline separates the rows or cells of a card.
func (u usageLook) hairline(c *ui.Context) { ui.Box(c).Height(1).Shrink(0).Background(u.p.line) }

func (u usageLook) vhairline(c *ui.Context) {
	ui.Box(c).Width(1).Shrink(0).AlignSelf(ui.Stretch).Background(u.p.line)
}

// caps is a small section label above a group.
func (u usageLook) caps(c *ui.Context, s string) ui.Element {
	return u.text(c, s, 11).FontWeight(600).LetterSpacing(0.4).TextColor(u.p.tertiary).NoWrap()
}

// sectionHead is a group's label, a note after it, and what goes on the
// right.
func (u usageLook) sectionHead(c *ui.Context, title, note string, right func()) {
	ui.Row(c).Gap(u.px(8)).Padding(u.px(2), u.px(4), 0).Children(func() {
		u.caps(c, title).Role(ui.RoleHeading)
		if note != "" {
			u.text(c, note, 11).TextColor(u.p.tertiary).FontFeatures("tnum").SingleLine().Shrink(1)
		}
		ui.Spacer(c)
		if right != nil {
			right()
		}
	})
}

// info is the info icon whose tooltip explains a section.
func (u usageLook) info(c *ui.Context, label, tip string, size float32) {
	ui.Box(c).Tooltip(tip).Label(label).Children(func() { u.icon(c, iconInfo, size, u.p.tertiary) })
}

// meter is a capsule filled to value (0…100). mark, when 0…100, draws a
// tick there: where the fill would be at an even pace.
func (u usageLook) meter(c *ui.Context, value float64, tone llmusage.Tone, height float32, mark float64) ui.Element {
	v := float32(min(max(value, 0), 100)) / 100
	m := float32(mark) / 100
	height = u.px(height)
	fill, track, tick := u.tone(tone), u.p.track, u.p.text.Alpha(0.55)
	return ui.Box(c).Role(ui.RoleMeter).Grow(1).Basis(0).MinWidth(u.px(24)).Height(height + 4).Draw(func(p *ui.Painter, r ui.Rect) {
		bar := ui.Rect{X: r.X, Y: r.Y + 2, W: r.W, H: height}
		p.Fill(bar, track, height/2)
		if v > 0 {
			p.Fill(ui.Rect{X: bar.X, Y: bar.Y, W: max(bar.W*v, height), H: height}, fill, height/2)
		}
		if mark >= 0 && mark <= 100 {
			x := bar.X + bar.W*m
			p.Line(x, r.Y, x, r.Y+r.H, 1.5, tick)
		}
	})
}

// metricStrip is numbers side by side in one card, a hairline apart: the
// figure, its name, then a note.
func (u usageLook) metricStrip(c *ui.Context, metrics []llmusage.MetricView) {
	p := u.p
	u.card(c).Shrink(0).Children(func() {
		ui.Row(c).AlignItems(ui.Stretch).Children(func() {
			for i, m := range metrics {
				if i > 0 {
					u.vhairline(c)
				}
				ui.Column(c.Key(i)).Role(ui.RoleGroup).Label(m.Title).Grow(1).Basis(0).MinWidth(0).Gap(u.px(3)).Padding(u.px(14), u.px(16)).Children(func() {
					ui.Row(c).Gap(u.px(6)).AlignItems(ui.End).Children(func() {
						u.text(c, m.Value, 22).FontWeight(600).LetterSpacing(-0.3).TextColor(u.tone(primaryTone(m.Tone))).FontFeatures("tnum").SingleLine().Shrink(1)
						if m.Sub != "" {
							u.text(c, m.Sub, 13).FontWeight(500).TextColor(p.secondary).FontFeatures("tnum").NoWrap().Padding(0, 0, u.px(3))
						}
					})
					u.text(c, m.Title, 11).FontWeight(600).TextColor(p.secondary).SingleLine()
					note := m.Note
					if note == "" {
						note = "\u00a0"
					}
					u.text(c, note, 11).TextColor(p.tertiary).FontFeatures("tnum").SingleLine()
				})
			}
		})
	})
}

// chart is the spend of the last days, a bar each with its day in a
// tooltip, under a dashed line at the daily limit; today's bar is the
// accent, a day over the limit amber.
func (u usageLook) chart(c *ui.Context, v llmusage.ChartView, height float32) {
	if v.Top <= 0 {
		return
	}
	p := u.p
	height = u.px(height)
	scale := func(val float64) float32 { return height * float32(min(max(val/v.Top, 0), 1)) }
	limitY := height - scale(v.Limit)
	ui.Column(c).Gap(u.px(6)).Children(func() {
		ui.Row(c).Height(height).Gap(u.px(6)).AlignItems(ui.End).DrawOver(func(pt *ui.Painter, r ui.Rect) {
			y := r.Y + limitY
			for x := r.X; x < r.X+r.W; x += 7 {
				pt.Line(x, y, min(x+4, r.X+r.W), y, 1, p.secondary.Alpha(0.8))
			}
		}).Children(func() {
			for i, b := range v.Bars {
				color := p.accent.Alpha(0.28)
				switch {
				case b.Today && b.Over:
					color = p.danger
				case b.Today:
					color = p.accent
				case b.Over:
					color = p.warning.Alpha(0.6)
				}
				ui.Column(c.Key(i)).Role(ui.RoleMeter).Label(b.Help).Grow(1).Basis(0).Height(height).Justify(ui.End).Tooltip(b.Help).Children(func() {
					ui.Box(c).Height(max(scale(b.Value), 2)).Radius(u.px(3)).Background(color)
				})
			}
		})
		ui.Row(c).Children(func() {
			u.text(c, v.First, 10).TextColor(p.tertiary).FontFeatures("tnum")
			ui.Spacer(c)
			u.text(c, v.Last, 10).TextColor(p.tertiary)
		})
	})
}

func (u usageLook) badge(c *ui.Context, b llmusage.Badge, compact bool) ui.Element {
	size, padX := float32(11), float32(7)
	if compact {
		size, padX = 10, 6
	}
	return u.text(c, b.Label, size).FontWeight(500).TextColor(u.tone(b.Tone)).
		Padding(1, u.px(padX)).Radius(u.px(6)).Background(u.tone(b.Tone).Alpha(0.12)).NoWrap()
}

// unavailable is a page's empty state: an icon, a title, a description and
// maybe an action.
func (u usageLook) unavailable(c *ui.Context, s *ui.SVG, title, description string, action func()) {
	ui.Column(c).Role(ui.RoleStatus).Grow(1).Center().Gap(u.px(8)).Padding(u.px(24)).Children(func() {
		u.icon(c, s, 28, u.p.faint)
		u.text(c, title, 15).FontWeight(600)
		u.text(c, description, 12).TextColor(u.p.secondary).TextAlign(ui.Center).MaxWidth(u.px(420))
		if action != nil {
			action()
		}
	})
}

// pressable gives a control its hover and pressed fills; a press darkens it
// at once, so the click feels heard.
func (u usageLook) pressable(e ui.Element, disabled bool) ui.Element {
	switch {
	case disabled:
		return e.Disabled(true).Opacity(0.4)
	case e.Pressed():
		e.Background(u.p.selected)
	case e.Hovered():
		e.Background(u.p.hover)
	}
	return e.Cursor(ui.CursorPointer)
}

// toolButton is a borderless icon button of the toolbar.
func (u usageLook) toolButton(c *ui.Context, s *ui.SVG, label, tip string, disabled bool) bool {
	b := ui.ButtonBase(c).Size(u.px(30), u.px(28)).Radius(u.px(7)).Label(label).Tooltip(tip)
	u.pressable(b, disabled).Children(func() { u.icon(c, s, 15, u.p.secondary) })
	return b.Clicked()
}

// textButton is a borderless text button.
func (u usageLook) textButton(c *ui.Context, label string) bool {
	b := ui.ButtonBase(c).Label(label).Padding(u.px(3), u.px(6)).Radius(u.px(6))
	u.pressable(b, false).Children(func() { u.text(c, label, 12).TextColor(u.p.secondary) })
	return b.Clicked()
}

// pushButton is a bordered button; primary fills it with the accent. name
// is its accessible name when the label alone does not say what it acts on.
func (u usageLook) pushButton(c *ui.Context, label, name string, primary, disabled bool) bool {
	p := u.p
	if name == "" {
		name = label
	}
	b := ui.ButtonBase(c).Label(name).Height(u.px(26)).PaddingX(u.px(12)).Radius(u.px(7)).Cursor(ui.CursorPointer)
	color := p.text
	if primary {
		b.Background(p.accent)
		color = ui.Hex("#ffffff")
		if b.Pressed() && !disabled {
			b.Background(p.accent.Mix(p.text, 0.15))
		}
	} else {
		b.Background(p.card).Border(1, p.line)
		if b.Pressed() && !disabled {
			b.Background(p.selected)
		} else if b.Hovered() && !disabled {
			b.Background(p.hover.Over(p.card))
		}
	}
	if disabled {
		b.Disabled(true).Opacity(0.45)
	}
	b.Children(func() { u.text(c, label, 12).FontWeight(500).TextColor(color).NoWrap() })
	return b.Clicked()
}

// segmented chooses one of labels; it reports a new choice. fill stretches
// the segments to the width they are given.
func (u usageLook) segmented(c *ui.Context, selected *int, labels []string, label, tip string, disabled, fill bool) bool {
	p := u.p
	parts := ui.SegmentedBase(c, selected, len(labels))
	parts.Track.Padding(2).Gap(2).Radius(u.px(8)).Background(p.segTrack).Label(label)
	if tip != "" {
		parts.Track.Tooltip(tip)
	}
	if disabled {
		parts.Track.Disabled(true)
	}
	parts.Track.Children(func() {
		for i, l := range labels {
			s := parts.Segment(i).Height(u.px(24)).PaddingX(u.px(12)).Radius(u.px(6)).Label(l)
			if fill {
				s.Grow(1).Basis(0).PaddingX(u.px(6))
			}
			color, weight := p.secondary, 400
			switch {
			case i == *selected:
				s.Background(p.segThumb).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.12))
				color, weight = p.text, 500
			case disabled:
				s.Opacity(0.5)
			case s.Hovered():
				color = p.text
			}
			s.Children(func() { u.text(c, l, 12).FontWeight(weight).TextColor(color).NoWrap() })
		}
	})
	return parts.Track.Changed()
}

// tokensInfo is the info icon whose tooltip turns credits into each
// model's tokens.
func (u usageLook) tokensInfo(c *ui.Context, tokens string) {
	u.info(c, L("In tokens"), tokens, 11)
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
