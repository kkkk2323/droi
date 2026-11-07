package host

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// The two halves of the reviewer's probe, which pre-closed both ends and so
// could not tell the pump's own clean-up from retire's.
//
// This is the first: the Host-owned read end is closed by the pump itself when
// the child's write end reaches EOF, without anyone closing it from outside.
func TestReview228R3PumpClosesTheReadEndItOwns(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	pair := &stdioPair{read: read, write: write}
	ch := &kernelChild{stdout: pair, output: newTailBuffer(4096), pumped: make(chan struct{})}
	if _, err := write.WriteString("a line\n"); err != nil {
		t.Fatal(err)
	}
	// The child is gone: its write end reaches EOF, which is what the pump
	// sees when the last holder of that end exits.
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	(&KernelSupervisor{}).pumpStdout(ch)
	select {
	case <-ch.pumped:
	case <-time.After(time.Second):
		t.Fatal("the pump did not return at EOF")
	}
	if !pipeEndClosed(t, read) {
		t.Fatal("pumpStdout returned but the Host-owned read fd is still open")
	}
}

// And the second: a process that left the tree keeps the write end open, so a
// reader that only waits for EOF never returns. Retiring the generation closes
// the read end under it, and the reader goes.
func TestReview228R3RetireClosesTheReadEndUnderAStuckPump(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close() // The holder the kill could not reach: never closed here.
	pair := &stdioPair{read: read, write: write}
	ch := &kernelChild{
		stdout: pair, output: newTailBuffer(4096),
		done: make(chan struct{}), pumped: make(chan struct{}),
	}
	close(ch.done)
	go (&KernelSupervisor{}).pumpStdout(ch)
	// The pump is blocked on a read that will not see EOF.
	time.Sleep(20 * time.Millisecond)
	started := time.Now()
	(&KernelSupervisor{}).retire(ch)
	if elapsed := time.Since(started); elapsed > waitDelay+pumpJoin+2*time.Second {
		t.Fatalf("retire waited %s for a reader it can close itself", elapsed)
	}
	select {
	case <-ch.pumped:
	case <-time.After(time.Second):
		t.Fatal("the reader is still blocked after retire closed the read end")
	}
	if !pipeEndClosed(t, read) {
		t.Fatal("retire returned with the Host-owned read fd still open")
	}
}
func TestReview228R3StartFailureClosesBothPipeEnds(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	states := make(chan KernelState, 8)
	s := &KernelSupervisor{
		Backoff: []time.Duration{time.Hour},
		OnState: func(st KernelState) { states <- st },
		Spawn: func(int, string) (*exec.Cmd, io.ReadCloser, error) {
			cmd := exec.Command(filepath.Join(t.TempDir(), "does-not-exist"))
			cmd.Stdout = write
			return cmd, &stdioPair{read: read, write: write}, nil
		},
	}
	s.Start()
	defer s.Stop()
	for {
		select {
		case st := <-states:
			if st.Status == KernelRestarting {
				if !pipeEndClosed(t, read) || !pipeEndClosed(t, write) {
					t.Fatal("cmd.Start failed but pipe ends remain open")
				}
				return
			}
		case <-time.After(time.Second):
			t.Fatal("failed launch did not finish")
		}
	}
}

// The reviewer's probe called announceReady from OnReady and started a Stop
// from another goroutine. OnReady is gone — one publisher hands every state over,
// so "ready" is the running state — and the cycle it was after is the same: a
// callback that delegates Stop must not be waited for. Stop publishes, invalidates
// the generation and signals the process; it never joins a callback.
func TestReview228R3OnStateDelegatedStopDoesNotDeadlock(t *testing.T) {
	s := &KernelSupervisor{StopGrace: time.Millisecond}
	defer s.Close()
	entered, release, stopped, callbackReturned := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	s.OnState = func(KernelState) {
		// A Stop pokes the publisher again; only the first callback is the
		// one that delegates it.
		if !first.CompareAndSwap(false, true) {
			return
		}
		go func() { s.Stop(); close(stopped) }()
		close(entered)
		select {
		case <-stopped:
		case <-release: // Let the probe clean up even when the cycle exists.
		}
		close(callbackReturned)
	}
	s.mu.Lock()
	s.setLocked(KernelState{Status: KernelRunning})
	s.mu.Unlock()
	<-entered
	deadlocked := false
	select {
	case <-stopped:
	case <-time.After(150 * time.Millisecond):
		deadlocked = true
	}
	close(release)
	<-callbackReturned
	<-stopped
	if deadlocked {
		t.Fatal("an OnState that delegates Stop keeps Stop waiting: the lifecycle must not join a callback")
	}
}

