package sessionview

import (
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/pasted"
)

// PastedCard is a pasted text's card, as tall as an attached image: a
// document, the text's first line and its size. action is the link under
// the title, "" for none, and expanded its state when it opens the text;
// remove labels the button that removes the card, "" for none.
func PastedCard(c *ui.Context, k *kit.Kit, text, action string, expanded *bool, remove string) (acted, removed bool) {
	t := k.T
	title := pasted.Title(text)
	if title == "" {
		title = l10n.L("Pasted text")
	}
	size := countL(utf8.RuneCountInString(text), l10n.N("%d character"), l10n.N("%d characters"))
	if n := pasted.Lines(text); n > 1 {
		size = countL(n, l10n.N("%d line"), l10n.N("%d lines"))
	}
	ui.Row(c).Role(ui.RoleGroup).Label(l10n.L("Pasted text, %s", size)).Width(k.Px(240)).MaxWidthPercent(100).Height(k.Px(64)).
		Gap(k.Px(10)).Padding(0, k.Px(12), 0, k.Px(10)).Radius(k.Px(8)).Border(1, t.Border).Background(t.Background).Children(func() {
		ui.Box(c).Size(k.Px(40), k.Px(40)).Radius(k.Px(6)).Background(t.Secondary).Center().Shrink(0).Children(func() {
			k.Icon(c, "file-text", 18, t.MutedForeground)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(k.Px(2)).Children(func() {
			k.Text(c, title, 13, 18).FontWeight(500).TextColor(t.Foreground).SingleLine().Tooltip(title)
			ui.Row(c).Gap(k.Px(4)).Children(func() {
				k.Text(c, size, 11.5, 16).TextColor(t.MutedForeground).FontFeatures("tnum").SingleLine().Shrink(0)
				if action == "" {
					return
				}
				k.Text(c, "·", 11.5, 16).TextColor(t.MutedForeground)
				b := ui.ButtonBase(c).Label(action).Cursor(ui.CursorPointer).Shrink(1).MinWidth(0)
				if expanded != nil {
					b.Expanded(*expanded)
				}
				color := t.MutedForeground
				if b.Hovered() {
					color = t.Foreground
				}
				b.Children(func() { k.Text(c, action, 11.5, 16).FontWeight(500).TextColor(color).SingleLine() })
				acted = b.Clicked()
			})
		})
		if remove == "" {
			return
		}
		x := ui.ButtonBase(c).Label(remove).Size(k.Px(20), k.Px(20)).Radius(k.Px(10)).Border(1, t.Border).
			Background(t.Background).Absolute().Top(-k.Px(6)).Right(-k.Px(6)).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.05)).Cursor(ui.CursorPointer)
		color := t.MutedForeground
		if x.Hovered() {
			color = t.Foreground
		}
		x.Children(func() { k.Icon(c, "x", 12, color) })
		removed = x.Clicked()
	})
	return acted, removed
}
