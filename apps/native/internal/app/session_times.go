package app

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/activity"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// timesState is the Session Times the window follows through its Gateway
// (ADR 0032). Guarded by App.mu.
type timesState struct {
	// ok: the Gateway keeps them. One of an older Droi keeps none, and the
	// window then dates Sessions as the Daemon does and keeps its own
	// unread marks.
	ok    bool
	times map[string]sessions.Times
	// viewed are the Sessions this window showed, at when, ahead of the
	// Gateway's answer that has them.
	viewed map[string]int64
	// gen counts the changes of times and viewed, for what is built from them.
	gen uint64
}

// timesRetry is the wait before asking a Gateway that did not answer again.
const timesRetry = 2 * time.Second

// followTimes keeps the window's Session Times up to date until the App
// closes: every Session's first, then each change as the Gateway has it.
func (a *App) followTimes() {
	gw := a.cfg.Gateway
	var rev uint64
	var times map[string]sessions.Times
	for !a.closed() {
		snap, err := gw.SessionTimes(a.ctx, rev)
		if errors.Is(err, gateway.ErrNotFound) {
			return
		}
		if err != nil {
			select {
			case <-a.ctx.Done():
				return
			case <-time.After(timesRetry):
			}
			continue
		}
		changed := snap.Full || len(snap.Sessions) > 0
		times, rev = snap.Apply(times), snap.Rev
		a.mu.Lock()
		first := !a.times.ok
		a.times.ok = true
		if changed || first {
			a.times.times = times
			a.times.gen++
		}
		a.mu.Unlock()
		if changed || first {
			a.redraw()
		}
	}
}

// sessionTimes are the Session Times this frame shows, with the views
// this window made, and whether the Gateway keeps them; gen changes with
// either. Callers must not change the map. On Kernel there are none: the
// list's rows date a conversation by its last activity, and say when a
// person last saw it.
func (a *App) sessionTimes() (map[string]sessions.Times, bool, uint64) {
	onKernel := a.onKernel()
	a.mu.Lock()
	defer a.mu.Unlock()
	st := &a.times
	if onKernel {
		return nil, true, st.gen
	}
	if len(st.viewed) == 0 {
		return st.times, st.ok, st.gen
	}
	m := &a.listMemo
	if m.timesGen != st.gen || m.times == nil {
		merged := maps.Clone(st.times)
		if merged == nil {
			merged = map[string]sessions.Times{}
		}
		for id, at := range st.viewed {
			t := merged[id]
			if at > t.ViewedAt {
				t.ViewedAt = at
				merged[id] = t
			}
		}
		m.timesGen, m.times = st.gen, merged
	}
	return m.times, st.ok, st.gen
}

// viewed tells the Gateway that the Session is on screen, which reads it
// on every Client; the window reads it at once.
func (a *App) viewed(id string) {
	if id == "" {
		return
	}
	if a.onKernel() {
		a.seenOnKernel(id)
		return
	}
	a.mu.Lock()
	if a.times.viewed == nil {
		a.times.viewed = map[string]int64{}
	}
	a.times.viewed[id] = a.cfg.Now().UnixMilli()
	a.times.gen++
	a.mu.Unlock()
	if gw := a.cfg.Gateway; gw != nil {
		go func() { _ = gw.SessionViewed(a.ctx, id) }()
	}
}

// seenOnKernel tells Kernel that a person has seen conversation id up to
// its last activity, which reads it on every client; the window reads it at
// once. Opening a conversation tells Kernel by itself, so only a row that
// moved since needs it: one on screen while it works, or shown again from
// what this window keeps open.
func (a *App) seenOnKernel(id string) {
	conv, ok := a.kernelID(id)
	if !ok {
		return
	}
	a.mu.Lock()
	i := slices.IndexFunc(a.listed, func(s sessions.Summary) bool { return s.SessionID == id })
	if i < 0 || !a.listed[i].Unread() {
		a.mu.Unlock()
		return
	}
	// Seen up to the activity this window has, on Kernel's clock: a time of this computer's could be ahead
	// of Kernel's and hide what comes next.
	at := a.listed[i].ActivityAt
	list := slices.Clone(a.listed)
	list[i].ViewedAt = at
	a.listed = list
	a.listRev++
	a.mu.Unlock()
	a.redraw()
	a.tellSeen(conv, at)
}

