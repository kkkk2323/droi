package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
	"github.com/kkkk2323/droi/apps/native/pkg/dashkit"
	"github.com/kkkk2323/droi/apps/native/pkg/keypool"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

// The Usage tab's pages, in the order of its segmented control.
const (
	usageKeys    = "keys"
	usageWarming = "warming"
	usageObserve = "observe"
	usageEvents  = "events"
)

// How often the tab reads what is not pushed to it: the Key Pool's
// snapshot is cheap, the spend log is a file.
const (
	poolRefresh  = 2 * time.Second
	spendRefresh = 30 * time.Second
)

// detailMotion opens and closes a key's windows under its row.
var detailMotion = ui.ElementTransition{
	Size:  true,
	Enter: &ui.Motion{Collapse: true},
	Exit:  &ui.Motion{Collapse: true},
}

// usageState is the Usage tab's, kept between frames.
type usageState struct {
	// shown is whether the last frame built the tab, so that entering it
	// reads the quota again.
	shown   bool
	section string

	pool   keypool.State
	poolAt time.Time

	spend     llmusage.Spend
	hasSpend  bool
	spendAt   time.Time
	spendBusy bool

	observe observeState

	model usageModel

	// expanded holds the keys shown in full, by key ID.
	expanded map[string]bool
	evtList  ui.ListState

	addOpen        bool
	addKey, addErr string
	adding         bool

	// acting is what an action runs on, err what the last one said.
	acting, err string
	refreshing  bool

	// bar is the menu bar item, built from the snapshot at barAt.
	bar               llmusage.MenuBarView
	barBuilt          bool
	barAt, barSpendAt time.Time
	barLang           l10n.Language
}

// usageModel is what the tab shows, built again when what it is built from
// changes.
type usageModel struct {
	built   bool
	poolAt  time.Time
	spendAt time.Time
	lang    l10n.Language

	pools    []llmusage.PoolSummaryView
	runway   llmusage.RunwayView
	expiring llmusage.ExpiringView
	cards    []usageCard
	rows     []llmusage.KeyRowView
	problems int
	checked  string
	events   []llmusage.EventRowView
	warming  llmusage.CacheWarmingView
}

type usageCard struct {
	id, masked string
	current    bool
	view       llmusage.KeyCardView
}

// usageTabShown: Settings has the Usage tab once the Patched Droid runs the
// Daemon, since only then does the Key Pool serve its requests.
func (a *App) usageTabShown() bool {
	return a.cfg.Host != nil && a.cfg.Host.PatchedDroidState().Enabled
}

func (a *App) keyPool() *keypool.Pool {
	if a.cfg.Host == nil {
		return nil
	}
	return a.cfg.Host.KeyPool()
}

// poolState is the Key Pool's snapshot, read again at most every
// poolRefresh: the window builds many frames a second, and a snapshot
// copies every key and event.
func (a *App) poolState() keypool.State {
	s := &a.usage
	pool := a.keyPool()
	if pool == nil {
		return keypool.State{}
	}
	if s.poolAt.IsZero() || a.cfg.Now().Sub(s.poolAt) >= poolRefresh {
		s.pool, s.poolAt = pool.State(), a.cfg.Now()
	}
	return s.pool
}

// refreshPool drops the snapshot, so the next frame reads it again.
func (a *App) refreshPool() { a.usage.poolAt = time.Time{} }

func (a *App) loadSpend(force bool) {
	s := &a.usage
	pool := a.keyPool()
	if pool == nil || s.spendBusy || !force && !s.spendAt.IsZero() && a.cfg.Now().Sub(s.spendAt) < spendRefresh {
		return
	}
	s.spendBusy = true
	path := pool.UsageLog()
	go func() {
		spend, ok := llmusage.ReadSpend(path, time.Now())
		a.cfg.Update(func() {
			s.spendBusy, s.spendAt = false, a.cfg.Now()
			s.spend, s.hasSpend = spend, ok
		})
	}()
}

// enterUsage forgets what the last visit read, so the tab opens on what is
// true now.
func (a *App) enterUsage() {
	s := &a.usage
	s.poolAt, s.spendAt, s.observe.at = time.Time{}, time.Time{}, time.Time{}
	s.err, s.addErr = "", ""
	a.refreshQuota()
}

// refreshQuota reads every key's quota from Factory now.
func (a *App) refreshQuota() {
	s := &a.usage
	pool := a.keyPool()
	if pool == nil || s.refreshing {
		return
	}
	s.refreshing = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pool.RefreshUsage(ctx)
		a.cfg.Update(func() {
			s.refreshing = false
			a.refreshPool()
		})
	}()
}

// poolAct runs one of the tab's actions off the frame and keeps what it
// said.
func (a *App) poolAct(what string, fn func() error) {
	s := &a.usage
	s.acting, s.err = what, ""
	go func() {
		err := fn()
		a.cfg.Update(func() {
			s.acting = ""
			if err != nil {
				s.err = err.Error()
			}
			a.refreshPool()
		})
	}()
}

// keyStates are the keys as the display math reads them.
func keyStates(st keypool.State) []keys.KeyState {
	out := make([]keys.KeyState, 0, len(st.Keys))
	for _, k := range st.Keys {
		out = append(out, k.State)
	}
	return out
}

