package mermaid

import (
	"fmt"
	"strconv"
	"strings"
)

type shapeKind uint8

const (
	shRect shapeKind = iota
	shRound
	shStadium
	shSubroutine
	shCylinder
	shCircle
	shDoubleCircle
	shOdd
	shDiamond
	shHexagon
	shLeanR
	shLeanL
	shTrapB
	shTrapT
	shText
)

type markKind uint8

const (
	markNone markKind = iota
	markArrow
	markCircle
	markCross
)

type strokeKind uint8

const (
	strokeNormal strokeKind = iota
	strokeThick
	strokeDotted
	strokeInvisible
)

// fitem is a node or a subgraph; a subgraph's label is its title.
type fitem struct {
	id       string
	label    string
	markdown bool
	shape    shapeKind
	group    bool
	parent   int
	// For a subgraph, the order in which it was closed.
	closed int
}

type fedge struct {
	from, to   int
	label      string
	markdown   bool
	stroke     strokeKind
	start, end markKind
	minlen     int
}

type flowchart struct {
	dir   string
	title string
	items []*fitem
	edges []fedge
}

type fparser struct {
	s        string
	i        int
	line     int
	fc       *flowchart
	index    map[string]int
	stack    []int
	mentions [][]int
	closed   int
	edgeIDs  map[string]bool
	groups   int
}

func parseFlowchart(lines []string, header int) (*flowchart, error) {
	p := &fparser{
		s: strings.Join(lines[header:], "\n"), line: header + 1,
		fc: &flowchart{dir: "TB"}, index: map[string]int{}, edgeIDs: map[string]bool{},
	}
	p.skipSpaces()
	for p.i < len(p.s) && p.s[p.i] > ' ' && p.s[p.i] != ';' {
		p.i++
	}
	p.skipSpaces()
	if tok := p.token(); tok != "" {
		switch tok {
		case "TB", "TD", "BT", "LR", "RL":
			p.fc.dir = tok
			if tok == "TD" {
				p.fc.dir = "TB"
			}
		default:
			return nil, p.errorf("unknown direction %q", tok)
		}
	}
	if err := p.run(); err != nil {
		return nil, err
	}
	p.resolve()
	return p.fc, nil
}

// token reads a run of non-space characters up to a `;`.
func (p *fparser) token() string {
	j := p.i
	for j < len(p.s) && p.s[j] > ' ' && p.s[j] != ';' {
		j++
	}
	t := p.s[p.i:j]
	p.i = j
	return t
}

func (p *fparser) errorf(format string, args ...any) error {
	return &Error{Line: p.line, Msg: fmt.Sprintf(format, args...)}
}

func (p *fparser) eof() bool              { return p.i >= len(p.s) }
func (p *fparser) has(prefix string) bool { return strings.HasPrefix(p.s[p.i:], prefix) }
func (p *fparser) at(c byte) bool         { return p.i < len(p.s) && p.s[p.i] == c }

func (p *fparser) skipSpaces() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t') {
		p.i++
	}
}

func (p *fparser) skipLine() {
	for p.i < len(p.s) && p.s[p.i] != '\n' {
		p.i++
	}
}

// word is the keyword at the cursor, if a run of letters ends there at a
// boundary, without moving past it.
func (p *fparser) word() string {
	j := p.i
	for j < len(p.s) && (p.s[j] >= 'a' && p.s[j] <= 'z' || p.s[j] >= 'A' && p.s[j] <= 'Z') {
		j++
	}
	if j < len(p.s) && strings.IndexByte(" \t\n;:{", p.s[j]) < 0 {
		return ""
	}
	return p.s[p.i:j]
}

