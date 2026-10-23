package keys

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
)

func TestParseUsageResponse_StandardOnly(t *testing.T) {
	body := `{
		"usesTokenRateLimitsBilling": true,
		"overagePreference": "droidCore",
		"limits": {
			"standard": {
				"fiveHour":  {"usedPercent": 42.5, "windowEnd": "2026-06-19T15:00:00Z"},
				"weekly":    {"usedPercent": 67.0, "windowEnd": "2026-06-23T00:00:00Z"},
				"monthly":   {"usedPercent": 80.0, "windowEnd": "2026-07-01T00:00:00Z"}
			}
		}
	}`
	usage := parseUsageResponse("test-key", []byte(body))
	if usage.Error != "" {
		t.Fatalf("unexpected error: %s", usage.Error)
	}
	if usage.StandardPercent == nil || *usage.StandardPercent != 80.0 {
		t.Fatalf("expected standardPercent 80.0 (highest bucket), got %v", usage.StandardPercent)
	}
	if usage.CorePercent != nil {
		t.Fatalf("expected nil corePercent, got %v", usage.CorePercent)
	}
	if usage.OveragePreference != "droidCore" {
		t.Fatalf("expected overagePreference droidCore, got %q", usage.OveragePreference)
	}
	// Effective = standard (80.0 < 100)
	if usage.Used == nil || *usage.Used != 80.0 {
		t.Fatalf("expected used 80.0, got %v", usage.Used)
	}
	if usage.Total == nil || *usage.Total != 100 {
		t.Fatalf("expected total 100, got %v", usage.Total)
	}
	if usage.UsedRatio() != 0.8 {
		t.Fatalf("expected ratio 0.8, got %f", usage.UsedRatio())
	}
	if usage.Remaining() != 20 {
		t.Fatalf("expected remaining 20, got %f", usage.Remaining())
	}
	if usage.Expires == nil || *usage.Expires == "" {
		t.Fatalf("expected expires to be set, got %v", usage.Expires)
	}
	if usage.ExpiresTsMs == nil || *usage.ExpiresTsMs <= 0 {
		t.Fatalf("expected expiresTsMs to be set, got %v", usage.ExpiresTsMs)
	}
}

func TestParseUsageResponse_WithCore(t *testing.T) {
	body := `{
		"usesTokenRateLimitsBilling": true,
		"overagePreference": "droidCore",
		"limits": {
			"standard": {
				"fiveHour":  {"usedPercent": 50.0, "windowEnd": "2026-06-19T15:00:00Z"},
				"weekly":    {"usedPercent": 50.0, "windowEnd": "2026-06-23T00:00:00Z"},
				"monthly":   {"usedPercent": 50.0, "windowEnd": "2026-07-01T00:00:00Z"}
			},
			"core": {
				"fiveHour":  {"usedPercent": 15.0, "windowEnd": "2026-06-19T15:00:00Z"},
				"weekly":    {"usedPercent": 30.0, "windowEnd": "2026-06-23T00:00:00Z"},
				"monthly":   {"usedPercent": 45.0, "windowEnd": "2026-07-01T00:00:00Z"}
			}
		}
	}`
	usage := parseUsageResponse("test-key", []byte(body))
	if usage.Error != "" {
		t.Fatalf("unexpected error: %s", usage.Error)
	}
	if usage.StandardPercent == nil || *usage.StandardPercent != 50.0 {
		t.Fatalf("expected standardPercent 50.0, got %v", usage.StandardPercent)
	}
	if usage.CorePercent == nil || *usage.CorePercent != 45.0 {
		t.Fatalf("expected corePercent 45.0 (highest bucket), got %v", usage.CorePercent)
	}
	// Standard < 100, so effective = standard
	if usage.Used == nil || *usage.Used != 50.0 {
		t.Fatalf("expected effective used 50.0 (standard), got %v", usage.Used)
	}
}

func TestParseUsageResponse_StandardExhaustedCoreAvailable(t *testing.T) {
	body := `{
		"usesTokenRateLimitsBilling": true,
		"overagePreference": "droidCore",
		"limits": {
			"standard": {
				"fiveHour":  {"usedPercent": 100.0, "windowEnd": "2026-06-19T15:00:00Z"},
				"weekly":    {"usedPercent": 95.0, "windowEnd": "2026-06-23T00:00:00Z"},
				"monthly":   {"usedPercent": 90.0, "windowEnd": "2026-07-01T00:00:00Z"}
			},
			"core": {
				"fiveHour":  {"usedPercent": 20.0, "windowEnd": "2026-06-19T15:00:00Z"},
				"weekly":    {"usedPercent": 30.0, "windowEnd": "2026-06-23T00:00:00Z"},
				"monthly":   {"usedPercent": 40.0, "windowEnd": "2026-07-01T00:00:00Z"}
			}
		}
	}`
	usage := parseUsageResponse("test-key", []byte(body))
	if usage.Error != "" {
		t.Fatalf("unexpected error: %s", usage.Error)
	}
	// Standard highest = 100 → exhausted, so effective = core highest = 40
	if usage.Used == nil || *usage.Used != 40.0 {
		t.Fatalf("expected effective used 40.0 (core fallback), got %v", usage.Used)
	}
	if usage.UsedRatio() != 0.4 {
		t.Fatalf("expected ratio 0.4, got %f", usage.UsedRatio())
	}
}

