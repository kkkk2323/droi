package main

import (
	"testing"

	"github.com/egoist/mygo"
)

// Paste is the app's own, so the composer can take an image first; the
// rest of the Edit menu stays MyGo's roles.
func TestTheEditMenuPastesThroughTheApp(t *testing.T) {
	pasted := 0
	edit := editMenu(func() { pasted++ })
	var paste *mygo.MenuItem
	for _, it := range edit.Submenu {
		if it.Role == mygo.RolePaste {
			t.Fatal("the Edit menu keeps the Paste role, which bypasses the composer")
		}
		if it.Label == "Paste" {
			paste = it
		}
	}
	if paste == nil || paste.Accelerator != "CmdOrCtrl+V" || paste.Click == nil {
		t.Fatalf("Paste: %+v", paste)
	}
	paste.Click(paste, nil)
	if pasted != 1 {
		t.Fatalf("Paste ran the app's paste %d times", pasted)
	}
}
