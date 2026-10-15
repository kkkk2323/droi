package sessionview

import (
	"encoding/base64"
	"errors"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
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
	mdReply    = MarkdownStyle{Size: 15}
	mdError    = MarkdownStyle{Size: 15, Err: true}
	mdThinking = MarkdownStyle{Size: 14, Muted: true}
	mdReport   = MarkdownStyle{Size: 13}
)

// Markdown is the Markdown options of a page, one per style. A page keeps
// the images it loaded in its options, so they last as long as the page.
type Markdown struct {
	// Load reads an image a reply names; nil shows images as their
	// descriptions.
	Load func(url string) (ui.ImageSource, error)
	// Own gives each view its own selection and context menu, for text
	// outside the transcript's selectable rows.
	Own bool

	kit  *kit.Kit
	opts map[MarkdownStyle]*markdown.Options
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
	m.opts[s] = o
	return o
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
