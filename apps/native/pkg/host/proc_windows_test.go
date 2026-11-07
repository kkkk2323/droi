//go:build windows

package host

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestWindowsJobEndsTheSidecarsChildren is P1-1's case on the platform it was
// about: a Kernel that started a long-running child is stopped, and the child
// must die with it. taskkill /T could not, because the parent was gone by the
// time it asked — the Job Object is what the Host holds either way.
func TestWindowsJobEndsTheSidecarsChildren(t *testing.T) {
	data := t.TempDir()
	w := newWatch()
	s := w.supervisor(helperSpawnIn(t, data, "GO_KERNEL_CHILD_HOLD_MS=60000"))
	defer s.Stop()
	s.Start()
	w.waitState(t, KernelRunning)
	child := waitChildPid(t, data)
	if !processAlive(child) {
		t.Fatalf("the sidecar's child never started: pid %d is gone", child)
	}
	s.Stop()
	waitGone(t, child)
}

// TestWindowsJobEndsChildrenAfterTheParentExitsFirst is P1-2's case: the sidecar
// crashes on its own, so there is no parent to name and no Stop to run. The job
// outlives it, and the generation's clean-up ends the tree before the next
// launch.
func TestWindowsJobEndsChildrenAfterTheParentExitsFirst(t *testing.T) {
	data := t.TempDir()
	w := newWatch()
	s := w.supervisor(helperSpawnIn(t, data,
		"GO_KERNEL_CHILD_HOLD_MS=60000", "GO_KERNEL_EXIT_MS=400"))
	// One attempt only: the sidecar must crash by itself, and a restart would
	// leave a second parent whose Stop is not the one being tested.
	s.Backoff = []time.Duration{time.Hour}
	defer s.Close()
	defer s.Stop()
	s.Start()
	w.waitState(t, KernelRunning)
	// The child exists before the crash: that is what is left behind.
	child := waitChildPid(t, data)
	if !processAlive(child) {
		t.Fatalf("the sidecar's child %d never ran", child)
	}
	// Stop is not called until the parent has exited on its own and its
	// generation has been retired — otherwise this would test an ordinary
	// Stop, which is the other test's case.
	w.waitState(t, KernelRestarting)
	waitGone(t, child)
}

// waitChildPid waits for the helper to write the pid of the process it started.
func waitChildPid(t *testing.T, data string) int {
	t.Helper()
	path := filepath.Join(data, childPidFile)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil {
			pid := 0
			if _, err := fmt.Sscanf(string(b), "%d", &pid); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the sidecar never wrote %s", path)
	return 0
}

// processAlive reports whether pid names a running process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(p)
	var code uint32
	if err := windows.GetExitCodeProcess(p, &code); err != nil {
		return false
	}
	// STILL_ACTIVE is 259: a process that exited keeps that code only by
	// returning it, which ping never does.
	return code == 259
}

// waitGone waits for a killed process to disappear: TerminateJobObject is
// asynchronous, and the process table is the only proof.
func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid %d is still alive after its tree was ended", pid)
}

// A launch that never becomes ready is retired on Windows too. TerminateProcess
// is not ignorable, so the bound is not what this proves; what it proves is that
// the failed generation reaches retire and the retry — the reviewer's case used
// an unkillable sidecar, which only Unix can build.
func TestWindowsFailedReadinessRetiresAndRetries(t *testing.T) {
	w := newWatch()
	s := w.supervisor(helperSpawn(t, "GO_KERNEL_NO_HANDSHAKE=1"))
	s.ReadyTimeout = 300 * time.Millisecond
	s.Backoff = []time.Duration{10 * time.Millisecond}
	defer s.Close()
	defer s.Stop()
	s.Start()
	restarting := w.waitState(t, KernelRestarting)
	if !strings.Contains(restarting.Reason, "handshake") {
		t.Fatalf("reason = %q, want the handshake's", restarting.Reason)
	}
}
