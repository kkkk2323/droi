//go:build !windows

package host

import "os/exec"

func hideConsole(*exec.Cmd) {}
