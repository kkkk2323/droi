package app

import (
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
)

// accountsState is the Accounts group's: the switch or removal waiting for
// an answer ("switch:<id>", "remove:<id>"), and the add flow's start.
type accountsState struct {
	confirm  string
	starting bool
	err      string
	opened   string
}

// accountMotion is how an account row and what opens under it come and go:
// rare, so they ease in rather than jump.
var accountMotion = ui.ElementTransition{Duration: 180 * time.Millisecond, Enter: &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true}}

// accountsGroup lists the Factory accounts the Daemon can run as (ADR
// 0017): the one in use, which signs in and out, the others to switch to
// or remove, and a way to add one.
func (a *App) accountsGroup(c *ui.Context, login host.LoginState) {
	h := a.cfg.Host
	adding := h.AddingAccount()
	if p := adding.Pending; adding.Status == host.Pending && p != nil && p.VerificationURIComplete != "" && a.accounts.opened != p.UserCode {
		a.accounts.opened = p.UserCode
		c.OpenURL(p.VerificationURIComplete)
	}
	_, _, reported, _, _, _ := a.snapshot()
	working := len(a.activity(reported))
	accounts := h.Accounts()
	signedIn := 0
	for _, acc := range accounts {
		if acc.Login != nil {
			signedIn++
			h.RefreshUsage(acc.ID)
		}
	}
	var rows []func()
	for _, acc := range accounts {
		if acc.Active && acc.Login == nil && login.Status == host.SignedIn {
			acc.Login = login.Account
		}
		rows = append(rows, func() { a.accountRow(c, acc, login, working, signedIn > 1) })
	}
	rows = append(rows, func() { a.addAccountRow(c, adding) })
	a.settingGroup(c, L("Accounts"), L("Signing out of the droid CLI's account signs the CLI out too. Added accounts keep their own login and share skills, droids, MCP servers, settings and sessions. Switching restarts the Daemon."), rows...)
}

// accountName is what an account row says: who, then where it comes from.
func accountName(acc host.AccountInfo) (name, detail string) {
	var parts []string
	if acc.ID == "" {
		parts = append(parts, L("droid CLI"))
	}
	name = L("Signed out")
	if l := acc.Login; l != nil {
		name = l.UserID
		if l.Email != nil {
			name = *l.Email
		}
		if l.OrgID != nil {
			parts = append(parts, *l.OrgID)
		}
	}
	return name, strings.Join(parts, " · ")
}

func (a *App) accountRow(c *ui.Context, acc host.AccountInfo, login host.LoginState, working int, others bool) {
	h := a.cfg.Host
	k, t := a.kit, a.kit.T
	s := &a.accounts
	name, detail := accountName(acc)
	pending := acc.Active && login.Status == host.Pending && login.Pending != nil
	ui.Column(c).Key("account:"+acc.ID).Role(ui.RoleGroup).Label(name).Transition(accountMotion).Padding(k.Px(12), k.Px(16)).Children(func() {
		ui.Row(c).Gap(k.Px(12)).AlignItems(ui.Center).Children(func() {
			initial := ""
			if acc.Login != nil {
				r, _ := utf8.DecodeRuneInString(name)
				initial = string(unicode.ToUpper(r))
			}
			a.accountMark(c, initial, "user-round")
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
				color := t.Foreground
				if acc.Login == nil {
					color = t.MutedForeground
				}
				k.Text(c, name, 14, 20).Role(ui.RoleHeading).FontWeight(500).TextColor(color).SingleLine()
				if pending {
					detail = L("Your browser opened Factory. Enter this code there if it asks for one.")
				}
				if detail != "" {
					k.Text(c, detail, 12, 16).TextColor(t.MutedForeground)
				}
				if acc.Login != nil && !pending {
					usage := h.AccountUsage(acc.ID)
					a.usageMeters(c, usage)
					if acc.Active && others && nearLimit(usage) {
						k.Text(c, L("This account is close to its limit. Switch to one with room left."), 12, 16).Role(ui.RoleStatus).TextColor(t.Attention).Margin(k.Px(4), 0, 0, 0)
					}
				}
			})
			ui.Row(c).Gap(k.Px(8)).Shrink(0).AlignItems(ui.Center).Children(func() {
				if acc.Active {
					a.activeAccountControls(c, acc, login)
					return
				}
				if a.smallButton(c, kit.Outline, L("Switch"), "", false).Label(L("Switch to %s", name)).Clicked() {
					if working > 0 {
						s.confirm = "switch:" + acc.ID
					} else {
						a.switchAccount(acc.ID)
					}
				}
				if acc.ID != "" && k.IconButton(c, "trash", L("Remove %s", name), 32).Clicked() {
					s.confirm = "remove:" + acc.ID
				}
			})
		})
		if pending {
			a.signInCode(c, login.Pending)
		}
		if acc.Active {
			msg := a.signIn.err
			if msg == "" && login.Status == host.SignedOut {
				msg = login.Error
			}
			a.accountError(c, msg)
		}
		switch s.confirm {
		case "switch:" + acc.ID:
			msg := L("%d sessions are working. Switching restarts the Daemon and stops them.", working)
			if working == 1 {
				msg = L("A session is working. Switching restarts the Daemon and stops it.")
			}
			a.accountQuestion(c, msg, L("Switch anyway"), kit.Primary, func() { a.switchAccount(acc.ID) })
		case "remove:" + acc.ID:
			a.accountQuestion(c, L("Droi deletes this account's login from this computer. Skills, settings and sessions stay."), L("Remove"), kit.Destructive, func() {
				if err := h.RemoveAccount(acc.ID); err != nil {
					a.settings.err = err.Error()
				}
			})
		}
	})
}

