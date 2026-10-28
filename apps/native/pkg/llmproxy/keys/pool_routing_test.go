package keys

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
)

// usageWithPools builds a token-rate-limits usage entry with the given Standard
// and Core percentages. Used=effective mirrors parseUsageResponse.
func usageWithPools(key string, stdPct, corePct float64) *KeyUsage {
	std, core, total := stdPct, corePct, 100.0
	used := stdPct
	if stdPct >= 100 {
		used = corePct
	}
	return &KeyUsage{
		Key:             key,
		StandardPercent: &std,
		CorePercent:     &core,
		Total:           &total,
		Used:            &used,
		LastCheckedAt:   testNow(),
	}
}

func testRotator(keys ...string) *KeyRotator {
	return &KeyRotator{keys: keys, usages: make(map[string]*KeyUsage)}
}

// Regression for the reported bug: a Standard-only model (claude-fable-5.1)
// must never be routed to a key whose Standard pool is exhausted while its Core
// pool is healthy, even though the effective-usage heuristic considers that key
// partially used.
func TestSelectKeyForModel_StandardModelSkipsStandardExhaustedKey(t *testing.T) {
	r := testRotator("key-spent", "key-fresh")
	r.usages["key-spent"] = usageWithPools("key-spent", 100, 60)
	r.usages["key-fresh"] = usageWithPools("key-fresh", 0, 0)

	key, pool := r.SelectKeyForModel("", "claude-fable-5.1", nil)
	if key != "key-fresh" || pool != PoolStandard {
		t.Fatalf("expected key-fresh/standard, got %s/%s", key, pool)
	}
}

// A Standard-only model must not keep using a presented key whose Standard pool
// is exhausted; it should rotate to one with Standard headroom.
func TestSelectKeyForModel_PresentedStandardExhaustedRotates(t *testing.T) {
	r := testRotator("key-spent", "key-fresh")
	r.usages["key-spent"] = usageWithPools("key-spent", 100, 60)
	r.usages["key-fresh"] = usageWithPools("key-fresh", 0, 0)

	key, pool := r.SelectKeyForModel("key-spent", "claude-fable-5.1", nil)
	if key != "key-fresh" || pool != PoolStandard {
		t.Fatalf("expected rotation to key-fresh/standard, got %s/%s", key, pool)
	}
}

func TestSelectKeyForModel_CoreModelUsesCoreWhenStandardExhausted(t *testing.T) {
	r := testRotator("key-spent")
	r.usages["key-spent"] = usageWithPools("key-spent", 100, 40)

	key, pool := r.SelectKeyForModel("", "kimi-k3", nil)
	if key != "key-spent" || pool != PoolCore {
		t.Fatalf("expected key-spent/core, got %s/%s", key, pool)
	}
}

// A Core-capable model prefers a key whose Standard pool is already exhausted
// (billing its Core quota) over a Standard-healthy key: Factory bills Standard
// first, so spreading Core-capable traffic across Standard-healthy keys would
// drain every key's Standard pool together and leave Core unreachable, while
// concentrating it preserves Standard headroom for Standard-only models.
func TestSelectKeyForModel_CoreModelPrefersCoreOfStandardExhaustedKey(t *testing.T) {
	r := testRotator("key-spent", "key-fresh")
	r.usages["key-spent"] = usageWithPools("key-spent", 100, 10)
	r.usages["key-fresh"] = usageWithPools("key-fresh", 5, 0)

	key, pool := r.SelectKeyForModel("", "glm-5.3-flash", nil)
	if key != "key-spent" || pool != PoolCore {
		t.Fatalf("expected key-spent/core, got %s/%s", key, pool)
	}
}

// usageWithWeekly builds a pool-usage entry whose Standard pool also reports a
// weekly bucket at weeklyPct% with weeklyHoursLeft remaining, for
// concentration tests.
func usageWithWeekly(key string, stdPct, weeklyPct, weeklyHoursLeft float64) *KeyUsage {
	u := usageWithPools(key, stdPct, 0)
	u.StandardLimits = &PoolLimits{
		Weekly: &BucketLimit{
			UsedPercent: weeklyPct,
			WindowEnd:   time.Now().Add(time.Duration(weeklyHoursLeft * float64(time.Hour))).UTC().Format(time.RFC3339),
		},
	}
	return u
}

// Core-capable traffic concentrates on the Standard-healthy key furthest into
// its weekly bucket (highest burn under the pace cap) so it reaches the weekly
// cap and spills into Core, even when another key has higher raw usage.
func TestSelectKeyForModel_CoreModelConcentratesOnDeepestStandardWeek(t *testing.T) {
	r := testRotator("key-shallow", "key-deep")
	r.usages["key-shallow"] = usageWithWeekly("key-shallow", 60, 60, 10) // burn ≈ 0.64
	r.usages["key-deep"] = usageWithWeekly("key-deep", 55, 55, 100)      // burn ≈ 1.36

	key, pool := r.SelectKeyForModel("", "kimi-k3", nil)
	if key != "key-deep" || pool != PoolStandard {
		t.Fatalf("expected key-deep/standard (highest weekly burn under the pace cap), got %s/%s", key, pool)
	}
}

