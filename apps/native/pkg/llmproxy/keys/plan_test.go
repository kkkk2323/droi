package keys

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A key's plan comes from its organization, read once in a while, not with
// every usage read; a failed read keeps the plan read before.
func TestAKeysPlanIsReadFromItsOrganization(t *testing.T) {
	var asked atomic.Int32
	var down atomic.Bool
	r := addKeyTestRotator(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == organizationPath:
			asked.Add(1)
			if down.Load() {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			tier := "team_annual"
			if strings.Contains(req.Header.Get("Authorization"), "fk-max") {
				tier = "max"
			}
			w.Write([]byte(`{"organization":{"subscription":{"factoryTier":"` + tier + `"}}}`))
		case req.URL.Path == whoamiPath:
			w.Write([]byte(`{"orgId":"org-1"}`))
		default:
			w.Write([]byte(addKeyUsageBody))
		}
	}, "fk-pro-1234567890", "fk-max-1234567890")
	now := time.Now()
	r.SetClockForTest(func() time.Time { return now })
	r.FetchAllUsages(context.Background())
	states := r.Snapshot()
	if states[0].Plan != "team_annual" || states[1].Plan != "max" || asked.Load() != 2 {
		t.Fatalf("plans %q %q, asked %d", states[0].Plan, states[1].Plan, asked.Load())
	}
	r.FetchAllUsages(context.Background())
	if asked.Load() != 2 {
		t.Fatalf("asked again before planTTL: %d", asked.Load())
	}
	now = now.Add(planTTL + time.Minute)
	down.Store(true)
	r.FetchAllUsages(context.Background())
	if asked.Load() != 4 || r.Snapshot()[1].Plan != "max" {
		t.Fatalf("a failed read: asked %d, plan %q", asked.Load(), r.Snapshot()[1].Plan)
	}
}

func TestPlanNamesAndScales(t *testing.T) {
	for tier, want := range map[string]struct {
		name  string
		scale float64
	}{"team": {"Pro", 1}, "team_annual": {"Pro", 1}, "pro_plus": {"Plus", 5}, "max": {"Max", 10}, "ultra": {"Ultra", 100}, "": {"", 1}, "something": {"", 1}} {
		if PlanName(tier) != want.name || PlanScale(tier) != want.scale {
			t.Errorf("%q: %q %v", tier, PlanName(tier), PlanScale(tier))
		}
	}
}

// A day's openings are counted in Pro months: a Max key's month is worth ten,
// so after it nothing else opens that day, though it opens as the day's
// first.
func TestOpeningCountsABiggerPlanAsSeveralKeys(t *testing.T) {
	now := time.Date(2026, 10, 16, 10, 0, 0, 0, time.Local)
	r := openingRotator(t, now)
	r.usages["never"].Plan = "max"
	r.SetScheduledOpening(true)
	if key, ok := r.ClaimNextOpening(); !ok || key != "never" {
		t.Fatalf("claim = %s %v", key, ok)
	}
	if key, ok := r.ClaimNextOpening(); ok {
		t.Fatalf("opened %s after a Max key's month", key)
	}
	if st := r.OpeningStatus(); st.OpenedToday != 10 {
		t.Fatalf("status = %+v", st)
	}
}
