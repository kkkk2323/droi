//go:build !windows

package fakekernel_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
)

// TestFakeKernelLeavesNoToolProcess is the leak the first version had: the Kernel was killed and the shell it
// started — a `sleep 60` — kept running.
//
// POSIX only because the check is `kill(pid, 0)`: Windows names a process differently, and its job object is
// covered by the implementation rather than by this probe.
func TestFakeKernelLeavesNoToolProcess(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "tool.pid")
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Call: &fakekernel.Call{Name: "bash", Arguments: map[string]any{
			"command": fmt.Sprintf("echo $$ > %q; exec /bin/sleep 60", pidfile),
		}}},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	k.Submit(t, c, id, "start something long")

	var pid int
	for deadline := time.Now().Add(10 * time.Second); pid == 0; {
		data, _ := os.ReadFile(pidfile)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		if time.Now().After(deadline) {
			t.Fatalf("the tool never started\n%s", k.Logs())
		}
		time.Sleep(10 * time.Millisecond)
	}
	// This test owns the process it checks, and always ends it.
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	k.Close()
	for deadline := time.Now().Add(5 * time.Second); ; {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Close left the tool's process %d running", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
