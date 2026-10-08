package kit

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// AreaStyle is how a TextArea looks: its padding (top, right, bottom,
// left), font size and line height, and the lines it grows between.
type AreaStyle struct {
	Pad        [4]float32
	Size, Line float32
	Mono       bool
	// MinLines and MaxLines are the lines it grows between; it grows
	// without end when MaxLines is 0.
	MinLines, MaxLines int
	Color              ui.Color
}

// TextArea is a text area as high as its text, as CSS's field-sizing:
// content makes the web Client's, from MinLines up to MaxLines, where it
// scrolls.
func (k *Kit) TextArea(c *ui.Context, value *string, label, placeholder string, s AreaStyle) ui.Element {
	font := k.Sans
	if s.Mono {
		font = k.Mono
	}
	most := s.MaxLines
	if most <= 0 {
		most = math.MaxInt32
	}
	var in ui.Element
	ui.Box(c).Grow(1).MinWidth(0).Children(func() {
		in = ui.TextAreaBase(c, value).Label(label).Placeholder(placeholder).FillWidth().
			Padding(k.Px(s.Pad[0]), k.Px(s.Pad[1]), k.Px(s.Pad[2]), k.Px(s.Pad[3])).
			Font(font).FontSize(k.Px(s.Size)).FixedLineHeight(k.Px(s.Line)).TextColor(s.Color).
			Lines(max(s.MinLines, 1), most)
	})
	return in
}
