package mermaid

import "math"

// Sizes at a 16px label size, scaled to the size asked for. Most are Pele's.
const (
	fPadX      = 16
	fPadY      = 10
	fNodeSep   = 40
	fEdgeSep   = 16
	fRankSep   = 48
	fPortSep   = 20
	fGroupPad  = 16
	fCorner    = 12 // kept clear at each end of a straight side
	fRadius    = 10 // the most a bend in an edge is rounded by
	fArrow     = 9
	fLoopReach = 26
	fWrapCells = 26
	fDiamCells = 18
	fStroke    = 1.5
)

type nodeView struct {
	lab      label
	shape    shapeKind
	w, h, dy float64
}

func shapeSize(sh shapeKind, tw, th, s float64) (w, h, dy float64) {
	px, py := fPadX*s, fPadY*s
	switch sh {
	case shStadium:
		h = th + 2*py
		return tw + h*0.5 + 2*px - 8*s, h, 0
	case shSubroutine:
		return tw + 2*px + 16*s, th + 2*py, 0
	case shCylinder:
		w = math.Max(tw+2*px, 56*s)
		ry := cylinderRy(w, s)
		return w, th + 2*py + 2*ry, ry / 2
	case shCircle:
		d := math.Max(math.Hypot(tw, th)+16*s, 44*s)
		return d, d, 0
	case shDoubleCircle:
		d := math.Max(math.Hypot(tw, th)+26*s, 54*s)
		return d, d, 0
	case shDiamond:
		a, b := tw+16*s, th+8*s
		return a + 1.6*b, a/1.6 + b, 0
	case shHexagon, shLeanR, shLeanL, shTrapB, shTrapT:
		h = th + 2*py
		return tw + 2*px + h/2, h, 0
	case shOdd:
		h = th + 2*py
		return tw + 2*px + h/4, h, 0
	case shText:
		return tw + 8*s, th + 8*s, 0
	}
	return math.Max(tw+2*px, 2*px+12*s), th + 2*py, 0
}

func cylinderRy(w, s float64) float64 { return math.Min(12*s, math.Max(6*s, w/14)) }

// shapeSpan is how much of a side of the shape edges may spread along: the
// top and bottom, or the left and right when sideways.
func shapeSpan(sh shapeKind, w, h float64, sideways bool, s float64) float64 {
	c := fCorner * s
	side := w
	if sideways {
		side = h
	}
	var v float64
	switch sh {
	case shCircle, shDoubleCircle, shDiamond:
		v = side * 0.6
	case shStadium:
		v = w - h
		if sideways {
			v = h * 0.6
		}
	case shCylinder:
		v = w * 0.6
		if sideways {
			v = h - 2*c - 12*s
		}
	case shHexagon:
		v = w - h/2 - c
		if sideways {
			v = h * 0.5
		}
	case shLeanR, shLeanL, shTrapB, shTrapT, shOdd:
		v = w - h/2 - 2*c
		if sideways {
			v = 0
		}
	default:
		v = side - 2*c
	}
	return math.Max(0, v)
}

func bulge(t, half, depth float64) float64 {
	k := math.Min(1, math.Abs(t)/half)
	return depth * (1 - math.Sqrt(1-k*k))
}

// shapeInset is the distance from the bounding box to the outline on a side
// (0 top, 1 right, 2 bottom, 3 left), t along the side from its middle,
// rightward or downward.
func shapeInset(sh shapeKind, w, h float64, side int, t, s float64) float64 {
	across := side == 1 || side == 3
	switch sh {
	case shCircle, shDoubleCircle:
		if across {
			return bulge(t, h/2, w/2)
		}
		return bulge(t, w/2, h/2)
	case shStadium:
		if across {
			return bulge(t, h/2, h/2)
		}
	case shCylinder:
		if !across {
			return bulge(t, w/2, cylinderRy(w, s))
		}
	case shDiamond:
		if across {
			return math.Abs(t) * w / h
		}
		return math.Abs(t) * h / w
	case shHexagon:
		if across {
			return math.Abs(t) / 2
		}
	case shLeanR, shLeanL, shTrapB, shTrapT:
		if !across {
			return 0
		}
		// The slanted sides lean in by h/4 from one end of the side to the other.
		in := -1.0
		switch {
		case sh == shLeanR && side == 1, sh == shLeanL && side == 3, sh == shTrapT:
			in = 1
		}
		return math.Max(0, h/8+in*t/4)
	case shOdd:
		if side == 3 {
			return h / 4 * (1 - math.Min(1, math.Abs(t)/(h/2)))
		}
	}
	return 0
}