func (p *fparser) run() error {
	for {
		for p.i < len(p.s) && strings.IndexByte(" \t\n;", p.s[p.i]) >= 0 {
			if p.s[p.i] == '\n' {
				p.line++
			}
			p.i++
		}
		if p.eof() {
			break
		}
		if p.has("%%") {
			p.skipLine()
			continue
		}
		var err error
		switch w := p.word(); w {
		case "subgraph":
			p.i += len(w)
			err = p.subgraph()
		case "end":
			p.i += len(w)
			if len(p.stack) == 0 {
				return p.errorf("end without a subgraph")
			}
			p.closeGroup()
		case "accDescr":
			p.i += len(w)
			p.skipSpaces()
			if p.at('{') {
				for p.i < len(p.s) && p.s[p.i] != '}' {
					if p.s[p.i] == '\n' {
						p.line++
					}
					p.i++
				}
				p.i++
			} else {
				p.skipLine()
			}
		case "direction", "classDef", "class", "style", "linkStyle", "click", "accTitle", "title":
			p.skipLine()
		default:
			err = p.chain()
		}
		if err != nil {
			return err
		}
		p.skipSpaces()
		if !p.eof() && !p.at('\n') && !p.at(';') && !p.has("%%") {
			return p.errorf("unexpected %q", excerpt(p.s[p.i:]))
		}
	}
	for len(p.stack) > 0 {
		p.closeGroup()
	}
	return nil
}

func excerpt(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 24 {
		s = s[:24] + "…"
	}
	return s
}

func (p *fparser) closeGroup() {
	g := p.stack[len(p.stack)-1]
	p.stack = p.stack[:len(p.stack)-1]
	p.fc.items[g].closed = p.closed
	p.closed++
}

// item returns the node with id, declaring it if it is new.
func (p *fparser) item(id string) (int, error) {
	if i, ok := p.index[id]; ok {
		return i, nil
	}
	if len(p.fc.items) >= maxNodes {
		return 0, p.errorf("more than %d nodes", maxNodes)
	}
	i := len(p.fc.items)
	p.fc.items = append(p.fc.items, &fitem{id: id, label: id, parent: -1, closed: -1})
	p.mentions = append(p.mentions, nil)
	p.index[id] = i
	return i, nil
}

// mention puts an item in the innermost open subgraph.
func (p *fparser) mention(i int) {
	if n := len(p.stack); n > 0 && p.stack[n-1] != i {
		p.mentions[i] = append(p.mentions[i], p.stack[n-1])
	}
}

func (p *fparser) subgraph() error {
	p.skipSpaces()
	j := p.i
	for j < len(p.s) && p.s[j] != '\n' && p.s[j] != ';' {
		j++
	}
	rest := strings.TrimSpace(p.s[p.i:j])
	p.i = j
	id, title := "", ""
	switch {
	case rest == "":
		return p.errorf("subgraph without a name")
	case rest[0] == '"':
		title = rest
	default:
		k := 0
		for k < len(rest) && rest[k] != ' ' && rest[k] != '\t' && rest[k] != '[' {
			k++
		}
		tail := strings.TrimSpace(rest[k:])
		switch {
		case tail == "":
			id, title = rest, rest
		case tail[0] == '[' && tail[len(tail)-1] == ']':
			id, title = rest[:k], strings.TrimSpace(tail[1:len(tail)-1])
		default:
			title = rest
		}
	}
	markdown := false
	if len(title) >= 2 && title[0] == '"' && title[len(title)-1] == '"' {
		title = title[1 : len(title)-1]
		if len(title) >= 2 && title[0] == '`' && title[len(title)-1] == '`' {
			title, markdown = title[1:len(title)-1], true
		}
	}
	if id == "" {
		id = "\x00subgraph" + strconv.Itoa(p.groups)
	}
	p.groups++
	g, err := p.item(id)
	if err != nil {
		return err
	}
	it := p.fc.items[g]
	it.group, it.label, it.markdown = true, title, markdown
	p.mention(g)
	p.stack = append(p.stack, g)
	return nil
}

