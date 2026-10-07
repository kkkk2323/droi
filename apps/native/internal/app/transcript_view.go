package app

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/internal/jsonrender"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
	"github.com/kkkk2323/droi/apps/native/internal/sessions"
	"github.com/kkkk2323/droi/apps/native/internal/subagents"
	"github.com/kkkk2323/droi/apps/native/internal/transcript"
)

const resultPreviewLines = 40

// transcriptView is the MessageList: the entries in a list that follows
// its end, the lead above them (previous messages), the activity row
// below them, and the button back to the latest.
func (v *sessionView) transcriptView(c *ui.Context, s *session.Session, listed []sessions.Summary) {
	a := v.a
	k, t := a.kit, a.kit.T
	ws := string(s.WorkingState())
	if v.compacting {
		ws = string(protocol.DroidWorkingStateCompactingConversation)
	}
	running := ws != string(protocol.DroidWorkingStateIdle)
	entries := v.entries
	hasOlder := s.HasOlderMessages()
	if len(entries) == 0 && !hasOlder {
		ui.Box(c).Fill().Center().Children(func() {
			k.Text(c, "What should Droid work on?", 14, 20).TextColor(t.MutedForeground)
		})
		return
	}
	var streamingID string
	if ws == string(protocol.DroidWorkingStateStreamingAssistantMessage) && len(entries) > 0 && !entries[len(entries)-1].User {
		streamingID = entries[len(entries)-1].ID
	}
	ends := transcript.TurnEnds(entries, running)
	activity := transcript.WorkingLabel(ws)
	v.links = &subagents.Links{ByToolUse: subagents.ByToolUse(listed), Runs: a.subagentRuns(listed)}
	v.streamingID = streamingID
	rows := v.rows(entries, streamingID, ends)

	// Row 0 is the lead, then the entries' rows, then the activity row.
	n := len(rows) + 2
	v.list.Key = func(i int) any {
		switch {
		case i == 0:
			return "lead"
		case i == n-1:
			return "activity"
		}
		return rows[i-1].key
	}
	ui.Box(c).Fill().Children(func() {
		list := ui.List(c, &v.list, n, func(i int) {
			switch {
			case i == 0:
				v.lead(c, s)
			case i == n-1:
				v.activityRow(c, activity)
			default:
				v.row(c, rows[i-1])
			}
		}).Fill().Label("Transcript").PaddingX(scrollGutter)
		_ = list
		if first, _ := v.list.Visible(); first == 0 && hasOlder {
			v.loadOlder()
		}
		if !v.list.AtEnd() {
			b := k.Button(c, kit.Outline, 32, true, "Scroll to latest").Radius(k.Px(16)).Absolute().Bottom(k.Px(12)).
				Left(0).Right(0).Margin(0, ui.Auto, 0, ui.Auto).Shadow(0, k.Px(4), k.Px(12), 0, ui.RGBA(0, 0, 0, 0.08))
			b.Children(func() { k.Icon(c, "arrow-down", 16, t.Foreground) })
			if b.Clicked() {
				v.list.FollowEnd = true
				v.list.ScrollToEnd()
			}
		}
	})
}

// scrollGutter is the room the web Client's transcript keeps for its
// scrollbar on both sides (scrollbar-gutter: stable both-edges), which
// centers the column in the rest.
const scrollGutter = 11

// column is the reading column: max-w-3xl centered, px-6.
func (v *sessionView) column(c *ui.Context) *ui.Element {
	k := v.a.kit
	return ui.Column(c).FillWidth().MaxWidth(k.Px(columnWidth)).AlignSelf(ui.Center).PaddingX(k.Px(24))
}

// lead is the ListHeader: 12px of room, then the button for previous
// messages while there are some.
func (v *sessionView) lead(c *ui.Context, s *session.Session) {
	a := v.a
	k, t := a.kit, a.kit.T
	ui.Column(c).FillWidth().Children(func() {
		ui.Box(c).Height(k.Px(12))
		if !s.HasOlderMessages() {
			return
		}
		v.mu.Lock()
		loading, err := v.olderLoading, v.olderErr
		v.mu.Unlock()
		v.column(c).Padding(0, k.Px(24), k.Px(8), k.Px(24)).Children(func() {
			if err != "" {
				k.Text(c, "Previous messages did not load: "+err, 12, 16).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
				return
			}
			label := "Load previous messages"
			if loading {
				label = "Loading previous messages…"
			}
			b := ui.ButtonBase(c).Label(label).AlignSelf(ui.Start).Gap(k.Px(6)).Padding(k.Px(4), k.Px(6)).Radius(k.Px(6)).Disabled(loading).Cursor(ui.CursorPointer)
			color := t.MutedForeground
			if b.Hovered() {
				color = t.Foreground
			}
			b.Children(func() {
				if loading {
					k.Spinner(c, 14, color)
				} else {
					k.Icon(c, "chevron-up", 14, color)
				}
				k.Text(c, label, 12, 16).TextColor(color)
			})
			if b.Clicked() {
				v.loadOlder()
			}
		})
	})
}

