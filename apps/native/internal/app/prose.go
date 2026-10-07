package app

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/highlight"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
	"github.com/kkkk2323/droi/apps/native/internal/md"
	"github.com/kkkk2323/droi/apps/native/internal/theme"
)

// flow lays blocks out one under another with CSS's collapsing margins:
// the room between two blocks is the larger of the margin below the one
// and above the other. Its container's padding keeps the first and last
// margins inside it, as the transcript's articles do.
type flow struct {
	c       *ui.Context
	pending float32
	started bool
	// open: no padding above the first block, so its margin collapses
	// into the container's own (the container takes it, see firstMargin).
	open bool
}

func (f *flow) block(top, bottom float32, build func()) {
	gap := top
	if f.started {
		gap = max(f.pending, top)
	} else if f.open {
		gap = 0
	}
	if gap > 0 {
		ui.Box(f.c).Height(gap).Shrink(0)
	}
	build()
	f.started = true
	f.pending = bottom
}

// end adds the last block's bottom margin.
func (f *flow) end() {
	if f.started && f.pending > 0 {
		ui.Box(f.c).Height(f.pending).Shrink(0)
	}
}

// prose draws Markdown as the web Client's Streamdown does under
// .prose-droi: 15px text on 28px lines, its margins in rem.
type prose struct {
	k *kit.Kit
	// size and lh are the body text's, 15 and 28 in a reply, 14 and 24 in
	// a thinking block.
	size, lh float32
	color    ui.Color
	// shiki is the Shiki theme code blocks take.
	shiki string
}

func newProse(k *kit.Kit) prose {
	return prose{k: k, size: 15, lh: 28, color: k.T.Foreground, shiki: highlight.Theme(string(k.T.Name))}
}

// margins are a block's margin-block, in px of the default text size.
func margins(n *md.Node) (top, bottom float32) {
	switch n.Kind {
	case md.NodeHeading:
		return 16, 8
	case md.NodeCode:
		return 12, 12
	case md.NodeRule:
		return 16, 16
	case md.NodeTable:
		return 16, 16
	}
	return 8, 8
}

// firstMargin is the top margin of a document's first block, which
// collapses with that of a container without padding.
func (p prose) firstMargin(nodes []*md.Node) float32 {
	if len(nodes) == 0 {
		return 0
	}
	top, _ := margins(nodes[0])
	return p.k.Px(top)
}

// nodes adds a document's blocks to a flow.
func (p prose) nodes(c *ui.Context, f *flow, nodes []*md.Node) {
	for _, n := range nodes {
		top, bottom := margins(n)
		f.block(p.k.Px(top), p.k.Px(bottom), func() { p.node(c, n) })
	}
}

func (p prose) node(c *ui.Context, n *md.Node) {
	k, t := p.k, p.k.T
	switch n.Kind {
	case md.NodeParagraph:
		p.inlines(c, n.Inlines, p.size, p.lh, 0)
	case md.NodeHeading:
		size, lh := headingSize(n.Level)
		p.inlines(c, n.Inlines, size, lh, 600).LetterSpacing(k.Px(-0.01 * size))
	case md.NodeCode:
		p.codeBlock(c, n)
	case md.NodeList:
		p.list(c, n)
	case md.NodeQuote:
		ui.Column(c).BorderWidth(0, 0, 0, 2).BorderColor(t.Border).Padding(0, 0, 0, k.Px(12)).Children(func() {
			q := p
			q.color = t.MutedForeground
			f := flow{c: c}
			q.nodes(c, &f, n.Children)
		})
	case md.NodeRule:
		ui.Box(c).Height(1).Background(t.Border)
	case md.NodeTable:
		p.table(c, n)
	}
}

// headingSize is .prose-droi's h1-h4 sizes over the line heights of
// Streamdown's text-3xl, text-2xl, text-xl and text-lg.
func headingSize(level int) (size, lh float32) {
	switch level {
	case 1:
		return 20, 24
	case 2:
		return 17.6, 23.47
	case 3:
		return 16, 22.4
	case 4:
		return 16, 24.89
	case 5:
		return 16, 24
	}
	return 14, 20
}

