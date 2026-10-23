package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
)

func measuredCalls() diag.SessionCalls {
	return diag.SessionCalls{
		Calls: 3, Usage: diag.TokenUsage{Input: 300_000, Output: 1_200, CacheRead: 270_000, CacheWrite: 2_000},
		DurationMs: 30_000, TTFTMs: 4_500, TTFTCalls: 3, DecodeMs: 20_000, DecodeTokens: 1_200,
		Last: diag.RequestRecord{Model: "claude-opus-5-5", Key: "fk-aa…1111", DurationMs: 9_000, TTFTMs: 1_500,
			Usage: &diag.TokenUsage{Input: 100_000, Output: 600, CacheRead: 98_000, CacheWrite: 2_000, Reasoning: 120}},
	}
}

// With the Key Pool, the speed under the composer is the call streaming now,
// and its details are what the Key Pool measured: the call under way, the
// last one and the Session's sums, cache and keys included.
func TestTheSpeedUnderTheComposerIsTheKeyPoolsLiveMeasure(t *testing.T) {
	var h *harness
	streaming := true
	h = newHarnessWith(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "Fast", Cwd: "/Users/dev/acme-web",
			Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}, Extra: map[string]any{"sessionId": "s-live"}}},
	}, "", func(c *Config) {
		c.ModelCalls = func(id string) (diag.SessionCalls, *diag.LiveCall, bool) {
			if id != "s-live" {
				return diag.SessionCalls{}, nil, false
			}
			if !streaming {
				return measuredCalls(), nil, true
			}
			now := h.a.cfg.Now()
			return measuredCalls(), &diag.LiveCall{SessionID: id, Model: "claude-opus-5-5", Key: "fk-bb…2222",
				Start: now.Add(-3 * time.Second), FirstToken: now.Add(-2 * time.Second), Output: 200,
				Usage: diag.TokenUsage{Input: 50_000, CacheRead: 49_000}}, true
		}
	})
	h.openSession("Fast")
	h.until("the live speed under the composer", func() bool { return h.hasText("100 tok/s") })
	h.until("the context meter", func() bool { return h.hasText("Context used") })
	h.settle()
	h.click("Model speed")
	for _, want := range []string{"Now", "claude-opus-5-5 · fk-bb…2222", "≈ 200", "50,000 · 98% cached", "Last model call",
		"claude-opus-5-5 · fk-aa…1111", "1.5s", "80 tok/s", "600 · 120 reasoning", "100,000 · 98% cached", "2,000", "9.0s",
		"This Session", "300,000 · 90% cached", "60 tok/s", "Measured by the Key Pool since it started."} {
		if !h.hasText(want) {
			t.Fatalf("details lack %q; texts %q", want, h.tt.Texts())
		}
	}
	if dir := os.Getenv("DROI_NATIVE_SHOTS"); dir != "" {
		h.settle()
		_ = os.MkdirAll(dir, 0o755)
		writePNG(filepath.Join(dir, "speed-details-measured.png"), h.tt.Image())
	}
	h.click("Model speed")
	streaming = false
	h.until("the Session's measured average once the call ended", func() bool { return h.hasText("60 tok/s") })
	h.click("Model speed")
	h.click("Open Usage")
	if r := h.a.Route(); r.Name != "settings" || r.Tab != "usage" {
		t.Fatalf("the details' link opened %+v, want Settings → Usage", r)
	}
}

// While the Key Pool serves the Daemon, a ring beside Settings shows its
// monthly quota left, and opens Usage.
func TestTheKeyPoolRingBesideSettingsOpensUsage(t *testing.T) {
	h := newHarnessWith(t, oneSession(), "", nil)
	h.until("the session list", func() bool { return h.hasText("One") })
	if h.hasButton("Open Usage") {
		t.Fatal("the ring shows without the Key Pool")
	}
	h0 := keyPoolTestHost(t)
	h = newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0 })
	h.until("the ring beside Settings", func() bool { return h.hasButton("Open Usage") })
	if err := h0.KeyPool().Add(context.Background(), "fk-"+strings.Repeat("b", 20)+"2222"); err != nil {
		t.Fatal(err)
	}
	h.until("the key's monthly quota in the ring", func() bool {
		left, ok := llmusage.MonthlyRemaining(keyStates(h.a.poolState()), h.a.cfg.Now())
		return ok && left == 70
	})
	if dir := os.Getenv("DROI_NATIVE_SHOTS"); dir != "" {
		h.settle()
		_ = os.MkdirAll(dir, 0o755)
		writePNG(filepath.Join(dir, "key-pool-ring.png"), h.tt.Image())
	}
	h.click("Open Usage")
	if r := h.a.Route(); r.Name != "settings" || r.Tab != "usage" {
		t.Fatalf("the ring opened %+v, want Settings → Usage", r)
	}
}

func TestSpeedSectionsWaitForTheFirstToken(t *testing.T) {
	now := time.Unix(1000, 0)
	live := &diag.LiveCall{Model: "m", Key: "k", Start: now.Add(-1200 * time.Millisecond)}
	secs := speedSections(diag.SessionCalls{}, live, session.Stats{}, now)
	if len(secs) != 1 || secs[0].title != "Now" {
		t.Fatalf("sections %+v, want the call under way alone", secs)
	}
	var rows []string
	for _, r := range secs[0].rows {
		rows = append(rows, r.label+"="+r.value)
	}
	if got := strings.Join(rows, ", "); got != "Model=m · k, First token after=Waiting 1.2s" {
		t.Fatalf("rows %s", got)
	}
}