// activityRow is the live turn's closing row: three dots and what the
// Droid is doing, empty while idle.
func (v *sessionView) activityRow(c *ui.Context, label string) {
	a := v.a
	k, t := a.kit, a.kit.T
	v.column(c).Row().Role(ui.RoleStatus).Label("Session activity").Gap(k.Px(8)).Padding(0, k.Px(24), k.Px(16), k.Px(24)).Children(func() {
		if label == "" {
			return
		}
		ui.Row(c).Gap(k.Px(2)).Children(func() {
			for i := range 3 {
				bounceDot(c, k, i, t.MutedForeground)
			}
		})
		k.Text(c, label, 13, 19.5).TextColor(t.MutedForeground)
	})
}

// bounceDot is one of Tailwind's animate-bounce dots, a quarter of a
// second apart: up 25% of the line and back each second.
func bounceDot(c *ui.Context, k *kit.Kit, i int, color ui.Color) {
	const period = 1000
	ms := (int(c.Now().UnixMilli()) + period - i*150) % period
	c.After(33 * time.Millisecond)
	p := float32(ms) / period
	var y float32
	if p < 0.5 {
		y = -k.Px(4) * (1 - (2*p)*(2*p))
	} else {
		q := 2*p - 1
		y = -k.Px(4) * q * q
	}
	kit.Dot(c, k.Px(4), color).Margin(y, 0, -y, 0)
}

// pulseWave is Tailwind's animate-pulse, 1 at rest and 0.5 halfway
// through each 2s.
func pulseWave(c *ui.Context) float64 {
	ms := c.Now().UnixMilli() % 2000
	c.After(50 * time.Millisecond)
	x := float64(ms) / 1000
	if x > 1 {
		x = 2 - x
	}
	return 1 - x
}

// reply is a text block: Markdown, with the json-render blocks a reply
// can carry drawn as their components.
func (v *sessionView) reply(c *ui.Context, f *flow, text string, color ui.Color) {
	p := newProse(v.a.kit)
	p.color = color
	if !jsonrender.HasRenderTag(text) {
		p.nodes(c, f, v.doc(text))
		return
	}
	for _, seg := range jsonrender.SplitReply(text) {
		switch seg.Kind {
		case jsonrender.Markdown:
			p.nodes(c, f, v.doc(seg.Text))
		case jsonrender.Render:
			f.block(v.a.kit.Px(8), v.a.kit.Px(8), func() { renderTree(c, v.a.kit, seg.Spec) })
		case jsonrender.Pending:
			f.block(v.a.kit.Px(8), v.a.kit.Px(8), func() {
				v.a.kit.Text(c, "Rendering…", 13, 19.5).TextColor(v.a.kit.T.MutedForeground)
			})
		}
	}
}

func (v *sessionView) picture(c *ui.Context, id string, img transcript.Image, maxH float32, label string) {
	k := v.a.kit
	bm := v.image(id, img)
	if bm == nil {
		return
	}
	w, h := bm.Size()
	scale := float32(1)
	if float32(h) > k.Px(maxH) {
		scale = k.Px(maxH) / float32(h)
	}
	// Width and aspect ratio, not a fixed size, so a wide image narrows
	// to its column as the web Client's max-w-full object-contain does.
	ui.Image(c, bm).Label(label).Width(float32(w)*scale).MaxWidthPercent(100).AspectRatio(float32(w)/float32(h)).
		Shrink(1).MinWidth(0).Radius(k.Px(12)).Border(1, k.T.Border)
}