func drawShape(sc *scene, v nodeView, cx, cy, stroke, s float64) {
	w, h := v.w, v.h
	x0, y0, x1, y1 := cx-w/2, cy-h/2, cx+w/2, cy+h/2
	fill := func(ops []Op) { sc.shape(ops, PaintSurface, PaintBorder, stroke, nil) }
	line := func(ops []Op) { sc.shape(ops, PaintNone, PaintBorder, stroke, nil) }
	switch v.shape {
	case shRound:
		fill(roundRectPath(x0, y0, w, h, math.Min(10*s, h/2)))
	case shStadium:
		fill(roundRectPath(x0, y0, w, h, h/2))
	case shSubroutine:
		fill(rectPath(x0, y0, w, h))
		var p path
		p.move(x0+8*s, y0)
		p.line(x0+8*s, y1)
		p.move(x1-8*s, y0)
		p.line(x1-8*s, y1)
		line(p.ops)
	case shCylinder:
		rx, ry := w/2, cylinderRy(w, s)
		top, bottom := y0+ry, y1-ry
		k := kappa
		var p path
		p.move(x0, top)
		p.cubic(x0, top-ry*k, cx-rx*k, y0, cx, y0)
		p.cubic(cx+rx*k, y0, x1, top-ry*k, x1, top)
		p.line(x1, bottom)
		p.cubic(x1, bottom+ry*k, cx+rx*k, y1, cx, y1)
		p.cubic(cx-rx*k, y1, x0, bottom+ry*k, x0, bottom)
		p.close()
		fill(p.ops)
		var rim path
		rim.move(x0, top)
		rim.cubic(x0, top+ry*k, cx-rx*k, top+ry, cx, top+ry)
		rim.cubic(cx+rx*k, top+ry, x1, top+ry*k, x1, top)
		line(rim.ops)
	case shCircle:
		fill(ellipsePath(cx, cy, w/2, h/2))
	case shDoubleCircle:
		fill(ellipsePath(cx, cy, w/2, h/2))
		line(ellipsePath(cx, cy, w/2-5*s, h/2-5*s))
	case shDiamond:
		fill(polyPath(cx, y0, x1, cy, cx, y1, x0, cy))
	case shHexagon:
		m := h / 4
		fill(polyPath(x0+m, y0, x1-m, y0, x1, cy, x1-m, y1, x0+m, y1, x0, cy))
	case shOdd:
		fill(polyPath(x0, y0, x1, y0, x1, y1, x0, y1, x0+h/4, cy))
	case shLeanR:
		fill(polyPath(x0+h/4, y0, x1, y0, x1-h/4, y1, x0, y1))
	case shLeanL:
		fill(polyPath(x0, y0, x1-h/4, y0, x1, y1, x0+h/4, y1))
	case shTrapB:
		fill(polyPath(x0+h/4, y0, x1-h/4, y0, x1, y1, x0, y1))
	case shTrapT:
		fill(polyPath(x0, y0, x1, y0, x1-h/4, y1, x0+h/4, y1))
	case shText:
	default:
		fill(rectPath(x0, y0, w, h))
	}
	sc.label(v.lab, cx, cy+v.dy, PaintForeground, false)
}

type rect struct{ x0, y0, x1, y1 float64 }

func (r rect) contains(x, y float64) bool { return x >= r.x0 && x <= r.x1 && y >= r.y0 && y <= r.y1 }

// flowPlace is a flowchart laid out in drawing coordinates, before it is drawn.
type flowPlace struct {
	fc       *flowchart
	size, s  float64
	vertical bool
	views    []nodeView
	titles   []label
	// The box each item takes in the layout; a node is drawn at its middle at its own size.
	boxes  []rect
	edges  []placedEdge
	labels []labelPlace
}

type placedEdge struct {
	e        fedge
	src, dst int
	// The route, with its ends on the outlines of the nodes.
	pts []pt
}

type labelPlace struct {
	lab    label
	cx, cy float64
}

func (fp *flowPlace) nodeRect(i int) rect {
	b, v := fp.boxes[i], fp.views[i]
	cx, cy := (b.x0+b.x1)/2, (b.y0+b.y1)/2
	return rect{cx - v.w/2, cy - v.h/2, cx + v.w/2, cy + v.h/2}
}

func layoutFlowchart(fc *flowchart, size float64) *Scene {
	return drawFlowchart(placeFlowchart(fc, size))
}

