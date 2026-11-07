package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/brand"
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
//
// It is a value the Supervisor publishes asynchronously, and the promise is
// deliberately weaker than "nothing stale after Stop returns":
//
//   - A callback that began before Stop returned may return after it, and it
//     may carry a running state: the publisher takes the state, releases the
//     lock and then calls. Stop does not join a callback — a callback may be the
//     thing waiting for Stop — so no ordering there can be closed without
//     reintroducing that deadlock.
//   - What is guaranteed is the end: after Stop returns, a stale running may
//     arrive, and the stopped state that follows it always does, so the last
//     state a Host hears is the current one.
//
// A window is therefore written to draw what it is handed and to check
// Generation before it acts on a running sidecar: a state of an earlier
// generation is stale by definition.
type KernelState struct {
	Status KernelStatus
	Port   int
	PID    int
	// Generation counts launches from one. A Host that sees a new generation
	// tears its old Client down and dials again; a state of an earlier
	// generation for a sidecar that is gone is stale by definition.
	Generation int
	Attempt    int
	Delay      time.Duration
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

// KernelSupervisor keeps exactly one Kernel sidecar alive: it launches the
// binary with a token of its own, waits for the handshake and the health
// check, and restarts it with backoff when it exits.
//
// The sidecar owns its data. The Supervisor never deletes or moves DataDir,
// not even on Stop: Windows keeps the SQLite file locked for about two seconds
// after the process is gone, so a clean-up there fails for a reason it cannot
// see. A Host that needs one does it at the next start.
//
// States reach the Host through one publisher goroutine for the Supervisor's
// whole life, on a latest-value rule: a change stores the new State and pokes
// a one-slot signal, and the publisher reads whatever State is current when it
// wakes. So the callbacks are serialized, the last one a Host sees is the
// current state, and states that came and went between two wakes are merged
// away on purpose — a window draws the state, not a history of it. Stop and
// Restart never wait for a callback, so a callback may call them, or start a
// goroutine that does, without deadlocking.
type KernelSupervisor struct {
	// Binary is the sidecar executable; DataDir is its --data.
	Binary  string
	DataDir string
	// UserDir is the user's own directory for Kernel — personal skills, the
	// global mcp.json, the global AGENTS.md — which it receives as
	// DROI_USER_DIR. "" is the brand's, ~/.droi, which is also what a Kernel
	// started by hand falls back to.
	UserDir string
	// Spawn replaces the launch, for a test: it answers the sidecar's command
	// with token in its environment, and its stdout.
	Spawn func(port int, token string) (*exec.Cmd, io.ReadCloser, error)
	// OnState hears the state as it settles, on a goroutine of the Supervisor's
	// own, serialized, and never with a lock held. It may call Stop or Restart.
	OnState func(KernelState)
	// AskShutdown, optional, asks the running sidecar to close in order —
	// kernel.shutdown over the connection the Host already has — and returns
	// once Kernel has answered. It is the only orderly way to stop Kernel on
	// Windows, where a parent can only TerminateProcess, which skips
	// storage.close() and leaves the SQLite file to recover from its log.
	//
	// Stop asks it first, before the platform's signal, which is what the
	// contract calls an orderly stop; a negative answer of any kind falls back
	// to the signal.
	AskShutdown func(ctx context.Context) error
	// Terminate, optional, asks the sidecar of cmd to shut down in place of
	// the process group's terminate signal.
	Terminate func(cmd *exec.Cmd)
	// Log, optional, hears what the sidecar printed and why an attempt
	// failed. It never hears the token.
	Log func(string)

	Backoff      []time.Duration
	ReadyTimeout time.Duration
	StableAfter  time.Duration
	// HealthEvery is how often a running sidecar's health is asked; 0 is 5 s,
	// and a negative value turns the check off.
	HealthEvery time.Duration
	// HealthFailures is how many checks in a row must fail before the sidecar
	// is restarted; 0 is 3. A process that is up but wedged answers nothing,
	// and this is what notices.
	HealthFailures int
	// StopGrace is how long Stop waits for the sidecar's own close before it
	// ends the tree: the contract's 12 s, the time harness.close() and
	// storage.close() get.
	StopGrace time.Duration

	mu       sync.Mutex
	state    KernelState
	child    *kernelChild
	running  bool
	attempt  int
	launched int
	timer    *time.Timer
	gen      int
	// pubWake carries one pending "the state changed" signal, and pubStop ends
	// the publisher. Both are made once, the first time a state is set: the
	// publisher is the Supervisor's for its whole life, never a generation's.
	pubWake chan struct{}
	pubStop chan struct{}
	// closed records Close: the publisher is gone for good, and a later state
	// change must not start another one that nothing would end.
	closed bool
}

// kernelChild is one launched sidecar, with everything that has to be released
// when it is gone.
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
	// pumped closes when the reader of stdout has drained it, so a failure's
	// reason is not read while the last line is still in the pipe.
	pumped chan struct{}
	// tree owns every process of this generation: a process group on Unix, a
	// Job Object on Windows. It is what makes a stop end the sidecar's
	// children too, and what makes a crash leave none of them behind.
	tree processTree
	// retired makes the clean-up run once, whichever path gets there first.
	retired sync.Once
	// killed ends the process tree once. Both the watchdog and retire kill
	// it, and a second kill of a tree whose handles are already released is
	// exactly what a supervisor must not do.
	killed sync.Once
	// failure, when set, is why the Supervisor ended this child itself — a
	// health check that failed too often. It is what the window says. The
	// Supervisor's mutex guards it.
	failure string
}

