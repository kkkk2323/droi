package app

import (
	"net/url"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/relay"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
)

// Remote is the Gateway that lets a paired phone reach the Daemon.
type Remote interface {
	SetRemoteAccess(on bool)
	Pairing(token string) gateway.PairingInfo
	// Revoke ends the paired phones' connections.
	Revoke(kind gateway.TokenKind)
	// ReconcileRelay connects to the Relay the settings name, or stops.
	ReconcileRelay()
	RelayState() gateway.RelayState
}

// remoteState is what Settings → Remote Access keeps between frames.
type remoteState struct {
	copiedAt time.Time
	qrFor    string
	qr       *ui.Bitmap

	// settingUp shows the steps that set up a Relay; domain and address
	// are what they were given.
	settingUp       bool
	domain, address string
	commandCopiedAt time.Time
}

func (a *App) remoteTab(c *ui.Context) {
	st := a.cfg.Host.Settings.Get()
	a.settingGroup(c, "", "",
		func() { a.remoteAccessRow(c, st.RemoteAccess) },
		func() { a.pairingRow(c, st) },
	)
	a.settingGroup(c, L("Away from this network"), "",
		func() { a.relayRow(c, st) },
	)
}

// relayDocs is how to run a Relay behind Caddy or nginx, or by hand.
const relayDocs = "https://github.com/kkkk2323/droi/blob/main/apps/native/README.md#a-relay-reaching-a-computer-from-anywhere"

func (a *App) relayRow(c *ui.Context, st host.Settings) {
	k := a.kit
	r := &a.remote
	intro := L("Lets a phone on another network reach this computer. A relay is a server of yours that this computer connects out to: no port to open on the router, no VPN on the phone.")
	if st.RelayURL == nil || r.settingUp {
		var control, below func()
		if r.settingUp {
			below = func() { a.relaySteps(c, st) }
		} else {
			control = func() {
				if a.smallButton(c, kit.Outline, L("Set up relay"), "", false).Clicked() {
					r.settingUp, r.address = true, ""
				}
			}
		}
		a.settingRow(c, L("Relay"), intro, control, below)
		return
	}
	desc, label, ok := a.relayStatus(st)
	a.settingRow(c, L("Relay"), desc, func() { a.statusPill(c, ok, label) }, func() {
		ui.Row(c).Gap(k.Px(8)).Children(func() {
			if a.smallButton(c, kit.Outline, L("Change relay"), "", false).Clicked() {
				r.settingUp, r.address = true, ""
			}
			if a.smallButton(c, kit.Outline, L("Remove relay"), "", false).Clicked() {
				a.hostUpdate(false, func(s *host.Settings) { s.RelayURL, s.RelayKey = nil, nil })
				if g := a.cfg.Remote; g != nil {
					go g.ReconcileRelay()
				}
			}
		})
	})
}

