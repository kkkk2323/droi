package dashkit

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// DrawRing strokes a ring around (cx, cy) in track, and over it, in color,
// the arc of ratio of it, clockwise from the top.
func DrawRing(p *ui.Painter, cx, cy, rad, width float32, ratio float64, track, color ui.Color) {
	p.StrokePath(new(ui.Path).Circle(cx, cy, rad), width, track)
	if ratio <= 0 {
		return
	}
	ratio = math.Min(ratio, 1)
	arc := new(ui.Path)
	steps := max(2, int(48*ratio))
	for i := 0; i <= steps; i++ {
		a := -math.Pi/2 + 2*math.Pi*ratio*float64(i)/float64(steps)
		x, y := cx+rad*float32(math.Cos(a)), cy+rad*float32(math.Sin(a))
		if i == 0 {
			arc.MoveTo(x, y)
		} else {
			arc.LineTo(x, y)
		}
	}
	p.StrokePath(arc, width, color)
}