func TestParseUsageResponse_BothExhausted(t *testing.T) {
	body := `{
		"usesTokenRateLimitsBilling": true,
		"overagePreference": "droidCore",
		"limits": {
			"standard": {
				"fiveHour": {"usedPercent": 100.0, "windowEnd": "2026-06-19T15:00:00Z"}
			},
			"core": {
				"fiveHour": {"usedPercent": 100.0, "windowEnd": "2026-06-19T15:00:00Z"}
			}
		}
	}`
	usage := parseUsageResponse("test-key", []byte(body))
	if usage.Error != "" {
		t.Fatalf("unexpected error: %s", usage.Error)
	}
	if usage.Used == nil || *usage.Used != 100.0 {
		t.Fatalf("expected effective used 100.0, got %v", usage.Used)
	}
	if usage.Remaining() != 0 {
		t.Fatalf("expected remaining 0, got %f", usage.Remaining())
	}
}

func TestParseUsageResponse_LegacyBilling(t *testing.T) {
	body := `{
		"usesTokenRateLimitsBilling": false
	}`
	usage := parseUsageResponse("test-key", []byte(body))
	if usage.Error != "" {
		t.Fatalf("unexpected error: %s", usage.Error)
	}
	if usage.Total != nil || usage.Used != nil {
		t.Fatalf("expected nil Total/Used for legacy billing, got %v %v", usage.Total, usage.Used)
	}
}

func TestParseUsageResponse_InvalidJSON(t *testing.T) {
	body := `not json`
	usage := parseUsageResponse("test-key", []byte(body))
	if usage.Error != "parse_error" {
		t.Fatalf("expected parse_error, got %q", usage.Error)
	}
}

func TestParseUsageResponse_HTTP401(t *testing.T) {
	usage := &KeyUsage{Key: "test", Error: "http_401"}
	if !IsInvalidKeyError(usage.Error) {
		t.Fatal("expected 401 to be invalid key error")
	}
}

func TestParseUsageResponse_NoLimits(t *testing.T) {
	body := `{"usesTokenRateLimitsBilling": true}`
	usage := parseUsageResponse("test-key", []byte(body))
	if usage.Error != "" {
		t.Fatalf("unexpected error: %s", usage.Error)
	}
	if usage.Total != nil {
		t.Fatalf("expected nil total for no limits, got %v", usage.Total)
	}
}

func TestParseChatUsageResponse_Standard(t *testing.T) {
	body := `{
		"usage": {
			"endDate": 1782802800000,
			"standard": {
				"totalAllowance": 20000000,
				"orgTotalTokensUsed": 19767303,
				"orgOverageUsed": 0,
				"usedRatio": 0.98836515
			}
		}
	}`
	usage := parseChatUsageResponse("test-key", []byte(body))
	if usage.Error != "" {
		t.Fatalf("unexpected error: %s", usage.Error)
	}
	if !usage.LegacyBilling {
		t.Fatal("expected LegacyBilling=true")
	}
	if usage.StandardUsedTokens == nil || *usage.StandardUsedTokens != 19767303 {
		t.Fatalf("expected usedTokens 19767303, got %v", usage.StandardUsedTokens)
	}
	if usage.StandardTotalTokens == nil || *usage.StandardTotalTokens != 20000000 {
		t.Fatalf("expected totalTokens 20000000, got %v", usage.StandardTotalTokens)
	}
	if usage.StandardPercent == nil || *usage.StandardPercent < 98.8 || *usage.StandardPercent > 98.9 {
		t.Fatalf("expected standardPercent ~98.8, got %v", usage.StandardPercent)
	}
	if usage.UsedRatio() < 0.988 || usage.UsedRatio() > 0.989 {
		t.Fatalf("expected ratio ~0.988, got %f", usage.UsedRatio())
	}
	if usage.Expires == nil || *usage.Expires == "" {
		t.Fatalf("expected expires set, got %v", usage.Expires)
	}
}

func TestParseChatUsageResponse_NoSections(t *testing.T) {
	body := `{"usage": {}}`
	usage := parseChatUsageResponse("test-key", []byte(body))
	if usage.Error != "" {
		t.Fatalf("unexpected error: %s", usage.Error)
	}
	if usage.Total != nil {
		t.Fatalf("expected nil total, got %v", usage.Total)
	}
}