// relaySteps walk the user through running droi-relay on a server and
// pasting the relay address it prints.
func (a *App) relaySteps(c *ui.Context, st host.Settings) {
	k, t := a.kit, a.kit.T
	r := &a.remote
	step := func(n, title, text string, body func()) {
		ui.Row(c).Gap(k.Px(10)).AlignItems(ui.Start).Children(func() {
			ui.Box(c).Size(k.Px(20), k.Px(20)).Shrink(0).Radius(k.Px(10)).Background(t.Muted).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				k.Text(c, n, 12, 16).FontWeight(500)
			})
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(k.Px(6)).Children(func() {
				k.Text(c, title, 13, 20).Role(ui.RoleHeading).FontWeight(500)
				k.Text(c, text, 13, 20).TextColor(t.MutedForeground)
				if body != nil {
					body()
				}
			})
		})
	}
	ui.Column(c).Gap(k.Px(14)).Children(func() {
		step("1", L("Get a server and a domain name"), L("The server needs a public IP address, with TCP port 443 open in its firewall and in the cloud provider’s security group. Add an A record that points the domain name to the server. From mainland China, a server in Hong Kong is a good choice."), nil)
		step("2", L("Install the relay on the server"), L("Enter the domain name, then sign in to the server with SSH and run the command. It installs the relay, gets an HTTPS certificate, starts it at every boot and prints the relay address."), func() {
			a.field(c, &r.domain, L("Domain name"), "relay.example.com", true, false)
			command := relay.InstallCommand(r.domain)
			ui.Box(c).Label(L("Install command")).Padding(k.Px(8), k.Px(12)).Radius(k.Px(10)).Border(1, t.Border).Background(t.Background).Children(func() {
				k.Text(c, command, 12, 20).Font(k.Mono).Selectable()
			})
			ui.Row(c).Children(func() {
				label, icon := L("Copy command"), "copy"
				if a.cfg.Now().Sub(r.commandCopiedAt) < 1500*time.Millisecond {
					label, icon = L("Copied"), "check"
				}
				if a.smallButton(c, kit.Outline, label, icon, false).Clicked() {
					c.WriteClipboard(command)
					r.commandCopiedAt = a.cfg.Now()
					c.After(1600 * time.Millisecond)
				}
			})
		})
		step("3", L("Paste the relay address"), L("Copy the whole address the server printed, with the #key=… at its end."), func() {
			in := a.field(c, &r.address, L("Relay address"), "https://relay.example.com/#key=…", true, false)
			ui.Row(c).Gap(k.Px(8)).Children(func() {
				connect := a.smallButton(c, kit.Primary, L("Connect relay"), "", false).Height(k.Px(36)).Clicked()
				if connect || in.Submitted() {
					a.connectRelay(st)
				}
				if a.smallButton(c, kit.Outline, L("Cancel"), "", false).Height(k.Px(36)).Clicked() {
					r.settingUp, r.address = false, ""
				}
			})
		})
		l := ui.ButtonBase(c).Role(ui.RoleLink).Label(L("Already run Caddy or nginx? See other ways to deploy")).Gap(k.Px(4)).AlignSelf(ui.Start).Cursor(ui.CursorPointer)
		l.Children(func() {
			k.Text(c, L("Already run Caddy or nginx? See other ways to deploy"), 13, 20).TextColor(t.MutedForeground).Underline()
			k.Icon(c, "external-link", 12, t.MutedForeground)
		})
		if l.Clicked() {
			c.OpenURL(relayDocs)
		}
	})
}

// connectRelay saves the pasted relay address and connects to it, turning
// Remote Access on, which the Relay needs.
func (a *App) connectRelay(st host.Settings) {
	r := &a.remote
	address, key := host.RelayAddressOf(r.address)
	if address == nil {
		a.settings.err = L("A relay address starts with https://. Copy the whole address the server printed.")
		return
	}
	a.settings.err = ""
	a.hostUpdate(false, func(s *host.Settings) {
		s.RelayURL, s.RelayKey, s.RemoteAccess = address, key, true
	})
	r.settingUp, r.address = false, ""
	if g := a.cfg.Remote; g != nil {
		if st.RemoteAccess {
			go g.ReconcileRelay()
		} else {
			// Turning Remote Access on connects to the Relay too.
			go g.SetRemoteAccess(true)
		}
	}
}

// relayStatus is the Relay row's description, and its pill's label and
// colour, once a Relay is set.
func (a *App) relayStatus(st host.Settings) (desc, label string, ok bool) {
	name := *st.RelayURL
	if u, err := url.Parse(name); err == nil && u.Host != "" {
		name = u.Host
	}
	if !st.RemoteAccess {
		return L("Remote Access is off, so this computer does not connect to the relay. Turn on Remote Access above and it connects."), L("Paused"), false
	}
	if a.cfg.Remote == nil {
		return L("Connecting to %s…", name), L("Connecting"), false
	}
	s := a.cfg.Remote.RelayState()
	at := s.Next.Local().Format("15:04:05")
	switch s.Status {
	case gateway.RelayConnected:
		return L("Connected to %s. Away from this network, paired phones reach this computer through it.", name), L("Connected"), true
	case gateway.RelayRetrying:
		if strings.Contains(s.Err, "certificate has expired") {
			return L("Cannot reach %s: its HTTPS certificate has expired. The relay renews it while TCP port 443 stays open; check the port, then run sudo systemctl restart droi-relay on the server. Trying again at %s.", name, at), L("Not connected"), false
		}
		return L("Cannot reach %s (%s). Check that the relay runs on the server and that TCP port 443 is open. Trying again at %s.", name, s.Err, at), L("Not connected"), false
	case gateway.RelayRefused:
		if s.Code == relay.CloseBadKey || s.Code == relay.CloseBadToken {
			return L("%s refused this computer: the key in the relay address is wrong or missing. On the server, run sudo droi-relay address, then click Change relay and paste the whole address.", name), L("Refused"), false
		}
		return L("%s refused this computer: %s. Trying again at %s.", name, s.Err, at), L("Refused"), false
	}
	return L("Connecting to %s…", name), L("Connecting"), false
}