// A callback that began before Stop returned may return after it — the
// publisher takes the state, releases the lock and calls, and Stop does not
// join a callback because a callback may be the thing waiting for Stop. What a
// window is promised is the end of the sequence, not the absence of a stale
// state: the stopped state always follows, and Generation says which is which.
//
// This is deterministic: the callback itself is the barrier.
func TestReview228R3AStartedCallbackOutlastsStopAndStoppedFollows(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	delivered := make(chan KernelState, 4)
	var first atomic.Bool
	s := &KernelSupervisor{}
	defer s.Close()
	s.OnState = func(st KernelState) {
		if first.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
		delivered <- st
	}
	s.mu.Lock()
	s.setLocked(KernelState{Status: KernelRunning, Generation: 7})
	s.mu.Unlock()
	<-entered // The running state is inside a callback now.
	s.Stop()  // It returns without waiting for that callback.
	close(release)
	var sawRunning, sawStopped bool
	for deadline := time.After(2 * time.Second); !(sawRunning && sawStopped); {
		select {
		case st := <-delivered:
			switch st.Status {
			case KernelRunning:
				sawRunning = true
				if st.Generation != 7 {
					t.Fatalf("the stale running state lost its generation: %+v", st)
				}
			case KernelStopped:
				sawStopped = true
			}
		case <-deadline:
			t.Fatalf("saw running=%v stopped=%v: the stopped state must always follow a stale one", sawRunning, sawStopped)
		}
	}
	if current := s.State(); current.Status != KernelStopped {
		t.Fatalf("the last state a Host hears must be the current one: %+v", current)
	}
}
func TestReview228R3RestartKeepsCallbacksSerialized(t *testing.T) {
	var active atomic.Int32
	var first atomic.Bool
	overlap := make(chan struct{}, 8)
	restarted, release := make(chan struct{}), make(chan struct{})
	s := &KernelSupervisor{Binary: filepath.Join(t.TempDir(), "no-kernel"), Backoff: []time.Duration{time.Hour}}
	s.OnState = func(KernelState) {
		if active.Add(1) > 1 {
			select {
			case overlap <- struct{}{}:
			default:
			}
		}
		defer active.Add(-1)
		if first.CompareAndSwap(false, true) {
			s.Restart()
			close(restarted)
			<-release
		}
	}
	s.mu.Lock()
	s.setLocked(KernelState{Status: KernelStarting})
	s.mu.Unlock()
	<-restarted
	concurrent := false
	select {
	case <-overlap:
		concurrent = true
	case <-time.After(250 * time.Millisecond):
	}
	close(release)
	s.Stop()
	if concurrent {
		t.Fatal("Restart inside OnState starts a second publisher before the first callback returns")
	}
}

func TestReview228R3OnStateDoesNotPublishRunningAfterStopReturns(t *testing.T) {
	entered, release, stopped, stale := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	var first atomic.Bool
	s := &KernelSupervisor{}
	s.OnState = func(st KernelState) {
		if first.CompareAndSwap(false, true) {
			close(entered)
			<-release
			s.Stop()
			close(stopped)
		} else if st.Status == KernelRunning {
			select {
			case <-stopped:
				stale <- struct{}{}
			default:
			}
		}
	}
	s.mu.Lock()
	s.setLocked(KernelState{Status: KernelStarting})
	s.mu.Unlock()
	<-entered
	s.mu.Lock()
	s.setLocked(KernelState{Status: KernelRunning})
	s.mu.Unlock()
	close(release)
	<-stopped
	select {
	case <-stale:
		t.Fatal("queued OnState(running) was published after the callback's Stop returned")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestReview228R3StopStartDoesNotLeakPublisherGoroutines(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()
	s := &KernelSupervisor{
		Binary:  filepath.Join(t.TempDir(), "no-kernel"),
		Backoff: []time.Duration{time.Hour},
		OnState: func(KernelState) {},
	}
	for i := 0; i < 50; i++ {
		s.Start()
		s.Stop()
	}
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before+3 && time.Now().Before(deadline) {
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	after := runtime.NumGoroutine()
	t.Logf("goroutines before=%d after=%d (50 Start/Stop cycles)", before, after)
	if after > before+3 {
		t.Fatalf("publisher goroutines did not retire: before=%d after=%d", before, after)
	}
}
