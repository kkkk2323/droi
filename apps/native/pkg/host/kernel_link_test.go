package host

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
)

// A Kernel that restarts is a new process: a new port, a new token, and the
// conversations are still there because the data directory is. The link is what
// turns that into a new Client for a consumer.
func TestTheLinkDialsEachGenerationOnce(t *testing.T) {
	h := fakekernel.NewHarness(t, fakekernel.Scenario{
		Steps: []fakekernel.Step{{Match: "hello", Text: "the first answer"}},
	})
	w := newWatch()
	s := w.supervisor(h.Spawn)
	s.DataDir = h.DataDir
	s.Backoff = []time.Duration{10 * time.Millisecond}
	defer s.Close()
	defer s.Stop()

	clients := make(chan arrival, 8)
	gone := make(chan int, 8)
	link := NewKernelLink(KernelLinkOptions{
		Client: kernel.ClientWindow,
		Token:  s.Token,
		OnClient: func(c *kernel.Client, gen int) {
			clients <- arrival{c, gen}
		},
		OnGone: func(gen int, err error) { gone <- gen },
		Log:    func(string) {},
	})
	defer link.Close()
	s.OnState = func(st KernelState) {
		w.states <- st
		link.Use(st)
	}

	s.Start()
	first := waitArrival(t, clients)
	if first.gen == 0 || first.client == nil {
		t.Fatalf("the first connection: gen %d, client %v", first.gen, first.client)
	}
	if got, gen := link.Current(); got != first.client || gen != first.gen {
		t.Fatalf("Current() = %v/%d, want the first arrival", got, gen)
	}

	// A conversation with something in it, so the second generation has
	// something to find again.
	id := h.CreateConversation(t, first.client)
	sub := h.Submit(t, first.client, id, "hello")
	h.WaitForReply(t, first.client, sub, "the first answer", 60*time.Second)

	// The Kernel dies the way a crash does: the process goes, not a stop.
	pid := w.waitState(t, KernelRunning).PID
	proc, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Kill(); err != nil {
		t.Fatalf("kill pid %d: %v", pid, err)
	}

	second := waitArrival(t, clients)
	if second.gen <= first.gen {
		t.Fatalf("the restart arrived as generation %d, after %d", second.gen, first.gen)
	}
	if second.client == first.client {
		t.Fatal("the restart reused the first generation's connection")
	}
	if s.Token() == "" {
		t.Fatal("no token for the running kernel")
	}

	// Selecting an old generation is what a consumer must not do: this one is
	// closed, and the link says so.
	select {
	case <-first.client.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("the first generation's connection is still open after the restart")
	}
	select {
	case gen := <-gone:
		if gen != first.gen {
			t.Fatalf("OnGone said generation %d, want %d", gen, first.gen)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("OnGone never heard the first generation end")
	}

	// The consumer's job, done here by hand: open what it had, on the new
	// connection, and the whole view comes back from the same data directory.
	opened, err := second.client.Open(t.Context(), kernel.OpenParams{ID: id, Window: &fakekernel.DefaultWindow})
	if err != nil {
		t.Fatalf("open on the new generation: %v", err)
	}
	if opened.View.Conversation.ID != id {
		t.Fatalf("opened conversation %d, want %d", opened.View.Conversation.ID, id)
	}
	found := false
	for _, entry := range opened.View.Entries {
		if strings.Contains(string(entry.Model), "the first answer") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the reopened view lost its entries: %d of them", len(opened.View.Entries))
	}
}

// A connection that arrives for a generation the Supervisor has already left is
// not handed over, and is closed rather than leaked.
func TestALateDialForAnOldGenerationIsDropped(t *testing.T) {
	h := fakekernel.NewHarness(t, fakekernel.Scenario{})
	w := newWatch()
	s := w.supervisor(h.Spawn)
	s.DataDir = h.DataDir
	defer s.Close()
	defer s.Stop()
	s.Start()
	running := w.waitState(t, KernelRunning)

	// The first generation's dial is held until the second generation is live,
	// which is the race a slow dial loses.
	release := make(chan struct{})
	started := make(chan struct{})
	clients := make(chan arrival, 8)
	var late *kernel.Client
	var mu sync.Mutex
	link := NewKernelLink(KernelLinkOptions{
		Token: s.Token,
		Dial: func(ctx context.Context, gen int, url, token string) (*kernel.Client, error) {
			if gen == running.Generation {
				// The old generation's dial lands only after the new one is
				// live, and ignores the cancel: the race a slow dial loses.
				close(started)
				<-release
				ctx = context.Background()
			}
			c, err := kernel.Dial(ctx, kernel.Options{URL: url, Token: token, Client: kernel.ClientWindow})
			if gen == running.Generation {
				mu.Lock()
				late = c
				mu.Unlock()
			}
			return c, err
		},
		OnClient: func(c *kernel.Client, gen int) { clients <- arrival{c, gen} },
		Log:      func(string) {},
	})
	defer link.Close()

	link.Use(running)
	<-started
	// A second generation, from the same run: the generation number is what
	// says which connection a consumer may use.
	next := running
	next.Generation = running.Generation + 1
	link.Use(next)
	taken := waitArrival(t, clients)
	if taken.gen != next.Generation {
		t.Fatalf("the connection handed over is generation %d, want %d", taken.gen, next.Generation)
	}
	close(release)
	for deadline := time.Now().Add(30 * time.Second); ; {
		mu.Lock()
		c := late
		mu.Unlock()
		if c != nil {
			select {
			case <-c.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the late dial for the old generation was neither hung up nor closed")
		}
	}
}

// A dial that fails is tried again on the backoff, and the connection it
// finally makes is the one handed over.
func TestAFailedDialIsRetried(t *testing.T) {
	var calls atomic.Int32
	clients := make(chan arrival, 4)
	server := startLinkKernel(t)
	link := NewKernelLink(KernelLinkOptions{
		Backoff: []time.Duration{5 * time.Millisecond, 5 * time.Millisecond, 5 * time.Millisecond},
		Dial: func(ctx context.Context, gen int, url, token string) (*kernel.Client, error) {
			if calls.Add(1) < 3 {
				return nil, errors.New("not yet")
			}
			return server.dial(ctx)
		},
		OnClient: func(c *kernel.Client, gen int) { clients <- arrival{c, gen} },
	})
	defer link.Close()
	link.Use(KernelState{Status: KernelRunning, Port: 1, Generation: 1})
	got := waitArrival(t, clients)
	if got.gen != 1 || calls.Load() != 3 {
		t.Fatalf("handed over generation %d after %d dials, want 1 after 3", got.gen, calls.Load())
	}
}

// Use is called on the Supervisor's own goroutine, so a slow dial must not hold
// it: the Supervisor's next state would wait behind the network.
func TestUseDoesNotWaitForTheDial(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	link := NewKernelLink(KernelLinkOptions{
		Dial: func(ctx context.Context, gen int, url, token string) (*kernel.Client, error) {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil, ctx.Err()
		},
	})
	defer link.Close()
	done := make(chan struct{})
	go func() {
		link.Use(KernelState{Status: KernelRunning, Port: 1, Generation: 1})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Use waited for the dial")
	}
}

// After Close nothing is dialled again, however many retries were pending.
func TestCloseStopsTheRetries(t *testing.T) {
	var calls atomic.Int32
	link := NewKernelLink(KernelLinkOptions{
		Backoff: []time.Duration{20 * time.Millisecond, 20 * time.Millisecond, 20 * time.Millisecond, 20 * time.Millisecond},
		Dial: func(ctx context.Context, gen int, url, token string) (*kernel.Client, error) {
			calls.Add(1)
			return nil, errors.New("refused")
		},
	})
	link.Use(KernelState{Status: KernelRunning, Port: 1, Generation: 1})
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	link.Close()
	after := calls.Load()
	time.Sleep(150 * time.Millisecond)
	if calls.Load() != after {
		t.Fatalf("dialled %d more times after Close", calls.Load()-after)
	}
}

// A stopped Supervisor ends the connection, once, and a Start after it is
// dialled again: stopping the Kernel is not closing the link.
func TestAStopEndsTheConnectionAndAStartDialsAgain(t *testing.T) {
	server := startLinkKernel(t)
	clients := make(chan arrival, 4)
	gone := make(chan int, 4)
	link := NewKernelLink(KernelLinkOptions{
		Dial: func(ctx context.Context, gen int, url, token string) (*kernel.Client, error) {
			return server.dial(ctx)
		},
		OnClient: func(c *kernel.Client, gen int) { clients <- arrival{c, gen} },
		OnGone:   func(gen int, err error) { gone <- gen },
	})
	defer link.Close()
	link.Use(KernelState{Status: KernelRunning, Port: 1, Generation: 1})
	first := waitArrival(t, clients)
	link.Use(KernelState{Status: KernelStopped})
	select {
	case <-first.client.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the connection outlived the stop")
	}
	if gen := <-gone; gen != 1 {
		t.Fatalf("OnGone said %d, want 1", gen)
	}
	link.Use(KernelState{Status: KernelRunning, Port: 1, Generation: 2})
	if second := waitArrival(t, clients); second.gen != 2 {
		t.Fatalf("after the restart the link handed over generation %d, want 2", second.gen)
	}
	time.Sleep(100 * time.Millisecond)
	select {
	case gen := <-gone:
		t.Fatalf("OnGone heard generation %d twice", gen)
	default:
	}
}
