package fakekernel

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestContextHonoursTheTestDeadline pins the bound every RPC and every wait uses: the caller's `within`, or
// the test's own deadline if that is sooner. Without the second, a stalled RPC answers after the test has
// already been killed for running too long, and the panic that stops the test skips the cleanups that would
// have asserted something.
func TestContextHonoursTheTestDeadline(t *testing.T) {
	k := &Kernel{}
	ctx, cancel := k.context(t, 5*time.Second)
	defer cancel()
	if deadline, ok := ctx.Deadline(); !ok {
		t.Fatal("no deadline: a wait would be unbounded")
	} else if remaining := time.Until(deadline); remaining > 5*time.Second+100*time.Millisecond {
		t.Fatalf("the deadline is %s away, longer than the 5 s asked for", remaining)
	}

	// A `within` longer than the test's own remaining time is cut down to it.
	ctx, cancel = k.context(t, time.Hour)
	defer cancel()
	testDeadline, ok := t.Deadline()
	if !ok {
		t.Skip("the runner set no test deadline")
	}
	if deadline, ok := ctx.Deadline(); !ok {
		t.Fatal("no deadline")
	} else if deadline.After(testDeadline) {
		t.Fatalf("the deadline outlives the test: %s after %s", deadline, testDeadline)
	}
}

// TestCloseCancelsASlowStream is the goroutine the first version leaked: a handler sleeping between deltas
// kept running after Close, so a test with a slow stream left a writer for every chunk it still had.
func TestCloseCancelsASlowStream(t *testing.T) {
	chunks := make([]string, 40)
	for i := range chunks {
		chunks[i] = "part "
	}
	k := Start(t, Scenario{Steps: []Step{{Text: strings.Join(chunks, ""), Chunks: chunks, Delay: 100 * time.Millisecond}}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	k.Submit(t, c, id, "a slow answer")

	// Wait until the stream is really in progress, so Close interrupts a handler rather than racing its start.
	for deadline := time.Now().Add(10 * time.Second); len(k.Requests()) == 0; {
		if time.Now().After(deadline) {
			t.Fatalf("the model was never asked\n%s", k.Logs())
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)

	closed := make(chan struct{})
	go func() {
		k.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return: it waits for a stream handler that does not answer its cancellation")
	}

	// The handler must be gone, not sleeping out the rest of its script.
	buffer := make([]byte, 1<<20)
	for deadline := time.Now().Add(3 * time.Second); ; {
		n := runtime.Stack(buffer, true)
		if !strings.Contains(string(buffer[:n]), "(*scriptedModel).stream") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a stream handler outlives Close: its delay ignores cancellation")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestBookkeepingBelongsToTheSubmissionThatNamesIt pins the ownership rule for the Kernel's own entries.
//
// They are not model-visible and are written outside a turn's placement: a request memo for a submission that
// is only queued lands after the running turn's own entry. Attributing that memo to whoever happens to be
// placed is how a wait for `droi.request` matched a stranger's bookkeeping.
func TestBookkeepingBelongsToTheSubmissionThatNamesIt(t *testing.T) {
	turn := Submission{Conversation: 1, ID: 10}
	cases := []struct {
		name  string
		entry Entry
		want  bool
	}{
		{"model entry", Entry{ID: 9, Kind: "pi.user"}, true},
		{"own memo", Entry{ID: 14, Kind: "droi.request", Data: []byte(`{"requestId":"r","submissionId":10}`)}, true},
		{"foreign memo", Entry{ID: 14, Kind: "droi.request", Data: []byte(`{"requestId":"r","submissionId":11}`)}, false},
		{"unnamed memo", Entry{ID: 14, Kind: "droi.request", Data: []byte(`{"requestId":"r"}`)}, false},
		{"no payload", Entry{ID: 14, Kind: "droi.request"}, false},
		{"unreadable payload", Entry{ID: 14, Kind: "droi.meta", Data: []byte(`not json`)}, false},
	}
	for _, test := range cases {
		if got := belongsToTurn(test.entry, turn); got != test.want {
			t.Errorf("%s: belongsToTurn = %v, want %v", test.name, got, test.want)
		}
	}
}
