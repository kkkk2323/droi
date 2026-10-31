//go:build windows

package host

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// TestAnchorHelper is the anchor when the test below runs this binary as one.
func TestAnchorHelper(t *testing.T) {
	if os.Getenv("DROI_ANCHOR_HELPER") != "1" {
		t.Skip("run as the anchor only")
	}
	os.Exit(AnchorMain())
}

// Once the Host ends the anchor, the check droid's parent PID poll makes
// (process.kill(pid, 0): open the process, read its exit code) finds it gone.
func TestTheDaemonSeesItsAnchorGo(t *testing.T) {
	t.Setenv("DROI_ANCHOR_HELPER", "1")
	a, err := startAnchor(os.Args[0], "-test.run=^TestAnchorHelper$")
	if err != nil {
		t.Fatal(err)
	}
	pid := uint32(a.cmd.Process.Pid)
	if !running(pid) {
		t.Fatal("the anchor is not running")
	}
	a.end()
	if running(pid) {
		t.Fatal("the anchor still runs once its input closed")
	}
}

func running(pid uint32) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}
