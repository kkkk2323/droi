package host

import (
	"sync"
	"testing"
)

type r4RetireEvents struct {
	mu     sync.Mutex
	events []string
}

func (r *r4RetireEvents) add(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

type r4RetireTree struct{ events *r4RetireEvents }

func (r r4RetireTree) kill()  { r.events.add("kill") }
func (r r4RetireTree) close() { r.events.add("release") }

type r4RetirePipe struct{ events *r4RetireEvents }

func (r r4RetirePipe) Read([]byte) (int, error) { panic("already drained") }
func (r r4RetirePipe) Close() error             { r.events.add("pipe-close"); return nil }

func TestReview228R4ConcurrentRetireRunsOnceInOrder(t *testing.T) {
	events := &r4RetireEvents{}
	ch := &kernelChild{
		tree: r4RetireTree{events}, stdout: r4RetirePipe{events},
		done: make(chan struct{}), pumped: make(chan struct{}),
	}
	close(ch.done)
	close(ch.pumped)
	s := &KernelSupervisor{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.retire(ch) }()
	}
	wg.Wait()
	if got := events.events; len(got) != 3 || got[0] != "kill" || got[1] != "pipe-close" || got[2] != "release" {
		t.Fatalf("retire was repeated or reordered: %v", got)
	}
}
