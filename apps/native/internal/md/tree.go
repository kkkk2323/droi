package md

import (
	"strings"

	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// NodeKind is what a Node of a document tree is.
type NodeKind uint8

const (
	NodeParagraph NodeKind = iota
	NodeHeading
	NodeCode
	NodeList
	NodeItem
	NodeQuote
	NodeRule
	NodeTable
)

// Node is a block of a document as HTML nests it: lists hold items, items
// and quotes hold blocks. A view lays it out as the browser lays out the
// HTML react-markdown makes of the same source.
type Node struct {
	Kind    NodeKind
	Level   int // a heading's rank
	Inlines []Inline

	Ordered bool
	Start   int
	// Tight: a list whose items hold their text directly, not in paragraphs.
	Tight bool
	// Checked is a task item's box: 0 none, 1 open, 2 checked.
	Checked int

	Lang string
	Code string

	Header [][]Inline
	Rows   [][][]Inline

	Children []*Node
}

// ParseTree parses a complete Markdown document into blocks as HTML nests
// them.
func ParseTree(src string) []*Node {
	source := []byte(src)
	doc := parser.Parse(text.NewReader(source))
	w := walker{src: source}
	return w.tree(doc, false)
}

func (w *walker) tree(n ast.Node, tight bool) []*Node {
	var out []*Node
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		switch v := ch.(type) {
		case *ast.Paragraph:
			out = append(out, &Node{Kind: NodeParagraph, Inlines: w.inlines(v, Inline{}, nil)})
		case *ast.TextBlock:
			out = append(out, &Node{Kind: NodeParagraph, Inlines: w.inlines(v, Inline{}, nil)})
		case *ast.Heading:
			out = append(out, &Node{Kind: NodeHeading, Level: v.Level, Inlines: w.inlines(v, Inline{}, nil)})
		case *ast.FencedCodeBlock:
			out = append(out, &Node{Kind: NodeCode, Lang: string(v.Language(w.src)), Code: w.lines(v)})
		case *ast.CodeBlock:
			out = append(out, &Node{Kind: NodeCode, Code: w.lines(v)})
		case *ast.List:
			l := &Node{Kind: NodeList, Ordered: v.IsOrdered(), Start: v.Start, Tight: v.IsTight}
			for item := v.FirstChild(); item != nil; item = item.NextSibling() {
				it := &Node{Kind: NodeItem, Children: w.tree(item, v.IsTight)}
				if first := item.FirstChild(); first != nil {
					if box, ok := first.FirstChild().(*extast.TaskCheckBox); ok {
						it.Checked = 1
						if box.IsChecked {
							it.Checked = 2
						}
						if len(it.Children) > 0 && len(it.Children[0].Inlines) > 0 {
							ins := it.Children[0].Inlines
							ins[0].Text = strings.TrimPrefix(strings.TrimPrefix(ins[0].Text, "☑ "), "☐ ")
						}
					}
				}
				l.Children = append(l.Children, it)
			}
			out = append(out, l)
		case *ast.Blockquote:
			out = append(out, &Node{Kind: NodeQuote, Children: w.tree(v, false)})
		case *ast.ThematicBreak:
			out = append(out, &Node{Kind: NodeRule})
		case *ast.HTMLBlock:
			out = append(out, &Node{Kind: NodeParagraph, Inlines: []Inline{{Text: strings.TrimRight(w.lines(v), "\n")}}})
		case *extast.Table:
			t := w.table(v)
			out = append(out, &Node{Kind: NodeTable, Header: t.Header, Rows: t.Rows})
		default:
			out = append(out, w.tree(ch, tight)...)
		}
	}
	return out
}
