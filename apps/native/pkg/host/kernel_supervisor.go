package host

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// KernelStatus is where the Kernel sidecar stands.
type KernelStatus string

const (
	KernelStopped    KernelStatus = "stopped"
	KernelStarting   KernelStatus = "starting"
	KernelRunning    KernelStatus = "running"
	KernelRestarting KernelStatus = "restarting"
)

// KernelState is the sidecar's state, as Settings and the window show it.
type KernelState struct {
	Status  KernelStatus
	Port    int
	PID     int
	Attempt int
	Delay   time.Duration
	// Reason is why the last attempt failed, while it is retried.
	Reason  string
	Version string
}

// RPCURL is the sidecar's WebSocket address while it runs; "" otherwise.
func (s KernelState) RPCURL() string {
	if s.Status != KernelRunning || s.Port == 0 {
		return ""
	}
	return kernel.URL(s.Port)
}

// KernelMissing reports that the last attempt found no kernel binary.
func (s KernelState) KernelMissing() bool { return s.Reason == errKernelMissing.Error() }

// errKernelMissing is why a launch fails when the binary is not there.
var errKernelMissing = fmt.Errorf("no kernel binary")

// KernelSupervisor keeps exactly one Kernel sidecar alive: it picks a free
// loopback port, launches the binary with a token of its own, waits for the
// handshake and the health check, and restarts it with backoff when it exits.
//
// The sidecar owns its data. The Supervisor never deletes or moves DataDir,
// not even on Stop: Windows keeps the SQLite file locked for about two seconds
// after the process is gone, so a clean-up there fails for a reason it cannot
// see. A Host that needs one does it at the next start.
type KernelSupervisor struct {
	// Binary is the sidecar executable; DataDir is its --data.
	Binary  string
	DataDir string
	// Spawn replaces the launch, for a test: it answers the sidecar's command
	// on port, with token in the environment, and its stdout.
	Spawn func(port int, token string) (*exec.Cmd, io.ReadCloser, error)
	// OnState hears every change, in order, on a goroutine of its own.
	OnState func(KernelState)
	// OnReady hears that the sidecar answers its health check, with the token
	// a Client dials with and the port it answers on.
	OnReady func(KernelState, string)
	// Terminate, optional, asks the sidecar of cmd to shut down in order in
	// place of terminateSignal.
	Terminate func(cmd *exec.Cmd)
	// Log, optional, hears what the sidecar printed and why an attempt
	// failed. It never hears the token.
	Log func(string)

	Backoff      []time.Duration
	ReadyTimeout time.Duration
	StableAfter  time.Duration
	// StopGrace is how long Stop waits before it kills the sidecar: the
	// contract's 12 s, the time harness.close() and storage.close() get.
	StopGrace time.Duration

	mu      sync.Mutex
	state   KernelState
	child   *kernelChild
	running bool
	attempt int
	timer   *time.Timer
	gen     int
	events  chan KernelState
}

// kernelChild is one launched sidecar.
type kernelChild struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	token  string
	done   chan struct{}
	err    error
	// version is what the handshake said the sidecar is.
	version string
	// handshake carries the handshake line once: the reader that pumps stdout
	// sends it, then keeps draining for the process's whole life.
	handshake chan kernel.Handshake
	// output is the end of what it wrote, so a failure's reason is its own
	// last line rather than a bare exit code.
	output *tailBuffer
}

// waitDelay bounds how long cmd.Wait lingers after the process exited: it
// waits for the pipes to close, and the processes the sidecar started hold
// them. Two seconds is enough to read what was already written.
const waitDelay = 2 * time.Second

// kernelStopGrace is the contract's shutdown window: harness.close(), then
// storage.close(), then exit.
const kernelStopGrace = 12 * time.Second

func (s *KernelSupervisor) backoff() []time.Duration {
	if len(s.Backoff) > 0 {
		return s.Backoff
	}
	return []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}
}

// State is the sidecar's current state.
func (s *KernelSupervisor) State() KernelState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Token is the live sidecar's token, "" when it does not run. It is the one
// secret of the kernel's connection, so it goes no further than a Client's
// dial.
func (s *KernelSupervisor) Token() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.child == nil || s.state.Status != KernelRunning {
		return ""
	}
	return s.child.token
}