// buildUsage is the tab's model, built again when the Key Pool's snapshot,
// the spend, the history or the language changed.
func (a *App) buildUsage(st keypool.State) *usageModel {
	s := &a.usage
	m := &s.model
	lang := l10n.Code()
	if m.built && m.poolAt.Equal(s.poolAt) && m.spendAt.Equal(s.spendAt) && m.lang == lang {
		return m
	}
	now := a.cfg.Now()
	pool := keyStates(st)
	spend := a.spendOrNil()
	*m = usageModel{built: true, poolAt: s.poolAt, spendAt: s.spendAt, lang: lang}
	m.pools = llmusage.PoolSummaries(pool, now)
	m.runway = llmusage.BuildRunway(pool, spend, now)
	m.expiring = llmusage.BuildExpiring(pool, now, spend)
	for _, k := range st.Keys {
		m.cards = append(m.cards, usageCard{id: k.ID, masked: k.Masked, current: k.Current, view: llmusage.BuildKeyCard(k.State, now)})
		m.rows = append(m.rows, llmusage.BuildKeyRow(k.State, now))
		if llmusage.KeyDisplayOf(k.State, now).IsProblem() {
			m.problems++
		}
	}
	if checked := llmusage.UsageCheckedAt(pool); checked != nil {
		m.checked = L("Quota checked %s", llmusage.FormatRelative(*checked, now))
	}
	m.events = llmusage.EventRows(st.Events)
	m.warming = llmusage.BuildCacheWarming(st.CacheWarming, now)
	return m
}

type usageSection struct{ id, label string }

func usageSections() []usageSection {
	return []usageSection{
		{usageKeys, L("Keys and quota")},
		{usageWarming, L("Cache warming")},
		{usageObserve, L("Observability")},
		{usageEvents, L("Event log")},
	}
}

// usageTab is Settings → Usage, drawn as droid-proxy's window: the pages
// as segments with the actions beside them, the page, and the status bar.
func (a *App) usageTab(c *ui.Context) {
	u := a.usageLook(c)
	s := &a.usage
	if s.expanded == nil {
		s.expanded = map[string]bool{}
	}
	if s.section == "" {
		s.section = usageKeys
	}
	if !s.observe.inited {
		s.observe.reqSel, s.observe.inited = -1, true
	}
	st := a.poolState()
	a.loadSpend(false)
	m := a.buildUsage(st)
	base := c.Theme()
	c.SetTheme(u.Themed(base))
	defer c.SetTheme(base)
	ui.Column(c).Role(ui.RoleGroup).Label(L("Usage")).Grow(1).MinWidth(0).FillHeight().Background(u.P.Bg).TextColor(u.P.Text).FontSize(u.Px(13)).Children(func() {
		a.usageHeader(c, u)
		if s.err != "" {
			ui.Row(c).Role(ui.RoleStatus).Shrink(0).Gap(u.Px(8)).Padding(0, u.Px(24), u.Px(6)).Children(func() {
				u.Icon(c, dashkit.IconWarning, 13, u.P.Danger)
				u.Text(c, s.err, 12).TextColor(u.P.Danger).Selectable().Shrink(1)
			})
		}
		ui.Column(c).Grow(1).Basis(0).MinHeight(0).Children(func() {
			switch s.section {
			case usageWarming:
				a.warmingPage(c, u, m)
			case usageObserve:
				a.observePage(c, u, st)
			case usageEvents:
				a.eventsPage(c, u, m)
			default:
				a.keysPage(c, u, st, m)
			}
		})
		a.usageStatusBar(c, u, st)
	})
	a.addKeySheet(c, u)
}

// usageHeader is the pages in the middle and the page's actions on the
// right.
func (a *App) usageHeader(c *ui.Context, u usageLook) {
	s := &a.usage
	ui.Row(c).Height(u.Px(52)).Shrink(0).Padding(0, u.Px(14)).Gap(u.Px(12)).DragWindow().Children(func() {
		ui.Box(c).Grow(1).Basis(0)
		sections := usageSections()
		selected := 0
		labels := make([]string, len(sections))
		for i, sec := range sections {
			labels[i] = sec.label
			if sec.id == s.section {
				selected = i
			}
		}
		if u.Segmented(c, &selected, labels, L("Usage pages"), "", false, false) {
			s.section = sections[selected].id
		}
		ui.Row(c).Grow(1).Basis(0).Gap(2).Justify(ui.End).Children(func() {
			if s.section == usageKeys && u.ToolButton(c, dashkit.IconPlus, L("Add key"), L("Add a Factory API key"), false) {
				a.openAddKey()
			}
			if u.ToolButton(c, dashkit.IconRefresh, L("Refresh"), L("Read the quota and the records again"), s.refreshing) {
				a.refreshQuota()
				a.loadSpend(true)
				if s.section == usageObserve {
					a.loadObservation(true)
				}
			}
		})
	})
}

// page is a page's scrolling column, as wide as droid-proxy's window at
// most.
func (u usageLook) page(c *ui.Context, fn func()) {
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(u.Px(960)).AlignSelf(ui.Center).Padding(u.Px(4), u.Px(20), u.Px(20)).Gap(u.Px(10)).Children(fn)
	})
}

// tablePage is a page whose table grows to the room left.
func (u usageLook) tablePage(c *ui.Context, fn func()) {
	ui.Column(c).Grow(1).MinHeight(0).FillWidth().MaxWidth(u.Px(960)).AlignSelf(ui.Center).Padding(u.Px(4), u.Px(20), u.Px(16)).Gap(u.Px(10)).Children(fn)
}

func (a *App) keysPage(c *ui.Context, u usageLook, st keypool.State, m *usageModel) {
	u.page(c, func() {
		u.runwaySection(c, m.runway)
		ui.Box(c).Height(u.Px(6))
		u.SectionHead(c, L("Quota"), m.checked, nil)
		u.poolCards(c, m.pools)
		ui.Box(c).Height(u.Px(6))
		u.expiring(c, m.expiring)
		ui.Box(c).Height(u.Px(6))
		a.keyList(c, u, st, m)
		ui.Box(c).Height(u.Px(6))
		a.keyOpening(c, u, st)
	})
}

