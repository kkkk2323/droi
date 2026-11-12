package app

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
)

type observedTurnCredits struct {
	own, children *float64
	// ownTokens and childTokens are the turn's, which say whether the
	// request log holds all of its calls.
	ownTokens, childTokens *protocol.TokenUsage
	at                     time.Time
	// took is how long the turn ran, 0 when the Daemon did not say.
	took time.Duration
}

// creditValue keeps a reported zero distinct from a missing field. Do not
// derive Session credits from its current model: it may have used others.
func creditValue(u *protocol.TokenUsage) *float64 {
	if u == nil || u.FactoryCredits == nil {
		return nil
	}
	n := *u.FactoryCredits
	if n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	return &n
}

// covers says the log's calls are those behind u.
func covers(l llmusage.LoggedCredits, u *protocol.TokenUsage) bool {
	return u != nil && l.Covers(u.OutputTokens, u.CacheReadTokens, u.CacheCreationTokens)
}

// loggedCredits replace the Daemon's figures with the request log's when
// the log holds every call behind them: droid's factoryCredits miss what
// Factory bills (a Gemini call's cache reads counted twice, Anthropic's
// hour-long cache writes at the 5-minute price), while the log prices the
// tokens each response reported (TestLiveRequestLogCreditsMatchFactoryBilling).
// Each is kept as the difference from the Daemon's figure it was worked out
// against, so that the figures between two reads move with the Daemon's;
// nil keeps the Daemon's. They are worked out again at most every
// observeRefresh, in the background.
type loggedCredits struct {
	own, inclusive, turn, children *float64
	// turnAt is the turn the turn figures are for.
	turnAt time.Time
	// of is what they were worked out for, and changed when that changed:
	// they are read again only when it changes, and for a little while
	// after, as the proxy may write a call down after the Daemon reports it.
	of      loggedKey
	changed time.Time
	at      time.Time
	busy    bool
}

type loggedKey struct {
	own, inclusive float64
	turnAt         time.Time
}

// logSettles is how long after the Daemon's figures change the log is read
// again for calls the proxy had not written down yet.
const logSettles = 30 * time.Second

func (l loggedCredits) any() bool {
	return l.own != nil || l.inclusive != nil || l.turn != nil || l.children != nil
}

// subagentsOf are the Sessions id started, and theirs, as the list knows
// them.
func (a *App) subagentsOf(id string) map[string]bool {
	a.mu.Lock()
	byCaller := map[string][]string{}
	for _, s := range a.listed {
		if s.CallingSessionID != "" {
			byCaller[s.CallingSessionID] = append(byCaller[s.CallingSessionID], s.SessionID)
		}
	}
	a.mu.Unlock()
	out := map[string]bool{}
	next := []string{id}
	for len(next) > 0 {
		cur := next[len(next)-1]
		next = next[:len(next)-1]
		for _, ch := range byCaller[cur] {
			if !out[ch] && ch != id {
				out[ch] = true
				next = append(next, ch)
			}
		}
	}
	return out
}

// readLog works the request log's figures out again for those shown.
func (v *sessionView) readLog(u session.Usage, turn observedTurnCredits) {
	a := v.a
	l := &v.logged
	now := a.cfg.Now()
	key := loggedKey{turnAt: turn.at}
	if n := creditValue(u.Session); n != nil {
		key.own = *n
	}
	if n := creditValue(u.Inclusive); n != nil {
		key.inclusive = *n
	}
	if key != l.of {
		l.of, l.changed = key, now
	}
	if l.busy || !l.at.IsZero() && (now.Sub(l.at) < observeRefresh || l.at.After(l.changed.Add(logSettles))) {
		return
	}
	log := a.requestLog()
	if log == nil {
		return
	}
	if a.cfg.Host != nil {
		llmusage.UseDroidModels(a.cfg.Host.DroidModels())
	}
	l.busy = true
	own, inclusive := creditValue(u.Session), creditValue(u.Inclusive)
	id, kids := v.id, a.subagentsOf(v.id)
	go func() {
		_ = log.Refresh()
		self := map[string]bool{id: true}
		all := map[string]bool{id: true}
		for k := range kids {
			all[k] = true
		}
		diff := func(daemon *float64, tokens *protocol.TokenUsage, ids map[string]bool, from, to time.Time) *float64 {
			if daemon == nil {
				return nil
			}
			got := log.SessionCredits(ids, from, to)
			if !covers(got, tokens) {
				return nil
			}
			d := got.Credits - *daemon
			return &d
		}
		next := loggedCredits{own: diff(own, u.Session, self, time.Time{}, time.Time{}), inclusive: diff(inclusive, u.Inclusive, all, time.Time{}, time.Time{})}
		if turn.took > 0 {
			// The log times a call when it ends; the slack covers the clocks
			// of the Daemon's notification and the proxy's record.
			from, to := turn.at.Add(-turn.took-5*time.Second), turn.at.Add(5*time.Second)
			next.turn, next.turnAt = diff(turn.own, turn.ownTokens, self, from, to), turn.at
			next.children = diff(turn.children, turn.childTokens, kids, from, to)
		}
		a.cfg.Update(func() {
			next.of, next.changed, next.at = v.logged.of, v.logged.changed, a.cfg.Now()
			v.logged = next
		})
	}()
}

