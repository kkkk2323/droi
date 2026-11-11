package llmusage

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/droidhook"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
)

func useDroidModels(t *testing.T, ms *droidhook.Models) {
	t.Helper()
	old := droidModels.Load()
	UseDroidModels(ms)
	t.Cleanup(func() { droidModels.Store(old) })
}

func sameWeight(a, b TokenWeight) bool {
	near := func(x, y float64) bool { return math.Abs(x-y) < 1e-9 }
	return near(a.Input, b.Input) && near(a.Output, b.Output) && near(a.CacheRead, b.CacheRead)
}

func f(v float64) *float64 { return &v }

// droid's registry comes first, with the promotion running at the time of
// each call; a dated ID on either side matches; a model droid lacks falls
// back to the bundled table.
func TestDroidModelsPriceTheLog(t *testing.T) {
	start := time.Date(2026, 11, 10, 16, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 1, 8, 0, 0, 0, time.UTC)
	useDroidModels(t, &droidhook.Models{Models: []droidhook.Model{
		{ID: "gemini-9-carbon", Name: "Gemini 9 Carbon", Provider: "google", TokenMultiplier: 1.6, OutputTokenMultiplier: f(5), CacheReadTokenMultiplier: f(0.05),
			Promotions: []droidhook.Promotion{{Discount: 0.5, StartsAt: start, ExpiresAt: end}}},
		{ID: "claude-haiku-4-5-20251001", Provider: "anthropic", TokenMultiplier: 0.4},
		{ID: "claude-opus-5-5", Provider: "anthropic", TokenMultiplier: 1},
	}})
	before := start.Add(-time.Hour)
	for model, id := range map[string]string{
		"gemini-9-carbon":          "gemini-9-carbon",
		"Gemini-9-Carbon-20261101": "gemini-9-carbon",
		"claude-haiku-4-5":         "claude-haiku-4-5-20251001",
		"claude-opus-5-5":          "claude-opus-5-5",
		"glm-5.3-flash":            "glm-5.3-flash",
	} {
		if w, ok := weightOf(model, before); !ok || w.ID != id {
			t.Errorf("weight of %s = %q, %v; want %q", model, w.ID, ok, id)
		}
	}
	if _, ok := weightOf("gemini-9-carbon-fast", before); ok {
		t.Error("a fast variant borrows its base model's price")
	}
	if w, _ := weightOf("claude-haiku-4-5", before); !sameWeight(w, TokenWeight{Input: 0.4, Output: 2, CacheRead: 0.04}) {
		t.Errorf("haiku with the provider's output and the default cache read = %+v", w)
	}
	u := diag.TokenUsage{Input: 20_300, Output: 47}
	for at, mult := range map[time.Time]float64{before: 1.6, start.Add(time.Hour): 0.8, end: 1.6} {
		if c, ok := Credits("gemini-9-carbon", u, at); !ok || math.Abs(c-mult*(20_300+5*47)) > 1e-6 {
			t.Errorf("carbon credits at %v = %v, %v; want multiplier %v", at, c, ok, mult)
		}
	}
}

// Droi's droid, when this computer has one, prices every model the bundled
// table holds as that table does, before any promotion: a change to how
// droid bundles its registry shows here before the request log goes
// unpriced.
func TestRealDroidMatchesTheTable(t *testing.T) {
	path := os.Getenv("DROID_PATH")
	if path == "" {
		dir, _ := os.UserConfigDir()
		found, _ := filepath.Glob(filepath.Join(dir, "Droi", "droid", "*", "droid"))
		if len(found) == 0 {
			t.Skip("no droid on this computer; DROID_PATH picks one")
		}
		path = found[len(found)-1]
	}
	ms, err := droidhook.ExportModels(context.Background(), path, filepath.Join(t.TempDir(), "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	useDroidModels(t, ms)
	matched := 0
	for _, row := range StandardTokenWeights {
		got, ok := droidWeightAt(row.ID, time.Time{})
		if !ok {
			t.Logf("%s: not in this droid", row.ID)
			continue
		}
		matched++
		if !sameWeight(got, row) {
			t.Errorf("%s: droid %+v, table %+v", row.ID, got, row)
		}
	}
	t.Logf("%s: %d models, %d of %d table rows", path, len(ms.Models), matched, len(StandardTokenWeights))
	if matched < len(StandardTokenWeights)/2 {
		t.Errorf("only %d of %d table rows found in droid", matched, len(StandardTokenWeights))
	}
}
