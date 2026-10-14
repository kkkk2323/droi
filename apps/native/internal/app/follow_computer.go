package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/internal/computer"
	"github.com/kkkk2323/droi/apps/native/internal/gateway"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
)

// following reports whether the window shows another computer than this one.
// It is the one flag behind every difference between the two.
func (a *App) following() bool { return a.followed != nil }

// computerID is the id of the computer the window follows, "" for this
// computer: the key a preference that belongs to one computer is kept under.
func (a *App) computerID() string {
	if a.followed == nil {
		return ""
	}
	return a.followed.ID
}

// computerName is how the followed computer is called.
func (a *App) computerName() string {
	if a.followed == nil {
		return ""
	}
	if a.followed.Name != "" {
		return a.followed.Name
	}
	return a.followed.Gateway
}

// followLink checks a pasted pairing link off the UI thread; one that
// reaches a Droi Gateway that accepts its token is saved, and Droi starts
// again on that computer. Nothing is saved otherwise.
func (a *App) followLink(link string) {
	s := &a.settings
	s.computerChecking, s.computerErr = true, ""
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
		defer cancel()
		p, err := computer.Verify(ctx, link)
		a.cfg.Update(func() {
			s.computerChecking = false
			if err != nil {
				s.computerErr = computerError(err)
				return
			}
			s.computerLink = ""
			computer.Save(a.prefs, p)
			a.relaunchOnto()
		})
	}()
}

// useThisComputer forgets the other computer and starts Droi again.
func (a *App) useThisComputer() {
	computer.Clear(a.prefs)
	a.relaunchOnto()
}

// relaunchOnto starts Droi again to pick up the Computer preference; where
// the app cannot (a development build), it says what to do instead.
func (a *App) relaunchOnto() {
	if a.cfg.Relaunch == nil {
		a.settings.computerNote = L("Quit and open Droi again to use the other computer.")
		return
	}
	a.relaunch()
}

func computerError(err error) string {
	switch {
	case errors.Is(err, gateway.ErrNotPairingLink):
		return L("That is not a pairing link. Paste the whole link from the other computer.")
	case errors.Is(err, gateway.ErrNoGatewayAddress):
		return L("The link has no address. Paste the whole link, not just the token.")
	case errors.Is(err, gateway.ErrNotGateway):
		return L("That address does not answer as a Droi Gateway.")
	case errors.Is(err, gateway.ErrTokenRejected):
		return L("That computer does not accept this Pairing Token. It may have been reset; copy the link again.")
	}
	return L("Could not reach that computer: %s", err.Error())
}

func (a *App) computerTab(c *ui.Context) {
	k, t := a.kit, a.kit.T
	s := &a.settings
	a.settingGroup(c, "", "",
		func() {
			desc := L("Droi shows the Sessions of this computer.")
			label := L("This computer")
			if a.following() {
				label = a.computerName()
				desc = L("Droi shows the Sessions of %s. This computer keeps running for paired phones and Automations.", label)
			}
			a.settingRow(c, L("Computer"), desc, func() {
				a.statusPill(c, true, label)
				if a.following() && a.smallButton(c, kit.Outline, L("Use this computer"), "laptop", false).Clicked() {
					a.useThisComputer()
				}
			}, nil)
		},
		func() {
			link := strings.TrimSpace(s.computerLink)
			follow := func() {
				if link != "" && !s.computerChecking {
					a.followLink(link)
				}
			}
			a.settingRow(c, L("Another computer"), L("Paste the pairing link of the other computer, the one a phone scans. Droi checks it and starts again on that computer."), nil, func() {
				ui.Row(c).Gap(k.Px(8)).Children(func() {
					in := a.field(c, &s.computerLink, L("Pairing link"), "http://ubuntu:41417/#pair=…", true, false)
					if in.Submitted() {
						follow()
					}
					if a.smallButton(c, kit.Outline, L("Follow"), "", link == "" || s.computerChecking).Label(L("Follow computer")).Clicked() {
						follow()
					}
				})
				msg, color := "", t.MutedForeground
				switch {
				case s.computerChecking:
					msg = L("Checking the link…")
				case s.computerErr != "":
					msg, color = s.computerErr, t.DestructiveForeground
				case s.computerNote != "":
					msg = s.computerNote
				}
				if msg != "" {
					k.Text(c, msg, 12, 16).Role(ui.RoleStatus).TextColor(color).Margin(k.Px(8), 0, 0, 0)
				}
			})
		},
	)
}

// followingStartView fills the window until the followed computer answers:
// a quiet line while it is tried, and once it fails the error with the way
// back to this computer.
func (a *App) followingStartView(c *ui.Context, status controller.Status) {
	k, t := a.kit, a.kit.T
	name := a.computerName()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(k.Px(44)).Shrink(0).PaddingX(k.Px(8)).AlignItems(ui.Center).DragWindow().Children(func() {
			if a.narrow {
				a.openSessionsButton(c)
			}
		})
		if status.Failure == nil {
			ui.Row(c).Grow(1).Center().Children(func() {
				ui.Row(c).Role(ui.RoleStatus).Label(L("Connecting")).Gap(k.Px(10)).AlignItems(ui.Center).Children(func() {
					wave := float32(pulseWave(c))
					kit.Dot(c, k.Px(8), t.MutedForeground.Alpha(0.4+0.3*wave))
					k.Text(c, L("Connecting to %s", name), 14, 20).TextColor(t.MutedForeground)
				})
			})
			return
		}
		title := L("Could not reach %s", name)
		ui.Column(c).Grow(1).Center().PaddingX(k.Px(24)).Children(func() {
			ui.Column(c).Role(ui.RoleStatus).Label(title).FillWidth().MaxWidth(k.Px(420)).AlignItems(ui.Center).Gap(k.Px(8)).Children(func() {
				k.Icon(c, "circle-alert", 20, t.Attention)
				k.Text(c, title, 14, 20).FontWeight(500).TextAlign(ui.Center)
				k.Text(c, status.Failure.Error(), 13, 18).TextColor(t.MutedForeground).TextAlign(ui.Center)
				k.Text(c, L("Droi keeps trying. Use this computer to leave %s.", name), 13, 18).TextColor(t.MutedForeground).TextAlign(ui.Center)
				if a.smallButton(c, kit.Primary, L("Use this computer"), "laptop", false).Margin(k.Px(8), 0, 0, 0).Clicked() {
					a.useThisComputer()
				}
			})
		})
	})
}