// processTree is a generation's whole process tree.
type processTree interface {
	// kill ends every process of the tree, bounded, and answers once they are
	// gone or the bound has passed.
	kill()
	// close releases the handle without ending anything else: after kill it
	// only frees what the Host holds.
	close()
}

// endTree ends this generation's process tree, once. Whoever gets there first —
// the watchdog that found a wedged sidecar, or retire — is the one that kills
// it, and the other waits for that kill rather than repeating it on handles
// that may already be released.
func (ch *kernelChild) endTree() {
	ch.killed.Do(func() {
		if ch.tree != nil {
			ch.tree.kill()
			return
		}
		if ch.cmd != nil && ch.cmd.Process != nil {
			_ = ch.cmd.Process.Kill()
		}
	})
}

// waitDelay bounds how long the Supervisor waits for a generation's stdout
// after its processes were killed: what was already written is worth reading,
// and a process that holds the write end without being in the tree is not.
const waitDelay = 2 * time.Second

// pumpJoin is how long the Supervisor waits for the stdout reader to return
// once it has closed the read end under it.
const pumpJoin = time.Second

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

// setLocked records a state and pokes the publisher. It never waits for a
// callback: the publisher reads whatever is current when it wakes, so a state
// that came and went inside one callback is merged away rather than queued.
//
// The holder must hold s.mu.
func (s *KernelSupervisor) setLocked(st KernelState) {
	s.state = st
	if s.closed {
		return
	}
	if s.pubWake == nil {
		if s.OnState == nil {
			return
		}
		s.pubWake = make(chan struct{}, 1)
		s.pubStop = make(chan struct{})
		go s.publish(s.pubWake, s.pubStop)
	}
	select {
	case s.pubWake <- struct{}{}:
	default:
	}
}

// publish hands the current state to OnState, one call at a time, until Close.
//
// It reads the state when it wakes rather than being handed one, which is what
// makes the callbacks serialized and the last one current: a Stop that lands
// while a callback runs is already in the state when the next wake reads it, so
// a queued "running" can never be published after it.
func (s *KernelSupervisor) publish(wake <-chan struct{}, stop <-chan struct{}) {
	var last KernelState
	var published bool
	for {
		select {
		case <-wake:
			s.mu.Lock()
			st := s.state
			s.mu.Unlock()
			if published && st == last {
				continue
			}
			last, published = st, true
			s.OnState(st)
		case <-stop:
			s.mu.Lock()
			st := s.state
			s.mu.Unlock()
			if !published || st != last {
				s.OnState(st)
			}
			return
		}
	}
}