func (a *App) remoteAccessRow(c *ui.Context, on bool) {
	desc := L("Off: only this window can connect. Turn it on to pair a phone on the same network.")
	if on {
		desc = L("On: the Gateway also listens on this computer’s network addresses so a paired phone can connect.")
	}
	a.settingRow(c, L("Remote Access"), desc, func() {
		if a.toggle(c, L("Remote Access"), on, false) {
			a.hostUpdate(false, func(s *host.Settings) { s.RemoteAccess = !on })
			if g := a.cfg.Remote; g != nil {
				go g.SetRemoteAccess(!on)
			}
		}
	}, nil)
}

func (a *App) pairingRow(c *ui.Context, st host.Settings) {
	k, t := a.kit, a.kit.T
	r := &a.remote
	desc := L("Turn on Remote Access to pair a phone.")
	if st.RemoteAccess {
		desc = L("Scan the code or open the link on a phone connected to the same network. To keep Droi on an iPhone home screen, add the page to the home screen first, then paste the link once inside the app. Resetting revokes every paired phone.")
		if st.RelayURL != nil {
			desc = L("Scan the code with the Droi app on a phone. On the same network it connects directly, elsewhere through the relay. A phone paired before gets the relay the next time it connects to this computer. Resetting revokes every paired phone.")
		}
	}
	var below func()
	if st.RemoteAccess && a.cfg.Remote != nil {
		info := a.cfg.Remote.Pairing(st.PairingToken)
		below = func() {
			if info.Link == "" {
				k.Text(c, L("No network address found. Connect this computer to a network and try again."), 14, 20).TextColor(t.MutedForeground)
				return
			}
			if r.qrFor != info.Link {
				r.qrFor, r.qr = info.Link, pairingQR(info.Link, 10)
			}
			ui.Row(c).Wrap().Gap(k.Px(16)).AlignItems(ui.Start).Children(func() {
				ui.Box(c).Role(ui.RoleImage).Label(L("Pairing QR code")).Size(k.Px(176), k.Px(176)).Shrink(0).Padding(k.Px(8)).
					Radius(k.Px(10)).Border(1, t.Border).Background(ui.RGBA(255, 255, 255, 1)).Children(func() {
					if r.qr != nil {
						ui.Image(c, r.qr).Fill().Fit(ui.Contain)
					}
				})
				ui.Column(c).Grow(1).Basis(0).MinWidth(k.Px(192)).Gap(k.Px(8)).Children(func() {
					ui.Box(c).Label(L("Pairing link")).Padding(k.Px(8), k.Px(12)).Radius(k.Px(10)).Border(1, t.Border).Background(t.Background).Children(func() {
						k.Text(c, info.Link, 12, 20).Font(k.Mono).Selectable()
					})
					ui.Row(c).Gap(k.Px(8)).Children(func() {
						copied := a.cfg.Now().Sub(r.copiedAt) < 1500*time.Millisecond
						label, icon := L("Copy link"), "copy"
						if copied {
							label, icon = L("Copied"), "check"
						}
						if a.smallButton(c, kit.Outline, label, icon, false).Clicked() {
							c.WriteClipboard(info.Link)
							r.copiedAt = a.cfg.Now()
							c.After(1600 * time.Millisecond)
						}
						if a.smallButton(c, kit.Destructive, L("Reset pairing token"), "refresh-cw", false).Clicked() {
							if err := a.cfg.Host.Settings.ResetPairingToken(); err != nil {
								a.settings.err = err.Error()
							} else {
								go a.cfg.Remote.Revoke(gateway.TokenPairing)
								a.cfg.Host.Changed()
							}
						}
					})
				})
			})
		}
	}
	a.settingRow(c, L("Pair a phone"), desc, nil, below)
}
