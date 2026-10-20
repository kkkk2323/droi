package mermaid

import (
	"math"
	"sort"
)

// Compound layout, after Pele's layout/compound.ts: each group is laid out on
// its own and then placed in its parent as a single box. An edge that crosses
// a group's border passes through a port on the side facing its other end.
// Everything runs in one direction here (subgraph directions are not
// honoured), down the y axis; the caller turns the result.

type cnode struct {
	w, h   float64
	parent int
	group  bool
	// The room inside a group's box around what it holds.
	padTop, padBottom, padLeft, padRight float64
	// The least size of a group's box.
	minW, minH float64
	seq        int
	span       float64
	x, y       float64
}

type cedge struct {
	src, dst           int
	minlen             int
	labelW, labelH     float64
	srcShare, dstShare bool
	// x, y pairs from src to dst.
	route          []float64
	labelX, labelY float64
}

type cport struct {
	e     int
	inner int
	out   bool
	top   bool
	// Where the edge crosses the border, in the group box's own coordinates.
	px, py float64
	route  []float64
}

type clevelEdge struct{ e, a, b int }

type clevel struct {
	id     int
	items  []int
	edges  []clevelEdge
	ports  []*cport
	portOf map[int]*cport
	routes map[int][]float64
	x, y   float64
}

func portKey(e int, out bool) int {
	if out {
		return 2 * e
	}
	return 2*e + 1
}

