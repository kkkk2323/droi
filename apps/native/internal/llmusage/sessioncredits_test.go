package llmusage

import (
	"math"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/droidhook"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
)

// Runs measured on 2026-11-12 against Factory's billing counter (chat-usage
// userTokens): for Gemini 4 Carbon droid 1.7.1 reported 383,855 credits and
// the counter moved by 75,505; for Opus 5.5 droid's figure and the counter
// agree.
func TestSessionCreditsTakeOffDroidsGeminiRecount(t *testing.T) {
	start := time.Date(2026, 11, 10, 16, 0, 0, 0, time.UTC)
	useDroidModels(t, &droidhook.Models{Models: []droidhook.Model{
		{ID: "gemini-4-carbon", Provider: "google", TokenMultiplier: 1.6, OutputTokenMultiplier: f(5), CacheReadTokenMultiplier: f(0.05),
			Promotions: []droidhook.Promotion{{Discount: 0.5, StartsAt: start, ExpiresAt: start.AddDate(0, 0, 20)}}},
		{ID: "claude-opus-5-5", Provider: "anthropic", TokenMultiplier: 1.6, OutputTokenMultiplier: f(5), CacheReadTokenMultiplier: f(0.05)},
	}})
	at := time.Date(2026, 11, 12, 0, 20, 0, 0, time.UTC)
	records := []diag.RequestRecord{
		{Time: at, Model: "gemini-4-carbon", Provider: "google", SessionID: "carbon", Status: 200,
			Usage: &diag.TokenUsage{Input: 20_663, CacheRead: 0, Output: 400}},
		{Time: at.Add(time.Minute), Model: "gemini-4-carbon", Provider: "google", SessionID: "carbon", Status: 200,
			Usage: &diag.TokenUsage{Input: 430_000, CacheRead: 385_436, Output: 1_575}},
		{Time: at, Model: "claude-opus-5-5", Provider: "anthropic", SessionID: "opus", Status: 200,
			Usage: &diag.TokenUsage{Input: 179_959, CacheRead: 113_226, CacheWrite: 66_723, Output: 1_287}},
		{Time: at, Model: "gemini-4-carbon", Provider: "google", SessionID: "other", Status: 200,
			Usage: &diag.TokenUsage{Input: 900_000, CacheRead: 800_000, Output: 10}},
	}
	log := NewRequestLog(writeRequestLog(t, records))
	if err := log.Refresh(); err != nil {
		t.Fatal(err)
	}
	near := func(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

	carbon := log.SessionCredits(map[string]bool{"carbon": true}, time.Time{}, time.Time{})
	if carbon.Calls != 2 || !near(carbon.Credits, 75_499, 1) || !near(carbon.Recount, 385_436*0.8, 1e-6) {
		t.Fatalf("carbon = %+v", carbon)
	}
	if got, ok := carbon.Recounted(383_855); !ok || !near(got, 75_505, 2) {
		t.Errorf("droid's 383,855 recounted = %v, %v; Factory billed 75,505", got, ok)
	}
	// A droid that stops recounting reports what the log prices: kept.
	if got, ok := carbon.Recounted(75_499); ok || got != 75_499 {
		t.Errorf("a figure without the recount was cut: %v", got)
	}
	// A Daemon figure the log cannot explain is kept as it is.
	if got, ok := carbon.Recounted(1_000); ok || got != 1_000 {
		t.Errorf("a figure the log does not cover was cut: %v", got)
	}

	opus := log.SessionCredits(map[string]bool{"opus": true}, time.Time{}, time.Time{})
	if opus.Recount != 0 || !near(opus.Credits, 152_818, 3) {
		t.Errorf("opus = %+v; droid counted 152,818", opus)
	}
	if got, ok := opus.Recounted(152_818); ok || got != 152_818 {
		t.Errorf("an Anthropic Session was cut: %v", got)
	}

	turn := log.SessionCredits(map[string]bool{"carbon": true}, at.Add(30*time.Second), time.Time{})
	if turn.Calls != 1 || !near(turn.Recount, 385_436*0.8, 1e-6) {
		t.Errorf("the turn's window = %+v", turn)
	}
}
