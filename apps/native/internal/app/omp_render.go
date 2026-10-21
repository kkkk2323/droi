package app

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

// ompRender draws OMP transcript entries with the same Markdown and tool
// summary patterns sessionview uses, without needing a Daemon Controller.
type ompRender struct {
	md    sessionview.Markdown
	flags map[string]*bool
}

func (r *ompRender) flag(key string, def bool) *bool {
	if r.flags == nil {
		r.flags = map[string]*bool{}
	}
	if p, ok := r.flags[key]; ok {
		return p
	}
	v := def
	r.flags[key] = &v
	return &v
}

func (r *ompRender) entries(c *ui.Context, a *App, entries []transcript.Entry) {
	k, t := a.kit, a.kit.T
	ui.Column(c).Gap(k.Px(12)).Children(func() {
		for i := range entries {
			e := &entries[i]
			if e.User {
				r.user(c, a, e)
				continue
			}
			ui.Column(c.Key(e.ID)).Gap(k.Px(8)).Children(func() {
				for j, b := range e.Blocks {
					switch b.Kind {
					case transcript.Text:
						style := sessionview.MarkdownStyle{Size: 13, Roomy: 15}
						if e.IsError {
							style.Err = true
						}
						r.markdown(c, a, b.Text, style, b.Streaming)
					case transcript.Thinking:
						r.thinking(c, a, b)
					case transcript.Tools:
						for _, call := range b.Calls {
							r.tool(c, a, call)
						}
					default:
						_ = j
						if b.Text != "" {
							k.Text(c, b.Text, 13, 20).TextColor(t.Foreground).Selectable()
						}
					}
				}
			})
		}
	})
}

func (r *ompRender) user(c *ui.Context, a *App, e *transcript.Entry) {
	k, t := a.kit, a.kit.T
	text := ""
	for _, b := range e.Blocks {
		if b.Kind == transcript.Text {
			text += b.Text
		}
	}
	ui.Row(c.Key(e.ID)).Justify(ui.End).Children(func() {
		ui.Column(c).MaxWidthPercent(85).Padding(k.Px(10), k.Px(14)).Radius(k.Px(16)).Background(t.Primary.Alpha(0.08)).Children(func() {
			k.Text(c, text, 13, 20).TextColor(t.Foreground).Selectable()
		})
	})
}

func (r *ompRender) thinking(c *ui.Context, a *App, b transcript.Block) {
	k, t := a.kit, a.kit.T
	open := r.flag("th:"+b.ID, false)
	label := L("Reasoning")
	if b.Streaming {
		label = L("Reasoning…")
	}
	ui.Column(c.Key(b.ID)).Children(func() {
		trig := ui.ButtonBase(c).Label(label).Expanded(*open).Height(k.Px(28)).Gap(k.Px(6)).PaddingX(k.Px(6)).Radius(k.Px(8)).Justify(ui.Start).Cursor(ui.CursorPointer)
		if trig.Hovered() {
			trig.Background(t.Muted)
		}
		trig.Children(func() {
			icon := "chevron-right"
			if *open {
				icon = "chevron-down"
			}
			k.Icon(c, icon, 14, t.MutedForeground)
			k.Text(c, label, 13, 19.5).TextColor(t.MutedForeground)
			if b.Streaming {
				k.Spinner(c, 12, t.MutedForeground)
			}
		})
		if trig.Clicked() {
			*open = !*open
		}
		if *open {
			ui.Column(c).Margin(k.Px(8), 0, 0, 0).BorderWidth(0, 0, 0, 2).BorderColor(t.Border).Padding(0, 0, 0, k.Px(16)).Selectable().Children(func() {
				r.markdown(c, a, b.Text, sessionview.MarkdownStyle{Size: 13, Roomy: 14, Muted: true}, b.Streaming)
			})
		}
	})
}

func (r *ompRender) tool(c *ui.Context, a *App, call *transcript.ToolCall) {
	k, t := a.kit, a.kit.T
	open := r.flag("tool:"+call.Use.ID, false)
	summary := transcript.Summary(call)
	name := call.Use.Name
	if name == "" {
		name = L("Tool")
	}
	label := name
	if summary != "" {
		label = name + ": " + summary
	}
	pending := call.Result == nil
	err := call.Result != nil && call.Result.IsError != nil && *call.Result.IsError
	ui.Column(c.Key(call.Use.ID)).Children(func() {
		trig := ui.ButtonBase(c).Label(label).Expanded(*open).FillWidth().Gap(k.Px(8)).Padding(k.Px(6), k.Px(8)).Radius(k.Px(8)).Justify(ui.Start).Cursor(ui.CursorPointer)
		if trig.Hovered() {
			trig.Background(t.Muted)
		}
		trig.Children(func() {
			icon := "wrench"
			switch {
			case strings.Contains(strings.ToLower(name), "bash") || strings.Contains(strings.ToLower(name), "shell"):
				icon = "terminal"
			case strings.Contains(strings.ToLower(name), "read"):
				icon = "file-text"
			case strings.Contains(strings.ToLower(name), "edit") || strings.Contains(strings.ToLower(name), "write"):
				icon = "file-pen"
			case strings.Contains(strings.ToLower(name), "grep") || strings.Contains(strings.ToLower(name), "search"):
				icon = "search"
			}
			color := t.MutedForeground
			if err {
				color = t.DestructiveForeground
			}
			k.Icon(c, icon, 14, color)
			k.Text(c, label, 13, 19.5).TextColor(t.Foreground).SingleLine().Grow(1).MinWidth(0)
			if pending {
				k.Spinner(c, 12, t.MutedForeground)
			}
		})
		if trig.Clicked() {
			*open = !*open
		}
		if *open {
			ui.Column(c).Gap(k.Px(6)).Padding(k.Px(8), k.Px(12)).Margin(k.Px(4), 0, 0, k.Px(22)).Radius(k.Px(8)).Border(1, t.Border).Background(t.Card).Children(func() {
				if in := transcript.InputText(call); in != "" {
					k.Text(c, in, 12, 18).Font(k.Mono).TextColor(t.MutedForeground).Selectable()
				}
				if call.Result != nil && len(call.Result.Content) > 0 {
					out := string(call.Result.Content)
					if strings.HasPrefix(out, `"`) {
						if u, err := strconv.Unquote(out); err == nil {
							out = u
						}
					}
					color := t.Foreground
					if err {
						color = t.DestructiveForeground
					}
					k.Text(c, out, 12, 18).Font(k.Mono).TextColor(color).Selectable()
				}
			})
		}
	})
}

func (r *ompRender) markdown(c *ui.Context, a *App, text string, style sessionview.MarkdownStyle, streaming bool) {
	k := a.kit
	o := r.md.Options(k, style)
	if streaming {
		markdown.Stream(c, text, o)
		return
	}
	markdown.View(c, text, o)
}
