package defaults

import (
	"slices"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// ToolMode is whether a Session's model calls tools directly or from a
// Script program. The Daemon fixes the mode when it creates the Session and
// decides again only at the first turn after the Session's model changes, so
// it is chosen only for new Sessions. droid gives Script only to the models
// Factory lists (Settings adds to them, ADR 0028) and resolves any other
// model to Direct, whatever mode is asked; a listed model gets Script unless
// Direct is asked. droid's own settings.json has no say. "" is no choice.
type ToolMode string

const (
	DirectOnly ToolMode = "direct_only"
	ScriptOnly ToolMode = "script_only"
)

var ToolModes = []ToolMode{DirectOnly, ScriptOnly}

var ToolModeLabels = map[ToolMode]string{
	DirectOnly: l10n.N("Direct"),
	ScriptOnly: l10n.N("Script"),
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
