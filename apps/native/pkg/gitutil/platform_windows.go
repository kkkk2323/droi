package gitutil

import (
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// candidates are Git for Windows' usual places: for every user, for every user on a 32-bit install, and for this
// user alone.
func candidates(env func(string) string) []string {
	var out []string
	for _, root := range []string{env("ProgramFiles"), env("ProgramFiles(x86)"), env("ProgramW6432")} {
		if root != "" {
			out = append(out, filepath.Join(root, "Git", "cmd", "git.exe"))
		}
	}
	if local := env("LOCALAPPDATA"); local != "" {
		out = append(out, filepath.Join(local, "Programs", "Git", "cmd", "git.exe"))
	}
	return out
}

// hideConsole keeps git from opening a console window: the app has none for it to borrow.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