func (s *KernelSupervisor) setLocked(st KernelState) {
	s.state = st
	if s.OnState == nil {
		return
	}
	if s.events == nil {
		s.events = make(chan KernelState, 64)
		go func(events chan KernelState) {
			for e := range events {
				s.OnState(e)
			}
		}(s.events)
	}
	s.events <- st
}

// Start launches the sidecar; a running Supervisor ignores it.
func (s *KernelSupervisor) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	s.running = true
	s.attempt = 0
	s.gen++
	go s.launch(s.gen)
}

// Stop ends the sidecar and disarms restarts. The data directory stays as it
// is.
//
// It asks the sidecar to stop, waits StopGrace (the contract's 12 s: the time
// harness.close() and storage.close() take), then ends its process group. On
// Unix that is SIGTERM, then SIGKILL to the group, which also ends the shell
// and the background processes the sidecar started. On Windows there is no
// SIGTERM: the ask is TerminateProcess, which skips the sidecar's own close and
// leaves its SQLite to recover from its write-ahead log on the next start —
// until Kernel answers a shutdown call, which the contract does not have yet
// (shared/interfaces/kernel-protocol.md asks for one).
//
// The stop never waits for the processes the sidecar started: they hold its
// pipes, and what the window waits on is the sidecar alone.
func (s *KernelSupervisor) Stop() {
	s.mu.Lock()
	s.running = false
	s.gen++
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	ch := s.child
	s.child = nil
	s.mu.Unlock()
	if ch != nil {
		if s.Terminate != nil {
			s.Terminate(ch.cmd)
		} else {
			signalProcess(ch.cmd)
		}
		grace := orDefault(s.StopGrace, kernelStopGrace)
		select {
		case <-ch.done:
		case <-time.After(grace):
			killProcess(ch.cmd)
			// Bounded even now: a pipe holder that outlives the kill must not
			// hold the window, and WaitDelay is what lets go of it.
			select {
			case <-ch.done:
			case <-time.After(2*waitDelay + grace):
				s.logf("kernel did not exit after it was killed")
			}
		}
	}
	s.mu.Lock()
	s.setLocked(KernelState{Status: KernelStopped})
	s.mu.Unlock()
}

// Restart stops the sidecar and starts a new one.
func (s *KernelSupervisor) Restart() {
	s.Stop()
	s.Start()
}

func (s *KernelSupervisor) current(gen int) bool { return s.running && gen == s.gen }

// liveChild reports whether ch is still the sidecar the Supervisor runs.
func (s *KernelSupervisor) liveChild(ch *kernelChild) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.child == ch && s.state.Status == KernelRunning
}

func (s *KernelSupervisor) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(fmt.Sprintf(format, args...))
	}
}

// LaunchToken makes the token of one launch, which Kernel requires in
// DROI_KERNEL_TOKEN.
func LaunchToken() (string, error) { return kernel.NewToken() }

