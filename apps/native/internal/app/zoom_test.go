package app

import (
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

func TestZoomStepsThroughChromiumsLevels(t *testing.T) {
	a := New(Config{})
	for _, c := range []struct {
		step int
		want float64
		rem  float32
	}{{1, 1.1, 17.6}, {1, 1.25, 20}, {-1, 1.1, 17.6}, {-1, 1, 16}, {-1, 0.9, 14.4}, {0, 1, 16}} {
		a.Zoom(c.step)
		if got := prefs.Zoom.Get(a.prefs); got != c.want {
			t.Fatalf("step %d: zoom %v, want %v", c.step, got, c.want)
		}
		if a.kit.Rem != c.rem {
			t.Fatalf("step %d: rem %v, want %v", c.step, a.kit.Rem, c.rem)
		}
	}
	for range 30 {
		a.Zoom(1)
	}
	if got := prefs.Zoom.Get(a.prefs); got != 5 {
		t.Fatalf("zoom stops at 500%%, got %v", got)
	}
}
