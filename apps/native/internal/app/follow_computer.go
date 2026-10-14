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
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

// following reports whether the window shows another computer than this one.
// It is the one flag behind every difference between the two.
func (a *App) following() bool { return a.cfg.Computer != nil }

// computerID is the id of the computer the window follows, "" for this
// computer: the key a preference that belongs to one computer is kept under.
func (a *App) computerID() string {
	if a.cfg.Computer == nil {
		return ""
	}
	return a.cfg.Computer.ID
}

// The preferences that name Workspaces by path, as kept for the computer the
// window shows.
func (a *App) pinnedWorkspaces() prefs.List { return prefs.PinnedWorkspaces.On(a.computerID()) }
func (a *App) foldedWorkspaces() prefs.List { return prefs.FoldedWorkspaces.On(a.computerID()) }
func (a *App) manualWorkspaces() prefs.List { return prefs.ManualWorkspaces.On(a.computerID()) }

// computerName is how the followed computer is called.
func (a *App) computerName() string {
	if a.cfg.Computer == nil {
		return ""
	}
	return pairedName(*a.cfg.Computer)
}

// pairedName is how a Paired Computer is called: the name its Gateway
// reported, or its address.
func pairedName(p computer.Paired) string {
	if p.Name != "" {
		return p.Name
	}
	return p.Gateway
}

// followLink checks a pasted pairing link off the UI thread; one that
// reaches a Droi Gateway that accepts its token is remembered, and the
// window switches to that computer. Nothing is kept otherwise.
func (a *App) followLink(link string) {
	s := &a.settings
	s.computerChecking, s.computerErr = true, ""
	thisID := ""
	if a.cfg.Host != nil {
		thisID = a.cfg.Host.Settings.Get().ComputerID
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
		defer cancel()
		p, err := computer.Verify(ctx, link, thisID)
		a.cfg.Update(func() {
			if a.closed() {
				return
			}
			s.computerChecking = false
			if err != nil {
				s.computerErr = computerError(err)
				return
			}
			s.computerLink = ""
			a.follow(p)
		})
	}()
}

// follow switches the window to a Paired Computer, remembered from then on.
func (a *App) follow(p computer.Paired) {
	computer.Follow(a.prefs, p)
	a.switchTo(&p)
}

// useThisComputer switches the window back to this computer; the others
// stay remembered.
func (a *App) useThisComputer() {
	computer.UseThisComputer(a.prefs)
	a.switchTo(nil)
}

// forgetComputer drops a remembered computer; forgetting the one the
// window follows switches back to this computer.
func (a *App) forgetComputer(id string) {
	computer.Forget(a.prefs, id)
	if id == a.computerID() {
		a.switchTo(nil)
	}
}

// computerMenu is the sidebar's switch between the computers Droi knows,
// at the window's top left beside the sidebar toggle; with no other
// computer remembered there is nothing to switch to, so it is not shown.
func (a *App) computerMenu(c *ui.Context, left float32) {
	remembered := computer.Remembered(a.prefs)
	if len(remembered) == 0 {
		return
	}
	k, t := a.kit, a.kit.T
	name := L("This computer")
	if a.following() {
		name = a.computerName()
	}
	b := ui.ButtonBase(c).Label(L("Computer: %s", name)).Tooltip(L("Switch computer")).Height(k.Px(32)).MaxWidth(k.Px(240 - left - 8)).
		PaddingX(k.Px(8)).Radius(k.Px(10)).Cursor(ui.CursorPointer).Absolute().Left(left).Top(6)
	color := t.MutedForeground
	if b.Hovered() {
		b.Background(t.SidebarAccent.Alpha(0.6))
		color = t.Foreground
	}
	b.Children(func() {
		ui.Row(c).Gap(k.Px(4)).AlignItems(ui.Center).Children(func() {
			k.Text(c, name, 13, 18).TextColor(color).SingleLine().Shrink(1)
			k.Icon(c, "chevron-down", 12, color)
		})
	})
	b.Menu(func(m *ui.Menu) {
		if m.Item(L("This computer")).Checked(!a.following()).Chosen() && a.following() {
			a.useThisComputer()
		}
		for _, p := range remembered {
			current := p.ID == a.computerID()
			if m.Item(pairedName(p)).Checked(current).Chosen() && !current {
				a.follow(p)
			}
		}
		m.Separator()
		if m.Item(L("Manage computers…")).Chosen() {
			a.Go(Route{Name: "settings", Tab: "computer"})
		}
	})
}

