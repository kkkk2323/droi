package dashkit

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
)

// The charts are drawn with MyGo's ui/chart: the values' axis on the left
// with a hairline at each tick, the buckets' names under the bars, and a
// card of what the bar or the cell under the pointer holds, shown at once
// rather than after a tooltip's wait.

// AxisLabel draws a tick's label of a chart.Plot's axes.
func (u Look) AxisLabel(c *ui.Context) func(chart.Side, chart.Tick) {
	return func(_ chart.Side, t chart.Tick) {
		u.Text(c, t.Label, 10).TextColor(u.P.Tertiary).FontFeatures("tnum").NoWrap()
	}
}

// GridLines paints a hairline across the plot at each tick.
func (u Look) GridLines(area ui.Element, ticks []chart.Tick) {
	line := u.P.Line
	area.Draw(func(p *ui.Painter, r ui.Rect) {
		for _, t := range ticks {
			y := chart.Y(r, t.At)
			p.Line(r.X, y, r.X+r.W, y, 1, line)
		}
	})
}

// ChartTip is the card of what target holds: beside it, or above it
// unless side. The pointer goes through it, so that the mark under it
// stays chosen.
func (u Look) ChartTip(c *ui.Context, target ui.Element, side bool, build func()) {
	p := u.P
	ui.Overlay(c, func() {
		tip := ui.Column(c).Role(ui.RoleTooltip).PassThrough().Gap(u.Px(4)).Padding(u.Px(8), u.Px(10)).MaxWidth(u.Px(320)).
			Radius(u.Px(8)).Background(p.Card).Border(1, p.Line).Shadow(0, u.Px(6), u.Px(16), -u.Px(4), ui.RGBA(0, 0, 0, 0.16))
		if side {
			tip.AttachTo(target, ui.AnchorRight, ui.AnchorLeft).MarginStart(u.Px(8))
		} else {
			tip.AttachTo(target, ui.AnchorTop, ui.AnchorBottom).Margin(0, 0, u.Px(6), 0)
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

// Chart is v's bars against its limit, a dashed line; today's bar is the
// accent, a bar over the limit amber.
func (u Look) Chart(c *ui.Context, v ChartView, height float32) {
	n := len(v.Bars)
	if v.Top <= 0 || n == 0 {
		return
	}
	p := u.P
	height = u.Px(height)
	x := chart.NewBand(make([]string, n)...).Padding(0.3, 0.05)
	k := v.Scale
	y := chart.NewLinear(0, v.Top*k).Nice(3)
	y.Format = v.Tick
	ticks := y.Ticks(3)
	xs := make([]float32, n)
	for i := range xs {
		xs[i] = x.At(i)
	}
	names := []chart.Tick{{At: xs[0], Label: v.First}, {At: xs[n-1], Label: v.Last}}
	chart.Plot(c, chart.Axes{Left: ticks, Bottom: names, Label: u.AxisLabel(c)}, func(area ui.Element) {
		u.GridLines(area, ticks)
		limit, dash := y.At(v.Limit*k), p.Secondary.Alpha(0.8)
		area.DrawOver(func(pt *ui.Painter, r ui.Rect) {
			ly := chart.Y(r, limit)
			for lx := r.X; lx < r.X+r.W; lx += 7 {
				pt.Line(lx, ly, min(lx+4, r.X+r.W), ly, 1, dash)
			}
		})
		at := hoveredBar(c, area, xs, func(i int) string { return v.Bars[i].Help })
		var band ui.Element
		if at >= 0 {
			band = chart.Rect(c.Key("hovered"), bandStep(x, at), chart.Span{Hi: 1}).Radius(u.Px(4)).Background(p.Hover)
		}
		for i, b := range v.Bars {
			color := p.Accent.Alpha(0.28)
			switch {
			case b.Today && b.Over:
				color = p.Danger
			case b.Today:
				color = p.Accent
			case b.Over:
				color = p.Warning.Alpha(0.6)
			}
			// A bar without a value still shows where it is.
			top := max(y.At(b.Value*k), 2/height)
			chart.Rect(c.Key(i), x.Band(i), chart.Span{Hi: top}).Role(ui.RoleMeter).Label(b.Help).Radius(u.Px(3)).Background(color)
		}
		if at >= 0 {
			u.ChartTip(c, band, true, func() { u.Text(c, v.Bars[at].Help, 12).FontFeatures("tnum").NoWrap() })
		}
	}).Label(v.Label).Height(height).GapX(u.Px(8)).GapY(u.Px(6))
}

// trendColors are the legend's first parts' colors, then Other's.
func (u Look) trendColors() []ui.Color {
	p := u.P
	return []ui.Color{p.Accent, ui.Hex("#8b5cf6"), ui.Hex("#14a3a3"), ui.Hex("#e08a2c"), ui.Hex("#d6578a"), p.Faint}
}

// PartColor is the color of part i of parts, the last being Other.
func (u Look) PartColor(i, parts int) ui.Color {
	colors := u.trendColors()
	if i == parts-1 {
		return colors[len(colors)-1]
	}
	return colors[min(i, len(colors)-2)]
}

// TrendChart is a bar a bucket, split among the legend's parts from the
// bottom up when stacked, on an axis of v.Tick; the current bucket is
// drawn full, the others a little lighter.
func (u Look) TrendChart(c *ui.Context, v TrendView, height float32) {
	n := len(v.Bars)
	if n == 0 {
		return
	}
	p := u.P
	x := chart.NewBand(make([]string, n)...).Padding(0.25, 0.05)
	// A low top still gets whole ticks, not "0.5 requests".
	y := chart.NewLinear(0, max(v.Top, 4)).Nice(4)
	y.Format = v.Tick
	ticks := y.Ticks(4)
	xs := make([]float32, n)
	var names []chart.Tick
	for i, b := range v.Bars {
		xs[i] = x.At(i)
		if b.Label != "" {
			names = append(names, chart.Tick{At: xs[i], Label: b.Label})
		}
	}
	chart.Plot(c, chart.Axes{Left: ticks, Bottom: names, Label: u.AxisLabel(c)}, func(area ui.Element) {
		u.GridLines(area, ticks)
		at := hoveredBar(c, area, xs, func(i int) string { return v.Bars[i].Help })
		var band ui.Element
		if at >= 0 {
			band = chart.Rect(c.Key("hovered"), bandStep(x, at), chart.Span{Hi: 1}).Radius(u.Px(4)).Background(p.Hover)
		}
		for i, b := range v.Bars {
			lo := 0.0
			for j, part := range b.Parts {
				if part <= 0 {
					continue
				}
				color := p.Accent
				if v.Stacked {
					color = u.PartColor(j, len(b.Parts))
				}
				if !b.Current && i != at {
					color = color.Alpha(0.82)
				}
				chart.Rect(c.Key([2]int{i, j}), x.Band(i), y.Span(lo, lo+part)).Background(color)
				lo += part
			}
		}
		if at >= 0 {
			u.ChartTip(c, band, true, func() { u.trendTip(c, v, v.Bars[at]) })
		}
	}).Label(v.Label).Description(v.About).Height(u.Px(height)).GapX(u.Px(8)).GapY(u.Px(6))
}

// trendTip is a bar's card: its bucket, its total, then each part by its
// color.
func (u Look) trendTip(c *ui.Context, v TrendView, b TrendBar) {
	p := u.P
	u.Text(c, b.Title, 12).FontWeight(600).NoWrap()
	u.Text(c, b.Total, 11).TextColor(p.Secondary).FontFeatures("tnum").NoWrap()
	if len(b.Lines) == 0 {
		return
	}
	ui.Column(c).Gap(u.Px(3)).Padding(u.Px(2), 0, 0).Children(func() {
		for _, l := range b.Lines {
			ui.Row(c.Key(l.Part)).Gap(u.Px(6)).Children(func() {
				ui.Box(c).Size(u.Px(8), u.Px(8)).Radius(u.Px(2)).Background(u.PartColor(l.Part, len(v.Legend)))
				name := u.Text(c, l.Name, 11).TextColor(p.Secondary).NoWrap()
				if l.Part < len(v.Legend)-1 {
					name.Font(u.K.Mono)
				}
				ui.Box(c).Grow(1).MinWidth(u.Px(16))
				u.Text(c, l.Value, 11).FontFeatures("tnum").NoWrap()
			})
		}
	})
}

// Legend is the parts' names by their colors; all but Other, the last,
// in the monospaced font.
func (u Look) Legend(c *ui.Context, names []string) {
	ui.Row(c).Wrap().Gap(u.Px(14)).Children(func() {
		for i, name := range names {
			ui.Row(c.Key(i)).Gap(u.Px(5)).Children(func() {
				ui.Box(c).Size(u.Px(8), u.Px(8)).Radius(u.Px(2)).Background(u.PartColor(i, len(names)))
				t := u.Text(c, name, 11).TextColor(u.P.Secondary).NoWrap()
				if i < len(names)-1 {
					t.Font(u.K.Mono)
				}
			})
		}
	})
}

// HeatMap is v's rows by the hours, each cell darker the more it holds;
// the cell under the pointer shows its card.
func (u Look) HeatMap(c *ui.Context, v HeatView) {
	days := len(v.Rows)
	if days == 0 {
		return
	}
	p := u.P
	levels := []ui.Color{p.Track, p.Accent.Alpha(0.22), p.Accent.Alpha(0.45), p.Accent.Alpha(0.7), p.Accent}
	// The axis counts up from the bottom; the first row is the top one.
	row := func(d int) int { return days - 1 - d }
	y := chart.NewBand(make([]string, days)...).Padding(0.18, 0)
	x := chart.NewBand(make([]string, 24)...).Padding(0.12, 0)
	var left, bottom []chart.Tick
	for d, r := range v.Rows {
		left = append(left, chart.Tick{At: y.At(row(d)), Label: r.Label})
	}
	for h := 0; h < 24; h += 3 {
		bottom = append(bottom, chart.Tick{At: x.At(h), Label: strconv.Itoa(h)})
	}
	chart.Plot(c, chart.Axes{Left: left, Bottom: bottom, Label: u.AxisLabel(c)}, func(area ui.Element) {
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
				e := chart.Rect(c.Key(d*24+h), x.Band(h), y.Band(row(d))).Role(ui.RoleMeter).Label(cell.Help).Radius(u.Px(3)).Background(levels[cell.Level])
				if d == hd && h == hh {
					hovered = e.Border(1.5, p.Text.Alpha(0.55))
				}
			}
		}
		if hd >= 0 {
			cell := v.Rows[hd].Cells[hh]
			u.ChartTip(c, hovered, false, func() {
				u.Text(c, cell.Title, 12).FontWeight(600).FontFeatures("tnum").NoWrap()
				u.Text(c, cell.Value, 11).TextColor(p.Secondary).FontFeatures("tnum").NoWrap()
			})
		}
	}).Label(v.Label).Description(v.Note).Height(u.Px(float32(days*17 + 16))).GapX(u.Px(10)).GapY(u.Px(4))
}