// seenQuiet is how long a conversation's "seen" waits for the next before it is written. One on screen while it
// works moves with every step Kernel indexes, and each step would cost a write and two index.changed on every
// client; one write once it rests says the same. Other clients draw no mark on it while it works anyway.
const seenQuiet = 500 * time.Millisecond

// seenWrites are the "seen" times waiting for seenQuiet, by conversation.
type seenWrites struct {
	mu     sync.Mutex
	at     map[kernel.ID]int64
	timers map[kernel.ID]*time.Timer
}

// tellSeen writes that conversation conv is seen up to at, once it has rested seenQuiet.
func (a *App) tellSeen(conv kernel.ID, at int64) {
	w := &a.seen
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.at == nil {
		w.at, w.timers = map[kernel.ID]int64{}, map[kernel.ID]*time.Timer{}
	}
	w.at[conv] = max(w.at[conv], at)
	// A fresh timer each time: Reset on one whose function has started runs that function again later, when it
	// may take the next write's place.
	if t := w.timers[conv]; t != nil {
		t.Stop()
	}
	var t *time.Timer
	t = time.AfterFunc(seenQuiet, func() {
		w.mu.Lock()
		if w.timers[conv] != t {
			w.mu.Unlock()
			return
		}
		at := w.at[conv]
		delete(w.at, conv)
		delete(w.timers, conv)
		w.mu.Unlock()
		a.writeSeen(a.ctx, conv, at)
	})
	w.timers[conv] = t
}

// flushSeen writes the "seen" times still waiting, so a window closed just after a turn it watched does not leave
// that turn unread.
func (a *App) flushSeen() {
	w := &a.seen
	w.mu.Lock()
	pending := w.at
	for _, t := range w.timers {
		t.Stop()
	}
	w.at, w.timers = nil, nil
	w.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for conv, at := range pending {
		a.writeSeen(ctx, conv, at)
	}
}

func (a *App) writeSeen(ctx context.Context, conv kernel.ID, at int64) {
	c, err := a.kernel.client()
	if err != nil {
		return
	}
	_ = c.UpdateIndex(ctx, kernel.IndexUpdateParams{ID: conv, ViewedAt: kernel.ViewedAtTime(at)})
}

// unreadSessions are the Sessions with a result nobody has seen: on Kernel
// by the list's rows, else by the Session Times when the Gateway keeps
// them, else by this window's marks. The Session on screen is never one:
// the end of its turn can reach the Session Times before this window notes
// it seen. Main thread only; callers must not change the map.
func (a *App) unreadSessions(listed []sessions.Summary, busy map[string]activity.Activity) map[string]bool {
	if a.onKernel() {
		return a.kernelUnread(listed, busy)
	}
	times, ok, gen := a.sessionTimes()
	if !ok {
		return a.unread
	}
	m := &a.listMemo
	if m.unread == nil || m.unreadGen != gen {
		unread := map[string]bool{}
		for id, t := range times {
			if t.Unread() {
				unread[id] = true
			}
		}
		m.unread, m.unreadGen = unread, gen
	}
	if id := a.route.SessionID; a.route.Name == "session" && m.unread[id] {
		out := maps.Clone(m.unread)
		delete(out, id)
		return out
	}
	return m.unread
}

// kernelUnread are the listed conversations that moved after a person last
// saw them. One at work is not yet: Kernel's last activity moves with each
// step of a run, and its result is news once the run ends, as the Session
// Times have it. Nor is the one on screen, which this window reads as it
// moves.
func (a *App) kernelUnread(listed []sessions.Summary, busy map[string]activity.Activity) map[string]bool {
	// No rows is not this revision's list, which a caller with none to hand asks about: kept, it would stand for
	// the real one.
	if len(listed) == 0 {
		return nil
	}
	m := &a.listMemo
	if m.kernelUnread == nil || m.kernelUnreadRev != a.shownRev {
		unread := map[string]bool{}
		for _, s := range listed {
			if s.Unread() {
				unread[s.SessionID] = true
			}
		}
		m.kernelUnread, m.kernelUnreadRev = unread, a.shownRev
	}
	out, cloned := m.kernelUnread, false
	drop := func(id string) {
		if !out[id] {
			return
		}
		if !cloned {
			out, cloned = maps.Clone(out), true
		}
		delete(out, id)
	}
	for id, doing := range busy {
		if doing != "" {
			drop(id)
		}
	}
	if a.route.Name == "session" {
		drop(a.route.SessionID)
	}
	return out
}
