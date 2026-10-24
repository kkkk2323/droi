package app

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/loading"
	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/dashkit"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
)

// accountsState is the Accounts group's: the switch or removal waiting for
// an answer ("switch:<id>", "remove:<id>"), and the add flow's start.
type accountsState struct {
	confirm  string
	starting bool
	err      string
	opened   string
	// details are the accounts whose usage shows each window in full.
	details map[string]bool
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
	for i, acc := range accounts {
		if acc.Active && acc.Login == nil && login.Status == host.SignedIn {
			accounts[i].Login = login.Account
		}
	}
	var rows []func()
	for _, acc := range accounts {
		rows = append(rows, func() { a.accountRow(c, acc, accounts, login, working, signedIn > 1) })
	}
	rows = append(rows, func() { a.addAccountRow(c, adding) })
	a.settingGroup(c, L("Accounts"), L("Signing out of the account shared with droid in the terminal signs droid out there too. All accounts share skills, droids, MCP servers, settings and sessions. Switching restarts the Daemon."), rows...)
}

// accountWho is who an account is signed in as.
func accountWho(acc host.AccountInfo) string {
	l := acc.Login
	if l == nil {
		return L("Signed out")
	}
	if l.Email != nil {
		return *l.Email
	}
	return l.UserID
}

// accountName is what an account row says: who, then only what tells it
// apart from the others: that it shares its login with droid in the
// terminal, and its organization when another account has the same name.
func accountName(acc host.AccountInfo, accounts []host.AccountInfo) (name, detail string) {
	name = accountWho(acc)
	var parts []string
	if acc.ID == "" && len(accounts) > 1 {
		parts = append(parts, L("Shared with droid in the terminal"))
	}
	if l := acc.Login; l != nil && l.OrgID != nil {
		for _, o := range accounts {
			if o.ID != acc.ID && o.Login != nil && accountWho(o) == name {
				parts = append(parts, *l.OrgID)
				break
			}
		}
	}
	return name, strings.Join(parts, " · ")
}

