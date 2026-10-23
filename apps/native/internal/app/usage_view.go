package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
	"github.com/kkkk2323/droi/apps/native/pkg/keypool"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

// The Usage tab's pages, in the order of its segmented control.
const (
	usageKeys     = "keys"
	usageWarming  = "warming"
	usageRequests = "requests"
	usageEvents   = "events"
)

// How often the tab reads what is not pushed to it: the Key Pool's
// snapshot is cheap, the spend log and the request history are files.
const (
	poolRefresh    = 2 * time.Second
	spendRefresh   = 30 * time.Second
	historyRefresh = 3 * time.Second
	// requestPageSize is one page of the request history.
	requestPageSize = 100
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

	history      *llmusage.History
	historyState llmusage.HistoryState
	historyAt    time.Time

	model usageModel

	// expanded holds the keys shown in full, by key ID.
	expanded         map[string]bool
	reqList, evtList ui.ListState

	addOpen        bool
	addKey, addErr string
	adding         bool

	// acting is what an action runs on, err what the last one said.
	acting, err string
	refreshing  bool
}

// usageModel is what the tab shows, built again when what it is built from
// changes.
type usageModel struct {
	built       bool
	poolAt      time.Time
	spendAt     time.Time
	lang        l10n.Language
	historyFrom int

	pools    []llmusage.PoolSummaryView
	runway   llmusage.RunwayView
	expiring llmusage.ExpiringView
	cards    []usageCard
	problems int
	checked  string
	events   []llmusage.EventRowView
	warming  llmusage.CacheWarmingView
	history  llmusage.HistoryView
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

// requestHistory is the request history's pages, made on first use.
func (a *App) requestHistory() *llmusage.History {
	s := &a.usage
	if s.history == nil {
		pool := a.keyPool()
		h := llmusage.NewHistory(func(_ context.Context, cursor string) (diag.RequestPage, error) {
			return pool.Requests(requestPageSize, cursor)
		})
		h.OnChange = func() { a.cfg.Update(func() { s.historyState = h.State() }) }
		s.history, s.historyState = h, h.State()
	}
	return s.history
}

// pollRequests reloads the newest page every historyRefresh while it is
// shown; an older page stays as it is.
func (a *App) pollRequests() {
	s := &a.usage
	h := a.requestHistory()
	if !s.historyAt.IsZero() && a.cfg.Now().Sub(s.historyAt) < historyRefresh {
		return
	}
	s.historyAt = a.cfg.Now()
	go h.RefreshLatest(context.Background())
}

// enterUsage forgets what the last visit read, so the tab opens on what is
// true now.
func (a *App) enterUsage() {
	s := &a.usage
	s.poolAt, s.spendAt, s.historyAt = time.Time{}, time.Time{}, time.Time{}
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
	if m.built && m.poolAt.Equal(s.poolAt) && m.spendAt.Equal(s.spendAt) && m.lang == lang && m.historyFrom == s.historyState.Version {
		return m
	}
	now := a.cfg.Now()
	pool := keyStates(st)
	var spend *llmusage.Spend
	if s.hasSpend {
		sp := s.spend
		spend = &sp
	}
	*m = usageModel{built: true, poolAt: s.poolAt, spendAt: s.spendAt, lang: lang, historyFrom: s.historyState.Version}
	m.pools = llmusage.PoolSummaries(pool, now)
	m.runway = llmusage.BuildRunway(pool, spend, now)
	m.expiring = llmusage.BuildExpiring(pool, now, spend)
	for _, k := range st.Keys {
		m.cards = append(m.cards, usageCard{id: k.ID, masked: k.Masked, current: k.Current, view: llmusage.BuildKeyCard(k.State, now)})
		if llmusage.KeyDisplayOf(k.State, now).IsProblem() {
			m.problems++
		}
	}
	if checked := llmusage.UsageCheckedAt(pool); checked != nil {
		m.checked = L("Quota checked %s", llmusage.FormatRelative(*checked, now))
	}
	m.events = llmusage.EventRows(st.Events)
	m.warming = llmusage.BuildCacheWarming(st.CacheWarming, now)
	m.history = llmusage.BuildHistory(s.historyState)
	return m
}

type usageSection struct{ id, label string }

func usageSections() []usageSection {
	return []usageSection{
		{usageKeys, L("Keys and quota")},
		{usageWarming, L("Cache warming")},
		{usageRequests, L("Requests")},
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
	st := a.poolState()
	a.loadSpend(false)
	if s.section == usageRequests && st.Running {
		a.pollRequests()
	}
	m := a.buildUsage(st)
	base := c.Theme()
	c.SetTheme(u.themed(base))
	defer c.SetTheme(base)
	ui.Column(c).Role(ui.RoleGroup).Label(L("Usage")).Grow(1).MinWidth(0).FillHeight().Background(u.p.bg).TextColor(u.p.text).FontSize(u.px(13)).Children(func() {
		a.usageHeader(c, u)
		if s.err != "" {
			ui.Row(c).Role(ui.RoleStatus).Shrink(0).Gap(u.px(8)).Padding(0, u.px(24), u.px(6)).Children(func() {
				u.icon(c, iconWarning, 13, u.p.danger)
				u.text(c, s.err, 12).TextColor(u.p.danger).Selectable().Shrink(1)
			})
		}
		ui.Column(c).Grow(1).Basis(0).MinHeight(0).Children(func() {
			switch s.section {
			case usageWarming:
				a.warmingPage(c, u, m)
			case usageRequests:
				a.requestsPage(c, u, st, m)
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
	ui.Row(c).Height(u.px(52)).Shrink(0).Padding(0, u.px(14)).Gap(u.px(12)).DragWindow().Children(func() {
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
		if u.segmented(c, &selected, labels, L("Usage pages"), "", false, false) {
			s.section = sections[selected].id
			s.historyAt = time.Time{}
		}
		ui.Row(c).Grow(1).Basis(0).Gap(2).Justify(ui.End).Children(func() {
			if s.section == usageKeys && u.toolButton(c, iconPlus, L("Add key"), L("Add a Factory API key"), false) {
				a.openAddKey()
			}
			if u.toolButton(c, iconRefresh, L("Refresh"), L("Read the quota and the records again"), s.refreshing) {
				a.refreshQuota()
				a.loadSpend(true)
				if s.history != nil {
					go s.history.Reload(context.Background())
				}
			}
		})
	})
}

// page is a page's scrolling column, as wide as droid-proxy's window at
// most.
func (u usageLook) page(c *ui.Context, fn func()) {
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(u.px(960)).AlignSelf(ui.Center).Padding(u.px(4), u.px(20), u.px(20)).Gap(u.px(10)).Children(fn)
	})
}

// tablePage is a page whose table grows to the room left.
func (u usageLook) tablePage(c *ui.Context, fn func()) {
	ui.Column(c).Grow(1).MinHeight(0).FillWidth().MaxWidth(u.px(960)).AlignSelf(ui.Center).Padding(u.px(4), u.px(20), u.px(16)).Gap(u.px(10)).Children(fn)
}

func (a *App) keysPage(c *ui.Context, u usageLook, st keypool.State, m *usageModel) {
	u.page(c, func() {
		u.runwaySection(c, m.runway)
		ui.Box(c).Height(u.px(6))
		u.sectionHead(c, L("Quota"), m.checked, nil)
		u.poolCards(c, m.pools)
		ui.Box(c).Height(u.px(6))
		u.expiring(c, m.expiring)
		ui.Box(c).Height(u.px(6))
		a.keyList(c, u, st, m)
	})
}

// runwaySection is today's spend, the last days' spend against the daily
// limit, and how long the pool lasts.
func (u usageLook) runwaySection(c *ui.Context, r llmusage.RunwayView) {
	p := u.p
	u.sectionHead(c, L("Today"), "", func() { u.info(c, L("About the forecast"), r.Help, 12) })
	if !r.Available {
		u.card(c).Padding(u.px(16)).Children(func() {
			u.text(c, r.Empty, 12).TextColor(p.secondary)
		})
		return
	}
	u.metricStrip(c, r.Metrics)
	u.card(c).Padding(u.px(14), u.px(16), u.px(12)).Gap(u.px(12)).Children(func() {
		ui.Row(c).Gap(u.px(8)).Children(func() {
			u.text(c, L("Last %d days", llmusage.SpendChartDays), 12).FontWeight(600).TextColor(p.secondary)
			ui.Spacer(c)
			ui.Row(c).Gap(u.px(6)).Children(func() {
				ui.Box(c).Width(u.px(14)).Height(u.px(8)).Draw(func(pt *ui.Painter, rc ui.Rect) {
					y := rc.Y + rc.H/2
					pt.Line(rc.X, y, rc.X+5, y, 1, p.secondary)
					pt.Line(rc.X+8, y, rc.X+13, y, 1, p.secondary)
				})
				u.text(c, r.Chart.LimitLabel, 11).TextColor(p.secondary).FontFeatures("tnum")
			})
		})
		u.chart(c, r.Chart, 96)
		u.hairline(c)
		for i, l := range r.Lines {
			ui.Row(c.Key(i)).Gap(u.px(8)).Children(func() {
				u.text(c, l.Label, 12).TextColor(p.secondary).FontFeatures("tnum").SingleLine().Shrink(1)
				ui.Spacer(c)
				u.text(c, l.Note, 12).TextColor(p.tertiary).FontFeatures("tnum").NoWrap()
				u.text(c, l.Value, 12).FontWeight(600).TextColor(u.tone(l.Tone)).FontFeatures("tnum").NoWrap().Width(u.px(150)).TextAlign(ui.End)
			})
		}
	})
}

// poolCard is one pool: its name, how many keys it routes, then a row per
// window with what is left on average and the soonest reset.
func (u usageLook) poolCard(c *ui.Context, pool llmusage.PoolSummaryView) {
	p := u.p
	pad := u.px(14)
	ui.Column(c.Key(pool.Title)).Role(ui.RoleGroup).Label(pool.Title).Grow(1).Basis(0).MinWidth(0).Gap(u.px(10)).Padding(pad-2, pad, pad).Children(func() {
		ui.Row(c).Gap(u.px(6)).Children(func() {
			u.text(c, pool.Title, 13).FontWeight(600)
			ui.Spacer(c)
			u.text(c, pool.Note, 11).TextColor(p.tertiary).FontFeatures("tnum").SingleLine()
		})
		for _, w := range pool.Windows {
			ui.Row(c.Key(w.Label)).Gap(u.px(8)).Tooltip(w.Help).Children(func() {
				u.text(c, w.Label, 11).TextColor(p.secondary).Width(u.px(36)).NoWrap()
				u.meter(c, w.Bar.Value, w.Bar.Tone, 5, -1).Label(w.Name)
				u.text(c, w.Percent, 12).FontWeight(600).TextColor(u.tone(primaryTone(w.PercentTone))).FontFeatures("tnum").Width(u.px(38)).TextAlign(ui.End)
				u.text(c, w.Reset, 11).TextColor(p.tertiary).FontFeatures("tnum").Width(u.px(150)).SingleLine()
			})
		}
	})
}

// poolCards puts the pools side by side in one card.
func (u usageLook) poolCards(c *ui.Context, pools []llmusage.PoolSummaryView) {
	u.card(c).Children(func() {
		ui.Row(c).AlignItems(ui.Stretch).Children(func() {
			for i, pool := range pools {
				if i > 0 {
					u.vhairline(c)
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
	p := u.p
	u.sectionHead(c, L("Expiring"), v.Summary, func() { u.info(c, L("About expiring quota"), v.Help, 12) })
	u.card(c).Padding(u.px(14), u.px(16)).Gap(u.px(14)).Children(func() {
		if len(v.Pools) == 0 {
			ui.Row(c).Gap(u.px(6)).Children(func() {
				u.icon(c, iconCheck, 13, p.green)
				u.text(c, v.Empty, 12).TextColor(p.secondary)
			})
			return
		}
		for i, pool := range v.Pools {
			if i > 0 {
				u.hairline(c)
			}
			ui.Column(c.Key(pool.Title)).Gap(u.px(12)).Children(func() {
				ui.Row(c).Gap(u.px(8)).AlignItems(ui.End).Children(func() {
					u.text(c, v.Credits, 20).FontWeight(600).LetterSpacing(-0.3).FontFeatures("tnum")
					u.text(c, L("credits expire"), 11).TextColor(p.tertiary).Padding(0, 0, u.px(3))
					if v.Loss != "" {
						u.text(c, v.Loss, 11).FontWeight(500).TextColor(p.danger).FontFeatures("tnum").Padding(0, 0, u.px(3))
					}
					ui.Spacer(c)
					ui.Column(c).AlignItems(ui.End).Gap(2).Tooltip(pool.PaceHelp).Children(func() {
						u.text(c, pool.Pace, 12).FontWeight(500).TextColor(u.tone(primaryTone(pool.PaceTone))).FontFeatures("tnum")
						if pool.Recent != "" {
							u.text(c, pool.Recent, 11).TextColor(p.tertiary).FontFeatures("tnum")
						}
					})
				})
				ui.Row(c).Gap(u.px(8)).Children(func() {
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
	p := u.p
	ui.Column(c.Key(b.Label)).Role(ui.RoleGroup).Label(b.Label).Grow(1).Basis(0).MinWidth(0).Gap(2).Padding(u.px(9), u.px(12)).Radius(u.px(8)).Background(p.bg).Children(func() {
		u.text(c, b.Label, 11).TextColor(p.secondary)
		u.text(c, b.Value, 17).FontWeight(600).TextColor(u.tone(b.Tone)).FontFeatures("tnum")
		u.text(c, b.Note, 10).TextColor(p.tertiary).FontFeatures("tnum").SingleLine()
	})
}

// expiringGroup is one deadline: when, how many keys, then the keys.
func (u usageLook) expiringGroup(c *ui.Context, key int, g llmusage.ExpiringGroupView) {
	p := u.p
	ui.Column(c.Key(key)).Gap(u.px(8)).Children(func() {
		ui.Row(c).Gap(u.px(8)).Children(func() {
			ui.Box(c).Size(u.px(6), u.px(6)).Radius(999).Background(u.tone(g.Tone))
			u.text(c, g.Countdown, 13).FontWeight(600).TextColor(u.tone(primaryTone(quietTone(g.Tone)))).FontFeatures("tnum")
			u.text(c, g.At, 11).TextColor(p.tertiary).FontFeatures("tnum")
			ui.Spacer(c)
			u.text(c, g.Note, 11).TextColor(p.secondary).FontFeatures("tnum")
		})
		ui.Grid(c).Columns(6).Gap(u.px(6)).Children(func() {
			for _, k := range g.Keys {
				u.expiringChip(c, k)
			}
		})
	})
}

// expiringChip is one key: its number and what is left, a bar under them,
// then what needs attention.
func (u usageLook) expiringChip(c *ui.Context, k llmusage.ExpiringChipView) {
	p := u.p
	ui.Column(c.Key(k.Index)).MinWidth(0).Gap(u.px(6)).Padding(u.px(7), u.px(9)).Radius(u.px(8)).Background(p.bg).Tooltip(k.Help).Children(func() {
		ui.Row(c).Gap(u.px(4)).Children(func() {
			u.text(c, "#"+itoa(k.Index), 12).FontWeight(600).FontFeatures("tnum")
			if k.Active {
				u.icon(c, iconStar, 9, p.warning)
			}
			ui.Spacer(c)
			u.text(c, k.Credits, 11).TextColor(p.secondary).FontFeatures("tnum").SingleLine().Shrink(1)
			u.tokensInfo(c, k.Tokens)
		})
		u.meter(c, k.Bar.Value, k.Bar.Tone, 3, -1).Label(k.Percent)
		if k.Note != "" {
			u.text(c, k.Note, 10).FontWeight(500).TextColor(u.tone(k.NoteTone)).FontFeatures("tnum").SingleLine()
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
	u.sectionHead(c, L("Keys"), note, func() {
		a.keyModePicker(c, u, st)
		if len(m.cards) == 0 {
			return
		}
		label := L("Expand all")
		if allOpen {
			label = L("Collapse all")
		}
		if u.textButton(c, label) {
			for _, k := range m.cards {
				s.expanded[k.id] = !allOpen
			}
		}
	})
	u.card(c).Children(func() {
		if len(m.cards) == 0 {
			u.unavailable(c, iconKey, L("The Key Pool has no key yet"), L("Model requests are sent with the keys you add here. Add a Factory API key to start."), nil)
			return
		}
		for i, k := range m.cards {
			if i > 0 {
				u.hairline(c)
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
	if u.segmented(c, &selected, []string{L("Automatic"), L("Manual")}, L("Key switching"), modeDescription(st.Mode), a.usage.acting != "", false) {
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
	p := u.p
	k := card.view
	open := s.expanded[card.id]
	ui.Column(c.Key(card.id)).Children(func() {
		head := ui.ButtonBase(c).Justify(ui.Start).Gap(u.px(8)).Padding(u.px(9), u.px(16), u.px(9), u.px(12)).Label(k.Key).Expanded(open)
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
		u.pressable(head, false)
		head.Children(func() {
			chevron := u.icon(c, iconChevron, 10, p.tertiary)
			if open {
				chevron.Rotate(90)
			}
			u.text(c, itoa(k.Index), 11).TextColor(p.tertiary).FontFeatures("tnum").Width(u.px(18)).TextAlign(ui.End)
			u.text(c, k.Key, 12).Font(u.k.Mono).SingleLine().Shrink(1)
			if k.Active {
				ui.Row(c).Gap(u.px(3)).Tooltip(L("Model requests use this key")).Children(func() {
					u.icon(c, iconStar, 10, p.warning)
					u.text(c, L("Current"), 11).FontWeight(500).TextColor(p.warning)
				})
			}
			u.badge(c, k.Badge, false)
			ui.Spacer(c)
			if open {
				if k.Cooldown != "" {
					ui.Row(c).Gap(u.px(4)).Children(func() {
						u.icon(c, iconSnowflake, 11, p.warning)
						u.text(c, k.Cooldown, 11).TextColor(p.warning)
					})
				}
				if k.Checked != "" {
					u.text(c, k.Checked, 11).TextColor(p.tertiary)
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
		ui.Column(c.Key("detail")).Transition(detailMotion).Gap(u.px(12)).Padding(u.px(2), u.px(16), u.px(14), u.px(42)).Children(func() {
			ui.Row(c).Gap(u.px(28)).AlignItems(ui.Start).Children(func() {
				for _, pool := range k.Pools {
					u.poolWindows(c, pool)
				}
			})
			ui.Row(c).Gap(u.px(8)).Children(func() {
				busy := s.acting != ""
				if !card.current && u.pushButton(c, L("Use this key"), L("Use %s", card.masked), false, busy) {
					pool := a.keyPool()
					a.poolAct(card.id, func() error { return pool.Use(card.id) })
				}
				if u.pushButton(c, L("Remove"), L("Remove %s", card.masked), false, busy) {
					pool := a.keyPool()
					a.poolAct(card.id, func() error { return pool.Remove(card.id) })
				}
			})
		})
	})
}

// collapsedPool is a pool's week and month left on a collapsed key row.
func (u usageLook) collapsedPool(c *ui.Context, pool llmusage.PoolWindowsView) {
	p := u.p
	ui.Row(c.Key(pool.Title)).Gap(u.px(6)).Padding(0, 0, 0, u.px(10)).Children(func() {
		u.text(c, pool.Title, 10).FontWeight(600).TextColor(p.tertiary).Width(u.px(52)).NoWrap()
		if !pool.HasLimits {
			u.text(c, L("No data yet"), 11).TextColor(p.tertiary).Width(u.px(118)).TextAlign(ui.End)
			return
		}
		for _, w := range []llmusage.Window{llmusage.Weekly, llmusage.Monthly} {
			text, tone := "—", llmusage.ToneTertiary
			for _, row := range pool.Rows {
				if row.Label == w.Label() && row.Live {
					text, tone = row.Percent, primaryTone(row.PercentTone)
				}
			}
			u.text(c, w.Label(), 10).TextColor(p.tertiary)
			u.text(c, text, 12).TextColor(u.tone(tone)).FontFeatures("tnum").Width(u.px(36)).TextAlign(ui.End)
		}
	})
}

// poolWindows is a pool's windows in full: what is left against an even
// pace (the tick), in credits, and when it resets.
func (u usageLook) poolWindows(c *ui.Context, pool llmusage.PoolWindowsView) {
	p := u.p
	ui.Column(c.Key(pool.Title)).Role(ui.RoleGroup).Label(pool.Title).Grow(1).Basis(0).MinWidth(0).Gap(u.px(7)).Children(func() {
		ui.Row(c).Children(func() {
			u.text(c, pool.Title, 12).FontWeight(600).TextColor(p.secondary)
			ui.Spacer(c)
			if pool.Available != "" {
				u.text(c, pool.Available, 11).TextColor(u.tone(pool.AvailableTone)).Tooltip(L("The lowest quota left across the windows. The Key Pool uses it to decide whether to use this key."))
			}
		})
		if !pool.HasLimits {
			u.text(c, L("No quota data"), 11).TextColor(p.tertiary)
			return
		}
		for _, row := range pool.Rows {
			r := ui.Row(c.Key(row.Label)).Gap(u.px(8))
			if row.HasPace {
				r.Tooltip(L("The tick is where an even pace would be. %s", row.Pace))
			}
			r.Children(func() {
				u.text(c, row.Label, 11).TextColor(p.secondary).Width(u.px(36)).NoWrap()
				mark := -1.0
				if row.HasPace {
					mark = row.Even
				}
				u.meter(c, row.Bar.Value, row.Bar.Tone, 5, mark).Label(row.Label).MinWidth(u.px(64))
				u.text(c, row.Percent, 11).FontWeight(500).TextColor(u.tone(primaryTone(row.PercentTone))).FontFeatures("tnum").Width(u.px(36)).TextAlign(ui.End)
				ui.Row(c).Width(u.px(80)).Gap(u.px(4)).Children(func() {
					if row.Credits == "" {
						return
					}
					u.text(c, row.Credits, 11).TextColor(p.tertiary).FontFeatures("tnum").SingleLine()
					ui.Spacer(c)
					u.tokensInfo(c, row.Tokens)
				})
				// The note gives way first when the column is narrower than
				// droid-proxy's window, which has no Settings sections beside it.
				u.text(c, row.Note, 11).TextColor(p.tertiary).FontFeatures("tnum").Width(u.px(120)).Shrink(1).MinWidth(0).SingleLine()
			})
		}
	})
}

func (a *App) warmingPage(c *ui.Context, u usageLook, m *usageModel) {
	p := u.p
	v := m.warming
	if !v.Available {
		u.unavailable(c, iconWaveform, L("Cache warming is not running"), L("Cache warming runs with the Key Pool, which starts once it has a key."), nil)
		return
	}
	u.page(c, func() {
		u.sectionHead(c, L("Mode"), v.Summary, nil)
		u.card(c).Padding(u.px(12), u.px(14)).Gap(u.px(10)).Children(func() {
			selected := 0
			labels := make([]string, len(v.Options))
			for i, o := range v.Options {
				labels[i] = o.Label
				if o.Selected {
					selected = i
				}
			}
			if u.segmented(c, &selected, labels, L("Cache warming mode"), v.Help, a.usage.acting != "", true) {
				pool, mode := a.keyPool(), v.Options[selected].Value
				a.poolAct("warming", func() error { return pool.SetCacheWarmingMode(mode) })
			}
			u.text(c, v.Help, 12).TextColor(p.secondary)
		})
		u.metricStrip(c, v.Metrics)
		ui.Box(c).Height(u.px(6))
		u.sectionHead(c, L("Sessions"), v.SessionsNote, nil)
		if len(v.Sessions) == 0 {
			u.card(c).Padding(u.px(16)).Children(func() {
				u.text(c, v.EmptyText, 12).TextColor(p.secondary)
			})
			return
		}
		cols := []struct {
			title string
			width float32
			end   bool
		}{{L("Session"), 90, false}, {L("Model"), 0, false}, {L("Context"), 72, true}, {L("Renewed"), 56, true}, {L("Next check"), 96, true}, {L("Status"), 64, false}}
		cell := func(i int, s string, color ui.Color) ui.Element {
			t := u.text(c, s, 12).TextColor(color).SingleLine()
			if cols[i].width > 0 {
				t.Width(u.px(cols[i].width))
			} else {
				t.Grow(1).Basis(0)
			}
			if cols[i].end {
				t.TextAlign(ui.End)
			}
			return t
		}
		u.card(c).Padding(2, u.px(16)).Children(func() {
			ui.Row(c).Gap(u.px(10)).Height(u.px(32)).Children(func() {
				for i, col := range cols {
					cell(i, col.title, p.tertiary).FontWeight(600).FontSize(u.px(11))
				}
			})
			for _, s := range v.Sessions {
				u.hairline(c)
				ui.Row(c.Key(s.ID)).Gap(u.px(10)).Height(u.px(32)).Children(func() {
					cell(0, s.Session, p.text).Font(u.k.Mono).Tooltip(s.ID)
					cell(1, s.Model, p.text)
					cell(2, s.Context, p.text).FontFeatures("tnum")
					cell(3, s.Refreshes, p.text).FontFeatures("tnum")
					cell(4, s.Next, p.secondary).FontFeatures("tnum")
					cell(5, s.State, u.tone(s.StateTone))
				})
			}
		})
	})
}

func (a *App) requestsPage(c *ui.Context, u usageLook, st keypool.State, m *usageModel) {
	p := u.p
	if !st.Running {
		desc := L("Model requests show here once the Key Pool runs.")
		if st.Err != "" {
			desc = st.Err
		}
		u.unavailable(c, iconOffline, L("The Key Pool is not running"), desc, nil)
		return
	}
	h := m.history
	history := a.requestHistory()
	u.tablePage(c, func() {
		if len(h.Summary) > 0 {
			u.metricStrip(c, h.Summary)
		}
		if h.Error != "" {
			ui.Row(c).Role(ui.RoleStatus).Gap(u.px(8)).Padding(0, u.px(4)).Children(func() {
				u.icon(c, iconWarning, 13, p.danger)
				u.text(c, h.Error, 12).TextColor(p.danger).Selectable().Shrink(1)
				ui.Spacer(c)
				if u.pushButton(c, L("Retry"), "", false, h.IsLoading) {
					go history.Reload(context.Background())
				}
			})
		}
		ui.Row(c).Gap(u.px(10)).Padding(0, u.px(4)).Children(func() {
			u.text(c, h.PageText, 12).FontFeatures("tnum").NoWrap()
			u.text(c, h.ModeText+" · "+L("%d per page", requestPageSize), 12).TextColor(p.tertiary).SingleLine().Shrink(1)
			if h.IsLoading {
				ui.Spinner(c).Label(L("Reading"))
			}
			ui.Spacer(c)
			if u.pushButton(c, L("Back to latest"), "", false, h.IsLoading || h.IsLatest) {
				go history.Latest(context.Background())
			}
			if u.pushButton(c, L("Previous page"), "", false, h.IsLoading || h.IsLatest) {
				go history.Previous(context.Background())
			}
			if u.pushButton(c, L("Next page"), "", false, h.IsLoading || !h.HasNext) {
				go history.Next(context.Background())
			}
		})
		u.card(c).Grow(1).MinHeight(0).Children(func() {
			if len(h.Records) == 0 {
				if h.IsLoading {
					ui.Column(c).Grow(1).Center().Gap(u.px(8)).Children(func() {
						ui.Spinner(c)
						u.text(c, L("Reading the request history…"), 12).TextColor(p.secondary)
					})
				} else {
					u.unavailable(c, iconArrows, h.EmptyTitle, L("Model requests sent through the Key Pool show here. They are logged on this computer and kept across restarts."), nil)
				}
				return
			}
			a.requestsTable(c, u, h.Records)
		})
	})
}

func (a *App) requestsTable(c *ui.Context, u usageLook, rows []llmusage.RequestRowView) {
	p := u.p
	s := &a.usage
	columns := []ui.TableColumn{
		{ID: "time", Title: L("Time"), Width: u.px(120)},
		{ID: "model", Title: L("Model"), MinWidth: u.px(160)},
		{ID: "status", Title: L("Status"), Width: u.px(80)},
		{ID: "input", Title: L("Input"), Width: u.px(64), Align: ui.End},
		{ID: "cache", Title: L("Cache"), Width: u.px(56), Align: ui.End},
		{ID: "output", Title: L("Output"), Width: u.px(64), Align: ui.End},
		{ID: "duration", Title: L("Time / first byte"), Width: u.px(96), Align: ui.End},
		{ID: "speed", Title: L("Speed"), Width: u.px(56), Align: ui.End},
		{ID: "switches", Title: L("Key switches"), Width: u.px(64), Align: ui.End},
	}
	s.reqList.Key = func(i int) any { return rows[i].ID }
	numeric := func(cell llmusage.Cell) {
		ui.Text(c, cell.Text).TextColor(u.tone(cell.Tone)).FontFeatures("tnum").Tooltip(cell.Help).SingleLine()
	}
	ui.Table(c, &s.reqList, columns, len(rows), func(i, col int) {
		r := rows[i]
		switch col {
		case 0:
			ui.Text(c, r.Time).TextColor(p.secondary).FontFeatures("tnum").Tooltip(r.TimeHelp).SingleLine()
		case 1:
			ui.Row(c).Gap(u.px(6)).Children(func() {
				u.text(c, r.ProviderMark, 9).FontWeight(700).TextColor(ui.Hex("#ffffff")).TextAlign(ui.Center).
					Size(u.px(20), u.px(16)).FixedLineHeight(u.px(16)).Radius(u.px(4)).Background(providerColor(r.ProviderClass)).Tooltip(r.ProviderHelp).NoWrap()
				u.text(c, r.Model, 11).Font(u.k.Mono).SingleLine().Shrink(1)
			})
		case 2:
			ui.Row(c).Gap(u.px(4)).Tooltip(r.StatusHelp).Children(func() {
				u.badge(c, llmusage.Badge{Label: r.Status, Tone: r.StatusTone}, true).FontWeight(600)
				if r.Stream {
					u.icon(c, iconWaveform, 10, p.tertiary).Label(L("Streamed response"))
				}
			})
		case 3:
			numeric(r.Input)
		case 4:
			numeric(r.Cache)
		case 5:
			numeric(r.Output)
		case 6:
			ui.Column(c).AlignItems(ui.End).Tooltip(L("Top: the total time. Bottom: the time to the first byte, how long Factory took to start answering.")).Children(func() {
				ui.Text(c, r.Duration).TextColor(u.tone(r.DurationTone)).FontFeatures("tnum")
				if r.TTFB != "" {
					u.text(c, r.TTFB, 10).TextColor(p.tertiary).FontFeatures("tnum")
				}
			})
		case 7:
			numeric(r.TokensPerSec)
		case 8:
			numeric(r.Retries)
		}
	}).Label(L("Requests")).Grow(1)
}

func (a *App) eventsPage(c *ui.Context, u usageLook, m *usageModel) {
	p := u.p
	s := &a.usage
	events := m.events
	u.tablePage(c, func() {
		u.sectionHead(c, L("Events"), countL(len(events), l10n.N("%d event"), l10n.N("%d events")), nil)
		u.card(c).Grow(1).MinHeight(0).Children(func() {
			if len(events) == 0 {
				u.unavailable(c, iconList, L("No events yet"), L("Key switches, key cooldowns, and errors from Factory are logged here."), nil)
				return
			}
			columns := []ui.TableColumn{
				{ID: "time", Title: L("Time"), Width: u.px(80)},
				{ID: "kind", Title: L("Type"), Width: u.px(160)},
				{ID: "key", Title: L("Key"), Width: u.px(140)},
				{ID: "pool", Title: L("Pool"), Width: u.px(70)},
				{ID: "model", Title: L("Model"), MinWidth: u.px(100)},
				{ID: "status", Title: L("Status"), Width: u.px(56)},
				{ID: "details", Title: L("Details"), MinWidth: u.px(120)},
			}
			s.evtList.Key = func(i int) any { return events[i].ID }
			ui.Table(c, &s.evtList, columns, len(events), func(i, col int) {
				e := events[i]
				switch col {
				case 0:
					ui.Text(c, e.Time).TextColor(p.secondary).FontFeatures("tnum")
				case 1:
					u.text(c, e.Kind, 11).Font(u.k.Mono).SingleLine()
				case 2:
					u.text(c, e.Key, 11).Font(u.k.Mono).TextColor(p.secondary).SingleLine()
				case 3:
					ui.Text(c, e.Pool).SingleLine()
				case 4:
					ui.Text(c, e.Model).SingleLine()
				case 5:
					ui.Text(c, e.Status).TextColor(u.tone(e.StatusTone)).FontFeatures("tnum")
				case 6:
					ui.Text(c, e.Detail).TextColor(p.secondary).SingleLine().Tooltip(e.Detail)
				}
			}).Label(L("Event log")).Grow(1)
		})
	})
}

// usageStatusBar is whether the Key Pool runs and with which key, and how
// the Key Pool switches keys.
func (a *App) usageStatusBar(c *ui.Context, u usageLook, st keypool.State) {
	p := u.p
	symbol, tone := iconClock, llmusage.ToneWarning
	text := L("The Key Pool starts once it has a key.")
	switch {
	case st.Running:
		current := "—"
		for _, k := range st.Keys {
			if k.Current {
				current = k.Masked
			}
		}
		symbol, tone, text = iconCheck, llmusage.ToneGreen, L("Key Pool running · current key %s", current)
	case st.Err != "" && len(st.Keys) > 0:
		symbol, tone, text = iconWarning, llmusage.ToneDanger, L("The Key Pool is not running: %s", st.Err)
	case len(st.Keys) > 0:
		symbol, tone, text = iconWarning, llmusage.ToneDanger, L("The Key Pool is not running")
	}
	mode := L("Switches keys automatically")
	if st.Mode == keypool.Manual {
		mode = L("Keeps the key you chose")
	}
	ui.Row(c).Role(ui.RoleStatus).Shrink(0).Gap(u.px(8)).Padding(u.px(7), u.px(20)).BorderWidth(1, 0, 0, 0).BorderColor(p.line).Children(func() {
		u.icon(c, symbol, 13, u.tone(tone))
		u.text(c, text, 12).TextColor(p.secondary).SingleLine().Shrink(1)
		ui.Spacer(c)
		u.text(c, mode, 12).TextColor(p.tertiary).NoWrap()
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
	p := u.p
	open := s.addOpen
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(u.px(380)).Gap(u.px(12)).Children(func() {
			ui.Column(c).Gap(u.px(4)).Children(func() {
				u.text(c, L("Add key"), 14).Role(ui.RoleHeading).FontWeight(600)
				u.text(c, L("Droi checks the key's quota with Factory first, then adds it to the Key Pool."), 12).TextColor(p.secondary)
			})
			submitted := ui.TextInput(c, &s.addKey).Password().AutoFocus().Placeholder("fk-…").Label(L("Factory API key to add")).Disabled(s.adding).Submitted()
			if s.addErr != "" {
				ui.Row(c).Role(ui.RoleStatus).Gap(u.px(4)).Children(func() {
					u.icon(c, iconWarning, 13, p.danger)
					u.text(c, s.addErr, 12).TextColor(p.danger).Shrink(1)
				})
			}
			empty := strings.TrimSpace(s.addKey) == ""
			ui.Row(c).Gap(u.px(8)).Children(func() {
				if s.adding {
					ui.Spinner(c)
					u.text(c, L("Checking the key's quota…"), 12).TextColor(p.secondary)
				}
				ui.Spacer(c)
				if u.pushButton(c, L("Cancel"), "", false, s.adding) {
					open = false
				}
				if u.pushButton(c, L("Add key"), "", true, empty || s.adding) || (submitted && !empty && !s.adding) {
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
