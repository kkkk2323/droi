package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/app"
	"github.com/kkkk2323/droi/apps/native/internal/computer"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// pairing is the pairing page's state.
type pairing struct {
	link     string
	checking bool
	err      string
}

// pairingView is what the window shows while the phone follows no
// computer: the pairing link from Droi → Settings → Remote Access, scanned
// or pasted, and the computers paired before.
func (p *phone) pairingView(c *ui.Context, k *kit.Kit) {
	t := k.T
	s := &p.pair
	c.SetTheme(k.UITheme(c.Theme()))
	pair := func(link string) {
		link = strings.TrimSpace(link)
		if link == "" || s.checking {
			return
		}
		s.checking, s.err = true, ""
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			paired, err := computer.Verify(ctx, link, "")
			p.update(func() {
				s.checking = false
				if err != nil {
					s.err = app.ComputerError(err)
					return
				}
				s.link = ""
				computer.Follow(p.prefs, paired)
				p.show(&paired)
			})
		}()
	}
	button := func(v kit.Variant, icon, label string, disabled bool) bool {
		b := k.Button(c, v, 44, false, label).FontSize(k.Px(15)).Disabled(disabled)
		b.Children(func() {
			if icon != "" {
				color := t.Foreground
				if v == kit.Primary {
					color = t.PrimaryForeground
				}
				k.Icon(c, icon, 16, color)
			}
			ui.Text(c, label)
		})
		return b.Clicked()
	}
	ui.Scroll(c).Fill().Background(t.Background).TextColor(t.Foreground).Children(func() {
		ui.Column(c).FillWidth().Padding(k.Px(48), k.Px(24), k.Px(24), k.Px(24)).Gap(k.Px(16)).Children(func() {
			k.Text(c, l10n.L("Pair with a computer"), 22, 28).FontWeight(600).Role(ui.RoleHeading)
			k.Text(c, l10n.L("Open Droi on your computer and turn on Settings → Remote Access. Scan its pairing code, or paste its pairing link."), 15, 22).TextColor(t.MutedForeground)
			if button(kit.Primary, "scan-qr-code", l10n.L("Scan QR code"), s.checking) {
				go func() {
					code, err := scanQR()
					p.update(func() {
						switch {
						case err != nil:
							s.err = scanError(err)
						case code != "":
							pair(code)
						}
					})
				}()
			}
			in := ui.TextInput(c, &s.link).Label(l10n.L("Pairing link")).Placeholder("http://…/#pair=…").
				Keyboard(ui.KeyboardURL).Capitalize(ui.CapitalizeNone).Autocorrect(false).ReturnKey(ui.ReturnGo).
				Height(k.Px(44)).FontSize(k.Px(15)).Radius(k.Px(10)).Border(1, t.Input).PaddingX(k.Px(12)).Margin(k.Px(8), 0, 0, 0)
			if in.Submitted() {
				pair(s.link)
			}
			label := l10n.L("Connect")
			if s.checking {
				label = l10n.L("Checking the link…")
			}
			if button(kit.Outline, "", label, strings.TrimSpace(s.link) == "" || s.checking) {
				pair(s.link)
			}
			if s.err != "" {
				k.Text(c, s.err, 14, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
			}
			remembered := computer.Remembered(p.prefs)
			if len(remembered) == 0 {
				return
			}
			k.Text(c, l10n.L("Paired computers"), 13, 18).FontWeight(500).TextColor(t.MutedForeground).Margin(k.Px(16), 0, 0, 0)
			for _, pc := range remembered {
				name := pc.Name
				if name == "" {
					name = pc.Gateway
				}
				ui.Row(c.Key(pc.ID)).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
					b := ui.ButtonBase(c).Label(name).Grow(1).MinWidth(0).Height(k.Px(52)).PaddingX(k.Px(12)).Gap(k.Px(10)).
						Radius(k.Px(10)).Border(1, t.Border).Justify(ui.Start)
					b.Children(func() {
						k.Icon(c, "laptop", 16, t.MutedForeground)
						k.Text(c, name, 15, 22).SingleLine().Shrink(1)
					})
					if b.Clicked() {
						computer.Follow(p.prefs, pc)
						p.show(&pc)
					}
					if k.IconButton(c, "trash", l10n.L("Forget %s", name), 44).Clicked() {
						computer.Forget(p.prefs, pc.ID)
					}
				})
			}
		})
	})
}

func scanError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return l10n.L("The camera closed before it read a code.")
	}
	return l10n.L("Could not scan: %s", err.Error())
}
