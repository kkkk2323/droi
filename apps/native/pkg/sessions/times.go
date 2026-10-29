package sessions

import (
	"cmp"
	"maps"
	"slices"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// Times are what Droi keeps of a Session itself, apart from what the
// Daemon lists (CONTEXT.md, Session Times), in Unix milliseconds; 0 for
// never.
type Times struct {
	// SentAt is when the user last sent the Session a message through a
	// Gateway, from any Client.
	SentAt int64 `json:"sentAt,omitempty"`
	// EndedAt is when its last turn ended.
	EndedAt int64 `json:"endedAt,omitempty"`
	// ViewedAt is when a Client last showed it.
	ViewedAt int64 `json:"viewedAt,omitempty"`
}

// Unread is a turn that ended after a Client last showed the Session.
func (t Times) Unread() bool { return t.EndedAt > t.ViewedAt }

// TimesSnapshot is the Session Times at revision Rev: every Session's when
// Full, else those of the Sessions changed since the revision asked after.
type TimesSnapshot struct {
	Rev      uint64           `json:"rev"`
	Full     bool             `json:"full,omitempty"`
	Sessions map[string]Times `json:"sessions"`
}

// Apply is m with what the snapshot says: m's Times in place of a Full
// one's, or with its changes. It returns a new map; m is left as it is.
func (snap TimesSnapshot) Apply(m map[string]Times) map[string]Times {
	if snap.Full {
		return maps.Clone(snap.Sessions)
	}
	out := maps.Clone(m)
	if out == nil {
		out = map[string]Times{}
	}
	maps.Copy(out, snap.Sessions)
	return out
}

// UsedAt is when the Session was last used, in Unix ms: when the user last
// sent it a message, else the Daemon's date for it. The Daemon dates a
// Session by its file, which every hook run and every step of a turn
// writes, so a Session sent nothing through a Gateway since Droi kept its
// Times (one used in the droid CLI) is the only one dated that way.
func UsedAt(s Summary, times map[string]Times) int64 {
	if t := times[s.SessionID].SentAt; t > 0 {
		return t
	}
	return s.UpdatedAt * 1000
}

// Ranks float a Session above the time order: one whose turn runs first,
// then one whose result nobody has seen.
const (
	RankRest   = 0
	RankUnread = 1
	RankBusy   = 2
)

// Key is what places a Session in the list: its rank, then At, when it was
// last used, in Unix ms.
type Key struct {
	Rank int
	At   int64
}

// LiveKey is s's Key now. The Created order has no ranks: it is a plain
// list by creation.
func (o Order) LiveKey(s Summary) Key {
	k := Key{At: UsedAt(s, o.Times)}
	if o.Sessions == SessionsCreated {
		return k
	}
	switch {
	case o.Busy[s.SessionID]:
		k.Rank = RankBusy
	case o.Unread[s.SessionID]:
		k.Rank = RankUnread
	}
	return k
}

// KeyOf is the Key s was held at, or its Key now.
func (o Order) KeyOf(s Summary) Key {
	if k, ok := o.Held[s.SessionID]; ok {
		return k
	}
	return o.LiveKey(s)
}

// HoldKeys are the Keys of the listed Sessions now, for Order.Held while
// the pointer is over the list: no row moves under it, however the
// Sessions change, and a Session new to the list still takes its place.
func (o Order) HoldKeys(list []Summary) map[string]Key {
	held := make(map[string]Key, len(list))
	for _, s := range list {
		held[s.SessionID] = o.LiveKey(s)
	}
	return held
}

// created is when s was first seen, in Unix ms.
func (o Order) created(s Summary) float64 {
	if v, ok := o.FirstSeen[s.SessionID]; ok {
		return v
	}
	return float64(s.UpdatedAt) * 1000
}

// compare orders two Sessions of a list: pinned ones first, then by the
// order chosen, newest first.
func (o Order) compare(pins Pins, a, b Summary) int {
	if c := cmp.Compare(b2i(pins.Sessions[b.SessionID]), b2i(pins.Sessions[a.SessionID])); c != 0 {
		return c
	}
	if o.Sessions == SessionsCreated {
		if c := cmp.Compare(o.created(b), o.created(a)); c != 0 {
			return c
		}
	}
	ka, kb := o.KeyOf(a), o.KeyOf(b)
	return cmp.Or(cmp.Compare(kb.Rank, ka.Rank), cmp.Compare(kb.At, ka.At))
}

// View is how the sidebar lists the Sessions.
type View string

const (
	ViewWorkspaces View = "workspaces"
	ViewTime       View = "time"
)

var Views = []View{ViewWorkspaces, ViewTime}

var ViewLabels = map[View]string{ViewWorkspaces: l10n.N("By workspace"), ViewTime: l10n.N("By time")}

// The time view's sections.
const (
	NowGroupKey       = "droi:time:now"
	TodayGroupKey     = "droi:time:today"
	YesterdayGroupKey = "droi:time:yesterday"
	EarlierGroupKey   = "droi:time:earlier"
)

// GroupByTime is the time view, one list of every Session: the pinned
// ones, then those working or with an unseen result (Right now), then the rest by the
// day they were last used (created, in the Created order), today, yesterday
// and earlier, in local days of now. Each section is in the list's order.
// Every group's Places names each Session's Workspace, "" for a Scratch one.
func GroupByTime(list []Summary, pins Pins, order Order, now time.Time) []Group {
	var paths []string
	for _, s := range list {
		if !IsScratchSession(s) {
			paths = append(paths, WorkspaceOf(s))
		}
	}
	named := WorkspaceLabels(paths)
	places := make(map[string]string, len(list))
	for _, s := range list {
		if !IsScratchSession(s) {
			places[s.SessionID] = cmp.Or(named[WorkspaceOf(s)], WorkspaceLabel(""))
		}
	}
	sorted := slices.Clone(list)
	slices.SortStableFunc(sorted, func(a, b Summary) int { return order.compare(Pins{}, a, b) })
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()
	yesterday := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, now.Location()).UnixMilli()
	sections := []*Group{
		{Key: PinnedSessionsGroupKey, Label: l10n.L("Pinned")},
		{Key: NowGroupKey, Label: l10n.L("Right now")},
		{Key: TodayGroupKey, Label: l10n.L("Today")},
		{Key: YesterdayGroupKey, Label: l10n.L("Yesterday")},
		{Key: EarlierGroupKey, Label: l10n.L("Earlier")},
	}
	for _, s := range sorted {
		k := order.KeyOf(s)
		at := float64(k.At)
		if order.Sessions == SessionsCreated {
			at = order.created(s)
		}
		var g *Group
		switch {
		case pins.Sessions[s.SessionID]:
			g = sections[0]
		case k.Rank > RankRest:
			g = sections[1]
		case at >= float64(today):
			g = sections[2]
		case at >= float64(yesterday):
			g = sections[3]
		default:
			g = sections[4]
		}
		g.Sessions = append(g.Sessions, s)
	}
	var out []Group
	for _, g := range sections {
		if len(g.Sessions) > 0 {
			g.Places = places
			out = append(out, *g)
		}
	}
	return out
}