// activeAccountControls are the account in use's: that it is in use, and
// signing it out, in, or back out of a sign-in under way.
func (a *App) activeAccountControls(c *ui.Context, acc host.AccountInfo, login host.LoginState) {
	h := a.cfg.Host
	a.statusPill(c, login.Status == host.SignedIn, L("In use"))
	switch login.Status {
	case host.SignedIn:
		if a.smallButton(c, kit.Outline, L("Sign out"), "log-out", false).Clicked() {
			if err := h.SignOut(); err != nil {
				a.settings.err = err.Error()
			}
		}
	case host.Pending:
		if a.smallButton(c, kit.Ghost, L("Cancel"), "", false).Clicked() {
			h.Auth.CancelSignIn()
		}
	default:
		label := L("Sign in")
		if a.signIn.starting {
			label = L("Opening Factory…")
		}
		if a.smallButton(c, kit.Primary, label, "", a.signIn.starting).Clicked() {
			a.startSignIn()
		}
	}
}

// usageMeters is an account's Standard usage under its name: the 5-hour,
// weekly and monthly windows as short bars, colored once they run high.
func (a *App) usageMeters(c *ui.Context, st host.UsageState) {
	k, t := a.kit, a.kit.T
	now := time.Now()
	u := st.Usage
	if u == nil {
		msg := L("Checking usage…")
		if st.Err != "" {
			msg = L("Usage unavailable: %s", st.Err)
		}
		k.Text(c, msg, 12, 16).Role(ui.RoleStatus).TextColor(t.MutedForeground).Margin(k.Px(6), 0, 0, 0)
		return
	}
	ui.Row(c).Role(ui.RoleGroup).Label(L("Usage")).Wrap().GapX(k.Px(14)).GapY(k.Px(4)).AlignItems(ui.Center).Margin(k.Px(6), 0, 0, 0).Children(func() {
		for _, m := range []struct {
			key, label string
			w          host.UsageWindow
		}{{"5h", L("5h"), u.Standard.FiveHour}, {"week", L("Week"), u.Standard.Weekly}, {"month", L("Month"), u.Standard.Monthly}} {
			pct := m.w.Percent
			if !m.w.Active(now) {
				pct = 0
			}
			color := t.Foreground.Alpha(0.55)
			switch {
			case pct >= 100:
				color = t.Destructive
			case pct >= 80:
				color = t.Attention
			}
			ui.Row(c).Key(m.key).Gap(k.Px(6)).AlignItems(ui.Center).Label(L("%s: %d%% used", m.label, int(pct))).Children(func() {
				k.Text(c, m.label, 11, 16).TextColor(t.MutedForeground)
				track := ui.Box(c).Width(k.Px(40)).Height(k.Px(4)).Radius(k.Px(2)).Background(t.Border).ClipX()
				track.Children(func() {
					fill := ui.Box(c).Height(k.Px(4)).Radius(k.Px(2)).Background(color)
					fill.Width(k.Px(40 * min(fill.Animate("fill", float32(pct), 240*time.Millisecond), 100) / 100))
				})
				pctColor := t.MutedForeground
				if pct >= 80 {
					pctColor = color
				}
				k.Text(c, strconv.Itoa(int(pct))+"%", 12, 16).Font(k.Mono).TextColor(pctColor).MinWidth(k.Px(30))
			})
		}
	})
	if w, ok := u.Standard.Highest(now); ok && w.Percent >= 100 {
		k.Text(c, L("Standard usage is used up. It resets %s.", resetsIn(w.Ends.Sub(now))), 12, 16).Role(ui.RoleStatus).TextColor(t.Destructive).Margin(k.Px(4), 0, 0, 0)
	}
}

// resetsIn says when a window ends, as droid does: hours and minutes
// within a day, else the date.
func resetsIn(d time.Duration) string {
	if d >= 24*time.Hour {
		return L("on %s", time.Now().Add(d).Format(L("Jan 2")))
	}
	h, m := int(d.Hours()), int(d.Minutes())%60
	if h > 0 {
		return L("in %dh %dm", h, m)
	}
	return L("in %dm", m)
}

// nearLimit reports that the account's Standard usage is at 90% or more in
// a running window.
func nearLimit(st host.UsageState) bool {
	if st.Usage == nil {
		return false
	}
	w, ok := st.Usage.Standard.Highest(time.Now())
	return ok && w.Percent >= 90
}

