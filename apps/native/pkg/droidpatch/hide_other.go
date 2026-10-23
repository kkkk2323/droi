//go:build !windows

package droidpatch

import "os/exec"

func hideConsole(*exec.Cmd) {}