func TestFetchKeyUsage_LegacyBillingFallback(t *testing.T) {
	chatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"usage": {
				"endDate": 1782802800000,
				"standard": {
					"totalAllowance": 20000000,
					"orgTotalTokensUsed": 10000000,
					"usedRatio": 0.5
				}
			}
		}`))
	}))
	defer chatServer.Close()

	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"usesTokenRateLimitsBilling": false, "extraUsageAllowed": true}`))
	}))
	defer billingServer.Close()

	cfg := config.New()
	cfg.UsageAPIURL = billingServer.URL
	cfg.ChatUsageAPIURL = chatServer.URL
	r := NewKeyRotator(cfg, []string{"test-key"})

	usages := r.ProbeAllUsages(context.Background())
	if len(usages) != 1 {
		t.Fatalf("expected 1 usage, got %d", len(usages))
	}
	u := usages[0]
	if u.Error != "" {
		t.Fatalf("unexpected error: %s", u.Error)
	}
	if !u.LegacyBilling {
		t.Fatal("expected LegacyBilling=true")
	}
	if u.StandardPercent == nil || *u.StandardPercent != 50.0 {
		t.Fatalf("expected standardPercent 50.0, got %v", u.StandardPercent)
	}
	if u.CorePercent != nil {
		t.Fatalf("expected nil corePercent for legacy billing, got %v", u.CorePercent)
	}
}

