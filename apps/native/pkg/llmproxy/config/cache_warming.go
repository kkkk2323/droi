package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// The cache warming mode chosen while Droi runs is saved in
// CacheWarmingModeFile so it survives a restart.

type cacheWarmingModeFile struct {
	Mode string `json:"mode"`
}

// ValidCacheWarmingMode reports whether mode is one of off, dry-run,
// streaming, idle or smart.
func ValidCacheWarmingMode(mode string) bool {
	switch mode {
	case "off", "dry-run", "streaming", "idle", "smart":
		return true
	}
	return false
}

// ApplyCacheWarmingModeFile loads the saved mode. A missing or unreadable
// file, or an unknown mode in it, leaves the mode unchanged so a bad file
// cannot stop the proxy from starting.
func (c *Config) ApplyCacheWarmingModeFile() {
	data, err := os.ReadFile(c.CacheWarmingModeFile)
	if err != nil {
		return
	}
	var f cacheWarmingModeFile
	if json.Unmarshal(data, &f) != nil {
		return
	}
	if mode := strings.ToLower(strings.TrimSpace(f.Mode)); ValidCacheWarmingMode(mode) {
		c.CacheWarmingMode = mode
	}
}

// SaveCacheWarmingMode writes the mode file through a rename, so a reader
// never sees a half-written file.
func SaveCacheWarmingMode(path, mode string) error {
	data, err := json.Marshal(cacheWarmingModeFile{Mode: mode})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
