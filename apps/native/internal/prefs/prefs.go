// Package prefs keeps the Client's local preferences, in the place of the web
// Client's localStorage: one JSON file in the app's data directory, values
// stored under the same keys and in the same string forms the web Client uses
// (packages/daemon-layer/src/local-preference.ts), so the two read alike.
package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
)

// Store is a key-value file. Its zero value is in memory only.
type Store struct {
	mu        sync.Mutex
	file      string
	values    map[string]string
	listeners map[int]func()
	next      int
}

// Open loads the file, empty when it is missing or unreadable.
func Open(file string) *Store {
	s := &Store{file: file, values: map[string]string{}}
	if b, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(b, &s.values)
	}
	return s
}

// Memory is a Store that is never written, for tests.
func Memory() *Store { return &Store{values: map[string]string{}} }

func (s *Store) saveLocked() {
	if s.file == "" {
		return
	}
	b, err := json.MarshalIndent(s.values, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.file), 0o700)
	tmp := s.file + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, s.file)
	}
}

// Get returns a stored string.
func (s *Store) Get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		return "", false
	}
	v, ok := s.values[key]
	return v, ok
}

// Set stores a string and tells the listeners.
func (s *Store) Set(key, value string) {
	s.mu.Lock()
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	s.saveLocked()
	fns := s.listenersLocked()
	s.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// Remove deletes a key.
func (s *Store) Remove(key string) {
	s.mu.Lock()
	delete(s.values, key)
	s.saveLocked()
	fns := s.listenersLocked()
	s.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func (s *Store) listenersLocked() []func() {
	fns := make([]func(), 0, len(s.listeners))
	for _, fn := range s.listeners {
		fns = append(fns, fn)
	}
	return fns
}

// OnChange hears every Set and Remove.
func (s *Store) OnChange(fn func()) (unsubscribe func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listeners == nil {
		s.listeners = map[int]func(){}
	}
	id := s.next
	s.next++
	s.listeners[id] = fn
	return func() {
		s.mu.Lock()
		delete(s.listeners, id)
		s.mu.Unlock()
	}
}

// Bool is a preference stored as "true" or "false".
type Bool struct {
	Key      string
	Fallback bool
}

func (p Bool) Get(s *Store) bool {
	if v, ok := s.Get(p.Key); ok {
		return v == "true"
	}
	return p.Fallback
}

func (p Bool) Set(s *Store, v bool) {
	if v {
		s.Set(p.Key, "true")
	} else {
		s.Set(p.Key, "false")
	}
}

// String is a preference stored as is; "" is none.
type String struct {
	Key      string
	Fallback string
}

func (p String) Get(s *Store) string {
	if v, ok := s.Get(p.Key); ok && v != "" {
		return v
	}
	return p.Fallback
}

func (p String) Set(s *Store, v string) { s.Set(p.Key, v) }

// List is a list of strings stored as JSON.
type List struct{ Key string }

func (p List) Get(s *Store) []string {
	v, ok := s.Get(p.Key)
	if !ok {
		return nil
	}
	var out []string
	if json.Unmarshal([]byte(v), &out) != nil {
		return nil
	}
	return out
}

func (p List) Set(s *Store, v []string) {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	s.Set(p.Key, string(b))
}

// Has reports whether the list holds entry.
func (p List) Has(s *Store, entry string) bool { return slices.Contains(p.Get(s), entry) }

// Toggle adds entry, or takes it out when listed.
func (p List) Toggle(s *Store, entry string) {
	cur := p.Get(s)
	if i := slices.Index(cur, entry); i >= 0 {
		p.Set(s, slices.Delete(cur, i, i+1))
		return
	}
	p.Set(s, append(cur, entry))
}

// Number is a number stored as its decimal text; Fallback when unset or
// unreadable.
type Number struct {
	Key      string
	Fallback float64
}

func (p Number) Get(s *Store) float64 {
	v, ok := s.Get(p.Key)
	if !ok {
		return p.Fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return p.Fallback
	}
	return f
}

func (p Number) Set(s *Store, v float64) { s.Set(p.Key, strconv.FormatFloat(v, 'g', -1, 64)) }

// Map is a map of numbers stored as JSON, as sessionsFirstSeen is.
type Map struct{ Key string }

func (p Map) Get(s *Store) map[string]float64 {
	out := map[string]float64{}
	if v, ok := s.Get(p.Key); ok {
		_ = json.Unmarshal([]byte(v), &out)
	}
	return out
}

func (p Map) Set(s *Store, v map[string]float64) {
	b, _ := json.Marshal(v)
	s.Set(p.Key, string(b))
}

// The preferences, under the web Client's keys.
var (
	Theme    = String{Key: "droi.theme", Fallback: "light"}
	Font     = String{Key: "droi.font", Fallback: "geist"}
	TextSize = String{Key: "droi.textSize", Fallback: "default"}
	// Zoom is ⌘= and ⌘-, kept as Chromium keeps a page's zoom.
	Zoom           = Number{Key: "droi.zoom", Fallback: 1}
	SidebarVisible = Bool{Key: "droi.sidebar", Fallback: true}
	OpenInApp      = String{Key: "droi.openInApp"}
	ShowArchived   = Bool{Key: "droi.showArchived"}
	FavoriteModels = List{Key: "droi.favoriteModels"}
	LastSessionID  = String{Key: "droi.lastSession"}
	// SkippedUpdate is the release Skip this version passed over.
	SkippedUpdate     = String{Key: "droi.skippedUpdate"}
	FoldedWorkspaces  = List{Key: "droi.foldedWorkspaces"}
	PinnedWorkspaces  = List{Key: "droi.pinnedWorkspaces"}
	PinnedSessions    = List{Key: "droi.pinnedSessions"}
	WorkspaceSort     = String{Key: "droi.workspaceSort"}
	SessionSort       = String{Key: "droi.sessionSort"}
	ManualWorkspaces  = List{Key: "droi.workspaceOrder"}
	SessionsFirstSeen = Map{Key: "droi.sessionsFirstSeen"}
	DefaultToolMode   = String{Key: "droi.toolExecutionMode"}
	// NewSessionWorktree: new Sessions start in a worktree of their
	// repository; WorktreeLifecycle is the lifecycle picked last.
	NewSessionWorktree = Bool{Key: "droi.newSessionWorktree"}
	WorktreeLifecycle  = String{Key: "droi.worktreeLifecycle", Fallback: "ephemeral"}
	// Alerts holds the alert settings as JSON, as use-session-alerts.ts does.
	Alerts = String{Key: "droi.alerts"}
	// LargeMemoriesNoticed are the large Project Memories the corner card
	// announced, each as workspace + "\n" + its last consolidation.
	LargeMemoriesNoticed = List{Key: "droi.largeMemoriesNoticed"}
)

// Drafts keep the composer's text per Session, as drafts.ts does.
func DraftKey(sessionID string) string { return "droi.draft." + sessionID }
