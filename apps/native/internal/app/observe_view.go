package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
	"github.com/kkkk2323/droi/apps/native/pkg/keypool"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// observeRefresh is how often the Observe page reads what the request log
// gained: it reads only the new lines.
const observeRefresh = 3 * time.Second

// observeState is the Observe page's, kept between frames.
type observeState struct {
	log     *llmusage.RequestLog
	logPath string
	data    llmusage.Observation
	loaded  bool
	at      time.Time
	busy    bool
	err     string
	version int

	query    llmusage.ObserveQuery
	metric   llmusage.ObserveMetric
	grouping llmusage.Grouping

	// view is built again when what it is built from changes.
	view    llmusage.ObserveView
	viewKey observeViewKey

	// groupSel and reqSel are the rows chosen, -1 for none; inited says
	// they were set so.
	groupSel, reqSel   int
	inited             bool
	detailOpen         bool
	groupList, reqList ui.ListState
}

type observeViewKey struct {
	version  int
	metric   llmusage.ObserveMetric
	grouping llmusage.Grouping
	lang     string
	names    int
}

// loadObservation reads the request log's new lines and adds the period up
// again, at most every observeRefresh unless forced.
func (a *App) loadObservation(force bool) {
	s := &a.usage.observe
	pool := a.keyPool()
	if pool == nil || s.busy || !force && !s.at.IsZero() && a.cfg.Now().Sub(s.at) < observeRefresh {
		return
	}
	if path := pool.RequestLog(); s.log == nil || s.logPath != path {
		s.log, s.logPath = llmusage.NewRequestLog(path), path
	}
	s.busy = true
	log, q := s.log, s.query
	go func() {
		err := log.Refresh()
		o := log.Observe(q, time.Now())
		a.cfg.Update(func() {
			s.busy, s.at, s.loaded = false, a.cfg.Now(), true
			s.data, s.err = o, ""
			if err != nil {
				s.err = err.Error()
			}
			s.version++
			// The query changed while this one ran.
			if s.query != q {
				s.at = time.Time{}
			}
		})
	}()
}

// changeQuery asks the log again at once.
func (a *App) changeQuery(fn func(q *llmusage.ObserveQuery)) {
	s := &a.usage.observe
	fn(&s.query)
	s.reqSel = -1
	if s.busy {
		s.at = time.Time{}
		return
	}
	a.loadObservation(true)
}

// sessionNames are the titles of the sessions the sidebar lists, by ID.
func (a *App) sessionNames() map[string]string {
	names := make(map[string]string, len(a.listed))
	for _, s := range a.listed {
		if s.Title != "" {
			names[s.SessionID] = s.Title
		}
	}
	return names
}

func (a *App) observeView() llmusage.ObserveView {
	s := &a.usage.observe
	names := a.sessionNames()
	key := observeViewKey{s.version, s.metric, s.grouping, string(l10n.Code()), len(names)}
	if key != s.viewKey {
		s.view = llmusage.BuildObserve(s.data, s.metric, s.grouping, func(id string) string { return names[id] })
		s.viewKey = key
	}
	return s.view
}

