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
// the default text size, and its color.
type MarkdownStyle struct {
	Size  float32
	Muted bool
	Err   bool
}

var (
	mdReply    = MarkdownStyle{Size: 13}
	mdError    = MarkdownStyle{Size: 13, Err: true}
	mdThinking = MarkdownStyle{Size: 13, Muted: true}
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
	images   sync.Map // url → *mdImage
	diagrams map[diagramKey]*diagram
}

// mdImage is an image a reply names, loading or loaded.
type mdImage struct {
	mu  sync.Mutex
	src ui.ImageSource
}

// Options are the options of style s for kit k, made anew when the kit
// changes (the theme, the font, the text size or the zoom).
func (m *Markdown) Options(k *kit.Kit, s MarkdownStyle) *markdown.Options {
	if m.kit != k || m.opts == nil {
		m.kit, m.opts = k, map[MarkdownStyle]*markdown.Options{}
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
	o := &markdown.Options{
		Theme:          &markdown.Theme{FontSize: k.Px(s.Size), Font: k.Sans, CodeFont: k.Mono, Text: color, Strong: color, Code: t.Code},
		LoadImage:      m.Load,
		ShareSelection: !m.Own,
	}
	em := o.Theme.FontSize
	o.Code = map[string]func(c *ui.Context, b *markdown.CodeBlockView){
		"mermaid": func(c *ui.Context, b *markdown.CodeBlockView) { m.mermaidBlock(c, k, b, em) },
	}
	if m.Load != nil && m.Enlarge != nil {
		o.Block = func(c *ui.Context, d *parser.Document, i int) bool {
			if d.Block(i).Kind != parser.Paragraph || !imagesOnly(d.Inlines(i)) {
				return false
			}
			m.imageRow(c, k, d.Inlines(i), em)
			return true
		}
	}
	m.opts[s] = o
	return o
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
