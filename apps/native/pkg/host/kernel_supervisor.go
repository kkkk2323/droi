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
	// ListenHost is --host; "" is 127.0.0.1.
	ListenHost string
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
	// output is the end of what it wrote, so a failure's reason is its own
	// last line rather than a bare exit code.
	output *tailBuffer
}

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

// Stop ends the sidecar and disarms restarts: the terminate signal, then the
// contract's 12 s window, then kill. The data directory stays as it is.
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
		} else if ch.cmd.Process != nil {
			_ = ch.cmd.Process.Signal(terminateSignal())
		}
		select {
		case <-ch.done:
		case <-time.After(orDefault(s.StopGrace, kernelStopGrace)):
			if ch.cmd.Process != nil {
				_ = ch.cmd.Process.Kill()
			}
			<-ch.done
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

	port, err := PickFreePort()
	var token string
	if err == nil {
		token, err = LaunchToken()
	}
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
	ch := &kernelChild{cmd: cmd, stdout: stdout, token: token, done: make(chan struct{}), output: out}
	if err := cmd.Start(); err != nil {
		s.scheduleRestart(gen, err.Error())
		return
	}
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

	ready, failure := s.waitReady(port, ch)
	s.mu.Lock()
	if s.child == ch {
		if ready {
			s.setLocked(KernelState{Status: KernelRunning, Port: port, PID: cmd.Process.Pid, Version: ch.version, Attempt: attempt})
		} else if cmd.Process != nil {
			// Spawned but never healthy; its exit restarts it below.
			_ = cmd.Process.Kill()
		}
	}
	s.mu.Unlock()
	if ready && s.OnReady != nil {
		s.OnReady(KernelState{Status: KernelRunning, Port: port, PID: cmd.Process.Pid, Version: ch.version, Attempt: attempt}, token)
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
	listen := s.ListenHost
	if listen == "" {
		listen = "127.0.0.1"
	}
	cmd := exec.Command(s.Binary,
		"--port", strconv.Itoa(port),
		"--host", listen,
		"--data", s.DataDir,
	)
	cmd.Env = append(os.Environ(), kernel.TokenEnv+"="+token)
	if s.DataDir != "" {
		cmd.Dir = s.DataDir
	}
	hideConsole(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	return cmd, stdout, nil
}

// waitReady reads the handshake and asks for health: an open port alone does
// not prove the sidecar can serve.
func (s *KernelSupervisor) waitReady(port int, ch *kernelChild) (bool, string) {
	timeout := orDefault(s.ReadyTimeout, 30*time.Second)
	deadline := time.Now().Add(timeout)
	if handshake, ok := s.readHandshake(ch, deadline); ok {
		if handshake.Version != "" {
			ch.version = handshake.Version
		}
		if handshake.Port != 0 && handshake.Port != port {
			// Kernel answers on the port it names, which is the one it got.
			port = handshake.Port
		}
	}
	for time.Now().Before(deadline) {
		select {
		case <-ch.done:
			return false, ""
		default:
		}
		if _, err := kernel.CheckHealth(context.Background(), port); err == nil {
			return true, ""
		}
		select {
		case <-ch.done:
			return false, ""
		case <-time.After(200 * time.Millisecond):
		}
	}
	return false, "no health answer within " + timeout.String()
}

// readHandshake reads stdout until the JSON handshake line arrives, logging
// the other lines it may print first.
func (s *KernelSupervisor) readHandshake(ch *kernelChild, deadline time.Time) (kernel.Handshake, bool) {
	stdout := ch.stdout
	if stdout == nil {
		return kernel.Handshake{}, false
	}
	lines := make(chan string, 8)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ch.done:
				return
			}
		}
	}()
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return kernel.Handshake{}, false
			}
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "{") {
				s.logf("kernel said: %s", trimmed)
				continue
			}
			var h kernel.Handshake
			if err := json.Unmarshal([]byte(trimmed), &h); err != nil || (h.Port == 0 && h.Version == "") {
				s.logf("kernel said: %s", trimmed)
				continue
			}
			return h, true
		case <-ch.done:
			return kernel.Handshake{}, false
		case <-time.After(time.Until(deadline)):
			return kernel.Handshake{}, false
		}
	}
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

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
