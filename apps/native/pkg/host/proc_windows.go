package host

import (
	"os/exec"
	"strconv"
)

// prepareProcess leaves the sidecar's creation alone: Windows has no process
// groups to join, and the console window is already hidden by hideConsole.
//
// A Windows sidecar starts its own processes the same way (no job object), so
// killing the sidecar does not kill them; they hold no pipe the Host waits on
// once WaitDelay has passed, and the sidecar's own start-up check marks an
// orphan's process entry as exited.
func prepareProcess(*exec.Cmd) {}

// signalProcess tells the sidecar to stop.
//
// Windows has no SIGTERM: Process.Kill is TerminateProcess, which ends it
// without harness.close() and storage.close(). Until Kernel answers a
// shutdown call (the contract has none yet: shared/interfaces/kernel-protocol.md
// asks for one), that is what a stop can do here; the SQLite file recovers
// from its write-ahead log on the next start.
func signalProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// killProcess ends the sidecar and, with it, the console processes it started:
// taskkill /T walks the child tree, which Kill alone does not.
func killProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	taskkill := "taskkill"
	if path, err := exec.LookPath("taskkill"); err == nil {
		taskkill = path
	}
	kill := exec.Command(taskkill, "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	kill.SysProcAttr = nil
	hideConsole(kill)
	_ = kill.Run()
	_ = cmd.Process.Kill()
}
