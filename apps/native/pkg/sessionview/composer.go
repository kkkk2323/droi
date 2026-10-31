package sessionview

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// plainComposer is the plain composer's state.
type plainComposer struct {
	text string
	// focused: the text area took the focus once already.
	focused bool
}

// Composer is a plain-text composer in the reading column under the
// transcript, as Droi's looks without its attachments, slash menu and
// settings: Enter sends and Shift+Enter breaks the line; while a turn runs
// Enter queues the message, Cmd+Enter hands it to the running turn and
// Escape stops the turn. The Session's open Prompts stand in for it, or sit
// above it when all are queued question sets.
func (v *View) Composer(c *ui.Context, s *session.Session) {
	ctl := v.env.Controller
	if s == nil || ctl == nil {
		return
	}
	k := v.kit()
	perms, asks := ctl.PendingPermissions(v.id), ctl.PendingAskUsers(v.id)
	waits := WaitsOnUser(perms, asks)
	running := s.WorkingState() != protocol.DroidWorkingStateIdle || v.compacting
	if running && !waits && c.Shortcut(0, ui.KeyEscape) {
		v.Interrupt()
	}
	ready := s.LoadState() == session.Loaded || v.hasLoaded
	Column(c, k).Shrink(0).Padding(0, k.Px(24), k.Px(12), k.Px(24)).Children(func() {
		if waits {
			v.Prompts(c, perms, asks)
			// The prompt takes the focus; the composer gets it back after.
			v.plain.focused = false
			return
		}
		if len(asks) > 0 {
			ui.Box(c.Key("queued-asks")).Margin(0, 0, k.Px(8), 0).Children(func() { v.Prompts(c, nil, asks) })
		}
		v.plainComposer(c, running, ready)
	})
}

func (v *View) plainComposer(c *ui.Context, running, ready bool) {
	k, t := v.look()
	p := &v.plain
	canSend := ready && strings.TrimSpace(p.text) != ""
	submit := func(placement protocol.QueuePlacement) bool {
		if !ready || strings.TrimSpace(p.text) == "" {
			return false
		}
		switch {
		case !running:
			placement = ""
		case placement == "":
			placement = protocol.QueuePlacementEndOfLoop
		}
		v.Send(p.text, nil, placement)
		p.text = ""
		return true
	}
	// Keyed, so that the error line coming and going before the text area
	// leaves it its identity, and the keyboard focus with it.
	ui.Column(c.Key("composer")).Role(ui.RoleGroup).Label(l10n.L("Message composer")).Gap(k.Px(6)).Children(func() {
		if v.err != "" {
			k.Text(c, v.err, 12, 16).Role(ui.RoleStatus).TextColor(t.DestructiveForeground).PaddingX(k.Px(4))
		}
		ui.Column(c.Key("card")).Radius(k.Px(16)).Border(1, t.Border).Background(t.Background).Children(func() {
			placeholder := l10n.L("Ask anything")
			if running {
				placeholder = l10n.L("Queue a message… (⌘↩ inserts it now)")
			}
			in := k.TextArea(c.Key("message"), &p.text, l10n.L("Message"), placeholder, kit.AreaStyle{Pad: [4]float32{14, 16, 4, 16}, Size: 14, Line: 24,
				MinLines: 1, MaxLines: 8, Color: t.Foreground}).Disabled(!ready)
			if !p.focused {
				in.AutoFocus()
				p.focused = true
			}
			in.OnTextKey(func(key ui.TextKey) bool {
				if key.Key != ui.KeyEnter || key.Mods&ui.Shift != 0 {
					return false
				}
				// The key comes with what was typed after this frame was built.
				p.text = key.Text()
				placement := protocol.QueuePlacement("")
				if key.Mods&ui.Cmd != 0 {
					placement = protocol.QueuePlacementEndOfTurn
				}
				if submit(placement) {
					key.SetText("")
				}
				return true
			})
			ui.Row(c).Gap(k.Px(4)).Padding(k.Px(4), k.Px(8), k.Px(8), k.Px(8)).Children(func() {
				ui.Spacer(c)
				if running {
					stop := k.Button(c, kit.Secondary, 32, true, l10n.L("Cancel")).Radius(k.Px(16))
					stop.Children(func() {
						ui.Box(c).Size(k.Px(10), k.Px(10)).Radius(k.Px(2)).Background(t.SecondaryForeground)
					})
					if stop.Clicked() {
						v.Interrupt()
					}
					if canSend {
						q := k.Button(c, kit.Primary, 32, true, l10n.L("Queue")).Radius(k.Px(16)).Tooltip(l10n.L("Send after this turn (↩); ⌘↩ hands it to the running turn"))
						q.Children(func() { k.Icon(c, "arrow-up", 16, t.PrimaryForeground) })
						if q.Clicked() {
							submit("")
						}
					}
					return
				}
				send := k.Button(c, kit.Primary, 32, true, l10n.L("Send")).Radius(k.Px(16)).Disabled(!canSend)
				send.Children(func() { k.Icon(c, "arrow-up", 16, t.PrimaryForeground) })
				if send.Clicked() {
					submit("")
				}
			})
		})
	})
}

// quote adds text to the plain composer's message as a Markdown quote, or
// hands it to Env.Quote, and puts the keyboard focus back in the composer.
func (v *View) quote(text string) {
	if v.env.Quote != nil {
		v.env.Quote(text)
		return
	}
	p := &v.plain
	p.text = QuoteText(p.text, text)
	p.focused = false
}

// QuoteText is message with text added as a Markdown quote, set apart from
// what message holds by a blank line.
func QuoteText(message, text string) string {
	quoted := "> " + strings.ReplaceAll(strings.TrimSpace(text), "\n", "\n> ") + "\n\n"
	switch {
	case message == "":
	case strings.HasSuffix(message, "\n\n"):
	case strings.HasSuffix(message, "\n"):
		quoted = "\n" + quoted
	default:
		quoted = "\n\n" + quoted
	}
	return message + quoted
}
