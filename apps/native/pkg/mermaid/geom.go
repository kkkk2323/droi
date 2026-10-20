package mermaid

import "math"

type path struct{ ops []Op }

func (p *path) move(x, y float64) { p.ops = append(p.ops, Op{Kind: MoveTo, X: f32(x), Y: f32(y)}) }
func (p *path) line(x, y float64) { p.ops = append(p.ops, Op{Kind: LineTo, X: f32(x), Y: f32(y)}) }
func (p *path) close()            { p.ops = append(p.ops, Op{Kind: Close}) }

func (p *path) quad(cx, cy, x, y float64) {
	p.ops = append(p.ops, Op{Kind: QuadTo, X1: f32(cx), Y1: f32(cy), X: f32(x), Y: f32(y)})
}

func (p *path) cubic(x1, y1, x2, y2, x, y float64) {
	p.ops = append(p.ops, Op{Kind: CubicTo, X1: f32(x1), Y1: f32(y1), X2: f32(x2), Y2: f32(y2), X: f32(x), Y: f32(y)})
}

func rectPath(x, y, w, h float64) []Op {
	return polyPath(x, y, x+w, y, x+w, y+h, x, y+h)
}

// polyPath is a closed polygon through x, y pairs.
func polyPath(pts ...float64) []Op {
	var p path
	p.move(pts[0], pts[1])
	for i := 2; i+1 < len(pts); i += 2 {
		p.line(pts[i], pts[i+1])
	}
	p.close()
	return p.ops
}

// kappa places the handles of a cubic that draws a quarter of a circle.
const kappa = 0.5522847498

func ellipsePath(cx, cy, rx, ry float64) []Op {
	var p path
	kx, ky := rx*kappa, ry*kappa
	p.move(cx+rx, cy)
	p.cubic(cx+rx, cy+ky, cx+kx, cy+ry, cx, cy+ry)
	p.cubic(cx-kx, cy+ry, cx-rx, cy+ky, cx-rx, cy)
	p.cubic(cx-rx, cy-ky, cx-kx, cy-ry, cx, cy-ry)
	p.cubic(cx+kx, cy-ry, cx+rx, cy-ky, cx+rx, cy)
	p.close()
	return p.ops
}

// roundRectPath is a rectangle whose corners are rounded by r.
func roundRectPath(x, y, w, h, r float64) []Op {
	r = math.Min(r, math.Min(w, h)/2)
	if r <= 0 {
		return rectPath(x, y, w, h)
	}
	var p path
	k := r * (1 - kappa)
	p.move(x+r, y)
	p.line(x+w-r, y)
	p.cubic(x+w-k, y, x+w, y+k, x+w, y+r)
	p.line(x+w, y+h-r)
	p.cubic(x+w, y+h-k, x+w-k, y+h, x+w-r, y+h)
	p.line(x+r, y+h)
	p.cubic(x+k, y+h, x, y+h-k, x, y+h-r)
	p.line(x, y+r)
	p.cubic(x, y+k, x+k, y, x+r, y)
	p.close()
	return p.ops
}

type pt struct{ x, y float64 }

