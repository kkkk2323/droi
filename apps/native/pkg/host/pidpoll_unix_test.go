//go:build !windows

package host

import (
	"errors"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// goneWithin is how long a process that was just killed may take to disappear.
// A SIGKILLed process is reaped in microseconds on an idle machine and in
// milliseconds under load, so one signal-0 sample right after the kill is a
// coin toss: the review measured a grandchild that still answered for 6.7 ms
// and then vanished, and a non-race suite failed one run in twenty on it.
const goneWithin = 2 * time.Second

// waitGoneUnix waits until pid answers no signal, and fails the test if it is
// still there when the bound passes. "Still alive" has to mean still alive
// after a fair wait; only ESRCH is proof of absence, and EPERM is proof of the
// opposite.
func waitGoneUnix(t *testing.T, pid int) {
	t.Helper()
	if left := pollGone(pid, goneWithin); left {
		out, _ := exec.Command("ps", "-o", "pid,ppid,stat,pgid,command", "-p", strconv.Itoa(pid)).Output()
		t.Fatalf("pid %d still answers signal 0 %s after it was killed\n%s", pid, goneWithin, out)
	}
}

// pollGone polls pid until it is gone, and answers whether it outlasted the
// bound.
func pollGone(pid int, within time.Duration) bool {
	for deadline := time.Now().Add(within); time.Now().Before(deadline); {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
	return syscall.Kill(pid, 0) != syscall.ESRCH
}
