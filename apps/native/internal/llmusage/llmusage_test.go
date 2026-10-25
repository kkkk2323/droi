package llmusage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

func bucket(used float64, end time.Time) *keys.BucketLimit {
	b := &keys.BucketLimit{UsedPercent: used}
	if !end.IsZero() {
		b.WindowEnd = end.Format(time.RFC3339Nano)
	}
	return b
}

func okKey(index int, standard *keys.PoolLimits) keys.KeyState {
	used := 0.0
	if standard != nil {
		for _, w := range Windows {
			if b := WindowOf(standard, w); b != nil && b.UsedPercent > used {
				used = b.UsedPercent
			}
		}
	}
	return keys.KeyState{Index: index, Key: "fk-000…0001", Status: "ok", StandardPercent: &used, StandardAvailable: used < 100, Limits: &keys.KeyLimits{Standard: standard}}
}

func TestAWindowThatHasNotStartedCarriesNoUsage(t *testing.T) {
	now := time.Now()
	if IsLive(bucket(0, time.Time{}), now) {
		t.Error("a window without an end is live")
	}
	if IsLive(nil, now) {
		t.Error("a missing window is live")
	}
	if !IsLive(bucket(40, now.Add(time.Hour)), now) {
		t.Error("a started window is not live")
	}
}

func TestAnExhaustedWindowPastItsEndIsStale(t *testing.T) {
	now := time.Now()
	spent := bucket(100, now.Add(-time.Minute))
	if !IsStaleExhausted(spent, now) {
		t.Error("an exhausted window past its end is not stale")
	}
	if IsLive(spent, now) {
		t.Error("a stale exhausted window is live")
	}
	if IsStaleExhausted(bucket(100, now.Add(time.Hour)), now) {
		t.Error("an exhausted window before its end is stale")
	}
}

func TestPaceComparesUseWithTheWindowGoneBy(t *testing.T) {
	now := time.Now()
	// Half the week is gone and 60% is used: ten points ahead.
	end := now.Add(84 * time.Hour)
	pace, ok := Pace(bucket(60, end), Weekly, now)
	if !ok {
		t.Fatal("no pace in a live window")
	}
	if pace < 9.9 || pace > 10.1 {
		t.Errorf("pace = %.2f, want 10", pace)
	}
	if _, ok := Pace(bucket(60, time.Time{}), Weekly, now); ok {
		t.Error("a window that has not started has a pace")
	}
}

func TestAWindowSummaryCountsUnmeasuredKeysAsFull(t *testing.T) {
	now := time.Now()
	pool := []keys.KeyState{
		okKey(1, &keys.PoolLimits{Monthly: bucket(75, now.Add(48*time.Hour))}),
		okKey(2, nil), // no monthly window yet: a whole month waiting
		{Index: 3, Status: "http_401"},
	}
	s := SummarizeWindow(pool, Standard, Monthly, now)
	if s.Measured != 1 || s.Unmeasured != 1 || s.KeyCount() != 2 {
		t.Fatalf("summary = %+v", s)
	}
	if s.TotalRemaining != 125 {
		t.Errorf("total remaining = %v, want 125", s.TotalRemaining)
	}
	if s.KeyEquivalent() != 1.25 || s.AverageRemaining() != 62.5 {
		t.Errorf("key equivalent = %v, average = %v", s.KeyEquivalent(), s.AverageRemaining())
	}
	if s.SoonestReset == nil {
		t.Error("no soonest reset")
	}
}

func TestAKeyReadsAsItsWorstTrouble(t *testing.T) {
	now := time.Now()
	later := now.Add(time.Minute)
	cases := []struct {
		name string
		key  keys.KeyState
		kind KeyKind
	}{
		{"fresh", keys.KeyState{Status: "no_usage_data"}, KeyNoData},
		{"rejected", keys.KeyState{Status: "http_401"}, KeyError},
		{"cooling", keys.KeyState{Status: "ok", StandardAvailable: true, StandardCooldownUntil: &later}, KeyCooldown},
		{"spent", keys.KeyState{Status: "ok"}, KeyExhausted},
		{"fine", keys.KeyState{Status: "ok", StandardAvailable: true}, KeyOK},
	}
	for _, c := range cases {
		got := KeyDisplayOf(c.key, now)
		if got.Kind != c.kind {
			t.Errorf("%s: kind = %v, want %v", c.name, got.Kind, c.kind)
		}
		if got.Label() == "" {
			t.Errorf("%s: no label", c.name)
		}
		if got.IsProblem() != (c.kind != KeyOK) {
			t.Errorf("%s: problem = %v", c.name, got.IsProblem())
		}
	}
}

