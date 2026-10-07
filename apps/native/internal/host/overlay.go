package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// MemoryServerName is the Memory Server's name in the Daemon's MCP list.
const MemoryServerName = "droi-memory"

// The app's own executable runs the Memory Server and the hook as these
// subcommands, where the Electron Shell ran its own Node.
const (
	MemoryServerCommand = "memory-server"
	MemoryHookCommand   = "memory-hook"
)

// HookEvents are the Session events Memory's hook runs at.
var HookEvents = []string{"SessionStart", "UserPromptSubmit", "PreCompact", "SessionEnd"}

const hookTimeoutSeconds = 10

// MemoryAttachment is what the Runtime Overlay attaches (ADR 0010).
type MemoryAttachment struct {
	Executable string
	MemoryDir  string
	// ApprovedAt is the permission record's time, as the Daemon writes one
	// when a user approves.
	ApprovedAt string
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// hookCommandLine is the hook for the shell the Daemon runs hooks with: sh,
// or cmd.exe on Windows. Windows paths hold no double quote, so quoting them
// is enough there; `set "K=V"` keeps the space before && out of the value.
func hookCommandLine(goos, memoryDir, executable string) string {
	if goos == "windows" {
		return `set "DROI_MEMORY_DIR=` + memoryDir + `"&& "` + executable + `" ` + MemoryHookCommand
	}
	return "DROI_MEMORY_DIR=" + shellQuote(memoryDir) + " " + shellQuote(executable) + " " + MemoryHookCommand
}

// BuildRuntimeOverlay is the settings file the Daemon merges for its own
// process with --settings: the Memory Server pre-approved and loaded before
// the first turn, and the hook at every Session event. Shape as droid
// 0.229.0 reads it: MCP under "mcp", general keys at the top level.
func BuildRuntimeOverlay(m MemoryAttachment) map[string]any {
	env := map[string]string{"DROI_MEMORY_DIR": m.MemoryDir}
	// Hooks run through the shell; the environment rides on the command line
	// so it does not depend on the Daemon passing the overlay's env to them.
	hookCommand := hookCommandLine(runtime.GOOS, m.MemoryDir, m.Executable)
	hook := []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": hookCommand, "timeout": hookTimeoutSeconds}}}}
	hooks := map[string]any{}
	for _, e := range HookEvents {
		hooks[e] = hook
	}
	return map[string]any{
		"env": env,
		// Otherwise a Session's first turn can start before the Memory Server is connected.
		"blockOnMcpLoad": true,
		"mcp": map[string]any{
			"mcpServers": map[string]any{
				MemoryServerName: map[string]any{
					"type":    "stdio",
					"command": m.Executable,
					"args":    []string{MemoryServerCommand},
					"env":     env,
				},
			},
			// The Daemon classifies an unknown MCP tool as high impact, so anything
			// lower would make every Session stop and ask before a Memory write.
			"persistentPermissions": map[string]any{
				"servers": map[string]any{MemoryServerName: map[string]any{"approvedAt": m.ApprovedAt, "impactLevel": "high"}},
			},
		},
		"mcpAutonomyOverrides": map[string]any{MemoryServerName: map[string]any{"defaultLevel": "low"}},
		"hooks":                hooks,
	}
}

// WriteRuntimeOverlay writes the overlay as JSON only the user can read.
func WriteRuntimeOverlay(path string, overlay map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(overlay, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