// A key burning its weekly Standard bucket faster than the pace cap yields
// Core-capable traffic to a paced key despite higher raw usage.
func TestSelectKeyForModel_CoreModelPaceCapYieldsToSlowerKey(t *testing.T) {
	r := testRotator("key-overcooked", "key-steady")
	r.usages["key-overcooked"] = usageWithWeekly("key-overcooked", 80, 80, 156) // burn ≈ 11 (fresh window)
	r.usages["key-steady"] = usageWithWeekly("key-steady", 30, 30, 84)          // burn ≈ 0.6

	key, _ := r.SelectKeyForModel("", "kimi-k3", nil)
	if key != "key-steady" {
		t.Fatalf("expected key-steady (overcooked is past the pace cap), got %q", key)
	}
}

// The Core-before-Standard preference is soft: a Core-billing key whose
// 5-hour bucket is nearly full spills new traffic to a calmer Standard-live
// peer instead of absorbing every Core-capable request.
func TestSelectKeyForModel_CoreModelHotCoreKeySpillsToStandardPeer(t *testing.T) {
	r := testRotator("key-spent", "key-fresh")
	spent := usageWithPools("key-spent", 100, 40)
	spent.CoreLimits = &PoolLimits{
		FiveHour: &BucketLimit{UsedPercent: 95, WindowEnd: time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)},
	}
	r.usages["key-spent"] = spent
	fresh := usageWithPools("key-fresh", 50, 0)
	fresh.StandardLimits = &PoolLimits{
		FiveHour: &BucketLimit{UsedPercent: 10, WindowEnd: time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)},
	}
	r.usages["key-fresh"] = fresh

	key, pool := r.SelectKeyForModel("", "kimi-k3", nil)
	if key != "key-fresh" || pool != PoolStandard {
		t.Fatalf("expected key-fresh/standard (key-spent's core 5h bucket is hot), got %s/%s", key, pool)
	}
}

// A Standard-pool cooldown must not remove a key from Core rotation.
func TestMarkKeyPoolError_ScopesCooldownToPool(t *testing.T) {
	r := NewKeyRotator(config.New(), []string{"key-a", "key-b"})
	r.mu.Lock()
	r.usages["key-a"] = usageWithPools("key-a", 10, 10)
	r.mu.Unlock()
	r.MarkKeyPoolError("key-a", PoolStandard, http.StatusTooManyRequests)

	if key, _ := r.SelectKeyForModel("", "claude-fable-5.1", nil); key != "key-b" {
		t.Fatalf("expected key-b while key-a standard is cooled, got %q", key)
	}
	if key, pool := r.SelectKeyForModel("", "kimi-k3", nil); key != "key-a" || pool != PoolCore {
		t.Fatalf("expected key-a/core (core pool untouched), got %s/%s", key, pool)
	}
}

func TestSelectKeyForModel_ExcludedKeysAreNotReused(t *testing.T) {
	r := testRotator("key-a", "key-b")
	r.usages["key-a"] = usageWithPools("key-a", 0, 0)
	r.usages["key-b"] = usageWithPools("key-b", 0, 0)

	key, _ := r.SelectKeyForModel("", "claude-fable-5.1", map[string]struct{}{"key-a": {}})
	if key != "key-b" {
		t.Fatalf("expected key-b, got %q", key)
	}

	key, _ = r.SelectKeyForModel("", "claude-fable-5.1", map[string]struct{}{"key-a": {}, "key-b": {}})
	if key != "" {
		t.Fatalf("expected no key when all are excluded, got %q", key)
	}
}

func TestParseUsageResponse_PoolResetsForExhaustedBuckets(t *testing.T) {
	body := `{
		"usesTokenRateLimitsBilling": true,
		"overagePreference": "droidCore",
		"limits": {
			"standard": {
				"fiveHour": {"usedPercent": 100.0, "windowEnd": "2026-06-19T15:00:00Z"},
				"weekly":   {"usedPercent": 95.0,  "windowEnd": "2026-06-23T00:00:00Z"}
			},
			"core": {
				"fiveHour": {"usedPercent": 20.0, "windowEnd": "2026-06-19T15:00:00Z"}
			}
		}
	}`
	usage := parseUsageResponse("test-key", []byte(body))
	if usage.StandardResetMs <= 0 {
		t.Fatalf("expected a Standard reset for the exhausted 5h bucket, got %v", usage.StandardResetMs)
	}
	if usage.CoreResetMs != 0 {
		t.Fatalf("expected no Core reset (healthy), got %v", usage.CoreResetMs)
	}
}