func TestTokensPerSecond(t *testing.T) {
	tps, ok := TokensPerSecond(diag.RequestRecord{DurationMs: 1200, TTFBMs: 200, Usage: &diag.TokenUsage{Output: 50}})
	if !ok || tps != 50 {
		t.Errorf("tokens per second = %v %v", tps, ok)
	}
}

// writeUsageLog writes snapshots of one key's monthly window: each pair is
// a time and the used percent read then.
func writeUsageLog(t *testing.T, dir string, end time.Time, readings map[time.Time]float64, order []time.Time) string {
	t.Helper()
	path := filepath.Join(dir, "usage.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, at := range order {
		line, err := json.Marshal(keys.UsageSnapshot{
			Time:     at,
			Index:    1,
			Standard: &keys.PoolLimits{Monthly: bucket(readings[at], end)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestSpendIsTheRiseOfEachKeysMonthlyUse(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	midnight := time.Date(2026, 10, 20, 0, 0, 0, 0, time.Local)
	readings := map[time.Time]float64{}
	var order []time.Time
	add := func(at time.Time, used float64) {
		readings[at], order = used, append(order, at)
	}
	// Two whole days before today, 4 points each, then 3 points today.
	add(midnight.AddDate(0, 0, -2), 0)
	add(midnight.AddDate(0, 0, -2).Add(6*time.Hour), 2)
	add(midnight.AddDate(0, 0, -1), 4)
	add(midnight.AddDate(0, 0, -1).Add(6*time.Hour), 6)
	add(midnight, 8)
	add(midnight.Add(6*time.Hour), 11)
	path := writeUsageLog(t, t.TempDir(), now.AddDate(0, 0, 10), readings, order)

	s, ok := ReadSpend(path, now)
	if !ok {
		t.Fatal("no spend read")
	}
	// A rise is spent at the reading that saw it, so the 8 read at midnight
	// counts as today's.
	if s.Today != 5 {
		t.Errorf("today = %v, want 5", s.Today)
	}
	if s.Days != 2 {
		t.Errorf("days = %v, want 2", s.Days)
	}
	if s.UsualDay != 3 || s.UsualByNow != 3 {
		t.Errorf("usual day = %v, by now = %v, want 3 and 3", s.UsualDay, s.UsualByNow)
	}
	if p, ok := s.ProjectedToday(); !ok || p != 5 {
		t.Errorf("projected today = %v %v, want 5", p, ok)
	}
	if len(s.Daily) != 3 || s.Daily[len(s.Daily)-1].Spent != 5 {
		t.Errorf("daily = %+v", s.Daily)
	}
	if _, ok := ReadSpend(filepath.Join(t.TempDir(), "missing.jsonl"), now); ok {
		t.Error("a missing log reads as spend")
	}
}

func TestRunwayLastsUntilTheMonthsRunOut(t *testing.T) {
	now := time.Now()
	pool := []keys.KeyState{
		okKey(1, &keys.PoolLimits{Monthly: bucket(50, now.Add(20*24*time.Hour))}),
		// A month Factory has not opened yet: a whole one waiting.
		okKey(2, &keys.PoolLimits{Monthly: bucket(0, time.Time{})}),
	}
	r, ok := NewRunway(pool, now)
	if !ok || r.Keys() != 2 {
		t.Fatalf("runway = %+v %v", r, ok)
	}
	if days, lasts := r.Lasts(2); !lasts || days != RunwayDays {
		t.Errorf("2 points a day: %v days, lasts = %v", days, lasts)
	}
	if days, lasts := r.Lasts(40); lasts || days > 10 {
		t.Errorf("40 points a day: %v days, lasts = %v", days, lasts)
	}
	if left := r.LeftAfter(2); left <= 0 {
		t.Errorf("left after %d days = %v", RunwayDays, left)
	}
	limit := r.DailyLimit()
	if _, lasts := r.Lasts(limit * 0.99); !lasts {
		t.Errorf("the pool does not last at its own daily limit %v", limit)
	}
	if _, lasts := r.Lasts(limit * 1.2); lasts {
		t.Errorf("the pool lasts above its daily limit %v", limit)
	}
	if _, ok := NewRunway(nil, now); ok {
		t.Error("an empty pool has a runway")
	}
}

func TestNumbersReadCompactly(t *testing.T) {
	if got := FormatDuration(840); got != "840ms" {
		t.Errorf("840ms = %q", got)
	}
	if got := FormatDuration(1400); got != "1.4s" {
		t.Errorf("1.4s = %q", got)
	}
	if got := FormatDuration(125_000); got != "2m05s" {
		t.Errorf("2m05s = %q", got)
	}
	if got := FormatTokens(12_345); got != "12.3K" {
		t.Errorf("12.3K = %q", got)
	}
	if got := FormatTokens(4_560_000); got != "4.56M" {
		t.Errorf("4.56M = %q", got)
	}
	if got := FormatBytes(2048); got != "2.0K" {
		t.Errorf("2.0K = %q", got)
	}
	if got := FormatCredits(38_900_000); got != "38.9M" {
		t.Errorf("38.9M = %q", got)
	}
	if got := FormatCredits(950); got != "950" {
		t.Errorf("950 = %q", got)
	}
	if got := FormatPercent(62.5); got != "63%" {
		t.Errorf("63%% = %q", got)
	}
	if got := FormatSpan(3*time.Hour + 5*time.Minute); got != "3 h 5 min" {
		t.Errorf("3 h 5 min = %q", got)
	}
	if got := FormatSpan(45 * time.Second); got != "45 sec" {
		t.Errorf("45 sec = %q", got)
	}
	now := time.Now()
	if got := FormatReset(now.Add(-time.Minute), now); got != "reset" {
		t.Errorf("reset = %q", got)
	}
	if got := FormatReset(now.Add(2*time.Hour), now); got != "resets in 2 h" {
		t.Errorf("resets in 2 h = %q", got)
	}
	if got := FormatRelative(now.Add(-2*time.Second), now); got != "just now" {
		t.Errorf("just now = %q", got)
	}
	if credits, ok := WindowCredits(Monthly, 50); !ok || credits != MonthlyStandardCredits/2 {
		t.Errorf("monthly credits = %v %v", credits, ok)
	}
	if _, ok := WindowCredits(FiveHour, 50); ok {
		t.Error("the 5-hour window reports credits")
	}
}

// The weights price the models the request log has shown, the longer ID
// first: Opus 5.5 and Opus 5, GLM-5.3-Flash and GLM-5.3, and a dated variant
// at its base model's price.
func TestWeightsPriceTheLogsModels(t *testing.T) {
	for model, want := range map[string]string{
		"claude-opus-5-5":           "claude-opus-5-5",
		"claude-opus-5":             "claude-opus-5",
		"claude-sonnet-5-5":         "claude-sonnet-5-5",
		"claude-haiku-4-5-20251001": "claude-haiku-4-5",
		"glm-5.3-flash":             "glm-5.3-flash",
		"glm-5.3":                   "glm-5.3",
		"deepseek-v4.1-flash-1017":  "deepseek-v4.1-flash",
		"GPT-6-Astra":               "gpt-6-astra",
	} {
		if w, ok := weightOf(model); !ok || w.ID != want {
			t.Errorf("weight of %s = %q, %v; want %q", model, w.ID, ok, want)
		}
	}
	if _, ok := weightOf("gpt-image-2.5-sunburst"); ok {
		t.Error("an image model is priced as a chat model")
	}
	// GLM-5.3-Flash: 1000 input at 0.06 and 100 output at 0.2004.
	if c, ok := Credits("glm-5.3-flash", diag.TokenUsage{Input: 1000, Output: 100}); !ok || c < 80.03 || c > 80.05 {
		t.Errorf("glm credits = %v, %v", c, ok)
	}
	// A cache write weighs 1.25 input tokens, a cache read its own weight:
	// 1000 plain input, 800 written and 100000 read at DeepSeek's weights.
	if c, ok := Credits("deepseek-v4.1-flash", diag.TokenUsage{Input: 101_800, CacheRead: 100_000, CacheWrite: 800, Output: 500}); !ok || c < 767.99 || c > 768.01 {
		t.Errorf("deepseek credits = %v, %v", c, ok)
	}
}