// Close ends the publisher once it has handed over the current state, and keeps
// it ended: states a later Stop records are stored but published to nobody. A
// Host that is going away calls it so the goroutine does not outlive it.
//
// It does not wait for the publisher: a callback that blocks would block the
// shutdown of the thing that owns the callbacks.
func (s *KernelSupervisor) Close() {
	s.mu.Lock()
	s.closed = true
	stop := s.pubStop
	s.pubStop = nil
	s.mu.Unlock()
	if stop != nil {
		close(stop)
	}
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
// It asks the sidecar to stop — AskShutdown when the Host wired one, which is
// kernel.shutdown and the only orderly stop on Windows, else the platform's
// signal — waits StopGrace for it to exit, then ends the generation's whole
// process tree. The tree is ended whether or not the sidecar exited: a shell
// or a background process it started keeps its own life, and one that ignores
// the signal outlives its parent.
//
// StopGrace is the contract's own shutdown window, and it covers what Kernel
// does after it answers: close the harness, end the tool process groups it
// started, close the storage. The ask may use the grace to answer and the same
// grace again to exit, so a close that spends its whole window on the harness
// still has time for the rest before the tree is ended from outside.
//
// Stop never waits for a callback. The state is published before the sidecar is
// asked to stop, so a window hears "stopped" while the process is still closing;
// a callback may call Stop or Restart without deadlocking. The price is the one
// KernelState documents: a callback already under way may deliver a stale
// running state after Stop returned, and the stopped state that follows it is
// what ends the sequence.
//
// How long it can take: StopGrace (12 s by default), then the bounded drain of
// what the sidecar already wrote (waitDelay, then pumpJoin) and the tree's own
// bound (graceGroup on Unix, one call on Windows). About 16 s at worst with the
// defaults, and no part of it waits for a callback.
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
	s.setLocked(KernelState{Status: KernelStopped})
	s.mu.Unlock()
	if ch == nil {
		return
	}
	grace := orDefault(s.StopGrace, kernelStopGrace)
	// An orderly close first, then the signal: kernel.shutdown runs the
	// sidecar's own harness.close() and storage.close(), and only a sidecar that
	// will not answer gets ended.
	if !s.askShutdown(grace, ch) {
		if s.Terminate != nil {
			s.Terminate(ch.cmd)
		} else {
			signalProcess(ch.cmd)
		}
		select {
		case <-ch.done:
		case <-time.After(grace):
			s.logf("kernel did not exit %s after it was asked", grace)
		}
	}
	s.retire(ch)
}

// askShutdown asks Kernel to close in order, within grace, and bounds how long
// the answer and the exit may take. It answers whether the sidecar went; an ask
// that is not wired, fails, or outlasts the grace is the caller's to handle
// with a signal.
func (s *KernelSupervisor) askShutdown(grace time.Duration, ch *kernelChild) bool {
	if s.AskShutdown == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	answered := make(chan error, 1)
	go func() { answered <- s.AskShutdown(ctx) }()
	select {
	case err := <-answered:
		if err != nil {
			s.logf("kernel.shutdown failed: %v", err)
			return false
		}
	case <-ctx.Done():
		s.logf("kernel.shutdown did not answer within %s", grace)
		return false
	}
	select {
	case <-ch.done:
		return true
	case <-time.After(grace):
		s.logf("kernel did not exit %s after it began closing", grace)
		return false
	case <-ctx.Done():
		return false
	}
}

// liveChild reports whether ch is still the generation the Supervisor owns.
func (s *KernelSupervisor) liveChild(ch *kernelChild) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.child == ch && s.running
}

// owning reports whether this generation is still the Supervisor's to end: it is
// what the watchdog checks before it kills, so a tree that a Stop retired first
// is not touched again.
func (s *KernelSupervisor) owning(ch *kernelChild) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.child == ch
}

// reasonOf is why the Supervisor ended this child itself, "" when it did not.
func (s *KernelSupervisor) reasonOf(ch *kernelChild) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ch.failure
}

