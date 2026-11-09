package sessionview

import (
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/pasted"
	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

// The transcript's list builds only the rows in view, so an entry is cut
// into rows of its own: a turn of dozens of tool calls would otherwise be
// one row, built and laid out whole on every frame it shows. Each row
// keeps the room the entry's flow would put above it, so the cut does not
// show.

type rowKind uint8

const (
	rowUser rowKind = iota
	rowBlock
	rowCluster
	rowCall
	rowNested
	// rowGroup is the header of a run of calls done to several agents.
	rowGroup
	rowTurnEnd
)

type transcriptRow struct {
	kind  rowKind
	key   string
	e     *transcript.Entry
	block int
	call  *transcript.ToolCall
	// top and bottom are the room above and below the row, in px.
	top, bottom float32
	// first: the first call of its cluster, or the first nested call of
	// its Script.
	first bool
	// grouped: the call is under a group's header; lifted: the row stands at
	// the top, not in a cluster.
	grouped, lifted bool
	// label is a group header's text.
	label string
	// streaming: the entry streams and this is its last text.
	streaming bool
	end       transcript.TurnEnd
	// turn: a turn end's entries, from the user's message on, for Copy.
	turn []*transcript.Entry
}

// rowCache keeps the transcript's rows from frame to frame. Cutting every
// entry again on each frame made a frame of a long Session cost what its
// whole history holds: 17 MB of garbage a frame at 27,000 messages. An
// entry's rows depend only on it, the next entry (whether its turn ends
// there), the turn before it, whether the Session runs and which entry
// streams, so a change cuts again only from the entry before the first one
// that changed.
type rowCache struct {
	entries     []*transcript.Entry
	streamingID string
	// streamingAt is the index of the entry that streamed, -1 for none.
	streamingAt int
	running     bool
	// gen is View.rowsGen the rows were cut at: a fold opened or closed.
	gen  uint64
	rows []transcriptRow
	// starts[i] is the first row of entries[i] and turns[i] the turn's
	// state before it, to go on cutting from there.
	starts []int
	turns  []turnState
}

// turnState is where the entries cut so far left the turn.
type turnState struct {
	// first is the index of the turn's user entry, for a turn end's Copy.
	first int
	// startedAt is when the turn's first user entry since the last turn
	// end came, as TurnEnds reads it.
	startedAt float64
	hasStart  bool
}

// cachedRows are the transcript's rows for entries, cut again only from
// where they changed since the last call.
func (v *View) cachedRows(entries []*transcript.Entry, streamingID string, running bool) []transcriptRow {
	return v.cutRows(&v.rowCache, entries, streamingID, running)
}

// cutRows brings rc's rows up to date with entries and returns them, in px
// before Kit.Px. A zero rowCache cuts every entry.
func (v *View) cutRows(rc *rowCache, entries []*transcript.Entry, streamingID string, running bool) []transcriptRow {
	from := 0
	if rc.gen == v.rowsGen && len(rc.starts) == len(rc.entries) {
		n := min(len(rc.entries), len(entries))
		from = n
		for i := range n {
			if rc.entries[i] != entries[i] {
				from = i
				break
			}
		}
		if from < len(rc.entries) || len(entries) != len(rc.entries) || streamingID != rc.streamingID || running != rc.running {
			// The entry before a change may end its turn there or no longer,
			// and the last one ends it while nothing runs.
			from = max(0, min(from, len(rc.entries))-1)
		}
		if streamingID != rc.streamingID && rc.streamingAt >= 0 {
			from = min(from, rc.streamingAt)
		}
		if from == len(entries) && from == len(rc.entries) {
			return rc.rows
		}
	}
	var st turnState
	if from < len(rc.starts) {
		st = rc.turns[from]
		rc.rows = rc.rows[:rc.starts[from]]
		rc.starts, rc.turns = rc.starts[:from], rc.turns[:from]
	} else {
		from = 0
		rc.rows, rc.starts, rc.turns = rc.rows[:0], rc.starts[:0], rc.turns[:0]
		rc.streamingAt = -1
	}
	if rc.streamingAt >= from {
		rc.streamingAt = -1
	}
	rows := rc.rows
	for i := from; i < len(entries); i++ {
		e := entries[i]
		if e.ID == streamingID {
			rc.streamingAt = i
		}
		rc.starts = append(rc.starts, len(rows))
		rc.turns = append(rc.turns, st)
		if e.User {
			if !st.hasStart {
				st.startedAt, st.hasStart = e.CreatedAt, true
			}
			st.first = i
			rows = append(rows, transcriptRow{kind: rowUser, key: e.ID, e: e})
			continue
		}
		replied := len(e.Blocks) > 0 && e.Blocks[len(e.Blocks)-1].Kind == transcript.Text
		closes := !running
		if i+1 < len(entries) {
			closes = entries[i+1].User && replied
		}
		var end transcript.TurnEnd
		if closes {
			end = transcript.TurnEnd{EndedAt: e.CreatedAt, StartedAt: st.startedAt, HasStart: st.hasStart}
			st.hasStart = false
		}
		rows = v.cutEntry(rows, entries, i, st.first, streamingID, end, closes)
	}
	rc.rows, rc.entries, rc.streamingID, rc.running, rc.gen = rows, entries, streamingID, running, v.rowsGen
	return rows
}

// cutEntry appends the rows of the assistant's entries[i] to rows; turnStart
// is the index of its turn's user entry, end its turn's end if it closes one.
func (v *View) cutEntry(rows []transcriptRow, entries []*transcript.Entry, i, turnStart int, streamingID string, end transcript.TurnEnd, closes bool) []transcriptRow {
	e := entries[i]
	start := len(rows)
	pending, started := float32(0), false
	add := func(r transcriptRow, top, bottom float32) {
		if started {
			r.top = max(pending, top)
		} else {
			r.top = top
		}
		r.e = e
		rows = append(rows, r)
		started, pending = true, bottom
	}
	for i, b := range e.Blocks {
		key := e.ID + "/" + strconv.Itoa(i)
		switch b.Kind {
		case transcript.Text:
			last := e.ID == streamingID && i == len(e.Blocks)-1
			add(transcriptRow{kind: rowBlock, key: key, block: i, streaming: last}, 8, 8)
		case transcript.Tools:
			if v.lifted(b) {
				// Only the Workstream's own calls: their rows stand at the
				// top, without a "Used N tools" over them.
				first := true
				for _, run := range transcript.WorkstreamRuns(transcript.LeafCalls(b.Calls)) {
					label, grouped := transcript.WorkstreamGroup(run)
					if grouped {
						r := transcriptRow{kind: rowGroup, lifted: true, key: key + "/" + run[0].Use.ID + "/group", block: i, call: run[0], label: label, first: first}
						if first {
							add(r, 8, 8)
						} else {
							r.e = e
							rows = append(rows, r)
						}
						first = false
					}
					for _, call := range run {
						r := transcriptRow{kind: rowCall, lifted: true, key: key + "/" + call.Use.ID, block: i, call: call, first: first, grouped: grouped}
						if first {
							add(r, 8, 8)
						} else {
							r.e = e
							rows = append(rows, r)
						}
						first = false
					}
				}
				continue
			}
			add(transcriptRow{kind: rowCluster, key: key, block: i}, 8, 8)
			if !*v.flag("cluster:"+b.ID, true) {
				continue
			}
			j := 0
			for _, call := range b.Calls {
				rows = append(rows, transcriptRow{kind: rowCall, key: key + "/" + call.Use.ID, e: e, block: i, call: call, first: j == 0})
				j++
				for k, inner := range call.Nested {
					rows = append(rows, transcriptRow{kind: rowNested, key: key + "/" + call.Use.ID + "/" + inner.Use.ID, e: e, block: i, call: inner, first: k == 0})
				}
			}
		default:
			add(transcriptRow{kind: rowBlock, key: key, block: i}, 8, 8)
		}
	}
	if closes && end.EndedAt > 0 {
		add(transcriptRow{kind: rowTurnEnd, key: e.ID + "/end", end: end, turn: entries[turnStart : i+1]}, 8, 0)
	}
	if len(rows) == start {
		rows = append(rows, transcriptRow{kind: rowBlock, key: e.ID, e: e, block: -1})
		pending = 0
	}
	rows[start].top += 12
	rows[len(rows)-1].bottom = pending + 12
	return rows
}

// row builds one of the transcript's rows.
func (v *View) row(c *ui.Context, r transcriptRow) {
	k, t := v.look()
	if r.kind == rowUser {
		v.userEntry(c, r.e)
		return
	}
	color := t.Foreground
	if r.e.IsError {
		color = t.DestructiveForeground
	}
	col := v.column(c).Padding(k.Px(r.top), k.Px(24), k.Px(r.bottom), k.Px(24)).TextColor(color)
	if r.kind != rowCall && r.kind != rowNested && r.kind != rowGroup {
		col.Role(ui.RoleGroup).Label(l10n.L("Assistant"))
	}
	if r.kind == rowBlock && r.block >= 0 && r.e.Blocks[r.block].Kind == transcript.Text {
		text := r.e.Blocks[r.block].Text
		col.Selectable().ContextMenu(func(m *ui.Menu) {
			m.EditItems()
			m.Separator()
			if m.Item(l10n.L("Copy as Markdown")).Chosen() {
				c.WriteClipboard(text)
				c.Toast(l10n.L("Copied as Markdown"))
			}
			if m.Item(l10n.L("Quote in message")).Chosen() {
				v.quote(text)
			}
		})
	}
	col.Children(func() {
		if r.block < 0 {
			return
		}
		switch r.kind {
		case rowTurnEnd:
			ui.Row(c).Gap(k.Px(4)).AlignItems(ui.Center).Children(func() {
				if text := transcript.ReplyText(r.turn); text != "" {
					// The negative margins keep the row as tall as its text and
					// the icon in line with the reply's left edge.
					b := ui.ButtonBase(c).Label(l10n.L("Copy reply")).Tooltip(l10n.L("Copy reply")).Size(k.Px(24), k.Px(24)).
						Margin(-k.Px(4), 0, -k.Px(4), -k.Px(5)).Radius(k.Px(6)).Center().Cursor(ui.CursorPointer)
					color := t.MutedForeground
					if b.Hovered() {
						b.Background(t.Muted)
						color = t.Foreground
					}
					b.Children(func() { k.Icon(c, "copy", 14, color) })
					if b.Clicked() {
						c.WriteClipboard(text)
						c.Toast(l10n.L("Copied the reply"))
					}
				}
				k.Text(c, transcript.FormatTurnEnd(r.end, v.env.Now()), 12, 16).TextColor(t.MutedForeground)
			})
		case rowCluster:
			v.cluster(c, r.e.Blocks[r.block])
		case rowGroup:
			gap := k.Px(4)
			if r.first {
				gap = 0
			}
			ui.Column(c).Padding(gap, 0, 0, 0).Children(func() { v.groupHeader(c, r.call, r.label) })
		case rowCall, rowNested:
			if r.lifted {
				gap := k.Px(2)
				if r.first {
					gap = 0
				}
				ui.Column(c).Padding(gap, 0, 0, 0).Children(func() {
					if !r.grouped {
						v.toolRow(c, r.call)
						return
					}
					// Under its group's header, a step in.
					ui.Column(c).Margin(0, 0, 0, k.Px(7)).BorderWidth(0, 0, 0, 1).BorderColor(t.Border).BorderStyle(ui.BorderDashed).
						Padding(0, 0, 0, k.Px(12)).Children(func() { v.toolRow(c, r.call) })
				})
				return
			}
			// The cluster's calls column: 4px under its trigger and 2px
			// between calls; a Script's calls hang right under its row.
			callTop, callGap := k.Px(4), float32(0)
			switch {
			case r.kind == rowNested:
				callTop = 0
			case !r.first:
				callTop, callGap = 0, k.Px(2)
			}
			ui.Column(c).Margin(callTop, 0, 0, k.Px(6)).BorderWidth(0, 0, 0, 1).BorderColor(t.Border).Padding(callGap, 0, 0, k.Px(12)).Children(func() {
				if r.kind == rowCall {
					if r.call.Nested != nil {
						v.scriptGroup(c, r.call)
					} else {
						v.toolRow(c, r.call)
					}
					return
				}
				innerGap := k.Px(2)
				if r.first {
					innerGap = 0
				}
				ui.Column(c).Margin(0, 0, 0, k.Px(7)).BorderWidth(0, 0, 0, 1).BorderColor(t.Border).BorderStyle(ui.BorderDashed).
					Padding(innerGap, 0, 0, k.Px(12)).Children(func() { v.toolRow(c, r.call) })
			})
		default:
			b := r.e.Blocks[r.block]
			switch b.Kind {
			case transcript.Text:
				style := mdReply
				if r.e.IsError {
					style = mdError
				}
				v.reply(c, b.Text, style, r.streaming)
				if r.streaming {
					cursor := ui.Box(c).Label(l10n.L("Assistant is typing")).Size(k.Px(8), k.Px(16)).Margin(0, 0, 0, k.Px(2))
					kit.Pulse(cursor, k.Px(2), func(w float32) ui.Color { return t.Foreground.Alpha(0.6 * (0.5 + 0.5*w)) })
				}
			case transcript.Picture:
				v.picture(c, b.ID, b.Image, 288, l10n.L("Image from Droid"))
			case transcript.Thinking:
				v.thinking(c, b, r.e.ID == v.streamingID)
			case transcript.Subagent:
				v.subagentCard(c, b.Call, v.links)
			case transcript.Spec:
				v.specCard(c, b.Call)
			}
		}
	})
}

// userEntry is the user's bubble on the right.
func (v *View) userEntry(c *ui.Context, e *transcript.Entry) {
	k, t := v.look()
	if len(e.Blocks) == 1 && e.Blocks[0].Kind == transcript.QueuedAnswers {
		v.queuedRecord(c, e.Blocks[0].Records)
		return
	}
	var parts []string
	var images []transcript.Block
	for _, b := range e.Blocks {
		switch b.Kind {
		case transcript.Text:
			parts = append(parts, b.Text)
		case transcript.Picture:
			images = append(images, b)
		}
	}
	if h, ok := transcript.ReadSpecHandoff(strings.Join(parts, "\n")); ok && len(images) == 0 {
		v.handoffCard(c, e.ID, h)
		return
	}
	if m, ok := transcript.ReadAgentMessage(strings.Join(parts, "\n")); ok && len(images) == 0 {
		v.agentMessageCard(c, e.ID, m)
		return
	}
	text, texts := pasted.Split(strings.Join(parts, "\n"))
	v.column(c).Role(ui.RoleGroup).Label(l10n.L("You")).AlignItems(ui.End).Gap(k.Px(6)).PaddingY(k.Px(12)).Children(func() {
		if len(images) > 0 {
			ui.Row(c).Wrap().Justify(ui.End).Gap(k.Px(6)).MaxWidthPercent(85).Children(func() {
				for _, b := range images {
					v.picture(c, b.ID, b.Image, 192, l10n.L("Attached image"))
				}
			})
		}
		for i, p := range texts {
			open := v.flag("paste:"+e.ID+":"+strconv.Itoa(i), false)
			action := l10n.L("Show text")
			if *open {
				action = l10n.L("Hide text")
			}
			ui.Column(c.Key("paste" + strconv.Itoa(i))).AlignItems(ui.End).Gap(k.Px(6)).MaxWidthPercent(85).Children(func() {
				if acted, _ := PastedCard(c, k, p, action, open, ""); acted {
					*open = !*open
				}
				if *open {
					k.Text(c, p, 12.5, 20).Font(k.Mono).Selectable().Padding(k.Px(8), k.Px(12)).Radius(k.Px(8)).
						Border(1, t.Border).Background(t.Muted).TextColor(t.Foreground)
				}
			})
		}
		if text != "" {
			// The reference's user bubble: rounded-2xl, the faint secondary
			// fill, at the transcript's reading size.
			k.Text(c, text, k.Dense(13, 15), k.Dense(21, 24)).Selectable().MaxWidthPercent(85).Padding(k.Px(10), k.Px(14)).Radius(k.Px(12)).
				Background(t.Secondary).TextColor(t.SecondaryForeground)
		}
	})
}

// queuedRecord is the user's answers to queued question sets, or their
// dismissal, as the agent got them: a compact card in the bubble's place,
// each question over its answer.
func (v *View) queuedRecord(c *ui.Context, records []transcript.QueuedRecord) {
	k, t := v.look()
	title := transcript.QueuedTitle(records)
	answered := slices.ContainsFunc(records, func(r transcript.QueuedRecord) bool { return !r.Dismissed })
	v.column(c).Role(ui.RoleGroup).Label(l10n.L("You")).AlignItems(ui.End).PaddingY(k.Px(12)).Children(func() {
		ui.Column(c).Role(ui.RoleGroup).Label(title).Gap(k.Px(6)).MaxWidthPercent(85).Padding(k.Px(8), k.Px(14), k.Px(10), k.Px(14)).
			Radius(k.Px(12)).Background(t.Secondary).Children(func() {
			ui.Row(c).Gap(k.Px(6)).Children(func() {
				k.Icon(c, "message-circle-question-mark", 12, t.MutedForeground)
				k.Text(c, title, 11, 16.5).FontWeight(600).TextColor(t.MutedForeground)
			})
			for _, r := range records {
				for _, a := range r.Answers {
					ui.Column(c).Gap(k.Px(2)).Children(func() {
						k.Text(c, a.Question, k.Dense(12, 13), k.Dense(18, 19.5)).TextColor(t.MutedForeground).Selectable()
						// A set dismissed among answered ones says so; alone, the title does.
						if r.Dismissed && !answered {
							return
						}
						answer, color := a.Answer, t.SecondaryForeground
						if r.Dismissed {
							answer, color = l10n.L("dismissed"), t.MutedForeground
						}
						ui.Row(c).Gap(k.Px(6)).AlignItems(ui.Start).Children(func() {
							k.Text(c, "↳", k.Dense(12, 13), k.Dense(18, 19.5)).TextColor(t.MutedForeground).Shrink(0)
							k.Text(c, answer, k.Dense(12, 13), k.Dense(18, 19.5)).FontWeight(500).TextColor(color).Selectable().Grow(1).MinWidth(0)
						})
					})
				}
			}
		})
	})
}

// rowOf is the list row where entry starts: row 0 is the lead.
func rowOf(rows []transcriptRow, e *transcript.Entry) int {
	for i, r := range rows {
		if r.e == e {
			return i + 1
		}
	}
	return -1
}
