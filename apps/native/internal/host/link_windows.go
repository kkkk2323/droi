package host

import (
	"os"
	"os/exec"
)

// link makes a symbolic link where Windows allows one (Developer Mode or an
// elevated process), else a junction for a folder and a hard link for a
// file, which need no privilege.
func link(target, at string, dir bool) error {
	if os.Symlink(target, at) == nil {
		return nil
	}
	if !dir {
		return os.Link(target, at)
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", at, target)
	hideConsole(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return hostError("mklink /J: " + string(out))
	}
	return nil
}
