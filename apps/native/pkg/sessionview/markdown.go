package sessionview

import (
	"encoding/base64"
	"errors"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/attachments"
	"github.com/kkkk2323/droi/apps/native/pkg/jsonrender"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// MarkdownStyle is how a kind of Markdown reads: its text size, in px of
// the default text size, at the Compact density and (Roomy, when set) the
// Comfortable one, and its color.
type MarkdownStyle struct {
	Size  float32
	Roomy float32
	Muted bool
	Err   bool
	// Wide lets a table wider than the reading column grow past it, as
	// far as Markdown.SetRoom allows.
	Wide bool
}

var (
	mdReply    = MarkdownStyle{Size: 13, Roomy: 15, Wide: true}
	mdError    = MarkdownStyle{Size: 13, Roomy: 15, Err: true, Wide: true}
	mdThinking = MarkdownStyle{Size: 13, Roomy: 14, Muted: true}
	mdReport   = MarkdownStyle{Size: 13}
)

// Markdown is the Markdown options of a page, one per style. A page keeps
// the images it loaded in its options, so they last as long as the page.
type Markdown struct {
	// Load reads an image a reply names; nil shows images as their
	// descriptions.
	Load func(url string) (ui.ImageSource, error)
	// Enlarge, with Load, makes the images Load read clickable: a click
	// hands the image and its description to Enlarge.
	Enlarge func(bm *ui.Bitmap, label string)
	// Own gives each view its own selection and context menu, for text
	// outside the transcript's selectable rows.
	Own bool

	kit      *kit.Kit
	opts     map[MarkdownStyle]*markdown.Options
	tables   map[tableOptionsKey]*markdown.Options
	measured map[tableKey]tableLayout
	images   sync.Map // url → *mdImage
	diagrams map[diagramKey]*diagram
	// textW is the reading column's width inside its padding, and wideW
	// how wide a Wide style's table may be, centered on the column.
	textW, wideW float32
}

// tableOptionsKey keys the options of the tables a Wide style lays out
// itself: open while one is still being written, and col the widest its
// columns' text may be, in quarter ems.
type tableOptionsKey struct {
	style MarkdownStyle
	open  bool
	col   int
}

// tableKey keys a table's measures: its source, at a theme's sizes.
type tableKey struct {
	src string
	th  *markdown.Theme
}

// wideTableScroll keys the sideways scrolling of a table wider than the
// room there is.
type wideTableScroll struct{}

const (
	// wideTableMax is how wide, in px of the default text size, a table
	// grows past the reading column at most.
	wideTableMax = 1200
	// wideTableMargin is the room, in px of the default text size, a
	// table past the column keeps from the transcript list's sides,
	// clear of what Env.Overlay draws along them (Droi's TurnRail).
	wideTableMargin = 40
	// tableMinColumn is the narrowest, in ems, a column's text wraps at
	// for its table to fit: narrower, the table scrolls sideways instead.
	tableMinColumn = 10
)

// SetRoom sets the width the reading column's text takes and how wide a
// table may grow past it, centered on it; a table stays in the column
// while wide is not the larger.
func (m *Markdown) SetRoom(text, wide float32) { m.textW, m.wideW = text, wide }

// mdImage is an image a reply names, loading or loaded.
type mdImage struct {
	mu  sync.Mutex
	src ui.ImageSource
}

// Options are the options of style s for kit k, made anew when the kit
// changes (the theme, the font, the text size, the density or the zoom).
func (m *Markdown) Options(k *kit.Kit, s MarkdownStyle) *markdown.Options {
	if m.kit != k || m.opts == nil {
		m.kit, m.opts, m.tables, m.measured = k, map[MarkdownStyle]*markdown.Options{}, map[tableOptionsKey]*markdown.Options{}, nil
	}
	if o := m.opts[s]; o != nil {
		return o
	}
	t := k.T
	color := t.Foreground
	switch {
	case s.Err:
		color = t.DestructiveForeground
	case s.Muted:
		color = t.MutedForeground
	}
	size := s.Size
	if s.Roomy > 0 {
		size = k.Dense(s.Size, s.Roomy)
	}
	o := &markdown.Options{
		Theme:          &markdown.Theme{FontSize: k.Px(size), Font: k.Sans, CodeFont: k.Mono, Text: color, Strong: color, Code: t.Code},
		LoadImage:      m.Load,
		ShareSelection: !m.Own,
	}
	em := o.Theme.FontSize
	o.Code = map[string]func(c *ui.Context, b *markdown.CodeBlockView){
		"mermaid": func(c *ui.Context, b *markdown.CodeBlockView) { m.mermaidBlock(c, k, b, em) },
	}
	images := m.Load != nil && m.Enlarge != nil
	if images || s.Wide {
		o.Block = func(c *ui.Context, d *parser.Document, i int) bool {
			switch b := d.Block(i); {
			case s.Wide && b.Kind == parser.Table:
				return m.wideTable(c, k, d, i, s)
			case images && b.Kind == parser.Paragraph && imagesOnly(d.Inlines(i)):
				m.imageRow(c, k, d.Inlines(i), em)
				return true
			}
			return false
		}
	}
	m.opts[s] = o
	return o
}

// wideTable lays out a top-level table wider than the reading column:
// past the column, centered on it, as wide as its columns ask up to the
// room there is. Where they ask for more, its widest columns wrap
// sooner, down to tableMinColumn ems, and beyond that it scrolls
// sideways, faded at the edges that hide more of it. markdown.View lays a
// table out within its own column, so such a table is a view of its own.
func (m *Markdown) wideTable(c *ui.Context, k *kit.Kit, d *parser.Document, i int, s MarkdownStyle) bool {
	b := d.Block(i)
	if b.Parent != 0 || m.textW <= 0 {
		return false
	}
	src := tableSource(d, b)
	l := m.measure(c, src, d, i, m.opts[s].Theme)
	if l.width(l.maxCol) <= m.textW {
		return false
	}
	room := max(m.textW, m.wideW)
	col := l.fit(room)
	w := l.width(col)
	o := m.tableOptions(k, s, b.Open, col)
	show := func() {
		// The outer view reveals the table as it streams in: this one shows
		// what it is given at once, and carries on its parse once it ends.
		if b.Open {
			markdown.Stream(c, src, o)
		} else {
			markdown.View(c, src, o)
		}
	}
	box := ui.Column(c).Width(min(w, room)).Shrink(0).AlignSelf(ui.Center)
	box.Children(func() {
		if w <= room {
			show()
			return
		}
		st := *ui.Local(box, wideTableScroll{}, func() *ui.ScrollState { return new(ui.ScrollState) })
		ui.ScrollHorizontal(c).TrackScroll(st).FillWidth().Children(func() {
			ui.Column(c).Width(w).Shrink(0).Children(show)
		})
		bg, fade := k.T.Background, k.Px(32)
		if st.X > 0 {
			ui.Box(c).Absolute().Top(0).Bottom(0).Left(0).Width(fade).Gradient(bg, bg.Alpha(0), 90).PassThrough()
		}
		if st.X < st.MaxX {
			ui.Box(c).Absolute().Top(0).Bottom(0).Right(0).Width(fade).Gradient(bg.Alpha(0), bg, 90).PassThrough()
		}
	})
	return true
}

// tableSource is table b's source for a view of its own. A table at the
// end of a source stays open, its columns only growing, as while it is
// written: one that is done ends in a blank line, so that its columns
// narrow again when the room does.
func tableSource(d *parser.Document, b *parser.Block) string {
	src := d.Source()
	if b.Open {
		return src[b.Start:b.End]
	}
	end := b.End
	for end < len(src) && end < b.End+2 && src[end] == '\n' {
		end++
	}
	if strings.HasSuffix(src[b.Start:end], "\n\n") {
		return src[b.Start:end]
	}
	return strings.TrimRight(src[b.Start:end], "\n") + "\n\n"
}

// tableOptions are style s's options for a table it lays out itself,
// open while the table is still being written, its columns' text wrapping
// at col ems.
func (m *Markdown) tableOptions(k *kit.Kit, s MarkdownStyle, open bool, col float32) *markdown.Options {
	key := tableOptionsKey{s, open, int(col * 4)}
	if o := m.tables[key]; o != nil {
		return o
	}
	th := *m.Options(k, s).Theme
	th.TableMaxColumn = col
	o := &markdown.Options{Theme: &th, LoadImage: m.Load, ShareSelection: !m.Own}
	if open {
		o.Reveal = &markdown.Reveal{Unit: markdown.AtOnce, Fade: -1}
	}
	m.tables[key] = o
	return o
}

// tableLayout is how markdown.View lays a table's columns out: the width
// of the widest cell's text in each, and the room around a column's text,
// in DIPs; em is the theme's, and maxCol its TableMaxColumn.
type tableLayout struct {
	text       []float32
	pad        float32
	em, maxCol float32
}

// width is the table's width with its columns' text wrapping at col ems,
// inside its border.
func (l tableLayout) width(col float32) float32 {
	w := float32(2)
	for _, t := range l.text {
		w += min(t, col*l.em) + l.pad
	}
	return w
}

// fit is the widest, in quarter ems, that the columns' text may be for the
// table to fit in room, from maxCol down to tableMinColumn.
func (l tableLayout) fit(room float32) float32 {
	for col := l.maxCol; col > tableMinColumn; col -= 0.25 {
		if l.width(col) <= room {
			return col
		}
	}
	return tableMinColumn
}

// measure measures table i, whose source is src, as markdown.View draws
// it with theme th, once for each source and theme.
func (m *Markdown) measure(c *ui.Context, src string, d *parser.Document, i int, th *markdown.Theme) tableLayout {
	key := tableKey{src, th}
	if l, ok := m.measured[key]; ok {
		return l
	}
	if m.measured == nil || len(m.measured) >= 64 {
		m.measured = map[tableKey]tableLayout{}
	}
	l := measureTable(c, d, i, th)
	m.measured[key] = l
	return l
}

// measureTable measures each cell's text in the fonts markdown.View draws
// it with: the header's and strong text bold, code in the code font, with
// the room its background takes past the glyphs.
func measureTable(c *ui.Context, d *parser.Document, i int, th *markdown.Theme) tableLayout {
	em := th.FontSize
	maxCol, padX, code := th.TableMaxColumn, th.TableCellPadding[0], th.CodeSize
	if maxCol == 0 {
		maxCol = 24
	}
	if padX == 0 {
		padX = 0.6
	}
	if code == 0 {
		code = 0.875
	}
	l := tableLayout{text: make([]float32, len(d.Block(i).Align)), pad: 2*em*padX + 1, em: em, maxCol: maxCol}
	var spans []ui.Span
	for r := i + 1; r < d.Block(i).Next; r = d.Block(r).Next {
		weight := 400
		if d.Block(r).Header {
			weight = 600
		}
		col := 0
		for cell := r + 1; cell < d.Block(r).Next && col < len(l.text); cell = d.Block(cell).Next {
			spans = spans[:0]
			slack := float32(1)
			in := d.Inlines(cell)
			// strongTo is the end of the strong text the inlines are in: a
			// node's children follow it, up to its Next.
			strongTo := 0
			for j := range in {
				n := &in[j]
				if n.Kind == parser.Strong {
					strongTo = max(strongTo, n.Next)
				}
				wt := weight
				if j < strongTo {
					wt = 600
				}
				switch n.Kind {
				case parser.Text, parser.HTML, parser.Math:
					spans = append(spans, ui.Span{Text: n.Text, Font: th.Font, Size: em, Weight: wt})
				case parser.Code:
					spans = append(spans, ui.Span{Text: n.Text, Font: th.CodeFont, Size: em * code, Weight: wt})
					slack += em * 0.3
				case parser.SoftBreak, parser.LineBreak:
					spans = append(spans, ui.Span{Text: " ", Font: th.Font, Size: em, Weight: wt})
				}
			}
			var tw float32
			if len(spans) > 0 {
				tw, _ = c.MeasureText(0, spans...)
			}
			l.text[col] = max(l.text[col], tw+slack)
			col++
		}
	}
	return l
}

// imageRow shows a paragraph of images alone as markdown.View does, each
// at most as wide as the view, its description until it loads, but each
// one a button that enlarges it. markdown.View has no hook for clicks on
// its images, so the paragraph is built here.
func (m *Markdown) imageRow(c *ui.Context, k *kit.Kit, in []parser.Inline, em float32) {
	ui.Row(c).Wrap().Gap(em * 0.5).AlignSelf(ui.Stretch).Children(func() {
		for j := 0; j < len(in); j = in[j].Next {
			n := &in[j]
			if n.Kind != parser.Image {
				continue
			}
			alt := parser.PlainText(in[j+1 : n.Next])
			var src ui.ImageSource
			if !n.Pending {
				src = m.image(c, n.Dest)
			}
			bm, _ := src.(*ui.Bitmap)
			if bm == nil {
				label := alt
				if label == "" {
					label = l10n.L("Image")
				}
				ph := ui.Row(c).Padding(em*0.5, em*0.75).Radius(em * 0.3).Background(k.T.Foreground.Alpha(0.07))
				ph.Role(ui.RoleImage).Label(alt)
				ph.Children(func() { ui.Text(c, label).TextColor(k.T.MutedForeground).FontSize(em * 0.875) })
				continue
			}
			w, h := bm.Size()
			tip := n.Title
			if tip == "" {
				tip = l10n.L("Click to enlarge")
			}
			img := ui.Image(c, bm).Label(alt).Role(ui.RoleButton).Focusable().Tooltip(tip).Cursor(ui.CursorPointer).
				Radius(em * 0.3).MaxWidthPercent(100)
			if w > 0 && h > 0 {
				img.AspectRatio(float32(w) / float32(h))
			}
			if img.Clicked() {
				m.Enlarge(bm, alt)
			}
		}
	})
}

// image returns the image at url once Load has read it, starting it the
// first time, on a goroutine of its own.
func (m *Markdown) image(c *ui.Context, url string) ui.ImageSource {
	if url == "" {
		return nil
	}
	v, loaded := m.images.LoadOrStore(url, &mdImage{})
	e := v.(*mdImage)
	if !loaded {
		load, services := m.Load, c.Services()
		go func() {
			src, _ := load(url)
			e.mu.Lock()
			e.src = src
			e.mu.Unlock()
			services.Invalidate()
		}()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.src
}

// imagesOnly reports whether the inlines are images, and the spaces
// between them, alone: the paragraphs markdown.View shows as images.
func imagesOnly(in []parser.Inline) bool {
	n := 0
	for j := 0; j < len(in); j = in[j].Next {
		switch in[j].Kind {
		case parser.Image:
			n++
		case parser.SoftBreak, parser.LineBreak:
		case parser.Text:
			if strings.TrimSpace(in[j].Text) != "" {
				return false
			}
		default:
			return false
		}
	}
	return n > 0
}

// markdownText shows Markdown, as it streams in while streaming.
func markdownText(c *ui.Context, text string, o *markdown.Options, streaming bool) ui.Element {
	if streaming {
		return markdown.Stream(c, text, o)
	}
	return markdown.View(c, text, o)
}

// reply is a text block: Markdown, with the json-render blocks a reply
// can carry drawn as their components.
func (v *View) reply(c *ui.Context, text string, style MarkdownStyle, streaming bool) {
	k := v.kit()
	o := v.md.Options(k, style)
	if !jsonrender.HasRenderTag(text) {
		markdownText(c, text, o, streaming)
		return
	}
	segs := jsonrender.SplitReply(text)
	ui.Column(c).Gap(k.Px(12)).Children(func() {
		for i, seg := range segs {
			switch seg.Kind {
			case jsonrender.Markdown:
				markdownText(c, seg.Text, o, streaming && i == len(segs)-1)
			case jsonrender.Render:
				renderTree(c, k, seg.Spec)
			case jsonrender.Pending:
				k.Text(c, l10n.L("Rendering…"), 13, 19.5).TextColor(k.T.MutedForeground)
			}
		}
	})
}

var errNotLocalImage = errors.New("not an image on the computer")

// loadImage reads an image a reply names by its path on the computer
// through the Daemon, so a reply's screenshot shows over any connection
// to it. Other images stay their descriptions.
func (v *View) loadImage(url string) (ui.ImageSource, error) {
	path := attachments.LocalImagePath(url)
	ctl := v.env.Controller
	if path == "" || ctl == nil {
		return nil, errNotLocalImage
	}
	cl, err := ctl.Client()
	if err != nil {
		return nil, err
	}
	f, err := cl.GetWorkspaceFileContent(v.env.Ctx, protocol.GetWorkspaceFileContentParams{SessionID: v.id, FilePath: path,
		Encoding: protocol.GetWorkspaceFileContentParamsEncodingBase64})
	if err != nil {
		return nil, err
	}
	if f.Encoding != protocol.GetWorkspaceFileContentParamsEncodingBase64 || !strings.HasPrefix(f.MimeType, "image/") {
		return nil, errNotLocalImage
	}
	data, err := base64.StdEncoding.DecodeString(f.Content)
	if err != nil {
		return nil, err
	}
	bm, err := ui.DecodeBitmap(data)
	if err != nil {
		return nil, err
	}
	return bm, nil
}
