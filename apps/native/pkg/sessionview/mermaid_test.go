package sessionview

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/mermaid"
)

func replyScenario(reply string) fakedaemon.Scenario {
	return fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Diagram", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{
		{Role: "user", Text: "Draw it"},
		{Role: "assistant", Text: reply},
	}}}}
}

// A closed mermaid block shows as its diagram, and its source a click away.
func TestAMermaidBlockDrawsItsDiagram(t *testing.T) {
	h := newHarness(t, replyScenario("Here:\n\n```mermaid\nflowchart LR\n  A[Start] --> B[Done]\n```\n"))
	h.until("the diagram", func() bool { _, ok := h.tt.Find("Diagram: flowchart"); return ok })
	if h.hasText("A[Start] --> B[Done]") {
		t.Errorf("the source shows beside the diagram: %q", h.tt.Texts())
	}
	if err := h.tt.Click("Show source"); err != nil {
		t.Fatal(err)
	}
	h.until("the source", func() bool { return h.hasText("A[Start] --> B[Done]") })
	if err := h.tt.Click("Show diagram"); err != nil {
		t.Fatal(err)
	}
	h.until("the diagram again", func() bool { _, ok := h.tt.Find("Diagram: flowchart"); return ok })
}

// A diagram it cannot draw stays a code block.
func TestAMermaidBlockItCannotDrawShowsItsSource(t *testing.T) {
	for _, src := range []string{"pie title Pets\n  \"Dogs\" : 3", "flowchart LR\n  A --> [", "sequenceDiagram\n  Alice->>"} {
		h := newHarness(t, replyScenario("```mermaid\n"+src+"\n```\n"))
		h.until("the reply", func() bool { return h.hasText("Draw it") })
		h.frame()
		if _, ok := h.tt.Find("Mermaid diagram"); ok {
			t.Errorf("%q: drawn as a diagram", src)
		}
		if _, ok := h.tt.Find("Show source"); ok {
			t.Errorf("%q: offers a source toggle", src)
		}
	}
}

// A big shape draws without a big mask in the text atlas, which would leave
// the frame's later shapes and glyphs undrawn: a box as a rectangle, a long
// edge as lines and short paths.
func TestABigShapeDrawsWithoutABigMask(t *testing.T) {
	box := drawOf(mermaid.Shape{Path: []mermaid.Op{
		{Kind: mermaid.MoveTo}, {Kind: mermaid.LineTo, X: 900}, {Kind: mermaid.LineTo, X: 900, Y: 2000}, {Kind: mermaid.LineTo, Y: 2000}, {Kind: mermaid.Close},
	}, Fill: mermaid.PaintSurfaceAlt, Stroke: mermaid.PaintBorder, StrokeWidth: 1})
	if box.box == nil || *box.box != (ui.Rect{W: 900, H: 2000}) {
		t.Fatalf("the box draws as %+v", box)
	}
	edge := drawOf(mermaid.Shape{Path: []mermaid.Op{
		{Kind: mermaid.MoveTo, X: 10}, {Kind: mermaid.LineTo, X: 10, Y: 1500}, {Kind: mermaid.QuadTo, X1: 10, Y1: 1510, X: 20, Y: 1510},
		{Kind: mermaid.LineTo, X: 800, Y: 1510}, {Kind: mermaid.LineTo, X: 1400, Y: 2400},
	}, Stroke: mermaid.PaintLine, StrokeWidth: 1.5})
	if edge.box != nil || len(edge.lines) != 2 {
		t.Fatalf("the edge draws as %+v", edge)
	}
	for _, p := range edge.stroke {
		if b := p.Bounds(); b.W > maxPiece || b.H > maxPiece {
			t.Errorf("a piece of the edge is %+v", b)
		}
	}
}