func (a *App) accountRow(c *ui.Context, acc host.AccountInfo, accounts []host.AccountInfo, login host.LoginState, working int, others bool) {
	h := a.cfg.Host
	k, t := a.kit, a.kit.T
	s := &a.accounts
	name, detail := accountName(acc, accounts)
	pending := acc.Active && login.Status == host.Pending && login.Pending != nil
	var usage host.UsageState
	if acc.Login != nil {
		usage = h.AccountUsage(acc.ID)
	}
	ui.Column(c.Key("account:"+acc.ID)).Role(ui.RoleGroup).Label(name).Transition(accountMotion).Padding(k.Px(12), k.Px(16)).Children(func() {
		ui.Row(c).Gap(k.Px(12)).AlignItems(ui.Center).Children(func() {
			initial := ""
			if acc.Login != nil {
				initial = accountInitial(name)
			}
			a.accountMark(c, initial, "user-round")
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
				color := t.Foreground
				if acc.Login == nil {
					color = t.MutedForeground
				}
				ui.Row(c).Gap(k.Px(6)).AlignItems(ui.Center).MinWidth(0).Children(func() {
					k.Text(c, name, 14, 20).Role(ui.RoleHeading).FontWeight(500).TextColor(color).SingleLine().Shrink(1).MinWidth(0)
					if plan := planName(usage.Subscription); plan != "" {
						a.planBadge(c, plan)
					}
				})
				if pending {
					detail = L("Your browser opened Factory. Enter this code there if it asks for one.")
				}
				if detail != "" {
					k.Text(c, detail, 12, 16).TextColor(t.MutedForeground)
				}
				if acc.Login != nil && !pending {
					a.subscriptionLine(c, usage.Subscription)
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
		if acc.Login != nil && !pending {
			// Under the whole row, lined up with its text, so the meters have
			// the card's width rather than what the buttons leave.
			ui.Column(c).Gap(k.Px(10)).Margin(k.Px(10), 0, k.Px(2), k.Px(40)).Children(func() {
				a.usageMeters(c, acc.ID, name, usage)
				if acc.Active && others && nearLimit(usage) {
					k.Text(c, L("This account is close to its limit. Switch to one with room left."), 12, 16).Role(ui.RoleStatus).TextColor(t.Attention)
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

// usageWindowView is a window of a pool with its name.
type usageWindowView struct {
	key, label string
	w          host.UsageWindow
}

// usageWindows are a pool's windows, shortest first.
func usageWindows(p host.UsagePool) []usageWindowView {
	return []usageWindowView{{"5h", L("5h"), p.FiveHour}, {"week", L("Week"), p.Weekly}, {"month", L("Month"), p.Monthly}}
}

// usagePoolView is a billing pool as the Account tab shows it.
type usagePoolView struct {
	key, name string
	pool      host.UsagePool
}

// windowName is what a window is called on its own: Core's carry the
// pool's name, Standard's are the account's usage as droid shows it.
func (p usagePoolView) windowName(w usageWindowView) string {
	if p.key == "core" {
		return p.name + " " + w.label
	}
	return w.label
}

// drawsOn reports that a pool is in use: one of its windows runs with some
// of it used.
func drawsOn(p host.UsagePool, now time.Time) bool {
	for _, w := range usageWindows(p) {
		if w.w.Active(now) && w.w.Percent > 0 {
			return true
		}
	}
	return false
}

// usageLeft is the share of a window left and when it resets. A window
// that is not running has all of it.
func usageLeft(w host.UsageWindow, now time.Time) (left int, reset string) {
	if !w.Active(now) {
		return 100, L("Not started")
	}
	return max(100-int(math.Ceil(w.Percent)), 0), resetText(w.Ends.Sub(now), now)
}

// usageColors are a window's bar or ring and its share's text: quiet until
// a fifth is left, then the attention color, red once none is.
func (a *App) usageColors(left int) (fill, text ui.Color) {
	t := a.kit.T
	switch {
	case left <= 0:
		return t.Destructive, t.Destructive
	case left <= 20:
		return t.Attention, t.Attention
	}
	return t.Foreground.Alpha(0.55), t.Foreground
}

// usageMeters is an account's usage under its row: a ring per window of
// Standard, and of Core once it is in use, and on "Details" each window's
// share left and when it resets.
func (a *App) usageMeters(c *ui.Context, id, name string, st host.UsageState) {
	k, t := a.kit, a.kit.T
	s := &a.accounts
	now := time.Now()
	u := st.Usage
	if u == nil {
		msg := L("Checking usage…")
		if st.Err != "" {
			msg = L("Usage unavailable: %s", st.Err)
		}
		k.Text(c, msg, 12, 16).Role(ui.RoleStatus).TextColor(t.MutedForeground)
		return
	}
	pools := []usagePoolView{{"standard", L("Standard"), u.Standard}}
	if drawsOn(u.Core, now) {
		pools = append(pools, usagePoolView{"core", L("Core"), u.Core})
	}
	open := s.details[id]
	ui.Row(c).Gap(k.Px(12)).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Role(ui.RoleGroup).Label(L("Usage")).Grow(1).Basis(0).MinWidth(0).Gap(k.Px(6)).Children(func() {
			for _, p := range pools {
				ui.Row(c.Key("rings:" + p.key)).Wrap().GapX(k.Px(12)).GapY(k.Px(4)).MinHeight(k.Px(24)).AlignItems(ui.Center).Children(func() {
					if len(pools) > 1 {
						k.Text(c, p.name, 12, 16).FontWeight(500).Width(k.Px(64)).Shrink(0).SingleLine()
					}
					for _, w := range usageWindows(p.pool) {
						a.usageRing(c, w.key, w.label, p.windowName(w), w.w, now)
					}
				})
			}
		})
		b := ui.ButtonBase(c).Label(L("Usage details for %s", name)).Expanded(open).Height(k.Px(24)).Gap(k.Px(4)).PaddingX(k.Px(6)).
			Radius(k.Px(6)).Shrink(0).Cursor(ui.CursorPointer)
		color := t.MutedForeground
		if b.Hovered() {
			b.Background(t.Muted)
			color = t.Foreground
		}
		b.Children(func() {
			k.Text(c, L("Details"), 12, 16).TextColor(color)
			rot := float32(0)
			if open {
				rot = 180
			}
			k.Icon(c, "chevron-down", 12, color).Rotate(rot)
		})
		if b.Clicked() {
			if s.details == nil {
				s.details = map[string]bool{}
			}
			s.details[id] = !open
		}
	})
	if open {
		ui.Column(c.Key("details")).Transition(accountMotion).Gap(k.Px(10)).Children(func() {
			for _, p := range pools {
				ui.Column(c.Key("details:" + p.key)).Gap(k.Px(10)).Children(func() {
					if len(pools) > 1 {
						k.Text(c, p.name, 11, 16).FontWeight(500).TextColor(t.MutedForeground)
					}
					for _, w := range usageWindows(p.pool) {
						a.usageMeter(c, w.key, w.label, p.windowName(w), w.w, now)
					}
				})
			}
		})
	}
	if w, ok := u.Standard.Highest(now); ok && w.Percent >= 100 {
		k.Text(c, L("Standard usage is used up. It resets %s.", resetsIn(w.Ends.Sub(now))), 12, 16).Role(ui.RoleStatus).TextColor(t.Destructive)
	}
}

// usageRing is a window in brief: a ring of what is left, its name and
// the share left; its tooltip says when it resets.
func (a *App) usageRing(c *ui.Context, key, label, name string, w host.UsageWindow, now time.Time) {
	k, t := a.kit, a.kit.T
	left, reset := usageLeft(w, now)
	fill, pct := a.usageColors(left)
	ui.Row(c.Key(key)).Role(ui.RoleMeter).Label(L("%s: %d%% left", name, left)).Tooltip(reset).Gap(k.Px(6)).MinWidth(k.Px(108)).AlignItems(ui.Center).Children(func() {
		ring := ui.Box(c).Size(k.Px(16), k.Px(16)).Shrink(0)
		ratio := float64(ring.Animate("ring", float32(left), 240*time.Millisecond)) / 100
		ring.Draw(func(p *ui.Painter, r ui.Rect) {
			s := r.W / 16
			dashkit.DrawRing(p, r.X+8*s, r.Y+8*s, 6.5*s, 2.5*s, ratio, t.Foreground.Alpha(0.1), fill)
		})
		k.Text(c, label, 12, 16).TextColor(t.MutedForeground)
		k.Text(c, strconv.Itoa(left)+"%", 12, 16).FontWeight(500).TextColor(pct).FontFeatures("tnum")
	})
}

// usageMeter is one window in full, as Synara's usage popovers draw it: its
// name and the share left, when it resets at the end of the line, and a
// thin bar of what is left under them.
func (a *App) usageMeter(c *ui.Context, key, label, name string, w host.UsageWindow, now time.Time) {
	k, t := a.kit, a.kit.T
	left, reset := usageLeft(w, now)
	color, pctColor := a.usageColors(left)
	ui.Column(c.Key(key)).Role(ui.RoleGroup).Label(L("%s: %d%% left", name, left)).Gap(k.Px(4)).Children(func() {
		ui.Row(c).Gap(k.Px(6)).AlignItems(ui.Center).Children(func() {
			k.Text(c, label, 12, 16).FontWeight(500).Shrink(0)
			k.Text(c, L("%d%% left", left), 12, 16).TextColor(pctColor).FontFeatures("tnum").Grow(1).MinWidth(0).SingleLine()
			k.Text(c, reset, 12, 16).TextColor(t.MutedForeground).FontFeatures("tnum").Shrink(0)
		})
		ui.Box(c).FillWidth().Height(k.Px(4)).Radius(k.Px(2)).Background(t.Foreground.Alpha(0.08)).Clip().Children(func() {
			fill := ui.Box(c).FillHeight().Radius(k.Px(2)).Background(color)
			fill.WidthPercent(min(fill.Animate("fill", float32(left), 240*time.Millisecond), 100))
		})
	})
}

// resetText says when a window resets: hours and minutes within a day,
// else the days left and the date.
func resetText(d time.Duration, now time.Time) string {
	h, m := int(d.Hours()), int(d.Minutes())%60
	switch {
	case h >= 48:
		return L("Resets in %d days (%s)", h/24, now.Add(d).Format(L("Jan 2")))
	case h >= 24:
		return L("Resets in 1 day (%s)", now.Add(d).Format(L("Jan 2")))
	case h > 0:
		return L("Resets in %dh %dm", h, m)
	case m > 0:
		return L("Resets in %dm", m)
	}
	return L("Resets soon")
}

// planName is a plan's short name, "Pro" for "Factory Pro Plan".
func planName(s *host.Subscription) string {
	if s == nil {
		return ""
	}
	words := strings.Fields(s.Plan)
	if n := len(words); n > 2 && words[0] == "Factory" && words[n-1] == "Plan" {
		return strings.Join(words[1:n-1], " ")
	}
	return strings.Join(words, " ")
}

// planBadge is the plan's name beside the account's.
func (a *App) planBadge(c *ui.Context, plan string) {
	k, t := a.kit, a.kit.T
	ui.Row(c).Height(k.Px(18)).Shrink(0).PaddingX(k.Px(6)).Radius(k.Px(9)).Border(1, t.Border).Background(t.Background).AlignItems(ui.Center).Children(func() {
		k.Text(c, plan, 11, 14).FontWeight(500).TextColor(t.MutedForeground).SingleLine()
	})
}

// subscriptionLine is said only when the plan needs the user: it ends or
// its trial ends within a week, or it has ended.
func (a *App) subscriptionLine(c *ui.Context, s *host.Subscription) {
	if s == nil {
		return
	}
	k, t := a.kit, a.kit.T
	line, rest, level := subscriptionText(s, time.Now())
	if line == "" {
		return
	}
	label := line
	if rest != "" {
		label += " · " + rest
	}
	ui.Row(c.Key("plan")).Role(ui.RoleStatus).Label(label).Wrap().GapX(k.Px(4)).AlignItems(ui.Center).Margin(k.Px(4), 0, 0, 0).Children(func() {
		color := t.MutedForeground
		if level == planEnded {
			color = t.Destructive
		}
		k.Text(c, line, 12, 16).TextColor(color)
		if rest != "" {
			k.Text(c, "·", 12, 16).TextColor(t.MutedForeground)
			k.Text(c, rest, 12, 16).FontWeight(500).TextColor(t.Attention)
		}
	})
}

type planLevel int

const (
	planFine planLevel = iota
	planEnding
	planEnded
)

// subscriptionText is when the plan or its trial ends, with rest, how many
// days are left; "" while it renews by itself or ends more than a week on.
func subscriptionText(s *host.Subscription, now time.Time) (line, rest string, level planLevel) {
	date := func(t time.Time) string { return t.Local().Format(L("Jan 2, 2006")) }
	left := func(end time.Time) (string, planLevel) {
		d := end.Sub(now)
		if d > 7*24*time.Hour {
			return "", planFine
		}
		if days := int(math.Ceil(d.Hours() / 24)); days > 1 {
			return L("%d days left", days), planEnding
		}
		return L("1 day left"), planEnding
	}
	switch {
	case s.Status == "ended" || (!s.Ends.IsZero() && !s.Ends.After(now)):
		if s.Ends.IsZero() {
			return L("Ended"), "", planEnded
		}
		return L("Ended %s", date(s.Ends)), "", planEnded
	case !s.Ends.IsZero():
		if rest, level = left(s.Ends); level == planEnding {
			return L("Ends %s", date(s.Ends)), rest, level
		}
	case !s.TrialEnds.IsZero() && s.TrialEnds.After(now):
		if rest, level = left(s.TrialEnds); level == planEnding {
			return L("Trial ends %s", date(s.TrialEnds)), rest, level
		}
	}
	return "", "", planFine
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
	ui.Row(c.Key("code")).Transition(accountMotion).Wrap().Gap(k.Px(12)).AlignItems(ui.Center).Margin(k.Px(10), 0, 0, k.Px(40)).Children(func() {
		k.Text(c, p.UserCode, 16, 24).Font(k.Mono).LetterSpacing(k.Px(3.2)).Label(L("Sign-in code")).
			Padding(k.Px(4), k.Px(10)).Radius(k.Px(8)).Border(1, t.Border).Background(t.Background)
		ui.Row(c).Gap(k.Px(6)).AlignItems(ui.Center).Children(func() {
			loading.Pulse(c, loading.Options{Size: k.Px(14), Color: t.MutedForeground})
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
	ui.Row(c.Key("question")).Transition(accountMotion).Wrap().GapX(k.Px(16)).GapY(k.Px(8)).AlignItems(ui.Center).Margin(k.Px(10), 0, 0, k.Px(40)).Children(func() {
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
	ui.Column(c.Key("account:add")).Padding(k.Px(12), k.Px(16)).Children(func() {
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
