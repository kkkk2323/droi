package kit

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// SettingGroup is one card of related settings, a divider between rows. The
// card is the reference's: rounded-xl, a hairline, and the faintest ink fill
// (foreground at 2%) so it reads as one material lifted off the page.
func (k *Kit) SettingGroup(c *ui.Context, title, footer string, rows ...func()) {
	t := k.T
	ui.Column(c).Role(ui.RoleGroup).Label(title).Gap(k.Px(6)).Children(func() {
		if title != "" {
			k.Text(c, title, 12, 16).FontWeight(500).TextColor(t.MutedForeground).PaddingX(k.Px(8))
		}
		ui.Column(c).Radius(k.Px(12)).Border(1, t.Border).Background(t.Foreground.Alpha(0.02)).Clip().Children(func() {
			for i, row := range rows {
				if i > 0 {
					ui.Box(c).Height(1).Background(t.Border)
				}
				row()
			}
		})
		if footer != "" {
			k.Text(c, footer, 12, 20).TextColor(t.MutedForeground).PaddingX(k.Px(4))
		}
	})
}

// SettingRow is a setting: title and explanation on the left, its control
// on the right, and what goes under them.
func (k *Kit) SettingRow(c *ui.Context, title, description string, control, below func()) {
	k.SettingRowWith(c, func() { k.Text(c, title, 13, 18).Role(ui.RoleHeading).FontWeight(500) }, description, control, below)
}

// SettingRowWith is a SettingRow whose title is drawn by title.
func (k *Kit) SettingRowWith(c *ui.Context, title func(), description string, control, below func()) {
	t := k.T
	ui.Column(c).Padding(k.Px(10), k.Px(12)).Children(func() {
		ui.Row(c).Wrap().GapX(k.Px(16)).GapY(k.Px(8)).Children(func() {
			ui.Column(c).Grow(1).Basis(0).MinWidth(k.Px(192)).Children(func() {
				title()
				if description != "" {
					k.Text(c, description, 13, 18).TextColor(t.MutedForeground).Margin(k.Px(2), 0, 0, 0)
				}
			})
			if control != nil {
				ui.Row(c).MinWidth(0).Gap(k.Px(8)).Shrink(0).Children(control)
			}
		})
		if below != nil {
			ui.Column(c).Margin(k.Px(10), 0, 0, 0).Children(below)
		}
	})
}

