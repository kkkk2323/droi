package app

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/kit"
	"github.com/kkkk2323/droi/apps/native/internal/transcript"
)

// askState is what the user picked and typed on an AskUser card so far.
type askState struct {
	step   int
	chosen map[int][]string
	custom map[int]string
}

// promptArea is every open Prompt of the Session, in the composer's place.
func (v *sessionView) promptArea(c *ui.Context, perms []controller.Permission, asks []controller.AskUser) {
	k, t := v.a.kit, v.a.kit.T
	ui.Column(c).Gap(k.Px(8)).Shrink(0).Children(func() {
		if v.promptErr != "" {
			k.Text(c, v.promptErr, 12, 16).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
		}
		for _, p := range perms {
			v.permissionCard(c, p)
		}
		for _, q := range asks {
			v.askUserCard(c, q)
		}
	})
}

func (v *sessionView) answerFailed(err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	// Another Client got there first; the card is already gone.
	if strings.Contains(lower, "no pending") || strings.Contains(lower, "not found") {
		return
	}
	v.a.cfg.Update(func() { v.promptErr = msg })
}

// promptCard is the frame both cards share: rounded-2xl, bordered, on the
// page's background.
func (v *sessionView) promptCard(c *ui.Context, key, label string, fn func()) {
	k, t := v.a.kit, v.a.kit.T
	ui.Column(c).Key(key).Role(ui.RoleGroup).Label(label).Radius(k.Px(16)).Border(1, t.Border).Background(t.Background).
		Padding(k.Px(12), k.Px(14), k.Px(10), k.Px(14)).Children(fn)
}

// optionRow is an answer as a full-width row; a click picks it.
func (v *sessionView) optionRow(c *ui.Context, key, label string, checked bool, role ui.Role, trailing func()) *ui.Element {
	k, t := v.a.kit, v.a.kit.T
	b := ui.ButtonBase(c).Key(key).Role(role).Label(label).MinHeight(k.Px(36)).FillWidth().Gap(k.Px(8)).Padding(k.Px(6), k.Px(10)).
		Radius(k.Px(8)).Border(1, ui.Transparent).Background(t.Card).Justify(ui.Start).Cursor(ui.CursorPointer)
	if role == ui.RoleRadio || role == ui.RoleCheckBox {
		b.Checked(checked)
	}
	switch {
	case checked:
		b.Border(1, t.Primary.Alpha(0.35)).Background(t.Primary.Alpha(0.08))
	case b.Focused():
		b.Border(1, t.Ring)
	case b.Hovered():
		b.Border(1, t.Border)
	}
	b.Children(func() {
		k.Text(c, label, 12, 16).FontWeight(500).Grow(1).MinWidth(0)
		trailing()
	})
	return b
}

func (v *sessionView) permissionCard(c *ui.Context, p controller.Permission) {
	a := v.a
	k, t := a.kit, a.kit.T
	names := make([]string, len(p.ToolUses))
	for i, u := range p.ToolUses {
		names[i] = u.ToolUse.Name
	}
	title := strings.Join(names, ", ")
	v.promptCard(c, p.RequestID, L("Permission request: %s", title), func() {
		ui.Row(c).Gap(k.Px(6)).Children(func() {
			k.Icon(c, "shield-alert", 12, t.Attention)
			k.Text(c, L("Permission"), 11, 16.5).FontWeight(600).TextColor(t.Attention)
		})
		k.Text(c, L("Droid wants to run %s", title), 13, 18).FontWeight(500).Margin(k.Px(6), 0, 0, 0)
		ui.Column(c).Gap(k.Px(4)).Margin(k.Px(8), 0, 0, 0).Children(func() {
			for _, u := range p.ToolUses {
				details := u.Details.Raw
				if sp := transcript.ParseScriptPermission(details); sp != nil {
					v.scriptPermission(c, u, sp)
					continue
				}
				input, _ := json.Marshal(u.ToolUse.Input)
				k.Text(c, transcript.PermissionDetail(details, input), 12, 20).Font(k.Mono).Selectable().
					Padding(k.Px(6), k.Px(10)).Radius(k.Px(8)).Background(t.Card)
			}
		})
		ui.Column(c).Gap(k.Px(4)).Margin(k.Px(10), 0, 0, 0).Children(func() {
			for i, opt := range p.Options {
				cancel := opt.Value == "cancel"
				row := v.optionRow(c, string(opt.Value), opt.Label, false, ui.RoleButton, func() {
					if cancel {
						k.Icon(c, "x", 12, t.MutedForeground)
					} else {
						k.Icon(c, "check", 12, t.Primary)
					}
				})
				if i == 0 {
					row.AutoFocus()
				}
				if cancel {
					row.TextColor(t.MutedForeground)
				}
				if row.Clicked() {
					id, answer := p.RequestID, opt.Value
					go func() {
						v.answerFailed(a.ctl.RespondToPermission(a.ctx, id, controller.PermissionAnswer{SelectedOption: answer}))
					}()
				}
			}
		})
	})
}

