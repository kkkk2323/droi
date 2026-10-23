package keys

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
)

const addKeyUsageBody = `{
	"usesTokenRateLimitsBilling": true,
	"limits": {
		"standard": {"fiveHour": {"usedPercent": 10, "windowEnd": "2026-06-19T15:00:00Z"}},
		"core": {"fiveHour": {"usedPercent": 5, "windowEnd": "2026-06-19T15:00:00Z"}}
	}
}`

func addKeyTestRotator(t *testing.T, handler http.HandlerFunc, initial ...string) *KeyRotator {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.New()
	cfg.UsageAPIURL = server.URL
	cfg.ChatUsageAPIURL = ""
	return NewKeyRotator(cfg, initial)
}

func TestAddKey_ValidatesAndAppends(t *testing.T) {
	r := addKeyTestRotator(t, func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.Header.Get("Authorization"), "fk-bad") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(addKeyUsageBody))
	}, "fk-first-1234567890")

	if err := r.AddKey(context.Background(), "  fk-new-1234567890  "); err != nil {
		t.Fatalf("AddKey: %v", err)
	}
	states := r.Snapshot()
	if len(states) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(states))
	}
	if states[1].Status != "ok" {
		t.Fatalf("new key should have probed usage, got status %q", states[1].Status)
	}

	if err := r.AddKey(context.Background(), "fk-new-1234567890"); err == nil {
		t.Fatal("expected duplicate key to be rejected")
	}
	if err := r.AddKey(context.Background(), "fk-bad-1234567890"); err == nil {
		t.Fatal("expected upstream-rejected key to be refused")
	}
	if err := r.AddKey(context.Background(), "   "); err == nil {
		t.Fatal("expected empty key to be refused")
	}
	if got := len(r.Snapshot()); got != 2 {
		t.Fatalf("failed adds must not change the rotation, got %d keys", got)
	}
}
