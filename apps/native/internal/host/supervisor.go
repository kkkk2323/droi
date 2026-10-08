package host

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/l10n"
)

// DaemonStatus is where the Daemon child stands.
type DaemonStatus string

const (
	DaemonStopped    DaemonStatus = "stopped"
	DaemonStarting   DaemonStatus = "starting"
	DaemonRunning    DaemonStatus = "running"
	DaemonRestarting DaemonStatus = "restarting"
)

// DaemonState mirrors the Desktop Shell's DaemonSupervisor state.
type DaemonState struct {
	Status  DaemonStatus
	Port    int
	PID     int
	Attempt int
	Delay   time.Duration
	// Reason is why the last attempt failed, while it is retried.
	Reason string
}

// DroidMissing reports that the last attempt found no droid to start.
func (s DaemonState) DroidMissing() bool { return s.Reason == string(errNoDroid) }

// URL is the Daemon's WebSocket URL while it runs.
func (s DaemonState) URL() string {
	if s.Status != DaemonRunning {
		return ""
	}
	return "ws://127.0.0.1:" + strconv.Itoa(s.Port)
}

// Supervisor keeps exactly one Daemon child alive: it picks a free loopback
// port, waits until the Daemon answers its health check, and restarts it
// with backoff when it exits.
type Supervisor struct {
	// Spawn starts the Daemon on port; the Supervisor waits for it.
	Spawn func(port int) (*exec.Cmd, error)
	// OnState hears every change, in order, on a goroutine of its own.
	OnState func(DaemonState)

	Backoff      []time.Duration
	ReadyTimeout time.Duration
	StableAfter  time.Duration

	mu      sync.Mutex
	state   DaemonState
	child   *child
	running bool
	attempt int
	timer   *time.Timer
	gen     int
	events  chan DaemonState
}

type child struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

func (s *Supervisor) backoff() []time.Duration {
	if len(s.Backoff) > 0 {
		return s.Backoff
	}
	return []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// State returns the current state.
func (s *Supervisor) State() DaemonState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Supervisor) setLocked(st DaemonState) {
	s.state = st
	if s.OnState == nil {
		return
	}
	if s.events == nil {
		s.events = make(chan DaemonState, 64)
		go func(events chan DaemonState) {
			for e := range events {
				s.OnState(e)
			}
		}(s.events)
	}
	s.events <- st
}

// Start launches the Daemon; a running Supervisor ignores it.
func (s *Supervisor) Start() {
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

// Stop ends the child and disarms restarts.
func (s *Supervisor) Stop() {
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
		_ = ch.cmd.Process.Signal(terminateSignal())
		select {
		case <-ch.done:
		case <-time.After(3 * time.Second):
			_ = ch.cmd.Process.Kill()
			<-ch.done
		}
	}
	s.mu.Lock()
	s.setLocked(DaemonState{Status: DaemonStopped})
	s.mu.Unlock()
}

// Restart stops the Daemon and starts a new one.
func (s *Supervisor) Restart() {
	s.Stop()
	s.Start()
}

func (s *Supervisor) current(gen int) bool { return s.running && gen == s.gen }

func (s *Supervisor) launch(gen int) {
	s.mu.Lock()
	if !s.current(gen) {
		s.mu.Unlock()
		return
	}
	s.attempt++
	attempt := s.attempt
	s.mu.Unlock()

	port, err := PickFreePort()
	var cmd *exec.Cmd
	if err == nil {
		s.mu.Lock()
		if !s.current(gen) {
			s.mu.Unlock()
			return
		}
		// A retry keeps why the last attempt failed: the window shows it
		// instead of an endless start.
		reason := ""
		if attempt > 1 {
			reason = s.state.Reason
		}
		s.setLocked(DaemonState{Status: DaemonStarting, Port: port, Attempt: attempt, Reason: reason})
		s.mu.Unlock()
		cmd, err = s.Spawn(port)
	}
	if err != nil {
		// No port or no droid: retry with backoff like a crash.
		s.scheduleRestart(gen, err.Error())
		return
	}
	ch := &child{cmd: cmd, done: make(chan struct{})}
	go func() { ch.err = cmd.Wait(); close(ch.done) }()
	started := time.Now()

	s.mu.Lock()
	if !s.current(gen) {
		s.mu.Unlock()
		_ = cmd.Process.Kill()
		return
	}
	s.child = ch
	s.mu.Unlock()

	ready := waitForHealth(port, orDefault(s.ReadyTimeout, 30*time.Second), ch.done)
	s.mu.Lock()
	if s.child == ch {
		if ready {
			s.setLocked(DaemonState{Status: DaemonRunning, Port: port, PID: cmd.Process.Pid})
		} else {
			// Spawned but never healthy; its exit restarts it below.
			_ = cmd.Process.Kill()
		}
	}
	s.mu.Unlock()

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
	reason := l10n.L("exited")
	if ch.err != nil {
		reason = l10n.L("exited with %s", ch.err.Error())
	}
	s.scheduleRestart(gen, reason)
}

func (s *Supervisor) scheduleRestart(gen int, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.current(gen) {
		return
	}
	steps := s.backoff()
	delay := steps[min(max(s.attempt, 1), len(steps))-1]
	s.setLocked(DaemonState{Status: DaemonRestarting, Delay: delay, Attempt: s.attempt, Reason: reason})
	s.timer = time.AfterFunc(delay, func() { s.launch(gen) })
}

// PickFreePort asks the system for a free loopback port.
func PickFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	if a, ok := l.Addr().(*net.TCPAddr); ok {
		return a.Port, nil
	}
	return 0, fmt.Errorf("no port assigned")
}

// IsHealthy asks GET /health: the Daemon serves it once its RPC server is
// up, which an open port alone does not prove.
func IsHealthy(port int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+"/health", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode >= 200 && res.StatusCode < 300
}

func waitForHealth(port int, timeout time.Duration, exited <-chan struct{}) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			return false
		default:
		}
		if IsHealthy(port) {
			return true
		}
		select {
		case <-exited:
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
	return false
}
