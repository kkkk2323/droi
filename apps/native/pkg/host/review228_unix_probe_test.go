//go:build !windows

package host

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

func TestReview228StopKillsTermIgnoringGrandchild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child-pid")
	w := newWatch()
	s := w.supervisor(func(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
		// The child inherits ignored SIGTERM. The parent then execs the usual
		// helper, whose signal.Notify handler exits on SIGTERM.
		cmd := exec.Command("sh", "-c",
			`trap '' TERM; sleep 30 & echo $! > "$1"; trap - TERM; shift; exec "$@"`,
			"review-228", pidFile, os.Args[0], "-test.run=TestKernelHelperProcess",
			"--", "--port", strconv.Itoa(port))
		cmd.Env = append(os.Environ(), "GO_WANT_KERNEL_HELPER=1", kernel.TokenEnv+"="+token)
		prepareProcess(cmd)
		stdout, err := cmd.StdoutPipe()
		return cmd, stdout, err
	})
	s.StopGrace = 4 * time.Second
	defer s.Stop()
	s.Start()
	w.waitState(t, KernelRunning)
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	child, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Kill()
	s.Stop()
	if err := child.Signal(syscall.Signal(0)); err == nil {
		// What is left, if anything: a zombie is not a running process, and
		// only its parent can reap it.
		out, _ := exec.Command("ps", "-o", "pid,ppid,stat,pgid,command", "-p", strconv.Itoa(pid)).Output()
		t.Fatalf("SIGTERM-ignoring grandchild pid=%d answers signal 0 after Stop returned; StopGrace=%s > WaitDelay=%s\n%s", pid, s.StopGrace, waitDelay, out)
	}
}