// watchHealth asks a running sidecar's health while it runs, and kills one that
// stops answering: a process that is up but wedged — a storage call that never
// returns, an event loop stuck behind a synchronous wait — is invisible to a
// supervisor that only watches for an exit. A killed sidecar exits, and the
// launch loop restarts it as it would after a crash.
func (s *KernelSupervisor) watchHealth(ch *kernelChild, port int) {
	every := s.HealthEvery
	if every == 0 {
		every = 5 * time.Second
	}
	if every < 0 {
		return
	}
	failuresWanted := s.HealthFailures
	if failuresWanted <= 0 {
		failuresWanted = 3
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	failures := 0
	for {
		select {
		case <-ch.done:
			return
		case <-ticker.C:
		}
		if !s.liveChild(ch) {
			return
		}
		if _, err := kernel.CheckHealth(context.Background(), port); err == nil {
			failures = 0
			continue
		}
		failures++
		if failures < failuresWanted {
			continue
		}
		s.logf("kernel did not answer health %d times in a row: restarting it", failures)
		// Under the ownership of the generation: a Stop that landed while this
		// check was in flight has already retired the tree and released its
		// handles, and a kill after that is a kill of something else.
		if !s.owning(ch) {
			return
		}
		s.mu.Lock()
		ch.failure = "health: no answer after " + strconv.Itoa(failures) + " checks"
		s.mu.Unlock()
		// The whole tree: a wedged sidecar's children are wedged with it, and
		// the exit that follows is what wakes the launch loop.
		ch.endTree()
		return
	}
}

// Restart stops the sidecar and starts a new one.
func (s *KernelSupervisor) Restart() {
	s.Stop()
	s.Start()
}

// retire is the one clean-up path a generation has, whichever way it ended:
// explicit Stop, a launch that never became ready, a launch a newer one
// replaced, or the sidecar crashing on its own. It ends the tree, closes both
// ends of the stdout pipe, and lets the reader go — in that order, and once.
//
// Closing the read end under the reader is what makes a process that left the
// tree (a SetGid/Setsid descendant holding the write end) unable to keep the
// reader alive: the reader's Read fails and it returns.
func (s *KernelSupervisor) retire(ch *kernelChild) {
	ch.retired.Do(func() {
		ch.endTree()
		// The process itself is reaped by the waiter of its command, and "the
		// sidecar is gone" means reaped: until then it is a zombie that still
		// answers a signal, which is what a caller of Stop would see. A
		// SIGKILLed process exits at once, so this is microseconds; the bound
		// is for one that cannot be killed at all.
		if ch.done != nil {
			select {
			case <-ch.done:
			case <-time.After(waitDelay):
				s.logf("kernel: the sidecar of this generation did not exit after its tree was ended")
			}
		}
		// What was already written is worth reading; the pipe's write end may
		// be held by something the kill could not reach.
		select {
		case <-ch.pumped:
		case <-time.After(waitDelay):
		}
		if ch.stdout != nil {
			_ = ch.stdout.Close()
		}
		select {
		case <-ch.pumped:
		case <-time.After(pumpJoin):
			s.logf("kernel: the reader of the sidecar's stdout did not return after its pipe was closed")
		}
		if ch.tree != nil {
			ch.tree.close()
		}
	})
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
	if !s.running || gen != s.gen {
		s.mu.Unlock()
		return
	}
	s.attempt++
	s.launched++
	attempt, launch := s.attempt, s.launched
	// A retry keeps why the last attempt failed: the window shows it instead
	// of an endless start.
	reason := ""
	if attempt > 1 {
		reason = s.state.Reason
	}
	s.mu.Unlock()

	// The port is Kernel's to choose: --port 0 asks the operating system, and
	// the handshake names the one it got. Picking it here would hand out a port
	// that another process may take in between, and the handshake is the truth
	// either way.
	port := 0
	token, err := LaunchToken()
	var cmd *exec.Cmd
	var stdout io.ReadCloser
	if err == nil {
		s.mu.Lock()
		if !s.running || gen != s.gen {
			s.mu.Unlock()
			return
		}
		s.setLocked(KernelState{Status: KernelStarting, Attempt: attempt, Generation: launch, Reason: reason})
		s.mu.Unlock()
		cmd, stdout, err = s.spawn(port, token)
	}
	if err != nil {
		// Nothing was started, so there is no tree to end — but a Spawn hook
		// may have made its pipes already.
		closeReadCloser(stdout)
		s.scheduleRestart(gen, err.Error())
		return
	}
	// The user's own directory goes on whatever launched it — the binary, or a
	// test's Spawn hook — because it is Kernel's to read, not the launcher's to
	// remember. An empty value leaves Kernel at its own default.
	if dir := s.userDir(); dir != "" {
		cmd.Env = withUserDir(cmd.Env, dir)
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
	// processes it started still hold its stderr open.
	cmd.WaitDelay = waitDelay
	// Whatever made the command — a Spawn hook or spawn — its processes must
	// be a tree this Supervisor can end, so the platform's arrangement is made
	// here, before Start, not by whoever built it.
	prepareProcess(cmd)
	if err := cmd.Start(); err != nil {
		// Both ends of the pipe are the Host's until the child holds them: a
		// launch that never started must not leak them.
		closeReadCloser(stdout)
		s.scheduleRestart(gen, err.Error())
		return
	}
	// The child holds the write end now; the parent's must go, or the read end
	// never sees EOF. Only a pipe of this Supervisor's has one to close: a
	// Spawn hook that hands back a pipe of its own owns it.
	if pair, ok := stdout.(*stdioPair); ok {
		_ = pair.closeWrite()
	}
	// The tree is claimed before the sidecar can start anything: a Job Object
	// only holds what is assigned to it, and Kernel starts no process before
	// its handshake, which is after this.
	if tree, err := newProcessTree(cmd); err != nil {
		s.logf("kernel: cannot own the sidecar's process tree: %v", err)
	} else {
		ch.tree = tree
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
	if !s.running || gen != s.gen {
		s.mu.Unlock()
		// A child of a superseded generation is cleaned up like any other, so
		// what it started goes too.
		s.retire(ch)
		return
	}
	s.child = ch
	s.mu.Unlock()

	ready, answered, failure := s.waitReady(port, ch)
	running := KernelState{Status: KernelRunning, Port: answered, PID: cmd.Process.Pid,
		Version: ch.version, Attempt: attempt, Generation: launch}
	s.mu.Lock()
	live := s.child == ch && s.running && gen == s.gen
	if live && ready {
		// The port is the one Kernel answered on, which is the port it named in
		// its handshake: with --port 0 it is the only truth.
		s.setLocked(running)
	}
	s.mu.Unlock()
	if ready && live {
		// A sidecar that answered health once can still wedge later: the
		// watch is what notices, and a kill is answered by the launch loop as
		// it would answer a crash.
		go s.watchHealth(ch, answered)
	}
	if live && !ready {
		// Spawned but never healthy: end it now rather than wait for a
		// restart it would trigger anyway.
		//
		// The wait is bounded and the tree is ended after it, because this
		// launch is over either way: a sidecar that ignores the signal — a
		// harness stuck in close, a process in its own handler — would
		// otherwise hold the launch goroutine here forever, with the state
		// still saying "starting" and no restart ever scheduled.
		if s.Terminate != nil {
			s.Terminate(ch.cmd)
		} else {
			signalProcess(ch.cmd)
		}
		grace := orDefault(s.StopGrace, kernelStopGrace)
		select {
		case <-ch.done:
		case <-time.After(grace):
			s.logf("kernel did not exit %s after a failed readiness was signalled", grace)
		}
		s.retire(ch)
	}

	<-ch.done
	// However it ended — a crash, a failed readiness, a stop — this generation
	// is cleaned up here, before its record is dropped and the next one starts.
	s.retire(ch)
	s.mu.Lock()
	stillOurs := s.child == ch && s.running && gen == s.gen
	if stillOurs {
		s.child = nil
		if time.Since(started) >= orDefault(s.StableAfter, 30*time.Second) {
			s.attempt = 0
		}
	}
	s.mu.Unlock()
	if !stillOurs {
		return
	}
	s.logf("kernel exited: %v\n%s", ch.err, ch.output.String())
	// A refusal is not a crash. Kernel exits 2 when it will not start at all —
	// bad arguments, no token, or a data directory another kernel already holds
	// — and the one that matters here is the lock: retrying it in a backoff loop
	// would only spin against a directory that stays taken, so the Supervisor
	// says what the sidecar said and waits to be asked to start again.
	if exitCode(ch.err) == kernel.ExitRefused {
		reason := s.exitReason(ch, failure)
		// The decision is taken again under the lock that commits it: a Stop,
		// or a Stop and a Start, may have run between the ownership check and
		// here, and this refusal would then belong to a generation nobody owns.
		// Writing "stopped" over the next one left a live sidecar that no Stop
		// would ever reach.
		s.mu.Lock()
		if !s.running || gen != s.gen {
			s.mu.Unlock()
			s.logf("kernel refused to start; the generation it belonged to is gone: %s", reason)
			return
		}
		s.running = false
		s.attempt = 0
		s.setLocked(KernelState{Status: KernelStopped, Reason: "the kernel refused to start: " + reason})
		s.mu.Unlock()
		s.logf("kernel refused to start; not retried: %s", reason)
		return
	}
	s.scheduleRestart(gen, s.exitReason(ch, failure))
}

// exitCode is the status a sidecar exited with, -1 when it was killed by a
// signal or never ran.
func exitCode(err error) int {
	if err == nil {
		return kernel.ExitOK
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		return exited.ExitCode()
	}
	return -1
}

// closeReadCloser closes what a failed launch left behind, if it can.
func closeReadCloser(rc io.ReadCloser) {
	if rc != nil {
		_ = rc.Close()
	}
}

// exitReason is why an attempt ended, as the window should say it: the
// sidecar's own last line when it wrote one, else the failure, else its exit.
func (s *KernelSupervisor) exitReason(ch *kernelChild, failure string) string {
	if ended := s.reasonOf(ch); ended != "" {
		return ended
	}
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
	pair := newStdioPipe(cmd)
	if pair == nil {
		return cmd, nil, nil
	}
	return cmd, pair, nil
}

// withUserDir adds DROI_USER_DIR=dir to a launch's environment unless the
// launcher already chose one (a test keeps its own temporary directory). A nil
// environment is the process's own, as os/exec reads it, so it starts there.
func withUserDir(env []string, dir string) []string {
	if env == nil {
		env = os.Environ()
	}
	prefix := kernel.UserDirEnv + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return env
		}
	}
	return append(env, prefix+dir)
}

// userDir is the absolute directory Kernel is told to keep the user's own
// skills, mcp.json and AGENTS.md in.
//
// It is the app's own folder, ~/.droi, not the droid CLI's ~/.factory: that one
// belongs to another program, and its contents are copied once at first start
// (CopyFactoryHome) rather than read in place. A Kernel told to read ~/.factory
// would also start writing its own mcp.json there, beside droid's.
func (s *KernelSupervisor) userDir() string {
	if s.UserDir != "" {
		return s.UserDir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// No HOME is no answer rather than a wrong one: an empty value leaves
		// Kernel at its own default, which is this same folder.
		return ""
	}
	return brand.UserFolder(home)
}

// stdioPair is a pipe whose write end goes to the child and whose read end the
// Host drains. The Host closes the write end after Start, so the read end sees
// EOF when the child and every process holding it are gone.
//
// Both ends are closed from more than one goroutine: the parent's write end
// after Start, the read end by whoever retires the generation and by the reader
// as it leaves. So the pair is its own lock, and closing twice is nothing.
type stdioPair struct {
	mu    sync.Mutex
	read  *os.File
	write *os.File
}

// newStdioPipe makes the pair and hands the write end to cmd. A pipe of the
// Host's own, not cmd.StdoutPipe(): Wait closes the latter when it returns,
// which can cut the reader off before it has drained what the sidecar printed —
// and the last line it printed is what explains why it is gone.
func newStdioPipe(cmd *exec.Cmd) *stdioPair {
	read, write, err := os.Pipe()
	if err != nil {
		// A pipe that cannot be made is not a reason to refuse a sidecar: it
		// runs without one, and its handshake is then the only news.
		return nil
	}
	cmd.Stdout = write
	return &stdioPair{read: read, write: write}
}

// closeWrite closes the write end the parent keeps, once the child holds it.
func (p *stdioPair) closeWrite() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.write == nil {
		return nil
	}
	err := p.write.Close()
	p.write = nil
	return err
}