// keyOpening is whether Droi starts waiting keys' months on its own, and
// how today's openings stand.
func (a *App) keyOpening(c *ui.Context, u usageLook, st keypool.State) {
	p := u.P
	title := L("Start on a schedule")
	u.SectionHead(c, L("Waiting keys"), openingNote(st), nil)
	u.Card(c).Padding(u.Px(12), u.Px(16)).Children(func() {
		ui.Row(c).Gap(u.Px(12)).Children(func() {
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(u.Px(3)).Children(func() {
				u.Text(c, title, 13).FontWeight(500)
				u.Text(c, L("From 8:00 each day, Droi starts up to %d waiting keys a day, without waiting for your requests. Each start sends a one-token Haiku request, and the key's month counts from then. Off: a waiting key starts on the next request.", config.DailyKeyOpens), 12).TextColor(p.Secondary)
			})
			if u.K.Toggle(c, title, st.ScheduledOpening, a.usage.acting != "") {
				pool, on := a.keyPool(), !st.ScheduledOpening
				a.poolAct("opening", func() error { return pool.SetScheduledOpening(on) })
			}
		})
	})
}

// openingNote is how many keys opened today and how many wait; empty while
// the Key Pool is not running.
func openingNote(st keypool.State) string {
	o := st.Opening
	if o == nil {
		return ""
	}
	note := L("%d of %d started today · %d waiting", o.OpenedToday, o.DailyOpens, len(o.Queue))
	if st.ScheduledOpening && !o.Scheduled && len(o.Queue) > 0 {
		note = L("%s · starts once droid sends a Haiku request", note)
	}
	return note
}

// runwaySection is today's spend, the last days' spend against the daily
// limit, and how long the pool lasts.
func (u usageLook) runwaySection(c *ui.Context, r llmusage.RunwayView) {
	p := u.P
	u.SectionHead(c, L("Today"), "", func() { u.Info(c, L("About the forecast"), r.Help, 12) })
	if !r.Available {
		u.Card(c).Padding(u.Px(16)).Children(func() {
			u.Text(c, r.Empty, 12).TextColor(p.Secondary)
		})
		return
	}
	u.MetricStrip(c, r.Metrics)
	u.Card(c).Padding(u.Px(14), u.Px(16), u.Px(12)).Gap(u.Px(12)).Children(func() {
		ui.Row(c).Gap(u.Px(8)).Children(func() {
			u.Text(c, L("Last %d days", llmusage.SpendChartDays), 12).FontWeight(600).TextColor(p.Secondary)
			ui.Spacer(c)
			ui.Row(c).Gap(u.Px(6)).Children(func() {
				ui.Box(c).Width(u.Px(14)).Height(u.Px(8)).Draw(func(pt *ui.Painter, rc ui.Rect) {
					y := rc.Y + rc.H/2
					pt.Line(rc.X, y, rc.X+5, y, 1, p.Secondary)
					pt.Line(rc.X+8, y, rc.X+13, y, 1, p.Secondary)
				})
				u.Text(c, r.Chart.LimitLabel, 11).TextColor(p.Secondary).FontFeatures("tnum")
			})
		})
		u.Chart(c, r.Chart, 96)
		u.Hairline(c)
		for i, l := range r.Lines {
			ui.Row(c.Key(i)).Gap(u.Px(8)).Children(func() {
				u.Text(c, l.Label, 12).TextColor(p.Secondary).FontFeatures("tnum").SingleLine().Shrink(1)
				ui.Spacer(c)
				u.Text(c, l.Note, 12).TextColor(p.Tertiary).FontFeatures("tnum").SingleLine().Shrink(1).Tooltip(l.Note)
				u.Text(c, l.Value, 12).FontWeight(600).TextColor(u.Tone(l.Tone)).FontFeatures("tnum").NoWrap().Shrink(0).MinWidth(u.Px(150)).TextAlign(ui.End)
			})
		}
	})
}

// poolCard is one pool: its name, how many keys it routes, then a row per
// window with what is left on average and the soonest reset.
func (u usageLook) poolCard(c *ui.Context, pool llmusage.PoolSummaryView) {
	p := u.P
	pad := u.Px(14)
	ui.Column(c.Key(pool.Title)).Role(ui.RoleGroup).Label(pool.Title).Grow(1).Basis(0).MinWidth(0).Gap(u.Px(10)).Padding(pad-2, pad, pad).Children(func() {
		ui.Row(c).Gap(u.Px(6)).Children(func() {
			u.Text(c, pool.Title, 13).FontWeight(600)
			ui.Spacer(c)
			u.Text(c, pool.Note, 11).TextColor(p.Tertiary).FontFeatures("tnum").SingleLine()
		})
		for _, w := range pool.Windows {
			ui.Row(c.Key(w.Label)).Gap(u.Px(8)).Tooltip(w.Help).Children(func() {
				u.Text(c, w.Label, 11).TextColor(p.Secondary).Width(u.Px(36)).NoWrap()
				u.Meter(c, w.Bar.Value, w.Bar.Tone, 5, -1).Label(w.Name)
				u.Text(c, w.Percent, 12).FontWeight(600).TextColor(u.Tone(dashkit.PrimaryTone(w.PercentTone))).FontFeatures("tnum").Width(u.Px(38)).TextAlign(ui.End)
				u.Text(c, w.Reset, 11).TextColor(p.Tertiary).FontFeatures("tnum").Width(u.Px(150)).SingleLine()
			})
		}
	})
}

