package app

import (
	"math"
	"sort"
	"strconv"

	"github.com/egoist/mygo/ui"

	jr "github.com/kkkk2323/droi/apps/native/internal/jsonrender"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
)

// renderTree draws a json-render block of a reply: the CLI's terminal
// components, as the web Client's RenderTree draws them.
func renderTree(c *ui.Context, k *kit.Kit, spec *jr.Spec) {
	if spec == nil {
		return
	}
	r := treeRenderer{c: c, k: k, spec: spec}
	ui.Column(c).Margin(k.Px(4), 0).Gap(k.Px(8)).FontSize(k.Px(13)).Children(func() {
		r.node(spec.Root, map[string]bool{})
	})
}

type treeRenderer struct {
	c    *ui.Context
	k    *kit.Kit
	spec *jr.Spec
}

func (r treeRenderer) toneText(t jr.Tone, fallback ui.Color) ui.Color {
	th := r.k.T
	switch t {
	case jr.ToneMuted:
		return th.MutedForeground
	case jr.ToneSuccess:
		return th.Success
	case jr.ToneWarning:
		return th.AttentionForeground
	case jr.ToneError:
		return th.DestructiveForeground
	case jr.ToneInfo:
		return th.InfoForeground
	}
	return fallback
}

func (r treeRenderer) toneFill(t jr.Tone) ui.Color {
	th := r.k.T
	switch t {
	case jr.ToneMuted:
		return th.MutedForeground.Alpha(0.6)
	case jr.ToneSuccess:
		return th.Success
	case jr.ToneWarning:
		return th.Attention
	case jr.ToneError:
		return th.Destructive
	case jr.ToneInfo:
		return th.Info
	}
	return th.Foreground.Alpha(0.7)
}

func toneIcon(t jr.Tone) string {
	switch t {
	case jr.ToneSuccess:
		return "circle-check"
	case jr.ToneWarning:
		return "triangle-alert"
	case jr.ToneError:
		return "circle-x"
	}
	return "info"
}

// cells is the CLI's terminal cells of spacing, 4px each, up to 8.
func (r treeRenderer) cells(p map[string]any, key string, def float64) float32 {
	n, ok := jr.Num(p, key)
	if !ok {
		n = def
	}
	return r.k.Px(float32(math.Min(8, math.Max(0, n)) * 4))
}

func (r treeRenderer) children(id string, seen map[string]bool) {
	for _, ch := range jr.ChildrenOf(r.spec, id, seen) {
		r.node(ch, seen)
	}
}

