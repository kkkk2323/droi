package omp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DiskSession is one OMP session file found under the agent sessions dir.
type DiskSession struct {
	ID        string
	Title     string
	Cwd       string
	Path      string
	UpdatedAt time.Time
}

// DefaultSessionRoot is ~/.omp/agent (PI_CODING_AGENT_DIR default).
func DefaultSessionRoot(home string) string {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".omp", "agent")
}

// ListSessions walks sessionDir (or root/sessions) for *.jsonl files and
// reads the title/session headers. It is best-effort and skips unreadable files.
func ListSessions(sessionRoot string) ([]DiskSession, error) {
	if sessionRoot == "" {
		sessionRoot = DefaultSessionRoot("")
	}
	dir := sessionRoot
	if filepath.Base(dir) != "sessions" {
		dir = filepath.Join(dir, "sessions")
	}
	var out []DiskSession
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		s, ok := readDiskSession(path)
		if ok {
			out = append(out, s)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return out, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func readDiskSession(path string) (DiskSession, bool) {
	f, err := os.Open(path)
	if err != nil {
		return DiskSession{}, false
	}
	defer f.Close()
	st, _ := f.Stat()
	s := DiskSession{Path: path, UpdatedAt: st.ModTime()}
	// File name: 2026-07-29T02-14-35-903Z_019faba6-….jsonl
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if i := strings.IndexByte(base, '_'); i >= 0 && i+1 < len(base) {
		s.ID = base[i+1:]
	}
	dec := json.NewDecoder(f)
	// Only the first few header records matter.
	for i := 0; i < 8; i++ {
		var raw map[string]json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			break
		}
		typ, _ := rawString(raw["type"])
		switch typ {
		case "title":
			if t, ok := rawString(raw["title"]); ok {
				s.Title = t
			}
			if u, ok := rawString(raw["updatedAt"]); ok {
				if tm, err := time.Parse(time.RFC3339Nano, u); err == nil {
					s.UpdatedAt = tm
				} else if tm, err := time.Parse(time.RFC3339, u); err == nil {
					s.UpdatedAt = tm
				}
			}
		case "session":
			if id, ok := rawString(raw["id"]); ok {
				s.ID = id
			}
			if cwd, ok := rawString(raw["cwd"]); ok {
				s.Cwd = cwd
			}
			if ts, ok := rawString(raw["timestamp"]); ok {
				if tm, err := time.Parse(time.RFC3339Nano, ts); err == nil && s.UpdatedAt.IsZero() {
					s.UpdatedAt = tm
				}
			}
		}
	}
	if s.ID == "" {
		return DiskSession{}, false
	}
	if s.Title == "" {
		s.Title = "OMP session"
	}
	return s, true
}