// poolCards puts the pools side by side in one card.
func (u usageLook) poolCards(c *ui.Context, pools []llmusage.PoolSummaryView) {
	u.Card(c).Children(func() {
		ui.Row(c).AlignItems(ui.Stretch).Children(func() {
			for i, pool := range pools {
				if i > 0 {
					u.VHairline(c)
				}
				u.poolCard(c, pool)
			}
		})
	})
}

// expiring is the monthly quota each key has left before its month ends:
// key counts by urgency first, then one group per deadline with its keys
// as chips, the most left first.
func (u usageLook) expiring(c *ui.Context, v llmusage.ExpiringView) {
	p := u.P
	u.SectionHead(c, L("Expiring"), v.Summary, func() { u.Info(c, L("About expiring quota"), v.Help, 12) })
	u.Card(c).Padding(u.Px(14), u.Px(16)).Gap(u.Px(14)).Children(func() {
		if len(v.Pools) == 0 {
			ui.Row(c).Gap(u.Px(6)).Children(func() {
				u.Icon(c, dashkit.IconCheck, 13, p.Green)
				u.Text(c, v.Empty, 12).TextColor(p.Secondary)
			})
			return
		}
		for i, pool := range v.Pools {
			if i > 0 {
				u.Hairline(c)
			}
			ui.Column(c.Key(pool.Title)).Gap(u.Px(12)).Children(func() {
				ui.Row(c).Gap(u.Px(8)).AlignItems(ui.End).Children(func() {
					u.Text(c, v.Credits, 20).FontWeight(600).LetterSpacing(-0.3).FontFeatures("tnum")
					u.Text(c, L("credits expire"), 11).TextColor(p.Tertiary).Padding(0, 0, u.Px(3))
					if v.Loss != "" {
						u.Text(c, v.Loss, 11).FontWeight(500).TextColor(p.Danger).FontFeatures("tnum").Padding(0, 0, u.Px(3))
					}
					ui.Spacer(c)
					ui.Column(c).AlignItems(ui.End).Gap(2).Tooltip(pool.PaceHelp).Children(func() {
						u.Text(c, pool.Pace, 12).FontWeight(500).TextColor(u.Tone(dashkit.PrimaryTone(pool.PaceTone))).FontFeatures("tnum")
						if pool.Recent != "" {
							u.Text(c, pool.Recent, 11).TextColor(p.Tertiary).FontFeatures("tnum")
						}
					})
				})
				ui.Row(c).Gap(u.Px(8)).Children(func() {
					for _, b := range pool.Buckets {
						u.expiringBucket(c, b)
					}
				})
				for gi, g := range pool.Groups {
					u.expiringGroup(c, gi, g)
				}
			})
		}
	})
}

func (u usageLook) expiringBucket(c *ui.Context, b llmusage.ExpiringBucketView) {
	p := u.P
	ui.Column(c.Key(b.Label)).Role(ui.RoleGroup).Label(b.Label).Grow(1).Basis(0).MinWidth(0).Gap(2).Padding(u.Px(9), u.Px(12)).Radius(u.Px(8)).Background(p.Bg).Children(func() {
		u.Text(c, b.Label, 11).TextColor(p.Secondary)
		u.Text(c, b.Value, 17).FontWeight(600).TextColor(u.Tone(b.Tone)).FontFeatures("tnum")
		u.Text(c, b.Note, 10).TextColor(p.Tertiary).FontFeatures("tnum").SingleLine()
	})
}

// expiringGroup is one deadline: when, how many keys, then the keys.
func (u usageLook) expiringGroup(c *ui.Context, key int, g llmusage.ExpiringGroupView) {
	p := u.P
	ui.Column(c.Key(key)).Gap(u.Px(8)).Children(func() {
		ui.Row(c).Gap(u.Px(8)).Children(func() {
			ui.Box(c).Size(u.Px(6), u.Px(6)).Radius(999).Background(u.Tone(g.Tone))
			u.Text(c, g.Countdown, 13).FontWeight(600).TextColor(u.Tone(dashkit.PrimaryTone(dashkit.QuietTone(g.Tone)))).FontFeatures("tnum")
			u.Text(c, g.At, 11).TextColor(p.Tertiary).FontFeatures("tnum")
			ui.Spacer(c)
			u.Text(c, g.Note, 11).TextColor(p.Secondary).FontFeatures("tnum")
		})
		ui.Grid(c).Columns(6).Gap(u.Px(6)).Children(func() {
			for _, k := range g.Keys {
				u.expiringChip(c, k)
			}
		})
	})
}

// expiringChip is one key: its number and what is left, a bar under them,
// then what needs attention.
func (u usageLook) expiringChip(c *ui.Context, k llmusage.ExpiringChipView) {
	p := u.P
	ui.Column(c.Key(k.Index)).MinWidth(0).Gap(u.Px(6)).Padding(u.Px(7), u.Px(9)).Radius(u.Px(8)).Background(p.Bg).Tooltip(k.Help).Children(func() {
		ui.Row(c).Gap(u.Px(4)).Children(func() {
			u.Text(c, "#"+itoa(k.Index), 12).FontWeight(600).FontFeatures("tnum")
			if k.Active {
				u.Icon(c, dashkit.IconStar, 9, p.Warning)
			}
			ui.Spacer(c)
			u.Text(c, k.Credits, 11).TextColor(p.Secondary).FontFeatures("tnum").SingleLine().Shrink(1)
			u.tokensInfo(c, k.Tokens)
		})
		u.Meter(c, k.Bar.Value, k.Bar.Tone, 3, -1).Label(k.Percent)
		if k.Note != "" {
			u.Text(c, k.Note, 10).FontWeight(500).TextColor(u.Tone(k.NoteTone)).FontFeatures("tnum").SingleLine()
		}
	})
}

