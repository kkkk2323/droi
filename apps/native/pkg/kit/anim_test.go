package kit

import (
	"bytes"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/theme"
)

func TestEveryAnimationFrameFallsOnAStep(t *testing.T) {
	step := func(ms int) int { return ms * AnimationSteps / 1000 }
	for ms := range 1000 {
		d := stepDelay(ms)
		if d < 1 {
			t.Fatalf("at %d ms the next frame is %d ms away", ms, d)
		}
		if ms+d > 1000 {
			t.Fatalf("at %d ms the next frame, %d ms, is past the second", ms, ms+d)
		}
		if next := ms + d; next < 1000 && step(next) != step(ms)+1 {
			t.Fatalf("at %d ms the next frame, %d ms, is step %d, not %d", ms, next, step(next), step(ms)+1)
		}
		if ms+d < 1000 && step(ms+d-1) != step(ms) {
			t.Fatalf("at %d ms the next frame, %d ms, skips a step", ms, ms+d)
		}
	}
}

// The animations move in frames that only paint: they change the picture
// without building the view again.
func TestAnimationsMoveWithoutBuildingTheView(t *testing.T) {
	k := New(theme.Light, "", "", 1)
	for name, draw := range map[string]func(c *ui.Context){
		"spinner": func(c *ui.Context) { k.Spinner(c, 16, k.T.Foreground) },
		"pulse":   func(c *ui.Context) { PulseDot(c, 16, func(w float32) ui.Color { return k.T.Foreground.Alpha(w) }) },
	} {
		t.Run(name, func(t *testing.T) {
			builds := 0
			tt := ui.NewTester(func(c *ui.Context) { builds++; draw(c) }, 32, 32)
			tt.Advance(100 * time.Millisecond)
			built, before := builds, bytes.Clone(tt.Image().Pix)
			tt.Advance(300 * time.Millisecond)
			if builds != built {
				t.Errorf("the view was built %d more times", builds-built)
			}
			moved := bytes.Clone(tt.Image().Pix)
			if bytes.Equal(before, moved) {
				t.Error("the picture did not move")
			}
			tt.Advance(300 * time.Millisecond)
			if bytes.Equal(moved, tt.Image().Pix) {
				t.Error("the picture stopped moving")
			}
		})
	}
}

// Spinners that appear at different times turn together.
func TestSpinnersTurnTogether(t *testing.T) {
	k := New(theme.Light, "", "", 1)
	second := false
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Children(func() {
			k.Spinner(c, 16, k.T.Foreground)
			if second {
				k.Spinner(c, 16, k.T.Foreground)
			}
		})
	}, 32, 16)
	tt.Advance(300 * time.Millisecond)
	second = true
	tt.Frame()
	tt.Advance(100 * time.Millisecond)
	img := tt.Image()
	// Where a spinner's stroke points: the angle of its ink's centroid.
	angle := func(left int) float64 {
		var sx, sy float64
		for y := range 16 {
			for x := range 16 {
				ink := 255 - float64(img.RGBAAt(left+x, y).R)
				sx += ink * (float64(x) - 7.5)
				sy += ink * (float64(y) - 7.5)
			}
		}
		return math.Atan2(sy, sx) * 180 / math.Pi
	}
	a, b := angle(0), angle(16)
	if d := math.Abs(math.Remainder(a-b, 360)); d > 5 {
		t.Fatalf("the spinners point %.0f° and %.0f° apart", a, b)
	}
}
