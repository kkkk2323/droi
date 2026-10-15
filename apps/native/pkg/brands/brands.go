// Package brands holds the model providers' marks the model picker shows:
// the color marks the web Client renders from @lobehub/icons (MIT), and
// OpenAI's, xAI's and Moonshot's drawn in the text color.
package brands

import (
	"embed"
	"sync"

	"github.com/egoist/mygo/ui"
)

//go:embed svg/*.svg
var files embed.FS

var (
	mu    sync.Mutex
	cache = map[string]*ui.SVG{}
)

// Mark returns a brand's mark, nil for a brand without one (the Auto
// router and others show Lucide's sparkles).
func Mark(brand string) *ui.SVG {
	mu.Lock()
	defer mu.Unlock()
	if s, ok := cache[brand]; ok {
		return s
	}
	var s *ui.SVG
	if b, err := files.ReadFile("svg/" + brand + ".svg"); err == nil {
		s, _ = ui.ParseSVG(b)
	}
	cache[brand] = s
	return s
}