func TestFetchKeyUsage_TokenRateLimitsNoFallback(t *testing.T) {
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"usesTokenRateLimitsBilling": true,
			"overagePreference": "droidCore",
			"limits": {
				"standard": {
					"fiveHour": {"usedPercent": 30, "windowEnd": "2026-06-19T15:00:00Z"}
				},
				"core": {
					"fiveHour": {"usedPercent": 10, "windowEnd": "2026-06-19T15:00:00Z"}
				}
			}
		}`))
	}))
	defer billingServer.Close()

	cfg := config.New()
	cfg.UsageAPIURL = billingServer.URL
	cfg.ChatUsageAPIURL = "http://should-not-be-called"
	r := NewKeyRotator(cfg, []string{"test-key"})

	usages := r.ProbeAllUsages(context.Background())
	if len(usages) != 1 {
		t.Fatalf("expected 1 usage, got %d", len(usages))
	}
	u := usages[0]
	if u.Error != "" {
		t.Fatalf("unexpected error: %s", u.Error)
	}
	if u.LegacyBilling {
		t.Fatal("expected LegacyBilling=false for token rate limits")
	}
	if u.StandardPercent == nil || *u.StandardPercent != 30.0 {
		t.Fatalf("expected standardPercent 30.0, got %v", u.StandardPercent)
	}
	if u.CorePercent == nil || *u.CorePercent != 10.0 {
		t.Fatalf("expected corePercent 10.0, got %v", u.CorePercent)
	}
}

func TestSelectActiveKey_ExpireFirst(t *testing.T) {
	r := &KeyRotator{
		keys:   []string{"key-early", "key-late", "key-nodate"},
		usages: make(map[string]*KeyUsage),
	}

	earlyMs := float64(1742496000000)
	lateMs := float64(1750000000000)
	used50 := float64(50)
	used30 := float64(30)
	used60 := float64(60)
	total100 := float64(100)

	r.usages["key-early"] = &KeyUsage{
		Key: "key-early", Used: &used50, Total: &total100,
		ExpiresTsMs: &earlyMs, LastCheckedAt: testNow(),
	}
	r.usages["key-late"] = &KeyUsage{
		Key: "key-late", Used: &used30, Total: &total100,
		ExpiresTsMs: &lateMs, LastCheckedAt: testNow(),
	}
	r.usages["key-nodate"] = &KeyUsage{
		Key: "key-nodate", Used: &used60, Total: &total100,
		LastCheckedAt: testNow(),
	}

	selected := r.selectActiveKey()
	if selected != "key-early" {
		t.Fatalf("expected key-early (earliest expiry, ratio 0.5 < 0.98), got %s", selected)
	}
}

func TestSelectActiveKey_Spillover98(t *testing.T) {
	r := &KeyRotator{
		keys:   []string{"key-full", "key-ok"},
		usages: make(map[string]*KeyUsage),
	}

	earlyMs := float64(1742496000000)
	lateMs := float64(1750000000000)
	used98 := float64(98)
	used50 := float64(50)
	total100 := float64(100)

	r.usages["key-full"] = &KeyUsage{
		Key: "key-full", Used: &used98, Total: &total100,
		ExpiresTsMs: &earlyMs, LastCheckedAt: testNow(),
	}
	r.usages["key-ok"] = &KeyUsage{
		Key: "key-ok", Used: &used50, Total: &total100,
		ExpiresTsMs: &lateMs, LastCheckedAt: testNow(),
	}

	selected := r.selectActiveKey()
	if selected != "key-ok" {
		t.Fatalf("expected key-ok (key-full at 98%% spillover), got %s", selected)
	}
}

func TestSelectActiveKey_AllExhausted(t *testing.T) {
	r := &KeyRotator{
		keys:   []string{"key-a", "key-b"},
		usages: make(map[string]*KeyUsage),
	}

	ms1 := float64(1742496000000)
	ms2 := float64(1750000000000)
	used99 := float64(99)
	used95 := float64(95)
	total100 := float64(100)

	r.usages["key-a"] = &KeyUsage{
		Key: "key-a", Used: &used99, Total: &total100,
		ExpiresTsMs: &ms1, LastCheckedAt: testNow(),
	}
	r.usages["key-b"] = &KeyUsage{
		Key: "key-b", Used: &used95, Total: &total100,
		ExpiresTsMs: &ms2, LastCheckedAt: testNow(),
	}

	selected := r.selectActiveKey()
	// Both >= 98%: pick the one with most remaining (key-b: 5 > key-a: 1)
	if selected != "key-b" {
		t.Fatalf("expected key-b (most remaining: 5), got %s", selected)
	}
}

func TestSelectActiveKey_InvalidKey(t *testing.T) {
	r := &KeyRotator{
		keys:   []string{"key-bad", "key-good"},
		usages: make(map[string]*KeyUsage),
	}

	ms := float64(1750000000000)
	used50 := float64(50)
	total100 := float64(100)

	r.usages["key-bad"] = &KeyUsage{
		Key: "key-bad", Error: "http_401", LastCheckedAt: testNow(),
	}
	r.usages["key-good"] = &KeyUsage{
		Key: "key-good", Used: &used50, Total: &total100,
		ExpiresTsMs: &ms, LastCheckedAt: testNow(),
	}

	selected := r.selectActiveKey()
	if selected != "key-good" {
		t.Fatalf("expected key-good (key-bad has error), got %s", selected)
	}
}

func TestSelectActiveKey_EmptyKeys(t *testing.T) {
	r := &KeyRotator{keys: []string{}, usages: make(map[string]*KeyUsage)}
	selected := r.selectActiveKey()
	if selected != "" {
		t.Fatalf("expected empty string for no keys, got %s", selected)
	}
}

func TestSelectActiveKey_NoUsageData(t *testing.T) {
	r := &KeyRotator{
		keys:   []string{"key-1"},
		usages: make(map[string]*KeyUsage),
	}
	selected := r.selectActiveKey()
	if selected != "key-1" {
		t.Fatalf("expected key-1 as fallback, got %s", selected)
	}
}

func TestSelectActiveKey_NoUsageDataSkipsMarkedInvalid(t *testing.T) {
	r := &KeyRotator{
		keys:   []string{"key-bad", "key-good"},
		usages: make(map[string]*KeyUsage),
	}
	r.MarkKeyHTTPError("key-bad", http.StatusForbidden)

	selected := r.selectActiveKey()
	if selected != "key-good" {
		t.Fatalf("expected key-good after marking key-bad invalid, got %s", selected)
	}
}

// Regression: a runtime LLM 403 must set the cooldown mark, not the billing
// Error, so the billing refresh loop cannot clear it.
func TestMarkKeyHTTPError_SetsLLMCooldownNotBillingError(t *testing.T) {
	r := &KeyRotator{
		keys:   []string{"key-a"},
		usages: make(map[string]*KeyUsage),
	}
	r.MarkKeyHTTPError("key-a", http.StatusForbidden)

	u := r.usages["key-a"]
	if u == nil {
		t.Fatal("expected usage entry for key-a")
	}
	if u.LLMHTTPError != "http_403" {
		t.Fatalf("expected LLMHTTPError=http_403, got %q", u.LLMHTTPError)
	}
	if u.LLMErrorAt.IsZero() {
		t.Fatal("expected LLMErrorAt to be set")
	}
	if u.Error != "" {
		t.Fatalf("billing Error must remain empty, got %q", u.Error)
	}
	if !r.isLLMCooledDown(u) {
		t.Fatal("expected key-a to be within cooldown immediately after marking")
	}
}

// Core regression: the billing refresh loop must NOT clear a runtime LLM 403
// cooldown when the billing/limits endpoint returns 200 for the same key.
func TestFetchAllUsages_PreservesLLMErrorCooldownAcrossRefresh(t *testing.T) {
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"usesTokenRateLimitsBilling": true,
			"overagePreference": "droidCore",
			"limits": {"standard": {"fiveHour": {"usedPercent": 10, "windowEnd": "2026-06-19T15:00:00Z"}}}
		}`))
	}))
	defer billingServer.Close()

	cfg := config.New()
	cfg.UsageAPIURL = billingServer.URL
	cfg.ChatUsageAPIURL = "http://should-not-be-called"
	r := NewKeyRotator(cfg, []string{"key-bad", "key-good"})

	// Seed billing usage for both keys so they are eligible candidates.
	r.FetchAllUsages(context.Background())

	// Simulate a runtime LLM 403 on key-bad.
	r.MarkKeyHTTPError("key-bad", http.StatusForbidden)
	if got := r.selectActiveKey(); got != "key-good" {
		t.Fatalf("expected key-good after marking key-bad, got %s", got)
	}

	// A billing refresh returns 200 for key-bad; it must not re-enable it.
	r.FetchAllUsages(context.Background())
	if got := r.selectActiveKey(); got != "key-good" {
		t.Fatalf("expected key-good to remain selected after billing refresh, got %s", got)
	}

	r.mu.RLock()
	u := r.usages["key-bad"]
	r.mu.RUnlock()
	if u == nil || u.LLMHTTPError != "http_403" {
		t.Fatalf("expected key-bad to retain LLMHTTPError=http_403 after refresh, got %+v", u)
	}
	if u.Error != "" {
		t.Fatalf("billing Error should stay empty after refresh, got %q", u.Error)
	}
}

