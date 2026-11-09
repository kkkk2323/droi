package droidimport

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// Record is what one import wrote: the conversation, and the file as it was, so the list can tell a session
// imported from one droid went on with since.
type Record struct {
	Conversation kernel.ID `json:"conversation"`
	Hash         string    `json:"hash"`
	Size         int64     `json:"size"`
	ModTime      int64     `json:"modTime"`
	// At is when the import ended, in epoch milliseconds.
	At int64 `json:"at"`
}

// Current reports that src is still the file the record imported.
func (r Record) Current(src Source) bool {
	return r.Size == src.Size && r.ModTime == src.ModTime.UnixMilli()
}

// StoreFile is the records' file in the Host's user data folder.
const StoreFile = "droid-imports.json"

// Store keeps the records of the imports, by droid's session id, in one JSON file.
type Store struct {
	path string
	mu   sync.Mutex
	m    map[string]Record
}

// OpenStore reads the records at path; a file that is not there, or not JSON, holds none.
func OpenStore(path string) *Store {
	s := &Store{path: path, m: map[string]Record{}}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &s.m)
		if s.m == nil {
			s.m = map[string]Record{}
		}
	}
	return s
}

// Get is the record of droid's session id.
func (s *Store) Get(id string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[id]
	return r, ok
}

// Imported is when each imported conversation's import ended, in epoch milliseconds.
func (s *Store) Imported() map[int64]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int64]int64, len(s.m))
	for _, r := range s.m {
		out[int64(r.Conversation)] = r.At
	}
	return out
}

// Put keeps the record of droid's session id, and writes the file.
func (s *Store) Put(id string, r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = r
	raw, err := json.MarshalIndent(s.m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	return nil
}
