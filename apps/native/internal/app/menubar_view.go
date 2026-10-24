package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
	"github.com/kkkk2323/droi/apps/native/pkg/dashkit"
	"github.com/kkkk2323/droi/apps/native/pkg/keypool"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// MenuBar is what the Key Pool's menu bar item needs from the shell, which
// owns the icon and the panel it opens. Every func may be nil.
type MenuBar struct {
	// Fit sizes the open panel to its content, in DIPs.
	Fit func(width, height float32)
	// Close closes the panel.
	Close func()
	// OpenUsage brings the window forward on Settings → Usage.
	OpenUsage func()
	// Quit quits Droi.
	Quit func()
}

// The panel is as wide as droid-proxy's; it scrolls its keys past
// panelKeyRows of them.
const (
	// MenuBarPanelWidth is the panel's width at the default text size.
	MenuBarPanelWidth = 372
	panelKeyRows      = 8
	panelRowH         = 28
	panelPoolColW     = 64
)

var iconWindow = dashkit.StrokeIcon(`<rect x="3.5" y="5" width="17" height="14" rx="2.5"/><path d="M3.5 9h17"/>`)

// MenuBarShown is whether the shell shows the menu bar item: Advanced's
// setting is on and the Key Pool serves the Daemon.
func (a *App) MenuBarShown() bool {
	return a.cfg.MenuBar != nil && a.usageTabShown() && prefs.MenuBarUsage.Get(a.prefs)
}

// MenuBarItem is the item's title, icon and tooltip. Main thread only.
func (a *App) MenuBarItem() llmusage.MenuBarView {
	s := &a.usage
	st := a.poolState()
	a.loadSpend(false)
	if s.barBuilt && s.barAt.Equal(s.poolAt) && s.barSpendAt.Equal(s.spendAt) && s.barLang == l10n.Code() {
		return s.bar
	}
	now := a.cfg.Now()
	pool := keyStates(st)
	s.bar = llmusage.BuildMenuBar(pool, llmusage.BuildExpiring(pool, now, a.spendOrNil()), now)
	s.barBuilt, s.barAt, s.barSpendAt, s.barLang = true, s.poolAt, s.spendAt, l10n.Code()
	return s.bar
}

func (a *App) spendOrNil() *llmusage.Spend {
	if !a.usage.hasSpend {
		return nil
	}
	sp := a.usage.spend
	return &sp
}

// OpenUsage shows Settings → Usage. Main thread only.
func (a *App) OpenUsage() { a.Go(Route{Name: "settings", Tab: "usage"}) }

// RefreshKeyPool reads every key's quota and the spend log now. Main
// thread only.
func (a *App) RefreshKeyPool() {
	a.refreshQuota()
	a.loadSpend(true)
}

func (a *App) menuBarDo(fn func(*MenuBar) func()) {
	if a.cfg.MenuBar != nil {
		if f := fn(a.cfg.MenuBar); f != nil {
			f()
		}
	}
}

