package droidhook

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func ptr(v float64) *float64 { return &v }

func TestModelMultipliers(t *testing.T) {
	start := time.Date(2026, 11, 10, 16, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 1, 8, 0, 0, 0, time.UTC)
	carbon := Model{ID: "gemini-4-carbon", Provider: "google", TokenMultiplier: 1.6, OutputTokenMultiplier: ptr(5), CacheReadTokenMultiplier: ptr(0.05),
		Promotions: []Promotion{{Discount: 0.5, StartsAt: start, ExpiresAt: end}}}
	for at, want := range map[time.Time]float64{
		start.Add(-time.Second): 1.6,
		start:                   0.8,
		end.Add(-time.Second):   0.8,
		end:                     1.6,
	} {
		if got := carbon.MultiplierAt(at); got != want {
			t.Errorf("multiplier at %v = %v, want %v", at, got, want)
		}
	}
	open := Model{TokenMultiplier: 0.6, Promotions: []Promotion{{Discount: 0.5, StartsAt: start}}}
	if got := open.MultiplierAt(end.AddDate(5, 0, 0)); got != 0.3 {
		t.Errorf("an open-ended promotion ended: %v", got)
	}
	// droid rounds the multiplier to four places.
	if got := (Model{TokenMultiplier: 0.176, Promotions: []Promotion{{Discount: 1.0 / 3}}}).MultiplierAt(start); got != 0.1173 {
		t.Errorf("rounded multiplier = %v", got)
	}

	ms := &Models{}
	if got := ms.OutputMultiplier(carbon); got != 5 {
		t.Errorf("own output multiplier = %v", got)
	}
	if got := ms.OutputMultiplier(Model{Provider: "openai"}); got != 8 {
		t.Errorf("built-in openai output = %v", got)
	}
	if got := ms.OutputMultiplier(Model{Provider: "acme"}); got != 4 {
		t.Errorf("built-in default output = %v", got)
	}
	ms = &Models{OutputByProvider: map[string]float64{"google": 7}, OutputDefault: 3}
	if got := ms.OutputMultiplier(Model{Provider: "google"}); got != 7 {
		t.Errorf("exported google output = %v", got)
	}
	if got := ms.OutputMultiplier(Model{Provider: "openai"}); got != 3 {
		t.Errorf("exported default output = %v", got)
	}
	if got := (Model{}).CacheReadMultiplier(); got != CacheReadDefault {
		t.Errorf("default cache read = %v", got)
	}
}

func writeModels(t *testing.T, path string, ms Models) {
	t.Helper()
	b, _ := json.Marshal(ms)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func manyModels(n int) []Model {
	out := make([]Model, n)
	for i := range out {
		out[i] = Model{ID: "m" + string(rune('a'+i%26)) + string(rune('a'+i/26)), TokenMultiplier: 1}
	}
	return out
}

// An export that found too few models, or that failed, is not used.
func TestReadModelsRejectsAPoorExport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	writeModels(t, path, Models{Models: manyModels(minModels)})
	if _, err := ReadModels(path); err != nil {
		t.Errorf("a full export: %v", err)
	}
	writeModels(t, path, Models{Models: manyModels(3)})
	if _, err := ReadModels(path); err == nil {
		t.Error("an export of 3 models is used")
	}
	writeModels(t, path, Models{Models: manyModels(minModels), Error: "boom"})
	if _, err := ReadModels(path); err == nil {
		t.Error("a failed export is used")
	}
}

// Store.Models keeps the export of each build and makes it again only when
// the export changed.
func TestStoreKeepsTheExport(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(s.ModelsPath("1.7.1")), 0o700); err != nil {
		t.Fatal(err)
	}
	runs := 0
	t.Cleanup(SetExportForTest(func(_ context.Context, exe, out string) (*Models, error) {
		runs++
		if exe != s.Path("1.7.1") {
			t.Errorf("exported %s", exe)
		}
		ms := &Models{Export: exportVersion(), Models: manyModels(minModels)}
		b, _ := json.Marshal(ms)
		return ms, os.WriteFile(out, b, 0o600)
	}))
	for range 2 {
		if _, err := s.Models(context.Background(), "1.7.1"); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 1 {
		t.Errorf("exported %d times, want once", runs)
	}
	writeModels(t, s.ModelsPath("1.7.1"), Models{Export: "older", Models: manyModels(minModels)})
	if _, err := s.Models(context.Background(), "1.7.1"); err != nil || runs != 2 {
		t.Errorf("an older export was kept: %v, %d runs", err, runs)
	}
}

// realDroid is a droid on this computer for the tests that run one:
// DROID_PATH, else Droi's newest.
func realDroid(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("DROID_PATH"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Skip("no droid on this computer; DROID_PATH picks one")
	}
	found, _ := filepath.Glob(filepath.Join(dir, "Droi", "droid", "*", "droid*"))
	for i := len(found) - 1; i >= 0; i-- {
		if filepath.Ext(found[i]) == "" || filepath.Ext(found[i]) == ".exe" {
			return found[i]
		}
	}
	t.Skip("no droid on this computer; DROID_PATH picks one")
	return ""
}

// The export runs in a real droid and finds its registry: a change to how
// droid bundles it shows here first.
func TestExportModelsFromDroid(t *testing.T) {
	exe := realDroid(t)
	out := filepath.Join(t.TempDir(), "models.json")
	start := time.Now()
	ms, err := ExportModels(context.Background(), exe, out)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d models, outputs %v/%v, in %v", exe, len(ms.Models), ms.OutputByProvider, ms.OutputDefault, time.Since(start))
	if len(ms.OutputByProvider) == 0 {
		t.Error("the output multipliers by provider were not found")
	}
	byID := map[string]Model{}
	for _, m := range ms.Models {
		byID[m.ID] = m
	}
	opus, ok := byID["claude-opus-5-5"]
	if !ok || opus.TokenMultiplier != 1.6 || ms.OutputMultiplier(opus) != 5 || opus.CacheReadMultiplier() != 0.05 || opus.BillingPool != "standard" {
		t.Errorf("claude-opus-5-5 = %+v", opus)
	}
	if k := byID["kimi-k3"]; k.BillingPool != "core" {
		t.Errorf("kimi-k3 = %+v", k)
	}
	if again, err := ReadModels(out); err != nil || again.Export != exportVersion() || len(again.Models) != len(ms.Models) {
		t.Errorf("read back: %v, %+v", err, again)
	}
}