var impactTone = map[string]func(t *kit.Kit) ui.Color{
	"low":    func(k *kit.Kit) ui.Color { return k.T.MutedForeground },
	"medium": func(k *kit.Kit) ui.Color { return k.T.AttentionForeground },
	"high":   func(k *kit.Kit) ui.Color { return k.T.DestructiveForeground },
}

// scriptPermission is a Script's request: the calls in its source by
// line, and the program a click away.
func (v *sessionView) scriptPermission(c *ui.Context, u protocol.ToolConfirmationInfo, sp *transcript.ScriptPermission) {
	k, t := v.a.kit, v.a.kit.T
	var input struct{ Script string }
	if raw, ok := u.ToolUse.Input["script"]; ok {
		_ = jsonUnmarshal(raw, &input.Script)
	}
	open := v.flag("perm-source:"+u.ToolUse.ID, len(sp.Calls) == 0)
	marked := map[int]bool{}
	for _, call := range sp.Calls {
		marked[call.Line] = true
	}
	ui.Column(c).Radius(k.Px(8)).Background(t.Card).Clip().Children(func() {
		if len(sp.Calls) > 0 {
			ui.Column(c).Role(ui.RoleList).Label(L("Calls in the Script")).PaddingY(k.Px(4)).Children(func() {
				for _, call := range sp.Calls {
					ui.Row(c).AlignItems(ui.Start).Gap(k.Px(8)).Padding(k.Px(2), k.Px(10)).Children(func() {
						k.Text(c, "L"+strconv.Itoa(call.Line), 11, 20).Font(k.Mono).TextColor(t.MutedForeground.Alpha(0.7)).Width(k.Px(32)).FontFeatures("tnum").Shrink(0)
						k.Text(c, call.Tool, 12, 20).FontWeight(500).Shrink(0)
						k.Text(c, call.Detail, 12, 20).Font(k.Mono).Grow(1).MinWidth(0)
						if call.Impact != "" {
							color := t.MutedForeground
							if f := impactTone[call.Impact]; f != nil {
								color = f(k)
							}
							k.Text(c, call.Impact, 11, 16.5).TextColor(color).Shrink(0)
						}
					})
				}
			})
		}
		if input.Script == "" {
			return
		}
		label := L("Show source")
		if *open {
			label = L("Hide source")
		}
		b := ui.ButtonBase(c).Label(label).Expanded(*open).FillWidth().Height(k.Px(24)).Gap(k.Px(4)).PaddingX(k.Px(10)).Justify(ui.Start).
			BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Cursor(ui.CursorPointer)
		color := t.MutedForeground
		if b.Hovered() {
			color = t.Foreground
		}
		b.Children(func() {
			rot := float32(0)
			if *open {
				rot = 90
			}
			k.Icon(c, "chevron-right", 12, color).Rotate(rot)
			k.Text(c, label, 11, 16.5).TextColor(color)
		})
		if b.Clicked() {
			*open = !*open
		}
		if *open {
			ui.Box(c).MaxHeight(k.Px(240)).Children(func() { sourceView(c, k, input.Script, marked) })
		}
	})
}

