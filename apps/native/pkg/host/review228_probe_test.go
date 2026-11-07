package host

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReview228OnStateSurvivesStopStart(t *testing.T) {
	states := make(chan KernelState, 16)
	s := &KernelSupervisor{
		Binary:  filepath.Join(t.TempDir(), "no-kernel"),
		Backoff: []time.Duration{time.Hour},
		OnState: func(st KernelState) { states <- st },
	}
	defer s.Stop()
	wait := func(want KernelStatus) {
		t.Helper()
		for {
			select {
			case st := <-states:
				if st.Status == want {
					return
				}
			case <-time.After(time.Second):
				t.Fatalf("missing OnState(%s); actual State=%+v", want, s.State())
			}
		}
	}
	s.Start()
	wait(KernelRestarting)
	s.Stop()
	wait(KernelStopped)
	// Let the publisher finish its documented drain-and-exit.
	time.Sleep(50 * time.Millisecond)
	s.Start()
	wait(KernelRestarting)
}

func TestReview228SlowOnStatePastBufferDoesNotBlock(t *testing.T) {
	block := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	s := &KernelSupervisor{OnState: func(KernelState) {
		once.Do(func() { close(entered) })
		<-block
	}}
	s.mu.Lock()
	s.setLocked(KernelState{Status: KernelStarting})
	s.mu.Unlock()
	<-entered
	done := make(chan struct{})
	go func() {
		for i := 0; i < 80; i++ {
			s.mu.Lock()
			s.setLocked(KernelState{Status: KernelRestarting, Attempt: i})
			s.mu.Unlock()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Error("a slow OnState filled the state buffer and blocked the supervisor")
	}
	close(block)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("state writer never recovered")
	}
	s.Stop()
}

// The round-2 probe called announceReady from OnReady. OnReady is gone: one
// publisher hands every state over, and "ready" is the running state. The
// invariant is the same — a callback that stops the sidecar itself must not
// deadlock Stop — so this calls Stop from inside OnState, on the publisher's
// goroutine.
func TestReview228OnStateCanStop(t *testing.T) {
	s := &KernelSupervisor{StopGrace: time.Millisecond}
	defer s.Close()
	done := make(chan struct{})
	var once sync.Once
	s.OnState = func(KernelState) {
		s.Stop()
		once.Do(func() { close(done) })
	}
	s.mu.Lock()
	s.setLocked(KernelState{Status: KernelRunning})
	s.mu.Unlock()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("OnState -> Stop deadlocks: a callback must not be waited for")
	}
}

// The round-2 probe asserted that Stop waits for a published running state to
// finish. That promise is gone on purpose: a callback that waits for a Stop it
// delegates cannot be joined (round 3, P1-3). What remains, and is what a
// window needs, is that no callback *begins* with a running state once Stop has
// returned.
func TestReview228OnStateNotRunningAfterStopReturns(t *testing.T) {
	s := &KernelSupervisor{StopGrace: time.Millisecond}
	defer s.Close()
	entered, release, stopReturned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	stale := make(chan KernelState, 1)
	var first atomic.Bool
	s.OnState = func(st KernelState) {
		if !first.CompareAndSwap(false, true) {
			if st.Status == KernelRunning {
				select {
				case <-stopReturned:
					select {
					case stale <- st:
					default:
					}
				default:
				}
			}
			return
		}
		close(entered)
		<-release
	}
	s.mu.Lock()
	s.setLocked(KernelState{Status: KernelStarting})
	s.mu.Unlock()
	<-entered
	s.mu.Lock()
	s.setLocked(KernelState{Status: KernelRunning})
	s.mu.Unlock()
	s.Stop()
	close(stopReturned)
	close(release)
	select {
	case st := <-stale:
		t.Fatalf("a callback began with %+v after Stop returned", st)
	case <-time.After(150 * time.Millisecond):
	}
}
