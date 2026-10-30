package llmusage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

func planKey(index int, plan string, standard *keys.PoolLimits) keys.KeyState {
	k := okKey(index, standard)
	k.Plan = plan
	return k
}

// A percent of a Max key's window holds ten of a Pro key's: the pool's
// totals, its average, the runway and the credits count it so.
func TestABiggerPlanCountsAsSeveralKeys(t *testing.T) {
	now := time.Now()
	month := func(used float64) *keys.PoolLimits {
		return &keys.PoolLimits{Monthly: bucket(used, now.Add(20*24*time.Hour))}
	}
	pool := []keys.KeyState{planKey(1, "team_annual", month(50)), planKey(2, "max", month(90))}
	s := SummarizeWindow(pool, Standard, Monthly, now)
	if s.TotalRemaining != 50+100 || s.Capacity != 1100 || s.KeyEquivalent() != 1.5 {
		t.Fatalf("summary %+v", s)
	}
	if avg := s.AverageRemaining(); avg < 13.6 || avg > 13.7 {
		t.Fatalf("average %v, want 150/1100", avg)
	}
	r, _ := NewRunway(pool, now)
	if r.Capacity() != 1100 {
		t.Fatalf("runway capacity %v", r.Capacity())
	}
	// 150 points of a Pro month are left until the months end in 20 days.
	if _, lasts := r.Lasts(7); !lasts {
		t.Error("7 points a day do not last")
	}
	if days, lasts := r.Lasts(8); lasts || days < 18 || days > 20 {
		t.Errorf("8 points a day: %v days, lasts %v", days, lasts)
	}
	card := BuildKeyCard(pool[1], now)
	if card.Plan != "Max" || !strings.Contains(card.PlanHelp, "10") {
		t.Fatalf("card %q %q", card.Plan, card.PlanHelp)
	}
	pro := BuildKeyCard(pool[0], now)
	if pro.Plan != "Pro" || pro.Pools[0].Rows[2].Credits != BuildKeyCard(planKey(1, "", month(50)), now).Pools[0].Rows[2].Credits {
		t.Fatal("a Pro key's credits are not a key's of unknown plan")
	}
	maxCredits, _ := WindowCredits(Monthly, 100)
	if got := card.Pools[0].Rows[2].Credits; got != "≈ "+FormatCredits(maxCredits) {
		t.Fatalf("a Max key with 10%% of its month left shows %s", got)
	}
}

// The spend of a Max key's month rises ten times as far as a Pro key's.
func TestSpendCountsABiggerPlan(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	end := now.AddDate(0, 0, 10)
	var b []byte
	for i, used := range []float64{1, 3} {
		line, _ := json.Marshal(keys.UsageSnapshot{Time: now.Add(time.Duration(i-2) * time.Hour), Index: 1, Plan: "max", Standard: &keys.PoolLimits{Monthly: bucket(used, end)}})
		b = append(append(b, line...), '\n')
	}
	os.WriteFile(path, b, 0o600)
	if s, ok := ReadSpend(path, now); !ok || s.Today != 20 {
		t.Fatalf("spend %+v %v", s, ok)
	}
}
