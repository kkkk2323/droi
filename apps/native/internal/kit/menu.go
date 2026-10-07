package kit

import "github.com/egoist/mygo/ui"

// MenuPopup is Base UI's Menu.Popup as the web Client styles it: a panel
// of popover color 6px below anchor, aligned to its end when end is set,
// min-w-44, rounded-lg, a border, p-1 and shadow-lg. fn builds the items.
func (k *Kit) MenuPopup(c *ui.Context, anchor *ui.Element, open *bool, end bool, label string, fn func()) *ui.Element {
	t := k.T
	return ui.PopoverBase(c, anchor, open, func(p *ui.Element) {
		if end {
			p.AttachTo(anchor, ui.AnchorBottomRight, ui.AnchorTopRight)
		}
		p.Role(ui.RoleMenu).Label(label).Margin(k.Px(6), 0, 0, 0).MinWidth(k.Px(176)).Padding(k.Px(4)).Radius(k.Px(8)).
			Border(1, t.Border).Background(t.Popover).TextColor(t.PopoverForeground).FontSize(k.Px(14)).
			Shadow(0, k.Px(10), k.Px(15), -k.Px(3), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).Children(fn)
	})
}

// MenuItem is an item of a MenuPopup: an icon in the muted color and a
// label, highlighted under the pointer. A click closes the menu.
func (k *Kit) MenuItem(c *ui.Context, open *bool, icon, label string) *ui.Element {
	t := k.T
	b := ui.ButtonBase(c).Role(ui.RoleMenuItem).Label(label).Gap(k.Px(8)).Padding(k.Px(6), k.Px(8), k.Px(6), k.Px(10)).
		Radius(k.Px(6)).Justify(ui.Start).Cursor(ui.CursorPointer)
	color := t.PopoverForeground
	if b.Hovered() || b.Focused() {
		b.Background(t.Accent)
		color = t.AccentForeground
	}
	b.Children(func() {
		if icon != "" {
			k.Icon(c, icon, 16, t.MutedForeground)
		}
		k.Text(c, label, 14, 20).TextColor(color).SingleLine().Grow(1)
	})
	if b.Clicked() {
		*open = false
	}
	return b
}
