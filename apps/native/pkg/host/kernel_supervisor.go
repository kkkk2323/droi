package host

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
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

	mu             sync.Mutex
	state          KernelState
	child          *kernelChild
	running        bool
	attempt        int
	timer          *time.Timer
	gen            int
	events         chan KernelState
	ready          chan readyNotice
	dispatch       chan struct{}
	dispatchClosed bool
	// callbackDone closes when the Host callback now running returns, and
	// callbackG is the goroutine running it: Stop waits for the one and not
	// for itself.
	callbackDone chan struct{}
	callbackG    uint64
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
	// failure, when set, is why the Supervisor ended this child on purpose: a
	// health check that failed too often. It is what the window says.
	failure string
	// pumped closes when the reader of stdout has drained it, so a failure's
	// reason is not read while the last line is still in the pipe.
	pumped chan struct{}
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

// setLocked records a state and hands it to OnState without ever waiting for
// it. The callback runs on a goroutine of its own, so it may call back in —
// the state is already set, and the lock is not held while it runs — and a
// callback that falls behind costs the states it did not take, not the
// Supervisor.
//
// The holder must hold s.mu.
func (s *KernelSupervisor) setLocked(st KernelState) {
	s.state = st
	if s.OnState == nil {
		// Nothing hears states; a publisher may still be needed for OnReady.
		return
	}
	s.ensurePublisherLocked()
	select {
	case s.events <- st:
	default:
		// A callback this far behind is not keeping up; the newest state is
		// the one it needs, so the oldest waiting one goes.
		select {
		case <-s.events:
		default:
		}
		select {
		case s.events <- st:
		default:
		}
	}
}

// ensurePublisherLocked starts the state goroutine if it is not running. A Stop
// ends it, and a Start after that must begin a new one: a publisher that is
// never rebuilt leaves OnState silent for the life of the process while the
// state keeps changing underneath.
//
// The holder must hold s.mu.
func (s *KernelSupervisor) ensurePublisherLocked() {
	if (s.OnState == nil && s.OnReady == nil) || s.events != nil {
		return
	}
	s.events = make(chan KernelState, eventBuffer)
	s.ready = make(chan readyNotice, 8)
	s.dispatch = make(chan struct{})
	s.dispatchClosed = false
	// The channels are handed over, not read from the fields: the next Start
	// replaces them, and a publisher that kept looking would race with it.
	go s.publish(s.events, s.ready, s.dispatch)
}

// publish drains the state queue on its own goroutine and stops when the
// Supervisor does: it is the only caller of OnState and OnReady, so neither
// runs with a lock held and a callback may call back into the Supervisor.
func (s *KernelSupervisor) publish(events <-chan KernelState, ready <-chan readyNotice, dispatch <-chan struct{}) {
	for {
		select {
		case st := <-events:
			s.callback(func() { s.OnState(st) })
		case r := <-ready:
			// A stop that got here first has already put the state back to
			// stopped, and the ready was queued before it: drop it rather
			// than tell the window about a sidecar that is gone.
			if r.live() {
				s.callback(func() { s.OnReady(r.state, r.token) })
			}
		case <-dispatch:
			// Stop is over: hand over what is still queued, then go. A ready
			// still waiting is dropped, because Stop has already ended it.
			for {
				select {
				case st := <-events:
					s.callback(func() { s.OnState(st) })
				default:
					return
				}
			}
		}
	}
}

// callback runs one Host callback and marks it as running, so a Stop from
// another goroutine can wait for it — a window must not hear "running" after a
// stop has returned — while a Stop from inside the callback does not wait for
// itself.
func (s *KernelSupervisor) callback(fn func()) {
	done := make(chan struct{})
	s.mu.Lock()
	s.callbackDone = done
	s.callbackG = goroutineID()
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.callbackDone == done {
			s.callbackDone = nil
			s.callbackG = 0
		}
		s.mu.Unlock()
		close(done)
	}()
	fn()
}

