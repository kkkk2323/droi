// Package textsize is the Client's text size, kept per device like the theme
// (a port of text-size.ts).
package textsize

import (
	"slices"

	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

type Size string

const (
	Small   Size = "small"
	Default Size = "default"
	Large   Size = "large"
	Largest Size = "largest"
)

var Sizes = []Size{Small, Default, Large, Largest}

var Labels = map[Size]string{Small: "Small", Default: "Default", Large: "Large", Largest: "Largest"}

// Scale is how much each size scales the Client's text, relative to the default.
var Scale = map[Size]float64{Small: 0.90625, Default: 1, Large: 1.09375, Largest: 1.1875}

// Get reads the stored size; one nobody knows reads as the default.
func Get(s *prefs.Store) Size {
	if v := Size(prefs.TextSize.Get(s)); slices.Contains(Sizes, v) {
		return v
	}
	return Default
}

func Set(s *prefs.Store, size Size) { prefs.TextSize.Set(s, string(size)) }
