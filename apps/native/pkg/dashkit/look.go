package dashkit

import (
	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/kit"
)

// Look draws droid-proxy's controls and surfaces in P, at K's text size.
type Look struct {
	K *kit.Kit
	P *Palette
}

// New is the Look of k in the palette of c's theme: Dark when it is dark.
func New(k *kit.Kit, c *ui.Context) Look {
	p := &Light
	if c.Theme().Dark {
		p = &Dark
	}
	return Look{K: k, P: p}
}

// Px is v pixels at the kit's size.
func (u Look) Px(v float32) float32 { return u.K.Px(v) }

// Themed returns MyGo's theme set to the palette, for the widgets used as
// they are: tables, text inputs, dialogs, spinners and scroll bars.
func (u Look) Themed(base *ui.Theme) *ui.Theme {
	t, p := *base, u.P
	t.Background, t.Surface = p.Card, p.Card
	t.SurfaceHover, t.SurfacePressed = p.Hover, p.Selected
	t.Border = p.Line
	t.Text, t.TextMuted = p.Text, p.Secondary
	t.Accent, t.Danger, t.Warning, t.Success = p.Accent, p.Danger, p.Warning, p.Green
	t.Radius = u.Px(7)
	t.FontSize = u.Px(13)
	return &t
}

// Tone is t's color in the palette.
func (u Look) Tone(t Tone) ui.Color {
	p := u.P
	switch t {
	case TonePrimary:
		return p.Text
	case ToneTertiary:
		return p.Tertiary
	case ToneAccent:
		return p.Accent
	case ToneGreen:
		return p.Green
	case ToneWarning:
		return p.Warning
	case ToneDanger:
		return p.Danger
	case ToneTrack:
		return p.Track
	}
	return p.Secondary
}

// Text is s at size pixels of the kit's size.
func (u Look) Text(c *ui.Context, s string, size float32) ui.Element {
	return ui.Text(c, s).FontSize(u.Px(size))
}

// Icon is s, size pixels square, in color.
func (u Look) Icon(c *ui.Context, s *ui.SVG, size float32, color ui.Color) ui.Element {
	return ui.Icon(c, s).Size(u.Px(size), u.Px(size)).Shrink(0).TextColor(color)
}

// Card is a white surface a hairline from the page.
func (u Look) Card(c *ui.Context) ui.Element {
	return ui.Column(c).Radius(u.Px(12)).Background(u.P.Card).Border(1, u.P.Line).Clip()
}

// Hairline separates the rows of a card.
func (u Look) Hairline(c *ui.Context) { ui.Box(c).Height(1).Shrink(0).Background(u.P.Line) }

// VHairline separates the cells of a card's row.
func (u Look) VHairline(c *ui.Context) {
	ui.Box(c).Width(1).Shrink(0).AlignSelf(ui.Stretch).Background(u.P.Line)
}

// Caps is a small section label above a group.
func (u Look) Caps(c *ui.Context, s string) ui.Element {
	return u.Text(c, s, 11).FontWeight(600).LetterSpacing(0.4).TextColor(u.P.Tertiary).NoWrap()
}

// SectionHead is a group's label, a note after it, and what goes on the
// right.
func (u Look) SectionHead(c *ui.Context, title, note string, right func()) {
	ui.Row(c).Gap(u.Px(8)).Padding(u.Px(2), u.Px(4), 0).Children(func() {
		u.Caps(c, title).Role(ui.RoleHeading)
		if note != "" {
			u.Text(c, note, 11).TextColor(u.P.Tertiary).FontFeatures("tnum").SingleLine().Shrink(1)
		}
		ui.Spacer(c)
		if right != nil {
			right()
		}
	})
}

// Info is the info icon whose tooltip explains a section.
func (u Look) Info(c *ui.Context, label, tip string, size float32) {
	ui.Box(c).Tooltip(tip).Label(label).Children(func() { u.Icon(c, IconInfo, size, u.P.Tertiary) })
}

