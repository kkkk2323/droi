package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
	"github.com/kkkk2323/droi/apps/native/pkg/dashkit"
	"github.com/kkkk2323/droi/apps/native/pkg/keypool"
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

	// reqSel is the request whose details show, -1 for none; inited says
	// it was set so.
	reqSel              int
	inited              bool
	detailOpen          bool
	modelOpen, kindOpen bool
	groupList, reqList  ui.ListState
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
			s.busy = false
			// The query changed while this one ran: what it found is not
			// what the page asks for now, so it asks again at once.
			if s.query != q {
				s.at = time.Time{}
				return
			}
			s.at, s.loaded = a.cfg.Now(), true
			s.data, s.err = o, ""
			if err != nil {
				s.err = err.Error()
			}
			s.version++
		})
	}()
}

// changeQuery asks the log again at once.
func (a *App) changeQuery(fn func(q *llmusage.ObserveQuery)) {
	s := &a.usage.observe
	fn(&s.query)
	s.reqSel, s.detailOpen = -1, false
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
	p := u.P
	s := &a.usage.observe
	a.loadObservation(false)
	v := a.observeView()
	u.page(c, func() {
		ui.Row(c).Gap(u.Px(10)).Padding(0, u.Px(4)).Children(func() {
			sel := 0
			labels := make([]string, len(llmusage.Periods))
			for i, period := range llmusage.Periods {
				labels[i] = llmusage.PeriodLabel(period)
				if period == s.query.Period {
					sel = i
				}
			}
			if u.Segmented(c, &sel, labels, L("Period"), "", false, false) {
				a.changeQuery(func(q *llmusage.ObserveQuery) { q.Period = llmusage.Periods[sel] })
			}
			if s.loaded {
				u.Text(c, v.Range, 12).FontWeight(500).FontFeatures("tnum").NoWrap()
			}
			if s.busy {
				ui.Spinner(c).Label(L("Reading"))
			}
			ui.Spacer(c)
			if s.loaded {
				u.Text(c, v.Source, 11).TextColor(p.Tertiary).FontFeatures("tnum").SingleLine().Shrink(1)
			}
		})
		if s.err != "" {
			ui.Row(c).Role(ui.RoleStatus).Gap(u.Px(8)).Padding(0, u.Px(4)).Children(func() {
				u.Icon(c, dashkit.IconWarning, 13, p.Danger)
				u.Text(c, L("Could not read the request log: %s", s.err), 12).TextColor(p.Danger).Selectable().Shrink(1)
			})
		}
		if !s.loaded {
			u.Card(c).Height(u.Px(160)).Children(func() {
				ui.Column(c).Grow(1).Center().Gap(u.Px(8)).Children(func() {
					ui.Spinner(c)
					u.Text(c, L("Reading the request log…"), 12).TextColor(p.Secondary)
				})
			})
			return
		}
		u.MetricStrip(c, v.Summary)
		if v.Empty {
			u.Card(c).Height(u.Px(180)).Children(func() {
				desc := v.EmptyText
				if !st.Running && st.Err != "" {
					desc = st.Err
				}
				u.Unavailable(c, dashkit.IconWaveform, L("Nothing in this period"), desc, nil)
			})
			return
		}
		ui.Box(c).Height(u.Px(4))
		u.SectionHead(c, L("Trend"), v.Trend.Peak, func() {
			sel := int(s.metric)
			labels := make([]string, len(llmusage.ObserveMetrics))
			for i, m := range llmusage.ObserveMetrics {
				labels[i] = m.Label()
			}
			if u.Segmented(c, &sel, labels, L("What the trend and the heat map count"), "", false, false) {
				s.metric = llmusage.ObserveMetrics[sel]
			}
		})
		u.Card(c).Padding(u.Px(12), u.Px(16), u.Px(12)).Gap(u.Px(10)).Children(func() {
			u.Text(c, v.Trend.About, 11).TextColor(p.Secondary)
			u.TrendChart(c, v.Trend, 168)
			if v.Trend.Stacked {
				u.Legend(c, v.Trend.Legend)
			}
		})
		ui.Box(c).Height(u.Px(4))
		u.SectionHead(c, L("Active hours"), v.Heat.Note, nil)
		u.Card(c).Padding(u.Px(12), u.Px(16)).Children(func() { u.HeatMap(c, v.Heat) })
		ui.Box(c).Height(u.Px(4))
		u.SectionHead(c, L("Breakdown"), v.GroupsNote, func() {
			sel := int(s.grouping)
			labels := make([]string, len(llmusage.Groupings))
			for i, g := range llmusage.Groupings {
				labels[i] = llmusage.GroupingLabel(g)
			}
			if u.Segmented(c, &sel, labels, L("Group by"), "", false, false) {
				s.grouping = llmusage.Groupings[sel]
			}
		})
		a.groupTable(c, u, v.Groups)
		ui.Box(c).Height(u.Px(4))
		a.observeList(c, u, v)
	})
}

