package sessionview

import (
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/mermaid"
)

// diagramKey is a laid-out diagram's source and label size.
type diagramKey struct {
	src  string
	size float32
}

// diagram is a Mermaid block laid out once, with its shapes as the
// Painter draws them: laying out is the costly part, and the transcript
// builds its blocks again as it streams.
type diagram struct {
	scene *mermaid.Scene
	err   error
	draws []shapeDraw // per shape
	code  bool        // the block shows its source, not the drawing
}

// shapeDraw is a shape as the Painter draws it. The Painter draws a path
// from a mask of its bounds in the text atlas, which a few long edges or
// big boxes fill up, leaving the frame's later shapes and new glyphs
// undrawn; so a box of axis-aligned sides draws as a rectangle, and a big
// outline strokes in pieces: its horizontal and vertical sides as lines,
// the rest as short paths.
type shapeDraw struct {
	box    *ui.Rect
	fill   *ui.Path
	stroke []*ui.Path   // the outline, its pieces, or its dashes
	lines  [][4]float32 // x0, y0, x1, y1
}

// splitArea is the area of an outline's bounds, in DIPs, past which it
// strokes in pieces, and maxPiece the longest of its slanted lines.
const (
	splitArea = 160 * 160
	maxPiece  = 128
)

// maxDiagrams bounds a page's cache of laid-out diagrams.
const maxDiagrams = 64

func (m *Markdown) diagram(src string, size float32) *diagram {
	key := diagramKey{src, size}
	if d := m.diagrams[key]; d != nil {
		return d
	}
	if m.diagrams == nil || len(m.diagrams) >= maxDiagrams {
		m.diagrams = map[diagramKey]*diagram{}
	}
	d := &diagram{}
	d.scene, d.err = mermaid.Render(src, mermaid.Options{FontSize: size})
	if d.err == nil {
		d.draws = make([]shapeDraw, len(d.scene.Shapes))
		for i, s := range d.scene.Shapes {
			d.draws[i] = drawOf(s)
		}
	}
	m.diagrams[key] = d
	return d
}

// mermaidBlock draws a closed ```mermaid block as its diagram, with a
// button to its source; one still streaming, or one it cannot draw, stays
// a code block.
func (m *Markdown) mermaidBlock(c *ui.Context, k *kit.Kit, b *markdown.CodeBlockView, em float32) {
	if b.Open {
		markdown.DefaultCodeBlock(c, b)
		return
	}
	d := m.diagram(b.Code(), float32(math.Round(float64(em*0.8*2)))/2)
	if d.err != nil {
		markdown.DefaultCodeBlock(c, b)
		return
	}
	t := k.T
	box := ui.Column(c).Direction(ui.LeftToRight).Background(t.Background).Border(1, t.Border).Radius(k.Px(10)).AlignSelf(ui.Stretch).Clip()
	box.Role(ui.RoleGroup).Label(l10n.L("Mermaid diagram"))
	box.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(em*0.25).Padding(em*0.35, em*0.4, 0, em*0.75).Children(func() {
			ui.Text(c, "mermaid").FontSize(em * 0.8).TextColor(t.MutedForeground).Unselectable()
			ui.Spacer(c)
			label := l10n.L("Show source")
			if d.code {
				label = l10n.L("Show diagram")
			}
			btn := ui.ButtonBase(c).Padding(em*0.2, em*0.45).Radius(5).Label(label).Cursor(ui.CursorPointer)
			if btn.Hovered() {
				btn.Background(t.Foreground.Alpha(0.08))
			}
			btn.Children(func() { ui.Text(c, label).FontSize(em * 0.8).TextColor(t.MutedForeground) })
			if btn.Clicked() {
				d.code = !d.code
			}
			b.CopyButton(c)
		})
		if d.code {
			b.Body(c)
			return
		}
		s := d.scene
		pad := em * 0.75
		ui.ScrollHorizontal(c).AlignSelf(ui.Stretch).Children(func() {
			ui.Box(c).Role(ui.RoleImage).Label(l10n.L("Diagram: %s", s.Kind)).Size(s.Width+2*pad, s.Height+2*pad).Shrink(0).
				Draw(func(p *ui.Painter, r ui.Rect) { paintDiagram(p, k, d, r.X+pad, r.Y+pad) })
		})
	})
}