// inlines is a paragraph of runs. Inline code is Geist Mono at 0.85em in
// the code color, without the web Client's tint.
func (p prose) inlines(c *ui.Context, runs []md.Inline, size, lh float32, weight int) *ui.Element {
	k, t := p.k, p.k.T
	codeSize := size * 0.85
	para := ui.RichText(c).FontSize(k.Px(size)).FixedLineHeight(k.Px(lh)).TextColor(p.color)
	if weight > 0 {
		para.FontWeight(weight)
	}
	para.Children(func() {
		for _, r := range runs {
			if r.Code {
				ui.RichText(c, ui.Span{Text: r.Text}).Font(k.Mono).FontSize(k.Px(codeSize)).TextColor(t.Code)
				continue
			}
			span := ui.Span{Text: r.Text, Italic: r.Italic, Strikethrough: r.Strike}
			if r.Bold {
				span.Weight = 600
			}
			if r.URL != "" {
				l := ui.Link(c, r.Text, r.URL).FontWeight(500).Underline().TextColor(t.Primary)
				if r.Italic {
					l.Italic()
				}
				continue
			}
			ui.RichText(c, span)
		}
	})
	// After Children: Selectable takes the text the runs inside make.
	return para.Selectable()
}

// list is an ul or ol: 1.5rem of indent, the markers outside it in the
// muted color, items 0.125rem apart with Streamdown's py-1.
func (p prose) list(c *ui.Context, n *md.Node) {
	k, t := p.k, p.k.T
	ui.Column(c).Padding(0, 0, 0, k.Px(24)).Children(func() {
		for i, item := range n.Children {
			if i > 0 {
				ui.Box(c).Height(k.Px(2)).Shrink(0)
			}
			ui.Column(c).PaddingY(k.Px(4)).Children(func() {
				line := k.Px(p.lh)
				if n.Ordered {
					label := strconv.Itoa(n.Start+i) + "."
					ui.Text(c, label).FontSize(k.Px(p.size)).FixedLineHeight(line).TextColor(t.MutedForeground).TextAlign(ui.End).
						Absolute().Left(-k.Px(40)).Top(k.Px(4)).Width(k.Px(36)).FontFeatures("tnum")
				} else if item.Checked == 0 {
					d := k.Px(p.size / 3)
					kit.Dot(c, d, t.MutedForeground).Absolute().Left(-k.Px(14.5) - d/2).Top(k.Px(4) + line/2 - d/2)
				}
				f := flow{c: c}
				if item.Checked > 0 {
					checked := item.Checked == 2
					ui.Row(c).Gap(k.Px(6)).AlignItems(ui.Start).Children(func() {
						box := ui.Box(c).Size(k.Px(13), k.Px(13)).Margin((line-k.Px(13))/2, 0, 0, 0).Radius(k.Px(3)).Border(1, t.Input).Shrink(0)
						if checked {
							box.Background(t.Primary).Center().Children(func() { k.Icon(c, "check", 10, t.PrimaryForeground) })
						}
						ui.Column(c).Grow(1).Children(func() { p.itemBlocks(c, &f, item, n.Tight) })
					})
					return
				}
				p.itemBlocks(c, &f, item, n.Tight)
			})
		}
	})
}

// itemBlocks are an item's blocks: in a tight list its text runs in the
// item itself, without the paragraph's margins.
func (p prose) itemBlocks(c *ui.Context, f *flow, item *md.Node, tight bool) {
	for i, ch := range item.Children {
		if tight && ch.Kind == md.NodeParagraph {
			if i > 0 {
				f.block(0, 0, func() { p.inlines(c, ch.Inlines, p.size, p.lh, 0) })
			} else {
				p.inlines(c, ch.Inlines, p.size, p.lh, 0)
				f.started = true
			}
			continue
		}
		top, bottom := margins(ch)
		f.block(p.k.Px(top), p.k.Px(bottom), func() { p.node(c, ch) })
	}
	if !tight {
		f.end()
	}
}

