//go:build !windows

package host

import (
	"os/exec"
	"syscall"
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

// killProcess ends the sidecar's whole group, which is what a stop that ran out
// of time must do: the grandchildren holding its pipes would otherwise stay.
func killProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
}
