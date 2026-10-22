package app

import (
	"fmt"
	"slices"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// The Daemon lists only the Sessions of the organization its account
// belongs to, so a switch to an account of another organization would
// empty the sidebar of the first one's. The Host keeps each organization's
// list (host.RecordSessions); the sidebar shows the other signed-in
// accounts' Sessions beside the live ones, muted and marked with their
// account. Opening one moves it to the account in use (host.TakeSession),
// as droid opens a Session only under its own organization; a switch to
// its account is offered when the move fails.

// elsewhereState is what the window knows of other accounts' Sessions.
type elsewhereState struct {
	key string
	rev uint64
	// accounts are the shown Sessions of other accounts, by id.
	accounts map[string]host.AccountInfo
	extra    []sessions.Summary
	// unopened are the Sessions routed to while another account's: they
	// load once the Daemon lists them, after the switch.
	unopened map[string]unopenedSession
	// confirm is the Session whose switch was asked for and waits, while
	// other Sessions work, for "Switch anyway".
	confirm string
	// taking are the Sessions being moved to the account in use, until they
	// open; takeErr why a move failed.
	taking  map[string]bool
	takeErr map[string]string
}

// unopenedSession is a Session waiting for the switch to its account.
type unopenedSession struct {
	acc host.AccountInfo
	sum *sessions.Summary
}

// noteUnopened keeps Session id, shown as another account's, until the
// Daemon of that account lists it.
func (a *App) noteUnopened(id string, acc host.AccountInfo) {
	e := &a.elsewhere
	if e.unopened == nil {
		e.unopened = map[string]unopenedSession{}
	}
	u := unopenedSession{acc: acc}
	if s := findSummary(e.extra, id); s != nil {
		c := *s
		u.sum = &c
	}
	e.unopened[id] = u
}

// keepList gives the Host the whole list, once every page is read, to keep
// for the account's organization. Call with mu held.
func (a *App) keepList() {
	h := a.cfg.Host
	if h == nil || a.following() {
		return
	}
	list := slices.Clone(a.listed)
	go func() {
		if acc := h.LoginState().Account; acc != nil && acc.OrgID != nil {
			_ = h.RecordSessions(*acc.OrgID, list)
		}
	}()
}

// withElsewhere adds the other signed-in accounts' Sessions to listed,
// those the Daemon does not list itself.
func (a *App) withElsewhere(listed []sessions.Summary, rev uint64) ([]sessions.Summary, uint64) {
	h := a.cfg.Host
	e := &a.elsewhere
	if h == nil || a.following() {
		if len(e.accounts) > 0 {
			e.accounts, e.extra, e.key = nil, nil, ""
			e.rev++
		}
		return listed, rev + e.rev
	}
	key := fmt.Sprint(h.ActiveAccount(), rev, h.SessionIndexRev())
	if key != e.key {
		e.key = key
		live := make(map[string]bool, len(listed))
		for _, s := range listed {
			live[s.SessionID] = true
		}
		e.accounts, e.extra = map[string]host.AccountInfo{}, nil
		for _, s := range h.SessionsElsewhere() {
			if live[s.SessionID] || e.accounts[s.SessionID].Login != nil {
				continue
			}
			e.accounts[s.SessionID] = s.Account
			e.extra = append(e.extra, s.Summary)
		}
		e.rev++
	}
	if len(e.extra) == 0 {
		return listed, rev + e.rev
	}
	return append(slices.Clip(listed), e.extra...), rev + e.rev
}

// elsewhereOf is the account a Session routed to belongs to, while the
// Daemon cannot open it, and the Session as last listed; one waiting for a
// switch opens once it is listed.
func (a *App) elsewhereOf(id string, listed []sessions.Summary) (host.AccountInfo, *sessions.Summary, bool) {
	e := &a.elsewhere
	if acc, ok := e.accounts[id]; ok {
		return acc, findSummary(listed, id), true
	}
	u, ok := e.unopened[id]
	if !ok {
		return host.AccountInfo{}, nil, false
	}
	if findSummary(listed, id) == nil {
		return u.acc, u.sum, true
	}
	delete(e.unopened, id)
	delete(e.taking, id)
	a.view(id).Open()
	a.remember(id)
	return host.AccountInfo{}, nil, false
}

// accountInitial is the first letter of an account's name, as its mark shows.
func accountInitial(name string) string {
	r, _ := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(r))
}

// elsewhereMark is a sidebar row's sign that its Session is another
// account's: that account's initial in a small disc.
func (a *App) elsewhereMark(c *ui.Context, acc host.AccountInfo) {
	k, t := a.kit, a.kit.T
	who := accountWho(acc)
	ui.Box(c).Role(ui.RoleImage).Label(L("Belongs to %s", who)).Tooltip(L("Belongs to %s", who)).Size(k.Px(16), k.Px(16)).Shrink(0).
		Radius(k.Px(8)).Border(1, t.Border).Background(t.Background).Center().Children(func() {
		k.Text(c, accountInitial(who), 9, 12).FontWeight(600).TextColor(t.MutedForeground)
	})
}