func TestLLMCooldown_ExpiryReenablesKey(t *testing.T) {
	cfg := config.New()
	r := NewKeyRotator(cfg, []string{"key-a", "key-b"})

	ms := float64(1750000000000)
	used10 := float64(10)
	total100 := float64(100)
	r.mu.Lock()
	r.usages["key-a"] = &KeyUsage{
		Key: "key-a", Used: &used10, Total: &total100,
		ExpiresTsMs: &ms, LastCheckedAt: testNow(),
	}
	r.mu.Unlock()

	now := time.Now()
	r.SetClockForTest(func() time.Time { return now })
	r.MarkKeyHTTPError("key-a", http.StatusForbidden)
	if got := r.selectActiveKey(); got != "key-b" {
		t.Fatalf("expected key-b while key-a is cooled, got %s", got)
	}

	// Advance time past the cooldown; key-a becomes eligible again.
	r.SetClockForTest(func() time.Time { return now.Add(r.llmCooldown() + time.Second) })
	if got := r.selectActiveKey(); got != "key-a" {
		t.Fatalf("expected key-a after cooldown expired, got %s", got)
	}
}

func TestResolveKeyForRequest_PresentedKeyCooledFallsThrough(t *testing.T) {
	r := &KeyRotator{
		keys:   []string{"key-a", "key-b"},
		usages: make(map[string]*KeyUsage),
	}
	ms := float64(1750000000000)
	used10 := float64(10)
	total100 := float64(100)
	r.usages["key-a"] = &KeyUsage{
		Key: "key-a", Used: &used10, Total: &total100,
		ExpiresTsMs: &ms, LastCheckedAt: testNow(),
		LLMHTTPError: "http_403", LLMErrorAt: time.Now(),
	}
	r.activeKey.Store(str("key-b"))

	// Presented key-a is cooled, so the rotator must fall through to active.
	result := r.ResolveKeyForRequest("key-a")
	if result != "key-b" {
		t.Fatalf("expected key-b (presented key-a is cooled), got %s", result)
	}
}

func TestFetchAllUsages_AllInvalidAuthDisablesRefreshAndFallsBack(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	cfg := config.New()
	cfg.UsageAPIURL = server.URL
	r := NewKeyRotator(cfg, []string{"key-a", "key-b"})

	r.FetchAllUsages(context.Background())

	if !r.UsageRefreshDisabled() {
		t.Fatal("expected all invalid auth usage probes to disable refresh")
	}
	if got := r.GetActiveKey(); got != "key-a" {
		t.Fatalf("expected ordered fallback key-a, got %s", got)
	}
	r.MarkKeyHTTPError("key-a", http.StatusForbidden)
	if got := r.GetActiveKey(); got != "key-b" {
		t.Fatalf("expected marked key-a to rotate to key-b, got %s", got)
	}
}

func TestProbeAllUsages_ReturnsResultsInKeyOrder(t *testing.T) {
	// billing/limits returns legacy billing → fallback to chat-usage
	chatUsageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"usage": {
				"standard": {
					"totalAllowance": 20000000,
					"orgTotalTokensUsed": 10000000,
					"usedRatio": 0.5
				}
			}
		}`))
	}))
	defer chatUsageServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer key-a":
			w.WriteHeader(http.StatusForbidden)
		case "Bearer key-b":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"usesTokenRateLimitsBilling": true,
				"overagePreference": "droidCore",
				"limits": {
					"standard": {
						"fiveHour": {"usedPercent": 50, "windowEnd": "2026-06-19T15:00:00Z"}
					}
				}
			}`))
		default:
			// Fallback chat-usage calls go here (shouldn't happen for key-a/key-b)
			t.Fatalf("unexpected authorization header %q", r.Header.Get("Authorization"))
		}
	}))
	defer server.Close()

	cfg := config.New()
	cfg.UsageAPIURL = server.URL
	cfg.ChatUsageAPIURL = chatUsageServer.URL
	r := NewKeyRotator(cfg, []string{"key-a", "key-b"})

	usages := r.ProbeAllUsages(context.Background())
	if len(usages) != 2 {
		t.Fatalf("expected 2 usages, got %d", len(usages))
	}
	if usages[0].Key != "key-a" || usages[0].Error != "http_403" {
		t.Fatalf("unexpected first usage: %+v", usages[0])
	}
	if usages[1].Key != "key-b" || usages[1].Total == nil || *usages[1].Total != 100 {
		t.Fatalf("unexpected second usage: %+v", usages[1])
	}
	if usages[1].StandardPercent == nil || *usages[1].StandardPercent != 50 {
		t.Fatalf("expected standardPercent 50, got %v", usages[1].StandardPercent)
	}
}

