//go:build !windows

package droidhook

import "os/exec"

func hideConsole(*exec.Cmd) {}