func (r treeRenderer) node(id string, seen map[string]bool) {
	el, ok := r.spec.Elements[id]
	if !ok {
		return
	}
	below := make(map[string]bool, len(seen)+1)
	for k := range seen {
		below[k] = true
	}
	below[id] = true
	c, k, t, p := r.c, r.k, r.k.T, el.Props
	text := func(s string, size, lh float32) *ui.Element { return k.Text(c, s, size, lh) }
	switch el.Type {
	case "Box":
		box := ui.Column(c).Wrap().MinWidth(0).Gap(r.cells(p, "gap", 0)).Padding(r.cells(p, "padding", 0))
		if jr.Str(p, "flexDirection") == "row" {
			box.Row()
		}
		if bs := jr.Str(p, "borderStyle"); bs != "" && bs != "none" {
			box.Radius(k.Px(8)).Border(1, t.Border)
		}
		box.Children(func() { r.children(id, below) })
	case "Text":
		el := text(jr.Str(p, "text"), 13, 20).TextColor(r.toneText(jr.ToneOf(jr.Str(p, "color")), t.Foreground)).MinWidth(0)
		if p["bold"] == true {
			el.FontWeight(600)
		}
	case "Heading":
		level, _ := jr.Num(p, "level")
		size := float32(13.5)
		if level == 1 {
			size = 15
		}
		text(jr.Str(p, "text"), size, 20).Role(ui.RoleHeading).FontWeight(600)
	case "Divider":
		title := jr.Str(p, "title")
		if title == "" {
			ui.Box(c).Height(1).Background(t.Border)
			return
		}
		ui.Row(c).Gap(k.Px(8)).Children(func() {
			ui.Box(c).Height(1).Grow(1).Background(t.Border)
			text(title, 11, 16.5).TextColor(t.MutedForeground)
			ui.Box(c).Height(1).Grow(1).Background(t.Border)
		})
	case "Newline":
		ui.Box(c).Height(k.Px(8))
	case "Spacer":
		ui.Spacer(c)
	case "List":
		ordered := p["ordered"] == true
		ui.Column(c).Gap(k.Px(2)).Padding(0, 0, 0, k.Px(20)).Children(func() {
			for i, item := range jr.Strings(p, "items") {
				ui.Row(c).AlignItems(ui.Start).Children(func() {
					if ordered {
						text(strconv.Itoa(i+1)+".", 13, 20).TextColor(t.MutedForeground).Absolute().Left(-k.Px(20))
					} else {
						kit.Dot(c, k.Px(5), t.Foreground).Absolute().Left(-k.Px(12)).Top(k.Px(7.5))
					}
					text(item, 13, 20)
				})
			}
		})
	case "Card":
		title := jr.Str(p, "title")
		card := ui.Column(c).Gap(k.Px(8)).Padding(r.cells(p, "padding", 3)).Radius(k.Px(12)).Border(1, t.Border).Background(t.Card.Alpha(0.6))
		if title != "" {
			card.Role(ui.RoleGroup).Label(title)
		}
		card.Children(func() {
			if title != "" {
				text(title, 12, 16).FontWeight(600).TextColor(t.MutedForeground)
			}
			r.children(id, below)
		})
	case "StatusLine":
		tone := jr.ToneOf(jr.Str(p, "status"))
		ui.Row(c).Gap(k.Px(6)).Children(func() {
			label := jr.Str(p, "status")
			if label == "" {
				label = "Status"
			}
			k.Icon(c, toneIcon(tone), 14, r.toneText(tone, t.Foreground)).Label(label)
			text(jr.Str(p, "text"), 13, 20)
		})
	case "KeyValue":
		ui.Row(c).AlignItems(ui.Start).Gap(k.Px(12)).Children(func() {
			text(jr.Str(p, "label"), 13, 20).TextColor(t.MutedForeground).Width(k.Px(128)).Shrink(0)
			text(jr.Str(p, "value"), 12.5, 20).Font(k.Mono).MinWidth(0)
		})
	case "Badge":
		tone := jr.ToneOf(jr.Str(p, "variant"))
		ui.Row(c).AlignSelf(ui.Start).Height(k.Px(20)).PaddingX(k.Px(6)).Radius(k.Px(6)).Border(1, t.Border).Children(func() {
			text(jr.Str(p, "label"), 11, 16.5).FontWeight(500).TextColor(r.toneText(tone, t.Foreground))
		})
	case "ProgressBar":
		prog, _ := jr.Num(p, "progress")
		value := jr.Fraction(prog, 1)
		label := jr.Str(p, "label")
		ui.Row(c).Gap(k.Px(8)).Children(func() {
			if label != "" {
				text(label, 13, 20).TextColor(t.MutedForeground).Shrink(0)
			}
			name := label
			if name == "" {
				name = "Progress"
			}
			ui.Box(c).Role(ui.RoleProgress).Label(name).Height(k.Px(6)).MinWidth(k.Px(64)).Grow(1).Radius(k.Px(3)).Background(t.Muted).Clip().Children(func() {
				ui.Box(c).FillHeight().WidthPercent(float32(value * 100)).Radius(k.Px(3)).Background(t.Primary)
			})
			text(strconv.Itoa(int(math.Round(value*100)))+"%", 11.5, 17.25).TextColor(t.MutedForeground).TextAlign(ui.End).Width(k.Px(36)).FontFeatures("tnum")
		})
	case "Metric":
		trend := jr.Str(p, "trend")
		ui.Column(c).Children(func() {
			text(jr.Str(p, "label"), 11.5, 17.25).TextColor(t.MutedForeground)
			ui.Row(c).Gap(k.Px(6)).Children(func() {
				text(jr.Str(p, "value"), 18, 28).FontWeight(600).FontFeatures("tnum")
				if trend == "up" || trend == "down" {
					k.Icon(c, "trending-"+trend, 16, t.MutedForeground).Label("Trend " + trend)
				}
			})
		})
	case "Callout":
		kind := jr.Str(p, "type")
		tone := jr.ToneOf(kind)
		icon := toneIcon(tone)
		switch {
		case kind == "tip":
			icon = "lightbulb"
		case tone == jr.ToneDefault:
			icon = "circle-alert"
		}
		title := jr.Str(p, "title")
		name := title
		if name == "" {
			name = kind
		}
		if name == "" {
			name = "Note"
		}
		ui.Row(c).Role(ui.RoleGroup).Label(name).AlignItems(ui.Start).Gap(k.Px(8)).Padding(k.Px(8), k.Px(12)).Radius(k.Px(8)).
			Border(1, t.Border).Background(t.Card.Alpha(0.6)).Children(func() {
			k.Icon(c, icon, 14, r.toneText(tone, t.Foreground)).Margin(k.Px(2), 0, 0, 0)
			ui.Column(c).MinWidth(0).Children(func() {
				if title != "" {
					text(title, 13, 20).FontWeight(600)
				}
				text(jr.Str(p, "content"), 13, 20).TextColor(t.MutedForeground)
			})
		})
	case "Table":
		r.table(p)
	case "BarChart":
		r.barChart(p)
	case "Sparkline":
		r.sparkline(p)
	case "Timeline":
		r.timeline(p)
	default:
		ui.Column(c).Gap(k.Px(8)).Children(func() { r.children(id, below) })
	}
}

