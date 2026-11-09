package sessionview

import (
	"encoding/json"
	"path/filepath"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/l10n"
)

// ApprovalCards are the calls a Kernel conversation waits on, drawn in the composer's place: a card each, which
// answers Kernel. Every window and phone gets the same card and the first answer wins; the card goes when Kernel
// says the call was answered, here or elsewhere.
func (v *View) ApprovalCards(c *ui.Context, approvals []kernel.PendingRequest) {
	k, t := v.look()
	ui.Column(c).Gap(k.Px(8)).Shrink(0).Children(func() {
		if v.promptErr != "" {
			k.Text(c, v.promptErr, 12, 16).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
		}
		for _, p := range approvals {
			v.approvalCard(c, p)
		}
	})
}

func (v *View) approvalCard(c *ui.Context, p kernel.PendingRequest) {
	k, t := v.look()
	folded := v.flag("approval-folded:"+p.RequestID, false)
	answering := v.flag("approval-answering:"+p.RequestID, false)
	v.promptCard(c, p.RequestID, l10n.L("Permission request: %s", p.Tool), func() {
		fold := l10n.L("Hide options")
		if *folded {
			fold = l10n.L("Show options")
		}
		header := ui.ButtonBase(c).Label(fold).Expanded(!*folded).FillWidth().Gap(k.Px(8)).Justify(ui.Start).Cursor(ui.CursorPointer)
		chevron := t.MutedForeground
		if header.Hovered() {
			chevron = t.Foreground
		}
		header.Children(func() {
			ui.Row(c).Gap(k.Px(6)).Children(func() {
				k.Icon(c, "shield-alert", 12, t.Attention)
				k.Text(c, l10n.L("Permission"), 11, 16.5).FontWeight(600).TextColor(t.Attention)
			})
			ui.Spacer(c)
			rot := float32(0)
			if *folded {
				rot = 180
			}
			k.Icon(c, "chevron-down", 12, chevron).Rotate(rot)
		})
		if header.Clicked() {
			*folded = !*folded
		}
		heading := k.Text(c, l10n.L("Droi wants to run %s", p.Tool), 13, 18).FontWeight(500).Margin(k.Px(6), 0, 0, 0)
		if *folded {
			heading.SingleLine()
			return
		}
		if detail := approvalDetail(p.Tool, p.Args); detail != "" {
			// A long command would push the options below the window.
			ui.Scroll(c).MaxHeight(k.Px(240)).Margin(k.Px(8), 0, 0, 0).Radius(k.Px(8)).Background(t.Muted).Clip().Children(func() {
				k.Text(c, detail, 12, 20).Font(k.Mono).Selectable().Padding(k.Px(6), k.Px(10))
			})
		}
		var cancels, allows []kernel.ApprovalOption
		for _, opt := range p.Options {
			if opt.ID == "cancel" {
				cancels = append(cancels, opt)
			} else {
				allows = append(allows, opt)
			}
		}
		answer := func(b ui.Element, opt kernel.ApprovalOption) {
			b.Disabled(*answering)
			if !b.Clicked() || *answering {
				return
			}
			*answering = true
			id, outcome := p.RequestID, opt.ID
			go func() {
				_, err := v.env.Kernel.RespondApproval(v.env.Ctx, kernel.ApprovalRespondParams{RequestID: id, Outcome: outcome})
				if kernel.Is(err, kernel.CodeRequestGone) {
					// Answered on another window or phone, or ended with its run: approval.resolved takes the card.
					return
				}
				if err != nil {
					v.env.Update(func() { *answering = false })
				}
				v.answerFailed(err)
			}()
		}
		// Cancel sits alone on the left; Kernel's first option, allow once, is the primary button at the far right.
		ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Start).Margin(k.Px(10), 0, 0, 0).Children(func() {
			for _, opt := range cancels {
				answer(v.smallButton(c, kit.Ghost, l10n.L("Cancel")).Shrink(0), opt)
			}
			ui.Row(c).Wrap().Justify(ui.End).Gap(k.Px(6)).Grow(1).MinWidth(0).Children(func() {
				for i := len(allows) - 1; i >= 1; i-- {
					label, full := approvalLabel(allows[i])
					b := v.smallButton(c, kit.Outline, label)
					if full != "" {
						b.Tooltip(full)
					}
					answer(b, allows[i])
				}
				if len(allows) > 0 {
					label, _ := approvalLabel(allows[0])
					answer(v.smallButton(c, kit.Primary, label).AutoFocus(), allows[0])
				}
			})
		})
	})
}

// approvalLabel is an option's label in the window's language, and the whole path a file option's label
// shortens. Kernel's own label is English, and stays for an option this build does not know.
func approvalLabel(opt kernel.ApprovalOption) (label, full string) {
	switch {
	case opt.ID == "proceed_once":
		return l10n.L("Allow once"), ""
	case opt.ID == "proceed_always_tool" && opt.Scope != nil && opt.Scope.Tool != "":
		return l10n.L("Always allow %s", opt.Scope.Tool), ""
	case opt.ID == "proceed_always_file" && opt.Scope != nil && opt.Scope.Path != "":
		return l10n.L("Always allow %s", filepath.Base(opt.Scope.Path)), opt.Scope.Path
	}
	return opt.Label, ""
}

// approvalDetail is what a call would do, as a person checks it: the command a shell runs, the file an edit
// changes, the script codemode runs; the arguments as JSON for any other tool.
func approvalDetail(tool string, args json.RawMessage) string {
	var a struct {
		Command string `json:"command"`
		Path    string `json:"path"`
		Code    string `json:"code"`
	}
	_ = json.Unmarshal(args, &a)
	switch tool {
	case "bash", "powershell", "bash_background":
		if a.Command != "" {
			return a.Command
		}
	case "write", "edit":
		if a.Path != "" {
			return a.Path
		}
	case "codemode":
		if a.Code != "" {
			return a.Code
		}
	}
	if len(args) == 0 || string(args) == "null" || string(args) == "{}" {
		return ""
	}
	var v any
	if json.Unmarshal(args, &v) != nil {
		return string(args)
	}
	pretty, _ := json.MarshalIndent(v, "", "  ")
	return string(pretty)
}
