//go:build !windows

package host

import (
	"os/exec"
	"syscall"
	"time"
)

// prepareProcess puts the sidecar in a process group of its own, so the
// processes it starts — a shell, a background job — can be signalled with it
// and cannot outlive it.
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

// killProcess ends the sidecar's whole group and answers once the group is
// gone.
//
// The group, not the process: a shell or a background process the sidecar
// started keeps its own life, and one that ignores the signal outlives its
// parent. A group whose leader has exited is still there to kill, which is the
// case a stop that gave up on waiting reaches.
//
// It waits for the group to disappear rather than firing and returning: the
// window says "stopped", and a background process still writing is the thing
// that makes that untrue. A group that will not go within graceGroup is left to
// the operating system, and the wait is bounded so a stop cannot hang on it.
func killProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
	for deadline := time.Now().Add(graceGroup); time.Now().Before(deadline); {
		// Signal 0 asks whether any process of the group is left.
		if err := syscall.Kill(-pid, 0); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// graceGroup bounds how long a kill waits for the group to disappear. A
// process that was just killed is reaped in milliseconds; one stuck in an
// uninterruptible call is not, and the stop must not wait for it.
const graceGroup = time.Second
