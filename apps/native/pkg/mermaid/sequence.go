package mermaid

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type headKind uint8

const (
	headNone headKind = iota
	headArrow
	headOpen
	headCross
)

type participant struct {
	id, label string
	actor     bool
	box       int
}

type seqBox struct{ label string }

type sevKind uint8

const (
	evMessage sevKind = iota
	evNote
	evActivate
	evDeactivate
	evBlockStart
	evBlockDivider
	evBlockEnd
)

type placement uint8

const (
	noteOver placement = iota
	noteLeft
	noteRight
)

type sevent struct {
	kind     sevKind
	from, to int
	text     string
	dotted   bool
	head     headKind
	both     bool
	// + after the arrow activates the receiver; - deactivates the sender.
	activate, deactivate bool
	where                placement
	block                string
	number               int
}

type sequence struct {
	title  string
	parts  []participant
	boxes  []seqBox
	events []sevent
}

type sparser struct {
	sd    *sequence
	index map[string]int
	box   int
	// The open blocks and boxes, innermost last; a box is "box".
	open    []string
	number  int
	step    int
	visible bool
	line    int
}

var arrows = []struct {
	tok    string
	dotted bool
	head   headKind
	both   bool
}{
	{"<<-->>", true, headArrow, true},
	{"<<->>", false, headArrow, true},
	{"-->>", true, headArrow, false},
	{"->>", false, headArrow, false},
	{"--x", true, headCross, false},
	{"-x", false, headCross, false},
	{"--)", true, headOpen, false},
	{"-)", false, headOpen, false},
	{"-->", true, headNone, false},
	{"->", false, headNone, false},
}

var blockKinds = map[string]bool{
	"loop": true, "alt": true, "opt": true, "par": true, "par_over": true, "critical": true, "break": true, "rect": true,
}

func parseSequence(lines []string, header int) (*sequence, error) {
	p := &sparser{sd: &sequence{}, index: map[string]int{}, box: -1, number: 1, step: 1}
	first := strings.TrimSpace(lines[header])
	rest := strings.TrimSpace(strings.TrimPrefix(first, "sequenceDiagram"))
	todo := lines[header+1:]
	if rest != "" {
		todo = append([]string{rest}, todo...)
		p.line = header
	} else {
		p.line = header + 1
	}
	for i := 0; i < len(todo); i++ {
		p.line++
		line := strings.TrimSpace(todo[i])
		line = strings.TrimSpace(strings.TrimSuffix(line, ";"))
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		if strings.HasPrefix(line, "accDescr") && strings.HasSuffix(line, "{") {
			for i+1 < len(todo) && !strings.Contains(todo[i], "}") {
				i++
				p.line++
			}
			continue
		}
		if err := p.statement(line); err != nil {
			return nil, err
		}
		if len(p.sd.events) > maxSeqRows {
			return nil, p.errorf("more than %d rows", maxSeqRows)
		}
	}
	for len(p.open) > 0 {
		p.closeOne()
	}
	return p.sd, nil
}

func (p *sparser) errorf(format string, args ...any) error {
	return &Error{Line: p.line, Msg: fmt.Sprintf(format, args...)}
}

func (p *sparser) participant(id string) (int, error) {
	if i, ok := p.index[id]; ok {
		return i, nil
	}
	if len(p.sd.parts) >= maxNodes {
		return 0, p.errorf("more than %d participants", maxNodes)
	}
	i := len(p.sd.parts)
	p.sd.parts = append(p.sd.parts, participant{id: id, label: id, box: p.box})
	p.index[id] = i
	return i, nil
}

func (p *sparser) closeOne() {
	top := p.open[len(p.open)-1]
	p.open = p.open[:len(p.open)-1]
	if top == "box" {
		p.box = -1
		return
	}
	p.sd.events = append(p.sd.events, sevent{kind: evBlockEnd})
}

