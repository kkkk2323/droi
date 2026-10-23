// Package trayicon holds the Key Pool's menu bar icons: a gauge whose
// needle leans by how much monthly quota is left, black for macOS to tint
// to the menu bar, and in red where the tint would hide a warning.
package trayicon

import "embed"

//go:generate go run gen.go

//go:embed ok.png warning.png danger.png unknown.png expiring.png
var files embed.FS

// For is the PNG of a state ("ok", "warning", "danger", "unknown",
// "expiring"); an unknown state gets the unknown icon.
func For(state string) []byte {
	data, err := files.ReadFile(state + ".png")
	if err != nil {
		data, _ = files.ReadFile("unknown.png")
	}
	return data
}

// IsTemplate reports whether the system may tint the icon to the bar: the
// expiring icon keeps its red.
func IsTemplate(state string) bool { return state != "expiring" }