func placeFlowchart(fc *flowchart, size float64) *flowPlace {
	s := size / 16
	edgeSize := size * 0.875
	vertical := fc.dir == "TB" || fc.dir == "BT"
	items := fc.items
	n := len(items)
	fp := &flowPlace{fc: fc, size: size, s: s, vertical: vertical, views: make([]nodeView, n), titles: make([]label, n)}
	cn := make([]cnode, n)
	for i, it := range items {
		c := cnode{parent: it.parent, seq: i}
		if it.group {
			lab := makeLabel(it.label, size, 0, it.markdown)
			fp.titles[i] = lab
			pad := fGroupPad * s
			top := pad
			if lab.h > 0 {
				top = lab.h + 12*s
			}
			// The title sits at the top of the drawn box, which is another side of the box in layout.
			bottom, left, right := pad, pad, pad
			switch fc.dir {
			case "BT":
				top, bottom = bottom, top
			case "LR":
				top, bottom, left, right = left, right, top, bottom
			case "RL":
				top, bottom, left, right = right, left, top, bottom
			}
			c.group = true
			c.padTop, c.padBottom, c.padLeft, c.padRight = top, bottom, left, right
			if vertical {
				c.minW = lab.w + 2*pad
			} else {
				c.minH = lab.w + 2*pad
			}
			c.span = math.Inf(1)
		} else {
			cellsWide := float64(fWrapCells)
			if it.shape == shDiamond {
				cellsWide = fDiamCells
			}
			lab := makeLabel(it.label, size, cellsWide*advance*size, it.markdown)
			w, h, dy := shapeSize(it.shape, lab.w, lab.h, s)
			fp.views[i] = nodeView{lab, it.shape, w, h, dy}
			c.w, c.h = w, h
			if !vertical {
				c.w, c.h = h, w
			}
			c.span = shapeSpan(it.shape, w, h, !vertical, s)
		}
		cn[i] = c
	}

	edgeLabels := make([]label, len(fc.edges))
	var ce []cedge
	var drawn []int
	for ei, e := range fc.edges {
		lab := makeLabel(e.label, edgeSize, fWrapCells*advance*edgeSize, e.markdown)
		edgeLabels[ei] = lab
		if e.from == e.to {
			if !items[e.from].group {
				extra := 0.0
				if lab.w > 0 {
					extra = lab.w + 12*s
					if !vertical {
						extra = lab.h + 8*s
					}
				}
				cn[e.from].w += 2 * (fLoopReach*s + extra)
			}
			continue
		}
		c := cedge{src: e.from, dst: e.to, minlen: e.minlen, srcShare: e.start == markNone, dstShare: e.end == markNone}
		if lab.w > 0 {
			c.labelW, c.labelH = lab.w+12*s, lab.h+4*s
			if !vertical {
				c.labelW, c.labelH = c.labelH, c.labelW
			}
		}
		ce = append(ce, c)
		drawn = append(drawn, ei)
	}

	opt := layeredOptions{
		nodeSep: fNodeSep * s, edgeSep: fEdgeSep * s, rankSep: fRankSep * s, portSep: fPortSep * s,
		portPitch: 16 * s, minPitch: 9 * s, trackMargin: 18 * s, trackPitch: 10 * s, trackSpread: 14 * s, snap: 12 * s,
	}
	_, layH := compoundLayout(cn, ce, &opt)
	tf := func(x, y float64) (float64, float64) {
		switch fc.dir {
		case "BT":
			return x, layH - y
		case "LR":
			return y, x
		case "RL":
			return layH - y, x
		}
		return x, y
	}
	fp.boxes = make([]rect, n)
	for i := range cn {
		cx, cy := tf(cn[i].x, cn[i].y)
		w, h := cn[i].w, cn[i].h
		if !vertical {
			w, h = h, w
		}
		fp.boxes[i] = rect{cx - w/2, cy - h/2, cx + w/2, cy + h/2}
	}
	for k, c := range ce {
		e := fc.edges[drawn[k]]
		r := c.route
		pts := make([]pt, 0, len(r)/2)
		for i := 0; i+1 < len(r); i += 2 {
			x, y := tf(r[i], r[i+1])
			pts = append(pts, pt{x, y})
		}
		pts = cleanRoute(pts)
		if lab := edgeLabels[drawn[k]]; lab.w > 0 {
			x, y := tf(c.labelX, c.labelY)
			fp.labels = append(fp.labels, labelPlace{lab, x, y})
		}
		if len(pts) >= 2 {
			insetEnd(pts, 0, fp.boxes[c.src], items[c.src].group, fp.views[c.src], s)
			insetEnd(pts, len(pts)-1, fp.boxes[c.dst], items[c.dst].group, fp.views[c.dst], s)
		}
		fp.edges = append(fp.edges, placedEdge{e, c.src, c.dst, pts})
	}

	loopCount := make([]int, n)
	for ei, e := range fc.edges {
		if e.from != e.to || items[e.from].group {
			continue
		}
		i := e.from
		b := fp.boxes[i]
		cx, cy := (b.x0+b.x1)/2, (b.y0+b.y1)/2
		v := fp.views[i]
		k := float64(loopCount[i])
		loopCount[i]++
		reach := fLoopReach*s + k*8*s
		var pts []pt
		var lx, ly float64
		lab := edgeLabels[ei]
		if vertical {
			d := math.Min(v.h/2-4*s, 8*s+k*6*s)
			x0 := cx + v.w/2 - shapeInset(v.shape, v.w, v.h, 1, -d, s)
			x1 := cx + v.w/2 - shapeInset(v.shape, v.w, v.h, 1, d, s)
			out := cx + v.w/2 + reach
			pts = []pt{{x0, cy - d}, {out, cy - d}, {out, cy + d}, {x1, cy + d}}
			lx, ly = out+4*s+lab.w/2, cy
		} else {
			d := math.Min(v.w/2-4*s, 8*s+k*6*s)
			y0 := cy + v.h/2 - shapeInset(v.shape, v.w, v.h, 2, -d, s)
			y1 := cy + v.h/2 - shapeInset(v.shape, v.w, v.h, 2, d, s)
			out := cy + v.h/2 + reach
			pts = []pt{{cx - d, y0}, {cx - d, out}, {cx + d, out}, {cx + d, y1}}
			lx, ly = cx, out+4*s+lab.h/2
		}
		fp.edges = append(fp.edges, placedEdge{e, i, i, pts})
		if lab.w > 0 {
			fp.labels = append(fp.labels, labelPlace{lab, lx, ly})
		}
	}
	return fp
}

