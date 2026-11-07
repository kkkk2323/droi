//go:build windows

package host

import (
	"testing"
	"time"
)

// Unlike the author's parent-exits-first test, never call Stop until after the
// parent has spontaneously exited and its generation has retired.
func TestReview228R4WindowsParentReallyCrashesFirst(t *testing.T) {
	data := t.TempDir()
	w := newWatch()
	s := w.supervisor(helperSpawnIn(t, data,
		"GO_KERNEL_CHILD_HOLD_MS=60000", "GO_KERNEL_EXIT_MS=400"))
	s.Backoff = []time.Duration{time.Hour}
	defer s.Close()
	defer s.Stop()
	s.Start()
	w.waitState(t, KernelRunning)
	child := waitChildPid(t, data)
	if !processAlive(child) {
		t.Fatalf("child %d was never live", child)
	}
	w.waitState(t, KernelRestarting)
	waitGone(t, child)
}