func cutWord(s string) (string, string) {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

var cssColors = map[string]bool{
	"transparent": true, "aqua": true, "black": true, "blue": true, "fuchsia": true, "gray": true, "grey": true,
	"green": true, "lime": true, "maroon": true, "navy": true, "olive": true, "orange": true, "purple": true,
	"red": true, "silver": true, "teal": true, "white": true, "yellow": true, "pink": true, "brown": true,
	"cyan": true, "magenta": true, "gold": true, "violet": true, "indigo": true, "beige": true, "coral": true,
	"salmon": true, "khaki": true, "tan": true, "plum": true, "orchid": true, "wheat": true, "ivory": true,
	"azure": true, "lavender": true, "lightblue": true, "lightgreen": true, "lightgray": true, "lightgrey": true,
	"lightyellow": true, "lightpink": true, "aliceblue": true, "honeydew": true, "mintcream": true,
	"seashell": true, "linen": true, "snow": true, "darkblue": true, "darkgreen": true, "darkred": true,
}

// stripColor drops a leading CSS color from a box's or rect's text.
func stripColor(s string) string {
	lower := strings.ToLower(s)
	for _, fn := range []string{"rgb(", "rgba(", "hsl(", "hsla("} {
		if strings.HasPrefix(lower, fn) {
			if i := strings.IndexByte(s, ')'); i >= 0 {
				return strings.TrimSpace(s[i+1:])
			}
			return ""
		}
	}
	word, rest := cutWord(s)
	if strings.HasPrefix(word, "#") || cssColors[strings.ToLower(word)] {
		return rest
	}
	return s
}

func (p *sparser) statement(line string) error {
	word, rest := cutWord(line)
	switch kw := strings.ToLower(word); {
	case kw == "participant" || kw == "actor":
		return p.declare(rest, kw == "actor")
	case kw == "create":
		w2, r2 := cutWord(rest)
		if lw := strings.ToLower(w2); lw == "participant" || lw == "actor" {
			return p.declare(r2, lw == "actor")
		}
		return p.errorf("create needs a participant or an actor")
	case kw == "destroy":
		_, err := p.participant(strings.TrimSpace(rest))
		return err
	case kw == "box":
		if p.box >= 0 {
			return p.errorf("a box inside a box")
		}
		p.box = len(p.sd.boxes)
		p.sd.boxes = append(p.sd.boxes, seqBox{label: stripColor(rest)})
		p.open = append(p.open, "box")
		return nil
	case kw == "end":
		if len(p.open) == 0 {
			return p.errorf("end without a block")
		}
		p.closeOne()
		return nil
	case blockKinds[kw]:
		text := rest
		if kw == "rect" {
			text = ""
		}
		if kw == "par_over" {
			kw = "par"
		}
		p.sd.events = append(p.sd.events, sevent{kind: evBlockStart, block: kw, text: text})
		p.open = append(p.open, kw)
		return nil
	case kw == "else" || kw == "and" || kw == "option":
		if len(p.open) == 0 || p.open[len(p.open)-1] == "box" {
			return p.errorf("%s outside a block", kw)
		}
		p.sd.events = append(p.sd.events, sevent{kind: evBlockDivider, text: rest})
		return nil
	case kw == "note":
		return p.note(rest)
	case kw == "activate" || kw == "deactivate":
		i, err := p.participant(strings.TrimSpace(rest))
		if err != nil {
			return err
		}
		k := evActivate
		if kw == "deactivate" {
			k = evDeactivate
		}
		p.sd.events = append(p.sd.events, sevent{kind: k, from: i})
		return nil
	case kw == "autonumber":
		if rest == "off" {
			p.visible = false
			return nil
		}
		p.visible = true
		args := strings.Fields(rest)
		if len(args) > 0 {
			if v, err := strconv.Atoi(args[0]); err == nil {
				p.number = v
			}
		}
		if len(args) > 1 {
			if v, err := strconv.Atoi(args[1]); err == nil {
				p.step = v
			}
		}
		return nil
	case kw == "title" || strings.HasPrefix(kw, "title:"):
		p.sd.title = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, line[:5]), ":"))
		return nil
	case strings.HasPrefix(kw, "acctitle") || strings.HasPrefix(kw, "accdescr"),
		kw == "link" || kw == "links" || kw == "properties" || kw == "details":
		return nil
	}
	return p.message(line)
}

