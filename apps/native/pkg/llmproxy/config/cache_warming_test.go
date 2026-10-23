package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheWarmingModeFile(t *testing.T) {
	cfg := New()
	cfg.CacheWarmingModeFile = filepath.Join(t.TempDir(), "cache-warming.json")
	cfg.ApplyCacheWarmingModeFile()
	if cfg.CacheWarmingMode != "off" {
		t.Fatalf("missing file changed the mode to %q", cfg.CacheWarmingMode)
	}
	if err := SaveCacheWarmingMode(cfg.CacheWarmingModeFile, "idle"); err != nil {
		t.Fatal(err)
	}
	cfg.ApplyCacheWarmingModeFile()
	if cfg.CacheWarmingMode != "idle" {
		t.Fatalf("mode from file = %q, want idle", cfg.CacheWarmingMode)
	}
	if err := os.WriteFile(cfg.CacheWarmingModeFile, []byte(`{"mode":"always"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.ApplyCacheWarmingModeFile()
	if cfg.CacheWarmingMode != "idle" {
		t.Fatalf("unknown mode in file was applied: %q", cfg.CacheWarmingMode)
	}
}
