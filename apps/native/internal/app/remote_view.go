package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/gateway"
	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
)

// Remote is the Gateway that lets a paired phone reach the Daemon.
type Remote interface {
	SetRemoteAccess(on bool)
	Pairing(pairingHost, token string) gateway.PairingInfo
	// Revoke ends the paired phones' connections.
	Revoke(kind gateway.TokenKind)
}

// remoteState is what Settings → Remote Access keeps between frames.
type remoteState struct {
	pairingHost string
	loaded      bool
	copiedAt    time.Time
	qrFor       string
	qr          *ui.Bitmap
}

func (a *App) remoteTab(c *ui.Context) {
	h := a.cfg.Host
	st := h.Settings.Get()
	r := &a.remote
	if !r.loaded {
		r.pairingHost, r.loaded = derefOr(st.PairingHost), true
	}
	a.settingGroup(c, "", "",
		func() { a.remoteAccessRow(c, st.RemoteAccess) },
		func() { a.pairingRow(c, st) },
	)
	a.settingGroup(c, L("Away from this network"), "", func() { a.pairingHostRow(c, st) })
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
	}
	var below func()
	if st.RemoteAccess && a.cfg.Remote != nil {
		info := a.cfg.Remote.Pairing(derefOr(st.PairingHost), st.PairingToken)
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

func (a *App) pairingHostRow(c *ui.Context, st host.Settings) {
	r := &a.remote
	desc := L("Leave empty to use this computer’s network address. Set a host name that also reaches it away from home, such as a Tailscale or Surge Ponte name, and phones pair with that.")
	if st.PairingHost != nil {
		desc = L("The pairing link uses %s instead of this computer’s network address.", *st.PairingHost)
	}
	a.settingRow(c, L("Pairing address"), desc, nil, func() {
		ui.Row(c).Gap(a.kit.Px(8)).Children(func() {
			in := a.field(c, &r.pairingHost, L("Pairing address"), "laptop.myhome", true, false)
			save := a.smallButton(c, kit.Outline, L("Save"), "", false).Label(L("Save address")).Height(a.kit.Px(36)).Clicked()
			if save || in.Submitted() {
				h := host.PairingHostOf(r.pairingHost)
				a.hostUpdate(false, func(s *host.Settings) { s.PairingHost = h })
				r.pairingHost = derefOr(h)
			}
		})
	})
}
