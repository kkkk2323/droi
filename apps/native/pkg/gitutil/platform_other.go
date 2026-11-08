//go:build !windows

package gitutil

import "os/exec"

// candidates are the places git may be when PATH does not have it: none beyond PATH outside Windows.
func candidates(func(string) string) []string { return nil }

func hideConsole(*exec.Cmd) {}