// waitCallbacks returns once no Host callback is running, unless this
// goroutine is the one running it: a callback that calls Stop must not wait
// for itself.
func (s *KernelSupervisor) waitCallbacks() {
	s.mu.Lock()
	done, owner := s.callbackDone, s.callbackG
	s.mu.Unlock()
	if done == nil || owner == goroutineID() {
		return
	}
	<-done
}

// goroutineID is this goroutine's id, read from the stack header. It exists for
// one question: is the callback that is running the one that called Stop?
func goroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// "goroutine 123 [running]:" — the number is the id.
	const prefix = "goroutine "
	if n <= len(prefix) || string(buf[:len(prefix)]) != prefix {
		return 0
	}
	rest := buf[len(prefix):n]
	id := uint64(0)
	for _, c := range rest {
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + uint64(c-'0')
	}
	return id
}

// stopPublishing ends the state goroutine once what is queued has been handed
// over. The holder must hold s.mu.
func (s *KernelSupervisor) stopPublishing() {
	if s.dispatch == nil || s.dispatchClosed {
		return
	}
	s.dispatchClosed = true
	close(s.dispatch)
	// The next Start builds new channels: the old goroutine owns these.
	s.events = nil
	s.ready = nil
}

// readyNotice is one pending OnReady, with the check that decides whether it
// is still true when it is delivered.
type readyNotice struct {
	state KernelState
	token string
	live  func() bool
}

// eventBuffer is how many states may wait for a slow OnState.
const eventBuffer = 64

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
	// A publisher that a previous Stop ended is started again: without this,
	// OnState goes silent for the life of the process from the second Start on.
	s.ensurePublisherLocked()
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
// pipes, and what the window waits on is the sidecar alone. It returns within
// StopGrace plus WaitDelay — 14 s with the defaults — however the sidecar
// behaves.
func (s *KernelSupervisor) Stop() {
	// A callback that is already publishing "running" finishes first, so the
	// window never hears it after this returns. A callback that calls Stop
	// itself is not waited for: it is the one this would block on.
	s.waitCallbacks()
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
		exited := false
		select {
		case <-ch.done:
			exited = true
		case <-time.After(grace):
			// Bounded: a pipe holder that outlives the kill must not hold the
			// window, and WaitDelay is what lets go of it.
			select {
			case <-ch.done:
			case <-time.After(waitDelay):
				s.logf("kernel did not exit %s after it was asked", waitDelay)
			}
		}
		// The sidecar being gone is not the tree being gone: a shell or a
		// background process it started keeps its own life, and one that
		// ignores the signal outlives its parent. The group is killed either
		// way, and a group whose leader has exited is still there to kill on
		// Unix.
		killProcess(ch.cmd)
		if !exited {
			select {
			case <-ch.done:
			case <-time.After(waitDelay):
			}
		}
	}
	s.mu.Lock()
	s.setLocked(KernelState{Status: KernelStopped})
	s.stopPublishing()
	s.mu.Unlock()
}

// Restart stops the sidecar and starts a new one.
func (s *KernelSupervisor) Restart() {
	s.Stop()
	s.Start()
}

func (s *KernelSupervisor) current(gen int) bool { return s.running && gen == s.gen }

