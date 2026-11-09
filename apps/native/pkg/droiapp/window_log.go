package droiapp

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

// windowLogMax is the size past which the window's log starts over at the next launch.
const windowLogMax = 5 << 20

// logToFile sends the window's log lines to userData/logs/window.log as well as to stderr: an app started from
// the Finder has nowhere to put stderr, and a line nobody can read diagnoses nothing. A log past windowLogMax
// starts over, the previous one kept as window.log.1.
func logToFile(userData string) {
	dir := filepath.Join(userData, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(dir, "window.log")
	if st, err := os.Stat(path); err == nil && st.Size() > windowLogMax {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
}