// MenuBarPanel is the panel the menu bar item opens, drawn as
// droid-proxy's: today's spend first, then the pools, what is about to be
// lost, the keys and the cache warming mode.
func (a *App) MenuBarPanel(c *ui.Context) {
	c.SetTheme(a.kit.UITheme(c.Theme()))
	u := a.usageLook(c)
	p := u.P
	base := c.Theme()
	c.SetTheme(u.Themed(base))
	defer c.SetTheme(base)
	c.Root().Background(ui.Transparent)
	st := a.poolState()
	a.loadSpend(false)
	m := a.buildUsage(st)
	width := u.Px(MenuBarPanelWidth)

	box := ui.Column(c).Width(width)
	if !c.Vibrancy() {
		// Where the panel has no material of the system's behind it.
		box.Background(p.Bg)
	}
	box.TextColor(p.Text).FontSize(u.Px(13)).Padding(0, u.Px(12), u.Px(4)).Gap(u.Px(10)).Children(func() {
		a.panelHeader(c, u, st)
		if len(st.Keys) == 0 {
			u.Card(c).Padding(u.Px(22), u.Px(14)).Gap(u.Px(8)).AlignItems(ui.Center).Children(func() {
				u.Icon(c, dashkit.IconKey, 22, p.Faint)
				u.Text(c, L("The Key Pool has no key yet. Open Usage to add one."), 12).TextColor(p.Secondary).TextAlign(ui.Center)
			})
		} else {
			u.budgetCard(c, m.runway)
			u.panelPools(c, m.pools)
			if e := m.expiring; e.Summary != "" {
				u.expiringLine(c, e)
			}
			a.panelKeys(c, u, m)
		}
		if w := m.warming; w.Available {
			ui.Column(c).Gap(u.Px(6)).Children(func() {
				u.SectionHead(c, L("Cache warming"), w.Summary, nil)
				selected := 0
				labels := make([]string, len(w.Options))
				for i, o := range w.Options {
					labels[i] = o.Label
					if o.Selected {
						selected = i
					}
				}
				if u.Segmented(c, &selected, labels, L("Cache warming mode"), w.Help, a.usage.acting != "", true) {
					pool, mode := a.keyPool(), w.Options[selected].Value
					a.poolAct("warming", func() error { return pool.SetCacheWarmingMode(mode) })
				}
			})
		}
		ui.Row(c).Gap(u.Px(4)).Padding(0, 0, u.Px(6), u.Px(2)).Children(func() {
			if m.checked != "" {
				u.Text(c, m.checked, 11).TextColor(p.Tertiary).SingleLine().Shrink(1)
			}
			ui.Spacer(c)
			if u.TextButton(c, L("Open Usage")) {
				a.menuBarDo(func(mb *MenuBar) func() { return mb.OpenUsage })
			}
			if u.TextButton(c, L("Quit Droi")) {
				a.menuBarDo(func(mb *MenuBar) func() { return mb.Quit })
			}
		})
	})
	if mb := a.cfg.MenuBar; mb != nil && mb.Fit != nil {
		if h := box.Bounds().H; h > 0 {
			mb.Fit(width, h)
		}
	}
}

// panelHeader is whether the Key Pool runs, and with which key, beside
// refresh and the way to the Usage tab.
func (a *App) panelHeader(c *ui.Context, u usageLook, st keypool.State) {
	p := u.P
	tone, note := p.Green, ""
	for _, k := range st.Keys {
		if k.Current {
			note = k.Masked
		}
	}
	switch {
	case len(st.Keys) == 0:
		tone, note = p.Warning, L("No key yet")
	case !st.Running:
		tone, note = p.Danger, L("Not running")
	}
	ui.Row(c).Gap(u.Px(8)).Padding(u.Px(12), u.Px(2), 0).Children(func() {
		ui.Box(c).Size(u.Px(7), u.Px(7)).Radius(999).Background(tone)
		u.Text(c, L("Key Pool"), 14).FontWeight(600).LetterSpacing(-0.2)
		u.Text(c, note, 11).Font(u.K.Mono).TextColor(p.Tertiary).SingleLine().Shrink(1)
		ui.Spacer(c)
		if u.ToolButton(c, dashkit.IconRefresh, L("Refresh"), L("Read the quota and the records again"), a.usage.refreshing) {
			a.RefreshKeyPool()
		}
		if u.ToolButton(c, iconWindow, L("Open Usage"), L("Open Usage"), false) {
			a.menuBarDo(func(mb *MenuBar) func() { return mb.OpenUsage })
		}
	})
}

