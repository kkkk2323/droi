package sessionview

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/plugins/loading"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/kkkk2323/droid-sdk-go/controller"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
	"github.com/kkkk2323/droi/apps/native/pkg/subagents"
	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

const resultPreviewLines = 40

// transcriptView is the MessageList: the entries in a list that follows
// its end, the lead above them (previous messages), the activity row
// below them, and the button back to the latest.
func (v *View) transcriptView(c *ui.Context, s *session.Session, listed []sessions.Summary) {
	k, t := v.look()
	ws := string(s.WorkingState())
	if v.compacting {
		ws = string(protocol.DroidWorkingStateCompactingConversation)
	}
	running := ws != string(protocol.DroidWorkingStateIdle)
	entries := v.entries
	hasOlder := s.HasOlderMessages()
	if len(entries) == 0 && !hasOlder {
		ui.Box(c).Fill().Center().Children(func() {
			k.Text(c, l10n.L("What should Droid work on?"), 14, 20).TextColor(t.MutedForeground)
		})
		return
	}
	var streamingID string
	if ws == string(protocol.DroidWorkingStateStreamingAssistantMessage) && len(entries) > 0 && !entries[len(entries)-1].User {
		streamingID = entries[len(entries)-1].ID
	}
	ends := transcript.TurnEnds(entries, running)
	activity := transcript.WorkingLabel(ws)
	v.links = v.subagentLinks(listed)
	v.streamingID = streamingID
	rows := v.rows(entries, streamingID, ends)
	v.frameRows = rows

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
				v.activityRow(c, ws, activity)
			default:
				v.row(c, rows[i-1])
			}
		}).Fill().Label(l10n.L("Transcript")).PaddingX(scrollGutter)
		if v.pendingJump != "" {
			for i, e := range entries {
				if e.ID == v.pendingJump {
					if row := rowOf(rows, entries[i]); row >= 0 {
						v.hold()
						v.list.ScrollTo(row, ui.Start)
					}
				}
			}
			v.pendingJump = ""
		}
		if v.env.Overlay != nil {
			v.env.Overlay(c, s, list.Bounds().W)
		}
		if first, _ := v.list.Visible(); first == 0 && hasOlder {
			v.loadOlder()
		}
		if !v.list.AtEnd() {
			tip := l10n.L("Scroll to latest")
			if v.env.ScrollShortcut != "" {
				tip += " (" + v.env.ScrollShortcut + ")"
			}
			b := k.Button(c, kit.Outline, 32, true, l10n.L("Scroll to latest")).Tooltip(tip).Radius(k.Px(16)).Absolute().Bottom(k.Px(12)).
				Left(0).Right(0).Margin(0, ui.Auto, 0, ui.Auto).Shadow(0, k.Px(4), k.Px(12), 0, ui.RGBA(0, 0, 0, 0.08))
			b.Children(func() { k.Icon(c, "arrow-down", 16, t.Foreground) })
			if b.Clicked() {
				v.ScrollToLatest()
			}
		}
	})
}

// scrollGutter is the room the web Client's transcript keeps for its
// scrollbar on both sides (scrollbar-gutter: stable both-edges), which
// centers the column in the rest.
const scrollGutter = 11

// Column is the reading column: max-w-3xl centered, px-6. The transcript's
// rows, the Prompts and the composer share it.
func Column(c *ui.Context, k *kit.Kit) ui.Element {
	return ui.Column(c).FillWidth().MaxWidth(k.Px(ColumnWidth)).AlignSelf(ui.Center).PaddingX(k.Px(24))
}

func (v *View) column(c *ui.Context) ui.Element { return Column(c, v.kit()) }

