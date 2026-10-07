package host

import (
	"reflect"
	"testing"
)

func existsIn(present ...string) func(string) bool {
	return func(p string) bool {
		for _, x := range present {
			if x == p {
				return true
			}
		}
		return false
	}
}

func TestLocateOpenInAppsInCatalogOrder(t *testing.T) {
	found := LocateOpenInApps("darwin", "/Users/me", existsIn(
		"/System/Library/CoreServices/Finder.app",
		"/Users/me/Applications/Cursor.app",
		"/Applications/Visual Studio Code.app",
		"/System/Applications/Utilities/Terminal.app",
	))
	var got [][2]string
	for _, a := range found {
		got = append(got, [2]string{a.ID, a.AppPath})
	}
	want := [][2]string{
		{"vscode", "/Applications/Visual Studio Code.app"},
		{"cursor", "/Users/me/Applications/Cursor.app"},
		{"finder", "/System/Library/CoreServices/Finder.app"},
		{"terminal", "/System/Applications/Utilities/Terminal.app"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestLocateOpenInAppsFirstBundleFound(t *testing.T) {
	found := LocateOpenInApps("darwin", "/Users/me", existsIn("/Applications/Zed Preview.app"))
	want := []OpenInApp{{ID: "zed", Label: "Zed", AppPath: "/Applications/Zed Preview.app"}}
	if !reflect.DeepEqual(found, want) {
		t.Fatalf("got %v", found)
	}
}

func TestLocateOpenInAppsEmptyOffMac(t *testing.T) {
	if found := LocateOpenInApps("linux", "/Users/me", func(string) bool { return true }); len(found) != 0 {
		t.Fatalf("got %v", found)
	}
}
