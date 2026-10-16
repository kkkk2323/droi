package app

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// frameBudget is the most a frame of the window may allocate over the long
// history below. Such frames came 24 times a second while a spinner turned:
// when each decoded the Sessions' first-seen times (2.4 MB) or copied the
// whole list (0.6 MB), the app took a third of a CPU at rest. 190 to 290 KB
// in October 2026; part of that still grows with the history, not with the
// rows on screen.
const frameBudget = 512 << 10

// TestAFrameOverALongHistoryStaysCheap draws the window over a history of
// Sessions shaped like a real one, as a spinner redraws it: on the New
// session page, and with a Session open that called subagents. Run it with
// -v for each frame's time and allocations.
func TestAFrameOverALongHistoryStaysCheap(t *testing.T) {
	// The Fake Daemon cannot start with more than about 5000 Sessions.
	const sessionCount, subagentCount, frames = 4379, 372, 200
	sc, store := longHistory(sessionCount, subagentCount)
	h := newHarnessWith(t, sc, "", func(cfg *Config) { cfg.Prefs = store })
	h.until("the list", func() bool { return h.hasText("Task 0") })

	measure := func(where string) {
		t.Helper()
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
		bytes := (after.TotalAlloc - before.TotalAlloc) / frames
		t.Logf("%s: frame %v, %d KB allocated, %.0f GCs per 1000 frames", where,
			elapsed/frames, bytes>>10, float64(after.NumGC-before.NumGC)*1000/frames)
		if bytes > frameBudget {
			t.Errorf("%s: a frame allocates %d KB, more than %d KB", where, bytes>>10, frameBudget>>10)
		}
	}
	measure("new session page")
	h.a.Go(Route{Name: "session", SessionID: historyID(0)})
	h.until("the transcript", func() bool { return h.hasText("hi") })
	measure("session with subagents")
}

// historyID is the id of the i-th Session of longHistory.
func historyID(i int) string { return fmt.Sprintf("%08x-0000-4000-8000-%012x", i, i) }

// longHistory is a history of Sessions shaped like a real one: of the first
// 4379, 163 in 40 Workspaces changed in the last 3 days, 455 more in 118 in
// the last 30 and the rest in 252 over the year; the subagents follow, the
// first 6 called by Session 0. The Sessions' first-seen times and two pinned
// Sessions go in the returned preferences, as the window keeps them.
func longHistory(sessionCount, subagentCount int) (fakedaemon.Scenario, *prefs.Store) {
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
		sc.Sessions = append(sc.Sessions, fakedaemon.SessionSpec{
			Title:    fmt.Sprintf("Task %d", i),
			Cwd:      fmt.Sprintf("/Users/dev/project-%d", workspace),
			Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}},
			Extra:    map[string]any{"sessionId": historyID(i), "updatedAt": now - int64(age)},
		})
		first[historyID(i)] = float64(now-int64(age)) * 1000
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
			Extra: map[string]any{"sessionId": historyID(sessionCount + j), "updatedAt": now - int64(j*600), "subagent": map[string]any{
				"callingSessionId": historyID(caller), "callingToolUseId": fmt.Sprintf("toolu_%d", j), "subagentType": "worker",
				"description": fmt.Sprintf("step %d", j), "status": "completed",
			}},
		})
	}
	prefs.SessionsFirstSeen.Set(store, first)
	prefs.PinnedSessions.Set(store, []string{historyID(1), historyID(2)})
	return sc, store
}
