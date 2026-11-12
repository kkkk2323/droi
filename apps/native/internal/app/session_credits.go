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
	at            time.Time
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

// creditCuts are what the footer takes off the Daemon's figures, from the
// request log (llmusage.LoggedCredits.Recounted): droid counts a Gemini
// call's cached input twice. They are worked out again at most every
// observeRefresh, and kept as amounts so that the figures between two reads
// do not jump back to the Daemon's.
type creditCuts struct {
	own, inclusive, turn, children float64
	// turnAt is the turn the turn cuts are for.
	turnAt time.Time
	at     time.Time
	busy   bool
}

func (c creditCuts) any() bool { return c.own > 0 || c.inclusive > 0 || c.turn > 0 || c.children > 0 }

// recountable says droid's counters show its Gemini recount: its input
// tokens hold the cache reads, where other providers' leave them out.
func recountable(us ...*protocol.TokenUsage) bool {
	for _, u := range us {
		if u != nil && u.CacheReadTokens > 0 && u.InputTokens >= u.CacheReadTokens {
			return true
		}
	}
	return false
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

// recount works the cuts out again for the figures shown, in the
// background.
func (v *sessionView) recount(own, inclusive *float64, turn observedTurnCredits) {
	a := v.a
	cuts := &v.creditCuts
	if cuts.busy || !cuts.at.IsZero() && a.cfg.Now().Sub(cuts.at) < observeRefresh {
		return
	}
	log := a.requestLog()
	if log == nil {
		return
	}
	if a.cfg.Host != nil {
		llmusage.UseDroidModels(a.cfg.Host.DroidModels())
	}
	cuts.busy = true
	id, kids := v.id, a.subagentsOf(v.id)
	go func() {
		_ = log.Refresh()
		self := map[string]bool{id: true}
		all := map[string]bool{id: true}
		for k := range kids {
			all[k] = true
		}
		cut := func(daemon *float64, ids map[string]bool, from, to time.Time) float64 {
			if daemon == nil {
				return 0
			}
			fixed, ok := log.SessionCredits(ids, from, to).Recounted(*daemon)
			if !ok {
				return 0
			}
			return *daemon - fixed
		}
		next := creditCuts{own: cut(own, self, time.Time{}, time.Time{}), inclusive: cut(inclusive, all, time.Time{}, time.Time{})}
		if turn.took > 0 {
			// The log times a call when it ends; the slack covers the clocks
			// of the Daemon's notification and the proxy's record.
			from, to := turn.at.Add(-turn.took-5*time.Second), turn.at.Add(5*time.Second)
			next.turn, next.turnAt = cut(turn.own, self, from, to), turn.at
			next.children = cut(turn.children, kids, from, to)
		}
		a.cfg.Update(func() {
			next.at = a.cfg.Now()
			v.creditCuts = next
		})
	}()
}

// less is value with cut taken off, nil kept nil.
func less(value *float64, cut float64) *float64 {
	if value == nil || cut <= 0 {
		return value
	}
	n := max(*value-cut, 0)
	return &n
}

// creditsMeter reads the Daemon's counters independently of model-speed
// measurements and the Key Pool. Its totals are lifetime Session figures,
// never substituted for a request or a filtered period in Usage.
func (v *sessionView) creditsMeter(c *ui.Context, s *session.Session) {
	u := s.Usage()
	own, inclusive := creditValue(u.Session), creditValue(u.Inclusive)
	turn := v.turnCredits
	if recountable(u.Session, u.Inclusive) {
		v.recount(own, inclusive, turn)
		cuts := v.creditCuts
		own, inclusive = less(own, cuts.own), less(inclusive, cuts.inclusive)
		if cuts.turnAt.Equal(turn.at) {
			turn.own, turn.children = less(turn.own, cuts.turn), less(turn.children, cuts.children)
		}
	}
	total := inclusive
	if total == nil {
		total = own
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
			k.Text(c, L("Reported by the Daemon."), 12, 16).TextColor(t.MutedForeground)
			k.Text(c, L("Already counted at each model's multiplier and promotion, as the quota counts them."), 12, 16).TextColor(t.MutedForeground)
			if cuts := v.creditCuts; recountable(u.Session, u.Inclusive) && cuts.any() {
				k.Text(c, L("droid counts a Gemini call's cached input twice, which Factory does not bill. %s credits of that are taken off, from the request log.", groupDigits(max(cuts.own, cuts.inclusive))), 12, 16).TextColor(t.MutedForeground)
			}
			k.Text(c, L("Session totals cover its full history. Usage shows separate request estimates."), 12, 16).TextColor(t.MutedForeground)
		})
	})
}
