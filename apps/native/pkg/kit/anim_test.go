package kit

import "testing"

func TestEveryAnimationFrameFallsOnAStep(t *testing.T) {
	step := func(ms int) int { return ms * AnimationSteps / 1000 }
	for ms := range 1000 {
		d := stepDelay(ms)
		if d < 1 {
			t.Fatalf("at %d ms the next frame is %d ms away", ms, d)
		}
		if next := ms + d; next < 1000 && step(next) != step(ms)+1 {
			t.Fatalf("at %d ms the next frame, %d ms, is step %d, not %d", ms, next, step(next), step(ms)+1)
		}
		if ms+d > 1000 {
			t.Fatalf("at %d ms the next frame, %d ms, is past the second", ms, ms+d)
		}
		if ms+d < 1000 && step(ms+d-1) != step(ms) {
			t.Fatalf("at %d ms the next frame, %d ms, skips a step", ms, ms+d)
		}
	}
}
