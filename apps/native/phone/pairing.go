package main

import (
	"context"
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

// pairingView is what the window shows while no computer is paired: the
// pairing link from Droi → Settings → Remote Access, pasted or scanned.
func (p *phone) pairingView(c *ui.Context, k *kit.Kit) {
	t := k.T
	s := &p.pair
	c.SetTheme(k.UITheme(c.Theme()))
	ui.Column(c).Fill().Background(t.Background).TextColor(t.Foreground).Padding(k.Px(24)).Gap(k.Px(16)).Justify(ui.Center).Children(func() {
		k.Text(c, l10n.L("Pair with a computer"), 22, 28).FontWeight(600).Role(ui.RoleHeading)
		k.Text(c, l10n.L("Open Droi on your computer, turn on Settings → Remote Access, and paste its pairing link here."), 15, 22).TextColor(t.MutedForeground)
		in := ui.TextInput(c, &s.link).Label(l10n.L("Pairing link")).Placeholder("http://…/#pair=…").
			Keyboard(ui.KeyboardURL).Capitalize(ui.CapitalizeNone).Autocorrect(false).ReturnKey(ui.ReturnGo).
			Height(k.Px(44)).FontSize(k.Px(15)).Radius(k.Px(10)).Border(1, t.Input).PaddingX(k.Px(12))
		link := strings.TrimSpace(s.link)
		connect := func() {
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
		if in.Submitted() {
			connect()
		}
		label := l10n.L("Connect")
		if s.checking {
			label = l10n.L("Checking the link…")
		}
		b := k.Button(c, kit.Primary, 44, false, label).FontSize(k.Px(15)).Disabled(link == "" || s.checking)
		b.Children(func() { ui.Text(c, label) })
		if b.Clicked() {
			connect()
		}
		if s.err != "" {
			k.Text(c, s.err, 14, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
		}
	})
}