// chain reads `a & b --> c --> d` and its kin.
func (p *fparser) chain() error {
	group, any, err := p.nodeGroup()
	if err != nil {
		return err
	}
	if !any {
		return p.errorf("unexpected %q", excerpt(p.s[p.i:]))
	}
	for {
		p.skipSpaces()
		lk, ok, err := p.link()
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		p.skipSpaces()
		next, any, err := p.nodeGroup()
		if err != nil {
			return err
		}
		if !any || len(next) == 0 || len(group) == 0 {
			return p.errorf("a link needs a node at each end")
		}
		for _, a := range group {
			for _, b := range next {
				if len(p.fc.edges) >= maxEdges {
					return p.errorf("more than %d links", maxEdges)
				}
				lk.from, lk.to = a, b
				p.fc.edges = append(p.fc.edges, lk)
			}
		}
		group = next
	}
}

func (p *fparser) nodeGroup() ([]int, bool, error) {
	var out []int
	any := false
	for {
		i, ok, err := p.node()
		if err != nil || !ok {
			return out, any, err
		}
		any = true
		if i >= 0 {
			out = append(out, i)
		}
		save := p.i
		p.skipSpaces()
		if !p.at('&') {
			p.i = save
			return out, any, nil
		}
		p.i++
		p.skipSpaces()
	}
}

func isLinkStart(s string) bool {
	return strings.HasPrefix(s, "--") || strings.HasPrefix(s, "-.") || strings.HasPrefix(s, "==") || strings.HasPrefix(s, "~~~")
}

func identEnd(s string, i int) int {
	j := i
	for j < len(s) {
		c := s[j]
		if c <= ' ' || strings.IndexByte("[](){}<>|&;\"`,@", c) >= 0 ||
			c == ':' && strings.HasPrefix(s[j:], ":::") ||
			(c == '-' || c == '=' || c == '~') && isLinkStart(s[j:]) {
			break
		}
		j++
	}
	return j
}

type opener struct {
	open   string
	closes []string
	shapes []shapeKind
}