// Meter is a capsule filled to value (0…100). mark, when 0…100, draws a
// tick there: where the fill would be at an even pace.
func (u Look) Meter(c *ui.Context, value float64, tone Tone, height float32, mark float64) ui.Element {
	v := float32(min(max(value, 0), 100)) / 100
	m := float32(mark) / 100
	height = u.Px(height)
	fill, track, tick := u.Tone(tone), u.P.Track, u.P.Text.Alpha(0.55)
	return ui.Box(c).Role(ui.RoleMeter).Grow(1).Basis(0).MinWidth(u.Px(24)).Height(height + 4).Draw(func(p *ui.Painter, r ui.Rect) {
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

// MetricStrip is numbers side by side in one card, a hairline apart: the
// figure, its name, then a note.
func (u Look) MetricStrip(c *ui.Context, metrics []MetricView) {
	p := u.P
	u.Card(c).Shrink(0).Children(func() {
		ui.Row(c).AlignItems(ui.Stretch).Children(func() {
			for i, m := range metrics {
				if i > 0 {
					u.VHairline(c)
				}
				ui.Column(c.Key(i)).Role(ui.RoleGroup).Label(m.Title).Grow(1).Basis(0).MinWidth(0).Gap(u.Px(3)).Padding(u.Px(14), u.Px(16)).Children(func() {
					ui.Row(c).Gap(u.Px(6)).AlignItems(ui.End).Children(func() {
						u.Text(c, m.Value, 22).FontWeight(600).LetterSpacing(-0.3).TextColor(u.Tone(PrimaryTone(m.Tone))).FontFeatures("tnum").SingleLine().Shrink(1)
						if m.Sub != "" {
							u.Text(c, m.Sub, 13).FontWeight(500).TextColor(p.Secondary).FontFeatures("tnum").NoWrap().Padding(0, 0, u.Px(3))
						}
					})
					u.Text(c, m.Title, 11).FontWeight(600).TextColor(p.Secondary).SingleLine()
					note := m.Note
					if note == "" {
						note = "\u00a0"
					}
					u.Text(c, note, 11).TextColor(p.Tertiary).FontFeatures("tnum").SingleLine()
				})
			}
		})
	})
}

// Badge is b's label on a tint of its tone; compact makes it smaller.
func (u Look) Badge(c *ui.Context, b Badge, compact bool) ui.Element {
	size, padX := float32(11), float32(7)
	if compact {
		size, padX = 10, 6
	}
	return u.Text(c, b.Label, size).FontWeight(500).TextColor(u.Tone(b.Tone)).
		Padding(1, u.Px(padX)).Radius(u.Px(6)).Background(u.Tone(b.Tone).Alpha(0.12)).NoWrap()
}

// Unavailable is a page's empty state: an icon, a title, a description and
// maybe an action.
func (u Look) Unavailable(c *ui.Context, s *ui.SVG, title, description string, action func()) {
	ui.Column(c).Role(ui.RoleStatus).Grow(1).Center().Gap(u.Px(8)).Padding(u.Px(24)).Children(func() {
		u.Icon(c, s, 28, u.P.Faint)
		u.Text(c, title, 15).FontWeight(600)
		u.Text(c, description, 12).TextColor(u.P.Secondary).TextAlign(ui.Center).MaxWidth(u.Px(420))
		if action != nil {
			action()
		}
	})
}

// Pressable gives a control its hover and pressed fills; a press darkens it
// at once, so the click feels heard.
func (u Look) Pressable(e ui.Element, disabled bool) ui.Element {
	switch {
	case disabled:
		return e.Disabled(true).Opacity(0.4)
	case e.Pressed():
		e.Background(u.P.Selected)
	case e.Hovered():
		e.Background(u.P.Hover)
	}
	return e.Cursor(ui.CursorPointer)
}

// ToolButton is a borderless icon button of the toolbar; it reports a
// click.
func (u Look) ToolButton(c *ui.Context, s *ui.SVG, label, tip string, disabled bool) bool {
	b := ui.ButtonBase(c).Size(u.Px(30), u.Px(28)).Radius(u.Px(8)).Label(label).Tooltip(tip)
	u.Pressable(b, disabled).Children(func() { u.Icon(c, s, 15, u.P.Secondary) })
	return b.Clicked()
}

// TextButton is a borderless text button; it reports a click.
func (u Look) TextButton(c *ui.Context, label string) bool {
	b := ui.ButtonBase(c).Label(label).Padding(u.Px(3), u.Px(6)).Radius(u.Px(6))
	u.Pressable(b, false).Children(func() { u.Text(c, label, 12).TextColor(u.P.Secondary) })
	return b.Clicked()
}

// PushButton is a bordered button; primary fills it with the accent. name
// is its accessible name when the label alone does not say what it acts on.
// It reports a click.
func (u Look) PushButton(c *ui.Context, label, name string, primary, disabled bool) bool {
	p := u.P
	if name == "" {
		name = label
	}
	b := ui.ButtonBase(c).Label(name).Height(u.Px(26)).PaddingX(u.Px(12)).Radius(u.Px(8)).Cursor(ui.CursorPointer)
	color := p.Text
	if primary {
		b.Background(p.Accent)
		color = ui.Hex("#ffffff")
		if b.Pressed() && !disabled {
			b.Background(p.Accent.Mix(p.Text, 0.15))
		}
	} else {
		b.Background(p.Card).Border(1, p.Line)
		if b.Pressed() && !disabled {
			b.Background(p.Selected)
		} else if b.Hovered() && !disabled {
			b.Background(p.Hover.Over(p.Card))
		}
	}
	if disabled {
		b.Disabled(true).Opacity(0.45)
	}
	b.Children(func() { u.Text(c, label, 12).FontWeight(500).TextColor(color).NoWrap() })
	return b.Clicked()
}

// Segmented chooses one of labels; it reports a new choice. fill stretches
// the segments to the width they are given.
func (u Look) Segmented(c *ui.Context, selected *int, labels []string, label, tip string, disabled, fill bool) bool {
	p := u.P
	parts := ui.SegmentedBase(c, selected, len(labels))
	parts.Track.Padding(2).Gap(2).Radius(u.Px(8)).Background(p.SegTrack).Label(label)
	if tip != "" {
		parts.Track.Tooltip(tip)
	}
	if disabled {
		parts.Track.Disabled(true)
	}
	parts.Track.Children(func() {
		for i, l := range labels {
			s := parts.Segment(i).Height(u.Px(24)).PaddingX(u.Px(12)).Radius(u.Px(6)).Label(l)
			if fill {
				s.Grow(1).Basis(0).PaddingX(u.Px(6))
			}
			color, weight := p.SegText, 400
			switch {
			case i == *selected:
				s.Background(p.SegThumb).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.14)).Shadow(0, 0, 0, 0.5, p.Line)
				color, weight = p.Text, 500
			case disabled:
				s.Opacity(0.5)
			case s.Hovered():
				color = p.Text
			}
			s.Children(func() { u.Text(c, l, 12).FontWeight(weight).TextColor(color).NoWrap() })
		}
	})
	return parts.Track.Changed()
}

// Dropdown is a select in the segmented controls' groove: the chosen
// option and a chevron, opening a menu of the options with a tick by the
// chosen one. It reports the value picked.
func (u Look) Dropdown(c *ui.Context, open *bool, label, value string, options []Option) (string, bool) {
	p := u.P
	current := value
	for _, o := range options {
		if o.Value == value {
			current = o.Label
		}
	}
	tr := ui.ButtonBase(c).Role(ui.RoleComboBox).Label(label).Expanded(*open).Height(u.Px(28)).MinWidth(u.Px(128)).MaxWidth(u.Px(240)).
		Gap(u.Px(8)).PaddingX(u.Px(10)).Radius(u.Px(8)).Background(p.SegTrack).Justify(ui.SpaceBetween).Cursor(ui.CursorPointer)
	color := p.SegText
	if tr.Hovered() || *open {
		color = p.Text
		tr.Background(p.SegTrack.Mix(p.Text, 0.06))
	}
	tr.Children(func() {
		u.Text(c, current, 12).TextColor(color).SingleLine().Shrink(1)
		u.Icon(c, IconChevronDown, 12, p.Secondary)
	})
	if tr.Clicked() {
		*open = !*open
	}
	picked, ok := "", false
	ui.PopoverBase(c, tr, open, func(panel ui.Element) {
		panel.Role(ui.RoleList).Label(label).Margin(u.Px(4), 0, 0, 0).MinWidth(tr.Bounds().W).Padding(u.Px(4)).
			Radius(u.Px(10)).Border(1, p.Line).Background(p.Card).Shadow(0, u.Px(8), u.Px(20), -u.Px(4), ui.RGBA(0, 0, 0, 0.16))
		ui.Scroll(c).MaxHeight(u.Px(312)).Children(func() {
			ui.Column(c).Gap(1).Children(func() {
				for _, o := range options {
					on := o.Value == value
					it := ui.ButtonBase(c.Key(o.Value)).Role(ui.RoleListItem).Label(o.Label).Checked(on).Height(u.Px(28)).
						Gap(u.Px(6)).PaddingX(u.Px(8)).Radius(u.Px(6)).Justify(ui.Start).Cursor(ui.CursorPointer)
					if it.Hovered() || it.Focused() {
						it.Background(p.Hover)
					}
					it.Children(func() {
						ui.Box(c).Size(u.Px(14), u.Px(14)).Center().Shrink(0).Children(func() {
							if on {
								u.Icon(c, IconTick, 12, p.Accent)
							}
						})
						u.Text(c, o.Label, 12).TextColor(p.Text).SingleLine()
					})
					if it.Clicked() {
						*open = false
						if !on {
							picked, ok = o.Value, true
						}
					}
				}
			})
		})
	})
	return picked, ok
}

// Tables gives a table's chosen row a light tint whose text keeps its
// colors: MyGo's fills it with the accent and turns the text white, which
// the cells' own colors do not read on.
func (u Look) Tables(c *ui.Context) (restore func()) {
	base := c.Theme()
	t := *base
	t.Accent, t.AccentText = u.P.Accent.Alpha(0.12).Over(u.P.Card), u.P.Text
	c.SetTheme(&t)
	return func() { c.SetTheme(base) }
}