func (r treeRenderer) table(p map[string]any) {
	c, k, t := r.c, r.k, r.k.T
	rows := jr.Records(p, "rows")
	type col struct{ key, header string }
	var cols []col
	for _, d := range jr.Records(p, "columns") {
		h := jr.Str(d, "header")
		if h == "" {
			h = jr.Str(d, "key")
		}
		cols = append(cols, col{jr.Str(d, "key"), h})
	}
	if len(cols) == 0 && len(rows) > 0 {
		// Go maps keep no order: the columns go alphabetically.
		keys := make([]string, 0, len(rows[0]))
		for key := range rows[0] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			cols = append(cols, col{key, key})
		}
	}
	if len(cols) == 0 {
		return
	}
	ui.ScrollHorizontal(c).Radius(k.Px(8)).Border(1, t.Border).Clip().Children(func() {
		ui.Grid(c).Columns(len(cols)).Grow(1).Children(func() {
			for _, col := range cols {
				ui.Box(c).Padding(k.Px(6), k.Px(10)).Background(t.Muted.Alpha(0.4)).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
					k.Text(c, col.header, 12.5, 18.75).FontWeight(500).NoWrap()
				})
			}
			for i, row := range rows {
				for _, col := range cols {
					cell := ui.Box(c).Padding(k.Px(6), k.Px(10))
					if i < len(rows)-1 {
						cell.BorderWidth(0, 0, 1, 0).BorderColor(t.Border)
					}
					cell.Children(func() { k.Text(c, jr.Cell(row[col.key]), 12.5, 18.75) })
				}
			}
		})
	})
}

