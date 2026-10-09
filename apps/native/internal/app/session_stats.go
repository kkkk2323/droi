package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/session"
)

// maxSavedStats bounds the file; the Sessions touched longest ago go first.
const maxSavedStats = 2000

// sessionStats keeps each Session's Stats in one JSON file, so a Session
// opened in a later run shows what earlier runs timed.
type sessionStats struct {
	mu    sync.Mutex
	path  string
	m     map[string]savedStats
	timer *time.Timer
}

type savedStats struct {
	session.Stats
	UpdatedAt int64 `json:"updatedAt"`
}

func openSessionStats(path string) *sessionStats {
	st := &sessionStats{path: path, m: map[string]savedStats{}}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &st.m)
		}
	}
	return st
}

func (st *sessionStats) get(id string) (session.Stats, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.m[id]
	return s.Stats, ok
}

// put records a Session's Stats and writes the file a moment later, once
// for a burst of calls.
func (st *sessionStats) put(id string, s session.Stats) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.m[id] = savedStats{Stats: s, UpdatedAt: time.Now().UnixMilli()}
	if st.path == "" || st.timer != nil {
		return
	}
	st.timer = time.AfterFunc(2*time.Second, st.save)
}

func (st *sessionStats) save() {
	st.mu.Lock()
	st.timer = nil
	if len(st.m) > maxSavedStats {
		ids := make([]string, 0, len(st.m))
		for id := range st.m {
			ids = append(ids, id)
		}
		slices.SortFunc(ids, func(a, b string) int { return int(st.m[b].UpdatedAt - st.m[a].UpdatedAt) })
		for _, id := range ids[maxSavedStats:] {
			delete(st.m, id)
		}
	}
	b, err := json.Marshal(st.m)
	st.mu.Unlock()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(st.path), 0o700)
	tmp := st.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, st.path)
	}
}
