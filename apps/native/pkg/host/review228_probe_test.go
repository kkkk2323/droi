package host

import (
	"os/exec"
	"path/filepath"
	"sync"
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

func TestReview228OnReadyCanStop(t *testing.T) {
	s := &KernelSupervisor{StopGrace: time.Millisecond}
	ch := &kernelChild{cmd: exec.Command("unused"), done: make(chan struct{})}
	close(ch.done)
	st := KernelState{Status: KernelRunning}
	s.child, s.state, s.running = ch, st, true
	done := make(chan struct{})
	s.OnReady = func(KernelState, string) {
		s.Stop()
		close(done)
	}
	go s.announceReady(ch, st, "test")
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("OnReady -> Stop deadlocks on the announce mutex")
	}
}

func TestReview228OnReadySerializesWithStop(t *testing.T) {
	s := &KernelSupervisor{StopGrace: time.Millisecond}
	ch := &kernelChild{cmd: exec.Command("unused"), done: make(chan struct{})}
	close(ch.done)
	st := KernelState{Status: KernelRunning}
	s.child, s.state, s.running = ch, st, true
	entered, release := make(chan struct{}), make(chan struct{})
	announced := make(chan struct{})
	s.OnReady = func(KernelState, string) {
		close(entered)
		<-release
		close(announced)
	}
	go s.announceReady(ch, st, "test")
	<-entered
	stopped := make(chan struct{})
	go func() { s.Stop(); close(stopped) }()
	tooEarly := false
	select {
	case <-stopped:
		tooEarly = true
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-announced
	<-stopped
	if tooEarly {
		t.Fatal("Stop returned while OnReady could still publish running")
	}
}
