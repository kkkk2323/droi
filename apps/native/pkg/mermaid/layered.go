package mermaid

import (
	"math"
	"sort"
)

// Layered (Sugiyama) layout of one flat graph, after Pele's layout/layered.ts.
// Ranks run down the y axis and callers turn the result for other directions.
// Every edge is cut into one segment per rank it passes, and each segment that
// does not run straight down gets a track of its own across the gap below its
// rank, so edges are drawn as runs at right angles.

type lkind uint8

const (
	kNode lkind = iota
	kDummy
	kLabel
	kStartPort
	kEndPort
)

type lnode struct {
	w, h float64
	kind lkind
	// How much of the top and bottom sides the ends of edges may spread along; 0 keeps them at the middle.
	span float64
	x, y float64
	rank int
}

type ledge struct {
	tail, head     int
	minlen         int
	labelW, labelH float64
	tailDx, headDx float64
	// A fixed end stays where its offset puts it, and takes no part in sharing out its side.
	tailFixed, headFixed bool
	// An end without a marker may share its place on a side with others like it.
	tailShare, headShare bool
	reversed             bool
	// x, y pairs from tail to head.
	points         []float64
	labelX, labelY float64
}

type layeredOptions struct {
	nodeSep, edgeSep, rankSep, portSep float64
	// The room between two ends on one side of a node, and the least that keeps their arrowheads apart.
	portPitch, minPitch float64
	// The room kept between a rank and the nearest track, between two tracks when
	// the gap has to grow to hold them, and the most they spread to.
	trackMargin, trackPitch, trackSpread float64
	// The farthest a node moves to bring an edge into line.
	snap float64
}

const (
	maxSweeps = 24
	// An edge gets a bend point in every rank it skips; past this many, the
	// longest edges are drawn as one straight line instead.
	maxDummies = 20000
	// Past this many pieces in one gap, tracks are given out in a plain order.
	maxWeighed = 400
	inLine     = 0.5
)

type lgraph struct {
	real, maxRank      int
	w, h               []float64
	kind               []lkind
	rank               []int
	from, to           []int
	fromDx, toDx       []float64
	fromFree, toFree   []bool
	fromShare, toShare []bool
	succ, pred         [][]int
	layers             [][]int
	pos                []int
}

func (g *lgraph) addSeg(a, b int, aDx, bDx float64) int {
	e := len(g.from)
	g.from = append(g.from, a)
	g.to = append(g.to, b)
	g.fromDx = append(g.fromDx, aDx)
	g.toDx = append(g.toDx, bDx)
	g.fromFree = append(g.fromFree, false)
	g.toFree = append(g.toFree, false)
	g.fromShare = append(g.fromShare, false)
	g.toShare = append(g.toShare, false)
	g.succ[a] = append(g.succ[a], e)
	g.pred[b] = append(g.pred[b], e)
	return e
}