// lead is the ListHeader: 12px of room, then the button for previous
// messages while there are some.
func (v *View) lead(c *ui.Context, s *session.Session) {
	k, t := v.look()
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
				k.Text(c, l10n.L("Previous messages did not load: %s", err), 12, 16).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
				return
			}
			label := l10n.L("Load previous messages")
			if loading {
				label = l10n.L("Loading previous messages…")
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

// activityRow is the live turn's closing row: an indicator of the working
// state and what the Droid is doing, empty while idle.
func (v *View) activityRow(c *ui.Context, state, label string) {
	k, t := v.look()
	v.column(c).Row().Role(ui.RoleStatus).Label(l10n.L("Session activity")).Gap(k.Px(8)).Padding(0, k.Px(24), k.Px(16), k.Px(24)).Children(func() {
		if label == "" {
			return
		}
		activityIndicator(c, state, k.Px(16), t.MutedForeground)
		k.Text(c, label, 13, 19.5).TextColor(t.MutedForeground)
	})
}

// activityIndicator draws each working state its own way, so that a glance
// tells thinking from answering, running a tool or waiting for the user.
func activityIndicator(c *ui.Context, state string, size float32, color ui.Color) ui.Element {
	o := loading.Options{Size: size, Color: color}
	switch protocol.DroidWorkingState(state) {
	case protocol.DroidWorkingStateThinking:
		return loading.Wave(c, o)
	case protocol.DroidWorkingStateStreamingAssistantMessage:
		return loading.LinearDots(c, o)
	case protocol.DroidWorkingStateExecutingTool:
		// The tool rows above turn kit's spinner; this one turns with them.
		return kit.Arc(c, size, color)
	case protocol.DroidWorkingStateWaitingForToolConfirmation:
		return loading.Pulse(c, o)
	case protocol.DroidWorkingStateCompactingConversation:
		return loading.Cascade(c, o)
	}
	return loading.BouncingDots(c, o)
}

func (v *View) picture(c *ui.Context, id string, img transcript.Image, maxH float32, label string) {
	k := v.kit()
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
	pic := ui.Image(c, bm).Label(label).Role(ui.RoleButton).Focusable().Tooltip(l10n.L("Click to enlarge")).
		Width(float32(w)*scale).MaxWidthPercent(100).AspectRatio(float32(w)/float32(h)).
		Shrink(1).MinWidth(0).Radius(k.Px(12)).Border(1, k.T.Border).Cursor(ui.CursorPointer)
	if pic.Clicked() {
		v.zoom, v.zoomLabel, v.zoomOpen = bm, label, true
	}
}

// zoomView shows the picture clicked in the transcript at the size the
// window holds; a click or Escape closes it.
func (v *View) zoomView(c *ui.Context) {
	if v.zoom == nil {
		v.zoomOpen = false
		return
	}
	k := v.kit()
	bm := v.zoom
	ww, wh := c.Size()
	w, h := bm.Size()
	maxW, maxH := ww-k.Px(64), wh-k.Px(64)
	scale := min(float32(1), maxW/float32(w), maxH/float32(h))
	ui.DialogBase(c, &v.zoomOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.7))
		panel.Label(l10n.L("Enlarged image")).Size(float32(w)*scale, float32(h)*scale).Radius(k.Px(8)).Clip().Cursor(ui.CursorPointer)
		ui.Image(c, bm).Label(v.zoomLabel).Fill().Fit(ui.Contain)
		if panel.Clicked() {
			v.zoomOpen = false
		}
	})
	if !v.zoomOpen {
		v.zoom = nil
	}
}

