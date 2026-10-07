//go:build !windows

package main

import "os/exec"

// playFile plays a sound file to its end.
func playFile(path string) { _ = exec.Command("/usr/bin/afplay", path).Run() }
