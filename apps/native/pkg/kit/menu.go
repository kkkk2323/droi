package kit

import "github.com/egoist/mygo/ui"

// MenuPopup is Base UI's Menu.Popup as the web Client styles it: a panel
// of popover color 6px below anchor, aligned to its end when end is set,
// min-w-44, rounded-lg, a border, p-1 and shadow-lg. fn builds the items.
func (k *Kit) MenuPopup(c *ui.Context, anchor ui.Element, open *bool, end bool, label string, fn func()) ui.Element {
	t := k.T
	return ui.PopoverBase(c, anchor, open, func(p ui.Element) {
		if end {
			p.AttachTo(anchor, ui.AnchorBottomRight, ui.AnchorTopRight)
		}
		p.Role(ui.RoleMenu).Label(label).Margin(k.Px(6), 0, 0, 0).MinWidth(k.Px(176)).Padding(k.Px(4)).Radius(k.Px(8)).
			Border(1, t.Border).Background(t.Popover).TextColor(t.PopoverForeground).FontSize(k.Px(14)).
			Shadow(0, k.Px(10), k.Px(15), -k.Px(3), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).Children(fn)
	})
}

// popoverMenuButton makes e a menu button whose menu MyGo draws in the
// window (ui.PopoverMenuButton), in MenuPopup's look: the window's theme
// gives MyGo's highlights the primary color, which would fill the item
// under the pointer black.
func (k *Kit) popoverMenuButton(c *ui.Context, e ui.Element, build func(m *ui.Menu)) {
	base := c.Theme()
	t := *base
	t.Background, t.Text, t.TextMuted = k.T.Popover, k.T.PopoverForeground, k.T.MutedForeground
	t.Accent, t.AccentText = k.T.Accent, k.T.AccentForeground
	t.Radius = k.Px(6)
	c.SetTheme(&t)
	defer c.SetTheme(base)
	e.PopoverMenuButton(build)
}

// optionMenu fills a select's menu with options, value's checked, and
// reports the other one chosen in picked.
func optionMenu(options []Option, value string, label func(string) string, picked *string, ok *bool) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		for _, o := range options {
			if m.Item(label(o.Label)).Radio(o.Value == value).Disabled(o.Disabled).Chosen() && o.Value != value {
				*picked, *ok = o.Value, true
			}
		}
	}
}

// MenuItem is an item of a MenuPopup: an icon in the muted color and a
// label, highlighted under the pointer. A click closes the menu.
func (k *Kit) MenuItem(c *ui.Context, open *bool, icon, label string) ui.Element {
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
