// Package md turns Markdown, including Markdown still being streamed, into a
// flat list of blocks a native view can lay out.
package md

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Inline is a run of text with one style.
type Inline struct {
	Text   string
	Bold   bool
	Italic bool
	Code   bool
	Strike bool
	URL    string
}

type Kind uint8

const (
	Paragraph Kind = iota
	Heading
	CodeBlock
	Table
	Rule
)

// Block is one block of a document. Lists and quotes are flattened into
// their paragraphs, which carry their nesting.
type Block struct {
	Kind    Kind
	Level   int // heading level
	Inlines []Inline

	// Indent is the depth of the list a paragraph belongs to, 0 outside
	// lists; Marker is the bullet or number of a list item's first
	// paragraph; Quote is the depth of block quotes around it.
	Indent int
	Marker string
	Quote  int

	Lang string
	Code string

	Header [][]Inline
	Rows   [][][]Inline
}

var parser = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()

// Parse parses a complete Markdown document.
func Parse(src string) []Block {
	source := []byte(src)
	doc := parser.Parse(text.NewReader(source))
	w := walker{src: source}
	w.blocks(doc, 0, 0)
	return w.out
}

type walker struct {
	src    []byte
	out    []Block
	marker string
}

func (w *walker) takeMarker() string {
	m := w.marker
	w.marker = ""
	return m
}

func (w *walker) blocks(n ast.Node, indent, quote int) {
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		switch v := ch.(type) {
		case *ast.Paragraph, *ast.TextBlock:
			w.out = append(w.out, Block{Kind: Paragraph, Inlines: w.inlines(v, Inline{}, nil), Indent: indent, Marker: w.takeMarker(), Quote: quote})
		case *ast.Heading:
			w.out = append(w.out, Block{Kind: Heading, Level: v.Level, Inlines: w.inlines(v, Inline{}, nil), Indent: indent, Quote: quote})
		case *ast.FencedCodeBlock:
			w.flushMarker(indent, quote)
			w.out = append(w.out, Block{Kind: CodeBlock, Lang: string(v.Language(w.src)), Code: w.lines(v), Indent: indent, Quote: quote})
		case *ast.CodeBlock:
			w.flushMarker(indent, quote)
			w.out = append(w.out, Block{Kind: CodeBlock, Code: w.lines(v), Indent: indent, Quote: quote})
		case *ast.List:
			num := v.Start
			for item := v.FirstChild(); item != nil; item = item.NextSibling() {
				if v.IsOrdered() {
					w.marker = strconv.Itoa(num) + "."
					num++
				} else {
					w.marker = bullet(indent)
				}
				w.blocks(item, indent+1, quote)
				w.flushMarker(indent+1, quote)
			}
		case *ast.Blockquote:
			w.blocks(v, indent, quote+1)
		case *ast.ThematicBreak:
			w.out = append(w.out, Block{Kind: Rule})
		case *ast.HTMLBlock:
			w.out = append(w.out, Block{Kind: Paragraph, Inlines: []Inline{{Text: strings.TrimRight(w.lines(v), "\n")}}, Indent: indent, Marker: w.takeMarker(), Quote: quote})
		case *extast.Table:
			w.out = append(w.out, w.table(v))
		default:
			w.blocks(ch, indent, quote)
		}
	}
}

// flushMarker keeps the bullet of an item whose first block is not a
// paragraph, such as an empty item or one that starts with code.
func (w *walker) flushMarker(indent, quote int) {
	if w.marker != "" {
		w.out = append(w.out, Block{Kind: Paragraph, Indent: indent, Marker: w.takeMarker(), Quote: quote})
	}
}

func bullet(depth int) string {
	switch depth % 3 {
	case 0:
		return "•"
	case 1:
		return "◦"
	}
	return "▪"
}

func (w *walker) lines(n ast.Node) string {
	var b bytes.Buffer
	l := n.Lines()
	for i := 0; i < l.Len(); i++ {
		seg := l.At(i)
		b.Write(seg.Value(w.src))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (w *walker) table(t *extast.Table) Block {
	b := Block{Kind: Table}
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		var cells [][]Inline
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			cells = append(cells, w.inlines(cell, Inline{}, nil))
		}
		if _, ok := row.(*extast.TableHeader); ok {
			b.Header = cells
		} else {
			b.Rows = append(b.Rows, cells)
		}
	}
	return b
}

// inlines flattens the inline children of n into styled runs, merging
// neighbours of one style.
func (w *walker) inlines(n ast.Node, style Inline, out []Inline) []Inline {
	add := func(s string, st Inline) {
		if s == "" {
			return
		}
		st.Text = ""
		if k := len(out) - 1; k >= 0 {
			last := out[k]
			last.Text = ""
			if last == st {
				out[k].Text += s
				return
			}
		}
		st.Text = s
		out = append(out, st)
	}
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		switch v := ch.(type) {
		case *ast.Text:
			add(string(v.Segment.Value(w.src)), style)
			if v.HardLineBreak() {
				add("\n", style)
			} else if v.SoftLineBreak() {
				add(" ", style)
			}
		case *ast.String:
			add(string(v.Value), style)
		case *ast.CodeSpan:
			st := style
			st.Code = true
			var b strings.Builder
			for c := v.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					b.Write(t.Segment.Value(w.src))
				} else if s, ok := c.(*ast.String); ok {
					b.Write(s.Value)
				}
			}
			add(b.String(), st)
		case *ast.Emphasis:
			st := style
			if v.Level >= 2 {
				st.Bold = true
			} else {
				st.Italic = true
			}
			out = w.inlines(v, st, out)
		case *extast.Strikethrough:
			st := style
			st.Strike = true
			out = w.inlines(v, st, out)
		case *ast.Link:
			st := style
			st.URL = string(v.Destination)
			out = w.inlines(v, st, out)
		case *ast.AutoLink:
			st := style
			st.URL = string(v.URL(w.src))
			add(string(v.Label(w.src)), st)
		case *ast.Image:
			st := style
			st.URL = string(v.Destination)
			add("[image]", st)
		case *ast.RawHTML:
			for i := 0; i < v.Segments.Len(); i++ {
				seg := v.Segments.At(i)
				add(string(seg.Value(w.src)), style)
			}
		case *extast.TaskCheckBox:
			if v.IsChecked {
				add("☑ ", style)
			} else {
				add("☐ ", style)
			}
		default:
			out = w.inlines(ch, style, out)
		}
	}
	return out
}

// PlainText returns the text of runs without their styles.
func PlainText(in []Inline) string {
	var b strings.Builder
	for _, r := range in {
		b.WriteString(r.Text)
	}
	return b.String()
}