func (a *App) observePage(c *ui.Context, u usageLook, st keypool.State) {
	p := u.p
	s := &a.usage.observe
	a.loadObservation(false)
	v := a.observeView()
	u.page(c, func() {
		ui.Row(c).Gap(u.px(10)).Padding(0, u.px(4)).Children(func() {
			sel := 0
			labels := make([]string, len(llmusage.Periods))
			for i, period := range llmusage.Periods {
				labels[i] = llmusage.PeriodLabel(period)
				if period == s.query.Period {
					sel = i
				}
			}
			if u.segmented(c, &sel, labels, L("Period"), "", false, false) {
				a.changeQuery(func(q *llmusage.ObserveQuery) { q.Period = llmusage.Periods[sel] })
			}
			if s.loaded {
				u.text(c, v.Range, 12).FontWeight(500).FontFeatures("tnum").NoWrap()
			}
			if s.busy {
				ui.Spinner(c).Label(L("Reading"))
			}
			ui.Spacer(c)
			if s.loaded {
				u.text(c, v.Source, 11).TextColor(p.tertiary).FontFeatures("tnum").SingleLine().Shrink(1)
			}
		})
		if s.err != "" {
			ui.Row(c).Role(ui.RoleStatus).Gap(u.px(8)).Padding(0, u.px(4)).Children(func() {
				u.icon(c, iconWarning, 13, p.danger)
				u.text(c, L("Could not read the request log: %s", s.err), 12).TextColor(p.danger).Selectable().Shrink(1)
			})
		}
		if !s.loaded {
			u.card(c).Height(u.px(160)).Children(func() {
				ui.Column(c).Grow(1).Center().Gap(u.px(8)).Children(func() {
					ui.Spinner(c)
					u.text(c, L("Reading the request log…"), 12).TextColor(p.secondary)
				})
			})
			return
		}
		u.metricStrip(c, v.Summary)
		if v.Empty {
			u.card(c).Height(u.px(180)).Children(func() {
				desc := v.EmptyText
				if !st.Running && st.Err != "" {
					desc = st.Err
				}
				u.unavailable(c, iconWaveform, L("Nothing in this period"), desc, nil)
			})
			return
		}
		ui.Box(c).Height(u.px(4))
		u.sectionHead(c, L("Trend"), v.Trend.Peak, func() {
			sel := int(s.metric)
			labels := make([]string, len(llmusage.ObserveMetrics))
			for i, m := range llmusage.ObserveMetrics {
				labels[i] = m.Label()
			}
			if u.segmented(c, &sel, labels, L("What the trend and the heat map count"), "", false, false) {
				s.metric = llmusage.ObserveMetrics[sel]
			}
		})
		u.card(c).Padding(u.px(14), u.px(16), u.px(12)).Gap(u.px(10)).Children(func() {
			u.trendChart(c, v.Trend, 132)
			if v.Trend.Stacked {
				u.legend(c, v.Trend.Legend)
			}
		})
		ui.Box(c).Height(u.px(4))
		u.sectionHead(c, L("Active hours"), v.Heat.Note, nil)
		u.card(c).Padding(u.px(12), u.px(16)).Children(func() { u.heatMap(c, v.Heat) })
		ui.Box(c).Height(u.px(4))
		u.sectionHead(c, L("Breakdown"), v.GroupsNote, func() {
			sel := int(s.grouping)
			labels := make([]string, len(llmusage.Groupings))
			for i, g := range llmusage.Groupings {
				labels[i] = llmusage.GroupingLabel(g)
			}
			if u.segmented(c, &sel, labels, L("Group by"), "", false, false) {
				s.grouping, s.groupSel = llmusage.Groupings[sel], -1
			}
		})
		a.groupTable(c, u, v.Groups)
		ui.Box(c).Height(u.px(4))
		a.observeList(c, u, v)
	})
}

// trendColors are the top models' colors, then the other models'.
func (u usageLook) trendColors() []ui.Color {
	p := u.p
	return []ui.Color{p.accent, ui.Hex("#8b5cf6"), ui.Hex("#14a3a3"), ui.Hex("#e08a2c"), ui.Hex("#d6578a"), p.faint}
}

func (u usageLook) partColor(i, parts int) ui.Color {
	colors := u.trendColors()
	if i == parts-1 {
		return colors[len(colors)-1]
	}
	return colors[min(i, len(colors)-2)]
}

// trendChart is a bar a bucket, split among the top models from the
// bottom up; the current bucket is drawn full, the others a little lighter.
func (u usageLook) trendChart(c *ui.Context, v llmusage.TrendView, height float32) {
	p := u.p
	height = u.px(height)
	top := v.Top
	if top <= 0 {
		top = 1
	}
	ui.Column(c).Gap(u.px(6)).Children(func() {
		ui.Row(c).Height(height).Gap(u.px(3)).AlignItems(ui.End).Children(func() {
			for i, b := range v.Bars {
				ui.Column(c.Key(i)).Role(ui.RoleMeter).Label(b.Help).Grow(1).Basis(0).MinWidth(1).Height(height).Justify(ui.End).Tooltip(b.Help).Children(func() {
					drawn := false
					for j := len(b.Parts) - 1; j >= 0; j-- {
						h := height * float32(min(max(b.Parts[j]/top, 0), 1))
						if h <= 0 {
							continue
						}
						color := p.accent
						if v.Stacked {
							color = u.partColor(j, len(b.Parts))
						}
						if !b.Current {
							color = color.Alpha(0.82)
						}
						ui.Box(c.Key(j)).Height(max(h, 1)).Background(color)
						drawn = true
					}
					if !drawn {
						ui.Box(c).Height(1).Background(p.track)
					}
				})
			}
		})
		ui.Row(c).Children(func() {
			for i, label := range v.Axis {
				if i > 0 {
					ui.Spacer(c)
				}
				u.text(c, label, 10).TextColor(p.tertiary).FontFeatures("tnum")
			}
		})
	})
}