func (s *KernelSupervisor) launch(gen int) {
	s.mu.Lock()
	if !s.current(gen) {
		s.mu.Unlock()
		return
	}
	s.attempt++
	attempt := s.attempt
	// A retry keeps why the last attempt failed: the window shows it instead
	// of an endless start.
	reason := ""
	if attempt > 1 {
		reason = s.state.Reason
	}
	s.mu.Unlock()

	// The port is Kernel's to choose: --port 0 asks the operating system, and
	// the handshake names the one it got. Picking it here would hand out a
	// port that another process may take in between, and the handshake is the
	// truth either way.
	port := 0
	var err error
	var token string
	token, err = LaunchToken()
	var cmd *exec.Cmd
	var stdout io.ReadCloser
	if err == nil {
		s.mu.Lock()
		if !s.current(gen) {
			s.mu.Unlock()
			return
		}
		s.setLocked(KernelState{Status: KernelStarting, Port: port, Attempt: attempt, Reason: reason})
		s.mu.Unlock()
		cmd, stdout, err = s.spawn(port, token)
	}
	if err != nil {
		s.scheduleRestart(gen, err.Error())
		return
	}
	out := newTailBuffer(4096)
	if cmd.Stderr != nil {
		cmd.Stderr = io.MultiWriter(cmd.Stderr, out)
	} else {
		cmd.Stderr = out
	}
	ch := &kernelChild{cmd: cmd, stdout: stdout, token: token, done: make(chan struct{}), output: out,
		handshake: make(chan kernel.Handshake, 1)}
	// Wait returns at most this long after the process exits, even when the
	// processes it started still hold its stdout or stderr open.
	cmd.WaitDelay = waitDelay
	if err := cmd.Start(); err != nil {
		s.scheduleRestart(gen, err.Error())
		return
	}
	// One goroutine owns stdout for the process's whole life: it takes the
	// handshake and then keeps reading. A sidecar that keeps printing must
	// never find its pipe full, which would stop it dead.
	go s.pumpStdout(ch)
	go func() {
		ch.err = cmd.Wait()
		close(ch.done)
	}()
	started := time.Now()

	s.mu.Lock()
	if !s.current(gen) {
		s.mu.Unlock()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return
	}
	s.child = ch
	s.mu.Unlock()

	ready, answered, failure := s.waitReady(port, ch)
	running := KernelState{Status: KernelRunning, Port: answered, PID: cmd.Process.Pid, Version: ch.version, Attempt: attempt}
	s.mu.Lock()
	live := s.child == ch && s.current(gen)
	if live && ready {
		// The port is the one Kernel answered on, which is the port it named
		// in its handshake: with --port 0 it is the only truth.
		s.setLocked(running)
	} else if live {
		s.mu.Unlock()
		// Spawned but never healthy; its exit restarts it below.
		killProcess(cmd)
		s.mu.Lock()
	}
	s.mu.Unlock()
	if ready && live && s.liveChild(ch) && s.OnReady != nil {
		// Outside the lock, so a callback may call back in. A Stop that lands
		// in between is why the state is read again first: the window must
		// never hear "ready" for a sidecar it already stopped.
		s.OnReady(running, token)
	}

	<-ch.done
	s.mu.Lock()
	if s.child != ch || !s.current(gen) {
		s.mu.Unlock()
		return
	}
	s.child = nil
	if time.Since(started) >= orDefault(s.StableAfter, 30*time.Second) {
		s.attempt = 0
	}
	s.mu.Unlock()
	s.logf("kernel exited: %v\n%s", ch.err, ch.output.String())
	s.scheduleRestart(gen, exitReason(ch, failure))
}

// exitReason is why an attempt ended, as the window should say it: the
// sidecar's own last line when it wrote one, else the failure, else its exit.
func exitReason(ch *kernelChild, failure string) string {
	if tail := strings.TrimSpace(ch.output.String()); tail != "" {
		lines := strings.Split(tail, "\n")
		if line := strings.TrimSpace(lines[len(lines)-1]); line != "" {
			return line
		}
	}
	if failure != "" {
		return failure
	}
	if ch.err != nil {
		return fmt.Sprintf("exited with %v", ch.err)
	}
	return "exited"
}

// spawn starts the sidecar: the Supervisor's Spawn when a test set one, else
// the binary with the contract's arguments and its token in the environment.
// The token never goes in argv, where other processes could read it.
//
// The command's working directory is left alone. Pointing it at DataDir made a
// launch fail whenever the directory did not exist yet, and on Windows a child
// process whose cwd is a directory holds a handle to it, which is the last
// thing a data directory that SQLite must release needs.
func (s *KernelSupervisor) spawn(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
	if s.Spawn != nil {
		return s.Spawn(port, token)
	}
	if s.Binary == "" {
		return nil, nil, errKernelMissing
	}
	if st, err := os.Stat(s.Binary); err != nil || st.IsDir() {
		return nil, nil, errKernelMissing
	}
	// Loopback always: another address would put an unauthenticated /health,
	// and a token-guarded /rpc, on a network the Host was never asked to
	// serve. There is no setting for it, so no setting can widen it.
	cmd := exec.Command(s.Binary,
		"--port", strconv.Itoa(port),
		"--host", "127.0.0.1",
		"--data", s.DataDir,
	)
	cmd.Env = append(os.Environ(), kernel.TokenEnv+"="+token)
	hideConsole(cmd)
	prepareProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	return cmd, stdout, nil
}

