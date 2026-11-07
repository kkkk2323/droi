//go:build !windows

package host

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// prepareProcess puts the sidecar in a process group of its own, so the
// processes it starts — a shell, a background job — are signalled with it and
// cannot outlive it.
func prepareProcess(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// signalProcess asks the sidecar's whole group to stop, which is the SIGTERM
// the contract's shutdown expects. A sidecar that already went takes the error
// quietly.
func signalProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
}

// newProcessTree is the sidecar's process group: a group of its own, named by
// the sidecar's pid, which outlives the sidecar and holds what it started.
func newProcessTree(cmd *exec.Cmd) (processTree, error) {
	if cmd.Process == nil {
		return nil, errors.New("no process")
	}
	return &groupTree{pid: cmd.Process.Pid}, nil
}

// groupTree is one generation's process group. It holds no handle, so killing
// it twice costs a signal and nothing else; endTree still makes it once.
type groupTree struct{ pid int }

// kill ends the group and answers once no member is left, bounded by
// graceGroup.
//
// Only ESRCH means "gone": EPERM says a process is there that this user may not
// signal, which is not a reason to claim the tree ended. The bound is what a
// process in an uninterruptible call costs.
func (g *groupTree) kill() {
	_ = syscall.Kill(-g.pid, syscall.SIGKILL)
	for deadline := time.Now().Add(graceGroup); time.Now().Before(deadline); {
		// Ask again while anyone is left: a process can enter the group between
		// the signal and this check — a shell that was mid-fork — and one
		// SIGKILL is then not enough.
		_ = syscall.Kill(-g.pid, syscall.SIGKILL)
		err := syscall.Kill(-g.pid, 0)
		if err == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		// EPERM and anything else: not proof of absence, but not a reason to
		// spin either.
		return
	}
}

// close releases nothing: a process group has no handle.
func (g *groupTree) close() {}

// graceGroup bounds how long a kill waits for the group to disappear. A
// process that was just killed is reaped in milliseconds; one stuck in an
// uninterruptible call is not, and a stop must not wait for it.
const graceGroup = time.Second
