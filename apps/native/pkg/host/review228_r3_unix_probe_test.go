//go:build !windows

package host

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

func TestReview228R3CrashDoesNotOrphanOldProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild-pid")
	w := newWatch()
	var read *os.File
	s := w.supervisor(func(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
		cmd := exec.Command("sh", "-c",
			`trap '' TERM; sleep 60 & echo $! > "$1"; trap - TERM; shift; exec "$@"`,
			"r3", pidFile, os.Args[0], "-test.run=TestKernelHelperProcess", "--", "--port", strconv.Itoa(port))
		cmd.Env = append(os.Environ(), "GO_WANT_KERNEL_HELPER=1", "GO_KERNEL_EXIT_MS=50", kernel.TokenEnv+"="+token)
		prepareProcess(cmd)
		r, write, err := os.Pipe()
		if err != nil {
			return nil, nil, err
		}
		read = r
		cmd.Stdout = write
		return cmd, &stdioPair{read: r, write: write}, nil
	})
	s.Backoff = []time.Duration{time.Hour}
	s.Start()
	w.waitState(t, KernelRestarting)
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	defer read.Close()
	s.Stop()
	// The crash's generation is retired before the next one starts, so the
	// grandchild goes with it — after the reap, not the signal.
	waitGoneUnix(t, pid)
}

func TestReview228R3DetachedStdoutHolderHelper(t *testing.T) {
	if os.Getenv("REVIEW228_DETACHED_HOLDER") != "1" {
		t.Skip("subprocess helper")
	}
	child := exec.Command("sleep", "60")
	child.Stdout = os.Stdout
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("REVIEW228_PIDFILE"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	err := syscall.Exec(os.Args[0], []string{os.Args[0], "-test.run=TestKernelHelperProcess", "--", "--port", "0"}, os.Environ())
	t.Fatalf("exec helper: %v", err)
}

func TestReview228R3StopRetiresStdoutPumpWithDetachedHolder(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "detached-pid")
	before := runtime.NumGoroutine()
	w := newWatch()
	var read *os.File
	s := w.supervisor(func(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
		cmd := exec.Command(os.Args[0], "-test.run=TestReview228R3DetachedStdoutHolderHelper")
		cmd.Env = append(os.Environ(), "REVIEW228_DETACHED_HOLDER=1", "REVIEW228_PIDFILE="+pidFile,
			"GO_WANT_KERNEL_HELPER=1", kernel.TokenEnv+"="+token)
		prepareProcess(cmd)
		r, write, err := os.Pipe()
		if err != nil {
			return nil, nil, err
		}
		read = r
		cmd.Stdout = write
		return cmd, &stdioPair{read: r, write: write}, nil
	})
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
	defer syscall.Kill(pid, syscall.SIGKILL)
	defer read.Close()
	s.mu.Lock()
	ch := s.child
	s.mu.Unlock()
	started := time.Now()
	s.Stop()
	t.Logf("Stop returned in %s; goroutines before=%d after=%d", time.Since(started), before, runtime.NumGoroutine())
	select {
	case <-ch.pumped:
	case <-time.After(2500 * time.Millisecond):
		stack := make([]byte, 1<<20)
		stack = stack[:runtime.Stack(stack, true)]
		t.Logf("still has pumpStdout stack=%t", strings.Contains(string(stack), "(*KernelSupervisor).pumpStdout"))
		t.Fatal("Stop and bounded launch-drain wait ended, but detached stdout holder keeps pumpStdout alive")
	}
}