// declare reads `Id`, `Id as Label` and `Id@{ ... } as Label`.
func (p *sparser) declare(rest string, actor bool) error {
	if rest == "" {
		return p.errorf("participant without a name")
	}
	id, label := rest, ""
	if i := strings.Index(rest, " as "); i >= 0 {
		id, label = strings.TrimSpace(rest[:i]), strings.TrimSpace(rest[i+4:])
	}
	if i := strings.Index(id, "@{"); i >= 0 {
		props := parseProps(strings.TrimSuffix(strings.TrimSpace(id[i+2:]), "}"))
		id = strings.TrimSpace(id[:i])
		if alias, ok := props["alias"]; ok && label == "" {
			label = alias
		}
		if props["type"] == "actor" {
			actor = true
		}
	}
	if id == "" || strings.ContainsAny(id, " \t") {
		return p.errorf("bad participant name %q", id)
	}
	i, err := p.participant(id)
	if err != nil {
		return err
	}
	pt := &p.sd.parts[i]
	if label != "" {
		pt.label = label
	}
	pt.actor = pt.actor || actor
	if p.box >= 0 {
		pt.box = p.box
	}
	return nil
}

func (p *sparser) note(rest string) error {
	where, ids, ok := strings.Cut(rest, ":")
	if !ok {
		return p.errorf("note without text")
	}
	text := strings.TrimSpace(ids)
	lw := strings.ToLower(strings.TrimSpace(where))
	ev := sevent{kind: evNote, text: text}
	var names string
	switch {
	case strings.HasPrefix(lw, "left of "):
		ev.where, names = noteLeft, where[strings.Index(lw, "left of ")+8:]
	case strings.HasPrefix(lw, "right of "):
		ev.where, names = noteRight, where[strings.Index(lw, "right of ")+9:]
	case strings.HasPrefix(lw, "over "):
		ev.where, names = noteOver, where[strings.Index(lw, "over ")+5:]
	default:
		return p.errorf("note needs left of, right of or over")
	}
	a, b, two := strings.Cut(names, ",")
	var err error
	if ev.from, err = p.participant(strings.TrimSpace(a)); err != nil {
		return err
	}
	ev.to = ev.from
	if two {
		if ev.to, err = p.participant(strings.TrimSpace(b)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(a) == "" || two && strings.TrimSpace(b) == "" {
		return p.errorf("note needs a participant")
	}
	p.sd.events = append(p.sd.events, ev)
	return nil
}

func (p *sparser) message(line string) error {
	for i := 1; i < len(line); i++ {
		for _, a := range arrows {
			if !strings.HasPrefix(line[i:], a.tok) {
				continue
			}
			from := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line[:i]), "()"))
			rest := strings.TrimSpace(line[i+len(a.tok):])
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "()"))
			ev := sevent{kind: evMessage, dotted: a.dotted, head: a.head, both: a.both}
			if strings.HasPrefix(rest, "+") {
				ev.activate, rest = true, rest[1:]
			} else if strings.HasPrefix(rest, "-") {
				ev.deactivate, rest = true, rest[1:]
			}
			to, text, _ := strings.Cut(rest, ":")
			to = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(to), "()"))
			if from == "" || to == "" || strings.ContainsAny(from+to, "<>") {
				return p.errorf("a message needs a sender and a receiver")
			}
			var err error
			if ev.from, err = p.participant(from); err != nil {
				return err
			}
			if ev.to, err = p.participant(to); err != nil {
				return err
			}
			ev.text = strings.TrimSpace(text)
			if p.visible {
				ev.number = p.number
			}
			p.number += p.step
			p.sd.events = append(p.sd.events, ev)
			return nil
		}
	}
	return p.errorf("unknown statement %q", excerpt(line))
}

// Sizes at a 16px label size, scaled to the size asked for. Most are Pele's.
const (
	qActorMargin = 40
	qMinW        = 64
	qPadX        = 12
	qPadY        = 10
	qMsgGap      = 14
	qMsgPad      = 12
	qSelfW       = 28
	qSelfH       = 22
	qActW        = 10
	qActStep     = 4
	qActMin      = 14
	qNoteGap     = 10
	qNoteOver    = 16
	qNotePad     = 10
	qFramePad    = 16
	qFrameNest   = 10
	qTabH        = 20
	qBadgeH      = 16
	qBoxPad      = 10
	qFigure      = 34
)

