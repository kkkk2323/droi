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

// TestFakeKernelLeavesNoBackgroundToolProcess is the leak that survived the first fix: the Kernel's process
// group was ended, but Pi starts each tool shell `detached`, in a group of its own, so a `sleep 100 &` the
// shell left behind was not in the Kernel's group at all.
func TestFakeKernelLeavesNoBackgroundToolProcess(t *testing.T) {
	for _, command := range []string{
		"sleep 100 & echo $! > %s",
		"nohup sleep 100 > /dev/null 2>&1 & echo $! > %s",
	} {
		pidfile := filepath.Join(t.TempDir(), "tool.pid")
		k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
			{Call: &fakekernel.Call{Name: "bash", Arguments: map[string]any{
				"command": fmt.Sprintf(command, pidfile),
			}}},
			{Text: "left it running"},
		}})
		c := k.Dial(t)
		id := k.CreateConversation(t, c)
		sub := k.Submit(t, c, id, "leave something running")
		k.WaitForReply(t, c, sub, "left it running", 60*time.Second)

		var pid int
		for deadline := time.Now().Add(10 * time.Second); pid == 0; {
			data, _ := os.ReadFile(pidfile)
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			if time.Now().After(deadline) {
				t.Fatalf("the tool never wrote its pid: %s\n%s", command, k.Logs())
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

		k.Close()
		for deadline := time.Now().Add(5 * time.Second); ; {
			if err := syscall.Kill(pid, 0); err != nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s left pid %d running after Close", command, pid)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// TestCloseWaitsForTheRequestedShutdown is the other half: a signal sent right after `kernel.shutdown` raced
// the Kernel's own cleanup, so a tool's background process could outlive a shutdown that was already running.
func TestCloseWaitsForTheRequestedShutdown(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Call: &fakekernel.Call{Name: "bash", Arguments: map[string]any{
			"command": fmt.Sprintf("sleep 100 & echo $! > %s", pidfile),
		}}},
		{Text: "started"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	k.Ask(t, c, id, "start a background process", "started", 30*time.Second)
	pid := readPID(t, pidfile)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	k.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("a background process %d outlived Close: the shutdown it asked for was cut short", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// readPID waits for a file a tool wrote and returns the number in it.
func readPID(t testing.TB, path string) int {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; {
		data, err := os.ReadFile(path)
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil && pid > 0 {
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no pid in %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
