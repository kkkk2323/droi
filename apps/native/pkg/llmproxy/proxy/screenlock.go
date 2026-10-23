package proxy

import (
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"time"
)

// ScreenLock reports whether the user's screen is locked. ok is false when the
// state cannot be read, and callers then ignore it.
type ScreenLock interface {
	Locked() (locked bool, ok bool)
}

// MacScreenLock reads the lock state from the IORegistry root, where macOS
// publishes IOConsoleLocked and, while locked, CGSSessionScreenIsLocked.
type MacScreenLock struct{}

func (MacScreenLock) Locked() (bool, bool) {
	if runtime.GOOS != "darwin" {
		return false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/ioreg", "-n", "Root", "-d1").Output()
	if err != nil {
		return false, false
	}
	return parseScreenLocked(out), true
}

var screenLockedPattern = regexp.MustCompile(`"(?:CGSSessionScreenIsLocked|IOConsoleLocked)"\s*=\s*Yes`)

func parseScreenLocked(ioregOutput []byte) bool {
	return screenLockedPattern.Match(ioregOutput)
}
