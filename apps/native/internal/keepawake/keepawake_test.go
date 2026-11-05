package keepawake

import "testing"

func TestKeeperStartsOnTheFirstHoldAndStopsAfterTheLast(t *testing.T) {
	starts, stops := 0, 0
	k := New(func() func() {
		starts++
		return func() { stops++ }
	})
	a := k.Hold()
	b := k.Hold()
	if starts != 1 || stops != 0 {
		t.Fatalf("two holds: %d starts, %d stops", starts, stops)
	}
	a()
	a()
	if stops != 0 {
		t.Fatal("stopped while b holds")
	}
	b()
	if starts != 1 || stops != 1 {
		t.Fatalf("released: %d starts, %d stops", starts, stops)
	}
	c := k.Hold()
	c()
	if starts != 2 || stops != 2 {
		t.Fatalf("again: %d starts, %d stops", starts, stops)
	}
}
