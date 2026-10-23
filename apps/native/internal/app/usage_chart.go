package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
)

// The Usage tab's charts are drawn with MyGo's ui/chart: the values' axis
// on the left with a hairline at each tick, the buckets' names under the
// bars, and a card of what the bar or the cell under the pointer holds,
// shown at once rather than after a tooltip's wait.

func (u usageLook) axisLabel(c *ui.Context) func(chart.Side, chart.Tick) {
	return func(_ chart.Side, t chart.Tick) {
		u.text(c, t.Label, 10).TextColor(u.p.tertiary).FontFeatures("tnum").NoWrap()
	}
}

// gridLines paints a hairline across the plot at each tick.
func (u usageLook) gridLines(area ui.Element, ticks []chart.Tick) {
	line := u.p.line
	area.Draw(func(p *ui.Painter, r ui.Rect) {
		for _, t := range ticks {
			y := chart.Y(r, t.At)
			p.Line(r.X, y, r.X+r.W, y, 1, line)
		}
	})
}

// chartTip is the card of what target holds: beside it, or above it
// unless side. The pointer goes through it, so that the mark under it
// stays chosen.
func (u usageLook) chartTip(c *ui.Context, target ui.Element, side bool, build func()) {
	p := u.p
	ui.Overlay(c, func() {
		tip := ui.Column(c).Role(ui.RoleTooltip).PassThrough().Gap(u.px(4)).Padding(u.px(8), u.px(10)).MaxWidth(u.px(320)).
			Radius(u.px(8)).Background(p.card).Border(1, p.line).Shadow(0, u.px(6), u.px(16), -u.px(4), ui.RGBA(0, 0, 0, 0.16))
		if side {
			tip.AttachTo(target, ui.AnchorRight, ui.AnchorLeft).MarginStart(u.px(8))
		} else {
			tip.AttachTo(target, ui.AnchorTop, ui.AnchorBottom).Margin(0, 0, u.px(6), 0)
		}
		tip.Children(build)
	})
}

// hoveredBar is the bar under the pointer, or the one the arrows chose,
// -1 for none; the plot's area keeps it.
func hoveredBar(c *ui.Context, area ui.Element, xs []float32, describe func(int) string) int {
	sel := ui.Local(area, "hovered", func() int { return -1 })
	chart.Select(c, area, sel, xs, describe)
	return *sel
}

// bandStep is the i-th band of x with the room around it, half the gap to
// each neighbour.
func bandStep(x chart.Band, i int) chart.Span {
	b := x.Band(i)
	pad := x.Width() * x.Inner / (1 - x.Inner) / 2
	return chart.Span{Lo: b.Lo - pad, Hi: b.Hi + pad}
}

