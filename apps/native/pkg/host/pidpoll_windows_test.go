//go:build windows

package host

import "testing"

// waitGoneUnix is the same rule on Windows: a process that was ended is gone
// when its exit code can be read, and that is a poll rather than a sample —
// TerminateProcess and TerminateJobObject are asynchronous.
func waitGoneUnix(t *testing.T, pid int) {
	t.Helper()
	waitGone(t, pid)
}
