package keys

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
)

type lineCollector struct{ lines [][]byte }

func (c *lineCollector) Write(line []byte) error {
	c.lines = append(c.lines, append([]byte(nil), line...))
	return nil
}

func TestFetchAllUsages_LogsSnapshotsOnlyWhenWindowsChange(t *testing.T) {
	var monthly atomic.Int64
	monthly.Store(40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pct := monthly.Load()
		if r.Header.Get("Authorization") == "Bearer key-error-xyz" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.Header.Get("Authorization") == "Bearer key-steady-12345" {
			pct = 7
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"monthly":{"usedPercent":%d,"windowEnd":"2026-10-17T08:18:31Z"}}}}`, pct)
	}))
	defer server.Close()

	cfg := config.New()
	cfg.UsageAPIURL = server.URL
	r := NewKeyRotator(cfg, []string{"key-moving-12345", "key-steady-12345", "key-error-xyz"})
	log := &lineCollector{}
	r.SetUsageLog(log)

	r.FetchAllUsages(context.Background())
	if len(log.lines) != 2 {
		t.Fatalf("first refresh: want a snapshot for each healthy key, got %d lines", len(log.lines))
	}

	r.FetchAllUsages(context.Background())
	if len(log.lines) != 2 {
		t.Fatalf("unchanged refresh must not log, got %d lines", len(log.lines))
	}

	monthly.Store(41)
	r.FetchAllUsages(context.Background())
	if len(log.lines) != 3 {
		t.Fatalf("changed refresh: want one new line, got %d lines", len(log.lines))
	}
	var got UsageSnapshot
	if err := json.Unmarshal(log.lines[2], &got); err != nil {
		t.Fatal(err)
	}
	if got.Index != 1 || got.Key != MaskKey("key-moving-12345") {
		t.Fatalf("want key 1 masked, got index=%d key=%q", got.Index, got.Key)
	}
	if got.Standard == nil || got.Standard.Monthly == nil || got.Standard.Monthly.UsedPercent != 41 {
		t.Fatalf("want monthly 41%%, got %+v", got.Standard)
	}
	if got.Time.IsZero() {
		t.Fatal("snapshot time must be set")
	}
}