// keyList is the pool's keys, with how the pool switches between them.
func (a *App) keyList(c *ui.Context, u usageLook, st keypool.State, m *usageModel) {
	s := &a.usage
	allOpen := len(m.cards) > 0
	for _, k := range m.cards {
		allOpen = allOpen && s.expanded[k.id]
	}
	note := countL(len(m.cards), l10n.N("%d key"), l10n.N("%d keys"))
	if m.problems > 0 {
		note = L("%s · %d need attention", note, m.problems)
	}
	u.SectionHead(c, L("Keys"), note, func() {
		a.keyModePicker(c, u, st)
		if len(m.cards) == 0 {
			return
		}
		label := L("Expand all")
		if allOpen {
			label = L("Collapse all")
		}
		if u.TextButton(c, label) {
			for _, k := range m.cards {
				s.expanded[k.id] = !allOpen
			}
		}
	})
	u.Card(c).Children(func() {
		if len(m.cards) == 0 {
			u.Unavailable(c, dashkit.IconKey, L("The Key Pool has no key yet"), L("Model requests are sent with the keys you add here. Add a Factory API key to start."), nil)
			return
		}
		for i, k := range m.cards {
			if i > 0 {
				u.Hairline(c)
			}
			a.keyRow(c, u, k)
		}
	})
}

// keyModePicker is whether the Key Pool moves droid off a key that runs
// out.
func (a *App) keyModePicker(c *ui.Context, u usageLook, st keypool.State) {
	selected := 0
	if st.Mode == keypool.Manual {
		selected = 1
	}
	if u.Segmented(c, &selected, []string{L("Automatic"), L("Manual")}, L("Key switching"), modeDescription(st.Mode), a.usage.acting != "", false) {
		mode := keypool.Auto
		if selected == 1 {
			mode = keypool.Manual
		}
		pool := a.keyPool()
		a.poolAct("mode", func() error { return pool.SetMode(mode) })
	}
}

func modeDescription(mode keypool.Mode) string {
	if mode == keypool.Manual {
		return L("Manual: Droi keeps the key you chose. When it runs out, the session shows the error.")
	}
	return L("Automatic: when a key runs out, Droi switches to the next one and the request is retried.")
}

// keyRow is one key. Collapsed, it is one line ending with the week and
// month left in each pool; a click shows every window and what can be done
// with the key.
func (a *App) keyRow(c *ui.Context, u usageLook, card usageCard) {
	s := &a.usage
	p := u.P
	k := card.view
	open := s.expanded[card.id]
	ui.Column(c.Key(card.id)).Children(func() {
		head := ui.ButtonBase(c).Justify(ui.Start).Gap(u.Px(8)).Padding(u.Px(9), u.Px(16), u.Px(9), u.Px(12)).Label(k.Key).Expanded(open)
		if !open {
			var tip []string
			for _, t := range []string{k.Cooldown, k.Checked} {
				if t != "" {
					tip = append(tip, t)
				}
			}
			if len(tip) > 0 {
				head.Tooltip(strings.Join(tip, "\n"))
			}
		}
		u.Pressable(head, false)
		head.Children(func() {
			chevron := u.Icon(c, dashkit.IconChevron, 10, p.Tertiary)
			if open {
				chevron.Rotate(90)
			}
			u.Text(c, itoa(k.Index), 11).TextColor(p.Tertiary).FontFeatures("tnum").Width(u.Px(18)).TextAlign(ui.End)
			u.Text(c, k.Key, 12).Font(u.K.Mono).SingleLine().Shrink(1)
			if k.Active {
				ui.Row(c).Gap(u.Px(3)).Tooltip(L("Model requests use this key")).Children(func() {
					u.Icon(c, dashkit.IconStar, 10, p.Warning)
					u.Text(c, L("Current"), 11).FontWeight(500).TextColor(p.Warning)
				})
			}
			u.Badge(c, k.Badge, false)
			ui.Spacer(c)
			if open {
				if k.Cooldown != "" {
					ui.Row(c).Gap(u.Px(4)).Children(func() {
						u.Icon(c, dashkit.IconSnowflake, 11, p.Warning)
						u.Text(c, k.Cooldown, 11).TextColor(p.Warning)
					})
				}
				if k.Checked != "" {
					u.Text(c, k.Checked, 11).TextColor(p.Tertiary)
				}
				return
			}
			for _, pool := range k.Pools {
				u.collapsedPool(c, pool)
			}
		})
		if head.Clicked() {
			s.expanded[card.id] = !open
		}
		if !open {
			return
		}
		ui.Column(c.Key("detail")).Transition(detailMotion).Gap(u.Px(12)).Padding(u.Px(2), u.Px(16), u.Px(14), u.Px(42)).Children(func() {
			ui.Row(c).Gap(u.Px(28)).AlignItems(ui.Start).Children(func() {
				for _, pool := range k.Pools {
					u.poolWindows(c, pool)
				}
			})
			ui.Row(c).Gap(u.Px(8)).Children(func() {
				busy := s.acting != ""
				if !card.current && u.PushButton(c, L("Use this key"), L("Use %s", card.masked), false, busy) {
					pool := a.keyPool()
					a.poolAct(card.id, func() error { return pool.Use(card.id) })
				}
				if u.PushButton(c, L("Remove"), L("Remove %s", card.masked), false, busy) {
					pool := a.keyPool()
					a.poolAct(card.id, func() error { return pool.Remove(card.id) })
				}
			})
		})
	})
}