// pumpStdout is the one reader of the sidecar's stdout, for the process's
// whole life. It sends the handshake once and then keeps draining: a sidecar
// that writes more than its pipe holds after the handshake would block on its
// own logging, which is a way to stop it dead from the outside.
//
// A line longer than maxStdoutLine keeps only its tail: a sidecar that prints
// a megabyte without a newline must not make the Host grow, and must not make
// the reader give up either.
func (s *KernelSupervisor) pumpStdout(ch *kernelChild) {
	if ch.stdout == nil {
		return
	}
	defer ch.stdout.Close()
	reader := bufio.NewReaderSize(ch.stdout, 64<<10)
	var line []byte
	tooLong := false
	logged := 0
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(chunk) > 0 {
			if room := maxStdoutLine - len(line); room > 0 {
				if len(chunk) > room {
					chunk = chunk[len(chunk)-room:]
					tooLong = true
				}
				line = append(line, chunk...)
			} else {
				tooLong = true
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if len(line) > 0 || err == nil {
			text := strings.TrimRight(string(line), "\r\n")
			if tooLong {
				text = "…" + text
				tooLong = false
			}
			if h, ok := parseHandshake(text); ok {
				select {
				case ch.handshake <- h:
				default:
				}
			} else if text != "" {
				// The tail is kept, so a failure's reason can be this line;
				// the log hears the first few, so a chatty sidecar cannot
				// drown it.
				ch.output.writeString(text + "\n")
				if logged < 20 {
					s.logf("kernel said: %s", text)
					logged++
				}
			}
			line = line[:0]
		}
		if err != nil {
			return
		}
	}
}

// maxStdoutLine bounds what one line of the sidecar's stdout may cost.
const maxStdoutLine = 1 << 20

// parseHandshake reads a handshake line, and only a handshake line: it must be
// a JSON object with a port and a pid. Anything else a sidecar prints, even if
// it starts with a brace, is just output.
func parseHandshake(line string) (kernel.Handshake, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
		return kernel.Handshake{}, false
	}
	var h kernel.Handshake
	if err := json.Unmarshal([]byte(line), &h); err != nil {
		return kernel.Handshake{}, false
	}
	if h.Port <= 0 || h.Port > 65535 || h.PID <= 0 {
		return kernel.Handshake{}, false
	}
	return h, true
}

// waitReady waits for the handshake and then for health: an open port alone
// does not prove the sidecar can serve. It answers the port Kernel reports,
// which is the one a Client must dial.
func (s *KernelSupervisor) waitReady(port int, ch *kernelChild) (ready bool, answered int, failure string) {
	timeout := orDefault(s.ReadyTimeout, 30*time.Second)
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	select {
	case h := <-ch.handshake:
		if h.Version != "" {
			ch.version = h.Version
		}
		port = h.Port
	case <-ch.done:
		return false, port, ""
	case <-deadline.C:
		// Health may still come up without a handshake being needed: a
		// sidecar may print nothing and listen on the port it was given.
	}
	healthDeadline := time.Now().Add(timeout)
	for time.Now().Before(healthDeadline) {
		select {
		case <-ch.done:
			return false, port, ""
		default:
		}
		if _, err := kernel.CheckHealth(context.Background(), port); err == nil {
			return true, port, ""
		}
		select {
		case <-ch.done:
			return false, port, ""
		case <-time.After(200 * time.Millisecond):
		}
	}
	return false, port, "no health answer within " + timeout.String()
}

func (s *KernelSupervisor) scheduleRestart(gen int, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.current(gen) {
		return
	}
	steps := s.backoff()
	delay := steps[min(max(s.attempt, 1), len(steps))-1]
	s.setLocked(KernelState{Status: KernelRestarting, Delay: delay, Attempt: s.attempt, Reason: reason})
	s.timer = time.AfterFunc(delay, func() { s.launch(gen) })
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func newTailBuffer(max int) *tailBuffer { return &tailBuffer{max: max} }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) writeString(s string) { _, _ = t.Write([]byte(s)) }

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
