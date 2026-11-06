package keepawake

import (
	"os"
	"slices"
	"strconv"
	"testing"
)

func TestHoldRunsCaffeinateUntilTheLastRelease(t *testing.T) {
	var ran [][]string
	stopped := 0
	old := command
	command = func(name string, args ...string) (func(), error) {
		ran = append(ran, append([]string{name}, args...))
		return func() { stopped++ }, nil
	}
	t.Cleanup(func() { command = old })

	a, b := Hold(), Hold()
	want := []string{"/usr/bin/caffeinate", "-i", "-w", strconv.Itoa(os.Getpid())}
	if len(ran) != 1 || !slices.Equal(ran[0], want) {
		t.Fatalf("ran %v, want once %v", ran, want)
	}
	a()
	if stopped != 0 {
		t.Fatal("stopped with a hold left")
	}
	b()
	if stopped != 1 {
		t.Fatalf("stopped %d times", stopped)
	}
}