// collapsedPool is a pool's week and month left on a collapsed key row.
func (u usageLook) collapsedPool(c *ui.Context, pool llmusage.PoolWindowsView) {
	p := u.P
	ui.Row(c.Key(pool.Title)).Gap(u.Px(6)).Padding(0, 0, 0, u.Px(10)).Children(func() {
		u.Text(c, pool.Title, 10).FontWeight(600).TextColor(p.Tertiary).Width(u.Px(52)).NoWrap()
		if !pool.HasLimits {
			u.Text(c, L("No data yet"), 11).TextColor(p.Tertiary).Width(u.Px(118)).TextAlign(ui.End)
			return
		}
		for _, w := range []llmusage.Window{llmusage.Weekly, llmusage.Monthly} {
			text, tone := "—", llmusage.ToneTertiary
			for _, row := range pool.Rows {
				if row.Label == w.Label() && row.Live {
					text, tone = row.Percent, dashkit.PrimaryTone(row.PercentTone)
				}
			}
			u.Text(c, w.Label(), 10).TextColor(p.Tertiary)
			u.Text(c, text, 12).TextColor(u.Tone(tone)).FontFeatures("tnum").Width(u.Px(36)).TextAlign(ui.End)
		}
	})
}

// poolWindows is a pool's windows in full: what is left against an even
// pace (the tick), in credits, and when it resets.
func (u usageLook) poolWindows(c *ui.Context, pool llmusage.PoolWindowsView) {
	p := u.P
	ui.Column(c.Key(pool.Title)).Role(ui.RoleGroup).Label(pool.Title).Grow(1).Basis(0).MinWidth(0).Gap(u.Px(7)).Children(func() {
		ui.Row(c).Children(func() {
			u.Text(c, pool.Title, 12).FontWeight(600).TextColor(p.Secondary)
			ui.Spacer(c)
			if pool.Available != "" {
				u.Text(c, pool.Available, 11).TextColor(u.Tone(pool.AvailableTone)).Tooltip(L("The lowest quota left across the windows. The Key Pool uses it to decide whether to use this key."))
			}
		})
		if !pool.HasLimits {
			u.Text(c, L("No quota data"), 11).TextColor(p.Tertiary)
			return
		}
		for _, row := range pool.Rows {
			r := ui.Row(c.Key(row.Label)).Gap(u.Px(8))
			if row.HasPace {
				r.Tooltip(L("The tick is where an even pace would be. %s", row.Pace))
			}
			r.Children(func() {
				u.Text(c, row.Label, 11).TextColor(p.Secondary).Width(u.Px(36)).NoWrap()
				mark := -1.0
				if row.HasPace {
					mark = row.Even
				}
				u.Meter(c, row.Bar.Value, row.Bar.Tone, 5, mark).Label(row.Label).MinWidth(u.Px(64))
				u.Text(c, row.Percent, 11).FontWeight(500).TextColor(u.Tone(dashkit.PrimaryTone(row.PercentTone))).FontFeatures("tnum").Width(u.Px(36)).TextAlign(ui.End)
				ui.Row(c).Width(u.Px(80)).Gap(u.Px(4)).Children(func() {
					if row.Credits == "" {
						return
					}
					u.Text(c, row.Credits, 11).TextColor(p.Tertiary).FontFeatures("tnum").SingleLine()
					ui.Spacer(c)
					u.tokensInfo(c, row.Tokens)
				})
				// The note gives way first when the column is narrower than
				// droid-proxy's window, which has no Settings sections beside it.
				u.Text(c, row.Note, 11).TextColor(p.Tertiary).FontFeatures("tnum").Width(u.Px(120)).Shrink(1).MinWidth(0).SingleLine()
			})
		}
	})
}

func (a *App) warmingPage(c *ui.Context, u usageLook, m *usageModel) {
	p := u.P
	v := m.warming
	if !v.Available {
		u.Unavailable(c, dashkit.IconWaveform, L("Cache warming is not running"), L("Cache warming runs with the Key Pool, which starts once it has a key."), nil)
		return
	}
	u.page(c, func() {
		u.SectionHead(c, L("Mode"), v.Summary, nil)
		u.Card(c).Padding(u.Px(12), u.Px(14)).Gap(u.Px(10)).Children(func() {
			selected := 0
			labels := make([]string, len(v.Options))
			for i, o := range v.Options {
				labels[i] = o.Label
				if o.Selected {
					selected = i
				}
			}
			if u.Segmented(c, &selected, labels, L("Cache warming mode"), v.Help, a.usage.acting != "", true) {
				pool, mode := a.keyPool(), v.Options[selected].Value
				a.poolAct("warming", func() error { return pool.SetCacheWarmingMode(mode) })
			}
			u.Text(c, v.Help, 12).TextColor(p.Secondary)
		})
		u.MetricStrip(c, v.Metrics)
		ui.Box(c).Height(u.Px(6))
		u.SectionHead(c, L("Sessions"), v.SessionsNote, nil)
		if len(v.Sessions) == 0 {
			u.Card(c).Padding(u.Px(16)).Children(func() {
				u.Text(c, v.EmptyText, 12).TextColor(p.Secondary)
			})
			return
		}
		cols := []struct {
			title string
			width float32
			end   bool
		}{{L("Session"), 90, false}, {L("Model"), 0, false}, {L("Context"), 72, true}, {L("Renewed"), 56, true}, {L("Next check"), 96, true}, {L("Status"), 64, false}}
		cell := func(i int, s string, color ui.Color) ui.Element {
			t := u.Text(c, s, 12).TextColor(color).SingleLine()
			if cols[i].width > 0 {
				t.Width(u.Px(cols[i].width))
			} else {
				t.Grow(1).Basis(0)
			}
			if cols[i].end {
				t.TextAlign(ui.End)
			}
			return t
		}
		u.Card(c).Padding(2, u.Px(16)).Children(func() {
			ui.Row(c).Gap(u.Px(10)).Height(u.Px(32)).Children(func() {
				for i, col := range cols {
					cell(i, col.title, p.Tertiary).FontWeight(600).FontSize(u.Px(11))
				}
			})
			for _, s := range v.Sessions {
				u.Hairline(c)
				ui.Row(c.Key(s.ID)).Gap(u.Px(10)).Height(u.Px(32)).Children(func() {
					cell(0, s.Session, p.Text).Font(u.K.Mono).Tooltip(s.ID)
					cell(1, s.Model, p.Text)
					cell(2, s.Context, p.Text).FontFeatures("tnum")
					cell(3, s.Refreshes, p.Text).FontFeatures("tnum")
					cell(4, s.Next, p.Secondary).FontFeatures("tnum")
					cell(5, s.State, u.Tone(s.StateTone))
				})
			}
		})
	})
}

