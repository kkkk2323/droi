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

func TestReview228R3PipeReadEndClosesAfterDrain(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	pair := &stdioPair{read: read, write: write}
	_ = pair.Close() // The parent's post-Start close.
	ch := &kernelChild{stdout: pair, output: newTailBuffer(4096), pumped: make(chan struct{})}
	(&KernelSupervisor{}).pumpStdout(ch)
	if !pipeEndClosed(t, read) {
		t.Fatal("pumpStdout returned but the Host-owned read fd is still open")
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

// The reviewer's admission probe put a barrier between the live check and the
// callback. There is no such gap now: nothing is admitted, the publisher reads
// whatever state is current when it wakes, so a state a Stop made stale can only
// be delivered if its callback began before Stop returned. This checks that over
// real launches: no callback that *starts* after Stop returned ever sees running.
func TestReview228R3RunningIsNeverPublishedAfterStopReturns(t *testing.T) {
	var stopped atomic.Bool
	stale := make(chan KernelState, 4)
	spawn := helperSpawn(t)
	s := &KernelSupervisor{
		Spawn: spawn,
		OnState: func(st KernelState) {
			if st.Status == KernelRunning && stopped.Load() {
				select {
				case stale <- st:
				default:
				}
			}
		},
		Backoff:      []time.Duration{time.Hour},
		ReadyTimeout: 10 * time.Second,
		StableAfter:  10 * time.Second,
		StopGrace:    time.Second,
		Log:          func(string) {},
	}
	for i := 0; i < 10; i++ {
		stopped.Store(false)
		s.Start()
		// Stop at a different point of the launch each time, so one of them
		// lands between the health answer and the running state.
		time.Sleep(time.Duration(i) * 5 * time.Millisecond)
		s.Stop()
		stopped.Store(true)
		select {
		case st := <-stale:
			t.Fatalf("iteration %d: running published after Stop returned: %+v", i, st)
		default:
		}
	}
	s.Close()
	select {
	case st := <-stale:
		t.Fatalf("running published after Stop returned: %+v", st)
	case <-time.After(200 * time.Millisecond):
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