func (u usageLook) legend(c *ui.Context, names []string) {
	ui.Row(c).Wrap().Gap(u.px(14)).Children(func() {
		for i, name := range names {
			ui.Row(c.Key(i)).Gap(u.px(5)).Children(func() {
				ui.Box(c).Size(u.px(8), u.px(8)).Radius(u.px(2)).Background(u.partColor(i, len(names)))
				t := u.text(c, name, 11).TextColor(u.p.secondary).NoWrap()
				if i < len(names)-1 {
					t.Font(u.k.Mono)
				}
			})
		}
	})
}

// heatMap is the weekdays by the hours, each cell darker the more it holds.
func (u usageLook) heatMap(c *ui.Context, v llmusage.HeatView) {
	p := u.p
	levels := []ui.Color{p.track, p.accent.Alpha(0.22), p.accent.Alpha(0.45), p.accent.Alpha(0.7), p.accent}
	label := u.px(64)
	ui.Column(c).Gap(u.px(3)).Children(func() {
		for d, row := range v.Rows {
			ui.Row(c.Key(d)).Gap(u.px(3)).Children(func() {
				u.text(c, row.Label, 11).TextColor(p.secondary).Width(label).NoWrap()
				for h, cell := range row.Cells {
					ui.Box(c.Key(h)).Role(ui.RoleMeter).Label(cell.Help).Grow(1).Basis(0).Height(u.px(14)).Radius(u.px(3)).Background(levels[cell.Level]).Tooltip(cell.Help)
				}
			})
		}
		ui.Row(c).Gap(u.px(3)).Children(func() {
			ui.Box(c).Width(label)
			for h := range 24 {
				t := ""
				if h%6 == 0 {
					t = itoa(h)
				}
				u.text(c.Key(h), t, 10).TextColor(p.tertiary).FontFeatures("tnum").Grow(1).Basis(0).NoWrap()
			}
		})
	})
}

// groupTable is the breakdown; choosing a model or a type lists its
// requests below.
func (a *App) groupTable(c *ui.Context, u usageLook, rows []llmusage.GroupRowView) {
	p := u.p
	s := &a.usage.observe
	columns := []ui.TableColumn{
		{ID: "name", Title: llmusage.GroupingLabel(s.grouping), MinWidth: u.px(110)},
		{ID: "calls", Title: L("Calls"), Width: u.px(52), Align: ui.End},
		{ID: "failed", Title: L("Failed"), Width: u.px(52), Align: ui.End},
		{ID: "share", Title: L("Share"), Width: u.px(76)},
		{ID: "input", Title: L("Input"), Width: u.px(64), Align: ui.End},
		{ID: "output", Title: L("Output"), Width: u.px(60), Align: ui.End},
		{ID: "cache", Title: L("Cache"), Width: u.px(52), Align: ui.End},
		{ID: "credits", Title: L("Credits"), Width: u.px(72), Align: ui.End},
		{ID: "ttft", Title: L("First token"), Width: u.px(78), Align: ui.End},
		{ID: "speed", Title: L("Speed"), Width: u.px(48), Align: ui.End},
	}
	filters := s.grouping == llmusage.ByModel || s.grouping == llmusage.ByKind
	s.groupList.Key = func(i int) any { return rows[i].ID }
	s.groupList.Selected = nil
	if filters {
		s.groupList.Selected = &s.groupSel
	}
	numeric := func(cell llmusage.Cell) {
		ui.Text(c, cell.Text).TextColor(u.tone(cell.Tone)).FontFeatures("tnum").Tooltip(cell.Help).SingleLine()
	}
	shown := min(len(rows), 8)
	u.card(c).Height(u.px(float32(38 + 30*max(shown, 1)))).Children(func() {
		if len(rows) == 0 {
			u.unavailable(c, iconList, L("Nothing to group"), L("No call in this period names its session."), nil)
			return
		}
		t := ui.Table(c, &s.groupList, columns, len(rows), func(i, col int) {
			r := rows[i]
			switch col {
			case 0:
				name := u.text(c, r.Name, 12).SingleLine().Shrink(1).Tooltip(r.Help)
				if r.Mono {
					name.Font(u.k.Mono).FontSize(u.px(11))
				}
			case 1:
				ui.Text(c, r.Calls).FontFeatures("tnum")
			case 2:
				numeric(r.Failed)
			case 3:
				ui.Row(c).Gap(u.px(6)).Children(func() {
					u.meter(c, r.Share, llmusage.ToneAccent, 4, -1)
					u.text(c, r.ShareText, 11).TextColor(p.secondary).FontFeatures("tnum").Width(u.px(34)).TextAlign(ui.End).NoWrap()
				})
			case 4:
				numeric(r.Input)
			case 5:
				numeric(r.Output)
			case 6:
				numeric(r.Cache)
			case 7:
				numeric(r.Credits)
			case 8:
				numeric(r.TTFT)
			case 9:
				numeric(r.Speed)
			}
		}).Label(L("Breakdown")).Grow(1)
		if filters && t.Changed() && s.groupSel >= 0 && s.groupSel < len(rows) {
			id := rows[s.groupSel].ID
			a.changeQuery(func(q *llmusage.ObserveQuery) {
				if s.grouping == llmusage.ByModel {
					q.Model = id
				} else {
					q.Kind = id
					if id == "" {
						q.Kind = "request"
					}
				}
			})
		}
	})
}

