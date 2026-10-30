// Package droidhook runs Factory's own droid with Droi's LLM key hook (ADR
// 0033): a script Bun preloads into droid through BUN_OPTIONS, which gives
// every Factory LLM request the key of the Key Pool's key file while every
// other request keeps droid's own login. The droid is Factory's build,
// unchanged: Droi downloads it into a folder of its own and checks, before
// it uses a version, that the hook runs in it.
package droidhook

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// KeyFileEnv names the file the hook reads the LLM key from. The variable
// holds a path, never the key, so the key can change while droid runs.
const KeyFileEnv = "DROI_LLM_KEY_FILE"

// ReadyEnv names the file the hook writes droid's process id to once it
// runs. Bun ignores a --preload that follows another option without a word,
// so the Host checks this file before it trusts the Daemon with the Key
// Pool.
const ReadyEnv = "DROI_HOOK_READY"

//go:embed llm-hook.js
var hook []byte

// HookVersion names the embedded hook: a droid verified with another hook is
// verified again.
func HookVersion() string {
	sum := sha256.Sum256(hook)
	return hex.EncodeToString(sum[:6])
}

// WriteHook writes the hook into dir unless it is there already, and
// answers its path.
func WriteHook(dir string) (string, error) {
	path := filepath.Join(dir, "llm-hook.js")
	if b, err := os.ReadFile(path); err == nil && string(b) == string(hook) {
		return path, nil
	}
	return path, WritePrivate(path, hook)
}

// BunOptions is BUN_OPTIONS that preloads hook ahead of existing, the
// BUN_OPTIONS droid would have had.
func BunOptions(hook, existing string) string { return bunOptions(runtime.GOOS, hook, existing) }

func bunOptions(goos, hook, existing string) string {
	// Bun on Windows reads a backslash in quotes as an escape, and forward
	// slashes work there too.
	if goos == "windows" {
		hook = strings.ReplaceAll(hook, `\`, "/")
	}
	// The quotes keep a path with spaces in one argument. Bun ignores a
	// --preload that follows another option, so it goes first.
	own := `"--preload=` + hook + `"`
	existing = strings.TrimSpace(strings.ReplaceAll(existing, own, ""))
	if existing == "" {
		return own
	}
	return own + " " + existing
}

// Env is what droid gets for the hook: BUN_OPTIONS ahead of
// existingBunOptions, the key file and the ready file.
func Env(hook, keyFile, ready, existingBunOptions string) []string {
	return []string{
		"BUN_OPTIONS=" + BunOptions(hook, existingBunOptions),
		KeyFileEnv + "=" + keyFile,
		ReadyEnv + "=" + ready,
	}
}

// ErrNotLoaded is why CheckReady refuses a droid.
var ErrNotLoaded = errors.New("droidhook: the LLM key hook did not run in droid")

// CheckReady reports whether the droid of process pid wrote the ready file
// at path, which the hook does once it runs.
func CheckReady(path string, pid int) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return ErrNotLoaded
	}
	var v struct {
		PID int `json:"pid"`
	}
	if json.Unmarshal(b, &v) != nil || v.PID != pid {
		return ErrNotLoaded
	}
	return nil
}