// paintDiagram draws a laid-out diagram with its top-left corner at (x, y),
// in the theme's colors, its labels in the monospace font it was measured
// for.
func paintDiagram(p *ui.Painter, k *kit.Kit, d *diagram, x, y float32) {
	p.Transform(ui.Translation(x, y), 0, 0, func() {
		for i, s := range d.scene.Shapes {
			dr := d.draws[i]
			fill, filled := diagramColor(k, s.Fill)
			stroke, stroked := diagramColor(k, s.Stroke)
			stroked = stroked && s.StrokeWidth > 0
			if r := dr.box; r != nil {
				if filled {
					p.Fill(*r, fill, 0)
				}
				if w := s.StrokeWidth; stroked {
					// Stroke draws inside the rectangle; a path's stroke is
					// centred on its outline.
					p.Stroke(ui.Rect{X: r.X - w/2, Y: r.Y - w/2, W: r.W + w, H: r.H + w}, stroke, 0, w)
				}
				continue
			}
			if filled && dr.fill != nil {
				p.FillPath(dr.fill, fill)
			}
			if stroked {
				for _, path := range dr.stroke {
					p.StrokePath(path, s.StrokeWidth, stroke)
				}
				for _, l := range dr.lines {
					p.Line(l[0], l[1], l[2], l[3], s.StrokeWidth, stroke)
				}
			}
		}
		for _, tx := range d.scene.Texts {
			color, ok := diagramColor(k, tx.Color)
			if !ok {
				continue
			}
			weight := 400
			if tx.Bold {
				weight = 600
			}
			span := ui.Span{Text: tx.Text, Font: k.Mono, Size: tx.Size, Weight: weight, Color: color}
			w, h := p.MeasureText(0, span)
			left := tx.X
			switch tx.Align {
			case mermaid.AlignCenter:
				left += (tx.W - w) / 2
			case mermaid.AlignEnd:
				left += tx.W - w
			}
			p.RichText(left, tx.Y+(tx.H-h)/2, 0, span)
		}
	})
}

// diagramColor is a diagram's paint in the theme; false for none. Every
// color is opaque, so that the segments of an outline do not show darker
// where they meet.
func diagramColor(k *kit.Kit, paint mermaid.Paint) (ui.Color, bool) {
	t := k.T
	switch paint {
	case mermaid.PaintBackground:
		return t.Background, true
	case mermaid.PaintSurface:
		return t.Card, true
	case mermaid.PaintSurfaceAlt:
		return t.Muted.Alpha(0.6).Over(t.Background), true
	case mermaid.PaintBorder:
		return t.Foreground.Alpha(0.45).Over(t.Background), true
	case mermaid.PaintLine:
		return t.MutedForeground, true
	case mermaid.PaintForeground:
		return t.Foreground, true
	case mermaid.PaintMuted:
		return t.MutedForeground, true
	case mermaid.PaintAccent:
		return t.Primary, true
	}
	return ui.Color{}, false
}

// drawOf is how a shape draws: dashed, as one path per dash (the Painter
// strokes paths solid) and unfilled.
func drawOf(s mermaid.Shape) shapeDraw {
	if len(s.Dash) > 0 {
		return shapeDraw{stroke: dashes(flatten(s.Path), s.Dash)}
	}
	if r, ok := axisBox(s.Path); ok {
		return shapeDraw{box: &r}
	}
	whole := new(ui.Path)
	for _, op := range s.Path {
		switch op.Kind {
		case mermaid.MoveTo:
			whole.MoveTo(op.X, op.Y)
		case mermaid.LineTo:
			whole.LineTo(op.X, op.Y)
		case mermaid.QuadTo:
			whole.QuadTo(op.X1, op.Y1, op.X, op.Y)
		case mermaid.CubicTo:
			whole.CubeTo(op.X1, op.Y1, op.X2, op.Y2, op.X, op.Y)
		case mermaid.Close:
			whole.Close()
		}
	}
	d := shapeDraw{fill: whole, stroke: []*ui.Path{whole}}
	if b := whole.Bounds(); b.W*b.H > splitArea {
		d.lines, d.stroke = pieces(s.Path)
	}
	return d
}

// axisBox is the rectangle of an outline that is one closed run of four
// axis-aligned sides.
func axisBox(ops []mermaid.Op) (ui.Rect, bool) {
	var pts [][2]float32
	closed := false
	for i, op := range ops {
		switch {
		case op.Kind == mermaid.MoveTo && i == 0, op.Kind == mermaid.LineTo:
			pts = append(pts, [2]float32{op.X, op.Y})
		case op.Kind == mermaid.Close && i == len(ops)-1:
			closed = true
		default:
			return ui.Rect{}, false
		}
	}
	if len(pts) == 5 && pts[4] == pts[0] {
		pts, closed = pts[:4], true
	}
	if !closed || len(pts) != 4 {
		return ui.Rect{}, false
	}
	for i, a := range pts {
		b := pts[(i+1)%4]
		if (a[0] == b[0]) == (a[1] == b[1]) {
			return ui.Rect{}, false
		}
	}
	x0, x1 := min(pts[0][0], pts[2][0]), max(pts[0][0], pts[2][0])
	y0, y1 := min(pts[0][1], pts[2][1]), max(pts[0][1], pts[2][1])
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, true
}