func isDummy(k lkind) bool { return k == kDummy || k == kLabel }

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// layered places nodes and routes edges, and returns the size of the drawing.
func layered(nodes []lnode, edges []ledge, opt *layeredOptions) (float64, float64) {
	real := len(nodes)
	if real == 0 {
		return 0, 0
	}
	m := len(edges)

	// Every edge gets a middle rank of its own when labels or parallel edges need the room.
	step := 1
	seen := make(map[[2]int]bool, m)
	for _, e := range edges {
		key := [2]int{min(e.tail, e.head), max(e.tail, e.head)}
		if e.labelW > 0 || seen[key] {
			step = 2
			break
		}
		seen[key] = true
	}

	orient(nodes, edges)
	rankNodes(nodes, edges, step)

	span := make([]int, m)
	direct := make([]bool, m)
	dummies := 0
	for i, e := range edges {
		span[i] = absInt(nodes[e.head].rank - nodes[e.tail].rank)
		if span[i] < 1 {
			direct[i] = true
		} else {
			dummies += span[i] - 1
		}
	}
	if dummies > maxDummies {
		var longest []int
		for i := range edges {
			if span[i] > 1 && !direct[i] {
				longest = append(longest, i)
			}
		}
		sort.SliceStable(longest, func(a, b int) bool { return span[longest[a]] > span[longest[b]] })
		for _, i := range longest {
			if dummies <= maxDummies {
				break
			}
			direct[i] = true
			dummies -= span[i] - 1
		}
	}

	n := real + dummies
	g := &lgraph{
		real: real, w: make([]float64, n), h: make([]float64, n), kind: make([]lkind, n), rank: make([]int, n),
		succ: make([][]int, n), pred: make([][]int, n), pos: make([]int, n),
	}
	for i, nd := range nodes {
		g.w[i], g.h[i], g.kind[i], g.rank[i] = nd.w, nd.h, nd.kind, nd.rank
		g.maxRank = max(g.maxRank, nd.rank)
	}

	firstDummy := make([]int, m)
	labelNode := make([]int, m)
	firstSeg := make([]int, m)
	lastSeg := make([]int, m)
	next := real
	for ei := range edges {
		firstDummy[ei], labelNode[ei], firstSeg[ei], lastSeg[ei] = -1, -1, -1, -1
		if direct[ei] {
			continue
		}
		e := &edges[ei]
		a, b, aDx, bDx := e.tail, e.head, e.tailDx, e.headDx
		aFixed, bFixed, aShare, bShare := e.tailFixed, e.headFixed, e.tailShare, e.headShare
		if e.reversed {
			a, b, aDx, bDx = b, a, bDx, aDx
			aFixed, bFixed, aShare, bShare = bFixed, aFixed, bShare, aShare
		}
		ra, rb := g.rank[a], g.rank[b]
		labelRank := -1
		if e.labelW > 0 {
			// The label goes halfway along: in the rank between two neighbouring
			// ranks of nodes, or for a longer edge beside the nodes of a rank it passes.
			labelRank = ra + (rb-ra)>>1
			shift := -1
			if e.reversed {
				shift = 1
			}
			if step == 2 && rb-ra >= 4 {
				if labelRank&1 == 1 {
					labelRank += shift
				}
			} else if step == 2 && labelRank&1 == 0 {
				labelRank += shift
			}
		}
		if rb-ra > 1 {
			firstDummy[ei] = next
		}
		prev := a
		first := -1
		for r := ra + 1; r < rb; r++ {
			d := next
			next++
			g.rank[d] = r
			if r == labelRank {
				g.w[d], g.h[d], g.kind[d] = e.labelW, e.labelH, kLabel
				labelNode[ei] = d
			} else {
				g.kind[d] = kDummy
			}
			dx := 0.0
			if prev == a {
				dx = aDx
			}
			s := g.addSeg(prev, d, dx, 0)
			if first < 0 {
				first = s
			}
			prev = d
		}
		dx := 0.0
		if prev == a {
			dx = aDx
		}
		last := g.addSeg(prev, b, dx, bDx)
		if first < 0 {
			first = last
		}
		firstSeg[ei], lastSeg[ei] = first, last
		g.fromFree[first] = !aFixed && g.kind[a] == kNode
		g.fromShare[first] = aShare
		g.toFree[last] = !bFixed && g.kind[b] == kNode
		g.toShare[last] = bShare
	}

	// Initial order: depth-first from the top, so that subtrees start out together.
	g.layers = make([][]int, g.maxRank+1)
	{
		byRank := make([]int, 0, n)
		for r := 0; r <= g.maxRank; r++ {
			for v := 0; v < n; v++ {
				if g.rank[v] == r {
					byRank = append(byRank, v)
				}
			}
		}
		visited := make([]bool, n)
		var stack []int
		for _, root := range byRank {
			if visited[root] {
				continue
			}
			stack = append(stack, root)
			for len(stack) > 0 {
				v := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if visited[v] {
					continue
				}
				visited[v] = true
				g.layers[g.rank[v]] = append(g.layers[g.rank[v]], v)
				for j := len(g.succ[v]) - 1; j >= 0; j-- {
					if w := g.to[g.succ[v][j]]; !visited[w] {
						stack = append(stack, w)
					}
				}
			}
		}
	}
	g.setPositions()
	if len(g.from) > 0 && g.maxRank > 0 {
		g.reduceCrossings()
	}
	for v := 0; v < n; v++ {
		sort.SliceStable(g.succ[v], func(i, j int) bool { return g.pos[g.to[g.succ[v][i]]] < g.pos[g.to[g.succ[v][j]]] })
		sort.SliceStable(g.pred[v], func(i, j int) bool { return g.pos[g.from[g.pred[v][i]]] < g.pos[g.from[g.pred[v][j]]] })
	}

	xs := g.position(opt)
	g.spread(xs, nodes, opt)
	g.snap(xs, opt)
	for ei := range edges {
		if direct[ei] {
			continue
		}
		e := &edges[ei]
		aDx, bDx := g.fromDx[firstSeg[ei]], g.toDx[lastSeg[ei]]
		if e.reversed {
			aDx, bDx = bDx, aDx
		}
		e.tailDx, e.headDx = aDx, bDx
	}

	// A rank that holds nothing but bends is passed through: the pieces of an
	// edge run from one rank that holds something to the next.
	empty := make([]bool, g.maxRank+1)
	for r := 1; r < g.maxRank; r++ {
		only := len(g.layers[r]) > 0
		for _, v := range g.layers[r] {
			if g.kind[v] != kDummy {
				only = false
				break
			}
		}
		empty[r] = only
	}
	type piece struct {
		from, to float64
		rank     int
		lower    int
	}
	var pieces []piece
	pieceStart := make([]int, m+1)
	for ei := range edges {
		pieceStart[ei] = len(pieces)
		if direct[ei] {
			continue
		}
		fs, ls := firstSeg[ei], lastSeg[ei]
		x := xs[g.from[fs]] + g.fromDx[fs]
		rk := g.rank[g.from[fs]]
		if d0 := firstDummy[ei]; d0 >= 0 {
			for d := d0; d < d0+span[ei]-1; d++ {
				if empty[g.rank[d]] {
					continue
				}
				pieces = append(pieces, piece{x, xs[d], rk, d})
				x, rk = xs[d], g.rank[d]
			}
		}
		pieces = append(pieces, piece{x, xs[g.to[ls]] + g.toDx[ls], rk, -1})
	}
	pieceStart[m] = len(pieces)

	pf := make([]float64, len(pieces))
	pto := make([]float64, len(pieces))
	byRank := make([][]int, g.maxRank+1)
	track := make([]int, len(pieces))
	for k, p := range pieces {
		pf[k], pto[k] = p.from, p.to
		track[k] = -1
		if math.Abs(p.from-p.to) >= inLine {
			byRank[p.rank] = append(byRank[p.rank], k)
		}
	}
	trackCount := make([]int, g.maxRank+1)
	for r, list := range byRank {
		if len(list) > 0 {
			trackCount[r] = layTracks(pf, pto, list, track)
		}
	}

	portOnly := func(r int, k lkind) bool {
		found := false
		for _, v := range g.layers[r] {
			if g.kind[v] == k {
				found = true
			} else if g.kind[v] != kDummy {
				return false
			}
		}
		return found
	}
	startRanks, endRanks := 0, 0
	if portOnly(0, kStartPort) {
		startRanks = step
	}
	if portOnly(g.maxRank, kEndPort) {
		endRanks = step
	}
	gapUnder := make([]float64, g.maxRank+1)
	for r := 0; r < g.maxRank; r++ {
		sep := opt.rankSep
		if r < startRanks || r >= g.maxRank-endRanks {
			sep = opt.portSep
		}
		gapUnder[r] = sep / float64(step)
	}
	below := make([]int, g.maxRank+1)
	for r := 0; r < g.maxRank; r++ {
		if empty[r] {
			continue
		}
		next := r + 1
		room := gapUnder[r]
		for next < g.maxRank && empty[next] {
			room += gapUnder[next]
			next++
		}
		below[r] = next
		if c := trackCount[r]; c > 0 {
			gapUnder[r] += math.Max(0, 2*opt.trackMargin+float64(c-1)*opt.trackPitch-room)
		}
	}
	bandTop := make([]float64, g.maxRank+1)
	bandBottom := make([]float64, g.maxRank+1)
	y := 0.0
	for r := 0; r <= g.maxRank; r++ {
		h := 0.0
		for _, v := range g.layers[r] {
			h = math.Max(h, g.h[v])
		}
		bandTop[r], bandBottom[r] = y, y+h
		y += h + gapUnder[r]
	}
	height := y
	trackY := func(k int) float64 {
		r := pieces[k].rank
		top, bottom := bandBottom[r], bandTop[below[r]]
		count := trackCount[r]
		pitch := 0.0
		if count > 1 {
			pitch = math.Min(opt.trackSpread, (bottom-top-2*opt.trackMargin)/float64(count-1))
		}
		return (top+bottom)/2 + (float64(track[k])-float64(count-1)/2)*pitch
	}

	minX, maxX := math.Inf(1), math.Inf(-1)
	for v := 0; v < n; v++ {
		minX = math.Min(minX, xs[v]-g.w[v]/2)
		maxX = math.Max(maxX, xs[v]+g.w[v]/2)
	}
	// A rank whose nodes only send edges on stands on one line, and one whose
	// nodes only receive them hangs from one, so the edges of nodes of
	// different heights set out from, or arrive at, the same level.
	arrives := make([]bool, g.maxRank+1)
	leaves := make([]bool, g.maxRank+1)
	for v := 0; v < real; v++ {
		if g.kind[v] != kNode {
			continue
		}
		if len(g.pred[v]) > 0 {
			arrives[g.rank[v]] = true
		}
		if len(g.succ[v]) > 0 {
			leaves[g.rank[v]] = true
		}
	}
	for i := range nodes {
		nd := &nodes[i]
		r := nd.rank
		nd.x = xs[i] - minX
		switch {
		case g.kind[i] == kNode && leaves[r] && !arrives[r]:
			nd.y = bandBottom[r] - nd.h/2
		case g.kind[i] == kNode && arrives[r] && !leaves[r]:
			nd.y = bandTop[r] + nd.h/2
		default:
			nd.y = (bandTop[r] + bandBottom[r]) / 2
		}
	}

	for ei := range edges {
		e := &edges[ei]
		a, b, aDx, bDx := e.tail, e.head, e.tailDx, e.headDx
		if e.reversed {
			a, b, aDx, bDx = b, a, bDx, aDx
		}
		first, last := &nodes[a], &nodes[b]
		pts := []float64{first.x + aDx, first.y + first.h/2}
		for k := pieceStart[ei]; k < pieceStart[ei+1]; k++ {
			if track[k] >= 0 {
				ty := trackY(k)
				pts = append(pts, pieces[k].from-minX, ty, pieces[k].to-minX, ty)
			}
			if d := pieces[k].lower; d >= 0 {
				r := g.rank[d]
				pts = append(pts, xs[d]-minX, bandTop[r])
				if bandBottom[r] > bandTop[r] {
					pts = append(pts, xs[d]-minX, bandBottom[r])
				}
			}
		}
		pts = append(pts, last.x+bDx, last.y-last.h/2)
		if e.reversed {
			for i, j := 0, len(pts)-2; i < j; i, j = i+2, j-2 {
				pts[i], pts[j] = pts[j], pts[i]
				pts[i+1], pts[j+1] = pts[j+1], pts[i+1]
			}
		}
		e.points = pts
		if ln := labelNode[ei]; ln >= 0 {
			e.labelX = xs[ln] - minX
			e.labelY = (bandTop[g.rank[ln]] + bandBottom[g.rank[ln]]) / 2
		} else if e.labelW > 0 {
			e.labelX = (pts[0] + pts[len(pts)-2]) / 2
			e.labelY = (pts[1] + pts[len(pts)-1]) / 2
		}
	}
	return maxX - minX, height
}

