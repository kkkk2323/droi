package automationwork

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/automations"
)

// Run with -race: every entry point at once, then every run ended, and no
// keep-awake left held.
func TestTheSchedulerUnderConcurrentUse(t *testing.T) {
	h := newHarness(t, "08:00")
	for i := range 3 {
		h.save(automations.Automation{Name: fmt.Sprint("often ", i), Schedule: "* * * * *"})
	}
	ctx := context.Background()
	const rounds = 30
	var wg sync.WaitGroup
	wg.Go(func() {
		for range rounds {
			h.mu.Lock()
			h.clock = h.clock.Add(time.Minute)
			h.mu.Unlock()
			h.s.Tick(ctx)
		}
	})
	wg.Go(func() {
		for i := range rounds {
			_, _ = h.s.RunNow(ctx, fmt.Sprint("often-", i%3))
		}
	})
	wg.Go(func() {
		for range rounds {
			for _, r := range h.s.Active() {
				h.s.Finished(r.RunID, automations.RunSucceeded, "")
			}
		}
	})
	wg.Go(func() {
		for i := range rounds {
			a, err := h.s.Save(automations.Automation{Name: "churn", Schedule: "* * * * *", Prompt: "p"})
			if err != nil {
				t.Error(err)
				return
			}
			if i%2 == 0 {
				a.Schedule = "*/2 * * * *"
				// The other goroutine may have deleted it.
				if _, err := h.s.Save(a); err != nil && !errors.Is(err, automations.ErrNotFound) {
					t.Error(err)
				}
			}
			_ = h.s.SetPaused(a.ID, i%3 == 0)
		}
	})
	wg.Go(func() {
		for i := range rounds {
			_ = h.s.Delete("churn")
			_ = h.s.Delete(fmt.Sprint("churn-", i%5+2))
		}
	})
	wg.Go(func() {
		for range rounds {
			items, err := h.s.List()
			if err != nil {
				t.Error(err)
				return
			}
			for _, it := range items {
				_, _ = h.s.Runs(it.ID, 5)
			}
		}
	})
	wg.Wait()
	for _, r := range h.s.Active() {
		h.s.Finished(r.RunID, automations.RunSucceeded, "")
	}
	if h.holds() != 0 || len(h.s.Active()) != 0 {
		t.Fatalf("holds %d, active %+v", h.holds(), h.s.Active())
	}
	items, err := h.s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Running || it.Invalid != "" {
			t.Errorf("item %+v", it)
		}
	}
}

func TestTheHandledTimeNeverGoesBack(t *testing.T) {
	h := newHarness(t, "08:00")
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	later, earlier := at(t, "2026-11-05T09:00:00+08:00"), at(t, "2026-11-05T08:30:00+08:00")
	if !h.s.markHandled("a", later) || h.s.markHandled("a", earlier) || !h.s.state.LastScheduled["a"].Equal(later) {
		t.Fatalf("handled %s", h.s.state.LastScheduled["a"])
	}
}