var openers = []opener{
	{"(((", []string{")))"}, []shapeKind{shDoubleCircle}},
	{"((", []string{"))"}, []shapeKind{shCircle}},
	{"([", []string{"])"}, []shapeKind{shStadium}},
	{"(", []string{")"}, []shapeKind{shRound}},
	{"[[", []string{"]]"}, []shapeKind{shSubroutine}},
	{"[(", []string{")]"}, []shapeKind{shCylinder}},
	{"[/", []string{"/]", `\]`}, []shapeKind{shLeanR, shTrapB}},
	{`[\`, []string{`\]`, "/]"}, []shapeKind{shLeanL, shTrapT}},
	{"[", []string{"]"}, []shapeKind{shRect}},
	{">", []string{"]"}, []shapeKind{shOdd}},
	{"{{", []string{"}}"}, []shapeKind{shHexagon}},
	{"{", []string{"}"}, []shapeKind{shDiamond}},
}

var shapeNames = map[string]shapeKind{
	"rect": shRect, "proc": shRect, "process": shRect, "rectangle": shRect, "square": shRect,
	"rounded": shRound, "event": shRound, "round": shRound,
	"stadium": shStadium, "terminal": shStadium, "pill": shStadium,
	"fr-rect": shSubroutine, "subprocess": shSubroutine, "subproc": shSubroutine, "framed-rectangle": shSubroutine, "subroutine": shSubroutine,
	"cyl": shCylinder, "db": shCylinder, "database": shCylinder, "cylinder": shCylinder,
	"circle": shCircle, "circ": shCircle, "sm-circ": shCircle, "small-circle": shCircle, "start": shCircle, "f-circ": shCircle, "junction": shCircle,
	"dbl-circ": shDoubleCircle, "double-circle": shDoubleCircle, "fr-circ": shDoubleCircle, "framed-circle": shDoubleCircle, "stop": shDoubleCircle,
	"diam": shDiamond, "decision": shDiamond, "diamond": shDiamond, "question": shDiamond,
	"hex": shHexagon, "hexagon": shHexagon, "prepare": shHexagon,
	"lean-r": shLeanR, "lean-right": shLeanR, "in-out": shLeanR,
	"lean-l": shLeanL, "lean-left": shLeanL, "out-in": shLeanL,
	"trap-b": shTrapB, "priority": shTrapB, "trapezoid-bottom": shTrapB, "trapezoid": shTrapB,
	"trap-t": shTrapT, "manual": shTrapT, "trapezoid-top": shTrapT, "inv-trapezoid": shTrapT,
	"odd": shOdd, "text": shText,
}

// node reads one node with its optional shape, class and properties. It
// returns -1 for the properties of an edge, which name no node.
func (p *fparser) node() (int, bool, error) {
	j := identEnd(p.s, p.i)
	if j == p.i {
		return 0, false, nil
	}
	id := p.s[p.i:j]
	p.i = j
	var text string
	shape := shRect
	shaped, markdown := false, false
	if !p.eof() && strings.IndexByte("([{>", p.s[p.i]) >= 0 {
		var err error
		text, shape, markdown, err = p.shape()
		if err != nil {
			return 0, false, err
		}
		shaped = true
	}
	props := false
	for {
		if p.has(":::") {
			p.i = identEnd(p.s, p.i+3)
			continue
		}
		if p.has("@{") {
			body, err := p.braces()
			if err != nil {
				return 0, false, err
			}
			props = true
			kv := parseProps(body)
			if v, ok := kv["shape"]; ok {
				shape, shaped = shapeNames[v], true
			}
			if v, ok := kv["label"]; ok {
				text, shaped = v, true
				if len(text) >= 2 && text[0] == '`' && text[len(text)-1] == '`' {
					text, markdown = text[1:len(text)-1], true
				}
			} else if shaped && text == "" {
				text = id
			}
			continue
		}
		break
	}
	if props && p.edgeIDs[id] {
		return -1, true, nil
	}
	i, err := p.item(id)
	if err != nil {
		return 0, false, err
	}
	if shaped {
		it := p.fc.items[i]
		it.label, it.shape, it.markdown = text, shape, markdown
	}
	p.mention(i)
	return i, true, nil
}

// shape reads a node's bracketed text: the longest opener at the cursor whose close follows.
func (p *fparser) shape() (string, shapeKind, bool, error) {
	for _, op := range openers {
		if !p.has(op.open) {
			continue
		}
		j := p.i + len(op.open)
		for j < len(p.s) && p.s[j] == ' ' {
			j++
		}
		if j < len(p.s) && p.s[j] == '"' {
			text, k, markdown, ok := quoted(p.s, j)
			if !ok {
				return "", 0, false, p.errorf("unclosed string")
			}
			for k < len(p.s) && p.s[k] == ' ' {
				k++
			}
			for ci, c := range op.closes {
				if strings.HasPrefix(p.s[k:], c) {
					p.line += strings.Count(p.s[p.i:k], "\n")
					p.i = k + len(c)
					return text, op.shapes[ci], markdown, nil
				}
			}
			continue
		}
		// A single bracket may hold brackets of its own kind, in pairs.
		nests := op.open == "(" || op.open == "[" || op.open == "{"
		end, which, depth := -1, 0, 0
		for k := p.i + len(op.open); end < 0 && k < len(p.s) && p.s[k] != '\n'; k++ {
			if nests && p.s[k] == op.open[0] {
				depth++
				continue
			}
			for ci, c := range op.closes {
				if !strings.HasPrefix(p.s[k:], c) {
					continue
				}
				if depth > 0 {
					depth--
				} else {
					end, which = k, ci
				}
				break
			}
		}
		if end < 0 {
			continue
		}
		text := p.s[p.i+len(op.open) : end]
		p.i = end + len(op.closes[which])
		return text, op.shapes[which], false, nil
	}
	return "", 0, false, p.errorf("unclosed node shape")
}

// quoted reads a "string" starting at s[i], or a "`markdown string`".
func quoted(s string, i int) (text string, end int, markdown, ok bool) {
	if strings.HasPrefix(s[i:], "\"`") {
		if k := strings.Index(s[i+2:], "`\""); k >= 0 {
			return s[i+2 : i+2+k], i + 2 + k + 2, true, true
		}
	}
	if k := strings.IndexByte(s[i+1:], '"'); k >= 0 {
		return s[i+1 : i+1+k], i + 1 + k + 1, false, true
	}
	return "", 0, false, false
}

