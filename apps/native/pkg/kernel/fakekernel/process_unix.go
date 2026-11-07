//go:build !windows

package fakekernel

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// processTree is everything a test's Kernel started, so a test leaves no process behind.
//
// This is a minimal copy of what `apps/native/pkg/host` does for the real sidecar (its `proc_unix.go` and
// `proc_windows.go`): a process group on Unix, a Job Object on Windows. When that lands here — PR #228 —
// this file should be deleted and the package should use `host`'s version, so there is one implementation of
// "stop the tree" in the repository rather than two that can drift.
type processTree interface {
	kill()
	close()
}

// prepareProcess puts the Kernel in a process group of its own, so every tool it starts joins that group
// and is signalled with it. Pi starts a shell detached from the Kernel's stdin; without this, killing the
// Kernel's pid leaves the shell — and a `sleep 60` it began — running.
func prepareProcess(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func newProcessTree(cmd *exec.Cmd) (processTree, error) {
	if cmd.Process == nil {
		return nil, errors.New("fakekernel: no process to own")
	}
	return &groupTree{pid: cmd.Process.Pid}, nil
}

// groupTree is one Kernel's process group, named by the Kernel's pid.
type groupTree struct{ pid int }

// kill ends the group and returns once no member is left, bounded by graceGroup.
//
// It signals repeatedly: a shell mid-fork can enter the group between the signal and the check, and one
// SIGKILL is then not enough. Only ESRCH counts as "gone" — EPERM says a process is there that this user may
// not signal, which is not a reason to claim the tree ended.
func (g *groupTree) kill() {
	_ = syscall.Kill(-g.pid, syscall.SIGKILL)
	for deadline := time.Now().Add(graceGroup); time.Now().Before(deadline); {
		_ = syscall.Kill(-g.pid, syscall.SIGKILL)
		err := syscall.Kill(-g.pid, 0)
		if err == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		return
	}
}

func (g *groupTree) close() {}

// graceGroup bounds how long a kill waits for a group to disappear. A just-killed process is reaped in
// milliseconds; one stuck in an uninterruptible call is not, and a test must not wait for it.
const graceGroup = time.Second
