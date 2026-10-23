package keys

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSnapshot_ExposesWindowLimitsAndResetTimestamp(t *testing.T) {
	body := `{
		"usesTokenRateLimitsBilling": true,
		"overagePreference": "extraUsage",
		"extraUsageBalance": 1250,
		"limits": {
			"standard": {
				"fiveHour": {"usedPercent": 100.0, "windowEnd": "2026-06-19T15:00:00Z"},
				"weekly":   {"usedPercent": 40.0,  "windowEnd": "2026-06-23T00:00:00Z"},
				"monthly":  {"usedPercent": 12.5,  "windowEnd": "2026-07-01T00:00:00Z"}
			},
			"core": {
				"fiveHour": {"usedPercent": 20.0, "windowEnd": "2026-06-19T15:00:00Z"}
			}
		}
	}`
	usage := parseUsageResponse("fk-window-1234567890", []byte(body))
	r := testRotator("fk-window-1234567890")
	r.usages[usage.Key] = usage

	states := r.Snapshot()
	if len(states) != 1 {
		t.Fatalf("expected 1 state, got %d", len(states))
	}
	ks := states[0]

	if ks.Key == "fk-window-1234567890" {
		t.Fatalf("snapshot must mask the key, got %q", ks.Key)
	}
	if ks.Limits == nil || ks.Limits.Standard == nil || ks.Limits.Core == nil {
		t.Fatalf("expected both pools' window limits, got %+v", ks.Limits)
	}
	if ks.Limits.Standard.Weekly == nil || ks.Limits.Standard.Weekly.UsedPercent != 40 {
		t.Fatalf("expected standard weekly 40%%, got %+v", ks.Limits.Standard.Weekly)
	}
	if ks.Limits.Core.Weekly != nil {
		t.Fatalf("core weekly should be absent, got %+v", ks.Limits.Core.Weekly)
	}
	if ks.OveragePreference != "extraUsage" || ks.ExtraUsageBalance == nil || *ks.ExtraUsageBalance != 1250 {
		t.Fatalf("expected overage fields to be passed through, got %+v", ks)
	}

	want := time.Date(2026, 6, 19, 15, 0, 0, 0, time.UTC)
	if ks.ResetsAt == nil || !ks.ResetsAt.Equal(want) {
		t.Fatalf("expected resetsAt %v (nearest standard window), got %v", want, ks.ResetsAt)
	}

	// The wire format must stay RFC3339 so the menu bar app can parse it.
	raw, err := json.Marshal(ks)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["resets"]; ok {
		t.Fatalf("legacy resets string must not be emitted: %s", raw)
	}
	if s, _ := decoded["resetsAt"].(string); s == "" {
		t.Fatalf("expected resetsAt string in JSON: %s", raw)
	} else if _, err := time.Parse(time.RFC3339, s); err != nil {
		t.Fatalf("resetsAt is not RFC3339: %q", s)
	}
}

func TestSnapshot_NoUsageDataOmitsLimits(t *testing.T) {
	r := testRotator("fk-empty-1234567890")
	ks := r.Snapshot()[0]
	if ks.Status != "no_usage_data" || ks.Limits != nil || ks.ResetsAt != nil {
		t.Fatalf("unexpected state for key without usage: %+v", ks)
	}
}
