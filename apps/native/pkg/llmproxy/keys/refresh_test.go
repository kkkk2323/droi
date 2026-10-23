package keys

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
)

func TestNoteLLMActivityWakesOnlyAfterIdle(t *testing.T) {
	r := NewKeyRotator(config.New(), nil)
	now := mustTime(t, "2026-10-22T10:00:00Z")
	r.lastRound.Store(now.UnixNano())

	r.NoteLLMActivity(now.Add(30 * time.Second))
	select {
	case <-r.wake:
		t.Fatal("woke the loop although the last round is fresh")
	default:
	}
	r.NoteLLMActivity(now.Add(3 * time.Minute))
	select {
	case <-r.wake:
	default:
		t.Fatal("activity after an idle spell did not wake the loop")
	}
}

func TestRecentlyActive(t *testing.T) {
	r := NewKeyRotator(config.New(), nil)
	now := mustTime(t, "2026-10-22T10:00:00Z")
	r.SetClockForTest(func() time.Time { return now })
	if r.recentlyActive() {
		t.Fatal("active before any LLM request")
	}
	r.NoteLLMActivity(now.Add(-time.Duration(config.UsageActiveWindowSec) * time.Second))
	if !r.recentlyActive() {
		t.Fatal("not active at the edge of the window")
	}
	r.NoteLLMActivity(now.Add(-time.Duration(config.UsageActiveWindowSec+1) * time.Second))
	if r.recentlyActive() {
		t.Fatal("still active after the window")
	}
}

func TestRecheckKeyIsSpacedPerKey(t *testing.T) {
	r := NewKeyRotator(config.New(), nil)
	now := mustTime(t, "2026-10-22T10:00:00Z")
	r.SetClockForTest(func() time.Time { return now })
	r.RecheckKey("k1")
	r.RecheckKey("k1")
	r.RecheckKey("k2")
	if n := len(r.recheck); n != 2 {
		t.Fatalf("queued %d rechecks, want 2", n)
	}
	now = now.Add(time.Duration(config.UsageRecheckMinSec) * time.Second)
	r.RecheckKey("k1")
	if n := len(r.recheck); n != 3 {
		t.Fatalf("queued %d rechecks after the gap, want 3", n)
	}
}

func TestRefreshLoopRunsOnWakeAndRechecksExhaustedKey(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/billing/limits" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		hits[NormalizeBearerToken(r.Header.Get("Authorization"))]++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"fiveHour":{"usedPercent":50,"windowEnd":"2099-01-01T00:00:00Z"}}}}`))
	}))
	defer server.Close()
	count := func(key string) int {
		mu.Lock()
		defer mu.Unlock()
		return hits[key]
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s (hits %v)", what, hits)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	cfg := config.New()
	cfg.UsageAPIURL = server.URL + "/api/billing/limits"
	r := NewKeyRotator(cfg, []string{"k1", "k2"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.StartRefreshLoop(ctx)

	r.NoteLLMActivity(time.Now())
	waitFor("a full round", func() bool { return count("k1") == 1 && count("k2") == 1 })

	r.MarkKeyPoolError("k1", PoolStandard, http.StatusTooManyRequests)
	waitFor("the recheck of k1", func() bool { return count("k1") == 2 })
	time.Sleep(50 * time.Millisecond)
	if count("k2") != 1 {
		t.Fatalf("recheck of k1 also read k2: %v", hits)
	}
	if u := r.UsageForTest("k1"); u == nil || u.StandardPoolCooldownUntil.IsZero() {
		t.Fatalf("recheck cleared the pool cooldown: %+v", u)
	}
}

func TestRefreshRoundReadsOnlyKeysThatCanHaveChanged(t *testing.T) {
	start := time.Now()
	soon := start.Add(30 * time.Second).UTC().Format(time.RFC3339)
	var mu sync.Mutex
	hits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/billing/limits" {
			http.NotFound(w, r)
			return
		}
		key := NormalizeBearerToken(r.Header.Get("Authorization"))
		mu.Lock()
		defer mu.Unlock()
		hits[key]++
		end := "2099-01-01T00:00:00Z"
		if key == "k3" && hits[key] == 1 {
			// The first read shows a window about to end; once it ends,
			// Factory reports the next one.
			end = soon
		}
		fmt.Fprintf(w, `{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"fiveHour":{"usedPercent":50,"windowEnd":%q}}}}`, end)
	}))
	defer server.Close()
	snapshot := func() map[string]int {
		mu.Lock()
		defer mu.Unlock()
		out := map[string]int{}
		for k, v := range hits {
			out[k] = v
		}
		return out
	}

	cfg := config.New()
	cfg.UsageAPIURL = server.URL + "/api/billing/limits"
	r := NewKeyRotator(cfg, []string{"k1", "k2", "k3"})
	now := start
	r.SetClockForTest(func() time.Time { return now })
	r.FetchAllUsages(context.Background())

	key, _ := r.SelectKeyForModel("", "claude-opus-5-5", map[string]struct{}{"k2": {}, "k3": {}})
	if key != "k1" {
		t.Fatalf("selected %q, want k1", key)
	}
	now = start.Add(time.Minute)
	r.refreshRound(context.Background())
	if got := snapshot(); got["k1"] != 2 || got["k2"] != 1 || got["k3"] != 2 {
		t.Fatalf("after a partial round hits = %v; want k1 (used) and k3 (window ended) only", got)
	}

	now = start.Add(2 * time.Minute)
	r.refreshRound(context.Background())
	if got := snapshot(); got["k1"] != 2 || got["k2"] != 1 || got["k3"] != 2 {
		t.Fatalf("a round with nothing changed read keys: %v", got)
	}

	now = start.Add(time.Duration(config.UsageFullRefreshSec) * time.Second)
	r.refreshRound(context.Background())
	if got := snapshot(); got["k1"] != 3 || got["k2"] != 2 || got["k3"] != 3 {
		t.Fatalf("full round hits = %v", got)
	}
}

