package host

import (
	"context"
	"os/exec"
	"strconv"
	"time"
)

// prepareProcess leaves the sidecar's creation alone: Windows has no process
// groups to join, and the console window is already hidden by hideConsole.
//
// A sidecar's own processes are ended through its tree (taskkill /T), which
// needs the sidecar still listed: once it has exited, its children are
// orphans the Host can no longer name. Killing before waiting for the exit is
// what makes that work, and Stop does exactly that.
func prepareProcess(*exec.Cmd) {}

// signalProcess tells the sidecar to stop.
//
// Windows has no SIGTERM. A stop asks kernel.shutdown first (KernelSupervisor
// .AskShutdown), which is the orderly way and runs the sidecar's own close;
// this is what is left when that is not wired or fails: TerminateProcess,
// which skips storage.close() and leaves SQLite to recover from its
// write-ahead log on the next start.
func signalProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// killProcess ends the sidecar and, with it, the processes it started:
// taskkill /T walks the child tree, which Kill alone does not.
//
// It is bounded: taskkill can wait on a process that will not die, and a stop
// must answer anyway. After the timeout the sidecar itself is killed and the
// call returns; whatever is left of its tree is an orphan that no longer holds
// the Host's pipes, and the sidecar's own start-up check marks its process
// entry as exited.
func killProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	taskkill := "taskkill"
	if path, err := exec.LookPath("taskkill"); err == nil {
		taskkill = path
	}
	ctx, cancel := context.WithTimeout(context.Background(), graceTree)
	defer cancel()
	kill := exec.CommandContext(ctx, taskkill, "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	hideConsole(kill)
	_ = kill.Run()
	_ = cmd.Process.Kill()
}

// graceTree bounds how long taskkill may take before the stop gives up on the
// tree and kills only the sidecar.
const graceTree = 3 * time.Second