// accountMark is a row's 28 px disc: the account's initial, else an icon.
func (a *App) accountMark(c *ui.Context, initial, icon string) {
	k, t := a.kit, a.kit.T
	ui.Box(c).Size(k.Px(28), k.Px(28)).Shrink(0).Radius(k.Px(14)).Border(1, t.Border).Background(t.Background).Center().Children(func() {
		if initial != "" {
			k.Text(c, initial, 12, 16).FontWeight(600).TextColor(t.Foreground)
		} else {
			k.Icon(c, icon, 14, t.MutedForeground)
		}
	})
}

// signInCode is the device flow's code under a row, lined up with its text.
func (a *App) signInCode(c *ui.Context, p *host.LoginPending) {
	k, t := a.kit, a.kit.T
	ui.Row(c).Key("code").Transition(accountMotion).Wrap().Gap(k.Px(12)).AlignItems(ui.Center).Margin(k.Px(10), 0, 0, k.Px(40)).Children(func() {
		k.Text(c, p.UserCode, 16, 24).Font(k.Mono).LetterSpacing(k.Px(3.2)).Label(L("Sign-in code")).
			Padding(k.Px(4), k.Px(10)).Radius(k.Px(8)).Border(1, t.Border).Background(t.Background)
		ui.Row(c).Gap(k.Px(6)).AlignItems(ui.Center).Children(func() {
			kit.Dot(c, k.Px(6), t.MutedForeground.Alpha(0.4+0.3*float32(pulseWave(c))))
			k.Text(c, L("Waiting for the browser"), 12, 16).TextColor(t.MutedForeground)
		})
		if p.VerificationURIComplete != "" {
			ui.Link(c, L("Open the sign-in page again"), p.VerificationURIComplete).FontSize(k.Px(12)).Underline()
		}
	})
}

func (a *App) accountError(c *ui.Context, msg string) {
	if msg == "" {
		return
	}
	k, t := a.kit, a.kit.T
	k.Text(c, msg, 13, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground).Margin(k.Px(10), 0, 0, k.Px(40))
}

// accountQuestion asks under a row before a switch or removal goes ahead,
// lined up with the row's text.
func (a *App) accountQuestion(c *ui.Context, text, action string, v kit.Variant, do func()) {
	k, t := a.kit, a.kit.T
	s := &a.accounts
	ui.Row(c).Key("question").Transition(accountMotion).Wrap().GapX(k.Px(16)).GapY(k.Px(8)).AlignItems(ui.Center).Margin(k.Px(10), 0, 0, k.Px(40)).Children(func() {
		k.Text(c, text, 13, 20).Role(ui.RoleStatus).TextColor(t.MutedForeground).Grow(1).Basis(0).MinWidth(k.Px(192))
		ui.Row(c).Gap(k.Px(8)).Shrink(0).Children(func() {
			if a.smallButton(c, kit.Ghost, L("Cancel"), "", false).Clicked() {
				s.confirm = ""
			}
			if a.smallButton(c, v, action, "", false).Clicked() {
				s.confirm = ""
				do()
			}
		})
	})
}

func (a *App) switchAccount(id string) {
	if err := a.cfg.Host.SwitchAccount(id); err != nil {
		a.settings.err = err.Error()
	}
}

// addAccountRow adds an account with the device flow, as signing in does,
// and keeps the code in the row while the browser is out.
func (a *App) addAccountRow(c *ui.Context, adding host.LoginState) {
	h := a.cfg.Host
	k, t := a.kit, a.kit.T
	s := &a.accounts
	pending := adding.Status == host.Pending && adding.Pending != nil
	ui.Column(c).Key("account:add").Padding(k.Px(12), k.Px(16)).Children(func() {
		ui.Row(c).Gap(k.Px(12)).AlignItems(ui.Center).Children(func() {
			a.accountMark(c, "", "plus")
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
				title, desc := L("Add an account"), L("Sign in with another Factory account in the browser.")
				if pending {
					title, desc = L("Finish signing in"), L("Your browser opened Factory. Sign in there with the account to add.")
				}
				k.Text(c, title, 14, 20).Role(ui.RoleHeading).FontWeight(500)
				k.Text(c, desc, 12, 16).TextColor(t.MutedForeground)
			})
			if pending {
				if a.smallButton(c, kit.Ghost, L("Cancel"), "", false).Label(L("Cancel adding")).Clicked() {
					h.CancelAddAccount()
				}
				return
			}
			label := L("Add account")
			if s.starting {
				label = L("Opening Factory…")
			}
			if a.smallButton(c, kit.Outline, label, "", s.starting).Clicked() {
				a.startAddAccount()
			}
		})
		if pending {
			a.signInCode(c, adding.Pending)
			return
		}
		msg := s.err
		if msg == "" {
			msg = adding.Error
		}
		a.accountError(c, msg)
	})
}

func (a *App) startAddAccount() {
	s := &a.accounts
	s.starting, s.err = true, ""
	go func() {
		_, err := a.cfg.Host.AddAccount(a.ctx)
		a.cfg.Update(func() {
			s.starting = false
			if err != nil {
				s.err = err.Error()
			}
		})
	}()
}
