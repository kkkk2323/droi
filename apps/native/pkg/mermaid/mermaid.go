// Package mermaid parses Mermaid flowcharts and sequence diagrams and lays them
// out as a Scene: outlines and one-line texts in pixels, painted by role rather
// than by color, for the app to draw with its theme. Text is measured as
// monospace (every glyph advances 0.6 em, a wide one twice that), so the
// painter's font is expected to be monospaced as well.
//
// The parsers and the layered layout follow Ondine (Rust) and Pele
// (TypeScript), both MIT licensed; see NOTICE.
package mermaid

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

var ErrUnsupported = errors.New("mermaid: unsupported diagram type")

// Error is a syntax error: Line is 1-based in the source.
type Error struct {
	Line int
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("mermaid: line %d: %s", e.Line, e.Msg) }

type Options struct {
	// FontSize is the label size in px; a glyph advances 0.6*FontSize (2 cells for wide runes).
	FontSize float32
}

type Scene struct {
	Kind          string  // "flowchart" | "sequence"
	Width, Height float32 // the drawing's size; every coordinate is within [0,Width]x[0,Height]
	Shapes        []Shape // painted in order, before Texts
	Texts         []Text  // painted after all shapes
	Title         string  // front-matter title, if any (also laid out as a Text)
}

type Paint uint8

const (
	PaintNone       Paint = iota
	PaintBackground       // the page behind the diagram (e.g. label backdrops that hide lines)
	PaintSurface          // node / participant / note fills
	PaintSurfaceAlt       // subgraph / loop-alt block fills (subtle)
	PaintBorder           // node outlines
	PaintLine             // edges, lifelines, message lines
	PaintForeground       // text, arrowheads
	PaintMuted            // edge labels, secondary text, block keywords
	PaintAccent           // highlight (rarely used; e.g. activation bars)
)

type OpKind uint8

const (
	MoveTo OpKind = iota
	LineTo
	QuadTo
	CubicTo
	Close
)

// Op: MoveTo/LineTo use X,Y. QuadTo: control X1,Y1 then end X,Y. CubicTo: X1,Y1,X2,Y2 then end X,Y.
type Op struct {
	Kind                 OpKind
	X, Y, X1, Y1, X2, Y2 float32
}

type Shape struct {
	Path        []Op
	Fill        Paint
	Stroke      Paint
	StrokeWidth float32   // px
	Dash        []float32 // on/off lengths; nil = solid
}

type Align uint8

const (
	AlignCenter Align = iota
	AlignStart
	AlignEnd
)

// Text is ONE line of label text placed in its line box (X,Y top-left, W,H);
// the painter measures the real text and aligns it inside the box.
type Text struct {
	X, Y, W, H float32
	Text       string
	Size       float32 // px
	Bold       bool
	Align      Align
	Color      Paint
}

// Inputs past these limits are refused with an *Error rather than laid out,
// which keeps the time and memory of one Render bounded.
const (
	maxSource   = 64 << 10
	maxNodes    = 500
	maxEdges    = 1000
	maxSeqRows  = 2000
	maxMinlen   = 8
	defaultSize = 14
)

// Render parses src (the fence body, without ```), lays it out and returns the Scene.
// Unknown diagram types return ErrUnsupported; bad syntax returns *Error.
func Render(src string, o Options) (scene *Scene, err error) {
	defer func() {
		// A layout bug must not take the window down; the caller shows the source instead.
		if r := recover(); r != nil {
			scene, err = nil, &Error{Line: 1, Msg: fmt.Sprintf("internal error: %v", r)}
		}
	}()
	return render(src, o)
}

func render(src string, o Options) (*Scene, error) {
	if len(src) > maxSource {
		return nil, &Error{Line: 1, Msg: "diagram is larger than 64 KiB"}
	}
	size := float64(o.FontSize)
	if !(size >= 4 && size <= 96) {
		size = defaultSize
	}
	src = strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(src, "\n")
	title, start := frontMatter(lines)
	kind, header := detect(lines, start)
	switch kind {
	case "flowchart":
		fc, err := parseFlowchart(lines, header)
		if err != nil {
			return nil, err
		}
		if fc.title == "" {
			fc.title = title
		}
		return layoutFlowchart(fc, size), nil
	case "sequence":
		sd, err := parseSequence(lines, header)
		if err != nil {
			return nil, err
		}
		if sd.title == "" {
			sd.title = title
		}
		return layoutSequence(sd, size), nil
	}
	return nil, ErrUnsupported
}

// frontMatter reads a leading `---` block for its title, and returns the
// index of the first line after it.
func frontMatter(lines []string) (string, int) {
	first := 0
	for first < len(lines) && strings.TrimSpace(lines[first]) == "" {
		first++
	}
	if first >= len(lines) || strings.TrimSpace(lines[first]) != "---" {
		return "", 0
	}
	title := ""
	for i := first + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			return title, i + 1
		}
		if rest, ok := strings.CutPrefix(line, "title:"); ok {
			title = unquote(strings.TrimSpace(rest))
		}
	}
	return "", 0
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// detect names the diagram type from its first statement and returns the
// index of the line that holds it.
func detect(lines []string, start int) (string, int) {
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		word := line
		if j := strings.IndexAny(line, " \t;"); j >= 0 {
			word = line[:j]
		}
		switch word {
		case "flowchart", "graph", "flowchart-elk":
			return "flowchart", i
		case "sequenceDiagram":
			return "sequence", i
		}
		return "", i
	}
	return "", -1
}

func f32(v float64) float32 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return float32(v)
}