func TestSelectActiveKey_SameExpiry_BurnMostUsedFirst(t *testing.T) {
	r := &KeyRotator{
		keys:   []string{"key-high", "key-low"},
		usages: make(map[string]*KeyUsage),
	}

	ms := float64(1742496000000)
	used80 := float64(80)
	used20 := float64(20)
	total100 := float64(100)

	r.usages["key-high"] = &KeyUsage{
		Key: "key-high", Used: &used80, Total: &total100,
		ExpiresTsMs: &ms, LastCheckedAt: testNow(),
	}
	r.usages["key-low"] = &KeyUsage{
		Key: "key-low", Used: &used20, Total: &total100,
		ExpiresTsMs: &ms, LastCheckedAt: testNow(),
	}

	selected := r.selectActiveKey()
	if selected != "key-high" {
		t.Fatalf("expected key-high (most used, burn it first), got %s", selected)
	}
}

func TestSelectActiveKey_SameExpiry_NoOscillation(t *testing.T) {
	r := &KeyRotator{
		keys:   []string{"key-high", "key-low"},
		usages: make(map[string]*KeyUsage),
	}

	ms := float64(1742496000000)
	used97 := float64(97)
	used20 := float64(20)
	total100 := float64(100)

	r.usages["key-high"] = &KeyUsage{
		Key: "key-high", Used: &used97, Total: &total100,
		ExpiresTsMs: &ms, LastCheckedAt: testNow(),
	}
	r.usages["key-low"] = &KeyUsage{
		Key: "key-low", Used: &used20, Total: &total100,
		ExpiresTsMs: &ms, LastCheckedAt: testNow(),
	}

	selected := r.selectActiveKey()
	if selected != "key-high" {
		t.Fatalf("expected key-high (97%% < 98%% threshold, keep burning), got %s", selected)
	}
}

func TestSelectActiveKey_SameExpiry_SpilloverSwitchesToNext(t *testing.T) {
	r := &KeyRotator{
		keys:   []string{"key-high", "key-low"},
		usages: make(map[string]*KeyUsage),
	}

	ms := float64(1742496000000)
	used98 := float64(98)
	used20 := float64(20)
	total100 := float64(100)

	r.usages["key-high"] = &KeyUsage{
		Key: "key-high", Used: &used98, Total: &total100,
		ExpiresTsMs: &ms, LastCheckedAt: testNow(),
	}
	r.usages["key-low"] = &KeyUsage{
		Key: "key-low", Used: &used20, Total: &total100,
		ExpiresTsMs: &ms, LastCheckedAt: testNow(),
	}

	selected := r.selectActiveKey()
	if selected != "key-low" {
		t.Fatalf("expected key-low (key-high hit 98%% spillover), got %s", selected)
	}
}

func TestSelectActiveKey_MillisecondPrecision(t *testing.T) {
	r := &KeyRotator{
		keys:   []string{"key-morning", "key-evening"},
		usages: make(map[string]*KeyUsage),
	}

	morningMs := float64(1742496000000) // 2025-03-20 00:00:00 UTC
	eveningMs := float64(1742539200000) // 2025-03-20 12:00:00 UTC
	used50 := float64(50)
	total100 := float64(100)

	r.usages["key-morning"] = &KeyUsage{
		Key: "key-morning", Used: &used50, Total: &total100,
		ExpiresTsMs: &morningMs, LastCheckedAt: testNow(),
	}
	r.usages["key-evening"] = &KeyUsage{
		Key: "key-evening", Used: &used50, Total: &total100,
		ExpiresTsMs: &eveningMs, LastCheckedAt: testNow(),
	}

	selected := r.selectActiveKey()
	if selected != "key-morning" {
		t.Fatalf("expected key-morning (expires 12h earlier same day), got %s", selected)
	}
}

func TestResolveKeyForRequest_PresentedKeyOK(t *testing.T) {
	r := &KeyRotator{
		keys:   []string{"key-a", "key-b"},
		usages: make(map[string]*KeyUsage),
	}

	ms := float64(1750000000000)
	used50 := float64(50)
	total100 := float64(100)
	r.usages["key-a"] = &KeyUsage{
		Key: "key-a", Used: &used50, Total: &total100,
		ExpiresTsMs: &ms, LastCheckedAt: testNow(),
	}
	r.activeKey.Store(str("key-a"))

	result := r.ResolveKeyForRequest("key-a")
	if result != "key-a" {
		t.Fatalf("expected key-a (presented key is valid), got %s", result)
	}
}

