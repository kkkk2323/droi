package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/omp"
	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
)

// ompView is the window state for one OMP Session: transcript, pending UI
// requests and a plain composer. It sits beside sessionView for Droid Sessions.
type ompView struct {
	a  *App
	id string

	text string
	err  string

	mu       sync.Mutex
	unsub    func()
	messages []omp.Message
	pending  []omp.UIRequest
	working  bool
	title    string
	model    string
	// answering guards concurrent UI answers.
	answering map[string]bool
}

func (a *App) ompViewOf(id string) *ompView {
	if a.ompViews == nil {
		a.ompViews = map[string]*ompView{}
	}
	v := a.ompViews[id]
	if v == nil {
		v = &ompView{a: a, id: id, answering: map[string]bool{}}
		a.ompViews[id] = v
		v.attach()
	}
	return v
}

func (v *ompView) attach() {
	h := v.a.cfg.Host
	if h == nil {
		return
	}
	handle := h.OMP().Get(v.id)
	if handle == nil || handle.Session == nil {
		return
	}
	v.mu.Lock()
	v.messages = handle.Session.Messages()
	v.pending = handle.Session.PendingUI()
	v.working = handle.Session.Working()
	v.title = handle.Title
	v.model = handle.Model
	if v.unsub != nil {
		v.unsub()
	}
	v.unsub = handle.Session.OnEvent(func(ev omp.Event) {
		v.mu.Lock()
		v.messages = handle.Session.Messages()
		v.pending = handle.Session.PendingUI()
		v.working = handle.Session.Working()
		if t := handle.Session.Title(); t != "" {
			v.title = t
		}
		if m := handle.Session.Model(); m != "" {
			v.model = m
		}
		if ev.Type == "error" && ev.Err != nil {
			v.err = ev.Err.Error()
		}
		v.mu.Unlock()
		v.a.cfg.Update(func() {})
	})
	v.mu.Unlock()
}