func drawFlowchart(fp *flowPlace) *Scene {
	fc, s, size := fp.fc, fp.s, fp.size
	stroke := fStroke * s
	items := fc.items
	n := len(items)
	sc := &scene{kind: "flowchart"}
	if fc.title != "" {
		lab := makeLabel(fc.title, size, 0, false)
		width := 0.0
		for _, b := range fp.boxes {
			width = math.Max(width, b.x1)
		}
		sc.label(lab, width/2, -lab.h/2-12*s, PaintForeground, true)
	}

	routes := make([][]pt, len(fp.edges))
	for i, e := range fp.edges {
		routes[i] = e.pts
	}
	// Groups, outer ones first, each with its title where no edge crosses it.
	depth := make([]int, n)
	maxDepth := 0
	for i := range items {
		for p := items[i].parent; p >= 0 && depth[i] <= n; p = items[p].parent {
			depth[i]++
		}
		maxDepth = max(maxDepth, depth[i])
	}
	var clusters, titleBacks []rect
	for d := 0; d <= maxDepth; d++ {
		for i, it := range items {
			if !it.group || depth[i] != d {
				continue
			}
			b := fp.boxes[i]
			clusters = append(clusters, b)
			sc.shape(rectPath(b.x0, b.y0, b.x1-b.x0, b.y1-b.y0), PaintSurfaceAlt, PaintBorder, s, nil)
			lab := fp.titles[i]
			if lab.w == 0 {
				continue
			}
			y := b.y0 + 6*s
			mid := (b.x0 + b.x1 - lab.w) / 2
			free := func(x float64) bool { return !crossed(routes, rect{x - 4*s, y, x + lab.w + 4*s, y + lab.h}) }
			// The title moves off the middle, a step at a time, to a place no edge
			// crosses; where there is none, it stays in the middle on a patch of the fill.
			best, found := mid, free(mid)
			for d := 4 * s; !found && (mid-d >= b.x0+4*s || mid+d+lab.w <= b.x1-4*s); d += 4 * s {
				for _, x := range []float64{mid - d, mid + d} {
					if x >= b.x0+4*s && x+lab.w <= b.x1-4*s && free(x) {
						best, found = x, true
						break
					}
				}
			}
			if !found {
				titleBacks = append(titleBacks, rect{best - 3*s, y, best + lab.w + 3*s, y + lab.h})
			}
			sc.labelAt(lab, best, y, PaintMuted, false, AlignCenter)
		}
	}

	for _, e := range fp.edges {
		if e.e.stroke != strokeInvisible && len(e.pts) >= 2 {
			drawEdge(sc, e.pts, e.e, stroke, s)
		}
	}
	for _, r := range titleBacks {
		sc.shape(rectPath(r.x0, r.y0, r.x1-r.x0, r.y1-r.y0), PaintSurfaceAlt, PaintNone, 0, nil)
	}
	for _, l := range fp.labels {
		back := PaintBackground
		for _, c := range clusters {
			if c.contains(l.cx, l.cy) {
				back = PaintSurfaceAlt
			}
		}
		w := l.lab.w + 8*s
		sc.shape(rectPath(l.cx-w/2, l.cy-l.lab.h/2, w, l.lab.h), back, PaintNone, 0, nil)
		sc.label(l.lab, l.cx, l.cy, PaintMuted, false)
	}
	for i, it := range items {
		if !it.group {
			r := fp.nodeRect(i)
			drawShape(sc, fp.views[i], (r.x0+r.x1)/2, (r.y0+r.y1)/2, stroke, s)
		}
	}
	return sc.finish(2, fc.title)
}