// disclosure is the trigger of a fold: label and a chevron that turns as
// it opens; quiet until hovered.
func (v *sessionView) disclosure(c *ui.Context, open *bool, label string, size, lh float32, weight int, leading func(color ui.Color)) ui.CollapsibleParts {
	k, t := v.a.kit, v.a.kit.T
	p := ui.CollapsibleBase(c, open)
	v.holdOnToggle(p.Trigger)
	tr := p.Trigger.AlignSelf(ui.Start).Gap(k.Px(6)).Radius(k.Px(8)).Margin(0, 0, 0, -k.Px(6)).Cursor(ui.CursorPointer)
	color := t.MutedForeground
	if tr.Hovered() {
		color = t.Foreground
	}
	tr.Children(func() {
		if leading != nil {
			leading(color)
		}
		labelEl := k.Text(c, label, size, lh).TextColor(color)
		if weight > 0 {
			labelEl.FontWeight(weight)
		}
		chevron := float32(14)
		if size < 13 {
			chevron = 12
		}
		k.Icon(c, "chevron-right", chevron, color).Rotate(90 * p.Progress())
	})
	return p
}

// thinking is the ThinkingSection: "Reasoned for 3s", folding the
// reasoning as muted Markdown behind a rule.
func (v *sessionView) thinking(c *ui.Context, b transcript.Block, streaming bool) {
	k, t := v.a.kit, v.a.kit.T
	label := "Reasoning"
	switch {
	case b.DurationMs != nil && *b.DurationMs > 0:
		label = "Reasoned for " + transcript.FormatDuration(*b.DurationMs)
	case streaming:
		label = "Reasoning…"
	}
	open := v.flag("thinking:"+b.ID, false)
	ui.Column(c).Children(func() {
		p := v.disclosure(c, open, label, 13, 19.5, 0, nil)
		p.Trigger.Padding(k.Px(4), k.Px(6))
		p.Panel(func() {
			pr := newProse(k)
			pr.size, pr.lh, pr.color = 14, 24, t.MutedForeground
			doc := v.doc(b.Text)
			ui.Column(c).Margin(max(k.Px(4), pr.firstMargin(doc)), 0, 0, 0).BorderWidth(0, 0, 0, 2).BorderColor(t.Border).Padding(0, 0, 0, k.Px(16)).Children(func() {
				f := flow{c: c, open: true}
				pr.nodes(c, &f, doc)
			})
		})
	})
}

var toolIcons = map[string]string{
	"Execute":    "terminal",
	"Read":       "file-text",
	"Edit":       "file-pen",
	"Create":     "file-plus",
	"ApplyPatch": "file-pen",
	"Grep":       "search",
	"Glob":       "folder-search",
	"LS":         "folder-search",
	"FetchUrl":   "globe",
	"WebSearch":  "globe",
	"Skill":      "book-open",
}

// cluster is a ToolCluster's trigger, "Used Read, Edit"; its calls are
// rows of their own (see rows), shown while it is open, as by default.
func (v *sessionView) cluster(c *ui.Context, b transcript.Block) {
	k, t := v.a.kit, v.a.kit.T
	open := v.flag("cluster:"+b.ID, true)
	label, pending := transcript.ClusterLabel(b.Calls)
	ui.Column(c).Children(func() {
		tr := ui.ButtonBase(c).Label(label).Expanded(*open).AlignSelf(ui.Start).Height(k.Px(24)).Gap(k.Px(6)).PaddingX(k.Px(6)).
			Margin(0, 0, 0, -k.Px(6)).Radius(k.Px(8)).Cursor(ui.CursorPointer)
		color := t.MutedForeground
		if tr.Hovered() {
			color = t.Foreground
		}
		tr.Children(func() {
			if pending {
				k.Spinner(c, 12, color)
			}
			k.Text(c, label, 12, 16).FontWeight(500).TextColor(color)
			rot := float32(0)
			if *open {
				rot = 90
			}
			k.Icon(c, "chevron-right", 12, color).Rotate(rot)
		})
		if tr.Clicked() {
			*open = !*open
			v.hold()
		}
	})
}

// rowTrigger is the 28px trigger of a tool row; it lights on hover.
func (v *sessionView) rowTrigger(c *ui.Context, open *bool, label string) ui.CollapsibleParts {
	k, t := v.a.kit, v.a.kit.T
	p := ui.CollapsibleBase(c, open)
	v.holdOnToggle(p.Trigger)
	tr := p.Trigger.Label(label).FillWidth().Height(k.Px(28)).Gap(k.Px(8)).PaddingX(k.Px(6)).Margin(0, 0, 0, -k.Px(6)).
		Radius(k.Px(8)).FontSize(k.Px(12.5)).Cursor(ui.CursorPointer)
	if tr.Hovered() {
		tr.Background(t.Muted.Alpha(0.7))
	}
	return p
}