func (v *ompView) build(c *ui.Context, a *App) {
	k, t := a.kit, a.kit.T
	h := a.cfg.Host
	var handle *host.OMPHandle
	if h != nil {
		handle = h.OMP().Get(v.id)
	}
	v.mu.Lock()
	msgs := append([]omp.Message(nil), v.messages...)
	pending := append([]omp.UIRequest(nil), v.pending...)
	working := v.working
	title := v.title
	model := v.model
	errMsg := v.err
	v.mu.Unlock()
	if title == "" {
		title = L("OMP session")
	}
	if handle == nil {
		ui.Column(c).Fill().Padding(k.Px(24)).Children(func() {
			k.Text(c, L("This OMP session is no longer running."), 14, 22).TextColor(t.MutedForeground)
			k.Text(c, L("Start a new OMP session from New session."), 13, 20).TextColor(t.MutedForeground)
		})
		return
	}
	ui.Column(c).Role(ui.RoleGroup).Label(title).Fill().Children(func() {
		ui.Row(c).Height(k.Px(44)).PaddingX(k.Px(16)).AlignItems(ui.Center).Gap(k.Px(8)).Shrink(0).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			if a.narrow {
				a.openSessionsButton(c)
			}
			k.Text(c, title, 14, 20).FontWeight(600).SingleLine().Grow(1).MinWidth(0)
			ui.Row(c).Padding(k.Px(4), k.Px(8)).Radius(k.Px(8)).Background(t.Secondary).Children(func() {
				k.Text(c, "OMP", 11, 14).FontWeight(600).TextColor(t.SecondaryForeground)
			})
			if model != "" {
				k.Text(c, model, 12, 16).TextColor(t.MutedForeground).SingleLine()
			}
			if working {
				k.Spinner(c, 14, t.MutedForeground)
			}
		})
		ui.Scroll(c).Grow(1).Padding(k.Px(16), k.Px(24)).Children(func() {
			ui.Column(c).MaxWidth(sessionview.ColumnWidth).Gap(k.Px(12)).Children(func() {
				if len(msgs) == 0 && !working {
					k.Text(c, L("Send a message to start."), 13, 20).TextColor(t.MutedForeground)
				}
				for _, m := range msgs {
					v.messageRow(c, k, m)
				}
				for _, p := range pending {
					v.uiCard(c, k, p)
				}
				if errMsg != "" {
					k.Text(c, errMsg, 12, 18).TextColor(t.DestructiveForeground)
				}
			})
		})
		ui.Column(c).Padding(k.Px(12), k.Px(16), k.Px(16), k.Px(16)).Shrink(0).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			ui.Column(c).MaxWidth(sessionview.ColumnWidth).Gap(k.Px(8)).Children(func() {
				ui.Row(c).Children(func() {
					in := k.TextArea(c, &v.text, L("Message"), L("Message OMP…"), kit.AreaStyle{
						Pad: [4]float32{12, 12, 12, 12}, Size: 13, Line: 20, MinLines: 2, MaxLines: 8, Color: t.Foreground,
					}).Grow(1)
					send := func() {
						text := strings.TrimSpace(v.text)
						if text == "" || handle.Session == nil {
							return
						}
						v.text = ""
						go v.send(text)
						_ = in
					}
					in.OnTextKey(func(key ui.TextKey) bool {
						if key.Key == ui.KeyEnter && key.Mods&ui.Shift == 0 {
							v.text = key.Text()
							send()
							return true
						}
						return false
					})
				})
				ui.Row(c).Gap(k.Px(8)).Justify(ui.End).Children(func() {
					if working {
						stop := k.Button(c, kit.Ghost, 32, false, L("Stop")).Radius(k.Px(16))
						stop.Children(func() { k.Text(c, L("Stop"), 13, 18).TextColor(t.Foreground) })
						if stop.Clicked() {
							go v.abort()
						}
					}
					btn := k.Button(c, kit.Primary, 32, false, L("Send")).Radius(k.Px(16)).Disabled(strings.TrimSpace(v.text) == "")
					btn.Children(func() {
						if working {
							k.Spinner(c, 14, t.PrimaryForeground)
						} else {
							k.Text(c, L("Send"), 13, 18).TextColor(t.PrimaryForeground)
						}
					})
					if btn.Clicked() {
						text := strings.TrimSpace(v.text)
						if text != "" {
							v.text = ""
							go v.send(text)
						}
					}
				})
			})
		})
	})
}

func (v *ompView) messageRow(c *ui.Context, k *kit.Kit, m omp.Message) {
	t := k.T
	label := m.Role
	switch m.Role {
	case "user":
		label = L("You")
	case "assistant":
		label = L("OMP")
	case "tool":
		label = m.ToolName
		if label == "" {
			label = L("Tool")
		}
	}
	ui.Column(c).Gap(k.Px(4)).Children(func() {
		ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
			k.Text(c, label, 12, 16).FontWeight(600).TextColor(t.MutedForeground)
			if m.Streaming {
				k.Spinner(c, 12, t.MutedForeground)
			}
		})
		if m.Thinking != "" {
			k.Text(c, m.Thinking, 12, 18).TextColor(t.MutedForeground)
		}
		body := m.Text
		if m.Role == "tool" {
			body = m.ToolOut
			if body == "" {
				body = m.ToolArgs
			}
		}
		if body != "" {
			color := t.Foreground
			if m.IsError {
				color = t.DestructiveForeground
			}
			k.Text(c, body, 13, 20).TextColor(color).Selectable()
		}
	})
}