// groupColumn is a column of the breakdown, width 0 for the one that takes
// the room left.
type groupColumn struct {
	title string
	width float32
	end   bool
}

// groupTable is the breakdown. A model's or a type's row lists only its
// requests below, and a second click lists them all again; the row in
// force is tinted, whatever period or grouping comes between.
func (a *App) groupTable(c *ui.Context, u usageLook, rows []llmusage.GroupRowView) {
	p := u.P
	s := &a.usage.observe
	filters := s.grouping == llmusage.ByModel || s.grouping == llmusage.ByKind
	inForce := s.query.Model
	if s.grouping == llmusage.ByKind {
		inForce = s.query.Kind
	}
	columns := []groupColumn{
		{llmusage.GroupingLabel(s.grouping), 0, false},
		{L("Calls"), 52, true},
		{L("Failed"), 52, true},
		{L("Share"), 76, false},
		{L("Input"), 64, true},
		{L("Output"), 60, true},
		{L("Cache"), 52, true},
		{L("Credits"), 72, true},
		{L("First token"), 78, true},
		{L("Output speed"), 72, true},
	}
	cell := func(col groupColumn, fn func()) {
		e := ui.Row(c).Gap(u.Px(6)).PaddingX(u.Px(10)).AlignItems(ui.Center).Shrink(0)
		if col.width > 0 {
			e.Width(u.Px(col.width))
		} else {
			e.Grow(1).Shrink(1).MinWidth(u.Px(110))
		}
		if col.end {
			e.Justify(ui.End)
		}
		e.Children(fn)
	}
	numeric := func(v llmusage.Cell) {
		ui.Text(c, v.Text).TextColor(u.Tone(v.Tone)).FontFeatures("tnum").Tooltip(v.Help).SingleLine()
	}
	shown := min(len(rows), 8)
	u.Card(c).Height(u.Px(float32(33 + 30*max(shown, 1)))).Children(func() {
		if len(rows) == 0 {
			u.Unavailable(c, dashkit.IconList, L("Nothing to group"), L("No call in this period names its session."), nil)
			return
		}
		ui.Row(c).Height(u.Px(32)).Shrink(0).AlignItems(ui.Stretch).Children(func() {
			for _, col := range columns {
				cell(col, func() { ui.Text(c, col.title).FontWeight(600).TextColor(p.Secondary).SingleLine() })
			}
		})
		u.Hairline(c)
		s.groupList.Key = func(i int) any { return rows[i].ID }
		ui.List(c, &s.groupList, len(rows), func(i int) {
			r := rows[i]
			on := filters && r.Filter == inForce
			var row ui.Element
			if filters {
				row = ui.ButtonBase(c).Role(ui.RoleToggleButton).Checked(on).Label(r.Name).Justify(ui.Start).Cursor(ui.CursorPointer)
			} else {
				row = ui.Row(c)
			}
			row.Height(u.Px(30)).AlignItems(ui.Stretch)
			switch {
			case on:
				row.Background(p.Accent.Alpha(0.1))
			case filters && row.Hovered():
				row.Background(p.Hover)
			}
			row.Children(func() {
				cell(columns[0], func() {
					if on {
						u.Icon(c, dashkit.IconTick, 12, p.Accent)
					}
					name := u.Text(c, r.Name, 12).SingleLine().Shrink(1).Tooltip(r.Help)
					if r.Mono {
						name.Font(u.K.Mono).FontSize(u.Px(11))
					}
					if on {
						name.FontWeight(600)
					}
				})
				cell(columns[1], func() { ui.Text(c, r.Calls).FontFeatures("tnum") })
				cell(columns[2], func() { numeric(r.Failed) })
				cell(columns[3], func() {
					u.Meter(c, r.Share, llmusage.ToneAccent, 4, -1)
					u.Text(c, r.ShareText, 11).TextColor(p.Secondary).FontFeatures("tnum").Width(u.Px(34)).TextAlign(ui.End).NoWrap()
				})
				cell(columns[4], func() { numeric(r.Input) })
				cell(columns[5], func() { numeric(r.Output) })
				cell(columns[6], func() { numeric(r.Cache) })
				cell(columns[7], func() { numeric(r.Credits) })
				cell(columns[8], func() { numeric(r.TTFT) })
				cell(columns[9], func() { numeric(r.Speed) })
			})
			if filters && row.Clicked() {
				filter := r.Filter
				if on {
					filter = ""
				}
				byModel := s.grouping == llmusage.ByModel
				a.changeQuery(func(q *llmusage.ObserveQuery) {
					if byModel {
						q.Model = filter
					} else {
						q.Kind = filter
					}
				})
			}
		}).Label(L("Breakdown")).Grow(1)
	})
}