// toolRow is a ToolRow: icon, name, the input's summary, the diff's
// counts, and whether it ran; it opens to the input and the result.
func (v *sessionView) toolRow(c *ui.Context, call *transcript.ToolCall) {
	k, t := v.a.kit, v.a.kit.T
	server, name := transcript.ToolName(call.Use.Name)
	icon := toolIcons[call.Use.Name]
	if icon == "" {
		icon = "wrench"
		if server != "" {
			icon = "plug"
		}
	}
	res := v.result(call)
	open := v.flag("tool:"+call.Use.ID, false)
	ui.Column(c).Key(call.Use.ID).Children(func() {
		p := v.rowTrigger(c, open, call.Use.Name+": "+transcript.Summary(call))
		hover := p.Trigger.Hovered() || p.Trigger.Focused()
		p.Trigger.Children(func() {
			fg, iconColor := t.Foreground, t.MutedForeground
			if res.IsError {
				fg, iconColor = t.DestructiveForeground, t.DestructiveForeground
			}
			k.Icon(c, icon, 14, iconColor)
			if server != "" {
				k.Text(c, server, 11.5, 17.25).TextColor(t.MutedForeground.Alpha(0.7)).Shrink(0)
			}
			k.Text(c, name, 12.5, 18.75).FontWeight(500).TextColor(fg).Shrink(0)
			parts := transcript.SummaryParts(call)
			spans := make([]ui.Span, 0, 3*len(parts))
			for i, part := range parts {
				if i > 0 {
					spans = append(spans, ui.Span{Text: " · ", Color: t.MutedForeground.Alpha(0.5)})
				}
				if part.Key != "" {
					spans = append(spans, ui.Span{Text: part.Key + ": ", Color: t.MutedForeground.Alpha(0.6)})
				}
				spans = append(spans, ui.Span{Text: part.Value})
			}
			ui.RichText(c, spans...).Font(k.Mono).FontSize(k.Px(11.5)).FixedLineHeight(k.Px(17.25)).TextColor(t.MutedForeground).
				SingleLine().Grow(1).Shrink(1).MinWidth(0)
			if d := res.Diff; d != nil {
				ui.RichText(c, ui.Span{Text: "+" + strconv.Itoa(d.Added), Color: t.Added}, ui.Span{Text: " "}, ui.Span{Text: "−" + strconv.Itoa(d.Removed), Color: t.Removed}).
					Font(k.Mono).FontSize(k.Px(11)).FontFeatures("tnum").Shrink(0)
			}
			if res.Pending {
				k.Spinner(c, 14, t.MutedForeground).Role(ui.RoleStatus).Label("Running")
				return
			}
			if res.IsError {
				k.Icon(c, "circle-x", 14, t.DestructiveForeground).Label("Failed")
			} else {
				k.Icon(c, "check", 14, t.Success).Label("Succeeded")
			}
			ch := k.Icon(c, "chevron-right", 14, t.MutedForeground).Rotate(90 * p.Progress())
			if !hover && !*open && p.Progress() == 0 {
				ch.Opacity(0)
			}
		})
		p.Panel(func() {
			v.toolDetail(c, call, res)
		})
	})
}