// announceReady tells the Host its sidecar is up, unless a Stop got there
// first. The callback runs on the publisher's goroutine, never with a lock
// held, so it may call back into the Supervisor — Stop or Restart from inside
// OnReady would deadlock if it were called under one.
func (s *KernelSupervisor) announceReady(ch *kernelChild, st KernelState, token string) {
	if s.OnReady == nil {
		return
	}
	s.mu.Lock()
	s.ensurePublisherLocked()
	ready := s.ready
	live := s.child == ch && s.state.Status == KernelRunning
	s.mu.Unlock()
	if !live || ready == nil {
		return
	}
	select {
	case ready <- readyNotice{state: st, token: token, live: func() bool { return s.liveChild(ch) }}:
	default:
		s.logf("kernel: a ready notice was dropped; the state is already published")
	}
}

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
		handshake: make(chan kernel.Handshake, 1), pumped: make(chan struct{})}
	// Wait returns at most this long after the process exits, even when the
	// processes it started still hold its stdout or stderr open.
	cmd.WaitDelay = waitDelay
	if err := cmd.Start(); err != nil {
		s.scheduleRestart(gen, err.Error())
		return
	}
	// The child holds the write end now; the parent's must go, or the read end
	// never sees EOF. Only a pipe of this Supervisor's has one to close: a
	// Spawn hook that hands back a pipe of its own owns it.
	if pair, ok := stdout.(*stdioPair); ok {
		_ = pair.Close()
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
		// A child of a superseded generation still owns a process group: kill
		// the group, not just the process, so what it started goes too.
		killProcess(cmd)
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
	if ready && live {
		s.announceReady(ch, running, token)
	}

	<-ch.done
	// The process is gone; what it wrote may still be in the pipe. The reason
	// the window shows is its last line, so wait for the drain — bounded, so a
	// process holding the write end cannot hold the Supervisor.
	select {
	case <-ch.pumped:
	case <-time.After(waitDelay):
	}
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
	// A pipe of the Host's own, not cmd.StdoutPipe(): Wait closes the latter
	// when it returns, which can cut the reader off before it has drained
	// what the sidecar printed — and the last line it printed is what explains
	// why it is gone.
	read, write, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	cmd.Stdout = write
	return cmd, &stdioPair{read: read, write: write}, nil
}

// stdioPair is a pipe whose write end goes to the child and whose read end the
// Host drains. The Host closes the write end after Start, so the read end sees
// EOF when the child and every process holding it are gone.
type stdioPair struct {
	read  *os.File
	write *os.File
}

// Close closes the write end the parent keeps, after the child has started.
func (p *stdioPair) Close() error { return p.write.Close() }

// Read reads what the child wrote.
func (p *stdioPair) Read(b []byte) (int, error) { return p.read.Read(b) }

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
	if ch.pumped != nil {
		defer close(ch.pumped)
	}
	reader := bufio.NewReaderSize(ch.stdout, 64<<10)
	var line []byte
	// omitted is how much of a line longer than maxStdoutLine was thrown
	// away; the tail is what is kept, because the last thing a sidecar wrote
	// before it died is what explains its death.
	omitted := 0
	logged := 0
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(chunk) > 0 {
			line = append(line, chunk...)
			if over := len(line) - maxStdoutLine; over > 0 {
				line = append(line[:0], line[over:]...)
				omitted += over
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if len(line) > 0 || err == nil {
			text := strings.TrimRight(string(line), "\r\n")
			if omitted > 0 {
				text = "…(" + strconv.Itoa(omitted) + " bytes left out) " + text
				omitted = 0
			}
			if h, ok := parseHandshake(text); ok {
				select {
				case ch.handshake <- h:
				default:
				}
			} else if text != "" {
				// The end of a line is the part that explains a death, so what
				// the log and the failure's reason keep is the tail.
				kept := text
				if over := len(kept) - maxLogLine; over > 0 {
					kept = "…(" + strconv.Itoa(over) + " more bytes) " + kept[over:]
				}
				ch.output.writeString(kept + "\n")
				// The tail is kept for a failure's reason; the log hears the
				// first few lines, so a chatty sidecar cannot drown it.
				if logged < 20 {
					s.logf("kernel said: %s", kept)
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

// maxStdoutLine bounds what one line of the sidecar's stdout may cost: past
// it the tail is kept and the rest counted, so a sidecar that prints a
// gigabyte without a newline cannot make the Host grow.
const maxStdoutLine = 1 << 20

// maxLogLine bounds what one line adds to the log; the rest is counted.
const maxLogLine = 4 << 10

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
		// No handshake, no address: the contract's launch is --port 0 and the
		// handshake names the port it got, so there is nothing to ask health
		// on. Waiting the whole timeout again would only double the time a
		// broken launch takes to fail.
		return false, port, "no handshake within " + timeout.String()
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
