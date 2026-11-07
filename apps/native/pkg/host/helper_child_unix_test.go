//go:build !windows

package host

import (
	"os/exec"
	"strconv"
)

// holdChild is a process the sidecar starts and does not wait for: it holds the
// sidecar's stdout and outlives it, which is what a process tree has to end.
func holdChild(ms int) *exec.Cmd {
	return exec.Command("sh", "-c", "sleep "+strconv.Itoa((ms+999)/1000))
}
