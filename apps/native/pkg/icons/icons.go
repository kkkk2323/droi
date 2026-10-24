// Package icons holds the Lucide icons the web Client uses, as SVG files that
// scripts/gen-icons.mjs writes from the same lucide-react release.
package icons

import (
	"bytes"
	"embed"
	"fmt"
	"sync"

	"github.com/egoist/mygo/ui"
)

//go:embed svg/*.svg
var files embed.FS

type key struct {
	name   string
	stroke float32
}

var (
	mu    sync.Mutex
	cache = map[key]*ui.SVG{}
	added = map[string][]byte{}
)

// Register adds an icon of another app's own under name, an SVG drawn as
// Lucide's are: a 24×24 viewBox, stroke="currentColor" and
// stroke-width="2". A name Droi has keeps Droi's icon.
func Register(name string, svg []byte) {
	mu.Lock()
	defer mu.Unlock()
	added[name] = svg
}

func source(name string) ([]byte, error) {
	b, err := files.ReadFile("svg/" + name + ".svg")
	if err != nil {
		if a, ok := added[name]; ok {
			return a, nil
		}
	}
	return b, err
}

// Get returns the icon of a Lucide file name, such as "panel-left". It
// panics on a name that has no file: icons are part of the program.
func Get(name string) *ui.SVG { return Stroked(name, 2) }

// Stroked returns the icon drawn with another stroke width, as lucide-react's
// strokeWidth prop does.
func Stroked(name string, width float32) *ui.SVG {
	mu.Lock()
	defer mu.Unlock()
	k := key{name, width}
	if s, ok := cache[k]; ok {
		return s
	}
	b, err := source(name)
	if err != nil {
		panic("icons: no " + name)
	}
	if width != 2 {
		b = bytes.Replace(b, []byte(`stroke-width="2"`), fmt.Appendf(nil, `stroke-width="%g"`, width), 1)
	}
	s := ui.MustParseSVG(b)
	cache[k] = s
	return s
}
