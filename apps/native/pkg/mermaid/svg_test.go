package mermaid

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var svgPalette = map[Paint]string{
	PaintNone:       "none",
	PaintBackground: "#fafaf9",
	PaintSurface:    "#ffffff",
	PaintSurfaceAlt: "#efeeea",
	PaintBorder:     "#3f3f46",
	PaintLine:       "#52525b",
	PaintForeground: "#18181b",
	PaintMuted:      "#71717a",
	PaintAccent:     "#2563eb",
}

// sceneSVG draws a Scene as SVG with a fixed light palette, for a person to look at.
func sceneSVG(sc *Scene) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%g" height="%g" viewBox="0 0 %g %g">`, sc.Width, sc.Height, sc.Width, sc.Height)
	fmt.Fprintf(&b, `<rect width="100%%" height="100%%" fill="%s"/>`, svgPalette[PaintBackground])
	for _, sh := range sc.Shapes {
		var d strings.Builder
		for _, op := range sh.Path {
			switch op.Kind {
			case MoveTo:
				fmt.Fprintf(&d, "M%g %g", op.X, op.Y)
			case LineTo:
				fmt.Fprintf(&d, "L%g %g", op.X, op.Y)
			case QuadTo:
				fmt.Fprintf(&d, "Q%g %g %g %g", op.X1, op.Y1, op.X, op.Y)
			case CubicTo:
				fmt.Fprintf(&d, "C%g %g %g %g %g %g", op.X1, op.Y1, op.X2, op.Y2, op.X, op.Y)
			case Close:
				d.WriteString("Z")
			}
		}
		dash := ""
		if len(sh.Dash) > 0 {
			parts := make([]string, len(sh.Dash))
			for i, v := range sh.Dash {
				parts[i] = fmt.Sprint(v)
			}
			dash = fmt.Sprintf(` stroke-dasharray="%s"`, strings.Join(parts, " "))
		}
		fmt.Fprintf(&b, `<path d="%s" fill="%s" stroke="%s" stroke-width="%g"%s stroke-linejoin="round"/>`,
			d.String(), svgPalette[sh.Fill], svgPalette[sh.Stroke], sh.StrokeWidth, dash)
	}
	for _, t := range sc.Texts {
		x, anchor := t.X+t.W/2, "middle"
		switch t.Align {
		case AlignStart:
			x, anchor = t.X, "start"
		case AlignEnd:
			x, anchor = t.X+t.W, "end"
		}
		weight := "normal"
		if t.Bold {
			weight = "bold"
		}
		text := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(t.Text)
		fmt.Fprintf(&b, `<text x="%g" y="%g" font-family="Menlo, monospace" font-size="%g" font-weight="%s" text-anchor="%s" dominant-baseline="central" fill="%s">%s</text>`,
			x, t.Y+t.H/2, t.Size, weight, anchor, svgPalette[t.Color], text)
	}
	b.WriteString("</svg>")
	return b.String()
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// writeSVG writes the Scene to $DROI_MERMAID_SVG/<name>.svg when that is set.
func writeSVG(t *testing.T, name string, sc *Scene) {
	t.Helper()
	dir := os.Getenv("DROI_MERMAID_SVG")
	if dir == "" || sc == nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, unsafeName.ReplaceAllString(name, "_")+".svg")
	if err := os.WriteFile(path, []byte(sceneSVG(sc)), 0o644); err != nil {
		t.Fatal(err)
	}
}
