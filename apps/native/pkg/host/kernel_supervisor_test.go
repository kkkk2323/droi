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
	"sync/atomic"
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
	var healthDelay atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if delay := healthDelay.Load(); delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
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
	if slow := os.Getenv("GO_KERNEL_SLOW_HEALTH_MS"); slow != "" {
		// Answer health late, so a test can stop the sidecar while the
		// launcher is still waiting for it.
		if ms, err := strconv.Atoi(slow); err == nil && ms > 0 {
			healthDelay.Store(int64(ms))
		}
	}
	if long := os.Getenv("GO_KERNEL_LONG_LINE"); long != "" {
		// A sidecar that prints one line no reader can hold whole. The tail
		// of it is what explains the death that follows.
		if n, err := strconv.Atoi(long); err == nil && n > 0 {
			// Only stdout: the failure's reason has one writer, so the test
			// does not depend on which of two readers gets there first.
			_, _ = os.Stdout.WriteString(strings.Repeat("p", n) + "TAIL-MARKER\n")
			os.Exit(1)
		}
	}
	if chatty := os.Getenv("GO_KERNEL_CHATTY_LINE"); chatty != "" {
		// A long, but not too long, line: the log must cap what it keeps.
		if n, err := strconv.Atoi(chatty); err == nil && n > 0 {
			fmt.Println(strings.Repeat("c", n) + "END-OF-CHATTY")
		}
	}
	if hold := os.Getenv("GO_KERNEL_CHILD_HOLD_MS"); hold != "" {
		// A process the sidecar starts — a shell, or a background job — which
		// inherits its stdout and stderr and outlives it.
		if ms, err := strconv.Atoi(hold); err == nil && ms > 0 {
			child := holdChild(ms)
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if err := child.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "kernel: child: %v\n", err)
			} else if data != "" {
				// Its pid is what a test checks for after the tree is ended.
				_ = os.WriteFile(filepath.Join(data, childPidFile),
					[]byte(strconv.Itoa(child.Process.Pid)), 0o600)
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

// childPidFile is where the helper writes the pid of the child it started.
const childPidFile = "child.pid"

// selfExe is the test binary's own path, absolute. os.Args[0] can be a bare
// name when the binary was started from its own directory, and Windows refuses
// to run a program found relative to the current directory.
func selfExe(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return exe
}

// helperSpawn launches the test binary as the sidecar, with env on top.
func helperSpawn(t *testing.T, env ...string) func(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
	t.Helper()
	return helperSpawnIn(t, t.TempDir(), env...)
}

// helperSpawnIn is helperSpawn with --data named by the test, so it can read
// what the sidecar wrote there.
func helperSpawnIn(t *testing.T, data string, env ...string) func(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
	t.Helper()
	exe := selfExe(t)
	return func(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
		// The "--" stops the test binary's own flag parsing, so the contract's
		// arguments reach the helper untouched.
		cmd := exec.Command(exe, "-test.run=TestKernelHelperProcess", "--", "--port", strconv.Itoa(port), "--data", data)
		cmd.Env = append(os.Environ(), append([]string{"GO_WANT_KERNEL_HELPER=1", kernel.TokenEnv + "=" + token}, env...)...)
		read, write, err := os.Pipe()
		if err != nil {
			return nil, nil, err
		}
		cmd.Stdout = write
		return cmd, &stdioPair{read: read, write: write}, nil
	}
}

// watchKernel records the states a Supervisor reports.
type watchKernel struct {
	states chan KernelState
}

func newWatch() *watchKernel {
	return &watchKernel{states: make(chan KernelState, 64)}
}

func (w *watchKernel) supervisor(spawn func(int, string) (*exec.Cmd, io.ReadCloser, error)) *KernelSupervisor {
	return &KernelSupervisor{
		Spawn:   spawn,
		OnState: func(s KernelState) { w.states <- s },
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
	if running.Generation == 0 {
		t.Fatalf("running state carries no generation: %+v", running)
	}
	if token := s.Token(); len(token) != 32 {
		t.Fatalf("Token() = %q, want the 32 hex characters Kernel got", token)
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
	exe := selfExe(t)
	w := newWatch()
	s := w.supervisor(func(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
		cmd := exec.Command(exe, "-test.run=TestKernelHelperProcess", "--", "--port", strconv.Itoa(port), "--data", data)
		cmd.Env = append(os.Environ(), "GO_WANT_KERNEL_HELPER=1", kernel.TokenEnv+"="+token, "GO_KERNEL_TOUCH=started.txt")
		read, write, err := os.Pipe()
		if err != nil {
			return nil, nil, err
		}
		cmd.Stdout = write
		return cmd, &stdioPair{read: read, write: write}, nil
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
	if !strings.Contains(restarting.Reason, "handshake") && !strings.Contains(restarting.Reason, "health") {
		t.Fatalf("reason = %q, want the handshake's or the health check's", restarting.Reason)
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
	// The process is gone: a signal no longer reaches it — after the reap,
	// which is not the instant the kill returns.
	waitGoneUnix(t, running.PID)
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
	exe := selfExe(t)
	w := newWatch()
	s := w.supervisor(func(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
		cmd := exec.Command(exe, "-test.run=TestKernelHelperProcess", "--", "--port", strconv.Itoa(port), "--data", data)
		cmd.Env = append(os.Environ(), "GO_WANT_KERNEL_HELPER=1", kernel.TokenEnv+"="+token,
			"GO_KERNEL_NOISE_BYTES=2097152", "GO_KERNEL_NOISE_DONE=noise-done")
		read, write, err := os.Pipe()
		if err != nil {
			return nil, nil, err
		}
		cmd.Stdout = write
		return cmd, &stdioPair{read: read, write: write}, nil
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

// No handshake must fail in ReadyTimeout, not twice it: the contract's launch
// is --port 0, so without a handshake there is no port to ask health on.
func TestKernelSupervisorFailsOnceWithoutAHandshake(t *testing.T) {
	w := newWatch()
	s := w.supervisor(helperSpawn(t, "GO_KERNEL_NO_HANDSHAKE=1"))
	s.ReadyTimeout = 500 * time.Millisecond
	started := time.Now()
	s.Start()
	restarting := w.waitState(t, KernelRestarting)
	s.Stop()
	elapsed := time.Since(started)
	// One window, not two: the failing launch must not go on to ask health on a
	// port no handshake named. The slack is the signal to the sidecar: a
	// sidecar that exits takes milliseconds, except under the race detector,
	// where its exit takes about a second.
	budget := 1500 * time.Millisecond
	if raceDetector {
		budget = 4 * time.Second
	}
	if elapsed > budget {
		t.Fatalf("a handshake-less launch took %s to fail with a %s timeout, so it waited twice", elapsed, s.ReadyTimeout)
	}
	if !strings.Contains(restarting.Reason, "handshake") {
		t.Fatalf("reason = %q, want the handshake's", restarting.Reason)
	}
}

// A line longer than the reader may hold is kept by its tail: that is where
// the sidecar's last word is, and it is what the window shows when the launch
// fails.
func TestKernelSupervisorKeepsTheTailOfALongLine(t *testing.T) {
	w := newWatch()
	s := w.supervisor(helperSpawn(t, "GO_KERNEL_LONG_LINE=2097152"))
	defer s.Stop()
	s.Start()
	restarting := w.waitState(t, KernelRestarting)
	if !strings.Contains(restarting.Reason, "TAIL-MARKER") {
		t.Fatalf("reason = %q, want the tail of the long line", tailOf(restarting.Reason, 300))
	}
}

// What the log keeps of one line is bounded, and says how much it left out.
func TestKernelSupervisorCapsALoggedLine(t *testing.T) {
	w := newWatch()
	var mu sync.Mutex
	var lines []string
	s := w.supervisor(helperSpawn(t, "GO_KERNEL_CHATTY_LINE=10240"))
	s.Log = func(line string) {
		mu.Lock()
		lines = append(lines, line)
		mu.Unlock()
	}
	defer s.Stop()
	s.Start()
	w.waitState(t, KernelRunning)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		found := false
		for _, line := range lines {
			if strings.Contains(line, "END-OF-CHATTY") {
				found = true
			}
		}
		mu.Unlock()
		if found {
			mu.Lock()
			for _, line := range lines {
				if strings.Contains(line, "END-OF-CHATTY") {
					if len(line) > 5<<10 {
						mu.Unlock()
						t.Fatalf("the log kept %d bytes of one line", len(line))
					}
					if !strings.Contains(line, "more bytes") {
						mu.Unlock()
						t.Fatalf("the log truncated without saying so: %q", line)
					}
				}
			}
			mu.Unlock()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("the long line never reached the log: %v", tailOf(strings.Join(lines, "\n"), 400))
}

// A slow OnState callback must not deadlock the Supervisor: it runs on a
// goroutine of its own, so Start and Stop never wait for it.
func TestKernelSupervisorSurvivesASlowOnStateCallback(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	s := &KernelSupervisor{
		Spawn:   helperSpawn(t),
		OnState: func(KernelState) { <-block },
		Backoff: []time.Duration{10 * time.Millisecond},
	}
	started := make(chan struct{})
	go func() {
		s.Start()
		close(started)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Start blocked on a slow OnState")
	}
	stopped := make(chan struct{})
	go func() {
		s.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop blocked on a slow OnState")
	}
}

// A sidecar a Stop already stopped must not be published as running. The case
// that matters is a stop that lands while the health check is still running:
// the launch answers after it, and its answer is stale.
func TestKernelSupervisorDoesNotAnnounceAStoppedSidecar(t *testing.T) {
	w := newWatch()
	s := w.supervisor(helperSpawn(t, "GO_KERNEL_SLOW_HEALTH_MS=1500"))
	var announced atomic.Int32
	s.OnState = func(st KernelState) {
		if st.Status == KernelRunning {
			announced.Add(1)
		}
		w.states <- st
	}
	s.Start()
	// Stop while the launcher is still asking health.
	time.Sleep(300 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("Stop never returned")
	}
	time.Sleep(2 * time.Second)
	if got := announced.Load(); got != 0 {
		t.Fatalf("a stopped sidecar was published running %d times", got)
	}
	if st := s.State(); st.Status != KernelStopped {
		t.Fatalf("state after Stop: %+v", st)
	}
}

// tailOf is the end of s, for a failure message that stays readable.
func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// A Supervisor that is stopped and started again must publish states again: a
// publisher that is never rebuilt leaves OnState silent for the rest of the
// process's life while the state keeps changing.
func TestOnStateSurvivesStopAndStart(t *testing.T) {
	w := newWatch()
	s := w.supervisor(nil)
	s.Binary = filepath.Join(t.TempDir(), "no-such-kernel")
	s.Backoff = []time.Duration{5 * time.Millisecond}
	s.Start()
	w.waitState(t, KernelRestarting)
	s.Stop()
	// Drain whatever the first run left.
	for len(w.states) > 0 {
		<-w.states
	}
	s.Start()
	defer s.Stop()
	restarting := w.waitState(t, KernelRestarting)
	if restarting.Status != KernelRestarting {
		t.Fatalf("state after a restart: %+v", restarting)
	}
}

// A Supervisor that was closed publishes nothing more: Close ends the one
// publisher for good, and a later state change must not start a second one that
// nothing would ever end.
func TestAClosedSupervisorStartsNoPublisher(t *testing.T) {
	var calls atomic.Int32
	s := &KernelSupervisor{OnState: func(KernelState) { calls.Add(1) }}
	s.mu.Lock()
	s.setLocked(KernelState{Status: KernelStarting})
	s.mu.Unlock()
	closeDeadline := time.Now().Add(5 * time.Second)
	for calls.Load() == 0 && time.Now().Before(closeDeadline) {
		time.Sleep(time.Millisecond)
	}
	s.Close()
	// Close hands the current state over as it leaves; that one is not a state
	// published after closing.
	time.Sleep(50 * time.Millisecond)
	before := calls.Load()
	s.Stop()
	s.mu.Lock()
	s.setLocked(KernelState{Status: KernelRunning})
	s.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != before {
		t.Fatalf("a closed Supervisor published %d more states", got-before)
	}
}
