package defaults

import (
	"slices"

	"github.com/kkkk2323/droi/apps/native/internal/l10n"
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

// ToolMode is whether a Session's model calls tools directly, from a Script
// program, or either way. The Daemon fixes the mode when it creates the
// Session and offers no way to change it later, so it is chosen only for new
// Sessions. Without a choice, droid decides: `toolExecutionMode` in
// ~/.factory/settings.json, else Factory's per-model list, else direct.
// "" is no choice.
type ToolMode string

const (
	DirectOnly      ToolMode = "direct_only"
	DirectAndScript ToolMode = "direct_and_script"
	ScriptOnly      ToolMode = "script_only"
)

var ToolModes = []ToolMode{DirectOnly, DirectAndScript, ScriptOnly}

var ToolModeLabels = map[ToolMode]string{
	DirectOnly:      l10n.N("Direct"),
	DirectAndScript: l10n.N("Both"),
	ScriptOnly:      l10n.N("Script"),
}

func IsToolMode(value string) bool { return slices.Contains(ToolModes, ToolMode(value)) }

// NewSessionToolMode is a new Session page's mode: the one it shows, and the
// one it asks the Daemon for. The page's own choice wins, then this device's
// default; when droid decides, the page shows what the Daemon gave the draft.
func NewSessionToolMode(choice, fallback, draft ToolMode) (shown, requested ToolMode) {
	requested = choice
	if requested == "" {
		requested = fallback
	}
	shown = requested
	if shown == "" {
		shown = draft
	}
	return shown, requested
}

// DefaultToolMode is what new Sessions on this device start with; "" leaves
// it to droid.
func DefaultToolMode(s *prefs.Store) ToolMode {
	if v := prefs.DefaultToolMode.Get(s); IsToolMode(v) {
		return ToolMode(v)
	}
	return ""
}

func SetDefaultToolMode(s *prefs.Store, mode ToolMode) { prefs.DefaultToolMode.Set(s, string(mode)) }
