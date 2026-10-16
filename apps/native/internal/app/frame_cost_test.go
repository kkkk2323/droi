package app

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// TestFrameCost measures a frame of the window over a long history of
// Sessions, as a spinner redraws it about 24 times a second. The history is
// shaped like a real one: 4751 Sessions in 252 Workspaces, 163 of them
// changed in the last 3 days and 618 in the last 30. Run it with
// DROI_FRAME_COST=1 (skipped in a normal run).
func TestFrameCost(t *testing.T) {
	if os.Getenv("DROI_FRAME_COST") == "" {
		t.Skip("set DROI_FRAME_COST=1 to measure")
	}
	const sessionCount, frames = 4751, 300
	const day = 24 * 60 * 60
	now := time.Now().Unix()
	sc := fakedaemon.Scenario{}
	store := prefs.Memory()
	first := map[string]float64{}
	for i := range sessionCount {
		var age, workspace int
		switch {
		case i < 163:
			age, workspace = i*3*day/163, i%40
		case i < 618:
			age, workspace = 3*day+(i-163)*27*day/455, i%118
		default:
			age, workspace = 30*day+(i-618)*335*day/4133, i%252
		}
		id := fmt.Sprintf("%08x-0000-4000-8000-%012x", i, i)
		sc.Sessions = append(sc.Sessions, fakedaemon.SessionSpec{
			Title:    fmt.Sprintf("Task %d", i),
			Cwd:      fmt.Sprintf("/Users/dev/project-%d", workspace),
			Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}},
			Extra:    map[string]any{"sessionId": id, "updatedAt": now - int64(age)},
		})
		first[id] = float64(now-int64(age)) * 1000
	}
	prefs.SessionsFirstSeen.Set(store, first)
	h := newHarnessWith(t, sc, "", func(cfg *Config) { cfg.Prefs = store })
	h.until("the list", func() bool { return h.hasText("Task 0") })
	for range 20 {
		h.tt.Frame()
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	for range frames {
		h.tt.Frame()
	}
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	t.Logf("frame: %v, %.1f MB allocated, %.0f GCs per 1000 frames",
		elapsed/frames, float64(after.TotalAlloc-before.TotalAlloc)/frames/1e6, float64(after.NumGC-before.NumGC)*1000/frames)

	start = time.Now()
	for range frames {
		h.a.sortOrder(nil)
	}
	t.Logf("sortOrder: %v", time.Since(start)/frames)
}
