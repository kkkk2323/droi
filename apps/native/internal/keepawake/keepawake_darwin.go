package keepawake

import (
	"os"
	"os/exec"
	"strconv"
)

// command starts a program and is how start stops it; a test replaces it.
var command = func(name string, args ...string) (stop func(), err error) {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}, nil
}

// start runs caffeinate, which keeps the system from idle sleep, and ends
// with Droi (-w) should Droi quit without releasing it.
func start() (stop func()) {
	stop, err := command("/usr/bin/caffeinate", "-i", "-w", strconv.Itoa(os.Getpid()))
	if err != nil {
		return func() {}
	}
	return stop
}
