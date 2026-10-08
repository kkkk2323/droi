package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
)

// signInState is the sign-in page's: the device flow being started, why it
// could not be, and the code whose page the browser was sent to.
type signInState struct {
	starting bool
	err      string
	opened   string
}

// startSignIn starts the device flow off the UI thread; the page shows the
// code once the Host has one.
func (a *App) startSignIn() {
	s := &a.signIn
	s.starting, s.err = true, ""
	go func() {
		_, err := a.cfg.Host.Auth.SignIn(a.ctx)
		a.cfg.Update(func() {
			s.starting = false
			if err != nil {
				s.err = err.Error()
			}
		})
	}()
}

// openSignInPage sends the browser to the device flow's page, once per
// code; L("Open the sign-in page again") reopens it.
func (a *App) openSignInPage(c *ui.Context, login host.LoginState) {
	p := login.Pending
	if login.Status != host.Pending || p == nil || p.VerificationURIComplete == "" || a.signIn.opened == p.UserCode {
		return
	}
	a.signIn.opened = p.UserCode
	c.OpenURL(p.VerificationURIComplete)
}

// needsSignIn: the Host has nothing to authenticate with, so every
// daemon.authenticate would fail and nothing but signing in can work.
func (a *App) needsSignIn() bool {
	h := a.cfg.Host
	return h != nil && !h.HasCredential()
}

// signInPage fills the window until Droi can authenticate: sign in with
// Factory, or use an API key. Settings stays reachable for what the Daemon
// needs before it can start (the droid path, the API base URL).
func (a *App) signInPage(c *ui.Context) {
	h := a.cfg.Host
	k, t := a.kit, a.kit.T
	login := h.LoginState()
	ui.Column(c).Role(ui.RoleGroup).Label(L("Sign in")).Fill().Background(t.Background).TextColor(t.Foreground).Children(func() {
		ui.Row(c).Height(k.Px(44)).Shrink(0).DragWindow()
		ui.Column(c).Grow(1).MinHeight(0).ClipY().Children(func() {
			ui.Column(c).Fill().Center().PaddingX(k.Px(24)).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(k.Px(340)).Gap(k.Px(24)).Children(func() {
					ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(k.Px(8)).Children(func() {
						droiMark(c, k, 40)
						k.Text(c, L("Sign in to Droi"), 20, 28).Role(ui.RoleHeading).FontWeight(500).LetterSpacing(-k.Px(0.5)).Margin(k.Px(12), 0, 0, 0)
						k.Text(c, L("Droid runs on this computer with your Factory account. Sessions and settings stay here."), 14, 20).
							TextColor(t.MutedForeground).TextAlign(ui.Center)
					})
					a.openSignInPage(c, login)
					if login.Status == host.Pending && login.Pending != nil {
						a.signInPending(c, login.Pending)
					} else {
						a.signInChoices(c, login)
					}
				})
			})
		})
		ui.Row(c).Shrink(0).Justify(ui.Center).Padding(k.Px(8), k.Px(16), k.Px(16), k.Px(16)).Children(func() {
			if a.smallButton(c, kit.Ghost, L("Settings"), "settings", false).Clicked() {
				a.Go(Route{Name: "settings", Tab: "advanced"})
			}
		})
	})
}

// signInChoices are the two ways in: the device flow first, an API key
// for automation and for accounts without a browser sign-in.
func (a *App) signInChoices(c *ui.Context, login host.LoginState) {
	k, t := a.kit, a.kit.T
	s := &a.settings
	ui.Column(c).FillWidth().Gap(k.Px(8)).Children(func() {
		label := L("Sign in with Factory")
		if a.signIn.starting {
			label = L("Opening Factory…")
		}
		if a.wideButton(c, kit.Primary, label, a.signIn.starting).Clicked() {
			a.startSignIn()
		}
		msg := a.signIn.err
		if msg == "" {
			msg = login.Error
		}
		if msg != "" {
			k.Text(c, msg, 13, 18).Role(ui.RoleStatus).TextColor(t.DestructiveForeground).TextAlign(ui.Center)
		}
	})
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(k.Px(12)).Children(func() {
		ui.Box(c).Height(1).Grow(1).Background(t.Border)
		k.Text(c, L("or"), 12, 16).TextColor(t.MutedForeground)
		ui.Box(c).Height(1).Grow(1).Background(t.Border)
	})
	ui.Column(c).FillWidth().Gap(k.Px(8)).Children(func() {
		key := strings.TrimSpace(s.apiKey)
		save := func() {
			a.hostUpdate(true, func(st *host.Settings) { st.APIKey = host.OptString(key) })
			s.apiKey = ""
		}
		// In a row: the field grows along it, as in Settings.
		ui.Row(c).FillWidth().Children(func() {
			if a.field(c, &s.apiKey, L("Factory API key"), L("Factory API key (fk-…)"), false, true).Submitted() && key != "" {
				save()
			}
		})
		if a.wideButton(c, kit.Outline, L("Continue with API key"), key == "").Clicked() {
			save()
		}
	})
	k.Text(c, L("Using the droid CLI? Sign in there with /login, and Droi uses the same login."), 12, 16).
		TextColor(t.MutedForeground).TextAlign(ui.Center)
}

// signInPending is the device flow under way: the code the browser asks
// for, the page again, and a way back.
func (a *App) signInPending(c *ui.Context, p *host.LoginPending) {
	h := a.cfg.Host
	k, t := a.kit, a.kit.T
	ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(k.Px(12)).Children(func() {
		k.Text(c, L("Your browser opened Factory. Enter this code there if it asks for one."), 14, 20).TextAlign(ui.Center)
		k.Text(c, p.UserCode, 20, 28).Font(k.Mono).LetterSpacing(k.Px(4)).Label(L("Sign-in code")).
			Padding(k.Px(10), k.Px(20)).Radius(k.Px(10)).Border(1, t.Border).Background(t.Card)
		ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
			kit.Dot(c, k.Px(6), t.MutedForeground.Alpha(0.4+0.3*float32(pulseWave(c))))
			k.Text(c, L("Waiting for the browser"), 13, 18).TextColor(t.MutedForeground)
		})
		if p.VerificationURIComplete != "" {
			ui.Link(c, L("Open the sign-in page again"), p.VerificationURIComplete).FontSize(k.Px(13)).Underline()
		}
	})
	ui.Column(c).FillWidth().Gap(k.Px(8)).Children(func() {
		if a.wideButton(c, kit.Ghost, L("Cancel"), false).Clicked() {
			h.Auth.CancelSignIn()
		}
	})
}

// wideButton is a full-width h-9 button of the sign-in page.
func (a *App) wideButton(c *ui.Context, v kit.Variant, label string, disabled bool) ui.Element {
	k, t := a.kit, a.kit.T
	b := k.Button(c, v, 36, false, label).FillWidth().Disabled(disabled)
	color := t.Foreground
	if v == kit.Primary {
		color = t.PrimaryForeground
	}
	b.Children(func() { k.Text(c, label, 14, 20).FontWeight(500).TextColor(color) })
	return b
}
