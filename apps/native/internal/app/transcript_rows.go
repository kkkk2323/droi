package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/jsonrender"
	"github.com/kkkk2323/droi/apps/native/internal/md"
	"github.com/kkkk2323/droi/apps/native/internal/transcript"
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
	// streaming: the entry streams and this is its last text.
	streaming bool
	end       transcript.TurnEnd
	// turn: a turn end's entries, from the user's message on, for Copy.
	turn []*transcript.Entry
}

// rows cuts the entries into the list's rows, in px before Kit.Px.
func (v *sessionView) rows(entries []*transcript.Entry, streamingID string, ends map[string]transcript.TurnEnd) []transcriptRow {
	var rows []transcriptRow
	turnStart := 0
	for i, e := range entries {
		if e.User {
			rows = append(rows, transcriptRow{kind: rowUser, key: e.ID, e: e})
			turnStart = i
			continue
		}
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
			key := e.ID + "/" + itoa(i)
			switch b.Kind {
			case transcript.Text:
				top, bottom := v.textMargins(b.Text)
				last := e.ID == streamingID && i == len(e.Blocks)-1
				add(transcriptRow{kind: rowBlock, key: key, block: i, streaming: last}, top, bottom)
			case transcript.Tools:
				add(transcriptRow{kind: rowCluster, key: key, block: i}, 8, 8)
				if !*v.flag("cluster:"+b.ID, true) {
					continue
				}
				for j, call := range b.Calls {
					rows = append(rows, transcriptRow{kind: rowCall, key: key + "/" + call.Use.ID, e: e, block: i, call: call, first: j == 0})
					for k, inner := range call.Nested {
						rows = append(rows, transcriptRow{kind: rowNested, key: key + "/" + call.Use.ID + "/" + inner.Use.ID, e: e, block: i, call: inner, first: k == 0})
					}
				}
			default:
				add(transcriptRow{kind: rowBlock, key: key, block: i}, 8, 8)
			}
		}
		if end, closes := ends[e.ID]; closes && end.EndedAt > 0 {
			add(transcriptRow{kind: rowTurnEnd, key: e.ID + "/end", end: end, turn: entries[turnStart : i+1]}, 8, 0)
		}
		if len(rows) == start {
			rows = append(rows, transcriptRow{kind: rowBlock, key: e.ID, e: e, block: -1})
			pending = 0
		}
		rows[start].top += 12
		rows[len(rows)-1].bottom = pending + 12
	}
	return rows
}

// textMargins are the room a reply's text takes above and below it: its
// first block's top margin and its last block's bottom one.
func (v *sessionView) textMargins(text string) (top, bottom float32) {
	edge := func(n []*md.Node) (float32, float32) {
		if len(n) == 0 {
			return 0, 0
		}
		t, _ := margins(n[0])
		_, b := margins(n[len(n)-1])
		return t, b
	}
	if !jsonrender.HasRenderTag(text) {
		return edge(v.doc(text))
	}
	segs := jsonrender.SplitReply(text)
	for i, s := range segs {
		t, b := float32(8), float32(8)
		if s.Kind == jsonrender.Markdown {
			t, b = edge(v.doc(s.Text))
		}
		if i == 0 {
			top = t
		}
		if i == len(segs)-1 {
			bottom = b
		}
	}
	return top, bottom
}

// row builds one of the transcript's rows.
func (v *sessionView) row(c *ui.Context, r transcriptRow) {
	a := v.a
	k, t := a.kit, a.kit.T
	if r.kind == rowUser {
		v.userEntry(c, r.e)
		return
	}
	color := t.Foreground
	if r.e.IsError {
		color = t.DestructiveForeground
	}
	col := v.column(c).Padding(k.Px(r.top), k.Px(24), k.Px(r.bottom), k.Px(24)).TextColor(color)
	if r.kind != rowCall && r.kind != rowNested {
		col.Role(ui.RoleGroup).Label(L("Assistant"))
	}
	if r.kind == rowBlock && r.block >= 0 && r.e.Blocks[r.block].Kind == transcript.Text {
		text := r.e.Blocks[r.block].Text
		col.Selectable().ContextMenu(func(m *ui.Menu) {
			m.EditItems()
			m.Separator()
			if m.Item(L("Copy as Markdown")).Chosen() {
				c.WriteClipboard(text)
				c.Toast(L("Copied as Markdown"))
			}
			if m.Item(L("Quote in message")).Chosen() {
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
					b := ui.ButtonBase(c).Label(L("Copy reply")).Tooltip(L("Copy reply")).Size(k.Px(24), k.Px(24)).
						Margin(-k.Px(4), 0, -k.Px(4), -k.Px(5)).Radius(k.Px(6)).Center().Cursor(ui.CursorPointer)
					color := t.MutedForeground
					if b.Hovered() {
						b.Background(t.Muted)
						color = t.Foreground
					}
					b.Children(func() { k.Icon(c, "copy", 14, color) })
					if b.Clicked() {
						c.WriteClipboard(text)
						c.Toast(L("Copied the reply"))
					}
				}
				k.Text(c, transcript.FormatTurnEnd(r.end, a.cfg.Now()), 12, 16).TextColor(t.MutedForeground)
			})
		case rowCluster:
			v.cluster(c, r.e.Blocks[r.block])
		case rowCall, rowNested:
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
				f := flow{open: true}
				v.reply(c, &f, b.Text, color)
				if r.streaming {
					pulse := 0.6 * (0.5 + 0.5*float32(pulseWave(c)))
					ui.Box(c).Label(L("Assistant is typing")).Size(k.Px(8), k.Px(16)).Radius(k.Px(2)).Background(t.Foreground.Alpha(pulse)).Margin(0, 0, 0, k.Px(2))
				}
			case transcript.Picture:
				v.picture(c, b.ID, b.Image, 288, L("Image from Droid"))
			case transcript.Thinking:
				v.thinking(c, b, r.e.ID == v.streamingID)
			case transcript.Subagent:
				v.subagentCard(c, b.Call, v.links)
			}
		}
	})
}

// userEntry is the user's bubble on the right.
func (v *sessionView) userEntry(c *ui.Context, e *transcript.Entry) {
	k, t := v.a.kit, v.a.kit.T
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
	text := strings.Join(parts, "\n")
	v.column(c).Role(ui.RoleGroup).Label(L("You")).AlignItems(ui.End).Gap(k.Px(6)).PaddingY(k.Px(12)).Children(func() {
		if len(images) > 0 {
			ui.Row(c).Wrap().Justify(ui.End).Gap(k.Px(6)).MaxWidthPercent(85).Children(func() {
				for _, b := range images {
					v.picture(c, b.ID, b.Image, 192, L("Attached image"))
				}
			})
		}
		if text != "" {
			k.Text(c, text, 15, 24).Selectable().MaxWidthPercent(85).Padding(k.Px(10), k.Px(16)).Radius(k.Px(16)).
				Background(t.Secondary).TextColor(t.SecondaryForeground)
		}
	})
}
