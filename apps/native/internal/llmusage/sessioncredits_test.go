package llmusage

import (
	"math"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/droidhook"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
)

// Calls measured on 2026-11-12 against Factory's billing counter (chat-usage
// userTokens, TestLiveRequestLogCreditsMatchFactoryBilling): the log prices
// them as Factory billed them, where droid reported a Gemini 4 Carbon run at
// 5 times its charge and a Haiku 5.5 run 10% under it.
func TestSessionCreditsPriceWhatFactoryBills(t *testing.T) {
	start := time.Date(2026, 11, 10, 16, 0, 0, 0, time.UTC)
	useDroidModels(t, &droidhook.Models{Models: []droidhook.Model{
		{ID: "gemini-4-carbon", Provider: "google", TokenMultiplier: 1.6, OutputTokenMultiplier: f(5), CacheReadTokenMultiplier: f(0.05),
			Promotions: []droidhook.Promotion{{Discount: 0.5, StartsAt: start, ExpiresAt: start.AddDate(0, 0, 20)}}},
		{ID: "claude-haiku-5-5", Provider: "anthropic", TokenMultiplier: 0.04, OutputTokenMultiplier: f(5)},
	}})
	at := time.Date(2026, 11, 12, 0, 20, 0, 0, time.UTC)
	records := []diag.RequestRecord{
		// Gemini's prompt count holds the cache reads.
		{Time: at, Model: "gemini-4-carbon", Provider: "google", SessionID: "carbon", Status: 200,
			Usage: &diag.TokenUsage{Input: 20_663, Output: 400}},
		{Time: at.Add(time.Minute), Model: "gemini-4-carbon", Provider: "google", SessionID: "carbon", Status: 200,
			Usage: &diag.TokenUsage{Input: 430_000, CacheRead: 385_436, Output: 1_575}},
		// 10,399 of the first call's cache writes are kept for an hour.
		{Time: at, Model: "claude-haiku-5-5", Provider: "anthropic", SessionID: "haiku", Status: 200,
			Usage: &diag.TokenUsage{Input: 12_918, CacheWrite: 12_914, CacheWrite1h: 10_399, Output: 194}},
		{Time: at.Add(time.Minute), Model: "claude-haiku-5-5", Provider: "anthropic", SessionID: "haiku", Status: 200,
			Usage: &diag.TokenUsage{Input: 15_555, CacheRead: 10_399, CacheWrite: 5_154, Output: 258}},
		{Time: at, Model: "mystery-1", SessionID: "mixed", Status: 200, Usage: &diag.TokenUsage{Input: 10, Output: 5}},
		{Time: at, Model: "claude-haiku-5-5", SessionID: "mixed", Status: 200, Usage: &diag.TokenUsage{Input: 10, Output: 5}},
	}
	log := NewRequestLog(writeRequestLog(t, records))
	if err := log.Refresh(); err != nil {
		t.Fatal(err)
	}
	near := func(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

	carbon := log.SessionCredits(map[string]bool{"carbon": true}, time.Time{}, time.Time{})
	// Factory's measured 75,505 is these calls' tokens, at half price.
	if carbon.Calls != 2 || carbon.Output != 1_975 || !near(carbon.Credits, 75_499, 1) {
		t.Fatalf("carbon = %+v", carbon)
	}
	haiku := log.SessionCredits(map[string]bool{"haiku": true}, time.Time{}, time.Time{})
	want := 0.04*(4+2) + 0.04*(1.25*2_515+2*10_399) + 0.04*1.25*5_154 + 0.004*10_399 + 0.2*(194+258)
	if !near(haiku.Credits, want, 1e-6) {
		t.Errorf("haiku = %.2f, want %.2f with the hour-long writes at 2 input tokens", haiku.Credits, want)
	}

	if !carbon.Covers(1_975, 385_436, 0) || !carbon.Covers(1_985, 385_436, 0) {
		t.Error("the log holds every call of the Session")
	}
	if carbon.Covers(2_500, 385_436, 0) || carbon.Covers(0, 0, 0) {
		t.Error("a Daemon figure with calls the log lacks is taken as covered")
	}
	// A GPT call logged before the proxy read its cache writes.
	if carbon.Covers(1_975, 385_436, 44_192) {
		t.Error("a log without the cache writes droid saw is taken as covered")
	}
	if !haiku.Covers(452, 10_399, 18_068) {
		t.Errorf("haiku = %+v", haiku)
	}
	if mixed := log.SessionCredits(map[string]bool{"mixed": true}, time.Time{}, time.Time{}); mixed.Unpriced != 1 || mixed.Covers(10, 0, 0) {
		t.Errorf("a Session with an unpriced call is taken as covered: %+v", mixed)
	}
	turn := log.SessionCredits(map[string]bool{"carbon": true}, at.Add(30*time.Second), time.Time{})
	if turn.Calls != 1 || turn.Output != 1_575 {
		t.Errorf("the turn's window = %+v", turn)
	}
}