func (g *lgraph) setPositions() {
	for _, layer := range g.layers {
		for i, v := range layer {
			g.pos[v] = i
		}
	}
}

// orient reverses the edges that close a cycle, found depth-first in declaration order.
func orient(nodes []lnode, edges []ledge) {
	n := len(nodes)
	out := make([][]int, n)
	for i := range edges {
		e := &edges[i]
		e.reversed = nodes[e.head].kind == kStartPort || nodes[e.tail].kind == kEndPort
		if !e.reversed {
			out[e.tail] = append(out[e.tail], i)
		}
	}
	state := make([]uint8, n)
	cursor := make([]int, n)
	var stack []int
	for root := 0; root < n; root++ {
		if state[root] != 0 {
			continue
		}
		stack = append(stack, root)
		state[root] = 1
		for len(stack) > 0 {
			v := stack[len(stack)-1]
			if cursor[v] < len(out[v]) {
				e := &edges[out[v][cursor[v]]]
				cursor[v]++
				switch w := e.head; state[w] {
				case 1:
					e.reversed = true
				case 0:
					state[w] = 1
					stack = append(stack, w)
				}
			} else {
				state[v] = 2
				stack = stack[:len(stack)-1]
			}
		}
	}
}

// rankNodes gives each node the longest path to it, then moves loosely
// attached nodes toward the side with more edges. Start ports go above
// everything and end ports below.
func rankNodes(nodes []lnode, edges []ledge, step int) {
	n := len(nodes)
	m := len(edges)
	from := make([]int, m)
	to := make([]int, m)
	length := make([]int, m)
	outE := make([][]int, n)
	inE := make([][]int, n)
	for i, e := range edges {
		a, b := e.tail, e.head
		if e.reversed {
			a, b = b, a
		}
		from[i], to[i] = a, b
		length[i] = max(1, e.minlen) * step
		outE[a] = append(outE[a], i)
		inE[b] = append(inE[b], i)
	}
	r := make([]int, n)
	indeg := make([]int, n)
	topo := make([]int, 0, n)
	for i := 0; i < n; i++ {
		indeg[i] = len(inE[i])
		if indeg[i] == 0 {
			topo = append(topo, i)
		}
	}
	for q := 0; q < len(topo); q++ {
		v := topo[q]
		for _, ei := range outE[v] {
			w := to[ei]
			r[w] = max(r[w], r[v]+length[ei])
			if indeg[w]--; indeg[w] == 0 {
				topo = append(topo, w)
			}
		}
	}
	for pass := 0; pass < 8; pass++ {
		changed := false
		for q := len(topo) - 1; q >= 0; q-- {
			v := topo[q]
			ins, outs := len(inE[v]), len(outE[v])
			if ins == 0 && outs == 0 {
				continue
			}
			lo, hi := math.MinInt, math.MaxInt
			for _, ei := range inE[v] {
				lo = max(lo, r[from[ei]]+length[ei])
			}
			for _, ei := range outE[v] {
				hi = min(hi, r[to[ei]]-length[ei])
			}
			next := r[v]
			if outs > ins && hi != math.MaxInt {
				next = hi
			} else if ins > outs && lo != math.MinInt {
				next = lo
			}
			if next != r[v] {
				r[v] = next
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	lo, hi := math.MaxInt, math.MinInt
	startPorts := false
	for i := range nodes {
		switch nodes[i].kind {
		case kStartPort:
			startPorts = true
		case kEndPort:
		default:
			lo, hi = min(lo, r[i]), max(hi, r[i])
		}
	}
	if lo > hi {
		lo, hi = 0, 0
	}
	for i := range nodes {
		switch nodes[i].kind {
		case kStartPort:
			r[i] = lo - step
		case kEndPort:
			r[i] = hi + step
		}
	}
	if startPorts {
		lo -= step
	}
	for i := range nodes {
		nodes[i].rank = r[i] - lo
	}
}

func (g *lgraph) crossCount() int {
	total := 0
	type pair struct{ a, b int }
	var pairs []pair
	var tree []int
	for r := 0; r < g.maxRank; r++ {
		south := len(g.layers[r+1])
		if south < 2 {
			continue
		}
		pairs = pairs[:0]
		for _, v := range g.layers[r] {
			for _, e := range g.succ[v] {
				pairs = append(pairs, pair{g.pos[v], g.pos[g.to[e]]})
			}
		}
		if len(pairs) < 2 {
			continue
		}
		sort.Slice(pairs, func(i, j int) bool {
			if pairs[i].a != pairs[j].a {
				return pairs[i].a < pairs[j].a
			}
			return pairs[i].b < pairs[j].b
		})
		tree = append(tree[:0], make([]int, south+1)...)
		for idx, p := range pairs {
			// Earlier pairs that end to the right of this one cross it.
			atMost := 0
			for i := p.b + 1; i > 0; i -= i & -i {
				atMost += tree[i]
			}
			total += idx - atMost
			for i := p.b + 1; i <= south; i += i & -i {
				tree[i]++
			}
		}
	}
	return total
}

func (g *lgraph) snapshot() [][]int {
	out := make([][]int, len(g.layers))
	for r, layer := range g.layers {
		out[r] = append([]int(nil), layer...)
	}
	return out
}

func (g *lgraph) restore(order [][]int) {
	for r, layer := range order {
		copy(g.layers[r], layer)
	}
	g.setPositions()
}

func (g *lgraph) sweep(down, biasRight bool, bary []float64, fixed []bool) {
	for step := 1; step <= g.maxRank; step++ {
		r := step
		if !down {
			r = g.maxRank - step
		}
		layer := g.layers[r]
		var movable []int
		for _, v := range layer {
			nb, other, otherDx := g.succ[v], g.to, g.toDx
			if down {
				nb, other, otherDx = g.pred[v], g.from, g.fromDx
			}
			fixed[v] = len(nb) == 0
			if fixed[v] {
				continue
			}
			sum := 0.0
			for _, e := range nb {
				u := other[e]
				sum += float64(g.pos[u])
				if g.w[u] > 0 {
					sum += otherDx[e] / g.w[u]
				}
			}
			bary[v] = sum / float64(len(nb))
			movable = append(movable, v)
		}
		if len(movable) < 2 {
			continue
		}
		sort.SliceStable(movable, func(i, j int) bool {
			a, b := movable[i], movable[j]
			if bary[a] != bary[b] {
				return bary[a] < bary[b]
			}
			if biasRight {
				return g.pos[a] > g.pos[b]
			}
			return g.pos[a] < g.pos[b]
		})
		k := 0
		for i, v := range layer {
			if !fixed[v] {
				layer[i] = movable[k]
				k++
			}
		}
		for i, v := range layer {
			g.pos[v] = i
		}
	}
}

// outOfOrder counts the pairs of nodes in one rank that are not in the order they were given.
func (g *lgraph) outOfOrder() int {
	total := 0
	for _, layer := range g.layers {
		for i, v := range layer {
			if v >= g.real {
				continue
			}
			for _, w := range layer[i+1:] {
				if w < v {
					total++
				}
			}
		}
	}
	return total
}

// reduceCrossings runs barycenter sweeps twice, going down first and going
// up first, and keeps the order with the fewest crossings; of two as good,
// the one that keeps more nodes in the order they were written.
func (g *lgraph) reduceCrossings() {
	best := g.crossCount()
	if best == 0 {
		return
	}
	n := len(g.w)
	bary := make([]float64, n)
	fixed := make([]bool, n)
	initial := g.snapshot()
	start := best
	small := n <= 600
	attempts := 1
	if small {
		attempts = 2
	}
	var chosen [][]int
	chosenOut := math.MaxInt
	for attempt := 0; attempt < attempts; attempt++ {
		g.restore(initial)
		found := start
		foundOrder := initial
		for i, stale := 0, 0; stale < 4 && i < maxSweeps; i, stale = i+1, stale+1 {
			g.sweep((i+attempt)%2 == 0, i%4 >= 2, bary, fixed)
			if c := g.crossCount(); c < found {
				found = c
				foundOrder = g.snapshot()
				stale = 0
				if c == 0 {
					break
				}
			}
		}
		g.restore(foundOrder)
		out := 0
		if small {
			out = g.outOfOrder()
		}
		if attempt == 0 || found < best || found == best && out < chosenOut {
			best, chosen, chosenOut = found, foundOrder, out
		}
	}
	g.restore(chosen)
}

// halfRoom is half the room a node, bend or port takes across the flow, with its share of the gap beside it.
func (g *lgraph) halfRoom(opt *layeredOptions, v int) float64 {
	if g.kind[v] == kNode {
		return g.w[v]/2 + opt.nodeSep/2
	}
	return g.w[v]/2 + opt.edgeSep/2
}

// position is Brandes-Köpf coordinate assignment: four aligned layouts, balanced.
func (g *lgraph) position(opt *layeredOptions) []float64 {
	n := len(g.w)
	segs := len(g.from)
	gap := make([]float64, n)
	for i := range gap {
		gap[i] = g.halfRoom(opt, i)
	}

	// An edge between real nodes may not cross a segment between two bends; mark those that would.
	conflict := make([]bool, segs)
	for r := 1; r <= g.maxRank; r++ {
		prevLen := len(g.layers[r-1])
		layer := g.layers[r]
		k0, scan := 0, 0
		for i, v := range layer {
			inner := -1
			if isDummy(g.kind[v]) && len(g.pred[v]) > 0 {
				if u := g.from[g.pred[v][0]]; isDummy(g.kind[u]) {
					inner = u
				}
			}
			k1 := prevLen
			if inner >= 0 {
				k1 = g.pos[inner]
			}
			if inner >= 0 || i == len(layer)-1 {
				for _, w := range layer[scan : i+1] {
					wd := isDummy(g.kind[w])
					for _, e := range g.pred[w] {
						up := g.pos[g.from[e]]
						if (up < k0 || k1 < up) && !(wd && isDummy(g.kind[g.from[e]])) {
							conflict[e] = true
						}
					}
				}
				scan = i + 1
				k0 = k1
			}
		}
	}

	var results [4][]float64
	root := make([]int, n)
	align := make([]int, n)
	shift := make([]float64, n)
	for variant := 0; variant < 4; variant++ {
		up := variant < 2
		right := variant&1 == 1
		for i := 0; i < n; i++ {
			root[i], align[i], shift[i] = i, i, 0
		}
		for step := 0; step <= g.maxRank; step++ {
			r := step
			if !up {
				r = g.maxRank - step
			}
			adjacent := r + 1
			if up {
				adjacent = r - 1
			}
			adjacentLast := 0
			if adjacent >= 0 && adjacent <= g.maxRank {
				adjacentLast = len(g.layers[adjacent]) - 1
			}
			prevIdx := -1
			layer := g.layers[r]
			for c := 0; c < len(layer); c++ {
				i := c
				if right {
					i = len(layer) - 1 - c
				}
				v := layer[i]
				nb := g.succ[v]
				if up {
					nb = g.pred[v]
				}
				d := len(nb)
				if d == 0 {
					continue
				}
				lo, hi := (d-1)>>1, d>>1
				for pass := 0; pass < 2; pass++ {
					if pass == 1 && lo == hi {
						break
					}
					idx := hi
					if (pass == 0) != right {
						idx = lo
					}
					e := nb[idx]
					w, ownDx, otherDx := g.to[e], g.fromDx[e], g.toDx[e]
					if up {
						w, ownDx, otherDx = g.from[e], g.toDx[e], g.fromDx[e]
					}
					p := g.pos[w]
					if right {
						p = adjacentLast - g.pos[w]
					}
					if align[v] == v && prevIdx < p && !conflict[e] {
						align[w] = v
						root[v] = root[w]
						align[v] = root[v]
						shift[v] = shift[w] + otherDx - ownDx
						prevIdx = p
					}
				}
			}
		}

		type blockEdge struct {
			to int
			w  float64
		}
		out := make([][]blockEdge, n)
		indeg := make([]int, n)
		for _, layer := range g.layers {
			for i := 1; i < len(layer); i++ {
				a, b := layer[i-1], layer[i]
				if right {
					a, b = layer[i], layer[i-1]
				}
				sep := gap[a] + gap[b]
				w := sep + shift[a] - shift[b]
				if right {
					w = sep - shift[a] + shift[b]
				}
				out[root[a]] = append(out[root[a]], blockEdge{root[b], w})
				indeg[root[b]]++
			}
		}
		x := make([]float64, n)
		var topo []int
		for i := 0; i < n; i++ {
			if root[i] == i && indeg[i] == 0 {
				topo = append(topo, i)
			}
		}
		for q := 0; q < len(topo); q++ {
			v := topo[q]
			for _, be := range out[v] {
				x[be.to] = math.Max(x[be.to], x[v]+be.w)
				if indeg[be.to]--; indeg[be.to] == 0 {
					topo = append(topo, be.to)
				}
			}
		}
		for q := len(topo) - 1; q >= 0; q-- {
			v := topo[q]
			least := math.Inf(1)
			for _, be := range out[v] {
				least = math.Min(least, x[be.to]-be.w)
			}
			if !math.IsInf(least, 1) && least > x[v] {
				x[v] = least
			}
		}
		xs := make([]float64, n)
		for i := 0; i < n; i++ {
			xs[i] = x[root[i]] + shift[i]
			if right {
				xs[i] = -x[root[i]] + shift[i]
			}
		}
		results[variant] = xs
	}

	narrowest := 0
	bestWidth := math.Inf(1)
	var mins, maxs [4]float64
	for k := 0; k < 4; k++ {
		lo, hi := math.Inf(1), math.Inf(-1)
		for i, x := range results[k] {
			lo = math.Min(lo, x-g.w[i]/2)
			hi = math.Max(hi, x+g.w[i]/2)
		}
		mins[k], maxs[k] = lo, hi
		if hi-lo < bestWidth {
			bestWidth, narrowest = hi-lo, k
		}
	}
	for k := 0; k < 4; k++ {
		delta := maxs[narrowest] - maxs[k]
		if k&1 == 0 {
			delta = mins[narrowest] - mins[k]
		}
		for i := range results[k] {
			results[k][i] += delta
		}
	}
	xs := make([]float64, n)
	for i := 0; i < n; i++ {
		v := []float64{results[0][i], results[1][i], results[2][i], results[3][i]}
		sort.Float64s(v)
		xs[i] = (v[1] + v[2]) / 2
	}
	return xs
}

// spread shares out each side of a node between the ends of edges that meet
// it, in the order of where they lead, so that no two arrowheads meet. Ends
// without a marker share one place, so edges that leave a node together
// branch from one point.
func (g *lgraph) spread(xs []float64, nodes []lnode, opt *layeredOptions) {
	type slot struct {
		segs []int
		key  float64
	}
	for v := 0; v < g.real; v++ {
		if g.kind[v] != kNode || nodes[v].span <= 0 {
			continue
		}
		for side := 0; side < 2; side++ {
			list, free, share, dx := g.pred[v], g.toFree, g.toShare, g.toDx
			other, otherDx := g.from, g.fromDx
			if side == 1 {
				list, free, share, dx = g.succ[v], g.fromFree, g.fromShare, g.fromDx
				other, otherDx = g.to, g.toDx
			}
			var slots []slot
			shared := -1
			for _, e := range list {
				if !free[e] {
					continue
				}
				x := xs[other[e]] + otherDx[e]
				if share[e] && shared >= 0 {
					slots[shared].segs = append(slots[shared].segs, e)
					slots[shared].key += x
					continue
				}
				if share[e] {
					shared = len(slots)
				}
				slots = append(slots, slot{[]int{e}, x})
			}
			if len(slots) < 2 {
				continue
			}
			for i := range slots {
				slots[i].key /= float64(len(slots[i].segs))
			}
			sort.SliceStable(slots, func(i, j int) bool { return slots[i].key < slots[j].key })
			// Where the side is too short for every end to have a place
			// apart, neighbouring ends share one, and so do their arrowheads.
			places := min(len(slots), int(nodes[v].span/opt.minPitch)+1)
			k := float64(places - 1)
			pitch := 0.0
			if places > 1 {
				pitch = math.Min(opt.portPitch, nodes[v].span/k)
			}
			for i, s := range slots {
				at := i * places / len(slots)
				for _, e := range s.segs {
					dx[e] = (float64(at) - k/2) * pitch
				}
			}
		}
	}
}

// snap brings edges that are nearly straight into line, by moving a node a
// little across the flow along with everything already in line with it,
// where the nodes beside them leave room.
func (g *lgraph) snap(xs []float64, opt *layeredOptions) {
	n := len(g.w)
	off := func(e int) float64 { return xs[g.to[e]] + g.toDx[e] - xs[g.from[e]] - g.fromDx[e] }
	block := make([]int, n)
	members := make([][]int, n)
	for i := range block {
		block[i] = i
		members[i] = []int{i}
	}
	join := func(x, y int) {
		keep, drop := block[x], block[y]
		if keep == drop {
			return
		}
		if len(members[keep]) < len(members[drop]) {
			keep, drop = drop, keep
		}
		for _, v := range members[drop] {
			block[v] = keep
		}
		members[keep] = append(members[keep], members[drop]...)
		members[drop] = nil
	}
	var near []int
	for e := range g.from {
		d := math.Abs(off(e))
		if d < 0.01 {
			join(g.from[e], g.to[e])
		} else if d <= opt.snap {
			near = append(near, e)
		}
	}
	if len(near) == 0 {
		return
	}
	sort.SliceStable(near, func(i, j int) bool { return math.Abs(off(near[i])) < math.Abs(off(near[j])) })
	free := func(id int, shift float64) bool {
		for _, v := range members[id] {
			layer := g.layers[g.rank[v]]
			i := g.pos[v]
			if i > 0 {
				u := layer[i-1]
				if block[u] != id && xs[v]+shift-xs[u] < g.halfRoom(opt, v)+g.halfRoom(opt, u)-0.01 {
					return false
				}
			}
			if i+1 < len(layer) {
				u := layer[i+1]
				if block[u] != id && xs[u]-xs[v]-shift < g.halfRoom(opt, v)+g.halfRoom(opt, u)-0.01 {
					return false
				}
			}
		}
		return true
	}
	for _, e := range near {
		d := off(e)
		a, b := g.from[e], g.to[e]
		if math.Abs(d) < 0.01 {
			join(a, b)
			continue
		}
		if math.Abs(d) > opt.snap || block[a] == block[b] {
			continue
		}
		upper, lower := block[a], block[b]
		first, second := upper, lower
		if len(members[lower]) < len(members[upper]) {
			first, second = lower, upper
		}
		for _, id := range []int{first, second} {
			shift := -d
			if id == upper {
				shift = d
			}
			if !free(id, shift) {
				continue
			}
			for _, v := range members[id] {
				xs[v] += shift
			}
			join(a, b)
			break
		}
	}
}

// layTracks gives the pieces of one gap in list their tracks: two share a
// track only where they cannot be taken for each other, where they do not
// overlap or meet at one end. Of two that overlap, the one above is whichever
// makes them cross less. It returns the number of tracks.
func layTracks(from, to []float64, list []int, track []int) int {
	lo := func(k int) float64 { return math.Min(from[k], to[k]) }
	hi := func(k int) float64 { return math.Max(from[k], to[k]) }
	within := func(x float64, k int) int {
		if x >= lo(k)-inLine && x <= hi(k)+inLine {
			return 1
		}
		return 0
	}
	mates := func(a, b int) bool {
		return math.Abs(from[a]-from[b]) < inLine || math.Abs(to[a]-to[b]) < inLine
	}
	overlap := func(a, b int) bool { return lo(a) < hi(b)-inLine && lo(b) < hi(a)-inLine }
	clash := func(a, b int) bool { return overlap(a, b) && !mates(a, b) }
	meet := func(a, b int) bool {
		return overlap(a, b) || within(to[a], b)+within(from[b], a)+within(to[b], a)+within(from[a], b) > 0
	}
	// How often a and b cross with a on the higher track.
	crossings := func(a, b int) int { return within(to[a], b) + within(from[b], a) }

	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if wa, wb := hi(a)-lo(a), hi(b)-lo(b); wa != wb {
			return wa > wb
		}
		return lo(a) < lo(b)
	})
	count := len(list)
	above := make([][]int, count)
	sequence := list
	if count <= maxWeighed {
		waiting := make([]int, count)
		after := make([][]int, count)
		for i := 0; i < count; i++ {
			for j := i + 1; j < count; j++ {
				a, b := list[i], list[j]
				// Pieces that meet at one end may share a track, as the branches of a tree do.
				if !meet(a, b) || mates(a, b) {
					continue
				}
				ab, ba := crossings(a, b), crossings(b, a)
				if ab == ba {
					continue
				}
				first, second := i, j
				if ab > ba {
					first, second = j, i
				}
				above[second] = append(above[second], first)
				after[first] = append(after[first], second)
				waiting[second]++
			}
		}
		placed := make([]bool, count)
		sequence = make([]int, 0, count)
		order := make([]int, 0, count)
		for done := 0; done < count; done++ {
			pick := -1
			for i := 0; i < count; i++ {
				if placed[i] {
					continue
				}
				if pick < 0 || waiting[i] < waiting[pick] {
					pick = i
				}
				if waiting[i] == 0 {
					break
				}
			}
			placed[pick] = true
			order = append(order, pick)
			for _, j := range after[pick] {
				waiting[j]--
			}
		}
		for _, i := range order {
			sequence = append(sequence, list[i])
		}
	}
	place := make(map[int]int, count)
	for i, k := range list {
		place[k] = i
	}
	var rows [][]int
	for _, k := range sequence {
		t := 0
		for _, i := range above[place[k]] {
			if track[list[i]] >= t {
				t = track[list[i]] + 1
			}
		}
		for ; ; t++ {
			for len(rows) <= t {
				rows = append(rows, nil)
			}
			ok := true
			for _, o := range rows[t] {
				if clash(k, o) {
					ok = false
					break
				}
			}
			if ok {
				rows[t] = append(rows[t], k)
				break
			}
		}
		track[k] = t
	}
	return len(rows)
}
