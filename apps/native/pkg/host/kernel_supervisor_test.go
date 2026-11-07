package host

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// TestKernelHelperProcess is the sidecar the Supervisor tests launch: the test
// binary itself, told by an environment variable to be a Kernel.
func TestKernelHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_KERNEL_HELPER") != "1" {
		t.Skip("not the helper")
	}
	token := os.Getenv(kernel.TokenEnv)
	if token == "" {
		fmt.Fprintln(os.Stderr, "kernel: DROI_KERNEL_TOKEN is not set")
		os.Exit(kernel.ExitNoToken)
	}
	port, data := 0, ""
	for i, arg := range os.Args {
		if i+1 >= len(os.Args) {
			break
		}
		switch arg {
		case "--port":
			port, _ = strconv.Atoi(os.Args[i+1])
		case "--data":
			data = os.Args[i+1]
		}
	}
	// --port 0 is the contract's own way of asking for a port: the helper
	// binds one, and the handshake names what it got.

	// The token must arrive in the environment only; refuse to start from argv
	// if a test ever puts it there.
	for _, arg := range os.Args {
		if strings.Contains(arg, token) {
			fmt.Fprintln(os.Stderr, "kernel: the token was in argv")
			os.Exit(3)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"version":"kernel-test-1","pi":"1.5.0"}`))
	})
	server := &http.Server{Handler: mux}
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "kernel: listen: %v\n", err)
		os.Exit(1)
	}
	port = listener.Addr().(*net.TCPAddr).Port
	if noise := os.Getenv("GO_KERNEL_NOISE"); noise != "" {
		// A compiled sidecar prints what its runtime says before its own
		// handshake; that line must not be taken for one.
		fmt.Println(noise)
	}
	if os.Getenv("GO_KERNEL_NO_HANDSHAKE") != "1" {
		fmt.Println(`{"port":` + strconv.Itoa(port) + `,"pid":` + strconv.Itoa(os.Getpid()) + `,"version":"kernel-test-1"}`)
	}
	if os.Getenv("GO_KERNEL_NO_HEALTH") == "1" {
		server = nil
	}
	if server != nil {
		go func() { _ = server.Serve(listener) }()
	}
	if noise := os.Getenv("GO_KERNEL_NOISE_BYTES"); noise != "" {
		// A sidecar that keeps printing after its handshake: whatever reads
		// its stdout must keep reading, or the pipe fills and it blocks.
		if n, err := strconv.Atoi(noise); err == nil && n > 0 {
			line := strings.Repeat("x", 200) + "\n"
			for written := 0; written < n; written += len(line) {
				if _, err := os.Stdout.WriteString(line); err != nil {
					fmt.Fprintf(os.Stderr, "kernel: stdout write: %v\n", err)
					os.Exit(4)
				}
			}
			if done := os.Getenv("GO_KERNEL_NOISE_DONE"); done != "" && data != "" {
				_ = os.WriteFile(filepath.Join(data, done), []byte("done"), 0o600)
			}
		}
	}
	if hold := os.Getenv("GO_KERNEL_CHILD_HOLD_MS"); hold != "" {
		// A process the sidecar starts — bash, or a background job — which
		// inherits its stderr and outlives it.
		if ms, err := strconv.Atoi(hold); err == nil && ms > 0 {
			child := exec.Command("sh", "-c", "sleep "+strconv.Itoa((ms+999)/1000))
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if err := child.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "kernel: child: %v\n", err)
			}
		}
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	if file := os.Getenv("GO_KERNEL_TOUCH"); file != "" {
		if data != "" {
			_ = os.WriteFile(filepath.Join(data, file), []byte("ran"), 0o600)
		}
	}
	// A helper without GO_KERNEL_EXIT_MS runs until it is stopped.
	var exitAfter <-chan time.Time
	if ms, err := strconv.Atoi(os.Getenv("GO_KERNEL_EXIT_MS")); err == nil && ms > 0 {
		exitAfter = time.After(time.Duration(ms) * time.Millisecond)
	}
	if os.Getenv("GO_KERNEL_IGNORE_SIGTERM") == "1" {
		// A sidecar stuck in storage.close(): it must be killed, not waited
		// for forever.
		for range signals {
		}
	}
	select {
	case <-signals:
		os.Exit(0)
	case <-exitAfter:
		fmt.Fprintln(os.Stderr, "kernel: I stopped on my own")
		os.Exit(1)
	}
}

// helperSpawn launches the test binary as the sidecar, with env on top.
func helperSpawn(t *testing.T, env ...string) func(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
	t.Helper()
	return func(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
		// The "--" stops the test binary's own flag parsing, so the contract's
		// arguments reach the helper untouched.
		cmd := exec.Command(os.Args[0], "-test.run=TestKernelHelperProcess", "--", "--port", strconv.Itoa(port), "--data", t.TempDir())
		cmd.Env = append(os.Environ(), append([]string{"GO_WANT_KERNEL_HELPER=1", kernel.TokenEnv + "=" + token}, env...)...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, nil, err
		}
		return cmd, stdout, nil
	}
}

// watchKernel records the states a Supervisor reports.
type watchKernel struct {
	states chan KernelState
	ready  chan readyEvent
}

type readyEvent struct {
	state KernelState
	token string
}

func newWatch() *watchKernel {
	return &watchKernel{states: make(chan KernelState, 64), ready: make(chan readyEvent, 8)}
}

func (w *watchKernel) supervisor(spawn func(int, string) (*exec.Cmd, io.ReadCloser, error)) *KernelSupervisor {
	return &KernelSupervisor{
		Spawn:   spawn,
		OnState: func(s KernelState) { w.states <- s },
		OnReady: func(s KernelState, token string) { w.ready <- readyEvent{s, token} },
		Backoff: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
		// The helper answers health at once; a long window only slows a
		// failure down.
		ReadyTimeout: 10 * time.Second,
		StableAfter:  10 * time.Second,
		StopGrace:    2 * time.Second,
		Log:          func(string) {},
	}
}

// waitState waits for the next state with status.
func (w *watchKernel) waitState(t *testing.T, status KernelStatus) KernelState {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case s := <-w.states:
			if s.Status == status {
				return s
			}
		case <-deadline:
			t.Fatalf("no %s state", status)
			return KernelState{}
		}
	}
}

// The Supervisor starts the sidecar, reads its handshake, waits for health and
// reports it running with the token a Client dials with.
func TestKernelSupervisorStartsTheSidecar(t *testing.T) {
	w := newWatch()
	s := w.supervisor(helperSpawn(t))
	defer s.Stop()
	s.Start()
	running := w.waitState(t, KernelRunning)
	if running.Port == 0 || running.PID == 0 {
		t.Fatalf("running state %+v", running)
	}
	if running.Version != "kernel-test-1" {
		t.Fatalf("version = %q", running.Version)
	}
	if got, want := running.RPCURL(), kernel.URL(running.Port); got != want {
		t.Fatalf("RPCURL() = %q, want %q", got, want)
	}
	select {
	case ready := <-w.ready:
		if len(ready.token) != 32 {
			t.Fatalf("token %q", ready.token)
		}
		if ready.state.Port != running.Port {
			t.Fatalf("ready port %d, running port %d", ready.state.Port, running.Port)
		}
		if s.Token() != ready.token {
			t.Fatal("Token() is not the token Kernel got")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnReady never fired")
	}
	if _, err := kernel.CheckHealth(t.Context(), running.Port); err != nil {
		t.Fatalf("health: %v", err)
	}
}

// A sidecar that exits is started again, and the reason it gives is the one
// the window shows.
func TestKernelSupervisorRestartsWithBackoffAndAReason(t *testing.T) {
	w := newWatch()
	s := w.supervisor(helperSpawn(t, "GO_KERNEL_EXIT_MS=300"))
	defer s.Stop()
	s.Start()
	first := w.waitState(t, KernelRunning)
	restarting := w.waitState(t, KernelRestarting)
	if restarting.Delay == 0 {
		t.Fatalf("restarting state %+v has no delay", restarting)
	}
	if !strings.Contains(restarting.Reason, "I stopped on my own") {
		t.Fatalf("reason = %q, want the sidecar's own line", restarting.Reason)
	}
	second := w.waitState(t, KernelRunning)
	if second.PID == first.PID {
		t.Fatalf("both attempts are pid %d", second.PID)
	}
	if second.Attempt < 2 {
		t.Fatalf("attempt = %d after a restart", second.Attempt)
	}
}

// Stop ends the sidecar, and the data directory is still there: Windows locks
// the SQLite file for about two seconds after the process is gone, so nothing
// in-process may clean it up.
func TestKernelSupervisorStopsAndKeepsTheDataDirectory(t *testing.T) {
	data := t.TempDir()
	w := newWatch()
	s := w.supervisor(func(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
		cmd := exec.Command(os.Args[0], "-test.run=TestKernelHelperProcess", "--", "--port", strconv.Itoa(port), "--data", data)
		cmd.Env = append(os.Environ(), "GO_WANT_KERNEL_HELPER=1", kernel.TokenEnv+"="+token, "GO_KERNEL_TOUCH=started.txt")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, nil, err
		}
		return cmd, stdout, nil
	})
	s.Start()
	w.waitState(t, KernelRunning)
	for deadline := time.Now().Add(5 * time.Second); ; {
		if _, err := os.Stat(filepath.Join(data, "started.txt")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the sidecar never wrote to its data directory")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Stop()
	if st := s.State(); st.Status != KernelStopped {
		t.Fatalf("state after Stop: %+v", st)
	}
	if entries, err := os.ReadDir(data); err != nil || len(entries) == 0 {
		t.Fatalf("the data directory is gone or empty: %v", err)
	}
	if s.Token() != "" {
		t.Fatal("Token() after Stop")
	}
	// A stopped Supervisor stays stopped: no restart follows.
	select {
	case st := <-w.states:
		if st.Status != KernelStopped {
			t.Fatalf("a state after Stop: %+v", st)
		}
	case <-time.After(200 * time.Millisecond):
	}
}

// Without a binary the Supervisor keeps retrying and says why, which is what
// the window shows instead of an endless start.
func TestKernelSupervisorReportsAMissingBinary(t *testing.T) {
	w := newWatch()
	s := w.supervisor(nil)
	s.Binary = filepath.Join(t.TempDir(), "no-such-kernel")
	s.Start()
	restarting := w.waitState(t, KernelRestarting)
	s.Stop()
	if !strings.Contains(restarting.Reason, "no kernel binary") {
		t.Fatalf("reason = %q", restarting.Reason)
	}
	if !restarting.KernelMissing() {
		t.Fatalf("KernelMissing() is false for %+v", restarting)
	}
}

// A sidecar that never prints its handshake is not reported running: an open
// port alone does not prove it can serve.
func TestKernelSupervisorNeedsAHandshakeAndHealth(t *testing.T) {
	w := newWatch()
	s := w.supervisor(helperSpawn(t, "GO_KERNEL_NO_HANDSHAKE=1"))
	s.ReadyTimeout = 500 * time.Millisecond
	s.Start()
	restarting := w.waitState(t, KernelRestarting)
	s.Stop()
	if !strings.Contains(restarting.Reason, "health") && !strings.Contains(restarting.Reason, "I stopped") {
		t.Fatalf("reason = %q", restarting.Reason)
	}
}

// A token is a fresh 32 hex characters per launch, and never repeats.
func TestLaunchTokensDiffer(t *testing.T) {
	first, err := LaunchToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := LaunchToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 || len(second) != 32 || first == second {
		t.Fatalf("tokens %q and %q", first, second)
	}
	if b, err := hex.DecodeString(first); err != nil || len(b) != 16 {
		t.Fatalf("token %q is not 32 hex characters: %v", first, err)
	}
}

// The handshake the fixtures' contract names decodes, so a change of shape is
// caught here and not in a launch.
func TestHandshakeShape(t *testing.T) {
	line := `{"port":51234,"pid":4121,"version":"kernel-1.5.0-1"}`
	var h kernel.Handshake
	if err := json.Unmarshal([]byte(line), &h); err != nil {
		t.Fatal(err)
	}
	if h.Port != 51234 || h.PID != 4121 || h.Version != "kernel-1.5.0-1" {
		t.Fatalf("handshake %+v", h)
	}
}

// A sidecar that ignores the stop signal is killed once the contract's window
// is over, and Stop still answers: a stuck storage.close() must not hold the
// app open.
func TestKernelSupervisorKillsASidecarThatIgnoresTheStopSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows has no SIGTERM: the stop is TerminateProcess, and there is
		// no window to outlast. That is why the contract needs a shutdown
		// method (shared/interfaces/kernel-protocol.md).
		t.Skip("no SIGTERM to ignore on Windows")
	}
	w := newWatch()
	s := w.supervisor(helperSpawn(t, "GO_KERNEL_IGNORE_SIGTERM=1"))
	s.StopGrace = 300 * time.Millisecond
	s.Start()
	running := w.waitState(t, KernelRunning)
	started := time.Now()
	s.Stop()
	elapsed := time.Since(started)
	if elapsed < s.StopGrace {
		t.Fatalf("Stop gave up after %s, before the %s window", elapsed, s.StopGrace)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Stop took %s with a %s window", elapsed, s.StopGrace)
	}
	if st := s.State(); st.Status != KernelStopped {
		t.Fatalf("state after Stop: %+v", st)
	}
	// The process is gone: a signal no longer reaches it.
	if process, err := os.FindProcess(running.PID); err == nil && process.Signal(syscall.Signal(0)) == nil {
		_ = process.Kill()
		t.Fatalf("pid %d is still alive after Stop", running.PID)
	}
}

// A sidecar whose runtime prints a line before the handshake still starts: the
// line is logged, the handshake after it is the one that counts.
func TestKernelSupervisorFindsTheHandshakeAfterOtherOutput(t *testing.T) {
	w := newWatch()
	var logged []string
	var mu sync.Mutex
	s := w.supervisor(helperSpawn(t, "GO_KERNEL_NOISE=[bun] warning: the runtime says hello"))
	s.Log = func(line string) {
		mu.Lock()
		logged = append(logged, line)
		mu.Unlock()
	}
	defer s.Stop()
	s.Start()
	running := w.waitState(t, KernelRunning)
	if running.Version != "kernel-test-1" {
		t.Fatalf("version = %q, want the handshake's", running.Version)
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, line := range logged {
		if strings.Contains(line, "the runtime says hello") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the line before the handshake was dropped; log %v", logged)
	}
}

// A sidecar that keeps writing after its handshake must not block: whatever
// reads its stdout has to keep draining it.
func TestKernelSupervisorKeepsDrainingStdoutAfterTheHandshake(t *testing.T) {
	data := t.TempDir()
	w := newWatch()
	s := w.supervisor(func(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
		cmd := exec.Command(os.Args[0], "-test.run=TestKernelHelperProcess", "--", "--port", strconv.Itoa(port), "--data", data)
		cmd.Env = append(os.Environ(), "GO_WANT_KERNEL_HELPER=1", kernel.TokenEnv+"="+token,
			"GO_KERNEL_NOISE_BYTES=2097152", "GO_KERNEL_NOISE_DONE=noise-done")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, nil, err
		}
		return cmd, stdout, nil
	})
	defer s.Stop()
	s.Start()
	w.waitState(t, KernelRunning)
	done := filepath.Join(data, "noise-done")
	for deadline := time.Now().Add(10 * time.Second); ; {
		if _, err := os.Stat(done); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the sidecar could not finish writing 2 MiB to stdout: nothing drains it after the handshake")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A process the sidecar starts inherits its stderr. Stop must not wait for
// that process: only the sidecar's own shutdown is what the window waits on.
func TestKernelSupervisorStopDoesNotWaitForAGrandchild(t *testing.T) {
	w := newWatch()
	s := w.supervisor(helperSpawn(t, "GO_KERNEL_CHILD_HOLD_MS=20000"))
	s.StopGrace = 500 * time.Millisecond
	s.Start()
	w.waitState(t, KernelRunning)
	// Let the sidecar start its child before it is stopped.
	time.Sleep(300 * time.Millisecond)
	started := time.Now()
	s.Stop()
	elapsed := time.Since(started)
	if elapsed > 5*time.Second {
		t.Fatalf("Stop took %s: it waited for a grandchild that holds the stderr pipe", elapsed)
	}
	if st := s.State(); st.Status != KernelStopped {
		t.Fatalf("state after Stop: %+v", st)
	}
}