// compoundLayout places nodes (whose parents must form a forest) and routes
// edges, which must not be loops. It returns the size of the drawing.
func compoundLayout(nodes []cnode, edges []cedge, opt *layeredOptions) (float64, float64) {
	n := len(nodes)
	levelOf := map[int]*clevel{}
	makeLevel := func(id int) *clevel {
		l := &clevel{id: id, portOf: map[int]*cport{}, routes: map[int][]float64{}}
		levelOf[id] = l
		return l
	}
	root := makeLevel(-1)
	children := make([][]int, n)
	for i := range nodes {
		if p := nodes[i].parent; p >= 0 {
			children[p] = append(children[p], i)
		} else {
			root.items = append(root.items, i)
		}
	}
	order := []*clevel{root}
	depth := make([]int, n)
	for q := 0; q < len(order); q++ {
		l := order[q]
		for _, item := range l.items {
			if l.id >= 0 {
				depth[item] = depth[l.id] + 1
			}
			if nodes[item].group {
				child := makeLevel(item)
				child.items = children[item]
				order = append(order, child)
			}
		}
	}
	for q := len(order) - 1; q > 0; q-- {
		l := order[q]
		seq := math.MaxInt
		for _, item := range l.items {
			seq = min(seq, nodes[item].seq)
		}
		if seq != math.MaxInt {
			nodes[l.id].seq = min(nodes[l.id].seq, seq)
		}
	}
	for _, l := range order {
		sort.SliceStable(l.items, func(i, j int) bool { return nodes[l.items[i]].seq < nodes[l.items[j]].seq })
	}

	var stray []int
	for ei := range edges {
		e := &edges[ei]
		e.route = nil
		a, b := e.src, e.dst
		var aPath, bPath []int
		for depth[a] > depth[b] {
			aPath = append(aPath, a)
			a = nodes[a].parent
		}
		for depth[b] > depth[a] {
			bPath = append(bPath, b)
			b = nodes[b].parent
		}
		if a == b {
			stray = append(stray, ei)
			continue
		}
		for nodes[a].parent != nodes[b].parent {
			aPath = append(aPath, a)
			bPath = append(bPath, b)
			a, b = nodes[a].parent, nodes[b].parent
		}
		common := levelOf[nodes[a].parent]
		common.edges = append(common.edges, clevelEdge{ei, a, b})
		for k, path := range [][]int{aPath, bPath} {
			for _, inner := range path {
				l := levelOf[nodes[inner].parent]
				p := &cport{e: ei, inner: inner, out: k == 0, top: true}
				l.ports = append(l.ports, p)
				l.portOf[portKey(ei, p.out)] = p
			}
		}
	}

	local := make([]int, n)
	// Whether the edge's end at item lies inside it, rather than being item itself.
	below := func(item, ei int, out bool) bool {
		if out {
			return edges[ei].src != item
		}
		return edges[ei].dst != item
	}
	share := func(ei int, out bool) bool {
		if out {
			return edges[ei].srcShare
		}
		return edges[ei].dstShare
	}
	build := func(l *clevel, sized bool) ([]lnode, []ledge) {
		var ln []lnode
		var le []ledge
		for i, item := range l.items {
			local[item] = i
			nd := &nodes[item]
			v := lnode{kind: kNode, span: nd.span}
			if sized {
				v.w, v.h = nd.w, nd.h
				if nd.group {
					v.span = math.Min(nd.span, nd.w-nd.padLeft-nd.padRight)
				}
			}
			ln = append(ln, v)
		}
		held := func(item, ei int, out bool) bool { return nodes[item].group && below(item, ei, out) }
		offset := func(item, ei int, out bool) float64 {
			if !sized || !held(item, ei, out) {
				return 0
			}
			return levelOf[item].portOf[portKey(ei, out)].px - nodes[item].w/2
		}
		for _, x := range l.edges {
			e := &edges[x.e]
			ed := ledge{tail: local[x.a], head: local[x.b], minlen: e.minlen}
			if sized {
				ed.labelW, ed.labelH = e.labelW, e.labelH
				ed.tailDx, ed.headDx = offset(x.a, x.e, true), offset(x.b, x.e, false)
				ed.tailFixed, ed.headFixed = held(x.a, x.e, true), held(x.b, x.e, false)
				ed.tailShare, ed.headShare = e.srcShare, e.dstShare
			}
			le = append(le, ed)
		}
		for _, p := range l.ports {
			d := len(ln)
			kind := kEndPort
			if p.top {
				kind = kStartPort
			}
			ln = append(ln, lnode{kind: kind})
			var ed ledge
			if p.out {
				ed = ledge{tail: local[p.inner], head: d, tailDx: offset(p.inner, p.e, true), tailFixed: held(p.inner, p.e, true)}
				ed.tailShare = share(p.e, true)
			} else {
				ed = ledge{tail: d, head: local[p.inner], headDx: offset(p.inner, p.e, false), headFixed: held(p.inner, p.e, false)}
				ed.headShare = share(p.e, false)
			}
			le = append(le, ed)
		}
		return ln, le
	}

	// Top down: rank each level to learn which side of a group every crossing edge leaves from.
	for _, l := range order {
		for _, p := range l.ports {
			if nodes[p.inner].group && below(p.inner, p.e, p.out) {
				levelOf[p.inner].portOf[portKey(p.e, p.out)].top = p.top
			}
		}
		needed := false
		for _, x := range l.edges {
			if nodes[x.a].group && below(x.a, x.e, true) || nodes[x.b].group && below(x.b, x.e, false) {
				needed = true
				break
			}
		}
		if !needed {
			continue
		}
		ln, le := build(l, false)
		orient(ln, le)
		rankNodes(ln, le, 1)
		for i, x := range l.edges {
			aAbove := !le[i].reversed
			if nodes[x.a].group && below(x.a, x.e, true) {
				levelOf[x.a].portOf[portKey(x.e, true)].top = !aAbove
			}
			if nodes[x.b].group && below(x.b, x.e, false) {
				levelOf[x.b].portOf[portKey(x.e, false)].top = aAbove
			}
		}
	}

	// Bottom up: lay out each level now that the sizes of its groups are known.
	var totalW, totalH float64
	for q := len(order) - 1; q >= 0; q-- {
		l := order[q]
		ln, le := build(l, true)
		cw, ch := layered(ln, le, opt)
		var group *cnode
		var padL, padR, padT, padB, minW, minH float64
		if l.id >= 0 {
			group = &nodes[l.id]
			padL, padR, padT, padB = group.padLeft, group.padRight, group.padTop, group.padBottom
			minW, minH = group.minW, group.minH
		}
		boxW := math.Max(cw+padL+padR, minW)
		boxH := math.Max(ch+padT+padB, minH)
		offX := padL + (boxW-padL-padR-cw)/2
		offY := padT + (boxH-padT-padB-ch)/2
		move := func(pts []float64) []float64 {
			out := make([]float64, len(pts))
			for i := 0; i+1 < len(pts); i += 2 {
				out[i], out[i+1] = pts[i]+offX, pts[i+1]+offY
			}
			return out
		}
		for i, item := range l.items {
			nodes[item].x, nodes[item].y = ln[i].x+offX, ln[i].y+offY
		}
		for i, x := range l.edges {
			l.routes[x.e] = move(le[i].points)
			if e := &edges[x.e]; e.labelW > 0 {
				e.labelX, e.labelY = le[i].labelX+offX, le[i].labelY+offY
			}
		}
		k := len(l.edges)
		for _, p := range l.ports {
			route := move(le[k].points)
			k++
			by := boxH
			if p.top {
				by = 0
			}
			if p.out {
				bx := route[len(route)-2]
				route = append(route, bx, by)
				p.px, p.py = bx, by
			} else {
				bx := route[0]
				route = append([]float64{bx, by}, route...)
				p.px, p.py = bx, by
			}
			p.route = route
		}
		if group != nil {
			group.w, group.h = boxW, boxH
		} else {
			totalW, totalH = boxW, boxH
		}
	}

	// Top down again: turn positions inside each box into drawing coordinates.
	for _, l := range order {
		for _, item := range l.items {
			nd := &nodes[item]
			nd.x += l.x
			nd.y += l.y
			if nd.group {
				child := levelOf[item]
				child.x, child.y = nd.x-nd.w/2, nd.y-nd.h/2
			}
		}
	}

	appendRoute := func(route, part []float64, ox, oy float64) []float64 {
		for i := 0; i+1 < len(part); i += 2 {
			x, y := part[i]+ox, part[i+1]+oy
			if n := len(route); n >= 2 && math.Abs(route[n-2]-x) < 0.01 && math.Abs(route[n-1]-y) < 0.01 {
				continue
			}
			route = append(route, x, y)
		}
		return route
	}
	for _, l := range order {
		for _, x := range l.edges {
			e := &edges[x.e]
			var route []float64
			for item := e.src; nodes[item].parent != l.id; item = nodes[item].parent {
				lv := levelOf[nodes[item].parent]
				route = appendRoute(route, lv.portOf[portKey(x.e, true)].route, lv.x, lv.y)
			}
			route = appendRoute(route, l.routes[x.e], l.x, l.y)
			var inward []*clevel
			for item := e.dst; nodes[item].parent != l.id; item = nodes[item].parent {
				inward = append(inward, levelOf[nodes[item].parent])
			}
			for i := len(inward) - 1; i >= 0; i-- {
				lv := inward[i]
				route = appendRoute(route, lv.portOf[portKey(x.e, false)].route, lv.x, lv.y)
			}
			e.route = route
			if e.labelW > 0 {
				e.labelX += l.x
				e.labelY += l.y
			}
		}
	}
	for _, ei := range stray {
		e := &edges[ei]
		// One end is a group that holds the other: the edge runs from the
		// inner node straight to the group's nearer border.
		a, b := &nodes[e.src], &nodes[e.dst]
		if depth[e.src] > depth[e.dst] {
			e.route = []float64{a.x, a.y + a.h/2, a.x, b.y + b.h/2}
		} else {
			e.route = []float64{b.x, a.y - a.h/2, b.x, b.y - b.h/2}
		}
		e.labelX, e.labelY = (e.route[0]+e.route[2])/2, (e.route[1]+e.route[3])/2
	}
	return totalW, totalH
}
