package keys

import (
	"fmt"
	"testing"
	"time"
)

// limitsBody builds a billing/limits response shaped like Factory's (see
// app.factory.ai/api/billing/limits): only the Standard 5h and weekly buckets
// vary, Core stays unused.
func limitsBody(fiveHour, weekly float64) []byte {
	end5h := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	endWk := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	return []byte(fmt.Sprintf(`{
		"usesTokenRateLimitsBilling": true,
		"overagePreference": "droidCore",
		"limits": {
			"standard": {
				"fiveHour": {"usedPercent": %v, "windowEnd": %q},
				"weekly":   {"usedPercent": %v, "windowEnd": %q},
				"monthly":  {"usedPercent": %v, "windowEnd": %q}
			},
			"core": {
				"fiveHour": {"usedPercent": 0, "windowEnd": null},
				"weekly":   {"usedPercent": 0, "windowEnd": null},
				"monthly":  {"usedPercent": 0, "windowEnd": null}
			}
		}
	}`, fiveHour, end5h, weekly, endWk, weekly, endWk))
}

// A key whose Standard 5-hour bucket is exhausted must not receive Standard
// traffic even though its weekly/monthly buckets still have headroom.
func TestSelectKeyForModel_StandardFiveHourExhaustedKeyIsSkipped(t *testing.T) {
	// Both keys are hot, so the spill ordering cannot decide; only treating the
	// full 5h bucket as exhausted keeps key-5h-spent (the more used one by
	// weekly) out of rotation.
	r := testRotator("key-5h-spent", "key-ok")
	r.usages["key-5h-spent"] = parseUsageResponse("key-5h-spent", limitsBody(100, 20))
	r.usages["key-ok"] = parseUsageResponse("key-ok", limitsBody(95, 10))

	if key, pool := r.SelectKeyForModel("", "claude-fable-5.1", nil); key != "key-ok" || pool != PoolStandard {
		t.Fatalf("expected key-ok/standard (key-5h-spent's 5h bucket is full), got %s/%s", key, pool)
	}
}

// A key whose Standard 5-hour bucket is hot (>= 90%) yields new traffic to a
// cooler peer even though it would otherwise be preferred (sooner window end).
func TestSelectKeyForModel_StandardFiveHourHotKeySpillsToPeer(t *testing.T) {
	r := testRotator("key-hot", "key-cool")
	r.usages["key-hot"] = parseUsageResponse("key-hot", limitsBody(92, 30))
	r.usages["key-cool"] = parseUsageResponse("key-cool", limitsBody(0, 30))

	if key, _ := r.SelectKeyForModel("", "claude-fable-5.1", nil); key != "key-cool" {
		t.Fatalf("expected key-cool (key-hot's 5h bucket is past the spill ratio), got %s", key)
	}

	// Below the spill ratio the active 5h window keeps the key preferred, so
	// traffic stays concentrated instead of opening windows on fresh keys.
	r.usages["key-hot"] = parseUsageResponse("key-hot", limitsBody(50, 30))
	if key, _ := r.SelectKeyForModel("", "claude-fable-5.1", nil); key != "key-hot" {
		t.Fatalf("expected key-hot below the spill ratio, got %s", key)
	}
}