// toolDetail is a row's panel: the input, then the diff, the status or
// the result's first lines, and its pictures.
func (v *sessionView) toolDetail(c *ui.Context, call *transcript.ToolCall, res transcript.ToolResultView) {
	k, t := v.a.kit, v.a.kit.T
	mono := func(text string, color ui.Color) *ui.Element {
		return k.Text(c, text, 11.5, 20).Font(k.Mono).TextColor(color).Selectable()
	}
	ui.Column(c).Margin(k.Px(2), 0, k.Px(4), 0).Radius(k.Px(10)).Border(1, t.Border).Background(t.Card.Alpha(0.6)).Clip().Children(func() {
		input := transcript.InputText(call)
		if res.Diff != nil {
			ui.Box(c).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Padding(k.Px(6), k.Px(10)).Children(func() { mono(input, t.Foreground) })
			v.diffView(c, res.Diff)
		} else {
			ui.Scroll(c).MaxHeight(k.Px(384)).Children(func() {
				ui.Column(c).Padding(k.Px(10)).Children(func() {
					mono(input, t.Foreground)
					switch {
					case res.Status != nil:
						color, icon, msg := t.Success, "check", "Succeeded"
						if !res.Status.Success {
							color, icon, msg = t.DestructiveForeground, "circle-x", "Failed"
						}
						if res.Status.Message != "" {
							msg = res.Status.Message
						}
						ui.Row(c).Gap(k.Px(6)).Margin(k.Px(4), 0, 0, 0).Children(func() {
							k.Icon(c, icon, 14, color)
							mono(msg, color)
						})
					case res.Text != "":
						color := t.MutedForeground
						if res.IsError {
							color = t.DestructiveForeground
						}
						mono(transcript.TruncateLines(res.Text, resultPreviewLines), color)
					}
				})
			})
		}
		for i, img := range res.Images {
			ui.Box(c).Padding(0, k.Px(10), k.Px(10), k.Px(10)).Children(func() {
				v.picture(c, call.Use.ID+":"+strconv.Itoa(i), img, 320, "Picture from "+call.Use.Name)
			})
		}
	})
}

// diffView is the DiffView: both line numbers, the sign, the line, tinted
// green and red.
func (v *sessionView) diffView(c *ui.Context, d *transcript.Diff) {
	k, t := v.a.kit, v.a.kit.T
	width := 1
	for _, l := range d.Lines {
		width = max(width, len(strconv.Itoa(max(l.Old, l.New))))
	}
	ch := k.Px(11.5) * 0.6
	num := func(n int) string {
		if n == 0 {
			return ""
		}
		return strconv.Itoa(n)
	}
	ui.ScrollBoth(c).MaxHeight(k.Px(384)).Label("Diff").Children(func() {
		ui.Column(c).PaddingY(k.Px(4)).MinWidthPercent(100).Children(func() {
			for _, l := range d.Lines {
				bg, fg, sign := ui.Transparent, t.Foreground, " "
				switch l.Type {
				case "added":
					bg, fg, sign = t.Added.Alpha(0.1), t.AddedForeground, "+"
				case "removed":
					bg, fg, sign = t.Removed.Alpha(0.1), t.RemovedForeground, "−"
				}
				ui.Row(c).Background(bg).AlignItems(ui.Start).Children(func() {
					ui.Row(c).Gap(k.Px(6)).Padding(0, k.Px(8), 0, k.Px(10)).Shrink(0).Children(func() {
						for _, n := range []int{l.Old, l.New} {
							k.Text(c, num(n), 11.5, 20).Font(k.Mono).TextColor(t.MutedForeground.Alpha(0.6)).FontFeatures("tnum").
								MinWidth(ch * float32(width)).TextAlign(ui.End)
						}
					})
					k.Text(c, sign, 11.5, 20).Font(k.Mono).TextColor(fg).Width(k.Px(16)).Shrink(0)
					k.Text(c, l.Content, 11.5, 20).Font(k.Mono).TextColor(fg).NoWrap().Padding(0, k.Px(12), 0, 0).Selectable()
				})
			}
		})
	})
}

// scriptGroup is a ScriptGroup's row, which opens to its program, inputs
// and output; the calls its run made are rows of their own (see rows).
func (v *sessionView) scriptGroup(c *ui.Context, call *transcript.ToolCall) {
	k, t := v.a.kit, v.a.kit.T
	run := v.scriptRun(call)
	summary := v.scriptSummary(call)
	failed := run.Status == "failed" || run.Status == "cancelled"
	open := v.flag("script:"+call.Use.ID, false)
	ui.Column(c).Key(call.Use.ID).Children(func() {
		p := v.rowTrigger(c, open, call.Use.Name+": "+summary)
		hover := p.Trigger.Hovered() || p.Trigger.Focused()
		p.Trigger.Children(func() {
			fg, iconColor := t.Foreground, t.MutedForeground
			if failed {
				fg, iconColor = t.DestructiveForeground, t.DestructiveForeground
			}
			k.Icon(c, "braces", 14, iconColor)
			k.Text(c, "Script", 12.5, 18.75).FontWeight(500).TextColor(fg).Shrink(0)
			k.Text(c, summary, 11.5, 17.25).Font(k.Mono).TextColor(t.MutedForeground).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			switch {
			case call.Result == nil:
				k.Spinner(c, 14, t.MutedForeground).Role(ui.RoleStatus).Label("Running")
			case run.Status == "running":
				k.Icon(c, "clock", 14, t.MutedForeground).Label("Still running")
			case run.Status == "stalled":
				k.Icon(c, "circle-alert", 14, t.Attention).Label("Stalled")
			case failed:
				label := "Failed"
				if run.Status == "cancelled" {
					label = "Cancelled"
				}
				k.Icon(c, "circle-x", 14, t.DestructiveForeground).Label(label)
			default:
				k.Icon(c, "check", 14, t.Success).Label("Succeeded")
			}
			if call.Result != nil {
				ch := k.Icon(c, "chevron-right", 14, t.MutedForeground).Rotate(90 * p.Progress())
				if !hover && !*open && p.Progress() == 0 {
					ch.Opacity(0)
				}
			}
		})
		p.Panel(func() { v.scriptDetail(c, call, run) })
	})
}

