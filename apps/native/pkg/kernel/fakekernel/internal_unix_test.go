//go:build !windows

package fakekernel

import (
	"syscall"
	"testing"
	"time"
)

// TestCloseBoundsItsShutdownDial is the hang the review reproduced: a Kernel that accepts TCP and then stops
// answering (SIGSTOP) made Close block for ever, because the WebSocket handshake ran on context.Background
// and RequestTimeout bounds RPCs only.
func TestCloseBoundsItsShutdownDial(t *testing.T) {
	k := Start(t, Scenario{})
	// Stop the Kernel: it still holds the port, so a dial connects and never completes a handshake.
	if err := k.cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatalf("SIGSTOP: %v", err)
	}
	started := time.Now()
	done := make(chan struct{})
	go func() {
		k.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(40 * time.Second):
		_ = k.cmd.Process.Signal(syscall.SIGCONT)
		t.Fatal("Close blocked on a stopped Kernel: its shutdown dial has no deadline")
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Fatalf("Close took %s against a stopped Kernel", elapsed)
	}
}
