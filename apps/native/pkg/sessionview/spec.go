package sessionview

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/controller"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/models"
	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

var mdSpec = MarkdownStyle{Size: 14}

// specCard is an ExitSpecMode call in the transcript: the plan Spec mode
// made, under its title and how the user answered it.
func (v *View) specCard(c *ui.Context, call *transcript.ToolCall) {
	k, t := v.look()
	spec := transcript.ReadSpec(call)
	title := spec.Title
	if title == "" {
		title = l10n.L("Plan")
	}
	open := v.flag("spec:"+call.Use.ID, true)
	ui.Column(c).Role(ui.RoleGroup).Label(l10n.L("Spec: %s", title)).Radius(k.Px(12)).Border(1, t.Border).Background(t.Card.Alpha(0.6)).Clip().Children(func() {
		p := ui.CollapsibleBase(c, open)
		v.holdOnToggle(p.Trigger)
		tr := p.Trigger.Label(l10n.L("Spec: %s", title)).FillWidth().Gap(k.Px(10)).Padding(k.Px(8), k.Px(10)).Cursor(ui.CursorPointer)
		if tr.Hovered() {
			tr.Background(t.Muted.Alpha(0.6))
		}
		tr.Children(func() {
			ui.Box(c).Size(k.Px(28), k.Px(28)).Radius(k.Px(8)).Background(t.Muted).Center().Shrink(0).Children(func() {
				k.Icon(c, "list-checks", 14, t.MutedForeground)
			})
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Row(c).Gap(k.Px(6)).AlignItems(ui.End).Children(func() {
					k.Text(c, "Spec", 13, 19.5).FontWeight(500).TextColor(t.Foreground).Shrink(0)
					k.Text(c, title, 13, 19.5).TextColor(t.MutedForeground).SingleLine().Shrink(1)
				})
				specState(c, k, spec)
			})
			k.Icon(c, "chevron-right", 14, t.MutedForeground).Rotate(90 * p.Progress())
		})
		p.Panel(func() {
			ui.Column(c).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Padding(k.Px(10), k.Px(14), k.Px(12), k.Px(14)).Gap(k.Px(8)).Children(func() {
				if spec.Plan == "" {
					k.Text(c, l10n.L("Writing the plan…"), 13, 19.5).TextColor(t.MutedForeground)
					return
				}
				ui.Column(c).Selectable().Children(func() {
					markdownText(c, spec.Plan, v.md.Options(k, mdSpec), false)
				})
				if spec.Comment != "" {
					ui.Row(c).Gap(k.Px(6)).AlignItems(ui.Start).Padding(k.Px(6), k.Px(10)).Radius(k.Px(8)).Background(t.Muted.Alpha(0.6)).Children(func() {
						k.Icon(c, "message-square", 12, t.MutedForeground).Margin(k.Px(3), 0, 0, 0)
						k.Text(c, spec.Comment, 12.5, 19).TextColor(t.Foreground).Selectable().Grow(1).MinWidth(0)
					})
				}
			})
		})
	})
}

// specState is the line under a spec's title: waiting, approved (here or
// in a new Session), or sent back to plan further.
func specState(c *ui.Context, k *kit.Kit, spec transcript.SpecPlan) {
	t := k.T
	icon, color, label := "clock", t.AttentionForeground, l10n.L("Waiting for your review")
	switch spec.State {
	case transcript.SpecApproved:
		icon, color, label = "check", t.Success, l10n.L("Approved")
		if spec.NewSession {
			label = l10n.L("Approved, carried out in a new session")
		}
	case transcript.SpecRejected:
		icon, color, label = "pencil", t.MutedForeground, l10n.L("Sent back to plan further")
	}
	ui.Row(c).Role(ui.RoleStatus).Label(label).Gap(k.Px(4)).Children(func() {
		k.Icon(c, icon, 12, color)
		k.Text(c, label, 11.5, 17.25).TextColor(t.MutedForeground).SingleLine().Shrink(1)
	})
}

// specChoice is what a proceed option of the Daemon's spec review picks:
// the autonomy the work runs at, and whether it runs in a new Session.
type specChoice struct {
	level      string
	newSession bool
}

var specChoices = map[protocol.ToolConfirmationOutcome]specChoice{
	protocol.ToolConfirmationOutcomeProceedOnce:             {"off", false},
	protocol.ToolConfirmationOutcomeProceedAutoRunLow:       {"low", false},
	protocol.ToolConfirmationOutcomeProceedAutoRunMedium:    {"medium", false},
	protocol.ToolConfirmationOutcomeProceedAutoRunHigh:      {"high", false},
	protocol.ToolConfirmationOutcomeProceedNewSession:       {"off", true},
	protocol.ToolConfirmationOutcomeProceedNewSessionLow:    {"low", true},
	protocol.ToolConfirmationOutcomeProceedNewSessionMedium: {"medium", true},
	protocol.ToolConfirmationOutcomeProceedNewSessionHigh:   {"high", true},
}

var specLevels = []string{"off", "low", "medium", "high"}

// specAnswer is what the user picked and typed on a spec review so far.
type specAnswer struct {
	level      string
	newSession bool
	comment    *string
	levelOpen  bool
}

// isSpecReview tells a permission that asks to approve Spec mode's plan.
func isSpecReview(p controller.Permission) bool {
	return len(p.ToolUses) == 1 && p.ToolUses[0].ConfirmationType == protocol.ToolConfirmationTypeExitSpecMode
}

