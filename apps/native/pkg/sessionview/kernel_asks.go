package sessionview

import (
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/l10n"
)

// KernelAskCards are a Kernel conversation's ask_user questions, a card each, which answers Kernel: the blocking
// ones stand in for the composer, the others sit above it while the agent works on. Every window and phone gets
// the same card and the first answer wins.
func (v *View) KernelAskCards(c *ui.Context, asks []kernel.PendingRequest) {
	k, t := v.look()
	ui.Column(c).Gap(k.Px(8)).Shrink(0).Children(func() {
		if v.promptErr != "" {
			k.Text(c, v.promptErr, 12, 16).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
		}
		for _, p := range asks {
			v.kernelAskCard(c, p)
		}
	})
}

// BlockingAsks are the questions a run waits on.
func BlockingAsks(asks []kernel.PendingRequest) []kernel.PendingRequest {
	return slices.DeleteFunc(slices.Clone(asks), func(p kernel.PendingRequest) bool { return !p.Blocking })
}

// QueuedAsks are the questions the agent works on past.
func QueuedAsks(asks []kernel.PendingRequest) []kernel.PendingRequest {
	return slices.DeleteFunc(slices.Clone(asks), func(p kernel.PendingRequest) bool { return p.Blocking })
}

func (v *View) kernelAskCard(c *ui.Context, q kernel.PendingRequest) {
	k, t := v.look()
	if v.asks == nil {
		v.asks = map[string]*askState{}
	}
	st := v.asks[q.RequestID]
	if st == nil {
		st = &askState{chosen: map[int][]string{}, custom: map[int]*string{}}
		v.asks[q.RequestID] = st
	}
	if st.step >= len(q.Questions) {
		return
	}
	answering := v.flag("ask-answering:"+q.RequestID, false)
	question := q.Questions[st.step]
	idx := st.step
	selected := st.chosen[idx]
	typed := *st.customAnswer(idx)
	canContinue := !*answering && (len(selected) > 0 || strings.TrimSpace(typed) != "")
	last := st.step+1 == len(q.Questions)
	send := func(p kernel.AskRespondParams) {
		*answering = true
		go func() {
			_, err := v.env.Kernel.RespondAsk(v.env.Ctx, p)
			if kernel.Is(err, kernel.CodeRequestGone) {
				// Answered on another window or phone, or ended with its run: ask.resolved takes the card.
				return
			}
			if err != nil {
				v.env.Update(func() { *answering = false })
			} else {
				v.env.Update(func() { v.promptErr = "" })
			}
			v.answerFailed(err)
		}()
	}
	cancel := func() {
		if *answering {
			return
		}
		yes := true
		send(kernel.AskRespondParams{RequestID: q.RequestID, Cancelled: &yes})
	}
	advance := func() {
		if !canContinue {
			return
		}
		if !last {
			st.step++
			return
		}
		answers := make([]kernel.AskAnswer, len(q.Questions))
		for i, qq := range q.Questions {
			a := kernel.AskAnswer{QuestionID: qq.QuestionID}
			if text := strings.TrimSpace(*st.customAnswer(i)); text != "" {
				a.Text = &text
			} else {
				a.Selected = st.chosen[i]
			}
			answers[i] = a
		}
		send(kernel.AskRespondParams{RequestID: q.RequestID, Answers: answers})
	}
	label, dismiss := l10n.L("Droi has a question"), l10n.L("Cancel")
	if !q.Blocking {
		label, dismiss = l10n.L("Droi has a queued question"), l10n.L("Dismiss")
	}
	v.promptCard(c, q.RequestID, label, func() {
		// Escape stops the turn past a queued question, as without it.
		if q.Blocking && c.Shortcut(0, ui.KeyEscape) {
			cancel()
		}
		fold := l10n.L("Hide options")
		if st.folded {
			fold = l10n.L("Show options")
		}
		header := ui.ButtonBase(c).Label(fold).Expanded(!st.folded).FillWidth().Gap(k.Px(8)).Justify(ui.Start).Cursor(ui.CursorPointer)
		chevron := t.MutedForeground
		if header.Hovered() {
			chevron = t.Foreground
		}
		header.Children(func() {
			ui.Row(c).Gap(k.Px(6)).Grow(1).MinWidth(0).Children(func() {
				k.Icon(c, "message-circle-question-mark", 12, t.MutedForeground)
				k.Text(c, strings.TrimSpace(q.Context), 11, 16.5).FontWeight(600).TextColor(t.MutedForeground).SingleLine()
			})
			if !q.Blocking {
				ui.Row(c).Height(k.Px(18)).PaddingX(k.Px(6)).Radius(k.Px(5)).Background(t.Muted).Tooltip(l10n.L("Droi keeps working and reads your answer later")).Children(func() {
					k.Text(c, l10n.L("Not blocking"), 10, 15).FontWeight(500).TextColor(t.MutedForeground)
				})
			}
			if len(q.Questions) > 1 {
				ui.Row(c).Height(k.Px(18)).PaddingX(k.Px(6)).Radius(k.Px(5)).Background(t.Muted).Children(func() {
					k.Text(c, strconv.Itoa(st.step+1)+" / "+strconv.Itoa(len(q.Questions)), 10, 15).FontWeight(500).FontFeatures("tnum").TextColor(t.MutedForeground)
				})
			}
			rot := float32(0)
			if st.folded {
				rot = 180
			}
			k.Icon(c, "chevron-down", 12, chevron).Rotate(rot)
		})
		if header.Clicked() {
			st.folded = !st.folded
		}
		text := k.Text(c, question.Question, 13, 18).FontWeight(500).Margin(k.Px(6), 0, 0, 0).Selectable()
		if st.folded {
			text.SingleLine()
			return
		}
		if len(question.Options) > 0 {
			role, group := ui.RoleRadio, ui.RoleRadioGroup
			if question.MultiSelect {
				role, group = ui.RoleCheckBox, ui.RoleGroup
			}
			ui.Column(c).Role(group).Label(question.Question).Gap(k.Px(4)).Margin(k.Px(10), 0, 0, 0).Children(func() {
				for _, opt := range question.Options {
					checked := slices.Contains(selected, opt.ID)
					row := v.optionRow(c, opt.ID, opt.Label, "", checked, role, func() {})
					if !row.Clicked() {
						continue
					}
					*st.customAnswer(idx) = ""
					switch {
					case !question.MultiSelect:
						st.chosen[idx] = []string{opt.ID}
					case checked:
						st.chosen[idx] = slices.DeleteFunc(slices.Clone(selected), func(id string) bool { return id == opt.ID })
					default:
						st.chosen[idx] = append(slices.Clone(selected), opt.ID)
					}
				}
			})
		}
		filled := strings.TrimSpace(typed) != ""
		field := ui.Row(c).Height(k.Px(34)).Gap(k.Px(8)).PaddingX(k.Px(10)).Margin(k.Px(4), 0, 0, 0).Radius(k.Px(8)).
			Border(1, t.Input).Background(t.Background)
		if filled {
			field.Border(1, t.Tint).Background(t.Tint.Alpha(0.06))
		}
		field.Children(func() {
			pen := t.MutedForeground
			if filled {
				pen = t.Tint
			}
			k.Icon(c, "pencil", 12, pen)
			value := st.customAnswer(idx)
			in := ui.TextInputBase(c, value).Label(l10n.L("Other answer for: %s", question.Question)).Placeholder(l10n.L("Or type your own answer")).
				Grow(1).MinWidth(0).FontSize(k.Px(k.Dense(12, 13))).TextColor(t.Foreground)
			if in.Focused() {
				field.Border(1, t.Tint.Alpha(0.6))
			}
			if in.Changed() && strings.TrimSpace(*value) != "" {
				st.chosen[idx] = nil
			}
			if in.Submitted() {
				advance()
			}
		})
		ui.Row(c).Gap(k.Px(8)).Margin(k.Px(8), 0, 0, 0).Children(func() {
			if st.step > 0 && v.smallButton(c, kit.Ghost, l10n.L("Back")).Clicked() {
				st.step--
			}
			ui.Spacer(c)
			if v.smallButton(c, kit.Ghost, dismiss).Disabled(*answering).Clicked() {
				cancel()
			}
			next := l10n.L("Next")
			if last {
				next = l10n.L("Answer")
			}
			if v.smallButton(c, kit.Primary, next).Disabled(!canContinue).Clicked() {
				advance()
			}
		})
	})
}
