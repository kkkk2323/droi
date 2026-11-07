//go:build windows

package host

import (
	"os/exec"
	"strconv"
)

// holdChild is a process the sidecar starts and does not wait for: it holds the
// sidecar's stdout and outlives it, which is what a process tree has to end.
//
// ping is the sleep that ships with Windows; -n counts pings, not seconds.
func holdChild(ms int) *exec.Cmd {
	return exec.Command("cmd", "/c", "ping -n "+strconv.Itoa((ms+999)/1000+1)+" 127.0.0.1")
}