func (v *sessionView) scriptDetail(c *ui.Context, call *transcript.ToolCall, run transcript.ScriptRun) {
	k, t := v.a.kit, v.a.kit.T
	section := func(label string, fn func()) {
		ui.Column(c).Role(ui.RoleGroup).Label(label).Children(func() {
			k.Text(c, strings.ToUpper(label), 10.5, 15.75).FontWeight(500).LetterSpacing(k.Px(0.26)).TextColor(t.MutedForeground.Alpha(0.8)).
				Padding(k.Px(6), k.Px(10), k.Px(4), k.Px(10))
			fn()
		})
	}
	pre := func(text string, color ui.Color, maxH float32) {
		ui.Scroll(c).MaxHeight(k.Px(maxH)).Children(func() {
			k.Text(c, text, 11.5, 20).Font(k.Mono).TextColor(color).Selectable().Padding(0, k.Px(10), k.Px(8), k.Px(10))
		})
	}
	divider := func() { ui.Box(c).Height(1).Background(t.Border) }
	ui.Column(c).Margin(k.Px(2), 0, k.Px(4), 0).Radius(k.Px(10)).Border(1, t.Border).Background(t.Card.Alpha(0.6)).Clip().Children(func() {
		first := true
		sep := func() {
			if !first {
				divider()
			}
			first = false
		}
		script, inputs, ok := transcript.ScriptSource(call)
		if ok {
			sep()
			section("Source", func() { sourceView(c, k, script, nil) })
			for _, in := range inputs {
				sep()
				section("inputs."+in.Name, func() { pre(transcript.TruncateLines(in.Text, resultPreviewLines), t.MutedForeground, 240) })
			}
		}
		if run.Error != "" || run.Output != "" || run.Value != "" || len(run.Images) > 0 {
			sep()
			section("Output", func() {
				var spans []ui.Span
				if run.Output != "" {
					spans = append(spans, ui.Span{Text: transcript.TruncateLines(run.Output, resultPreviewLines)})
				}
				if run.Value != "" {
					lead := ""
					if run.Output != "" {
						lead = "\n"
					}
					spans = append(spans, ui.Span{Text: lead + transcript.TruncateLines(run.Value, resultPreviewLines), Color: t.Foreground})
				}
				if run.Error != "" {
					lead := ""
					if run.Output != "" || run.Value != "" {
						lead = "\n"
					}
					spans = append(spans, ui.Span{Text: lead + run.Error, Color: t.DestructiveForeground})
				}
				ui.Scroll(c).MaxHeight(k.Px(384)).Children(func() {
					ui.RichText(c, spans...).Font(k.Mono).FontSize(k.Px(11.5)).FixedLineHeight(k.Px(20)).TextColor(t.MutedForeground).
						Selectable().Padding(0, k.Px(10), k.Px(8), k.Px(10))
				})
				for i, img := range run.Images {
					ui.Box(c).Padding(0, k.Px(10), k.Px(10), k.Px(10)).Children(func() {
						v.picture(c, call.Use.ID+":run:"+strconv.Itoa(i), img, 320, "Picture from Script")
					})
				}
			})
		}
		if run.Stats != "" || run.LogPath != "" {
			sep()
			ui.Row(c).MinHeight(k.Px(28)).Gap(k.Px(8)).Padding(k.Px(4), k.Px(10)).Children(func() {
				if run.Stats != "" {
					k.Text(c, run.Stats, 11, 16.5).TextColor(t.MutedForeground).FontFeatures("tnum").Shrink(0)
				}
				if run.LogPath != "" {
					k.Text(c, run.LogPath, 10.5, 15.75).Font(k.Mono).TextColor(t.MutedForeground.Alpha(0.7)).Tooltip(run.LogPath).
						SingleLine().Grow(1).Shrink(1).MinWidth(0)
					copied := v.flag("logcopied:"+call.Use.ID, false)
					label, icon := "Copy log path", "copy"
					if *copied {
						label, icon = "Copied log path", "check"
					}
					b := ui.ButtonBase(c).Label(label).Size(k.Px(20), k.Px(20)).Radius(k.Px(4)).Cursor(ui.CursorPointer)
					if b.Hovered() {
						b.Background(t.Muted)
					}
					b.Children(func() { k.Icon(c, icon, 12, t.MutedForeground) })
					if b.Clicked() {
						c.WriteClipboard(run.LogPath)
						*copied = true
					}
				}
			})
		}
	})
}

