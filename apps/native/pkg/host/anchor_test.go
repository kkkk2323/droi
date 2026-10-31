package host

import (
	"io"
	"testing"
	"time"
)

// The anchor stays while its input is open and ends when the Host closes it.
func TestTheDaemonAnchorEndsWhenItsInputCloses(t *testing.T) {
	r, w := io.Pipe()
	done := make(chan int)
	go func() { done <- anchorMain(r) }()
	_, _ = w.Write([]byte("x"))
	select {
	case <-done:
		t.Fatal("the anchor ended with its input open")
	case <-time.After(100 * time.Millisecond):
	}
	_ = w.Close()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the anchor outlived its input")
	}
}