func (v *sessionView) askUserCard(c *ui.Context, q controller.AskUser) {
	a := v.a
	k, t := a.kit, a.kit.T
	if v.asks == nil {
		v.asks = map[string]*askState{}
	}
	st := v.asks[q.RequestID]
	if st == nil {
		st = &askState{chosen: map[int][]string{}, custom: map[int]string{}}
		v.asks[q.RequestID] = st
	}
	if st.step >= len(q.Questions) {
		return
	}
	question := q.Questions[st.step]
	idx := int(question.Index)
	multi := question.MultiSelect != nil && *question.MultiSelect
	selected := st.chosen[idx]
	typed := st.custom[idx]
	canContinue := len(selected) > 0 || strings.TrimSpace(typed) != ""
	last := st.step+1 == len(q.Questions)
	cancel := func() {
		id := q.RequestID
		yes := true
		go func() {
			v.answerFailed(a.ctl.RespondToAskUser(a.ctx, id, protocol.AskUserResult{Cancelled: &yes, Answers: []protocol.AskUserCollectedAnswer{}}))
		}()
	}
	advance := func() {
		if !canContinue {
			return
		}
		if !last {
			st.step++
			return
		}
		answers := make([]protocol.AskUserCollectedAnswer, len(q.Questions))
		for i, qq := range q.Questions {
			ans := strings.TrimSpace(st.custom[int(qq.Index)])
			if ans == "" {
				ans = strings.Join(st.chosen[int(qq.Index)], ", ")
			}
			answers[i] = protocol.AskUserCollectedAnswer{Index: qq.Index, Question: qq.Question, Answer: ans}
		}
		id := q.RequestID
		go func() { v.answerFailed(a.ctl.RespondToAskUser(a.ctx, id, protocol.AskUserResult{Answers: answers})) }()
	}
	v.promptCard(c, q.RequestID, L("Droid has a question"), func() {
		if c.Shortcut(0, ui.KeyEscape) {
			cancel()
		}
		ui.Row(c).Gap(k.Px(8)).Children(func() {
			ui.Row(c).Gap(k.Px(6)).Children(func() {
				k.Icon(c, "message-circle-question-mark", 12, t.MutedForeground)
				k.Text(c, question.Topic, 11, 16.5).FontWeight(600).TextColor(t.MutedForeground)
			})
			if len(q.Questions) > 1 {
				ui.Row(c).Height(k.Px(18)).PaddingX(k.Px(6)).Radius(k.Px(5)).Background(t.Card).Children(func() {
					k.Text(c, strconv.Itoa(st.step+1)+" / "+strconv.Itoa(len(q.Questions)), 10, 15).FontWeight(500).FontFeatures("tnum").TextColor(t.MutedForeground)
				})
			}
		})
		k.Text(c, question.Question, 13, 18).FontWeight(500).Margin(k.Px(6), 0, 0, 0).Selectable()
		if len(question.Options) > 0 {
			role, group := ui.RoleRadio, ui.RoleRadioGroup
			if multi {
				role, group = ui.RoleCheckBox, ui.RoleGroup
			}
			ui.Column(c).Role(group).Label(question.Question).Gap(k.Px(4)).Margin(k.Px(10), 0, 0, 0).Children(func() {
				for _, opt := range question.Options {
					checked := contains(selected, opt)
					row := v.optionRow(c, opt, opt, checked, role, func() {
						if checked {
							k.Icon(c, "check", 12, t.Primary)
						}
					})
					if row.Clicked() {
						st.custom[idx] = ""
						switch {
						case !multi:
							st.chosen[idx] = []string{opt}
						case checked:
							next := []string{}
							for _, s := range selected {
								if s != opt {
									next = append(next, s)
								}
							}
							st.chosen[idx] = next
						default:
							st.chosen[idx] = append(append([]string{}, selected...), opt)
						}
					}
				}
			})
		}
		filled := strings.TrimSpace(typed) != ""
		field := ui.Row(c).Height(k.Px(34)).Gap(k.Px(8)).PaddingX(k.Px(10)).Margin(k.Px(4), 0, 0, 0).Radius(k.Px(8)).
			Border(1, ui.Transparent).Background(t.Card)
		if filled {
			field.Border(1, t.Primary.Alpha(0.35)).Background(t.Primary.Alpha(0.06))
		}
		field.Children(func() {
			pen := t.MutedForeground.Alpha(0.6)
			if filled {
				pen = t.Primary
			}
			k.Icon(c, "pencil", 12, pen)
			value := typed
			in := ui.TextInputBase(c, &value).Label(L("Other answer for: %s", question.Question)).Placeholder(L("Or type your own answer")).
				Grow(1).MinWidth(0).FontSize(k.Px(12)).TextColor(t.Foreground)
			if in.Focused() {
				field.Border(1, t.Ring)
			}
			if in.Changed() {
				st.custom[idx] = value
				if strings.TrimSpace(value) != "" {
					st.chosen[idx] = nil
				}
			}
			if in.Submitted() {
				advance()
			}
		})
		ui.Row(c).Gap(k.Px(8)).Margin(k.Px(8), 0, 0, 0).Children(func() {
			small := func(v kit.Variant, label string) *ui.Element {
				b := k.Button(c, v, 28, false, label).PaddingX(k.Px(8)).FontSize(k.Px(11))
				color := t.Foreground
				if v == kit.Primary {
					color = t.PrimaryForeground
				}
				b.Children(func() { k.Text(c, label, 11, 16).FontWeight(500).TextColor(color) })
				return b
			}
			if st.step > 0 && small(kit.Ghost, L("Back")).Clicked() {
				st.step--
			}
			ui.Spacer(c)
			if small(kit.Ghost, L("Cancel")).Clicked() {
				cancel()
			}
			next := L("Next")
			if last {
				next = L("Answer")
			}
			go_ := small(kit.Primary, next).Disabled(!canContinue)
			if go_.Clicked() {
				advance()
			}
		})
	})
}