// sourceView is a program with its line numbers, the lines in marked
// tinted, as a permission request points at them.
func sourceView(c *ui.Context, k *kit.Kit, script string, marked map[int]bool) {
	t := k.T
	lines := strings.Split(strings.TrimSuffix(script, "\n"), "\n")
	width := float32(len(strconv.Itoa(len(lines))))*k.Px(11.5)*0.6 + k.Px(18)
	ui.ScrollBoth(c).MaxHeight(k.Px(320)).Label("Script source").Children(func() {
		ui.Column(c).Padding(0, 0, k.Px(6), 0).MinWidthPercent(100).Children(func() {
			for i, line := range lines {
				n := i + 1
				row := ui.Row(c).AlignItems(ui.Start)
				numColor := t.MutedForeground.Alpha(0.6)
				if marked[n] {
					row.Background(t.Attention.Alpha(0.1))
					numColor = t.Attention
				}
				row.Children(func() {
					k.Text(c, strconv.Itoa(n), 11.5, 20).Font(k.Mono).TextColor(numColor).FontFeatures("tnum").TextAlign(ui.End).
						Width(width).Padding(0, k.Px(8), 0, k.Px(10)).Shrink(0)
					k.Text(c, line, 11.5, 20).Font(k.Mono).TextColor(t.Foreground).NoWrap().Padding(0, k.Px(12), 0, 0)
				})
			}
		})
	})
}

var taskStateLabels = map[subagents.TaskState]string{
	"pending":          "Starting",
	"running":          "Running",
	subagents.Launched: "Started in background",
	"completed":        "Completed",
	"failed":           "Failed",
	"cancelled":        "Cancelled",
}