func TestAFailedCheckIsReadAgainTheNextRound(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/billing/limits" {
			http.NotFound(w, r)
			return
		}
		key := NormalizeBearerToken(r.Header.Get("Authorization"))
		mu.Lock()
		hits[key]++
		n := hits[key]
		mu.Unlock()
		switch {
		case key == "k2" && n == 1:
			w.WriteHeader(http.StatusBadGateway)
			return
		case key == "k3":
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"fiveHour":{"usedPercent":50,"windowEnd":"2099-01-01T00:00:00Z"}}}}`))
	}))
	defer server.Close()
	snapshot := func() map[string]int {
		mu.Lock()
		defer mu.Unlock()
		out := map[string]int{}
		for k, v := range hits {
			out[k] = v
		}
		return out
	}

	cfg := config.New()
	cfg.UsageAPIURL = server.URL + "/api/billing/limits"
	r := NewKeyRotator(cfg, []string{"k1", "k2", "k3"})
	start := time.Now()
	now := start
	r.SetClockForTest(func() time.Time { return now })
	r.FetchAllUsages(context.Background())
	if u := r.UsageForTest("k2"); u == nil || u.Error != "http_502" {
		t.Fatalf("k2 after the first round: %+v", u)
	}
	if got := r.failedKeys(); len(got) != 1 || got[0] != "k2" {
		t.Fatalf("failed keys %v, want [k2] (a rejected key is not a failed check)", got)
	}

	now = start.Add(time.Minute)
	r.refreshRound(context.Background())
	if got := snapshot(); got["k1"] != 1 || got["k2"] != 2 || got["k3"] != 1 {
		t.Fatalf("hits %v; want only the failed k2 read again", got)
	}
	if u := r.UsageForTest("k2"); u == nil || u.Error != "" {
		t.Fatalf("k2 after the retry: %+v", u)
	}
	if got := r.failedKeys(); len(got) != 0 {
		t.Fatalf("failed keys %v after the retry", got)
	}
}

func TestRefreshNowReadsEveryKeyButNotTwiceInARow(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/billing/limits" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		hits++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"fiveHour":{"usedPercent":50,"windowEnd":"2099-01-01T00:00:00Z"}}}}`))
	}))
	defer server.Close()
	cfg := config.New()
	cfg.UsageAPIURL = server.URL + "/api/billing/limits"
	r := NewKeyRotator(cfg, []string{"k1", "k2"})
	start := time.Now()
	now := start
	r.SetClockForTest(func() time.Time { return now })
	count := func() int { mu.Lock(); defer mu.Unlock(); return hits }

	r.RefreshNow(context.Background())
	if n := count(); n != 2 {
		t.Fatalf("%d reads, want 2", n)
	}
	now = start.Add(manualRefreshMin - time.Second)
	r.RefreshNow(context.Background())
	if n := count(); n != 2 {
		t.Fatalf("%d reads right after a round, want 2", n)
	}
	now = start.Add(manualRefreshMin)
	r.RefreshNow(context.Background())
	if n := count(); n != 4 {
		t.Fatalf("%d reads, want 4", n)
	}
}