func TestResolveKeyForRequest_PresentedKeySpillover(t *testing.T) {
	r := &KeyRotator{
		keys:   []string{"key-full", "key-ok"},
		usages: make(map[string]*KeyUsage),
	}

	ms := float64(1750000000000)
	used98 := float64(98)
	used50 := float64(50)
	total100 := float64(100)

	r.usages["key-full"] = &KeyUsage{
		Key: "key-full", Used: &used98, Total: &total100,
		ExpiresTsMs: &ms, LastCheckedAt: testNow(),
	}
	r.usages["key-ok"] = &KeyUsage{
		Key: "key-ok", Used: &used50, Total: &total100,
		ExpiresTsMs: &ms, LastCheckedAt: testNow(),
	}
	r.activeKey.Store(str("key-ok"))

	result := r.ResolveKeyForRequest("key-full")
	if result != "key-ok" {
		t.Fatalf("expected key-ok (key-full at spillover), got %s", result)
	}
}

func TestNormalizeBearerToken(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Bearer sk-123", "sk-123"},
		{"bearer sk-456", "sk-456"},
		{"BEARER sk-789", "sk-789"},
		{"  Bearer   sk-space  ", "sk-space"},
		{"sk-no-bearer", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := NormalizeBearerToken(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeBearerToken(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestKeyUsage_UsedRatio_NilFields(t *testing.T) {
	u := &KeyUsage{}
	if u.UsedRatio() != 1 {
		t.Fatalf("expected ratio 1 for nil fields, got %f", u.UsedRatio())
	}
	if u.Remaining() != 0 {
		t.Fatalf("expected remaining 0 for nil fields, got %f", u.Remaining())
	}
}

func TestKeyUsage_UsedRatio_ZeroTotal(t *testing.T) {
	used := float64(50)
	total := float64(0)
	u := &KeyUsage{Used: &used, Total: &total}
	if u.UsedRatio() != 1 {
		t.Fatalf("expected ratio 1 for zero total, got %f", u.UsedRatio())
	}
}

func str(s string) *string { return &s }

func testNow() time.Time { return time.Now() }

func TestSelectActiveKey_MonthlyDeadlineFirst(t *testing.T) {
	now := time.Date(2026, 10, 6, 4, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	used := func(v float64) *float64 { return &v }
	r := &KeyRotator{
		keys:   []string{"recent", "lateMonth", "earlyMonth"},
		usages: make(map[string]*KeyUsage),
		clock:  func() time.Time { return now },
	}
	// recent was just used: its 5h window ends in 1h, its month in 400h.
	r.usages["recent"] = &KeyUsage{Key: "recent", StandardPercent: used(40), LastCheckedAt: now, StandardLimits: &PoolLimits{
		FiveHour: &BucketLimit{UsedPercent: 40, WindowEnd: at(time.Hour)},
		Weekly:   &BucketLimit{UsedPercent: 20, WindowEnd: at(72 * time.Hour)},
		Monthly:  &BucketLimit{UsedPercent: 30, WindowEnd: at(400 * time.Hour)},
	}}
	// lateMonth's week ends in 4h, but three more weeks fit before its
	// month ends, so nothing is lost when this week ends unused.
	r.usages["lateMonth"] = &KeyUsage{Key: "lateMonth", StandardPercent: used(40), LastCheckedAt: now, StandardLimits: &PoolLimits{
		Weekly:  &BucketLimit{UsedPercent: 40, WindowEnd: at(4 * time.Hour)},
		Monthly: &BucketLimit{UsedPercent: 30, WindowEnd: at(300 * time.Hour)},
	}}
	// earlyMonth has no week yet; its month ends first.
	r.usages["earlyMonth"] = &KeyUsage{Key: "earlyMonth", StandardPercent: used(40), LastCheckedAt: now, StandardLimits: &PoolLimits{
		Weekly:  &BucketLimit{},
		Monthly: &BucketLimit{UsedPercent: 30, WindowEnd: at(200 * time.Hour)},
	}}
	if got, _ := r.SelectKeyForModel("", "claude-opus-5-5", nil); got != "earlyMonth" {
		t.Fatalf("expected the key whose month ends first, got %s", got)
	}
	// Now one week fits after lateMonth's week, and it holds only half of
	// the 80% monthly remainder: the week ending in 4h comes first.
	r.usages["lateMonth"].StandardLimits.Monthly = &BucketLimit{UsedPercent: 20, WindowEnd: at(150 * time.Hour)}
	if got, _ := r.SelectKeyForModel("", "claude-opus-5-5", nil); got != "lateMonth" {
		t.Fatalf("expected the key that loses quota when its week ends, got %s", got)
	}
}

func TestQuotaDeadlineMs(t *testing.T) {
	now := time.Date(2026, 10, 6, 4, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	ms := func(d time.Duration) float64 { return float64(now.Add(d).UnixMilli()) }
	cases := []struct {
		name   string
		limits *PoolLimits
		want   float64
	}{
		{"nil", nil, 0},
		{"weekly", &PoolLimits{Weekly: &BucketLimit{UsedPercent: 10, WindowEnd: at(5 * time.Hour)}, Monthly: &BucketLimit{UsedPercent: 10, WindowEnd: at(50 * time.Hour)}}, ms(5 * time.Hour)},
		{"monthly first", &PoolLimits{Weekly: &BucketLimit{UsedPercent: 10, WindowEnd: at(50 * time.Hour)}, Monthly: &BucketLimit{UsedPercent: 10, WindowEnd: at(5 * time.Hour)}}, ms(5 * time.Hour)},
		{"no week yet", &PoolLimits{Weekly: &BucketLimit{}, Monthly: &BucketLimit{UsedPercent: 10, WindowEnd: at(50 * time.Hour)}}, ms(50 * time.Hour)},
		{"week used up", &PoolLimits{Weekly: &BucketLimit{UsedPercent: 100, WindowEnd: at(5 * time.Hour)}, Monthly: &BucketLimit{UsedPercent: 10, WindowEnd: at(50 * time.Hour)}}, ms(50 * time.Hour)},
		{"all ended", &PoolLimits{Weekly: &BucketLimit{UsedPercent: 10, WindowEnd: at(-time.Hour)}}, 0},
		{"harmless week", &PoolLimits{Weekly: &BucketLimit{UsedPercent: 10, WindowEnd: at(5 * time.Hour)}, Monthly: &BucketLimit{UsedPercent: 10, WindowEnd: at(400 * time.Hour)}}, ms(400 * time.Hour)},
		{"week holds the overflow", &PoolLimits{Weekly: &BucketLimit{UsedPercent: 10, WindowEnd: at(5 * time.Hour)}, Monthly: &BucketLimit{UsedPercent: 60, WindowEnd: at(100 * time.Hour)}}, ms(100 * time.Hour)},
		{"weekly only", &PoolLimits{Weekly: &BucketLimit{UsedPercent: 10, WindowEnd: at(5 * time.Hour)}}, ms(5 * time.Hour)},
	}
	for _, tc := range cases {
		if got := quotaDeadlineMs(tc.limits, now); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestFetchAllUsages_VercelBlockAtStartKeepsRefreshing(t *testing.T) {
	block := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if block {
			w.Header().Set("X-Vercel-Mitigated", "deny")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write([]byte(`{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"weekly":{"usedPercent":40,"windowEnd":"2099-01-01T00:00:00Z"}}}}`))
	}))
	defer server.Close()

	cfg := config.New()
	cfg.UsageAPIURL = server.URL
	r := NewKeyRotator(cfg, []string{"key-a", "key-b"})
	r.FetchAllUsages(context.Background())
	if r.UsageRefreshDisabled() {
		t.Fatal("a firewall block on the first probe must not stop the refresh loop")
	}
	if u := r.UsageForTest("key-a"); u != nil {
		t.Fatalf("a blocked probe must not store usage, got %+v", u)
	}

	block = false
	r.FetchAllUsages(context.Background())
	if u := r.UsageForTest("key-a"); u == nil || u.Error != "" || u.StandardPercent == nil || *u.StandardPercent != 40 {
		t.Fatalf("usage should recover once the block lifts, got %+v", u)
	}

	block = true
	r.FetchAllUsages(context.Background())
	if u := r.UsageForTest("key-a"); u == nil || u.Error != "" || u.StandardPercent == nil || *u.StandardPercent != 40 {
		t.Fatalf("the last good usage should be kept while blocked, got %+v", u)
	}
}

func TestFetchAllUsages_TransientInvalidAuthKeepsRefreshing(t *testing.T) {
	forbid := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if forbid {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write([]byte(`{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"weekly":{"usedPercent":40,"windowEnd":"2099-01-01T00:00:00Z"}}}}`))
	}))
	defer server.Close()

	cfg := config.New()
	cfg.UsageAPIURL = server.URL
	r := NewKeyRotator(cfg, []string{"key-a", "key-b"})
	r.FetchAllUsages(context.Background())

	forbid = true
	r.FetchAllUsages(context.Background())
	if r.UsageRefreshDisabled() {
		t.Fatal("a 401/403 round after a good probe must not stop the refresh loop")
	}
	if u := r.UsageForTest("key-a"); u == nil || u.Error != "" || u.StandardPercent == nil || *u.StandardPercent != 40 {
		t.Fatalf("the last good usage should be kept, got %+v", u)
	}
}
