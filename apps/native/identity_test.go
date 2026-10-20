package main

import (
	"encoding/json"
	"os"
	"testing"
)

// electronAppID is the bundle identifier the Electron app was installed with;
// the native app replaces it in place, so it keeps the identifier.
const electronAppID = "com.droi.app"

func TestIdentityIsTheReleaseAndTheInstalledApp(t *testing.T) {
	var app struct{ Version, Identifier string }
	b, err := os.ReadFile("mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &app); err != nil {
		t.Fatal(err)
	}
	if app.Version != devVersion {
		t.Errorf("versions: mygo.json %q, devVersion %q", app.Version, devVersion)
	}
	if app.Identifier != electronAppID {
		t.Errorf("identifier %q, the installed app's %q", app.Identifier, electronAppID)
	}
}
