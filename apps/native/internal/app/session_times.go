package app

import (
	"errors"
	"maps"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
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
// either. Callers must not change the map.
func (a *App) sessionTimes() (map[string]sessions.Times, bool, uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := &a.times
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

// unreadSessions are the Sessions with a result nobody has seen: by the
// Session Times when the Gateway keeps them, else by this window's marks.
// The Session on screen is never one: the end of its turn can reach the
// Session Times before this window notes it seen. Main thread only;
// callers must not change the map.
func (a *App) unreadSessions() map[string]bool {
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
