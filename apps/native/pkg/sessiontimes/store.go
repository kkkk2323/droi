// Package sessiontimes keeps the Session Times (CONTEXT.md, ADR 0032):
// when the user last sent each Session a message, when its last turn ended
// and when a Client last showed it. The Host keeps one Store; the Gateway
// writes it from the frames it forwards and serves it to every Client,
// which order their Session lists and mark Sessions unread by it.
package sessiontimes

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// Store is the Session Times of one computer. Its methods are safe from
// any goroutine.
type Store struct {
	path string
	now  func() time.Time

	mu sync.Mutex
	// base is the revision at Open; rev grows from it by one a change.
	base, rev uint64
	times     map[string]sessions.Times
	// changedAt is the revision of each Session's last change.
	changedAt map[string]uint64
	// working are the Sessions seen working since their last turn ended.
	working    map[string]bool
	backfilled bool
	// changed is closed, and replaced, at every change.
	changed chan struct{}
	// dirty is a change not written yet; saving, a writer at work.
	dirty, saving bool
	saved         chan struct{}
}

type file struct {
	// Backfilled: the transcripts were read for the messages sent before
	// the Store kept them (Backfill).
	Backfilled bool                      `json:"backfilled,omitempty"`
	Sessions   map[string]sessions.Times `json:"sessions"`
}

// Open reads the Store kept at path; "" keeps it in memory only. now is
// the clock, time.Now when nil.
func Open(path string, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	s := &Store{path: path, now: now, times: map[string]sessions.Times{}, changedAt: map[string]uint64{}, working: map[string]bool{}, changed: make(chan struct{})}
	// A revision that grows across launches, so a Client holding the last
	// launch's never takes the new one for the same.
	s.base = uint64(now().UnixMilli())
	s.rev = s.base
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			var f file
			if json.Unmarshal(b, &f) == nil {
				s.backfilled = f.Backfilled
				for id, t := range f.Sessions {
					s.times[id] = t
				}
			}
		}
	}
	return s
}

// Sent notes a message the user sent the Session.
func (s *Store) Sent(id string) {
	s.update(id, func(t *sessions.Times, now int64) bool {
		t.SentAt = max(t.SentAt, now)
		return true
	})
}

// Working notes the Session working, so that its next idle ends a turn.
func (s *Store) Working(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	s.working[id] = true
	s.mu.Unlock()
}

// Ended notes the Session idle: the end of a turn when it was seen working
// since its last end, or was sent a message since. Every Client's socket
// carries the same report; only the first ends the turn.
func (s *Store) Ended(id string) {
	s.update(id, func(t *sessions.Times, now int64) bool {
		if !s.working[id] && t.SentAt <= t.EndedAt {
			return false
		}
		delete(s.working, id)
		t.EndedAt = max(t.EndedAt, now)
		return true
	})
}

// Viewed notes a Client showing the Session.
func (s *Store) Viewed(id string) {
	s.update(id, func(t *sessions.Times, now int64) bool {
		t.ViewedAt = max(t.ViewedAt, now)
		return true
	})
}

func (s *Store) update(id string, fn func(t *sessions.Times, now int64) bool) {
	if id == "" {
		return
	}
	now := s.now().UnixMilli()
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.times[id]
	if !fn(&t, now) {
		return
	}
	s.times[id] = t
	s.changedLocked()
	s.changedAt[id] = s.rev
}

// changedLocked counts a change, wakes the waiting and has it written.
// Call with mu held.
func (s *Store) changedLocked() {
	s.rev++
	close(s.changed)
	s.changed = make(chan struct{})
	if s.path == "" {
		return
	}
	s.dirty = true
	if !s.saving {
		s.saving = true
		s.saved = make(chan struct{})
		go s.save()
	}
}

// save writes the Store until no change is left unwritten.
func (s *Store) save() {
	for {
		s.mu.Lock()
		if !s.dirty {
			s.saving = false
			close(s.saved)
			s.mu.Unlock()
			return
		}
		s.dirty = false
		b, err := json.Marshal(file{Backfilled: s.backfilled, Sessions: s.times})
		s.mu.Unlock()
		if err == nil {
			_ = writeAtomic(s.path, b)
		}
	}
}

// Flush waits for the changes made so far to be written.
func (s *Store) Flush() {
	s.mu.Lock()
	done := s.saved
	saving := s.saving
	s.mu.Unlock()
	if saving {
		<-done
	}
}

// Snapshot is every Session's Times now.
func (s *Store) Snapshot() sessions.TimesSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sessions.TimesSnapshot{Rev: s.rev, Full: true, Sessions: maps.Clone(s.times)}
}

// Since is what changed after revision after: the Times of the Sessions
// changed since, or every Session's (Full) when after is no revision of
// this launch.
func (s *Store) Since(after uint64) sessions.TimesSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if after < s.base || after > s.rev {
		return sessions.TimesSnapshot{Rev: s.rev, Full: true, Sessions: maps.Clone(s.times)}
	}
	out := sessions.TimesSnapshot{Rev: s.rev, Sessions: map[string]sessions.Times{}}
	if after == s.rev {
		return out
	}
	for id, rev := range s.changedAt {
		if rev > after {
			out.Sessions[id] = s.times[id]
		}
	}
	return out
}

// Wait is Since(after) once the revision is other than after, or when ctx
// ends, whichever comes first.
func (s *Store) Wait(ctx context.Context, after uint64) sessions.TimesSnapshot {
	s.mu.Lock()
	rev, changed := s.rev, s.changed
	s.mu.Unlock()
	if rev == after {
		select {
		case <-changed:
		case <-ctx.Done():
		}
	}
	return s.Since(after)
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, fs.ErrPermission) {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
