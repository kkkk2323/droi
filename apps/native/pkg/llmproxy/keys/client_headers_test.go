package keys

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
)

func init() {
	// Keep tests from running the installed droid.
	readDroidVersion = func() string { return "" }
}

func gzipped(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(s))
	_ = zw.Close()
	return buf.Bytes()
}

func zstded(t *testing.T, s string) []byte {
	t.Helper()
	zw, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	return zw.EncodeAll([]byte(s), nil)
}

func TestBillingPollSendsDroidHeaders(t *testing.T) {
	old := readDroidVersion
	readDroidVersion = func() string { return "0.247.0" }
	t.Cleanup(func() { readDroidVersion = old })

	var mu sync.Mutex
	var billing []http.Header
	var whoamis atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/cli/whoami":
			whoamis.Add(1)
			if r.Header.Get("X-Factory-Whoami-Extended") != "true" || r.Header.Get("Authorization") != "Bearer fk-pool" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(gzipped(t, `{"userId":"u1","orgId":"org-of-key"}`))
		case "/api/billing/limits":
			mu.Lock()
			billing = append(billing, r.Header.Clone())
			mu.Unlock()
			w.Header().Set("Content-Encoding", "zstd")
			_, _ = w.Write(zstded(t, `{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"fiveHour":{"usedPercent":33,"windowEnd":"2026-10-22T10:00:00Z"}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.New()
	cfg.UsageAPIURL = server.URL + "/api/billing/limits"
	r := NewKeyRotator(cfg, []string{"fk-pool"})

	usage := r.fetchKeyUsage(context.Background(), "fk-pool")
	if usage.Error != "" || usage.StandardPercent == nil || *usage.StandardPercent != 33 {
		t.Fatalf("usage = %+v", usage)
	}
	r.NoteClientHeaders(http.Header{"X-Client-Version": {"0.248.0"}, "User-Agent": {"Bun/1.5.0"}})
	r.NoteClientHeaders(http.Header{"User-Agent": {"anthropic-sdk/1.0"}})
	if usage := r.fetchKeyUsage(context.Background(), "fk-pool"); usage.Error != "" {
		t.Fatalf("second poll: %+v", usage)
	}

	if n := whoamis.Load(); n != 1 {
		t.Errorf("whoami called %d times, want once", n)
	}
	want := []map[string]string{
		{"X-Client-Version": "0.247.0", "User-Agent": "Bun/1.4.3"},
		{"X-Client-Version": "0.248.0", "User-Agent": "Bun/1.5.0"},
	}
	if len(billing) != 2 {
		t.Fatalf("billing polls = %d", len(billing))
	}
	for i, h := range billing {
		for k, v := range map[string]string{
			"Authorization":    "Bearer fk-pool",
			"X-Factory-Client": "cli",
			"X-Factory-Org-Id": "org-of-key",
			"Accept":           "*/*",
			"Accept-Encoding":  "gzip, deflate, br, zstd",
			"X-Client-Version": want[i]["X-Client-Version"],
			"User-Agent":       want[i]["User-Agent"],
		} {
			if got := h.Get(k); got != v {
				t.Errorf("poll %d %s = %q, want %q", i+1, k, got, v)
			}
		}
	}
}

func TestBillingPollRetriesFailedWhoamiLater(t *testing.T) {
	var whoamis atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/whoami" {
			whoamis.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.Header.Get("X-Factory-Org-Id") != "" {
			t.Errorf("org id sent without a whoami answer")
		}
		_, _ = w.Write([]byte(`{"usesTokenRateLimitsBilling":true,"limits":{}}`))
	}))
	defer server.Close()

	cfg := config.New()
	cfg.UsageAPIURL = server.URL + "/api/billing/limits"
	r := NewKeyRotator(cfg, []string{"fk-pool"})
	now := mustTime(t, "2026-10-22T10:00:00Z")
	r.SetClockForTest(func() time.Time { return now })

	r.fetchKeyUsage(context.Background(), "fk-pool")
	r.fetchKeyUsage(context.Background(), "fk-pool")
	if n := whoamis.Load(); n != 1 {
		t.Fatalf("whoami retried at once: %d calls", n)
	}
	now = now.Add(orgRetry)
	r.fetchKeyUsage(context.Background(), "fk-pool")
	if n := whoamis.Load(); n != 2 {
		t.Fatalf("whoami not retried after %s: %d calls", orgRetry, n)
	}
}
func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
