package diag

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileSink appends newline-delimited JSON to a file and rotates it once it
// grows past maxBytes, keeping the newest `keep` rotated files, or all files
// when keep is negative.
type FileSink struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	keep     int
	file     *os.File
	written  int64
}

// OpenFileSink opens (creating if needed) an append-only JSONL sink.
// A zero keep defaults to 3 rotated files; a negative keep disables pruning.
func OpenFileSink(path string, maxBytes int64, keep int) (*FileSink, error) {
	if maxBytes <= 0 {
		maxBytes = 10 << 20
	}
	if keep == 0 {
		keep = 3
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &FileSink{path: path, maxBytes: maxBytes, keep: keep}
	if err := s.openLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileSink) openLocked() error {
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	s.file = f
	s.written = info.Size()
	return nil
}

// Write appends one line (a trailing newline is added).
func (s *FileSink) Write(line []byte) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		if err := s.openLocked(); err != nil {
			return err
		}
	}
	if s.written >= s.maxBytes {
		if err := s.rotateLocked(); err != nil {
			return err
		}
	}
	n, err := s.file.Write(append(line, '\n'))
	s.written += int64(n)
	return err
}

func (s *FileSink) rotateLocked() error {
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
	// Reserve a unique name so rapid rotations cannot overwrite an archive.
	archive, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+"."+s.archiveTimeLocked(time.Now()).Format(archiveStamp)+".")
	if err != nil {
		return err
	}
	rotated := archive.Name()
	if err := archive.Close(); err != nil {
		_ = os.Remove(rotated)
		return err
	}
	if err := os.Rename(s.path, rotated); err != nil {
		_ = os.Remove(rotated)
		if !os.IsNotExist(err) {
			return err
		}
	}
	if err := s.openLocked(); err != nil {
		return err
	}
	s.pruneLocked()
	return nil
}

const archiveStamp = "20060102-150405.000000000"

// archiveTimeLocked returns a time later than every existing archive's stamp.
// The Windows clock can tick coarser than the stamp, so rapid rotations would
// otherwise share a stamp and sort by their random suffix instead.
func (s *FileSink) archiveTimeLocked(now time.Time) time.Time {
	matches, _ := filepath.Glob(s.path + ".*")
	for _, m := range matches {
		rest := strings.TrimPrefix(m, s.path+".")
		if len(rest) < len(archiveStamp) {
			continue
		}
		if t, err := time.ParseInLocation(archiveStamp, rest[:len(archiveStamp)], time.Local); err == nil && !now.After(t) {
			now = t.Add(time.Nanosecond)
		}
	}
	return now
}

func (s *FileSink) pruneLocked() {
	if s.keep < 0 {
		return
	}
	matches, err := filepath.Glob(s.path + ".*")
	if err != nil || len(matches) <= s.keep {
		return
	}
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-s.keep] {
		_ = os.Remove(old)
	}
}

// Close flushes and closes the underlying file.
func (s *FileSink) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}
