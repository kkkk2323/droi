//go:build !windows

package workstream

import "os/exec"

func hideConsole(*exec.Cmd) {}