// braces reads `@{ ... }` and returns what is between the braces.
func (p *fparser) braces() (string, error) {
	start := p.i + 2
	inQuote := byte(0)
	for k := start; k < len(p.s); k++ {
		c := p.s[k]
		switch {
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			}
		case c == '"' || c == '\'':
			inQuote = c
		case c == '}':
			p.line += strings.Count(p.s[p.i:k], "\n")
			p.i = k + 1
			return p.s[start:k], nil
		}
	}
	return "", p.errorf("unclosed @{")
}

// parseProps reads the `key: value` pairs of a node's properties.
func parseProps(body string) map[string]string {
	out := map[string]string{}
	var parts []string
	inQuote := byte(0)
	last := 0
	for k := 0; k < len(body); k++ {
		c := body[k]
		switch {
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			}
		case c == '"' || c == '\'':
			inQuote = c
		case c == ',' || c == '\n':
			parts = append(parts, body[last:k])
			last = k + 1
		}
	}
	parts = append(parts, body[last:])
	for _, part := range parts {
		key, v, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		key = strings.Trim(strings.TrimSpace(key), `"'`)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[key] = v
	}
	return out
}

// link reads an arrow and its label, if one is at the cursor.
func (p *fparser) link() (fedge, bool, error) {
	save := p.i
	if j := identEnd(p.s, p.i); j > p.i && j < len(p.s) && p.s[j] == '@' && isLinkStart(strings.TrimLeft(p.s[j+1:], "<ox")) {
		p.edgeIDs[p.s[p.i:j]] = true
		p.i = j + 1
	}
	lk := fedge{minlen: 1}
	switch {
	case p.at('<'):
		lk.start = markArrow
		p.i++
	case (p.at('o') || p.at('x')) && p.i > 0 && (p.s[p.i-1] == ' ' || p.s[p.i-1] == '\t') && isLinkStart(p.s[p.i+1:]):
		lk.start = markCircle
		if p.s[p.i] == 'x' {
			lk.start = markCross
		}
		p.i++
	}
	s := p.s[p.i:]
	switch {
	case strings.HasPrefix(s, "-."):
		lk.stroke = strokeDotted
		k := 1
		for k < len(s) && s[k] == '.' {
			k++
		}
		if k < len(s) && s[k] == '-' {
			lk.minlen = k - 1
			k++
			k += endMark(s[k:], &lk)
			p.i += k
		} else {
			p.i += 2
			if err := p.edgeText(&lk); err != nil {
				return lk, false, err
			}
		}
	case strings.HasPrefix(s, "--") || strings.HasPrefix(s, "=="):
		c := s[0]
		if c == '=' {
			lk.stroke = strokeThick
		}
		n := 0
		for n < len(s) && s[n] == c {
			n++
		}
		if m := endMark(s[n:], &lk); m > 0 {
			lk.minlen = n - 1
			p.i += n + m
		} else if n >= 3 {
			lk.minlen = n - 2
			p.i += n
		} else {
			p.i += 2
			if err := p.edgeText(&lk); err != nil {
				return lk, false, err
			}
		}
	case strings.HasPrefix(s, "~~~"):
		lk.stroke = strokeInvisible
		n := 0
		for n < len(s) && s[n] == '~' {
			n++
		}
		lk.minlen = n - 2
		p.i += n
	default:
		p.i = save
		return lk, false, nil
	}
	p.skipSpaces()
	if p.at('|') {
		k := strings.IndexAny(p.s[p.i+1:], "|\n")
		if k < 0 || p.s[p.i+1+k] != '|' {
			return lk, false, p.errorf("unclosed link label")
		}
		lk.label, lk.markdown = labelText(strings.TrimSpace(p.s[p.i+1 : p.i+1+k]))
		p.i += k + 2
	}
	lk.minlen = min(max(lk.minlen, 1), maxMinlen)
	return lk, true, nil
}

