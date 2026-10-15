package host

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The Daemon's stdout and stderr go to one append-only log, rotated once to
// <name>.old past a megabyte, so a crash can be read after the fact.
const (
	DaemonLogMaxBytes  = 1024 * 1024
	DaemonLogTailBytes = 2048
)

// OpenDaemonLog opens the log for appending, rotating it first when full;
// nil when it cannot be opened (the Daemon then runs without one).
func OpenDaemonLog(path string) *os.File {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil
	}
	if st, err := os.Stat(path); err == nil && st.Size() > DaemonLogMaxBytes {
		_ = os.Remove(path + ".old")
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil
	}
	return f
}

// ReadDaemonLogTail is the end of the log, "" when there is none.
func ReadDaemonLogTail(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	n := min(int64(DaemonLogTailBytes), st.Size())
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, st.Size()-n); err != nil && err != io.EOF {
		return ""
	}
	return strings.TrimSpace(string(buf))
}