// budgetCard is today's spend against the daily limit, with a tick at what
// is usual by now, and how long the pool lasts at today's spend.
func (u usageLook) budgetCard(c *ui.Context, r llmusage.RunwayView) {
	p := u.P
	u.Card(c).Padding(u.Px(12), u.Px(14)).Gap(u.Px(8)).Tooltip(r.Help).Children(func() {
		if !r.Available {
			u.Text(c, r.Empty, 12).TextColor(p.Secondary)
			return
		}
		b := r.Budget
		ui.Row(c).Gap(u.Px(6)).AlignItems(ui.End).Children(func() {
			ui.Column(c).Gap(1).Children(func() {
				u.Text(c, L("Used today"), 11).FontWeight(600).TextColor(p.Secondary)
				ui.Row(c).Gap(u.Px(6)).AlignItems(ui.End).Children(func() {
					u.Text(c, b.Spent, 20).FontWeight(600).LetterSpacing(-0.3).FontFeatures("tnum")
					u.Text(c, b.SpentShare, 12).FontWeight(500).TextColor(p.Secondary).FontFeatures("tnum").Padding(0, 0, u.Px(3))
				})
			})
			ui.Spacer(c)
			u.Text(c, L("Daily limit %s", b.Limit), 11).TextColor(p.Tertiary).FontFeatures("tnum").Padding(0, 0, u.Px(3)).SingleLine().Shrink(1)
		})
		mark := -1.0
		if b.HasUsual {
			mark = b.Usual
		}
		ui.Row(c).Children(func() { u.Meter(c, b.Bar.Value, b.Bar.Tone, 6, mark).Label(L("Used today")) })
		u.Text(c, b.Note, 11).TextColor(u.Tone(b.NoteTone)).FontFeatures("tnum")
		if len(r.Lines) > 0 {
			l := r.Lines[0]
			ui.Row(c).Gap(u.Px(6)).Children(func() {
				u.Text(c, l.Label, 11).TextColor(p.Tertiary).FontFeatures("tnum").SingleLine().Shrink(1)
				ui.Spacer(c)
				u.Text(c, l.Value, 11).FontWeight(600).TextColor(u.Tone(l.Tone)).FontFeatures("tnum").NoWrap()
			})
		}
	})
}

// panelPools are the pools side by side, without the resets, which their
// tooltips say.
func (u usageLook) panelPools(c *ui.Context, pools []llmusage.PoolSummaryView) {
	p := u.P
	u.Card(c).Children(func() {
		ui.Row(c).AlignItems(ui.Stretch).Children(func() {
			for i, pool := range pools {
				if i > 0 {
					u.VHairline(c)
				}
				pad := u.Px(11)
				ui.Column(c.Key(pool.Title)).Role(ui.RoleGroup).Label(pool.Title).Grow(1).Basis(0).MinWidth(0).Gap(u.Px(7)).Padding(pad-2, pad, pad).Children(func() {
					ui.Row(c).Gap(u.Px(6)).Children(func() {
						u.Text(c, pool.Title, 13).FontWeight(600)
						ui.Spacer(c)
						u.Text(c, pool.Note, 11).TextColor(p.Tertiary).FontFeatures("tnum").SingleLine().Shrink(1)
					})
					for _, w := range pool.Windows {
						ui.Row(c.Key(w.Label)).Gap(u.Px(8)).Tooltip(w.Help + "\n" + w.Reset).Children(func() {
							u.Text(c, w.Label, 11).TextColor(p.Secondary).Width(u.Px(36)).NoWrap()
							u.Meter(c, w.Bar.Value, w.Bar.Tone, 5, -1).Label(w.Name)
							u.Text(c, w.Percent, 12).FontWeight(600).TextColor(u.Tone(dashkit.PrimaryTone(w.PercentTone))).FontFeatures("tnum").Width(u.Px(38)).TextAlign(ui.End)
						})
					}
				})
			}
		})
	})
}