// observeList is the period's requests, newest first, with the filters
// above them and the chosen one's details below.
func (a *App) observeList(c *ui.Context, u usageLook, v llmusage.ObserveView) {
	p := u.p
	s := &a.usage.observe
	u.sectionHead(c, L("Requests"), v.ListNote, func() {
		models := make([]kit.Option, len(v.Models))
		for i, o := range v.Models {
			models[i] = kit.Option{Value: o.Value, Label: o.Label}
		}
		if model, ok := a.fieldSelect(c, L("Model"), s.query.Model, models, false); ok {
			a.changeQuery(func(q *llmusage.ObserveQuery) { q.Model = model })
		}
		kinds := make([]kit.Option, len(v.Kinds))
		for i, o := range v.Kinds {
			kinds[i] = kit.Option{Value: o.Value, Label: o.Label}
		}
		if kind, ok := a.fieldSelect(c, L("Type"), s.query.Kind, kinds, false); ok {
			a.changeQuery(func(q *llmusage.ObserveQuery) { q.Kind = kind })
		}
		ui.Row(c).Gap(u.px(6)).Children(func() {
			u.text(c, L("Failed only"), 12).TextColor(p.secondary).NoWrap()
			if a.toggle(c, L("Failed only"), s.query.FailedOnly, false) {
				a.changeQuery(func(q *llmusage.ObserveQuery) { q.FailedOnly = !q.FailedOnly })
			}
		})
	})
	u.card(c).Height(u.px(420)).Children(func() {
		if len(v.Rows) == 0 {
			u.unavailable(c, iconArrows, L("No requests match"), L("Choose another model or type, or turn off Failed only."), nil)
			return
		}
		if a.requestsTable(c, u, v.Rows).Changed() && s.reqSel >= 0 {
			s.detailOpen = true
		}
	})
	a.requestDetailSheet(c, u)
}

// requestDetailSheet is everything the log knows of the request chosen in
// the list.
func (a *App) requestDetailSheet(c *ui.Context, u usageLook) {
	p := u.p
	s := &a.usage.observe
	listed := s.data.Listed
	if !s.detailOpen || s.reqSel < 0 || s.reqSel >= len(listed) {
		s.detailOpen = false
		return
	}
	names := a.sessionNames()
	lines := llmusage.RequestDetail(listed[s.reqSel], func(id string) string { return names[id] })
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(u.px(540)).Gap(u.px(10)).Children(func() {
			u.text(c, L("Request details"), 14).Role(ui.RoleHeading).FontWeight(600)
			ui.Column(c).Gap(u.px(6)).Children(func() {
				for i, l := range lines {
					ui.Row(c.Key(i)).Gap(u.px(12)).AlignItems(ui.Start).Children(func() {
						u.text(c, l.Label, 12).TextColor(p.tertiary).Width(u.px(88)).NoWrap()
						u.text(c, l.Value, 12).FontFeatures("tnum").Selectable().Grow(1).Shrink(1).MinWidth(0)
					})
				}
			})
			ui.Row(c).Children(func() {
				ui.Spacer(c)
				if u.pushButton(c, L("Close"), "", false, false) {
					open = false
				}
			})
		})
	})
	if !open {
		s.detailOpen = false
	}
}
