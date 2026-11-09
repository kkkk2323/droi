//go:build reviewadmission

package host

import (
	"sync/atomic"
	"testing"
	"time"
)

// The overlay inserts ONLY a barrier before the actual OnState invocation.
// This models preemption after the snapshot lock is released, not a change
// in the state read or in any branch decision.
func TestReview228R4SnapshotAndAdmissionStillHaveAGap(t *testing.T) {
	checked, release, stale, final := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1), make(chan struct{}, 1)
	var stopped atomic.Bool
	review228R4BeforeState = func(st KernelState) {
		if st.Status == KernelRunning {
			close(checked)
			<-release
		}
	}
	s := &KernelSupervisor{OnState: func(st KernelState) {
		if st.Status == KernelRunning && stopped.Load() {
			stale <- struct{}{}
		}
		if st.Status == KernelStopped {
			final <- struct{}{}
		}
	}}
	s.mu.Lock()
	s.setLocked(KernelState{Status: KernelRunning, Generation: 777})
	s.mu.Unlock()
	<-checked
	s.Stop()
	stopped.Store(true)
	close(release)
	select {
	case <-final:
	case <-time.After(time.Second):
		t.Fatal("final stopped state was lost")
	}
	s.Close()
	select {
	case <-stale:
		t.Fatal("OnState(running) BEGAN after Stop returned; snapshot is not atomic admission (final state does recover)")
	default:
	}
}
