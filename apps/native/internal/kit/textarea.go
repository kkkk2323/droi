package kit

import "github.com/egoist/mygo/ui"

// AreaStyle is how a TextArea looks: its padding (top, right, bottom,
// left), font size and line height, and the lines it grows between.
type AreaStyle struct {
	Pad          [4]float32
	Size, Line   float32
	Mono         bool
	MinLines     int
	MaxHeight    float32
	Color, Muted ui.Color
}

// TextArea is a text area as high as its text, as CSS's field-sizing:
// content makes the web Client's, from MinLines up to MaxHeight, where it
// scrolls. MyGo's own is three lines at least and paints no placeholder
// over a text area, so the height is set here from the text measured at
// the width of the last frame, and the placeholder is a text laid over it.
func (k *Kit) TextArea(c *ui.Context, value *string, label, placeholder string, s AreaStyle) *ui.Element {
	font := k.Sans
	if s.Mono {
		font = k.Mono
	}
	size, line := k.Px(s.Size), k.Px(s.Line)
	pad := [4]float32{k.Px(s.Pad[0]), k.Px(s.Pad[1]), k.Px(s.Pad[2]), k.Px(s.Pad[3])}
	var in *ui.Element
	box := ui.Box(c).Grow(1).MinWidth(0)
	box.Children(func() {
		in = ui.TextAreaBase(c, value).Label(label).FillWidth().Padding(pad[0], pad[1], pad[2], pad[3]).
			Font(font).FontSize(size).FixedLineHeight(line).TextColor(s.Color)
		w := in.Bounds().W - pad[1] - pad[3]
		lines := float32(max(s.MinLines, 1))
		if *value != "" && w > 0 {
			span := ui.Span{Text: *value + "\u200b", Font: font, Size: size}
			_, h := c.MeasureText(w, span)
			span.Text = "x"
			// MeasureText lays lines out at the font's own height, not line's.
			if _, one := c.MeasureText(0, span); one > 0 {
				lines = max(lines, float32(int(h/one+0.5)))
			}
		}
		h := lines*line + pad[0] + pad[2]
		if s.MaxHeight > 0 {
			h = min(h, k.Px(s.MaxHeight))
		}
		in.Height(h)
		if *value == "" && placeholder != "" {
			ui.Text(c, placeholder).Font(font).FontSize(size).FixedLineHeight(line).TextColor(s.Muted).SingleLine().
				Absolute().Top(pad[0]).Left(pad[3]).Right(pad[1]).Role(ui.RoleNone).PassThrough()
		}
	})
	return in
}