func TestFetchAllUsages_PreservesPoolCooldownAcrossRefresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"usesTokenRateLimitsBilling": true,
			"overagePreference": "droidCore",
			"limits": {"standard": {"fiveHour": {"usedPercent": 10, "windowEnd": "2026-06-19T15:00:00Z"}}}
		}`))
	}))
	defer server.Close()

	cfg := config.New()
	cfg.UsageAPIURL = server.URL
	cfg.ChatUsageAPIURL = "http://should-not-be-called"
	r := NewKeyRotator(cfg, []string{"key-a"})
	r.FetchAllUsages(context.Background())

	r.MarkKeyPoolError("key-a", PoolStandard, http.StatusPaymentRequired)
	r.FetchAllUsages(context.Background())

	r.mu.RLock()
	u := r.usages["key-a"]
	r.mu.RUnlock()
	if u == nil || u.StandardPoolCooldownUntil.IsZero() {
		t.Fatalf("expected Standard pool cooldown to survive refresh, got %+v", u)
	}
	if u.Error != "" {
		t.Fatalf("billing Error should stay empty, got %q", u.Error)
	}
}

// usageWithMonth is a Standard usage entry whose month ends after monthLeft
// at monthlyPct used, with a live 5-hour bucket at fiveHourPct.
func usageWithMonth(key string, monthlyPct float64, monthLeft time.Duration, fiveHourPct float64) *KeyUsage {
	u := usageWithPools(key, max(monthlyPct, fiveHourPct), 0)
	u.StandardLimits = &PoolLimits{
		FiveHour: &BucketLimit{UsedPercent: fiveHourPct, WindowEnd: time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)},
		Monthly:  &BucketLimit{UsedPercent: monthlyPct, WindowEnd: time.Now().Add(monthLeft).UTC().Format(time.RFC3339)},
	}
	return u
}

// Droid stays on the key it sent, except for a key whose month ends within
// ExpiringWithin: what is left of that month is lost unless droid moves to it.
func TestSelectKeyForModel_PresentedKeyYieldsToExpiringMonth(t *testing.T) {
	day := 24 * time.Hour
	for _, c := range []struct {
		name  string
		usage []*KeyUsage
		want  string
	}{
		{"an expiring month wins", []*KeyUsage{
			usageWithMonth("key-sent", 4, 25*day, 30),
			usageWithMonth("key-later", 50, 6*day, 0),
			usageWithMonth("key-expiring", 61, 3*day+16*time.Hour, 0),
		}, "key-expiring"},
		{"a month beyond the horizon does not", []*KeyUsage{
			usageWithMonth("key-sent", 4, 25*day, 30),
			usageWithMonth("key-other", 53, 8*day, 0),
		}, "key-sent"},
		{"a nearly full 5-hour bucket waits", []*KeyUsage{
			usageWithMonth("key-sent", 4, 25*day, 30),
			usageWithMonth("key-expiring", 61, 3*day, 92),
		}, "key-sent"},
		{"an expiring key that is sent stays", []*KeyUsage{
			usageWithMonth("key-sent", 61, 2*day, 30),
			usageWithMonth("key-expiring", 40, 5*day, 0),
		}, "key-sent"},
		{"a sooner expiring month wins over the sent one", []*KeyUsage{
			usageWithMonth("key-sent", 40, 5*day, 30),
			usageWithMonth("key-expiring", 61, 2*day, 0),
		}, "key-expiring"},
		{"a spent month is left", []*KeyUsage{
			usageWithMonth("key-sent", 4, 25*day, 30),
			usageWithMonth("key-expiring", 98, 2*day, 0),
		}, "key-sent"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var names []string
			for _, u := range c.usage {
				names = append(names, u.Key)
			}
			r := testRotator(names...)
			for _, u := range c.usage {
				r.usages[u.Key] = u
			}
			for _, model := range []string{"claude-opus-5-5", "glm-5.3", ""} {
				if key, pool := r.SelectKeyForModel("key-sent", model, nil); key != c.want || pool != PoolStandard {
					t.Errorf("%q: got %s/%s, want %s/standard", model, key, pool, c.want)
				}
			}
		})
	}
}

// A sent key that already bills Core stays on Core for a Core-capable model.
func TestSelectKeyForModel_PresentedCoreKeyIgnoresExpiringMonth(t *testing.T) {
	r := testRotator("key-sent", "key-expiring")
	r.usages["key-sent"] = usageWithPools("key-sent", 100, 10)
	r.usages["key-expiring"] = usageWithMonth("key-expiring", 61, 48*time.Hour, 0)
	if key, pool := r.SelectKeyForModel("key-sent", "glm-5.3", nil); key != "key-sent" || pool != PoolCore {
		t.Fatalf("got %s/%s, want key-sent/core", key, pool)
	}
}