// expiringLine is the panel's one line about monthly quota about to be lost.
func (u usageLook) expiringLine(c *ui.Context, e llmusage.ExpiringView) {
	p := u.P
	u.Card(c).Padding(u.Px(10), u.Px(14)).Gap(u.Px(4)).Tooltip(e.Help).Children(func() {
		ui.Row(c).Gap(u.Px(6)).Children(func() {
			u.Icon(c, dashkit.IconClock, 12, u.Tone(e.SummaryTone))
			u.Text(c, L("Expiring"), 12).FontWeight(600).TextColor(u.Tone(dashkit.PrimaryTone(dashkit.QuietTone(e.SummaryTone))))
			u.Text(c, e.Summary, 11).TextColor(p.Secondary).FontFeatures("tnum").SingleLine().Shrink(1)
		})
		ui.Row(c).Gap(u.Px(6)).Padding(0, 0, 0, u.Px(18)).AlignItems(ui.End).Children(func() {
			u.Text(c, e.Credits, 14).FontWeight(600).FontFeatures("tnum")
			u.Text(c, L("credits expire"), 11).TextColor(p.Tertiary)
			if e.Loss != "" {
				ui.Spacer(c)
				u.Text(c, e.Loss, 11).FontWeight(500).TextColor(p.Danger).FontFeatures("tnum")
			}
		})
	})
}

// panelKeys are the keys, a line each, scrolling past panelKeyRows.
func (a *App) panelKeys(c *ui.Context, u usageLook, m *usageModel) {
	note := countL(len(m.rows), l10n.N("%d key"), l10n.N("%d keys"))
	if m.problems > 0 {
		note = L("%s · %d need attention", note, m.problems)
	}
	ui.Column(c).Gap(u.Px(6)).Children(func() {
		u.SectionHead(c, L("Keys"), note, func() {
			u.Caps(c, "Standard").Width(u.Px(panelPoolColW)).TextAlign(ui.End)
			u.Caps(c, "Core").Width(u.Px(panelPoolColW)).TextAlign(ui.End)
		})
		u.Card(c).Children(func() {
			rows := func() {
				for i, row := range m.rows {
					if i > 0 {
						u.Hairline(c)
					}
					u.panelKeyRow(c, row)
				}
			}
			if len(m.rows) > panelKeyRows {
				ui.Scroll(c).Height((u.Px(panelRowH) + 1) * panelKeyRows).Children(rows)
			} else {
				ui.Column(c).Children(rows)
			}
		})
	})
}

// panelKeyRow is one key: its number, the masked key, a star on the one
// droid sends with, then a small bar per pool or what is wrong.
func (u usageLook) panelKeyRow(c *ui.Context, row llmusage.KeyRowView) {
	p := u.P
	ui.Row(c.Key(row.Index)).Role(ui.RoleGroup).Label(row.Key).Gap(u.Px(8)).Height(u.Px(panelRowH)).Padding(0, u.Px(12)).Tooltip(row.Help).Children(func() {
		u.Text(c, itoa(row.Index), 10).TextColor(p.Tertiary).FontFeatures("tnum").Width(u.Px(14))
		u.Text(c, row.Key, 11).Font(u.K.Mono).SingleLine().Shrink(1)
		star := u.Icon(c, dashkit.IconStar, 9, p.Warning).Label(L("Current"))
		if !row.Active {
			star.Invisible()
		}
		ui.Spacer(c)
		if row.Problem {
			u.Badge(c, row.Badge, true)
			return
		}
		for i, b := range row.Pools {
			ui.Row(c.Key(i)).Width(u.Px(panelPoolColW)).Gap(u.Px(4)).Children(func() {
				if b.HasValue {
					u.Meter(c, b.Bar.Value, b.Bar.Tone, 4, -1)
				} else {
					ui.Spacer(c)
				}
				u.Text(c, b.Percent, 10).TextColor(u.Tone(b.PercentTone)).FontFeatures("tnum").Width(u.Px(30)).TextAlign(ui.End)
			})
		}
	})
}

// MenuBarRefresh is how often the shell reads the item again while nothing
// else redraws the window: the quota changes without telling it.
const MenuBarRefresh = 30 * time.Second