// labelText unquotes a label, noting whether it was a Markdown string.
func labelText(s string) (string, bool) {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
		if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
			return s[1 : len(s)-1], true
		}
	}
	return s, false
}

// endMark reads the marker that ends an arrow, and returns its length.
func endMark(s string, lk *fedge) int {
	if s == "" {
		return 0
	}
	switch s[0] {
	case '>':
		lk.end = markArrow
		return 1
	case 'x', 'o':
		if len(s) == 1 || strings.IndexByte(" \t\n;|", s[1]) >= 0 {
			lk.end = markCircle
			if s[0] == 'x' {
				lk.end = markCross
			}
			return 1
		}
	}
	return 0
}

// edgeText reads the text of `-- text -->` and the arrow that closes it.
func (p *fparser) edgeText(lk *fedge) error {
	p.skipSpaces()
	if p.at('"') {
		text, k, markdown, ok := quoted(p.s, p.i)
		if !ok {
			return p.errorf("unclosed string")
		}
		p.line += strings.Count(p.s[p.i:k], "\n")
		p.i = k
		p.skipSpaces()
		n, ok := p.closeAt(p.i, lk)
		if !ok {
			return p.errorf("unclosed link text")
		}
		lk.label, lk.markdown = text, markdown
		p.i += n
		return nil
	}
	for k := p.i; k < len(p.s) && p.s[k] != '\n'; k++ {
		if n, ok := p.closeAt(k, lk); ok {
			lk.label = strings.TrimSpace(p.s[p.i:k])
			p.i = k + n
			return nil
		}
	}
	return p.errorf("unclosed link text")
}

// closeAt matches the arrow that ends a link's text at s[k], for the kind of line it began.
func (p *fparser) closeAt(k int, lk *fedge) (int, bool) {
	s := p.s[k:]
	if lk.stroke == strokeDotted {
		n := 0
		for n < len(s) && s[n] == '.' {
			n++
		}
		if n == 0 || n >= len(s) || s[n] != '-' {
			return 0, false
		}
		lk.minlen = n
		return n + 1 + endMark(s[n+1:], lk), true
	}
	c := byte('-')
	if lk.stroke == strokeThick {
		c = '='
	}
	n := 0
	for n < len(s) && s[n] == c {
		n++
	}
	if n < 2 {
		return 0, false
	}
	if m := endMark(s[n:], lk); m > 0 {
		lk.minlen = n - 1
		return n + m, true
	}
	if n >= 3 {
		lk.minlen = n - 2
		return n, true
	}
	return 0, false
}

// resolve puts each item in the subgraph that closed first among those that
// mention it, which is the innermost, and keeps the nesting free of cycles.
func (p *fparser) resolve() {
	items := p.fc.items
	for i, ms := range p.mentions {
		best := -1
		for _, g := range ms {
			if best < 0 || items[g].closed < items[best].closed {
				best = g
			}
		}
		items[i].parent = best
	}
	for i := range items {
		v := items[i].parent
		for steps := 0; v >= 0 && steps <= len(items); steps++ {
			if v == i {
				items[i].parent = -1
				break
			}
			v = items[v].parent
		}
	}
	members := make([]int, len(items))
	for _, it := range items {
		if it.parent >= 0 {
			members[it.parent]++
		}
	}
	for i, it := range items {
		if it.group && members[i] == 0 {
			it.group = false
		}
	}
}