// pieces cuts an outline into its horizontal and vertical lines, and
// paths of its curves and of its slanted lines, cut maxPiece long.
func pieces(ops []mermaid.Op) (lines [][4]float32, paths []*ui.Path) {
	var start, at [2]float32
	line := func(to [2]float32) {
		if at == to {
			return
		}
		if at[0] == to[0] || at[1] == to[1] {
			lines = append(lines, [4]float32{at[0], at[1], to[0], to[1]})
			return
		}
		n := math.Ceil(math.Hypot(float64(to[0]-at[0]), float64(to[1]-at[1])) / maxPiece)
		from := at
		for i := 1.0; i <= n; i++ {
			next := to
			if i < n {
				k := float32(i / n)
				next = [2]float32{at[0] + (to[0]-at[0])*k, at[1] + (to[1]-at[1])*k}
			}
			paths = append(paths, new(ui.Path).MoveTo(from[0], from[1]).LineTo(next[0], next[1]))
			from = next
		}
	}
	for _, op := range ops {
		to := [2]float32{op.X, op.Y}
		switch op.Kind {
		case mermaid.MoveTo:
			start = to
		case mermaid.LineTo:
			line(to)
		case mermaid.QuadTo:
			paths = append(paths, new(ui.Path).MoveTo(at[0], at[1]).QuadTo(op.X1, op.Y1, op.X, op.Y))
		case mermaid.CubicTo:
			paths = append(paths, new(ui.Path).MoveTo(at[0], at[1]).CubeTo(op.X1, op.Y1, op.X2, op.Y2, op.X, op.Y))
		case mermaid.Close:
			line(start)
			to = start
		}
		at = to
	}
	return lines, paths
}

// flatten turns a path's curves into lines: polylines, one per subpath.
func flatten(ops []mermaid.Op) [][][2]float32 {
	var lines [][][2]float32
	var cur [][2]float32
	var start, at [2]float32
	end := func() {
		if len(cur) > 1 {
			lines = append(lines, cur)
		}
		cur = nil
	}
	const steps = 8
	for _, op := range ops {
		switch op.Kind {
		case mermaid.MoveTo:
			end()
			at = [2]float32{op.X, op.Y}
			start = at
			cur = [][2]float32{at}
		case mermaid.LineTo:
			at = [2]float32{op.X, op.Y}
			cur = append(cur, at)
		case mermaid.QuadTo:
			p0 := at
			for i := 1; i <= steps; i++ {
				t := float32(i) / steps
				u := 1 - t
				cur = append(cur, [2]float32{u*u*p0[0] + 2*u*t*op.X1 + t*t*op.X, u*u*p0[1] + 2*u*t*op.Y1 + t*t*op.Y})
			}
			at = [2]float32{op.X, op.Y}
		case mermaid.CubicTo:
			p0 := at
			for i := 1; i <= steps; i++ {
				t := float32(i) / steps
				u := 1 - t
				cur = append(cur, [2]float32{
					u*u*u*p0[0] + 3*u*u*t*op.X1 + 3*u*t*t*op.X2 + t*t*t*op.X,
					u*u*u*p0[1] + 3*u*u*t*op.Y1 + 3*u*t*t*op.Y2 + t*t*t*op.Y,
				})
			}
			at = [2]float32{op.X, op.Y}
		case mermaid.Close:
			cur = append(cur, start)
			at = start
		}
	}
	end()
	return lines
}

// dashes cuts polylines into the dashes of an on/off pattern, which runs on
// across their corners.
func dashes(lines [][][2]float32, pattern []float32) []*ui.Path {
	var total float32
	for _, d := range pattern {
		total += d
	}
	if total <= 0 {
		return nil
	}
	var out []*ui.Path
	for _, line := range lines {
		idx, left, on := 0, pattern[0], true
		var dash *ui.Path
		for i := 1; i < len(line); i++ {
			a, b := line[i-1], line[i]
			seg := float32(math.Hypot(float64(b[0]-a[0]), float64(b[1]-a[1])))
			pos := float32(0)
			for seg-pos > 0 {
				step := min(left, seg-pos)
				x0, y0 := a[0]+(b[0]-a[0])*pos/seg, a[1]+(b[1]-a[1])*pos/seg
				x1, y1 := a[0]+(b[0]-a[0])*(pos+step)/seg, a[1]+(b[1]-a[1])*(pos+step)/seg
				if on {
					if dash == nil {
						dash = new(ui.Path).MoveTo(x0, y0)
					}
					dash.LineTo(x1, y1)
				}
				pos += step
				left -= step
				if left <= 0 {
					if on && dash != nil {
						out = append(out, dash)
						dash = nil
					}
					idx = (idx + 1) % len(pattern)
					left, on = pattern[idx], !on
				}
			}
		}
		if dash != nil {
			out = append(out, dash)
		}
	}
	return out
}