// table is Streamdown's table: a bordered frame, the header on muted.
func (p prose) table(c *ui.Context, n *md.Node) {
	k, t := p.k, p.k.T
	cols := len(n.Header)
	for _, r := range n.Rows {
		cols = max(cols, len(r))
	}
	if cols == 0 {
		return
	}
	cell := func(in []md.Inline, head, lastRow bool) {
		box := ui.Box(c).Padding(k.Px(8), k.Px(16))
		if !lastRow {
			box.BorderWidth(0, 0, 1, 0).BorderColor(t.Border)
		}
		if head {
			box.Background(t.Muted.Alpha(0.8))
		}
		box.Children(func() {
			if len(in) == 0 {
				in = []md.Inline{{Text: " "}}
			}
			w := 0
			if head {
				w = 600
			}
			p.inlines(c, in, 14, 20, w)
		})
	}
	// Columns as wide as their content within the reading column, as a
	// table with auto layout: long cells wrap instead of running off the
	// right edge.
	tracks := make([]ui.Track, cols)
	for i := range tracks {
		tracks[i] = ui.FitContent()
	}
	ui.Column(c).FillWidth().Radius(k.Px(8)).Border(1, t.Border).Clip().Children(func() {
		ui.Grid(c).FillWidth().ColumnTracks(tracks...).Children(func() {
			for i := range cols {
				var in []md.Inline
				if i < len(n.Header) {
					in = n.Header[i]
				}
				cell(in, true, len(n.Rows) == 0)
			}
			for ri, r := range n.Rows {
				for i := range cols {
					var in []md.Inline
					if i < len(r) {
						in = r[i]
					}
					cell(in, false, ri == len(n.Rows)-1)
				}
			}
		})
	})
}

// shikiForeground is the text color of a Shiki theme's unstyled tokens.
var shikiForeground = map[string]string{
	"github-light":         "#24292e",
	"github-dark":          "#e1e4e8",
	"solarized-light-plus": "#333333",
}

// codeBlock is Streamdown's code block under .prose-droi: a 2.25rem header
// with the language and the Download and Copy buttons over the body,
// numbered lines of 0.8rem Geist Mono on the page's background.
func (p prose) codeBlock(c *ui.Context, n *md.Node) {
	k, t := p.k, p.k.T
	lang := n.Lang
	ui.Column(c).Radius(k.Px(12)).Border(1, t.Border).Background(t.Card).Clip().Children(func() {
		ui.Row(c).Height(k.Px(36)).Padding(0, k.Px(8), 0, k.Px(16)).Children(func() {
			k.Text(c, lang, 12, 16).Font(k.Mono).TextColor(t.MutedForeground)
			ui.Spacer(c)
			ui.Row(c).Gap(k.Px(8)).Padding(k.Px(4), k.Px(7)).Children(func() {
				dl := p.codeAction(c, "download", "Download file")
				if dl.Clicked() {
					c.WriteClipboard(n.Code)
				}
				if p.codeAction(c, "copy", "Copy Code").Clicked() {
					c.WriteClipboard(n.Code)
					c.Toast("Copied the code")
				}
			})
		})
		ui.ScrollHorizontal(c).Background(t.Background).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			ui.Row(c).AlignItems(ui.Start).Padding(k.Px(12), k.Px(16)).Children(func() {
				lines := strings.Count(n.Code, "\n") + 1
				line := k.Px(20.8)
				ui.Column(c).Width(k.Px(24)).Shrink(0).Children(func() {
					for i := range lines {
						ui.Text(c, strconv.Itoa(i+1)).Font(k.Mono).FontSize(k.Px(13)).FixedLineHeight(line).
							TextColor(t.MutedForeground.Alpha(0.5)).TextAlign(ui.End).FillWidth()
					}
				})
				ui.Box(c).Width(k.Px(16)).Shrink(0)
				spans := highlight.Spans(lang, n.Code, p.shiki)
				if spans == nil {
					spans = []ui.Span{{Text: n.Code}}
				}
				ui.RichText(c, spans...).Font(k.Mono).FontSize(k.Px(12.8)).FixedLineHeight(line).
					TextColor(ui.Hex(shikiForeground[p.shiki])).NoWrap().Selectable()
			})
		})
	})
}

func (p prose) codeAction(c *ui.Context, icon, label string) *ui.Element {
	k, t := p.k, p.k.T
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Size(k.Px(24), k.Px(24)).Radius(k.Px(6)).Cursor(ui.CursorPointer)
	color := t.MutedForeground
	if b.Hovered() {
		color = t.Foreground
	}
	b.Children(func() { k.Icon(c, icon, 16, color) })
	return b
}

// themeOf is the theme name of a kit, for highlight.Theme.
func themeOf(k *kit.Kit) theme.Name { return k.T.Name }