// FieldSelect is the framed Select of a settings row: h-8, min-w-32, a
// border on the page's background; open is whether its list is out. The
// label and the options' labels are l10n keys, or words already
// translated. It returns the value picked, if one other than value was.
func (k *Kit) FieldSelect(c *ui.Context, open *bool, label, value string, options []Option, disabled bool) (string, bool) {
	t := k.T
	label = l10n.T(label)
	current := value
	for _, o := range options {
		if o.Value == value {
			current = l10n.T(o.Label)
		}
	}
	tr := ui.ButtonBase(c).Role(ui.RoleComboBox).Label(label).Expanded(*open).Height(k.Px(28)).MinWidth(k.Px(120)).Gap(k.Px(4)).
		PaddingX(k.Px(9)).Radius(k.Px(8)).Border(1, t.Border).Background(ui.Color{}).Justify(ui.SpaceBetween).Disabled(disabled).Cursor(ui.CursorPointer)
	if tr.Hovered() {
		tr.Background(t.Muted)
	}
	tr.Children(func() {
		k.Text(c, current, 13, 18).TextColor(t.Foreground).SingleLine()
		k.Icon(c, "chevron-down", 12, t.MutedForeground).Opacity(0.9)
	})
	if tr.Clicked() {
		*open = !*open
	}
	var picked string
	ok := false
	ui.PopoverBase(c, tr, open, func(p ui.Element) {
		p.Role(ui.RoleList).Label(label).Margin(k.Px(4), 0, 0, 0).MinWidth(tr.Bounds().W).Padding(k.Px(4)).Radius(k.Px(8)).
			Border(1, t.Border).Background(t.Popover).Shadow(0, k.Px(10), k.Px(15), -k.Px(3), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).Children(func() {
			for _, o := range options {
				it := Selected(ui.ButtonBase(c.Key(o.Value)).Role(ui.RoleListItem).Label(l10n.T(o.Label)), o.Value == value).
					Gap(k.Px(6)).Padding(k.Px(6), k.Px(12), k.Px(6), k.Px(6)).Radius(k.Px(6)).Justify(ui.Start).Cursor(ui.CursorPointer)
				color := t.PopoverForeground
				if it.Hovered() {
					it.Background(t.Accent)
					color = t.AccentForeground
				}
				it.Children(func() {
					ui.Box(c).Size(k.Px(16), k.Px(16)).Center().Children(func() {
						if o.Value == value {
							k.Icon(c, "check", 14, color)
						}
					})
					k.Text(c, l10n.T(o.Label), 13, 19.5).TextColor(color).SingleLine()
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

// Toggle is the settings' Switch, as the reference draws one: 32x20, a
// hairline while off and the accent tint while on.
func (k *Kit) Toggle(c *ui.Context, label string, on, disabled bool) bool {
	t := k.T
	b := ui.ButtonBase(c).Role(ui.RoleSwitch).Checked(on).Label(label).Size(k.Px(32), k.Px(20)).Radius(k.Px(10)).Disabled(disabled).Cursor(ui.CursorPointer)
	bg, border := t.Input, t.Border
	if on {
		bg, border = t.Tint, t.Tint
	}
	b.Background(bg).Border(1, border)
	x := b.Animate("on", b2f(on), 150*time.Millisecond)
	b.Children(func() {
		ui.Box(c).Size(k.Px(16), k.Px(16)).Radius(k.Px(8)).Background(t.PrimaryForeground).Absolute().Top(k.Px(1)).Left(k.Px(1)+x*k.Px(13)).
			Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.15))
	})
	return b.Clicked()
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// Field is a settings text input: h-8, rounded-lg, a hairline, and the tint
// while it has the focus.
func (k *Kit) Field(c *ui.Context, value *string, label, placeholder string, mono, password bool) ui.Element {
	t := k.T
	in := ui.TextInputBase(c, value).Label(label).Placeholder(placeholder).Height(k.Px(32)).Grow(1).MinWidth(0).PaddingX(k.Px(10)).
		Radius(k.Px(8)).Border(1, t.Border).Background(ui.Color{}).FontSize(k.Px(13)).TextColor(t.Foreground)
	if mono {
		in.Font(k.Mono)
	}
	if password {
		in.Password()
	}
	if in.Focused() {
		in.Border(1, t.Tint.Alpha(0.6))
	}
	return in
}

// SmallButton is a settings row's button: h-7, 13px, with an icon before
// its label when icon is set.
func (k *Kit) SmallButton(c *ui.Context, v Variant, label, icon string, disabled bool) ui.Element {
	t := k.T
	b := k.Button(c, v, 28, false, label).Disabled(disabled).FontSize(k.Px(13))
	color := t.Foreground
	switch v {
	case Primary:
		color = t.PrimaryForeground
	case Destructive:
		color = t.Destructive
	}
	b.Children(func() {
		if icon != "" {
			k.Icon(c, icon, 14, color)
		}
		k.Text(c, label, 13, 18).FontWeight(500).TextColor(color)
	})
	return b
}

// StatusPill is a dot and a word for a setting's state, green when ok.
func (k *Kit) StatusPill(c *ui.Context, ok bool, label string) {
	t := k.T
	color, bg := t.MutedForeground, t.Muted
	if ok {
		color, bg = t.Success, t.Success.Alpha(0.1)
	}
	ui.Row(c).Height(k.Px(20)).Gap(k.Px(6)).PaddingX(k.Px(7)).Radius(k.Px(10)).Background(bg).Children(func() {
		Dot(c, k.Px(6), color)
		k.Text(c, label, 12, 16).FontWeight(500).TextColor(color)
	})
}
