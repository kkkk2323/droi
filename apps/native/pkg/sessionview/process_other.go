//go:build !windows

package sessionview

import (
	"errors"
	"syscall"
)

// processAlive is whether process pid still runs on this computer.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
