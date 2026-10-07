package app

import (
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/internal/transcript"
)

// railItem is one message the user sent, as the TurnRail shows it.
type railItem struct {
	id string
	// entry is the user's entry in the transcript, -1 for a message from
	// before the loaded ones: jumping there loads it first.
	entry    int
	prompt   string
	response string
}

const (
	// railPitch is the height of one mark; the rail scrolls once marks
	// outgrow it.
	railPitch = 10
	// railInset is the room above the first mark and below the last.
	railInset         = 6
	railResponseChars = 300
	// railMinWidth is the transcript's width the rail needs
	// (@min-[53rem]): below it the reading column has no room beside it.
	railMinWidth = 848
)

func entryText(e *transcript.Entry) string {
	var parts []string
	for _, b := range e.Blocks {
		if b.Kind == transcript.Text {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

// railItems are the rail's items: the user's messages from before the
// loaded ones (unloaded, oldest first), then the loaded ones, each with the
// start of the reply that follows it.
func railItems(entries []*transcript.Entry, unloaded []*transcript.Entry) []railItem {
	prompt := func(e *transcript.Entry) string {
		if t := entryText(e); t != "" {
			return t
		}
		return "Attached image"
	}
	var items []railItem
	for _, e := range unloaded {
		items = append(items, railItem{id: e.ID, entry: -1, prompt: prompt(e)})
	}
	for i, e := range entries {
		if e.User {
			items = append(items, railItem{id: e.ID, entry: i, prompt: prompt(e)})
			continue
		}
		// After the unloaded messages the first rows can be the middle of
		// a reply, which is no message's.
		if n := len(items); n > 0 && items[n-1].entry >= 0 && items[n-1].response == "" {
			r := []rune(entryText(e))
			items[n-1].response = string(r[:min(len(r), railResponseChars)])
		}
	}
	return items
}

// activeRailItem is the message whose turn holds entry; the last one when
// unknown (-1).
func activeRailItem(items []railItem, entry int) int {
	if entry < 0 {
		return len(items) - 1
	}
	active := 0
	for i, it := range items {
		if it.entry >= 0 && it.entry <= entry {
			active = i
		}
	}
	return active
}

// unloadedUserMessages are the user's messages from before the first one
// the transcript holds; all of them when it holds none, since a Session
// loads its latest messages.
func unloadedUserMessages(all, entries []*transcript.Entry) []*transcript.Entry {
	loaded := map[string]bool{}
	for _, e := range entries {
		loaded[e.ID] = true
	}
	for i, e := range all {
		if loaded[e.ID] {
			return all[:i]
		}
	}
	return all
}

// railState is the rail's: the user's messages read from the Daemon, the
// mark previewed, and a jump to a message still loading.
type railState struct {
	mu      sync.Mutex
	all     []*transcript.Entry
	read    bool
	reading bool

	preview   int
	jumpingTo string
	scroll    ui.ScrollState
}

// userMessages reads every message the user sent, once: the Daemon filters
// a page by role, so it costs a request or two even in a long Session.
func (v *sessionView) userMessages(s *session.Session) []*transcript.Entry {
	r := &v.rail
	r.mu.Lock()
	defer r.mu.Unlock()
	if !s.HasOlderMessages() || r.read || r.reading || v.a.ctl == nil {
		return r.all
	}
	r.reading = true
	go func() {
		var pages [][]protocol.FactoryDroidMessage
		cl, err := v.a.ctl.Client()
		cursor := ""
		for err == nil {
			limit := float64(olderPage)
			var res *protocol.GetSessionMessagesResult
			res, err = cl.GetSessionMessages(v.a.ctx, protocol.GetSessionMessagesParams{SessionID: v.id, Role: protocol.MessageRoleNoSystemUser, Cursor: cursor, Limit: &limit})
			if err != nil {
				break
			}
			pages = append(pages, res.Messages)
			if !res.HasMore || len(res.Messages) == 0 {
				break
			}
			cursor = res.Messages[len(res.Messages)-1].ID
			if res.NextCursor != "" {
				cursor = res.NextCursor
			}
		}
		var msgs []protocol.FactoryDroidMessage
		for _, p := range pages {
			msgs = append(msgs, p...)
		}
		// Pages come newest first.
		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
		all := transcript.Build(session.FilterMessagesForUI(msgs))
		r.mu.Lock()
		r.reading, r.read = false, err == nil
		if err == nil {
			r.all = all
		}
		r.mu.Unlock()
		v.a.redraw()
	}()
	return r.all
}

// loadUntil pages older messages in until messageID is loaded or none are
// left, then calls done on the main thread.
func (v *sessionView) loadUntil(messageID string, done func()) {
	ctl := v.a.ctl
	go func() {
		defer v.a.cfg.Update(done)
		cl, err := ctl.Client()
		if err != nil {
			return
		}
		for {
			s := ctl.Store().Session(v.id)
			if s == nil || !s.HasOlderMessages() {
				return
			}
			cursor, ok := s.OlderMessagesCursor()
			if !ok {
				return
			}
			limit := float64(olderPage)
			res, err := cl.GetSessionMessages(v.a.ctx, protocol.GetSessionMessagesParams{SessionID: v.id, Cursor: cursor, Limit: &limit})
			if err != nil {
				return
			}
			page := make([]protocol.FactoryDroidMessage, len(res.Messages))
			found := false
			for i, m := range res.Messages {
				page[len(page)-1-i] = m
				found = found || m.ID == messageID
			}
			ctl.Store().PrependOlderMessages(v.id, page, res.HasMore)
			if found || !res.HasMore {
				return
			}
		}
	}()
}

// turnRail is the TurnRail: a ladder of marks along the transcript's right
// edge, one per message the user sent. Hovering a mark previews the message
// and the start of its reply; clicking it jumps there, loading the way
// first for a message from before the loaded ones.
func (v *sessionView) turnRail(c *ui.Context, s *session.Session, rows []transcriptRow, width float32) {
	k, t := v.a.kit, v.a.kit.T
	if width < k.Px(railMinWidth) {
		return
	}
	items := railItems(v.entries, unloadedUserMessages(v.userMessages(s), v.entries))
	if len(items) < 2 {
		return
	}
	// The entry of the first row in view, as the turn being read.
	reading := -1
	if first, _ := v.list.Visible(); !v.list.AtEnd() && first >= 1 && first-1 < len(rows) {
		reading = v.entryIndex(rows[first-1].e)
	}
	active := activeRailItem(items, reading)
	r := &v.rail
	pitch, inset := k.Px(railPitch), k.Px(railInset)
	_, h := c.Size()
	maxH := min(k.Px(420), h*0.6)
	nav := ui.Column(c).Role(ui.RoleGroup).Label("Your messages").Absolute().Top(0).Bottom(0).Right(k.Px(8)).Width(k.Px(28)).Justify(ui.Center)
	nav.Children(func() {
		box := ui.Box(c).FillWidth().MaxHeight(maxH)
		if !box.Hovered() {
			r.preview = -1
		}
		box.Children(func() {
			sc := ui.Scroll(c).TrackScroll(&r.scroll).FillWidth().MaxHeight(maxH)
			if !box.Hovered() {
				// Keep the active mark in view, as the web Client does while
				// no pointer is working the rail.
				center := inset + float32(active)*pitch + pitch/2
				if center < r.scroll.Y+k.Px(24) || center > r.scroll.Y+maxH-k.Px(24) {
					r.scroll.Y = max(0, center-maxH/2)
				}
			}
			sc.Children(func() {
				ui.Column(c).FillWidth().PaddingY(inset).Children(func() {
					for i, it := range items {
						current := i == active
						label := "Jump to message " + itoa(i+1)
						if current {
							label += ", current"
						}
						b := ui.ButtonBase(c).Key(it.id).Label(label).FillWidth().Height(pitch).Shrink(0).
							Justify(ui.End).AlignItems(ui.Center).Cursor(ui.CursorPointer)
						if b.Hovered() || b.Focused() {
							r.preview = i
						}
						w, color := k.Px(12), t.MutedForeground.Alpha(0.4)
						switch {
						case r.jumpingTo == it.id:
							w, color = k.Px(20), t.Info.Alpha(0.6+0.4*float32(pulseWave(c)))
						case current:
							w, color = k.Px(20), t.Foreground
						case r.preview == i:
							w, color = k.Px(18), t.MutedForeground
						}
						b.Children(func() { ui.Box(c).Width(w).Height(k.Px(2)).Radius(k.Px(1)).Background(color) })
						if b.Clicked() {
							v.jumpTo(it, rows)
						}
					}
				})
			})
			if r.preview >= 0 && r.preview < len(items) {
				it := items[r.preview]
				top := inset + float32(r.preview)*pitch + pitch/2 - r.scroll.Y
				ui.Column(c).Role(ui.RoleTooltip).Absolute().Right(k.Px(38)).Top(top).Width(k.Px(288)).Margin(-k.Px(20), 0, 0, 0).
					Padding(k.Px(10), k.Px(12)).Radius(k.Px(12)).Border(1, t.Border).Background(t.Popover).PassThrough().
					Shadow(0, k.Px(4), k.Px(12), 0, ui.RGBA(0, 0, 0, 0.08)).Children(func() {
					k.Text(c, it.prompt, 13, 19.5).FontWeight(500).TextColor(t.PopoverForeground).MaxLines(2)
					switch {
					case r.jumpingTo == it.id:
						k.Text(c, "Loading earlier messages…", 12, 20).TextColor(t.MutedForeground).Margin(k.Px(4), 0, 0, 0)
					case it.response != "":
						k.Text(c, it.response, 12, 20).TextColor(t.MutedForeground).MaxLines(3).Margin(k.Px(4), 0, 0, 0)
					}
				})
			}
		})
	})
}

func (v *sessionView) entryIndex(e *transcript.Entry) int {
	for i, x := range v.entries {
		if x == e {
			return i
		}
	}
	return -1
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

func (v *sessionView) jumpTo(it railItem, rows []transcriptRow) {
	r := &v.rail
	if it.entry >= 0 {
		if row := rowOf(rows, v.entries[it.entry]); row >= 0 {
			v.hold()
			v.list.ScrollTo(row, ui.Start)
		}
		return
	}
	if r.jumpingTo != "" {
		return
	}
	r.jumpingTo = it.id
	v.loadUntil(it.id, func() {
		r.jumpingTo = ""
		// The loaded messages reach the transcript at its next build.
		v.pendingJump = it.id
	})
}