// observeList is the period's requests, newest first, with the filters
// above them and the chosen one's details below.
func (a *App) observeList(c *ui.Context, u usageLook, v llmusage.ObserveView) {
	p := u.P
	s := &a.usage.observe
	u.SectionHead(c, L("Requests"), v.ListNote, func() {
		if model, ok := u.Dropdown(c, &s.modelOpen, L("Model"), s.query.Model, v.Models); ok {
			a.changeQuery(func(q *llmusage.ObserveQuery) { q.Model = model })
		}
		if kind, ok := u.Dropdown(c, &s.kindOpen, L("Type"), s.query.Kind, v.Kinds); ok {
			a.changeQuery(func(q *llmusage.ObserveQuery) { q.Kind = kind })
		}
		ui.Row(c).Gap(u.Px(6)).Children(func() {
			u.Text(c, L("Failed only"), 12).TextColor(p.Secondary).NoWrap()
			if a.toggle(c, L("Failed only"), s.query.FailedOnly, false) {
				a.changeQuery(func(q *llmusage.ObserveQuery) { q.FailedOnly = !q.FailedOnly })
			}
		})
	})
	u.Card(c).Height(u.Px(420)).Children(func() {
		if len(v.Rows) == 0 {
			u.Unavailable(c, dashkit.IconArrows, L("No requests match"), L("Choose another model or type, or turn off Failed only."), nil)
			return
		}
		if a.requestsTable(c, u, v.Rows).Changed() && s.reqSel >= 0 {
			s.detailOpen = true
		}
	})
	a.requestDetailSheet(c, u)
}

// requestDetailSheet is everything the log knows of the request chosen in
// the list. Closing it lets the row go, so that a click on it opens it
// again.
func (a *App) requestDetailSheet(c *ui.Context, u usageLook) {
	p := u.P
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
		ui.Column(c).Width(u.Px(540)).Gap(u.Px(10)).Children(func() {
			u.Text(c, L("Request details"), 14).Role(ui.RoleHeading).FontWeight(600)
			ui.Column(c).Gap(u.Px(6)).Children(func() {
				for i, l := range lines {
					ui.Row(c.Key(i)).Gap(u.Px(12)).AlignItems(ui.Start).Children(func() {
						u.Text(c, l.Label, 12).TextColor(p.Tertiary).Width(u.Px(88)).NoWrap()
						u.Text(c, l.Value, 12).FontFeatures("tnum").Selectable().Grow(1).Shrink(1).MinWidth(0)
					})
				}
			})
			ui.Row(c).Children(func() {
				ui.Spacer(c)
				if u.PushButton(c, L("Close"), "", false, false) {
					open = false
				}
			})
		})
	})
	if !open {
		s.detailOpen, s.reqSel = false, -1
	}
}