// crossed reports whether a route passes through r.
func crossed(routes [][]pt, r rect) bool {
	for _, pts := range routes {
		for i := 1; i < len(pts); i++ {
			a, b := pts[i-1], pts[i]
			if math.Max(a.x, b.x) < r.x0 || math.Min(a.x, b.x) > r.x1 || math.Max(a.y, b.y) < r.y0 || math.Min(a.y, b.y) > r.y1 {
				continue
			}
			return true
		}
	}
	return false
}

// insetEnd moves the end of a route at pts[i] from the node's layout box onto
// the outline of the shape drawn in it.
func insetEnd(pts []pt, i int, b rect, group bool, v nodeView, s float64) {
	if group || len(pts) < 2 {
		return
	}
	j := i + 1
	if i > 0 {
		j = i - 1
	}
	p := pts[i]
	cx, cy := (b.x0+b.x1)/2, (b.y0+b.y1)/2
	bw, bh := b.x1-b.x0, b.y1-b.y0
	var dx, dy, amount float64
	switch {
	case math.Abs(math.Abs(p.y-cy)-bh/2) < 0.5:
		side := 0
		dy = 1
		if p.y > cy {
			side, dy = 2, -1
		}
		amount = shapeInset(v.shape, v.w, v.h, side, p.x-cx, s) + (bh-v.h)/2
	case math.Abs(math.Abs(p.x-cx)-bw/2) < 0.5:
		side := 3
		dx = 1
		if p.x > cx {
			side, dx = 1, -1
		}
		amount = shapeInset(v.shape, v.w, v.h, side, p.y-cy, s) + (bw-v.w)/2
	default:
		return
	}
	amount = math.Min(amount, dist(p, pts[j])*0.9)
	pts[i] = pt{p.x + dx*amount, p.y + dy*amount}
}

func markTrim(m markKind, s float64) float64 {
	switch m {
	case markArrow:
		return (fArrow - 1) * s
	case markCircle:
		return 8 * s
	case markCross:
		return 4 * s
	}
	return 0
}

func drawEdge(sc *scene, pts []pt, e fedge, stroke, s float64) {
	width := stroke
	var dash []float32
	switch e.stroke {
	case strokeThick:
		width = 2 * stroke
	case strokeDotted:
		dash = []float32{f32(3 * s), f32(4 * s)}
	}
	pts = append([]pt(nil), pts...)
	type head struct {
		kind       markKind
		x, y       float64
		dirX, dirY float64
	}
	var heads []head
	if e.end != markNone {
		tip, dx, dy := trimEnd(pts, markTrim(e.end, s))
		heads = append(heads, head{e.end, tip.x, tip.y, dx, dy})
	}
	if e.start != markNone {
		reverse(pts)
		tip, dx, dy := trimEnd(pts, markTrim(e.start, s))
		reverse(pts)
		heads = append(heads, head{e.start, tip.x, tip.y, dx, dy})
	}
	sc.shape(routePath(pts, fRadius*s), PaintNone, PaintLine, width, dash)
	for _, h := range heads {
		switch h.kind {
		case markArrow:
			sc.shape(arrowHead(h.x, h.y, h.dirX, h.dirY, fArrow*s), PaintLine, PaintNone, 0, nil)
		case markCircle:
			sc.shape(ellipsePath(h.x-h.dirX*4*s, h.y-h.dirY*4*s, 3.5*s, 3.5*s), PaintLine, PaintNone, 0, nil)
		case markCross:
			sc.shape(crossHead(h.x, h.y, h.dirX, h.dirY, 4*s), PaintNone, PaintLine, stroke, nil)
		}
	}
}
