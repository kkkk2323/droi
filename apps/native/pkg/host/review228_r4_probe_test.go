package host

import (
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func r4FDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// Real, ready sidecars, not Start immediately followed by Stop before spawn.
// Do not GC between fd samples: finalizers must not mask unclosed ownership.
func TestReview228R4HundredRealCycles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("/dev/fd is Unix-only")
	}
	spawn := helperSpawn(t, "GORACE=atexit_sleep_ms=0")
	states := make(chan KernelState, 1024)
	s := &KernelSupervisor{
		Spawn: spawn, OnState: func(st KernelState) { states <- st },
		StopGrace: time.Second, Backoff: []time.Duration{time.Hour},
	}
	defer s.Close()
	defer s.Stop()
	cycle := func() {
		s.Start()
		timeout := time.NewTimer(10 * time.Second)
		defer timeout.Stop()
		for {
			select {
			case st := <-states:
				if st.Status != KernelRunning {
					continue
				}
				s.mu.Lock()
				ch := s.child
				s.mu.Unlock()
				s.Stop()
				select {
				case <-ch.done:
				default:
					t.Fatal("Stop returned before the sidecar was reaped")
				}
				select {
				case <-ch.pumped:
				default:
					t.Fatal("Stop returned before stdout was retired")
				}
				return
			case <-timeout.C:
				t.Fatalf("never became ready: %+v", s.State())
			}
		}
	}
	cycle() // Warm HTTP/runtime pools once.
	beforeG, beforeFD := runtime.NumGoroutine(), r4FDs(t)
	maxFD := beforeFD
	for i := 0; i < 100; i++ {
		cycle()
		if n := r4FDs(t); n > maxFD {
			maxFD = n
		}
	}
	afterFD := r4FDs(t)
	s.Close()
	for deadline := time.Now().Add(time.Second); runtime.NumGoroutine() > beforeG+2 && time.Now().Before(deadline); {
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	afterG := runtime.NumGoroutine()
	t.Logf("100 READY/Stop cycles: goroutines=%d -> %d; fds=%d -> %d; max=%d", beforeG, afterG, beforeFD, afterFD, maxFD)
	if afterG > beforeG+2 || afterFD > beforeFD+3 || maxFD > beforeFD+5 {
		t.Fatal("generation resources accumulated")
	}
}

func TestReview228R4FinalStateSurvivesCoalescingAndClose(t *testing.T) {
	entered, release, delivered := make(chan struct{}), make(chan struct{}), make(chan KernelState, 4)
	var once atomic.Bool
	s := &KernelSupervisor{}
	s.OnState = func(st KernelState) {
		if once.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
		delivered <- st
	}
	s.mu.Lock()
	s.setLocked(KernelState{Status: KernelStarting})
	s.mu.Unlock()
	<-entered
	for i := 0; i < 1000; i++ {
		s.mu.Lock()
		s.setLocked(KernelState{Status: KernelRunning, Generation: i})
		s.mu.Unlock()
	}
	s.Stop()
	want := s.State()
	s.Close()
	close(release)
	var last KernelState
	for last != want {
		select {
		case last = <-delivered:
		case <-time.After(time.Second):
			t.Fatalf("last=%+v, current=%+v", last, want)
		}
	}
}

// A failed readiness must retire the generation even when SIGTERM is ignored.
func TestReview228R4FailedReadinessMustRetire(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows TerminateProcess is not ignored")
	}
	spawn := helperSpawn(t, "GO_KERNEL_NO_HANDSHAKE=1", "GO_KERNEL_IGNORE_SIGTERM=1", "GORACE=atexit_sleep_ms=0")
	var attempts atomic.Int32
	s := &KernelSupervisor{
		Spawn: func(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
			attempts.Add(1)
			return spawn(port, token)
		},
		ReadyTimeout: 100 * time.Millisecond, StopGrace: 20 * time.Millisecond,
		Backoff: []time.Duration{time.Hour},
	}
	defer s.Close()
	defer s.Stop()
	s.Start()
	time.Sleep(time.Second)
	if st := s.State(); st.Status != KernelRestarting {
		t.Fatalf("failed readiness never retired/retried: state=%+v attempts=%d", st, attempts.Load())
	}
}