// disclosure is the trigger of a fold: label and a chevron that turns as
// it opens; quiet until hovered.
func (v *View) disclosure(c *ui.Context, open *bool, label string, size, lh float32, weight int, leading func(color ui.Color)) ui.CollapsibleParts {
	k, t := v.look()
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
func (v *View) thinking(c *ui.Context, b transcript.Block, streaming bool) {
	k, t := v.look()
	label := l10n.L("Reasoning")
	switch {
	case b.DurationMs != nil && *b.DurationMs > 0:
		label = l10n.L("Reasoned for %s", transcript.FormatDuration(*b.DurationMs))
	case streaming:
		label = l10n.L("Reasoning…")
	}
	open := v.flag("thinking:"+b.ID, false)
	ui.Column(c).Children(func() {
		p := v.disclosure(c, open, label, 13, 19.5, 0, nil)
		p.Trigger.Padding(k.Px(4), k.Px(6))
		p.Panel(func() {
			ui.Column(c).Margin(k.Px(8), 0, 0, 0).BorderWidth(0, 0, 0, 2).BorderColor(t.Border).Padding(0, 0, 0, k.Px(16)).Selectable().Children(func() {
				markdownText(c, b.Text, v.md.Options(k, mdThinking), streaming)
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
func (v *View) cluster(c *ui.Context, b transcript.Block) {
	k, t := v.look()
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
func (v *View) rowTrigger(c *ui.Context, open *bool, label string) ui.CollapsibleParts {
	k, t := v.look()
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
// counts, and whether it ran; it opens to the input and the result. A
// TaskOutput or TaskStop call shows the subagent it is about, with a button
// to its Session.
func (v *View) toolRow(c *ui.Context, call *transcript.ToolCall) {
	k, t := v.look()
	server, name := transcript.ToolName(call.Use.Name)
	custom, hasCustom := toolViewOf(call.Use.Name)
	icon := toolIcons[call.Use.Name]
	if hasCustom && custom.Icon != "" {
		icon = custom.Icon
	}
	if icon == "" {
		icon = "wrench"
		if server != "" {
			icon = "plug"
		}
	}
	res := v.result(call)
	open := v.flag("tool:"+call.Use.ID, hasCustom && custom.Open)
	target, targetTitle := v.taskTarget(call)
	parts := transcript.SummaryParts(call)
	summary := transcript.Summary(call)
	if targetTitle != "" {
		parts, summary = []transcript.SummaryPart{{Value: targetTitle}}, targetTitle
	}
	ui.Column(c.Key(call.Use.ID)).Children(func() {
		var p ui.CollapsibleParts
		trigger := func() {
			p = v.rowTrigger(c, open, call.Use.Name+": "+summary)
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
					k.Spinner(c, 14, t.MutedForeground).Role(ui.RoleStatus).Label(l10n.L("Running"))
					return
				}
				if j, ok := v.job(call.Use.ID); ok && (j.Done || !j.Script) {
					v.jobMark(c, j)
				} else if res.IsError {
					k.Icon(c, "circle-x", 14, t.DestructiveForeground).Label(l10n.L("Failed"))
				} else {
					k.Icon(c, "check", 14, t.Success).Label(l10n.L("Succeeded"))
				}
				ch := k.Icon(c, "chevron-right", 14, t.MutedForeground).Rotate(90 * p.Progress())
				if !hover && !*open && p.Progress() == 0 {
					ch.Opacity(0)
				}
			})
		}
		if target == "" {
			trigger()
		} else {
			ui.Row(c).Gap(k.Px(4)).Children(func() {
				trigger()
				p.Trigger.Shrink(1).MinWidth(0)
				label := l10n.L("Open subagent session")
				if targetTitle != "" {
					label = l10n.L("Open %s", targetTitle)
				}
				if k.IconButton(c, "arrow-up-right", label, 28).Clicked() {
					v.env.OpenSession(target)
				}
			})
		}
		p.Panel(func() {
			if hasCustom && custom.Detail != nil {
				v.customToolDetail(c, custom, call, res)
				return
			}
			v.toolDetail(c, call, res)
		})
	})
}

// customToolDetail is a row's panel drawn by a registered ToolView, in the
// card toolDetail draws.
func (v *View) customToolDetail(c *ui.Context, tv ToolView, call *transcript.ToolCall, res transcript.ToolResultView) {
	k, t := v.look()
	ui.Column(c).Margin(k.Px(2), 0, k.Px(4), 0).Padding(k.Px(10)).Radius(k.Px(10)).Border(1, t.Border).Background(t.Card.Alpha(0.6)).Clip().Children(func() {
		tv.Detail(c, ToolContext{Kit: k, Call: call, Result: res})
	})
}

// toolDetail is a row's panel: the input, then the diff, the status or
// the result's first lines, and its pictures.
func (v *View) toolDetail(c *ui.Context, call *transcript.ToolCall, res transcript.ToolResultView) {
	k, t := v.look()
	mono := func(text string, color ui.Color) ui.Element {
		return k.Text(c, text, 11.5, 20).Font(k.Mono).TextColor(color).Selectable()
	}
	ui.Column(c).Margin(k.Px(2), 0, k.Px(4), 0).Radius(k.Px(10)).Border(1, t.Border).Background(t.Card.Alpha(0.6)).Clip().Children(func() {
		input := transcript.InputText(call)
		if res.Diff != nil {
			ui.Box(c).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Padding(k.Px(6), k.Px(10)).Children(func() { mono(input, t.Foreground) })
			v.diffView(c, res.Diff)
		} else {
			job, isJob := v.job(call.Use.ID)
			output := isJob && v.ShowsOutput(job)
			ui.Scroll(c).MaxHeight(k.Px(384)).Children(func() {
				ui.Column(c).Padding(k.Px(10)).Children(func() {
					mono(input, t.Foreground)
					if isJob {
						mono(v.JobState(job), t.Foreground)
					}
					switch {
					case output:
					case res.Status != nil:
						color, icon, msg := t.Success, "check", l10n.L("Succeeded")
						if !res.Status.Success {
							color, icon, msg = t.DestructiveForeground, "circle-x", l10n.L("Failed")
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
			if output {
				ui.Box(c).Height(1).Background(t.Border)
				v.JobOutput(c, job)
			}
		}
		for i, img := range res.Images {
			ui.Box(c).Padding(0, k.Px(10), k.Px(10), k.Px(10)).Children(func() {
				v.picture(c, call.Use.ID+":"+strconv.Itoa(i), img, 320, l10n.L("Picture from %s", call.Use.Name))
			})
		}
	})
}

// diffView is the DiffView: both line numbers, the sign, the line, tinted
// green and red.
func (v *View) diffView(c *ui.Context, d *transcript.Diff) {
	k, t := v.look()
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
	ui.ScrollBoth(c).MaxHeight(k.Px(384)).Label(l10n.L("Diff")).Children(func() {
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
func (v *View) scriptGroup(c *ui.Context, call *transcript.ToolCall) {
	k, t := v.look()
	run := v.scriptRun(call)
	summary := v.scriptSummary(call)
	failed := run.Status == "failed" || run.Status == "cancelled"
	open := v.flag("script:"+call.Use.ID, false)
	ui.Column(c.Key(call.Use.ID)).Children(func() {
		p := v.rowTrigger(c, open, call.Use.Name+": "+summary)
		hover := p.Trigger.Hovered() || p.Trigger.Focused()
		p.Trigger.Children(func() {
			fg, iconColor := t.Foreground, t.MutedForeground
			if failed {
				fg, iconColor = t.DestructiveForeground, t.DestructiveForeground
			}
			k.Icon(c, "braces", 14, iconColor)
			k.Text(c, l10n.L("Script"), 12.5, 18.75).FontWeight(500).TextColor(fg).Shrink(0)
			k.Text(c, summary, 11.5, 17.25).Font(k.Mono).TextColor(t.MutedForeground).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			switch {
			case call.Result == nil:
				k.Spinner(c, 14, t.MutedForeground).Role(ui.RoleStatus).Label(l10n.L("Running"))
			case run.Status == "running":
				if j, ok := v.job(call.Use.ID); ok && j.Done {
					v.jobMark(c, j)
				} else {
					k.Icon(c, "clock", 14, t.MutedForeground).Label(l10n.L("Still running"))
				}
			case run.Status == "stalled":
				k.Icon(c, "circle-alert", 14, t.Attention).Label(l10n.L("Stalled"))
			case failed:
				label := l10n.L("Failed")
				if run.Status == "cancelled" {
					label = l10n.L("Cancelled")
				}
				k.Icon(c, "circle-x", 14, t.DestructiveForeground).Label(label)
			default:
				k.Icon(c, "check", 14, t.Success).Label(l10n.L("Succeeded"))
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

func (v *View) scriptDetail(c *ui.Context, call *transcript.ToolCall, run transcript.ScriptRun) {
	k, t := v.look()
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
			section(l10n.L("Source"), func() { sourceView(c, k, script, nil) })
			for _, in := range inputs {
				sep()
				section("inputs."+in.Name, func() { pre(transcript.TruncateLines(in.Text, resultPreviewLines), t.MutedForeground, 240) })
			}
		}
		if run.Error != "" || run.Output != "" || run.Value != "" || len(run.Images) > 0 {
			sep()
			section(l10n.L("Output"), func() {
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
						v.picture(c, call.Use.ID+":run:"+strconv.Itoa(i), img, 320, l10n.L("Picture from Script"))
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
					label, icon := l10n.L("Copy log path"), "copy"
					if *copied {
						label, icon = l10n.L("Copied log path"), "check"
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
	ui.ScrollBoth(c).MaxHeight(k.Px(320)).Label(l10n.L("Script source")).Children(func() {
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

// countL is a count with its noun, "1 tool" or "3 tools"; one and many are
// l10n.N keys holding a %d.
func countL(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf(l10n.T(one), n)
	}
	return fmt.Sprintf(l10n.T(many), n)
}

var taskStateLabels = map[subagents.TaskState]string{
	"pending":          l10n.N("Starting"),
	"running":          l10n.N("Running"),
	subagents.Launched: l10n.N("Started in background"),
	"completed":        l10n.N("Completed"),
	"failed":           l10n.N("Failed"),
	"cancelled":        l10n.N("Cancelled"),
}

// complexityLabels name the Task's complexity tiers, as Settings does.
var complexityLabels = map[string]string{
	"light":  l10n.N("Light task"),
	"medium": l10n.N("Medium task"),
	"heavy":  l10n.N("Heavy task"),
}

// subagentCard is a SubagentCard: the subagent's name and task, how the
// run went (live, its latest tool), a button to stop it and one to its
// Session, and the prompt and report folded.
func (v *View) subagentCard(c *ui.Context, call *transcript.ToolCall, links *subagents.Links) {
	k, t := v.look()
	link := subagents.LinkFor(call, links)
	name := subagents.SubagentName(link.Request.SubagentType)
	if link.Request.Resume != "" {
		name = l10n.L("Resume %s", name)
	}
	desc := link.Request.Description
	if desc == "" {
		desc = l10n.L("Task")
	}
	running := link.State == subagents.TaskState(subagents.Running) || link.State == subagents.TaskState(subagents.Pending)
	prog := v.subagentProgress(link.SessionID)
	var facts []string
	switch {
	case link.Run != nil && link.Run.ToolUseCount != nil:
		facts = append(facts, countL(*link.Run.ToolUseCount, l10n.N("%d tool"), l10n.N("%d tools")))
	case running && prog != nil && prog.count > 0:
		facts = append(facts, countL(prog.count, l10n.N("%d tool"), l10n.N("%d tools")))
	}
	if link.Run != nil && link.Run.DurationMs != nil {
		facts = append(facts, subagents.FormatRunDuration(*link.Run.DurationMs))
	}
	if tier := complexityLabels[link.Request.Complexity]; tier != "" {
		facts = append(facts, l10n.T(tier))
	}
	if prog != nil && prog.model != "" {
		facts = append(facts, prog.model)
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
						k.Text(c, f, 11.5, 17.25).TextColor(t.MutedForeground).FontFeatures("tnum").SingleLine().Shrink(1)
					}
				})
				if running && prog != nil && prog.tool != "" {
					ui.Row(c).Label(l10n.L("Latest tool")).Gap(k.Px(6)).MinWidth(0).Children(func() {
						icon := toolIcons[prog.tool]
						if icon == "" {
							icon = "wrench"
						}
						k.Icon(c, icon, 12, t.MutedForeground.Alpha(0.7))
						k.Text(c, prog.tool, 11.5, 17.25).FontWeight(500).TextColor(t.MutedForeground).Shrink(0)
						k.Text(c, prog.summary, 11.5, 17.25).Font(k.Mono).TextColor(t.MutedForeground.Alpha(0.8)).SingleLine().Shrink(1).MinWidth(0)
					})
				}
			})
			if running && link.SessionID != "" && v.env.Controller != nil && StopSubagentButton(c, k) {
				StopSubagent(v.env.Ctx, v.env.Controller, link.SessionID)
			}
			if link.SessionID != "" && v.env.OpenSession != nil {
				b := ui.ButtonBase(c).Label(l10n.L("Open subagent session")).Height(k.Px(28)).Gap(k.Px(4)).PaddingX(k.Px(8)).Radius(k.Px(8)).Shrink(0).Cursor(ui.CursorPointer)
				color := t.MutedForeground
				if b.Hovered() {
					b.Background(t.Muted)
					color = t.Foreground
				}
				b.Children(func() {
					k.Text(c, l10n.L("Open"), 12, 16).FontWeight(500).TextColor(color)
					k.Icon(c, "arrow-up-right", 14, color)
				})
				if b.Clicked() {
					v.env.OpenSession(link.SessionID)
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
			k.Text(c, l10n.L("Details"), 11.5, 17.25).TextColor(color)
		})
		p.Panel(func() {
			ui.Scroll(c).MaxHeight(k.Px(384)).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
				ui.Column(c).Padding(k.Px(10), k.Px(12)).Gap(k.Px(8)).Children(func() {
					heading := func(s string) ui.Element {
						return k.Text(c, strings.ToUpper(s), 11, 16.5).FontWeight(500).LetterSpacing(k.Px(0.275)).TextColor(t.MutedForeground).Margin(0, 0, k.Px(4), 0)
					}
					ui.Column(c).Role(ui.RoleGroup).Label(l10n.L("Prompt")).Children(func() {
						heading(l10n.L("Prompt"))
						k.Text(c, link.Request.Prompt, 12.5, 20).TextColor(t.Foreground).Selectable()
					})
					if link.Report != "" {
						ui.Column(c).Role(ui.RoleGroup).Label(l10n.L("Report")).Selectable().Children(func() {
							heading(l10n.L("Report")).Unselectable()
							markdown.View(c, link.Report, v.md.Options(k, mdReport))
						})
					}
				})
			})
		})
	})
}

// StopSubagentButton is the small square button that stops a running
// subagent, as the composer's stop is drawn; it reports a click.
func StopSubagentButton(c *ui.Context, k *kit.Kit) bool {
	t := k.T
	b := ui.ButtonBase(c).Label(l10n.L("Stop subagent")).Tooltip(l10n.L("Stop subagent")).Size(k.Px(28), k.Px(28)).Radius(k.Px(8)).
		Center().Shrink(0).Cursor(ui.CursorPointer)
	color := t.MutedForeground
	if b.Hovered() {
		b.Background(t.Muted)
		color = t.Foreground
	}
	b.Children(func() { ui.Box(c).Size(k.Px(8), k.Px(8)).Radius(k.Px(2)).Background(color) })
	return b.Clicked()
}

// StopSubagent stops a subagent's running turn, as TaskStop does: the
// Daemon then reports the run cancelled to its caller. Interrupting the
// caller leaves a background subagent running.
func StopSubagent(ctx context.Context, ctl *controller.Controller, id string) {
	go func() { _ = ctl.InterruptSession(ctx, id) }()
}

func (v *View) stateMark(c *ui.Context, state subagents.TaskState) {
	k, t := v.look()
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
		k.Text(c, l10n.T(taskStateLabels[state]), 11.5, 17.25).TextColor(color)
	})
}

// SubagentRuns are the runs of the listed subagents the Store knows of,
// by their Session.
func SubagentRuns(store *session.Store, listed []sessions.Summary) map[string]subagents.Run {
	runs := map[string]subagents.Run{}
	for _, s := range listed {
		if s.CallingSessionID == "" {
			continue
		}
		if r, ok := SubagentRun(store, s.SessionID); ok {
			runs[s.SessionID] = r
		}
	}
	return runs
}

// SubagentRun is what the Store knows of one subagent's run, listed yet or
// not: a background Task's subagent reaches the Store through its caller's
// notifications before the Session list is read again.
func SubagentRun(store *session.Store, id string) (subagents.Run, bool) {
	working := ""
	if h := store.Session(id); h != nil {
		working = string(h.WorkingState())
	}
	var summary *subagents.Run
	if sum, ok := store.SubagentInvocationSummary(id); ok {
		summary = subagents.FromSummary(sum)
	}
	return subagents.RunFrom(summary, working)
}

func (v *View) subagentRun(id string) (subagents.Run, bool) {
	if v.env.Controller == nil {
		return subagents.Run{}, false
	}
	return SubagentRun(v.env.Controller.Store(), id)
}

// linksKey is what a frame's subagent links are built from: the transcript
// and the list (which the app keeps while it does not change).
type linksKey struct {
	built int64
	list  *sessions.Summary
	n     int
}

// subagentLinks links the transcript's Task calls to their subagents,
// built again only when the transcript or the list changed: the list holds
// every subagent of every Session.
func (v *View) subagentLinks(listed []sessions.Summary) *subagents.Links {
	key := linksKey{built: v.built, n: len(listed)}
	if len(listed) > 0 {
		key.list = &listed[0]
	}
	if v.links != nil && v.linksAt == key {
		return v.links
	}
	by := subagents.ByToolUse(listed)
	v.linksAt = key
	return &subagents.Links{ByToolUse: by, RunOf: v.subagentRun, Latest: subagents.LatestCalls(v.entries, by)}
}

// subagentProgress is what a subagent's Session in the Store shows of its
// run: its tool calls so far, the latest one, and its model.
type subagentProgress struct {
	rev     int64
	count   int
	tool    string
	summary string
	model   string
}

// subagentProgress reads subagent id's Session again only when it changed;
// nil while the Store does not have it.
func (v *View) subagentProgress(id string) *subagentProgress {
	ctl := v.env.Controller
	if id == "" || ctl == nil {
		return nil
	}
	s := ctl.Store().Session(id)
	if s == nil {
		return nil
	}
	rev := v.env.Revisions.Load(id)
	if p := v.progress[id]; p != nil && p.rev == rev {
		return p
	}
	p := &subagentProgress{rev: rev}
	call, n := transcript.LatestTool(s.Messages())
	p.count = n
	if call != nil {
		_, p.tool = transcript.ToolName(call.Use.Name)
		p.summary = transcript.Summary(call)
	}
	if model := s.Settings().ModelID; model != "" {
		p.model = model
		for _, m := range s.AvailableModels() {
			if m.ID == model && m.DisplayName != "" {
				p.model = m.DisplayName
			}
		}
	}
	v.progress[id] = p
	return p
}

// taskTarget is the subagent a TaskOutput or TaskStop call is about, and
// its title, when the transcript reaches it and its Session can be opened.
func (v *View) taskTarget(call *transcript.ToolCall) (id, title string) {
	want := subagents.TaskTarget(call)
	if want == "" || v.env.OpenSession == nil || v.links == nil {
		return "", ""
	}
	for sessionID, callID := range v.links.Latest {
		// The Daemon takes the start of an id too.
		if sessionID != want && (len(want) < 8 || !strings.HasPrefix(sessionID, want)) {
			continue
		}
		title = v.links.ByToolUse[callID].Title
		if ctl := v.env.Controller; title == "" && ctl != nil {
			if s := ctl.Store().Session(sessionID); s != nil {
				title = s.Title()
			}
		}
		return sessionID, title
	}
	return "", ""
}
