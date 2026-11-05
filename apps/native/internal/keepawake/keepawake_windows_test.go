package keepawake

import (
	"slices"
	"sync"
	"testing"
)

func TestHoldSetsAndClearsTheExecutionState(t *testing.T) {
	var mu sync.Mutex
	var calls []uint32
	old := setState
	setState = func(flags uint32) {
		mu.Lock()
		calls = append(calls, flags)
		mu.Unlock()
	}
	t.Cleanup(func() { setState = old })

	a, b := Hold(), Hold()
	a()
	b()
	mu.Lock()
	defer mu.Unlock()
	if want := []uint32{esContinuous | esSystemRequired, esContinuous}; !slices.Equal(calls, want) {
		t.Fatalf("calls %#x, want %#x", calls, want)
	}
}