func (a *App) switchTo(p *computer.Paired) {
	if a.cfg.Switch != nil {
		a.cfg.Switch(p)
	}
}

func computerError(err error) string {
	switch {
	case errors.Is(err, gateway.ErrNotPairingLink):
		return L("That is not a pairing link. Paste the whole link from the other computer.")
	case errors.Is(err, gateway.ErrNoGatewayAddress):
		return L("The link has no address. Paste the whole link, not just the token.")
	case errors.Is(err, gateway.ErrNotGateway):
		return L("That address does not answer as a Droi Gateway.")
	case errors.Is(err, computer.ErrNoComputerID):
		return L("That Gateway does not say which computer it is. Update Droi on that computer.")
	case errors.Is(err, computer.ErrThisComputer):
		return L("That link points at this computer. Paste the link of the other computer.")
	case errors.Is(err, gateway.ErrTokenRejected):
		return L("That computer does not accept this Pairing Token. It may have been reset; copy the link again.")
	}
	return L("Could not reach that computer: %s", failureText(err))
}

// gatewayAddress is a Gateway's origin without its scheme, as a computer
// is told apart in the list.
func gatewayAddress(origin string) string {
	if _, rest, ok := strings.Cut(origin, "://"); ok {
		return rest
	}
	return origin
}

// failureText is err as the window shows it, without any token.
func failureText(err error) string { return gateway.RedactToken(err.Error()) }

func (a *App) computerTab(c *ui.Context) {
	k, t := a.kit, a.kit.T
	s := &a.settings
	// A row of the list: the current computer is marked, each other one
	// switched to with one click.
	row := func(name, desc string, current bool, use func(), switchLabel string, forget func()) func() {
		return func() {
			// Selected tells assistive technology which is current; the pill shows it.
			ui.Column(c.Key(name)).Role(ui.RoleListItem).Label(name).Selected(current).Background(ui.Color{}).TextColor(t.Foreground).Children(func() {
				a.settingRow(c, name, desc, func() {
					if current {
						a.statusPill(c, true, L("Current"))
					} else if b := a.smallButton(c, kit.Outline, switchLabel, "", false); b.Label(switchLabel).Clicked() {
						use()
					}
					if forget != nil && k.IconButton(c, "trash", L("Forget %s", name), 32).Clicked() {
						forget()
					}
				}, nil)
			})
		}
	}
	rows := []func(){row(L("This computer"), L("Paired phones and Automations keep running here whichever computer Droi shows."), !a.following(), a.useThisComputer, L("Use this computer"), nil)}
	for _, p := range computer.Remembered(a.prefs) {
		name := pairedName(p)
		rows = append(rows, row(name, gatewayAddress(p.Gateway), p.ID == a.computerID(), func() { a.follow(p) }, L("Switch to %s", name), func() { a.forgetComputer(p.ID) }))
	}
	a.settingGroup(c, L("Computers"), "", rows...)
	a.settingGroup(c, "", "",
		func() {
			link := strings.TrimSpace(s.computerLink)
			follow := func() {
				if link != "" && !s.computerChecking {
					a.followLink(link)
				}
			}
			a.settingRow(c, L("Another computer"), L("Paste the pairing link of the other computer, the one a phone scans. Droi checks it, remembers it and switches to it."), nil, func() {
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
				k.Text(c, failureText(status.Failure), 13, 18).TextColor(t.MutedForeground).TextAlign(ui.Center)
				k.Text(c, L("Droi keeps trying. If the Pairing Token of %s was reset, paste its new link.", name), 13, 18).TextColor(t.MutedForeground).TextAlign(ui.Center)
				ui.Row(c).Wrap().Justify(ui.Center).Gap(k.Px(8)).Margin(k.Px(8), 0, 0, 0).Children(func() {
					if a.smallButton(c, kit.Primary, L("Use this computer"), "laptop", false).Clicked() {
						a.useThisComputer()
					}
					if a.smallButton(c, kit.Outline, L("Paste a new link"), "", false).Clicked() {
						a.Go(Route{Name: "settings", Tab: "computer"})
					}
					forget := L("Forget %s", name)
					if a.smallButton(c, kit.Ghost, L("Forget"), "trash", false).Label(forget).Clicked() {
						a.forgetComputer(a.computerID())
					}
				})
			})
		})
	})
}