// subagentCard is a SubagentCard: the subagent's name and task, how the
// run went, a button to its Session, and the prompt and report folded.
func (v *sessionView) subagentCard(c *ui.Context, call *transcript.ToolCall, links *subagents.Links) {
	a := v.a
	k, t := a.kit, a.kit.T
	link := subagents.LinkFor(call, links)
	name := subagents.SubagentName(link.Request.SubagentType)
	desc := link.Request.Description
	if desc == "" {
		desc = "Task"
	}
	var facts []string
	if link.Run != nil && link.Run.ToolUseCount != nil {
		facts = append(facts, plural(*link.Run.ToolUseCount, "tool", "tools"))
	}
	if link.Run != nil && link.Run.DurationMs != nil {
		facts = append(facts, subagents.FormatRunDuration(*link.Run.DurationMs))
	}
	open := v.flag("subagent:"+call.Use.ID, false)
	ui.Column(c).Role(ui.RoleGroup).Label(name+": "+desc).Radius(k.Px(12)).Border(1, t.Border).Background(t.Card.Alpha(0.6)).Clip().Children(func() {
		ui.Row(c).Gap(k.Px(10)).Padding(k.Px(8), k.Px(8), k.Px(8), k.Px(10)).Children(func() {
			ui.Box(c).Size(k.Px(32), k.Px(32)).Radius(k.Px(8)).Background(t.Muted).Center().Shrink(0).Children(func() {
				k.Icon(c, "bot", 16, t.MutedForeground)
			})
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Row(c).Gap(k.Px(6)).AlignItems(ui.End).Children(func() {
					k.Text(c, name, 13, 19.5).FontWeight(500).TextColor(t.Foreground).Shrink(0)
					k.Text(c, link.Request.Description, 13, 19.5).TextColor(t.MutedForeground).SingleLine().Shrink(1)
				})
				ui.Row(c).Gap(k.Px(6)).Children(func() {
					v.stateMark(c, link.State)
					for _, f := range facts {
						k.Text(c, "·", 11.5, 17.25).TextColor(t.MutedForeground)
						k.Text(c, f, 11.5, 17.25).TextColor(t.MutedForeground).FontFeatures("tnum")
					}
				})
			})
			if link.SessionID != "" {
				b := ui.ButtonBase(c).Label("Open subagent session").Height(k.Px(28)).Gap(k.Px(4)).PaddingX(k.Px(8)).Radius(k.Px(8)).Shrink(0).Cursor(ui.CursorPointer)
				color := t.MutedForeground
				if b.Hovered() {
					b.Background(t.Muted)
					color = t.Foreground
				}
				b.Children(func() {
					k.Text(c, "Open", 12, 16).FontWeight(500).TextColor(color)
					k.Icon(c, "arrow-up-right", 14, color)
				})
				if b.Clicked() {
					a.Go(Route{Name: "session", SessionID: link.SessionID})
				}
			}
		})
		p := ui.CollapsibleBase(c, open)
		v.holdOnToggle(p.Trigger)
		tr := p.Trigger.FillWidth().Height(k.Px(28)).Gap(k.Px(4)).PaddingX(k.Px(12)).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Cursor(ui.CursorPointer)
		color := t.MutedForeground
		if tr.Hovered() {
			tr.Background(t.Muted.Alpha(0.6))
			color = t.Foreground
		}
		tr.Children(func() {
			k.Icon(c, "chevron-right", 12, color).Rotate(90 * p.Progress())
			k.Text(c, "Details", 11.5, 17.25).TextColor(color)
		})
		p.Panel(func() {
			ui.Scroll(c).MaxHeight(k.Px(384)).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
				ui.Column(c).Padding(k.Px(10), k.Px(12)).Gap(k.Px(8)).Children(func() {
					heading := func(s string) {
						k.Text(c, strings.ToUpper(s), 11, 16.5).FontWeight(500).LetterSpacing(k.Px(0.275)).TextColor(t.MutedForeground).Margin(0, 0, k.Px(4), 0)
					}
					ui.Column(c).Role(ui.RoleGroup).Label("Prompt").Children(func() {
						heading("Prompt")
						k.Text(c, link.Request.Prompt, 12.5, 20).TextColor(t.Foreground).Selectable()
					})
					if link.Report != "" {
						ui.Column(c).Role(ui.RoleGroup).Label("Report").Children(func() {
							heading("Report")
							pr := newProse(k)
							pr.size, pr.lh = 13, 24
							f := flow{c: c}
							pr.nodes(c, &f, v.doc(link.Report))
						})
					}
				})
			})
		})
	})
}

func (v *sessionView) stateMark(c *ui.Context, state subagents.TaskState) {
	k, t := v.a.kit, v.a.kit.T
	running := state == "running" || state == "pending"
	color, icon := t.MutedForeground, "circle-dashed"
	switch {
	case running:
		color = t.Info
	case state == "completed":
		color, icon = t.Success, "check"
	case state == "failed":
		color, icon = t.DestructiveForeground, "circle-x"
	case state == "cancelled":
		icon = "circle-slash"
	}
	row := ui.Row(c).Gap(k.Px(4))
	if running {
		row.Role(ui.RoleStatus)
	}
	row.Children(func() {
		if running {
			k.Spinner(c, 12, color)
		} else {
			k.Icon(c, icon, 12, color)
		}
		k.Text(c, taskStateLabels[state], 11.5, 17.25).TextColor(color)
	})
}

// subagentRuns are the runs of the listed subagents this Client knows of,
// by their Session.
func (a *App) subagentRuns(listed []sessions.Summary) map[string]subagents.Run {
	runs := map[string]subagents.Run{}
	if a.ctl == nil {
		return runs
	}
	store := a.ctl.Store()
	for _, s := range listed {
		if s.CallingSessionID == "" {
			continue
		}
		working := ""
		if h := store.Session(s.SessionID); h != nil {
			working = string(h.WorkingState())
		}
		if r, ok := subagents.RunFrom(nil, working); ok {
			runs[s.SessionID] = r
		}
	}
	return runs
}
