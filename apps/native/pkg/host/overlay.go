package host

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// MemoryServerName is the Memory Server's name in the Daemon's MCP list.
const MemoryServerName = "droi-memory"

// The app's own executable runs the Memory Server and the hook as these
// subcommands, where the Electron Shell ran its own Node.
const (
	MemoryServerCommand = "memory-server"
	MemoryHookCommand   = "memory-hook"
)

const hookTimeoutSeconds = 10

// sessionStartMatcher leaves out "resume": the Daemon writes every hook run
// into the Session's file, and the Session list dates a Session by that
// file, so merely opening one would move it to the top.
const sessionStartMatcher = "^(startup|clear|compact)$"

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

// OverlayOptions are what the Host adds to the Runtime Overlay besides
// Memory.
type OverlayOptions struct {
	// CustomModels are droid's customModels entries: the models the Daemon may
	// run that are not Factory's, such as a Magpie gateway's.
	CustomModels []any
	// MCPServers are the MCP servers the Daemon loads for every Session,
	// beside Memory's, by name. A name Memory's server has is left out.
	MCPServers map[string]MCPServer
	// BlockOnMCPLoad holds a Session's first turn until its MCP servers are
	// connected, as Memory does.
	BlockOnMCPLoad bool
	// Hooks are droid's hooks by event, such as "PreToolUse": lists of
	// {"matcher": ..., "hooks": [...]} entries, run after Memory's.
	Hooks map[string][]any
	// Env is added to the environment of the Daemon's Sessions.
	Env map[string]string
	// ApprovedAt is the time the approvals of MCPServers record, as the
	// Daemon writes it; "" is now.
	ApprovedAt string
}

// MCPServer is one MCP server of the Runtime Overlay.
type MCPServer struct {
	// Config is droid's mcpServers entry: {"type": "stdio", "command": ...,
	// "args": [...], "env": {...}} or {"type": "http", "url": ..., "headers": {...}}.
	Config map[string]any
	// Approved records the server as approved, as a user's approval does, so
	// that no Session asks before loading it.
	Approved bool
	// Autonomy, optional, is the autonomy level its tools run at without
	// asking ("low", "medium" or "high"). The Daemon treats an unknown MCP
	// tool as high impact, so "low" lets every Session run them, as Memory's.
	Autonomy string
	// Tools, optional, are the autonomy levels of single tools, by their
	// name on the server, over Autonomy: {"run_pipeline": "high"} has every
	// Session below autonomy High ask before each run, while the server's
	// other tools run at Autonomy.
	Tools map[string]string
}

// Add is o with more's: lists appended, maps merged, a name o has kept.
func (o OverlayOptions) Add(more OverlayOptions) OverlayOptions {
	o.CustomModels = append(slices.Clip(o.CustomModels), more.CustomModels...)
	o.MCPServers = mergeNew(o.MCPServers, more.MCPServers)
	o.BlockOnMCPLoad = o.BlockOnMCPLoad || more.BlockOnMCPLoad
	if len(more.Hooks) > 0 {
		hooks := make(map[string][]any, len(o.Hooks)+len(more.Hooks))
		for e, l := range o.Hooks {
			hooks[e] = slices.Clip(l)
		}
		for e, l := range more.Hooks {
			hooks[e] = append(hooks[e], l...)
		}
		o.Hooks = hooks
	}
	o.Env = mergeNew(o.Env, more.Env)
	if o.ApprovedAt == "" {
		o.ApprovedAt = more.ApprovedAt
	}
	return o
}

// empty says whether o adds nothing.
func (o OverlayOptions) empty() bool {
	return len(o.CustomModels) == 0 && len(o.MCPServers) == 0 && !o.BlockOnMCPLoad && len(o.Hooks) == 0 && len(o.Env) == 0
}

// mergeNew is a copy of a with what of b's a has no key for.
func mergeNew[V any](a, b map[string]V) map[string]V {
	if len(b) == 0 {
		return a
	}
	out := make(map[string]V, len(a)+len(b))
	for k, v := range b {
		out[k] = v
	}
	for k, v := range a {
		out[k] = v
	}
	return out
}

// BuildRuntimeOverlay is the settings file the Daemon merges for its own
// process with --settings: the Memory Server pre-approved and loaded before
// the first turn, and the hook at SessionStart but a resume. Shape as droid
// 0.229.0 reads it: MCP under "mcp", general keys at the top level.
func BuildRuntimeOverlay(m MemoryAttachment) map[string]any {
	return BuildRuntimeOverlayWith(m, OverlayOptions{})
}

// BuildRuntimeOverlayWith is BuildRuntimeOverlay with what else the Host
// attaches. An attachment without an Executable leaves Memory out.
func BuildRuntimeOverlayWith(m MemoryAttachment, o OverlayOptions) map[string]any {
	overlay := map[string]any{}
	if m.Executable != "" {
		for k, v := range memoryOverlay(m) {
			overlay[k] = v
		}
	}
	if len(o.CustomModels) > 0 {
		overlay["customModels"] = o.CustomModels
	}
	if len(o.Env) > 0 {
		env, _ := overlay["env"].(map[string]string)
		overlay["env"] = mergeNew(env, o.Env)
	}
	if o.BlockOnMCPLoad {
		overlay["blockOnMcpLoad"] = true
	}
	if len(o.MCPServers) > 0 {
		approvedAt := o.ApprovedAt
		if approvedAt == "" {
			approvedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		}
		mcp := subMap(overlay, "mcp")
		servers := subMap(mcp, "mcpServers")
		for _, name := range slices.Sorted(maps.Keys(o.MCPServers)) {
			if _, taken := servers[name]; taken {
				continue
			}
			s := o.MCPServers[name]
			servers[name] = s.Config
			if s.Approved {
				subMap(subMap(mcp, "persistentPermissions"), "servers")[name] = map[string]any{"approvedAt": approvedAt, "impactLevel": "high"}
			}
			level := map[string]any{}
			if s.Autonomy != "" {
				level["defaultLevel"] = s.Autonomy
			}
			if len(s.Tools) > 0 {
				level["tools"] = maps.Clone(s.Tools)
			}
			if len(level) > 0 {
				subMap(overlay, "mcpAutonomyOverrides")[name] = level
			}
		}
	}
	if len(o.Hooks) > 0 {
		hooks := subMap(overlay, "hooks")
		for e, l := range o.Hooks {
			before, _ := hooks[e].([]any)
			hooks[e] = append(slices.Clip(before), l...)
		}
	}
	return overlay
}

// subMap is m[key] as a map, made when m has none.
func subMap(m map[string]any, key string) map[string]any {
	if c, ok := m[key].(map[string]any); ok {
		return c
	}
	c := map[string]any{}
	m[key] = c
	return c
}

// memoryOverlay is Memory's part of the overlay.
func memoryOverlay(m MemoryAttachment) map[string]any {
	env := map[string]string{"DROI_MEMORY_DIR": m.MemoryDir}
	// Hooks run through the shell; the environment rides on the command line
	// so it does not depend on the Daemon passing the overlay's env to them.
	hookCommand := hookCommandLine(runtime.GOOS, m.MemoryDir, m.Executable)
	command := []any{map[string]any{"type": "command", "command": hookCommand, "timeout": hookTimeoutSeconds}}
	// Memory's hook runs at SessionStart only (ADR 0031).
	hooks := map[string]any{"SessionStart": []any{map[string]any{"matcher": sessionStartMatcher, "hooks": command}}}
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
			// lower would make every Session stop and ask before a Memory call.
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
