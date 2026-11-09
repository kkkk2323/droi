//go:build !windows

package host

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// pathMarker brackets PATH in the shell's output: an interactive shell's rc files may print anything around it.
const pathMarker = "__DROI_LOGIN_PATH__"

// loginPathTimeout bounds the shell: an rc file that waits on something must not keep the Kernel from starting.
const loginPathTimeout = 5 * time.Second

// loginPath is PATH as the person's shell sets it, in front of current with what only current has after it; current
// alone when the shell does not answer. A Droi opened from the Finder or the Dock gets launchd's PATH
// (/usr/bin:/bin:/usr/sbin:/sbin), without Homebrew, bun or ~/.local/bin, and so did every command the Kernel ran.
// The shell is interactive and a login one, as a terminal's is, so both .zprofile and .zshrc are read.
func loginPath(shell, current string) string {
	if shell == "" {
		shell = "/bin/sh"
		if runtime.GOOS == "darwin" {
			shell = "/bin/zsh"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginPathTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-i", "-l", "-c", `printf '%s%s%s' "$1" "$PATH" "$1"`, filepath.Base(shell), pathMarker)
	// A child the rc files left running keeps the pipe open; the timeout ends the wait for it too.
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return current
	}
	parts := bytes.Split(out, []byte(pathMarker))
	if len(parts) < 3 {
		return current
	}
	return joinPath(string(parts[len(parts)-2]), current)
}

// joinPath is first's entries, then current's that first does not have, each once.
func joinPath(first, current string) string {
	seen := map[string]bool{}
	var out []string
	for _, dir := range append(strings.Split(first, ":"), strings.Split(current, ":")...) {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return strings.Join(out, ":")
}