func (v *ompView) uiCard(c *ui.Context, k *kit.Kit, p omp.UIRequest) {
	t := k.T
	ui.Column(c).Gap(k.Px(8)).Padding(k.Px(12)).Radius(k.Px(12)).Border(1, t.Border).Background(t.Card).Children(func() {
		k.Text(c, L("OMP needs an answer"), 13, 18).FontWeight(600)
		k.Text(c, p.Method, 12, 16).TextColor(t.MutedForeground)
		// Best-effort parse of common shapes.
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(p.Raw, &raw)
		if msg, ok := rawStringMsg(raw["message"]); ok {
			k.Text(c, msg, 13, 20).Selectable()
		}
		if prompt, ok := rawStringMsg(raw["prompt"]); ok {
			k.Text(c, prompt, 13, 20).Selectable()
		}
		switch p.Method {
		case "confirm":
			ui.Row(c).Gap(k.Px(8)).Children(func() {
				yes := k.Button(c, kit.Primary, 32, false, L("Allow")).Radius(k.Px(12))
				yes.Children(func() { k.Text(c, L("Allow"), 13, 18).TextColor(t.PrimaryForeground) })
				if yes.Clicked() {
					go v.answerUI(p.ID, map[string]any{"confirmed": true})
				}
				no := k.Button(c, kit.Ghost, 32, false, L("Deny")).Radius(k.Px(12))
				no.Children(func() { k.Text(c, L("Deny"), 13, 18) })
				if no.Clicked() {
					go v.answerUI(p.ID, map[string]any{"confirmed": false})
				}
			})
		case "select":
			var opts []struct {
				Value string `json:"value"`
				Label string `json:"label"`
			}
			_ = json.Unmarshal(raw["options"], &opts)
			if len(opts) == 0 {
				// Some shapes nest under detail.
				_ = json.Unmarshal(raw["choices"], &opts)
			}
			ui.Column(c).Gap(k.Px(4)).Children(func() {
				for _, o := range opts {
					label := o.Label
					if label == "" {
						label = o.Value
					}
					b := k.Button(c, kit.Secondary, 32, false, label).Radius(k.Px(12))
					val := o.Value
					b.Children(func() { k.Text(c, label, 13, 18).SingleLine() })
					if b.Clicked() {
						go v.answerUI(p.ID, map[string]any{"value": val})
					}
				}
			})
		default:
			ui.Row(c).Gap(k.Px(8)).Children(func() {
				deny := k.Button(c, kit.Ghost, 32, false, L("Dismiss")).Radius(k.Px(12))
				deny.Children(func() { k.Text(c, L("Dismiss"), 13, 18) })
				if deny.Clicked() {
					go v.answerUI(p.ID, map[string]any{"cancel": true})
				}
			})
		}
	})
}

func rawStringMsg(r json.RawMessage) (string, bool) {
	if len(r) == 0 {
		return "", false
	}
	var s string
	if json.Unmarshal(r, &s) == nil {
		return s, true
	}
	return "", false
}

func (v *ompView) send(text string) {
	h := v.a.cfg.Host
	if h == nil {
		return
	}
	handle := h.OMP().Get(v.id)
	if handle == nil || handle.Session == nil {
		v.a.cfg.Update(func() { v.err = L("This OMP session is no longer running.") })
		return
	}
	ctx, cancel := context.WithTimeout(v.a.ctx, 30*time.Second)
	defer cancel()
	var err error
	if handle.Session.Working() {
		err = handle.Session.FollowUp(ctx, text, nil)
	} else {
		_, err = handle.Session.Prompt(ctx, text, nil)
	}
	if err != nil {
		v.a.cfg.Update(func() { v.err = err.Error() })
	}
}

func (v *ompView) abort() {
	h := v.a.cfg.Host
	if h == nil {
		return
	}
	handle := h.OMP().Get(v.id)
	if handle == nil || handle.Session == nil {
		return
	}
	ctx, cancel := context.WithTimeout(v.a.ctx, 10*time.Second)
	defer cancel()
	if err := handle.Session.Abort(ctx); err != nil {
		v.a.cfg.Update(func() { v.err = err.Error() })
	}
}

func (v *ompView) answerUI(id string, payload map[string]any) {
	v.mu.Lock()
	if v.answering[id] {
		v.mu.Unlock()
		return
	}
	v.answering[id] = true
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		delete(v.answering, id)
		v.mu.Unlock()
	}()
	h := v.a.cfg.Host
	if h == nil {
		return
	}
	handle := h.OMP().Get(v.id)
	if handle == nil || handle.Session == nil {
		return
	}
	ctx, cancel := context.WithTimeout(v.a.ctx, 15*time.Second)
	defer cancel()
	if err := handle.Session.AnswerUI(ctx, id, payload); err != nil {
		v.a.cfg.Update(func() { v.err = err.Error() })
	}
}