// Close closes both ends, which is what a launch that never started, a
// generation being retired and a reader that is leaving all need. A close of
// something already closed is not an error, so any of them may be the last.
func (p *stdioPair) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var err error
	if p.write != nil {
		_ = p.write.Close()
		p.write = nil
	}
	if p.read != nil {
		err = p.read.Close()
		p.read = nil
	}
	return err
}

// Read reads what the child wrote. The file is taken under the lock and read
// outside it, so a Close during a blocked Read reaches that Read at once.
func (p *stdioPair) Read(b []byte) (int, error) {
	p.mu.Lock()
	read := p.read
	p.mu.Unlock()
	if read == nil {
		return 0, os.ErrClosed
	}
	return read.Read(b)
}

// pumpStdout is the one reader of the sidecar's stdout, for the process's whole
// life. It sends the handshake once and then keeps draining: a sidecar that
// writes more than its pipe holds after the handshake would block on its own
// logging, which is a way to stop it dead from the outside.
//
// A line longer than maxStdoutLine keeps only its tail: a sidecar that prints a
// megabyte without a newline must not make the Host grow, and must not make the
// reader give up either.
func (s *KernelSupervisor) pumpStdout(ch *kernelChild) {
	if ch.stdout == nil {
		return
	}
	// Both ends go when the reader leaves: the read end is what a process
	// outside the tree would otherwise keep alive.
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

// maxStdoutLine bounds what one line of the sidecar's stdout may cost: past it
// the tail is kept and the rest counted, so a sidecar that prints a gigabyte
// without a newline cannot make the Host grow.
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
		// handshake names the port it got, so there is nothing to ask health on.
		// Waiting the whole timeout again would only double the time a broken
		// launch takes to fail.
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
	if !s.running || gen != s.gen {
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