// specReview stands in for the composer while Spec mode's plan waits for
// approval: the plan is the transcript's card above, and this asks how to
// go on. The Daemon's options are an autonomy level times this Session or a
// new one, so they show as a level menu and a switch; an option outside
// that shows as a row of its own.
func (v *View) specReview(c *ui.Context, p controller.Permission) {
	k, t := v.look()
	if v.specs == nil {
		v.specs = map[string]*specAnswer{}
	}
	st := v.specs[p.RequestID]
	offered := map[specChoice]protocol.ToolConfirmationOutcome{}
	var extra []protocol.ToolConfirmationListItem
	cancel := protocol.ToolConfirmationOutcome("")
	for _, o := range p.Options {
		if o.Value == protocol.ToolConfirmationOutcomeCancel {
			cancel = o.Value
			continue
		}
		if ch, ok := specChoices[o.Value]; ok {
			offered[ch] = o.Value
			continue
		}
		extra = append(extra, o)
	}
	if st == nil {
		st = &specAnswer{comment: new(string)}
		if s := v.env.Controller.Store().Session(v.id); s != nil {
			st.level = string(s.Settings().AutonomyLevel)
		}
		if _, ok := offered[specChoice{st.level, false}]; !ok {
			st.level = "off"
		}
		v.specs[p.RequestID] = st
	}
	_, canNew := offered[specChoice{st.level, true}]
	if !canNew {
		st.newSession = false
	}
	answer := func(a controller.PermissionAnswer, after func()) {
		id := p.RequestID
		go func() {
			err := v.env.Controller.RespondToPermission(v.env.Ctx, id, a)
			v.answerFailed(err)
			if err == nil && after != nil {
				v.env.Update(after)
			}
		}()
	}
	title := ""
	if d, err := p.ToolUses[0].Details.Value(); err == nil {
		if es, ok := d.(*protocol.ExitSpecModeConfirmationDetails); ok {
			title = es.Title
		}
	}
	v.promptCard(c, p.RequestID, l10n.L("Review the spec"), func() {
		ui.Row(c).Gap(k.Px(6)).Children(func() {
			k.Icon(c, "list-checks", 12, t.MutedForeground)
			k.Text(c, "Spec", 11, 16.5).FontWeight(600).TextColor(t.MutedForeground).Shrink(0)
			if title != "" {
				k.Text(c, title, 11, 16.5).TextColor(t.MutedForeground).SingleLine().Shrink(1)
			}
		})
		k.Text(c, l10n.L("Approve the plan above to start the work, or keep planning."), 13, 18).FontWeight(500).Margin(k.Px(6), 0, 0, 0)
		ui.Box(c).Margin(k.Px(8), 0, 0, 0).Radius(k.Px(8)).Background(t.Card).Children(func() {
			k.TextArea(c, st.comment, l10n.L("Comment for Droid"), l10n.L("Add a note for Droid (optional)"),
				kit.AreaStyle{Pad: [4]float32{6, 10, 6, 10}, Size: 12.5, Line: 19, MinLines: 1, MaxLines: 5, Color: t.Foreground})
		})
		for _, o := range extra {
			opt := o
			row := v.optionRow(c, string(opt.Value), opt.Label, false, ui.RoleButton, func() { k.Icon(c, "check", 12, t.Primary) })
			row.Margin(k.Px(4), 0, 0, 0)
			if row.Clicked() {
				answer(controller.PermissionAnswer{SelectedOption: opt.Value, Comment: strings.TrimSpace(*st.comment)}, nil)
			}
		}
		ui.Row(c).Wrap().Gap(k.Px(8)).AlignItems(ui.Center).Margin(k.Px(8), 0, 0, 0).Children(func() {
			opts := []kit.Option{}
			for _, l := range specLevels {
				if _, ok := offered[specChoice{l, st.newSession}]; ok {
					opts = append(opts, kit.Option{Value: l, Label: l10n.T(models.AutonomyLabels[l])})
				}
			}
			if len(opts) > 0 {
				if next, ok := k.QuietSelect(c, &st.levelOpen, l10n.L("Autonomy after approval"), "shield-check", st.level, opts); ok {
					st.level = next
				}
			}
			if canNew {
				ui.Row(c).Gap(k.Px(6)).AlignItems(ui.Center).Children(func() {
					if k.Toggle(c, l10n.L("Start in a new session"), st.newSession, false) {
						st.newSession = !st.newSession
					}
					k.Text(c, l10n.L("New session"), 12, 16).TextColor(t.MutedForeground).Unselectable()
				})
			}
			ui.Spacer(c)
			small := func(variant kit.Variant, label string) ui.Element {
				b := k.Button(c, variant, 28, false, label).PaddingX(k.Px(10)).FontSize(k.Px(12))
				color := t.Foreground
				if variant == kit.Primary {
					color = t.PrimaryForeground
				}
				b.Children(func() { k.Text(c, label, 12, 16).FontWeight(500).TextColor(color) })
				return b
			}
			if cancel != "" && small(kit.Ghost, l10n.L("Keep planning")).Clicked() {
				note := strings.TrimSpace(*st.comment)
				// The Daemon does not hand a declined plan's comment to the
				// model; it goes as the next message instead.
				answer(controller.PermissionAnswer{SelectedOption: cancel}, func() {
					if note != "" {
						v.Send(note, nil, "")
					}
				})
			}
			choice, ok := offered[specChoice{st.level, st.newSession}]
			label := l10n.L("Proceed")
			if st.newSession {
				label = l10n.L("Proceed in a new session")
			}
			go_ := small(kit.Primary, label).Disabled(!ok).AutoFocus()
			if go_.Clicked() && ok {
				answer(controller.PermissionAnswer{SelectedOption: choice, Comment: strings.TrimSpace(*st.comment), ClientStartsHandoffSession: st.newSession}, nil)
			}
		})
	})
}
