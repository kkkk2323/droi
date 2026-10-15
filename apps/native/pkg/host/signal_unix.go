//go:build !windows

package host

import (
	"os"
	"syscall"
)

func terminateSignal() os.Signal { return syscall.SIGTERM }