// moved is value moved by diff, nil kept nil.
func moved(value, diff *float64) *float64 {
	if value == nil || diff == nil {
		return value
	}
	n := max(*value+*diff, 0)
	return &n
}

// creditsMeter shows a Session's credits: the request log's, priced from
// what each response reported, when the log holds all of its calls, else the
// Daemon's counters. Its totals are lifetime Session figures, never
// substituted for a request or a filtered period in Usage.
func (v *sessionView) creditsMeter(c *ui.Context, s *session.Session) {
	u := s.Usage()
	daemonOwn, daemonInc := creditValue(u.Session), creditValue(u.Inclusive)
	turn := v.turnCredits
	if daemonOwn != nil || daemonInc != nil || turn.own != nil {
		v.readLog(u, turn)
	}
	logged := v.logged
	own, inclusive := moved(daemonOwn, logged.own), moved(daemonInc, logged.inclusive)
	if logged.turnAt.Equal(turn.at) {
		turn.own, turn.children = moved(turn.own, logged.turn), moved(turn.children, logged.children)
	}
	total, daemonTotal := inclusive, daemonInc
	if total == nil {
		total, daemonTotal = own, daemonOwn
	}
	if total == nil && turn.own == nil && turn.children == nil {
		return
	}
	k, t := v.a.kit, v.a.kit.T
	label := L("Credits")
	if total != nil {
		label = L("%s Credits", llmusage.FormatCredits(*total))
	}
	b := ui.ButtonBase(c).Label(L("Session credits")).Height(k.Px(24)).PaddingX(k.Px(6)).Radius(k.Px(6)).Shrink(0).
		Cursor(ui.CursorPointer).Expanded(v.creditsOpen)
	color := t.MutedForeground
	if b.Hovered() || v.creditsOpen {
		b.Background(t.Muted)
		color = t.Foreground
	}
	b.Children(func() {
		k.Text(c, label, 12, 16).TextColor(color).FontFeatures("tnum").SingleLine()
	})
	if b.Clicked() {
		v.creditsOpen = !v.creditsOpen
	}
	ui.PopoverBase(c, b, &v.creditsOpen, func(p ui.Element) {
		p.AttachTo(b, ui.AnchorTopRight, ui.AnchorBottomRight).Margin(0, 0, k.Px(6), 0).
			Role(ui.RoleDialog).Label(L("Session credits")).Width(k.Px(300)).Padding(k.Px(10), k.Px(12)).Radius(k.Px(8)).
			Border(1, t.Border).Background(t.Popover).TextColor(t.PopoverForeground).
			Shadow(0, k.Px(10), k.Px(15), -k.Px(3), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).FillWidth().Gap(k.Px(6)).Children(func() {
			row := func(label string, value *float64) {
				if value == nil {
					return
				}
				ui.Row(c).FillWidth().Gap(k.Px(8)).Children(func() {
					k.Text(c, label, 13, 18).SingleLine().Grow(1).MinWidth(0)
					k.Text(c, groupDigits(*value), 13, 18).FontFeatures("tnum").SingleLine().Shrink(0)
				})
			}
			row(L("This Session"), own)
			row(L("Including subagents"), inclusive)
			row(L("Last observed turn"), turn.own)
			row(L("Subagents in that turn"), turn.children)
			if turn.own != nil || turn.children != nil {
				k.Text(c, L("Turn observed %s", turn.at.Local().Format("2006-01-02 15:04:05")), 12, 16).TextColor(t.MutedForeground)
			}
			muted := func(s string) { k.Text(c, s, 12, 16).TextColor(t.MutedForeground) }
			if logged.any() {
				muted(L("Priced from the request log: the tokens each response reported, at droid's prices, as Factory bills them."))
				if total != nil && daemonTotal != nil && math.Abs(*total-*daemonTotal) > 0.01*max(*total, 1) {
					muted(L("droid reports %s, which misses how Factory bills some calls.", groupDigits(*daemonTotal)))
				}
			} else {
				muted(L("Reported by the Daemon."))
				muted(L("Already counted at each model's multiplier and promotion, as the quota counts them."))
			}
			muted(L("Session totals cover its full history. Usage shows separate request estimates."))
		})
	})
}
