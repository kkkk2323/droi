package app

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
)

// mdStyle is how a kind of Markdown reads: its text size, in px of the
// default text size, and its color.
type mdStyle struct {
	size  float32
	muted bool
	err   bool
}

var (
	mdReply    = mdStyle{size: 15}
	mdError    = mdStyle{size: 15, err: true}
	mdThinking = mdStyle{size: 14, muted: true}
	mdReport   = mdStyle{size: 13}
	mdNotes    = mdStyle{size: 13, muted: true}
)

// mdViews are the Markdown options of a page, one per style. A view keeps
// the images it loaded in its options, so they last as long as the page.
type mdViews struct {
	kit  *kit.Kit
	opts map[mdStyle]*markdown.Options
	// load reads an image a reply names; nil shows images as their
	// descriptions.
	load func(url string) (ui.ImageSource, error)
	// own gives each view its own selection and context menu, for text
	// outside the transcript's selectable rows.
	own bool
}

// options are the options of style for kit k, made anew when the kit
// changes (the theme, the font, the text size or the zoom).
func (m *mdViews) options(k *kit.Kit, s mdStyle) *markdown.Options {
	if m.kit != k || m.opts == nil {
		m.kit, m.opts = k, map[mdStyle]*markdown.Options{}
	}
	if o := m.opts[s]; o != nil {
		return o
	}
	t := k.T
	color := t.Foreground
	switch {
	case s.err:
		color = t.DestructiveForeground
	case s.muted:
		color = t.MutedForeground
	}
	o := &markdown.Options{
		Theme:          &markdown.Theme{FontSize: k.Px(s.size), Font: k.Sans, CodeFont: k.Mono, Text: color, Strong: color, Code: t.Code},
		LoadImage:      m.load,
		ShareSelection: !m.own,
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
func (v *sessionView) reply(c *ui.Context, text string, style mdStyle, streaming bool) {
	k := v.a.kit
	o := v.md.options(k, style)
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
				k.Text(c, L("Rendering…"), 13, 19.5).TextColor(k.T.MutedForeground)
			}
		}
	})
}

var errNotLocalImage = errors.New("not an image on the computer")

// loadImage reads an image a reply names by its path on the computer
// through the Daemon, so a reply's screenshot shows over any connection
// to it. Other images stay their descriptions.
func (v *sessionView) loadImage(url string) (ui.ImageSource, error) {
	path := attachments.LocalImagePath(url)
	if path == "" || v.a.ctl == nil {
		return nil, errNotLocalImage
	}
	cl, err := v.a.ctl.Client()
	if err != nil {
		return nil, err
	}
	f, err := cl.GetWorkspaceFileContent(v.a.ctx, protocol.GetWorkspaceFileContentParams{SessionID: v.id, FilePath: path,
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
