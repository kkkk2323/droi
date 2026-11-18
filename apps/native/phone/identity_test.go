package main

import (
	"encoding/json"
	"os"
	"testing"
)

// expoAppID is the bundle identifier the Expo Phone App was installed with;
// this one replaces it in place, so it keeps the identifier.
const expoAppID = "com.kkkk2323.droi"

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
	if app.Identifier != expoAppID {
		t.Errorf("identifier %q, the installed app's %q", app.Identifier, expoAppID)
	}
	var desktop struct{ Version string }
	if b, err = os.ReadFile("../mygo.json"); err == nil {
		err = json.Unmarshal(b, &desktop)
	}
	if err != nil {
		t.Fatal(err)
	}
	if app.Version != desktop.Version {
		t.Errorf("the Phone App is %q, the native app %q: a release is both", app.Version, desktop.Version)
	}
}