// chart is the spend of the last days against the daily limit, a dashed
// line; today's bar is the accent, a day over the limit amber.
func (u usageLook) chart(c *ui.Context, v llmusage.ChartView, height float32) {
	n := len(v.Bars)
	if v.Top <= 0 || n == 0 {
		return
	}
	p := u.p
	height = u.px(height)
	x := chart.NewBand(make([]string, n)...).Padding(0.3, 0.05)
	k := v.Credits
	y := chart.NewLinear(0, v.Top*k).Nice(3)
	y.Format = llmusage.FormatCredits
	ticks := y.Ticks(3)
	xs := make([]float32, n)
	for i := range xs {
		xs[i] = x.At(i)
	}
	names := []chart.Tick{{At: xs[0], Label: v.First}, {At: xs[n-1], Label: v.Last}}
	chart.Plot(c, chart.Axes{Left: ticks, Bottom: names, Label: u.axisLabel(c)}, func(area ui.Element) {
		u.gridLines(area, ticks)
		limit, dash := y.At(v.Limit*k), p.secondary.Alpha(0.8)
		area.DrawOver(func(pt *ui.Painter, r ui.Rect) {
			ly := chart.Y(r, limit)
			for lx := r.X; lx < r.X+r.W; lx += 7 {
				pt.Line(lx, ly, min(lx+4, r.X+r.W), ly, 1, dash)
			}
		})
		at := hoveredBar(c, area, xs, func(i int) string { return v.Bars[i].Help })
		var band ui.Element
		if at >= 0 {
			band = chart.Rect(c.Key("hovered"), bandStep(x, at), chart.Span{Hi: 1}).Radius(u.px(4)).Background(p.hover)
		}
		for i, b := range v.Bars {
			color := p.accent.Alpha(0.28)
			switch {
			case b.Today && b.Over:
				color = p.danger
			case b.Today:
				color = p.accent
			case b.Over:
				color = p.warning.Alpha(0.6)
			}
			// A day without spend still shows where it is.
			top := max(y.At(b.Value*k), 2/height)
			chart.Rect(c.Key(i), x.Band(i), chart.Span{Hi: top}).Role(ui.RoleMeter).Label(b.Help).Radius(u.px(3)).Background(color)
		}
		if at >= 0 {
			u.chartTip(c, band, true, func() { u.text(c, v.Bars[at].Help, 12).FontFeatures("tnum").NoWrap() })
		}
	}).Label(L("Last %d days", n)).Height(height).GapX(u.px(8)).GapY(u.px(6))
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
// bottom up, on an axis of the metric; the current bucket is drawn full,
// the others a little lighter.
func (u usageLook) trendChart(c *ui.Context, v llmusage.TrendView, height float32) {
	n := len(v.Bars)
	if n == 0 {
		return
	}
	p := u.p
	x := chart.NewBand(make([]string, n)...).Padding(0.25, 0.05)
	// A low top still gets whole ticks, not "0.5 requests".
	y := chart.NewLinear(0, max(v.Top, 4)).Nice(4)
	y.Format = v.Metric.Tick
	ticks := y.Ticks(4)
	xs := make([]float32, n)
	var names []chart.Tick
	for i, b := range v.Bars {
		xs[i] = x.At(i)
		if b.Label != "" {
			names = append(names, chart.Tick{At: xs[i], Label: b.Label})
		}
	}
	chart.Plot(c, chart.Axes{Left: ticks, Bottom: names, Label: u.axisLabel(c)}, func(area ui.Element) {
		u.gridLines(area, ticks)
		at := hoveredBar(c, area, xs, func(i int) string { return v.Bars[i].Help })
		var band ui.Element
		if at >= 0 {
			band = chart.Rect(c.Key("hovered"), bandStep(x, at), chart.Span{Hi: 1}).Radius(u.px(4)).Background(p.hover)
		}
		for i, b := range v.Bars {
			lo := 0.0
			for j, part := range b.Parts {
				if part <= 0 {
					continue
				}
				color := p.accent
				if v.Stacked {
					color = u.partColor(j, len(b.Parts))
				}
				if !b.Current && i != at {
					color = color.Alpha(0.82)
				}
				chart.Rect(c.Key([2]int{i, j}), x.Band(i), y.Span(lo, lo+part)).Background(color)
				lo += part
			}
		}
		if at >= 0 {
			u.chartTip(c, band, true, func() { u.trendTip(c, v, v.Bars[at]) })
		}
	}).Label(L("Trend")).Description(v.About).Height(u.px(height)).GapX(u.px(8)).GapY(u.px(6))
}

// trendTip is a bar's card: its bucket, its total, then each model's part
// by its color.
func (u usageLook) trendTip(c *ui.Context, v llmusage.TrendView, b llmusage.TrendBar) {
	p := u.p
	u.text(c, b.Title, 12).FontWeight(600).NoWrap()
	u.text(c, b.Total, 11).TextColor(p.secondary).FontFeatures("tnum").NoWrap()
	if len(b.Lines) == 0 {
		return
	}
	ui.Column(c).Gap(u.px(3)).Padding(u.px(2), 0, 0).Children(func() {
		for _, l := range b.Lines {
			ui.Row(c.Key(l.Part)).Gap(u.px(6)).Children(func() {
				ui.Box(c).Size(u.px(8), u.px(8)).Radius(u.px(2)).Background(u.partColor(l.Part, len(v.Legend)))
				name := u.text(c, l.Name, 11).TextColor(p.secondary).NoWrap()
				if l.Part < len(v.Legend)-1 {
					name.Font(u.k.Mono)
				}
				ui.Box(c).Grow(1).MinWidth(u.px(16))
				u.text(c, l.Value, 11).FontFeatures("tnum").NoWrap()
			})
		}
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

// heatMap is the weekdays by the hours, each cell darker the more it
// holds; the cell under the pointer shows its card.
func (u usageLook) heatMap(c *ui.Context, v llmusage.HeatView) {
	days := len(v.Rows)
	if days == 0 {
		return
	}
	p := u.p
	levels := []ui.Color{p.track, p.accent.Alpha(0.22), p.accent.Alpha(0.45), p.accent.Alpha(0.7), p.accent}
	// The axis counts up from the bottom; Monday is the top row.
	row := func(d int) int { return days - 1 - d }
	y := chart.NewBand(make([]string, days)...).Padding(0.18, 0)
	x := chart.NewBand(make([]string, 24)...).Padding(0.12, 0)
	var left, bottom []chart.Tick
	for d, r := range v.Rows {
		left = append(left, chart.Tick{At: y.At(row(d)), Label: r.Label})
	}
	for h := 0; h < 24; h += 3 {
		bottom = append(bottom, chart.Tick{At: x.At(h), Label: itoa(h)})
	}
	chart.Plot(c, chart.Axes{Left: left, Bottom: bottom, Label: u.axisLabel(c)}, func(area ui.Element) {
		hd, hh := -1, -1
		if px, py, over := area.PointerPosition(); over {
			if b := area.Bounds(); b.W > 0 && b.H > 0 {
				h, inX := x.Index(px / b.W)
				r, inY := y.Index(1 - py/b.H)
				if inX && inY {
					hd, hh = row(r), h
				}
			}
		}
		var hovered ui.Element
		for d, r := range v.Rows {
			for h, cell := range r.Cells {
				e := chart.Rect(c.Key(d*24+h), x.Band(h), y.Band(row(d))).Role(ui.RoleMeter).Label(cell.Help).Radius(u.px(3)).Background(levels[cell.Level])
				if d == hd && h == hh {
					hovered = e.Border(1.5, p.text.Alpha(0.55))
				}
			}
		}
		if hd >= 0 {
			cell := v.Rows[hd].Cells[hh]
			u.chartTip(c, hovered, false, func() {
				u.text(c, cell.Title, 12).FontWeight(600).FontFeatures("tnum").NoWrap()
				u.text(c, cell.Value, 11).TextColor(p.secondary).FontFeatures("tnum").NoWrap()
			})
		}
	}).Label(L("Active hours")).Description(v.Note).Height(u.px(float32(days*17 + 16))).GapX(u.px(10)).GapY(u.px(4))
}
