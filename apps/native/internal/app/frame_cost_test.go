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
// Sessions, as a spinner redraws it about 24 times a second: on the New
// session page, and with a Session open that called subagents. The history
// is shaped like a real one: 4379 Sessions in 252 Workspaces, 163 of them
// changed in the last 3 days and 618 in the last 30, and 372 subagents, 6 of
// them called by the Session opened. Run it with DROI_FRAME_COST=1 (skipped
// in a normal run).
func TestFrameCost(t *testing.T) {
	if os.Getenv("DROI_FRAME_COST") == "" {
		t.Skip("set DROI_FRAME_COST=1 to measure")
	}
	// The Fake Daemon cannot start with more than about 5000 Sessions.
	const sessionCount, subagentCount, frames = 4379, 372, 300
	const day = 24 * 60 * 60
	now := time.Now().Unix()
	id := func(i int) string { return fmt.Sprintf("%08x-0000-4000-8000-%012x", i, i) }
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
		sc.Sessions = append(sc.Sessions, fakedaemon.SessionSpec{
			Title:    fmt.Sprintf("Task %d", i),
			Cwd:      fmt.Sprintf("/Users/dev/project-%d", workspace),
			Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}},
			Extra:    map[string]any{"sessionId": id(i), "updatedAt": now - int64(age)},
		})
		first[id(i)] = float64(now-int64(age)) * 1000
	}
	for j := range subagentCount {
		caller := 0
		if j >= 6 {
			caller = j % 618
		}
		sc.Sessions = append(sc.Sessions, fakedaemon.SessionSpec{
			Title:    fmt.Sprintf("Worker: step %d", j),
			Cwd:      "/Users/dev/project-0",
			Messages: []fakedaemon.Message{{Role: "user", Text: "do it"}},
			Extra: map[string]any{"sessionId": id(sessionCount + j), "updatedAt": now - int64(j*600), "subagent": map[string]any{
				"callingSessionId": id(caller), "callingToolUseId": fmt.Sprintf("toolu_%d", j), "subagentType": "worker",
				"description": fmt.Sprintf("step %d", j), "status": "completed",
			}},
		})
	}
	prefs.SessionsFirstSeen.Set(store, first)
	h := newHarnessWith(t, sc, "", func(cfg *Config) { cfg.Prefs = store })
	h.until("the list", func() bool { return h.hasText("Task 0") })

	measure := func(where string) {
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
		t.Logf("%s: frame %v, %.1f MB allocated, %.0f GCs per 1000 frames", where,
			elapsed/frames, float64(after.TotalAlloc-before.TotalAlloc)/frames/1e6, float64(after.NumGC-before.NumGC)*1000/frames)
	}
	measure("new session page")
	h.a.Go(Route{Name: "session", SessionID: id(0)})
	h.until("the transcript", func() bool { return h.hasText("hi") })
	measure("session with subagents")
}
