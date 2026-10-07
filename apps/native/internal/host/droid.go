package host

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

func droidBinary() string {
	if runtime.GOOS == "windows" {
		return "droid.exe"
	}
	return "droid"
}

// LocateDroid finds the `droid` executable: the settings override, then
// PATH, then ~/.local/bin. "" when there is none.
func LocateDroid(override, pathEnv, home string) string {
	exists := func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && !st.IsDir()
	}
	if override != "" && exists(override) {
		return override
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		if p := filepath.Join(dir, droidBinary()); exists(p) {
			return p
		}
	}
	if p := filepath.Join(home, ".local", "bin", droidBinary()); exists(p) {
		return p
	}
	return ""
}

// DroidBuild is which `droid` file a Daemon was started from. The CLI
// replaces its executable when it updates itself; a running Daemon keeps
// the old build and its model catalog until it is restarted.
type DroidBuild struct {
	Path  string
	Ino   uint64
	Mtime time.Time
}

// BuildOf stats path; nil when it cannot.
func BuildOf(path string) *DroidBuild {
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	return &DroidBuild{Path: path, Ino: inode(st), Mtime: st.ModTime()}
}

// Replaced reports that the file at the build's path is no longer the one
// the Daemon started from. A file missing mid-update is not newer yet.
func (b *DroidBuild) Replaced() bool {
	if b == nil {
		return false
	}
	now := BuildOf(b.Path)
	return now != nil && (now.Ino != b.Ino || !now.Mtime.Equal(b.Mtime))
}

// DaemonArgs is the Daemon's command line.
func DaemonArgs(port int, liveness []string, runtimeOverlay string) []string {
	args := append([]string{"daemon", "--host", "127.0.0.1", "--port", strconv.Itoa(port)}, liveness...)
	if runtimeOverlay != "" {
		args = append(args, "--settings", runtimeOverlay)
	}
	return args
}

// sessionIDPattern guards file lookups by Session id.
func validSessionID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !(r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// FindSessionTranscript looks a Session's transcript up in every Workspace
// folder under dir (~/.factory/sessions); "" when absent.
func FindSessionTranscript(dir, sessionID string) string {
	if !validSessionID(sessionID) {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name(), sessionID+".jsonl")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
