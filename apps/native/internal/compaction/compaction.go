// Package compaction is the `/compact` runs this Client started. The Daemon
// does not change a Session's working state for one (only an automatic
// compaction mid-turn reports `compacting_conversation`), so the sidebar's
// mark while it runs and the alert when it is done come from here, and only on
// the Client that asked. The log is one per Client; Session ids are unique
// across Daemons (a port of compaction.ts).
package compaction

import (
	"maps"
	"sync"
)

// Done is a `/compact` that finished.
type Done struct {
	RemovedCount int
	FinishedAt   int64
}

// Finished is what a done listener hears: SessionID is the Session that was
// compacted, ShownIn where the user lands.
type Finished struct {
	SessionID string
	ShownIn   string
	Done
}

// Snapshot is the log at one moment; a later change never alters it.
type Snapshot struct {
	// Pending holds the Sessions whose `/compact` is still running.
	Pending map[string]bool
	// Finished holds the last `/compact` that finished for a Session, by the
	// Session it left the user in.
	Finished map[string]Done
}

type Log struct {
	mu       sync.Mutex
	snapshot Snapshot
	changed  map[int]func()
	done     map[int]func(Finished)
	next     int
}

func NewLog() *Log {
	return &Log{
		snapshot: Snapshot{Pending: map[string]bool{}, Finished: map[string]Done{}},
		changed:  map[int]func(){},
		done:     map[int]func(Finished){},
	}
}

func (l *Log) Start(sessionID string) {
	l.update(func(s *Snapshot) { s.Pending[sessionID] = true }, nil)
}

// Finish ends a run; shownIn is where the user lands: the Session itself, or
// the child of a handoff. nowMs is Unix milliseconds.
func (l *Log) Finish(sessionID, shownIn string, removedCount int, nowMs int64) {
	done := Done{RemovedCount: removedCount, FinishedAt: nowMs}
	l.update(func(s *Snapshot) {
		delete(s.Pending, sessionID)
		s.Finished[shownIn] = done
	}, &Finished{SessionID: sessionID, ShownIn: shownIn, Done: done})
}

func (l *Log) Fail(sessionID string) {
	l.update(func(s *Snapshot) { delete(s.Pending, sessionID) }, nil)
}

func (l *Log) update(edit func(*Snapshot), finished *Finished) {
	l.mu.Lock()
	next := Snapshot{Pending: maps.Clone(l.snapshot.Pending), Finished: maps.Clone(l.snapshot.Finished)}
	edit(&next)
	l.snapshot = next
	changed := make([]func(), 0, len(l.changed))
	for _, fn := range l.changed {
		changed = append(changed, fn)
	}
	var done []func(Finished)
	if finished != nil {
		for _, fn := range l.done {
			done = append(done, fn)
		}
	}
	l.mu.Unlock()
	for _, fn := range changed {
		fn()
	}
	for _, fn := range done {
		fn(*finished)
	}
}

func (l *Log) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.snapshot
}

// Subscribe hears every change.
func (l *Log) Subscribe(fn func()) (unsubscribe func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id := l.next
	l.next++
	l.changed[id] = fn
	return func() {
		l.mu.Lock()
		delete(l.changed, id)
		l.mu.Unlock()
	}
}

// OnDone is called once each time a `/compact` finishes.
func (l *Log) OnDone(fn func(Finished)) (unsubscribe func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id := l.next
	l.next++
	l.done[id] = fn
	return func() {
		l.mu.Lock()
		delete(l.done, id)
		l.mu.Unlock()
	}
}

// NoticeMs is how long the "compacted" notice stays after a `/compact` finishes.
const NoticeMs = 8_000

// NoticeShown is whether the notice for a `/compact` that finished at
// d.FinishedAt still shows at nowMs.
func (d Done) NoticeShown(nowMs int64) bool { return d.FinishedAt+NoticeMs > nowMs }