func (r treeRenderer) barChart(p map[string]any) {
	c, k, t := r.c, r.k, r.k.T
	type bar struct {
		label string
		value float64
		tone  jr.Tone
	}
	var data []bar
	maxV, total := 0.0, 0.0
	for _, d := range jr.Records(p, "data") {
		v, _ := jr.Num(d, "value")
		data = append(data, bar{jr.Str(d, "label"), v, jr.ToneOf(jr.Str(d, "color"))})
		maxV = math.Max(maxV, v)
		total += math.Max(0, v)
	}
	percent := p["showPercentage"] == true
	ui.Grid(c).Role(ui.RoleList).Label("Bar chart").ColumnTracks(ui.FitContent(), ui.Fr(1), ui.FitContent()).GapX(k.Px(10)).GapY(k.Px(4)).Children(func() {
		for _, d := range data {
			shown := jsNum(d.value)
			if percent {
				shown = strconv.Itoa(int(math.Round(jr.Fraction(d.value, total)*100))) + "%"
			}
			k.Text(c, d.label, 13, 20).TextColor(t.MutedForeground).SingleLine().Label(d.label + ": " + shown)
			ui.Box(c).Height(k.Px(8)).AlignSelf(ui.Center).Radius(k.Px(4)).Background(t.Muted).Clip().Children(func() {
				ui.Box(c).FillHeight().WidthPercent(float32(jr.Fraction(d.value, maxV) * 100)).Radius(k.Px(4)).Background(r.toneFill(d.tone))
			})
			k.Text(c, shown, 11.5, 17.25).TextAlign(ui.End).FontFeatures("tnum")
		}
	})
}

func jsNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func (r treeRenderer) sparkline(p map[string]any) {
	c, k, t := r.c, r.k, r.k.T
	data := jr.Numbers(p, "data")
	if len(data) < 2 {
		return
	}
	lo, hi := data[0], data[0]
	for _, v := range data {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	color := r.toneText(jr.ToneOf(jr.Str(p, "color")), t.Foreground.Alpha(0.7))
	label := "Sparkline from " + jsNum(data[0]) + " to " + jsNum(data[len(data)-1]) + ", range " + jsNum(lo) + " to " + jsNum(hi)
	ui.Box(c).Role(ui.RoleImage).Label(label).Size(k.Px(120), k.Px(24)).Draw(func(pt *ui.Painter, b ui.Rect) {
		path := new(ui.Path)
		for i, v := range data {
			x := b.X + float32(i)/float32(len(data)-1)*b.W
			y := b.Y + b.H - k.Px(2) - float32(jr.Fraction(v-lo, hi-lo))*(b.H-k.Px(4))
			if i == 0 {
				path.MoveTo(x, y)
			} else {
				path.LineTo(x, y)
			}
		}
		pt.StrokePath(path, k.Px(1.5), color)
	})
}

func (r treeRenderer) timeline(p map[string]any) {
	c, k, t := r.c, r.k, r.k.T
	items := jr.Records(p, "items")
	ui.Column(c).Role(ui.RoleList).Children(func() {
		for i, item := range items {
			tone := jr.ToneOf(jr.Str(item, "status"))
			dot := tone
			if dot == jr.ToneDefault {
				dot = jr.ToneMuted
			}
			last := i == len(items)-1
			pad := k.Px(8)
			if last {
				pad = 0
			}
			ui.Row(c).AlignItems(ui.Start).Gap(k.Px(10)).Padding(0, 0, pad, 0).Children(func() {
				ui.Column(c).Width(k.Px(8)).AlignSelf(ui.Stretch).AlignItems(ui.Center).Shrink(0).Children(func() {
					kit.Dot(c, k.Px(8), r.toneFill(dot)).Margin(k.Px(6), 0, 0, 0)
					if !last {
						ui.Box(c).Width(1).Grow(1).Margin(k.Px(2), 0, -pad-k.Px(4), 0).Background(t.Border)
					}
				})
				ui.Column(c).MinWidth(0).Children(func() {
					spans := []ui.Span{{Text: jr.Str(item, "title"), Weight: 500}}
					if st := jr.Str(item, "status"); st != "" {
						spans = append(spans, ui.Span{Text: "  " + st, Size: k.Px(11), Color: r.toneText(tone, t.MutedForeground)})
					}
					ui.RichText(c, spans...).FontSize(k.Px(13)).FixedLineHeight(k.Px(20))
					if d := jr.Str(item, "description"); d != "" {
						k.Text(c, d, 13, 20).TextColor(t.MutedForeground)
					}
				})
			})
		}
	})
}
