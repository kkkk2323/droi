package kit

import "github.com/egoist/mygo/ui"

// Option is a choice of a select or segmented control.
type Option struct {
	Value, Label string
	Disabled     bool
}

// QuietTrigger is the composer's text-button trigger: h-7, rounded-md,
// px-2, 13px in the foreground at 75%, the muted background while hovered
// or open.
func (k *Kit) QuietTrigger(c *ui.Context, label string, open bool) *ui.Element {
	t := k.T
	b := ui.ButtonBase(c).Label(label).Expanded(open).Height(k.Px(28)).Gap(k.Px(6)).PaddingX(k.Px(8)).Radius(k.Px(8)).Shrink(0).
		FontSize(k.Px(13)).Cursor(ui.CursorPointer)
	if b.Hovered() || open {
		b.Background(t.Muted)
	}
	return b
}

// QuietSelect is the web Client's Select with quiet: an icon, the chosen
// label and a chevron, opening a list of the options with a check by the
// chosen one. It returns the value picked in this frame.
func (k *Kit) QuietSelect(c *ui.Context, open *bool, label, icon, value string, options []Option) (string, bool) {
	t := k.T
	known := false
	for _, o := range options {
		if o.Value == value {
			known = true
		}
	}
	items := options
	if !known && value != "" {
		items = append([]Option{{Value: value, Label: value}}, options...)
	}
	current := label
	for _, o := range items {
		if o.Value == value {
			current = o.Label
		}
	}
	tr := k.QuietTrigger(c, label, *open).Role(ui.RoleComboBox).Gap(k.Px(4)).MaxWidth(k.Px(208)).Disabled(len(items) == 0)
	fg := t.Foreground.Alpha(0.75)
	if tr.Hovered() || *open {
		fg = t.Foreground
	}
	tr.Children(func() {
		if icon != "" {
			k.Icon(c, icon, 14, fg)
		}
		k.Text(c, current, 13, 19.5).TextColor(fg).SingleLine().Shrink(1)
		k.Icon(c, "chevron-down", 12, fg).Opacity(0.6)
	})
	if tr.Clicked() {
		*open = !*open
	}
	var picked string
	ok := false
	ui.PopoverBase(c, tr, open, func(p *ui.Element) {
		p.Role(ui.RoleList).Label(label).Margin(k.Px(4), 0, 0, 0).MinWidth(tr.Bounds().W).Padding(k.Px(4)).Radius(k.Px(8)).
			Border(1, t.Border).Background(t.Popover).Shadow(0, k.Px(10), k.Px(15), -k.Px(3), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).Children(func() {
			for _, o := range items {
				it := Selected(ui.ButtonBase(c).Key(o.Value).Role(ui.RoleListItem).Label(o.Label), o.Value == value).Disabled(o.Disabled).
					Gap(k.Px(6)).Padding(k.Px(6), k.Px(12), k.Px(6), k.Px(6)).Radius(k.Px(6)).Justify(ui.Start).Cursor(ui.CursorPointer)
				color := t.PopoverForeground
				if it.Hovered() || it.Focused() {
					it.Background(t.Accent)
					color = t.AccentForeground
				}
				if o.Disabled {
					it.Opacity(0.5)
				}
				it.Children(func() {
					ui.Box(c).Size(k.Px(16), k.Px(16)).Center().Children(func() {
						if o.Value == value {
							k.Icon(c, "check", 14, color)
						}
					})
					k.Text(c, o.Label, 13, 19.5).TextColor(color).SingleLine()
				})
				if it.Clicked() {
					*open = false
					if o.Value != value {
						picked, ok = o.Value, true
					}
				}
			}
		})
	})
	return picked, ok
}

// Segmented is every choice named at once, one click each, in a muted
// track with the chosen one raised. It returns the value picked.
func (k *Kit) Segmented(c *ui.Context, label, value string, options []Option) (string, bool) {
	t := k.T
	var picked string
	ok := false
	ui.Row(c).Role(ui.RoleRadioGroup).Label(label).Grow(1).MinWidth(0).Gap(k.Px(2)).Padding(k.Px(2)).Radius(k.Px(8)).Background(t.Muted).Children(func() {
		for _, o := range options {
			checked := o.Value == value
			b := ui.ButtonBase(c).Key(o.Value).Role(ui.RoleRadio).Checked(checked).Label(o.Label).Height(k.Px(24)).Grow(1).Basis(0).MinWidth(0).
				PaddingX(k.Px(6)).Radius(k.Px(6)).Cursor(ui.CursorPointer)
			color := t.MutedForeground
			weight := 400
			if checked {
				b.Background(t.Background).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.05))
				color, weight = t.Foreground, 500
			} else if b.Hovered() {
				color = t.Foreground
			}
			b.Children(func() { k.Text(c, o.Label, 12, 16).FontWeight(weight).TextColor(color).SingleLine() })
			if b.Clicked() && !checked {
				picked, ok = o.Value, true
			}
		}
	})
	return picked, ok
}