func (a *App) requestsTable(c *ui.Context, u usageLook, rows []llmusage.RequestRowView) ui.Element {
	p := u.P
	s := &a.usage.observe
	columns := []ui.TableColumn{
		{ID: "time", Title: L("Time"), Width: u.Px(126)},
		{ID: "model", Title: L("Model"), MinWidth: u.Px(112)},
		{ID: "status", Title: L("Status"), Width: u.Px(64)},
		{ID: "input", Title: L("Input"), Width: u.Px(70), Align: ui.End},
		{ID: "cache", Title: L("Cache"), Width: u.Px(52), Align: ui.End},
		{ID: "output", Title: L("Output"), Width: u.Px(60), Align: ui.End},
		{ID: "credits", Title: L("Credits"), Width: u.Px(76), Align: ui.End},
		{ID: "duration", Title: L("Time / first byte"), Width: u.Px(96), Align: ui.End},
		{ID: "speed", Title: L("Output speed"), Width: u.Px(72), Align: ui.End},
	}
	s.reqList.Key = func(i int) any { return rows[i].ID }
	s.reqList.Selected = &s.reqSel
	defer u.Tables(c)()
	numeric := func(cell llmusage.Cell) {
		ui.Text(c, cell.Text).TextColor(u.Tone(cell.Tone)).FontFeatures("tnum").Tooltip(cell.Help).SingleLine()
	}
	return ui.Table(c, &s.reqList, columns, len(rows), func(i, col int) {
		r := rows[i]
		switch col {
		case 0:
			ui.Text(c, r.Time).TextColor(p.Secondary).FontFeatures("tnum").Tooltip(r.TimeHelp).SingleLine()
		case 1:
			ui.Row(c).Gap(u.Px(6)).Children(func() {
				u.Text(c, r.ProviderMark, 9).FontWeight(700).TextColor(ui.Hex("#ffffff")).TextAlign(ui.Center).
					Size(u.Px(20), u.Px(16)).FixedLineHeight(u.Px(16)).Radius(u.Px(4)).Background(providerColor(r.ProviderClass)).Tooltip(r.ProviderHelp).NoWrap()
				u.Text(c, r.Model, 11).Font(u.K.Mono).SingleLine().Shrink(1).Tooltip(r.Model)
				if r.Kind != "" {
					u.Text(c, r.Kind, 10).TextColor(p.Tertiary).NoWrap()
				}
			})
		case 2:
			ui.Row(c).Gap(u.Px(4)).Tooltip(r.StatusHelp).Children(func() {
				u.Badge(c, llmusage.Badge{Label: r.Status, Tone: r.StatusTone}, true).FontWeight(600)
				if r.Stream {
					u.Icon(c, dashkit.IconWaveform, 10, p.Tertiary).Label(L("Streamed response"))
				}
			})
		case 3:
			numeric(r.Input)
		case 4:
			numeric(r.Cache)
		case 5:
			numeric(r.Output)
		case 6:
			numeric(r.Credits)
		case 7:
			ui.Column(c).AlignItems(ui.End).Tooltip(L("Top: the total time. Bottom: the time to the first byte, how long Factory took to start answering.")).Children(func() {
				ui.Text(c, r.Duration).TextColor(u.Tone(r.DurationTone)).FontFeatures("tnum")
				if r.TTFB != "" {
					u.Text(c, r.TTFB, 10).TextColor(p.Tertiary).FontFeatures("tnum")
				}
			})
		case 8:
			numeric(r.TokensPerSec)
		}
	}).Label(L("Requests")).Grow(1)
}