// takeSession moves Session id, another account's, to the account in use;
// it opens once the Daemon lists it. A move that failed is not tried again
// until Droi starts again.
func (a *App) takeSession(id string, acc host.AccountInfo) {
	e := &a.elsewhere
	a.noteUnopened(id, acc)
	if e.taking[id] || e.takeErr[id] != "" {
		return
	}
	if e.taking == nil {
		e.taking, e.takeErr = map[string]bool{}, map[string]string{}
	}
	e.taking[id] = true
	h := a.cfg.Host
	go func() {
		err := h.TakeSession(id)
		if err == nil {
			a.reloadList()
			return
		}
		a.cfg.Update(func() {
			delete(e.taking, id)
			e.takeErr[id] = err.Error()
		})
	}()
}

// activeWho is who the account in use is signed in as.
func (a *App) activeWho() string {
	for _, acc := range a.cfg.Host.Accounts() {
		if acc.Active {
			return accountWho(acc)
		}
	}
	return ""
}

// askSwitchFor asks to switch to the account Session id belongs to: at
// once when no Session works, else after "Switch anyway".
func (a *App) askSwitchFor(id string) {
	a.Go(Route{Name: "session", SessionID: id})
	a.elsewhere.confirm = id
}

// elsewhereView is the page of another account's Session: what it is,
// whose, and a way to switch to that account, after which it opens.
func (a *App) elsewhereView(c *ui.Context, id string, sel *sessions.Summary, acc host.AccountInfo, working int) {
	k, t := a.kit, a.kit.T
	e := &a.elsewhere
	who := accountWho(acc)
	title, workspace := L("Untitled session"), ""
	if sel != nil {
		title, workspace = sel.Title, sel.Cwd
	}
	switching := acc.ID == a.cfg.Host.ActiveAccount()
	doSwitch := func() {
		e.confirm = ""
		a.noteUnopened(id, acc)
		a.switchAccount(acc.ID)
	}
	if e.confirm == id && working == 0 && !switching {
		doSwitch()
		switching = true
	}
	ui.Column(c).Role(ui.RoleGroup).Label(title).Fill().Children(func() {
		ui.Box(c).Height(44).Shrink(0).DragWindow()
		ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Padding(0, k.Px(32), k.Px(64), k.Px(32)).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(k.Px(440)).Gap(k.Px(16)).Children(func() {
				ui.Column(c).Gap(k.Px(2)).Children(func() {
					k.Text(c, title, 16, 24).Role(ui.RoleHeading).FontWeight(600)
					if workspace != "" {
						k.Text(c, workspace, 12, 16).TextColor(t.MutedForeground).SingleLine()
					}
				})
				ui.Row(c).Gap(k.Px(12)).AlignItems(ui.Start).Padding(k.Px(12), k.Px(16)).Radius(k.Px(14)).Background(t.Muted).Children(func() {
					a.accountMark(c, accountInitial(who), "user-round")
					ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(k.Px(2)).Children(func() {
						k.Text(c, L("This session belongs to %s.", who), 13, 20).FontWeight(500)
						k.Text(c, L("droid opens a session only under the account it belongs to. Switch to that account to continue it here."), 12, 18).TextColor(t.MutedForeground)
					})
				})
				if e.taking[id] && !switching {
					ui.Row(c).Role(ui.RoleStatus).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
						k.Spinner(c, 14, t.MutedForeground)
						k.Text(c, L("Opening it under %s…", a.activeWho()), 13, 20).TextColor(t.MutedForeground)
					})
					return
				}
				if err := e.takeErr[id]; err != "" && !switching {
					k.Text(c, L("Could not move it to %s: %s", a.activeWho(), err), 13, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
				}
				switch {
				case switching:
					ui.Row(c).Role(ui.RoleStatus).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
						k.Spinner(c, 14, t.MutedForeground)
						k.Text(c, L("Switching to %s…", who), 13, 20).TextColor(t.MutedForeground)
					})
				case e.confirm == id:
					msg := L("%d sessions are working. Switching restarts the Daemon and stops them.", working)
					if working == 1 {
						msg = L("A session is working. Switching restarts the Daemon and stops it.")
					}
					k.Text(c, msg, 13, 20).Role(ui.RoleStatus).TextColor(t.MutedForeground)
					ui.Row(c).Gap(k.Px(8)).Children(func() {
						if a.smallButton(c, kit.Ghost, L("Cancel"), "", false).Clicked() {
							e.confirm = ""
						}
						if a.smallButton(c, kit.Primary, L("Switch anyway"), "", false).Clicked() {
							doSwitch()
						}
					})
				default:
					if a.smallButton(c, kit.Primary, L("Switch to %s", who), "", false).AlignSelf(ui.Start).Clicked() {
						e.confirm = id
					}
				}
			})
		})
	})
}
