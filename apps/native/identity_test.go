package main

import (
	"encoding/json"
	"os"
	"testing"
)

// The native app replaces the Electron one, so both carry the release
// version and the bundle identifier the installed app already has.
func TestIdentityMatchesTheDesktopShell(t *testing.T) {
	var app struct{ Version, Identifier string }
	var desktop struct {
		Version string
		Build   struct{ AppID string }
	}
	for path, v := range map[string]any{"mygo.json": &app, "../desktop/package.json": &desktop} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, v); err != nil {
			t.Fatal(err)
		}
	}
	if app.Version != desktop.Version || app.Version != devVersion {
		t.Errorf("versions: mygo.json %q, desktop %q, devVersion %q", app.Version, desktop.Version, devVersion)
	}
	if app.Identifier != desktop.Build.AppID {
		t.Errorf("identifier %q, the Desktop Shell's %q", app.Identifier, desktop.Build.AppID)
	}
}