func (a *App) eventsPage(c *ui.Context, u usageLook, m *usageModel) {
	p := u.P
	s := &a.usage
	events := m.events
	u.tablePage(c, func() {
		u.SectionHead(c, L("Events"), countL(len(events), l10n.N("%d event"), l10n.N("%d events")), nil)
		u.Card(c).Grow(1).MinHeight(0).Children(func() {
			if len(events) == 0 {
				u.Unavailable(c, dashkit.IconList, L("No events yet"), L("Key switches, key cooldowns, and errors from Factory are logged here."), nil)
				return
			}
			columns := []ui.TableColumn{
				{ID: "time", Title: L("Time"), Width: u.Px(80)},
				{ID: "kind", Title: L("Type"), Width: u.Px(200)},
				{ID: "key", Title: L("Key"), Width: u.Px(140)},
				{ID: "pool", Title: L("Pool"), Width: u.Px(70)},
				{ID: "model", Title: L("Model"), MinWidth: u.Px(100)},
				{ID: "status", Title: L("Status"), Width: u.Px(56)},
				{ID: "details", Title: L("Details"), MinWidth: u.Px(120)},
			}
			s.evtList.Key = func(i int) any { return events[i].ID }
			ui.Table(c, &s.evtList, columns, len(events), func(i, col int) {
				e := events[i]
				switch col {
				case 0:
					ui.Text(c, e.Time).TextColor(p.Secondary).FontFeatures("tnum")
				case 1:
					ui.Text(c, e.Name).SingleLine().Tooltip(e.Kind)
				case 2:
					u.Text(c, e.Key, 11).Font(u.K.Mono).TextColor(p.Secondary).SingleLine()
				case 3:
					ui.Text(c, e.Pool).SingleLine()
				case 4:
					ui.Text(c, e.Model).SingleLine()
				case 5:
					ui.Text(c, e.Status).TextColor(u.Tone(e.StatusTone)).FontFeatures("tnum")
				case 6:
					ui.Text(c, e.Detail).TextColor(p.Secondary).SingleLine().Tooltip(e.Detail)
				}
			}).Label(L("Event log")).Grow(1)
		})
	})
}

// usageStatusBar is whether the Key Pool runs and with which key, and how
// the Key Pool switches keys.
func (a *App) usageStatusBar(c *ui.Context, u usageLook, st keypool.State) {
	p := u.P
	symbol, tone := dashkit.IconClock, llmusage.ToneWarning
	text := L("The Key Pool starts once it has a key.")
	switch {
	case st.Running:
		current := "—"
		for _, k := range st.Keys {
			if k.Current {
				current = k.Masked
			}
		}
		symbol, tone, text = dashkit.IconCheck, llmusage.ToneGreen, L("Key Pool running · current key %s", current)
	case st.Err != "" && len(st.Keys) > 0:
		symbol, tone, text = dashkit.IconWarning, llmusage.ToneDanger, L("The Key Pool is not running: %s", st.Err)
	case len(st.Keys) > 0:
		symbol, tone, text = dashkit.IconWarning, llmusage.ToneDanger, L("The Key Pool is not running")
	}
	mode := L("Switches keys automatically")
	if st.Mode == keypool.Manual {
		mode = L("Keeps the key you chose")
	}
	ui.Row(c).Role(ui.RoleStatus).Shrink(0).Gap(u.Px(8)).Padding(u.Px(7), u.Px(20)).BorderWidth(1, 0, 0, 0).BorderColor(p.Line).Children(func() {
		u.Icon(c, symbol, 13, u.Tone(tone))
		u.Text(c, text, 12).TextColor(p.Secondary).SingleLine().Shrink(1)
		ui.Spacer(c)
		u.Text(c, mode, 12).TextColor(p.Tertiary).NoWrap()
	})
}

func (a *App) openAddKey() {
	s := &a.usage
	s.addOpen, s.addKey, s.addErr = true, "", ""
}

// submitAddKey checks the key with Factory's billing endpoint before it
// joins the pool, so a key that cannot serve is refused here and not in a
// session.
func (a *App) submitAddKey() {
	s := &a.usage
	key := strings.TrimSpace(s.addKey)
	pool := a.keyPool()
	if key == "" || s.adding || pool == nil {
		return
	}
	s.adding, s.addErr = true, ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := a.cfg.Host.AddPoolKey(ctx, key)
		a.cfg.Update(func() {
			s.adding = false
			if err != nil {
				s.addErr = err.Error()
				return
			}
			s.addOpen, s.addKey = false, ""
			a.refreshPool()
		})
	}()
}

func (a *App) addKeySheet(c *ui.Context, u usageLook) {
	s := &a.usage
	p := u.P
	open := s.addOpen
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(u.Px(380)).Gap(u.Px(12)).Children(func() {
			ui.Column(c).Gap(u.Px(4)).Children(func() {
				u.Text(c, L("Add key"), 14).Role(ui.RoleHeading).FontWeight(600)
				u.Text(c, L("Droi checks the key's quota with Factory first, then adds it to the Key Pool."), 12).TextColor(p.Secondary)
			})
			submitted := ui.TextInput(c, &s.addKey).Password().AutoFocus().Placeholder("fk-…").Label(L("Factory API key to add")).Disabled(s.adding).Submitted()
			if s.addErr != "" {
				ui.Row(c).Role(ui.RoleStatus).Gap(u.Px(4)).Children(func() {
					u.Icon(c, dashkit.IconWarning, 13, p.Danger)
					u.Text(c, s.addErr, 12).TextColor(p.Danger).Shrink(1)
				})
			}
			empty := strings.TrimSpace(s.addKey) == ""
			ui.Row(c).Gap(u.Px(8)).Children(func() {
				if s.adding {
					ui.Spinner(c)
					u.Text(c, L("Checking the key's quota…"), 12).TextColor(p.Secondary)
				}
				ui.Spacer(c)
				if u.PushButton(c, L("Cancel"), "", false, s.adding) {
					open = false
				}
				if u.PushButton(c, L("Add key"), "", true, empty || s.adding) || (submitted && !empty && !s.adding) {
					a.submitAddKey()
				}
			})
		})
	})
	// Escape and clicks outside close the sheet, unless a check is under way.
	if !open && !s.adding {
		s.addOpen = false
	}
}