// cleanRoute drops repeated points and the points in the middle of a straight run.
func cleanRoute(in []pt) []pt {
	out := make([]pt, 0, len(in))
	for _, p := range in {
		if n := len(out); n > 0 && math.Abs(out[n-1].x-p.x) < 0.01 && math.Abs(out[n-1].y-p.y) < 0.01 {
			continue
		}
		if n := len(out); n >= 2 {
			a, b := out[n-2], out[n-1]
			cross := (b.x-a.x)*(p.y-b.y) - (b.y-a.y)*(p.x-b.x)
			dot := (b.x-a.x)*(p.x-b.x) + (b.y-a.y)*(p.y-b.y)
			if math.Abs(cross) < 0.01 && dot > 0 {
				out[n-1] = p
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

func unit(a, b pt) (float64, float64) {
	dx, dy := b.x-a.x, b.y-a.y
	l := math.Hypot(dx, dy)
	if l == 0 {
		return 0, 0
	}
	return dx / l, dy / l
}

func dist(a, b pt) float64 { return math.Hypot(b.x-a.x, b.y-a.y) }

// routePath strokes a route of straight runs, each corner rounded by up to
// radius: a run that ends at the route's end can be taken whole, a run shared
// by two corners half each.
func routePath(pts []pt, radius float64) []Op {
	var p path
	if len(pts) < 2 {
		return nil
	}
	p.move(pts[0].x, pts[0].y)
	last := len(pts) - 1
	for i := 1; i < last; i++ {
		before := dist(pts[i-1], pts[i])
		after := dist(pts[i], pts[i+1])
		if i > 1 {
			before /= 2
		}
		if i+1 < last {
			after /= 2
		}
		r := math.Min(radius, math.Min(before, after))
		ux, uy := unit(pts[i-1], pts[i])
		vx, vy := unit(pts[i], pts[i+1])
		c := pts[i]
		p.line(c.x-ux*r, c.y-uy*r)
		p.quad(c.x, c.y, c.x+vx*r, c.y+vy*r)
	}
	p.line(pts[last].x, pts[last].y)
	return p.ops
}

// trimEnd moves the end of a route back along its last run by d, and returns
// where it was and the way it was heading.
func trimEnd(pts []pt, d float64) (tip pt, dx, dy float64) {
	n := len(pts)
	tip = pts[n-1]
	dx, dy = unit(pts[n-2], pts[n-1])
	d = math.Min(d, dist(pts[n-2], pts[n-1])*0.9)
	pts[n-1] = pt{tip.x - dx*d, tip.y - dy*d}
	return tip, dx, dy
}

func reverse(pts []pt) {
	for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
		pts[i], pts[j] = pts[j], pts[i]
	}
}

// scene collects what a layout draws, in drawing coordinates that are moved
// to start at the margin when it is finished.
type scene struct {
	kind   string
	shapes []Shape
	texts  []Text
}

func (s *scene) shape(ops []Op, fill, stroke Paint, width float64, dash []float32) {
	if len(ops) == 0 {
		return
	}
	s.shapes = append(s.shapes, Shape{Path: ops, Fill: fill, Stroke: stroke, StrokeWidth: f32(width), Dash: dash})
}

// label places l centered on (cx, cy), one Text a line.
func (s *scene) label(l label, cx, cy float64, color Paint, bold bool) {
	s.labelAt(l, cx-l.w/2, cy-l.h/2, color, bold, AlignCenter)
}

func (s *scene) labelAt(l label, x, y float64, color Paint, bold bool, align Align) {
	for i, line := range l.lines {
		if line == "" {
			continue
		}
		s.texts = append(s.texts, Text{
			X: f32(x), Y: f32(y + float64(i)*l.lh), W: f32(l.w), H: f32(l.lh),
			Text: line, Size: f32(l.size), Bold: bold, Align: align, Color: color,
		})
	}
}

// finish moves everything so the drawing starts pad from the top left, and
// sizes the Scene to hold it with pad to spare.
func (s *scene) finish(pad float64, title string) *Scene {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	grow := func(x, y float64) {
		minX, minY = math.Min(minX, x), math.Min(minY, y)
		maxX, maxY = math.Max(maxX, x), math.Max(maxY, y)
	}
	for _, sh := range s.shapes {
		half := float64(sh.StrokeWidth) / 2
		for _, op := range sh.Path {
			if op.Kind == Close {
				continue
			}
			pts := [][2]float32{{op.X, op.Y}}
			if op.Kind == QuadTo || op.Kind == CubicTo {
				pts = append(pts, [2]float32{op.X1, op.Y1})
			}
			if op.Kind == CubicTo {
				pts = append(pts, [2]float32{op.X2, op.Y2})
			}
			for _, q := range pts {
				grow(float64(q[0])-half, float64(q[1])-half)
				grow(float64(q[0])+half, float64(q[1])+half)
			}
		}
	}
	for _, t := range s.texts {
		grow(float64(t.X), float64(t.Y))
		grow(float64(t.X+t.W), float64(t.Y+t.H))
	}
	if minX > maxX {
		minX, minY, maxX, maxY = 0, 0, 0, 0
	}
	dx, dy := f32(pad-minX), f32(pad-minY)
	for i := range s.shapes {
		ops := s.shapes[i].Path
		for j := range ops {
			if ops[j].Kind == Close {
				continue
			}
			ops[j].X += dx
			ops[j].Y += dy
			if ops[j].Kind == QuadTo || ops[j].Kind == CubicTo {
				ops[j].X1 += dx
				ops[j].Y1 += dy
			}
			if ops[j].Kind == CubicTo {
				ops[j].X2 += dx
				ops[j].Y2 += dy
			}
		}
	}
	for i := range s.texts {
		s.texts[i].X += dx
		s.texts[i].Y += dy
	}
	return &Scene{
		Kind:   s.kind,
		Width:  f32(math.Ceil(maxX - minX + 2*pad)),
		Height: f32(math.Ceil(maxY - minY + 2*pad)),
		Shapes: s.shapes,
		Texts:  s.texts,
		Title:  title,
	}
}

// arrowHead is a filled triangle whose tip is at (x, y), pointing along (dx, dy).
func arrowHead(x, y, dx, dy, length float64) []Op {
	nx, ny := -dy, dx
	bx, by := x-dx*length, y-dy*length
	half := length * 0.45
	return polyPath(x, y, bx+nx*half, by+ny*half, bx-nx*half, by-ny*half)
}

// crossHead is the two strokes of an x centered size back from (x, y).
func crossHead(x, y, dx, dy, size float64) []Op {
	nx, ny := -dy, dx
	cx, cy := x-dx*size, y-dy*size
	a := size * 0.8
	var p path
	p.move(cx-(dx+nx)*a, cy-(dy+ny)*a)
	p.line(cx+(dx+nx)*a, cy+(dy+ny)*a)
	p.move(cx-(dx-nx)*a, cy-(dy-ny)*a)
	p.line(cx+(dx-nx)*a, cy+(dy-ny)*a)
	return p.ops
}
