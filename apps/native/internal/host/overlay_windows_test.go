package host

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// The Daemon runs a hook as `cmd.exe /d /s /c <command>`, verbatim.
func TestTheHookCommandRunsInCmd(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Droi & Co's", "memory")
	cmdExe := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	// A second cmd.exe prints the variable the command set for it.
	line := strings.TrimSuffix(hookCommandLine("windows", dir, cmdExe), MemoryHookCommand) + "/d /c set DROI_MEMORY_DIR"
	c := exec.Command(cmdExe)
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: `"` + cmdExe + `" /d /s /c ` + line}
	out, err := c.Output()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "DROI_MEMORY_DIR="+dir {
		t.Errorf("cmd.exe saw %q", got)
	}
}

func TestTheDaemonGetsNoConsoleWindow(t *testing.T) {
	c := exec.Command("cmd.exe")
	hideConsole(c)
	if c.SysProcAttr == nil || !c.SysProcAttr.HideWindow || c.SysProcAttr.CreationFlags&0x08000000 == 0 {
		t.Fatalf("%+v", c.SysProcAttr)
	}
}