type column struct {
	lab         label
	actor       bool
	w, h        float64
	x           float64
	top         float64
	activations []float64
}

type frame struct {
	kind     string
	x0, x1   float64
	top      float64
	title    label
	tabW     float64
	dividers []int
	divY     []float64
	divLabs  []label
}

func layoutSequence(sd *sequence, size float64) *Scene {
	s := size / 16
	small := size * 0.875
	tiny := size * 0.75
	stroke := fStroke * s
	cols := make([]column, len(sd.parts))
	for i, pt := range sd.parts {
		lab := makeLabel(pt.label, size, 0, false)
		c := column{lab: lab, actor: pt.actor}
		if pt.actor {
			c.w, c.h = math.Max(40*s, lab.w+8*s), qFigure*s+4*s+lab.h
		} else {
			c.w = math.Max(qMinW*s, lab.w+2*qPadX*s)
			c.h = math.Max(size*lineHeight+20*s, lab.h+2*qPadY*s)
		}
		cols[i] = c
	}
	last := len(cols) - 1
	needs := map[[2]int]float64{}
	need := func(lo, hi int, w float64) {
		if lo < 0 || hi > last || lo >= hi {
			return
		}
		key := [2]int{lo, hi}
		needs[key] = math.Max(needs[key], w)
	}
	badgeW := func(n int) float64 {
		if n == 0 {
			return 0
		}
		return math.Max(qBadgeH*s, textWidth(strconv.Itoa(n), tiny)+8*s)
	}
	labs := make([]label, len(sd.events))
	noteW := make([]float64, len(sd.events))
	for i, ev := range sd.events {
		a, b := ev.from, ev.to
		switch ev.kind {
		case evNote:
			lab := makeLabel(ev.text, small, 0, false)
			labs[i] = lab
			w := lab.w + 2*qNotePad*s
			noteW[i] = w
			switch {
			case ev.where == noteLeft:
				need(a-1, a, w+2*qNoteGap*s+qActW*s)
			case ev.where == noteRight:
				need(a, a+1, w+2*qNoteGap*s+qActW*s)
			case a == b:
				need(a-1, a, w/2+qNoteGap*s)
				need(a, a+1, w/2+qNoteGap*s)
			default:
				need(min(a, b), max(a, b), w-2*qNoteOver*s)
			}
		case evMessage:
			lab := makeLabel(ev.text, small, 0, false)
			labs[i] = lab
			if a == b {
				need(a, a+1, qSelfW*s+8*s+lab.w+16*s+qActW*s+badgeW(ev.number))
			} else {
				need(min(a, b), max(a, b), lab.w+2*qMsgPad*s+qActW*s+badgeW(ev.number))
			}
		}
	}

	boxPad := qBoxPad * s
	gaps := make([]float64, max(last, 0))
	for i := 0; i < last; i++ {
		l, r := sd.parts[i], sd.parts[i+1]
		boxed := 0.0
		if l.box != r.box {
			if l.box >= 0 {
				boxed += boxPad
			}
			if r.box >= 0 {
				boxed += boxPad
			}
		}
		gaps[i] = math.Max(cols[i].w/2+cols[i+1].w/2+qActorMargin*s+boxed, needs[[2]int{i, i + 1}])
	}
	// A message across several lifelines widens each gap it crosses by an equal share of what is missing.
	var spans [][2]int
	for key := range needs {
		if key[1]-key[0] > 1 {
			spans = append(spans, key)
		}
	}
	sortSpans(spans)
	for _, key := range spans {
		total := 0.0
		for i := key[0]; i < key[1]; i++ {
			total += gaps[i]
		}
		if w := needs[key]; total < w {
			share := (w - total) / float64(key[1]-key[0])
			for i := key[0]; i < key[1]; i++ {
				gaps[i] += share
			}
		}
	}
	type run struct {
		first, last int
		title       label
		grow        float64
	}
	var runs []run
	for i, pt := range sd.parts {
		if pt.box < 0 {
			continue
		}
		if n := len(runs); n > 0 && sd.parts[runs[n-1].last].box == pt.box && runs[n-1].last == i-1 {
			runs[n-1].last = i
			continue
		}
		runs = append(runs, run{first: i, last: i, title: makeLabel(sd.boxes[pt.box].label, small, 0, false)})
	}
	place := func() {
		for i := 1; i <= last; i++ {
			cols[i].x = cols[i-1].x + gaps[i-1]
		}
	}
	place()
	for k := range runs {
		r := &runs[k]
		inner := cols[r.last].x + cols[r.last].w/2 - (cols[r.first].x - cols[r.first].w/2)
		r.grow = math.Max(0, (r.title.w+2*boxPad-inner)/2)
		if r.grow == 0 {
			continue
		}
		if r.first > 0 {
			gaps[r.first-1] += r.grow
		}
		if r.last < last {
			gaps[r.last] += r.grow
		}
	}
	place()

	// Horizontal extents of every block, from what it holds.
	framePad, frameNest := qFramePad*s, qFrameNest*s
	frames := make([]*frame, len(sd.events))
	var open []*frame
	spanAll := func() (float64, float64) {
		if last < 0 {
			return 0, 120 * s
		}
		return cols[0].x - framePad, cols[last].x + framePad
	}
	noteX := make([]float64, len(sd.events))
	for i, ev := range sd.events {
		var x0, x1 float64
		switch ev.kind {
		case evBlockStart:
			f := &frame{kind: ev.block, x0: math.Inf(1), x1: math.Inf(-1), dividers: []int{i}}
			frames[i] = f
			open = append(open, f)
			continue
		case evBlockDivider:
			if n := len(open); n > 0 {
				open[n-1].dividers = append(open[n-1].dividers, i)
			}
			continue
		case evBlockEnd:
			n := len(open)
			if n == 0 {
				continue
			}
			f := open[n-1]
			open = open[:n-1]
			frames[i] = f
			if f.x0 > f.x1 {
				f.x0, f.x1 = spanAll()
			}
			if f.kind != "rect" {
				f.tabW = textWidth(f.kind, tiny) + 14*s
				for k, d := range f.dividers {
					raw := sd.events[d].text
					tab := 0.0
					if k == 0 {
						tab = f.tabW + 8*s
					}
					room := f.x1 - f.x0 - 16*s - tab
					var lab label
					if raw != "" {
						lab = makeLabel("["+raw+"]", small, 0, false)
					}
					if lab.w > room {
						f.x1 += lab.w - room
					}
					if k == 0 {
						f.title = lab
					} else {
						f.divLabs = append(f.divLabs, lab)
					}
				}
			}
			if n > 1 {
				p := open[n-2]
				p.x0 = math.Min(p.x0, f.x0-frameNest)
				p.x1 = math.Max(p.x1, f.x1+frameNest)
			}
			continue
		case evNote:
			a, b := cols[ev.from], cols[ev.to]
			w := noteW[i]
			switch {
			case ev.where == noteLeft:
				noteX[i] = a.x - qNoteGap*s - w
			case ev.where == noteRight:
				noteX[i] = a.x + qNoteGap*s
			case ev.from == ev.to:
				noteX[i] = a.x - w/2
			default:
				lo := math.Min(a.x, b.x) - qNoteOver*s
				hi := math.Max(a.x, b.x) + qNoteOver*s
				noteW[i] = math.Max(hi-lo, w)
				noteX[i] = (lo + hi - noteW[i]) / 2
			}
			x0, x1 = noteX[i], noteX[i]+noteW[i]
		case evMessage:
			a, b := cols[ev.from], cols[ev.to]
			if ev.from == ev.to {
				x0, x1 = a.x, a.x+qActW*s+qSelfW*s+8*s+labs[i].w+badgeW(ev.number)
			} else {
				x0, x1 = math.Min(a.x, b.x), math.Max(a.x, b.x)
			}
		default:
			continue
		}
		if n := len(open); n > 0 {
			f := open[n-1]
			f.x0 = math.Min(f.x0, x0-framePad)
			f.x1 = math.Max(f.x1, x1+framePad)
		}
	}

	// Layers of the drawing, in the order they are painted.
	var boxes, fills, lifelines, acts, outlines, lines, backs, notes, heads scene
	var texts scene
	headTop := 0.0
	if len(runs) > 0 {
		headTop = boxPad
		for _, r := range runs {
			if r.title.h > 0 {
				headTop = math.Max(headTop, boxPad+r.title.h+4*s)
			}
		}
	}
	rowH := 0.0
	for _, c := range cols {
		rowH = math.Max(rowH, c.h)
	}
	headBottom := headTop + rowH
	for i := range cols {
		cols[i].top = headBottom - cols[i].h
	}
	leftEdge := func(c *column, extra int) float64 {
		if len(c.activations)+extra > 0 {
			return c.x - qActW*s/2
		}
		return c.x
	}
	rightEdge := func(c *column, extra int) float64 {
		if d := len(c.activations) + extra; d > 0 {
			return c.x + qActW*s/2 + float64(d-1)*qActStep*s
		}
		return c.x
	}
	endActivation := func(c *column, end float64) float64 {
		n := len(c.activations)
		start := c.activations[n-1]
		c.activations = c.activations[:n-1]
		bottom := math.Max(end, start+qActMin*s)
		x := c.x - qActW*s/2 + float64(len(c.activations))*qActStep*s
		acts.shape(rectPath(x, start, qActW*s, bottom-start), PaintSurface, PaintBorder, s, nil)
		return bottom
	}
	var shaded []rect
	var pendingBacks []rect
	// A label that a lifeline runs through gets a patch of the background behind it.
	backed := func(lab label, cx, cy float64, color Paint) {
		if lab.w == 0 {
			return
		}
		l, r := cx-lab.w/2, cx+lab.w/2
		for _, c := range cols {
			if c.x > l && c.x < r {
				pendingBacks = append(pendingBacks, rect{l - 3*s, cy - lab.h/2 + 2*s, r + 3*s, cy + lab.h/2 - 2*s})
				break
			}
		}
		texts.label(lab, cx, cy, color, false)
	}
	badge := func(n int, cx, cy float64) {
		w := badgeW(n)
		lines.shape(roundRectPath(cx-w/2, cy-qBadgeH*s/2, w, qBadgeH*s, qBadgeH*s/2), PaintLine, PaintNone, 0, nil)
		texts.label(makeLabel(strconv.Itoa(n), tiny, 0, false), cx, cy, PaintBackground, false)
	}
	head := func(kind headKind, x, y, dir float64) {
		switch kind {
		case headArrow:
			heads.shape(arrowHead(x, y, dir, 0, fArrow*s), PaintLine, PaintNone, 0, nil)
		case headCross:
			lines.shape(crossHead(x, y, dir, 0, 4*s), PaintNone, PaintLine, stroke, nil)
		case headOpen:
			var p path
			p.move(x-dir*fArrow*s, y-4.5*s)
			p.line(x, y)
			p.line(x-dir*fArrow*s, y+4.5*s)
			lines.shape(p.ops, PaintNone, PaintLine, stroke, nil)
		}
	}
	trim := func(kind headKind) float64 {
		switch kind {
		case headArrow:
			return (fArrow - 1) * s
		case headCross:
			return 4 * s
		}
		return 0
	}
	var dash []float32
	dashed := func() []float32 {
		if dash == nil {
			dash = []float32{f32(4 * s), f32(4 * s)}
		}
		return dash
	}

	y := headBottom + 6*s
	var stack []*frame
	for i, ev := range sd.events {
		switch ev.kind {
		case evActivate:
			cols[ev.from].activations = append(cols[ev.from].activations, y)
		case evDeactivate:
			if c := &cols[ev.from]; len(c.activations) > 0 {
				y = math.Max(y, endActivation(c, y))
			}
		case evBlockStart:
			f := frames[i]
			if f == nil {
				continue
			}
			stack = append(stack, f)
			if f.kind == "rect" {
				f.top = y + 8*s
				y = f.top
			} else {
				f.top = y + 12*s
				y = f.top + math.Max(qTabH*s, f.title.h+2*s)
			}
		case evBlockDivider:
			if n := len(stack); n > 0 {
				f := stack[n-1]
				k := len(f.divY)
				f.divY = append(f.divY, y+12*s)
				if k < len(f.divLabs) {
					y += 12*s + f.divLabs[k].h
				} else {
					y += 12 * s
				}
			}
		case evBlockEnd:
			f := frames[i]
			if f == nil || len(stack) == 0 {
				continue
			}
			stack = stack[:len(stack)-1]
			bottom := y + 12*s
			box := rect{f.x0, f.top, f.x1, bottom}
			shaded = append(shaded, box)
			// Blocks close from the inside out; an outer block's fill must go under the inner ones.
			fills.shapes = append([]Shape{{Path: rectPath(f.x0, f.top, f.x1-f.x0, bottom-f.top), Fill: PaintSurfaceAlt}}, fills.shapes...)
			if f.kind != "rect" {
				outlines.shape(rectPath(f.x0, f.top, f.x1-f.x0, bottom-f.top), PaintNone, PaintBorder, s, nil)
				for k, dy := range f.divY {
					var p path
					p.move(f.x0, dy)
					p.line(f.x1, dy)
					outlines.shape(p.ops, PaintNone, PaintBorder, s, dashed())
					if k < len(f.divLabs) {
						lab := f.divLabs[k]
						backed(lab, f.x0+8*s+lab.w/2, dy+2*s+lab.h/2, PaintMuted)
					}
				}
				outlines.shape(rectPath(f.x0, f.top, f.tabW, qTabH*s), PaintSurface, PaintBorder, s, nil)
				kind := makeLabel(f.kind, tiny, 0, false)
				texts.labelAt(kind, f.x0+(f.tabW-kind.w)/2, f.top+(qTabH*s-kind.h)/2, PaintMuted, true, AlignCenter)
				if f.title.w > 0 {
					th := math.Max(qTabH*s, f.title.h)
					backed(f.title, f.x0+f.tabW+8*s+f.title.w/2, f.top+th/2, PaintMuted)
				}
			}
			y = bottom
		case evNote:
			c := &cols[ev.from]
			lab := labs[i]
			h := lab.h + 2*math.Max(4*s, qNotePad*s-4*s)
			top := y + 10*s
			x := noteX[i]
			switch ev.where {
			case noteRight:
				x += rightEdge(c, 0) - c.x
			case noteLeft:
				x += leftEdge(c, 0) - c.x
			}
			notes.shape(rectPath(x, top, noteW[i], h), PaintSurface, PaintBorder, s, nil)
			texts.label(lab, x+noteW[i]/2, top+h/2, PaintForeground, false)
			y = top + h
		case evMessage:
			a, b := &cols[ev.from], &cols[ev.to]
			lab := labs[i]
			bw := badgeW(ev.number)
			// The activation a + starts belongs to this message, so the arrow stops at its edge.
			starting := 0
			if ev.activate {
				starting = 1
			}
			for _, e := range sd.events[i+1:] {
				if e.kind != evActivate {
					break
				}
				if e.from == ev.to {
					starting++
				}
			}
			var dash []float32
			if ev.dotted {
				dash = dashed()
			}
			ht := trim(ev.head)
			var lineY float64
			if ev.from == ev.to {
				top := y + qMsgGap*s + math.Max(0, (lab.h-qSelfH*s)/2)
				bottom := top + qSelfH*s
				x0, x1 := rightEdge(a, 0), rightEdge(a, starting)
				out := math.Max(x0, x1) + qSelfW*s + bw/2
				if bw > 0 {
					badge(ev.number, x0, top)
					x0 += bw / 2
				}
				from := x0
				if ev.both {
					from += ht
					head(ev.head, x0, top, -1)
				}
				pts := []pt{{from, top}, {out, top}, {out, bottom}, {x1 + ht, bottom}}
				lines.shape(routePath(pts, 6*s), PaintNone, PaintLine, stroke, dash)
				head(ev.head, x1, bottom, -1)
				texts.labelAt(lab, out+8*s, (top+bottom)/2-lab.h/2, PaintForeground, false, AlignStart)
				lineY = bottom
			} else {
				dir := 1.0
				if b.x < a.x {
					dir = -1
				}
				lineY = y + qMsgGap*s + math.Max(lab.h+2*s, 6*s)
				x0, x1 := rightEdge(a, 0), leftEdge(b, starting)
				if dir < 0 {
					x0, x1 = leftEdge(a, 0), rightEdge(b, starting)
				}
				if bw > 0 {
					badge(ev.number, x0, lineY)
					x0 += dir * bw / 2
				}
				from, to := x0, x1-dir*ht
				if ev.both {
					from += dir * ht
					head(ev.head, x0, lineY, -dir)
				}
				var p path
				p.move(from, lineY)
				p.line(to, lineY)
				lines.shape(p.ops, PaintNone, PaintLine, stroke, dash)
				head(ev.head, x1, lineY, dir)
				if lab.w > 0 {
					backed(lab, (math.Min(x0, x1)+math.Max(x0, x1))/2, lineY-4*s-lab.h/2, PaintForeground)
				}
			}
			y = math.Max(y, lineY)
			if ev.activate {
				b.activations = append(b.activations, y)
			}
			if ev.deactivate && len(a.activations) > 0 {
				y = math.Max(y, endActivation(a, y))
			}
		}
	}
	y += 20 * s
	for i := range cols {
		for len(cols[i].activations) > 0 {
			endActivation(&cols[i], y)
		}
	}
	for _, c := range cols {
		var p path
		p.move(c.x, headBottom)
		p.line(c.x, y)
		lifelines.shape(p.ops, PaintNone, PaintLine, s, dashed())
		if c.actor {
			drawFigure(&heads, c.x, c.top, s, stroke)
			texts.label(c.lab, c.x, c.top+qFigure*s+4*s+c.lab.h/2, PaintForeground, false)
		} else {
			heads.shape(rectPath(c.x-c.w/2, c.top, c.w, c.h), PaintSurface, PaintBorder, stroke, nil)
			texts.label(c.lab, c.x, c.top+c.h/2, PaintForeground, false)
		}
	}
	for _, r := range runs {
		x0 := cols[r.first].x - cols[r.first].w/2 - boxPad - r.grow
		x1 := cols[r.last].x + cols[r.last].w/2 + boxPad + r.grow
		boxes.shape(rectPath(x0, 0, x1-x0, y+boxPad), PaintNone, PaintBorder, s, nil)
		texts.label(r.title, (x0+x1)/2, boxPad/2+2*s+r.title.h/2, PaintMuted, false)
	}
	for _, r := range pendingBacks {
		color := PaintBackground
		cx, cy := (r.x0+r.x1)/2, (r.y0+r.y1)/2
		for _, sh := range shaded {
			if sh.contains(cx, cy) {
				color = PaintSurfaceAlt
			}
		}
		backs.shape(rectPath(r.x0, r.y0, r.x1-r.x0, r.y1-r.y0), color, PaintNone, 0, nil)
	}

	out := &scene{kind: "sequence"}
	if sd.title != "" {
		lab := makeLabel(sd.title, size, 0, false)
		lo, hi := 0.0, 0.0
		if last >= 0 {
			lo, hi = cols[0].x-cols[0].w/2, cols[last].x+cols[last].w/2
		}
		out.label(lab, (lo+hi)/2, -lab.h/2-12*s, PaintForeground, true)
	}
	for _, layer := range []*scene{&boxes, &fills, &lifelines, &acts, &outlines, &lines, &backs, &notes, &heads} {
		out.shapes = append(out.shapes, layer.shapes...)
	}
	out.texts = append(out.texts, texts.texts...)
	return out.finish(2, sd.title)
}

func sortSpans(spans [][2]int) {
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0; j-- {
			a, b := spans[j-1], spans[j]
			la, lb := a[1]-a[0], b[1]-b[0]
			if la < lb || la == lb && a[0] <= b[0] {
				break
			}
			spans[j-1], spans[j] = b, a
		}
	}
}

// drawFigure draws a stick figure whose top center is at (cx, top).
func drawFigure(sc *scene, cx, top, s, stroke float64) {
	sc.shape(ellipsePath(cx, top+7*s, 6*s, 6*s), PaintSurface, PaintBorder, stroke, nil)
	var p path
	p.move(cx, top+13*s)
	p.line(cx, top+24*s)
	p.move(cx-10*s, top+17*s)
	p.line(cx+10*s, top+17*s)
	p.move(cx-8*s, top+34*s)
	p.line(cx, top+24*s)
	p.line(cx+8*s, top+34*s)
	sc.shape(p.ops, PaintNone, PaintBorder, stroke, nil)
}
