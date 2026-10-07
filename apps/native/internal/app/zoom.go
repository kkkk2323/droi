package app

import (
	"slices"

	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

// zoomFactors are Chromium's zoom levels, which ⌘= and ⌘- step through in
// the Desktop Shell, on top of Settings → Text size.
var zoomFactors = []float64{0.25, 1.0 / 3, 0.5, 2.0 / 3, 0.75, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2, 2.5, 3, 4, 5}

// Zoom steps the window's zoom: +1 in, -1 out, 0 back to 100%.
func (a *App) Zoom(step int) {
	cur := prefs.Zoom.Get(a.prefs)
	i := slices.IndexFunc(zoomFactors, func(f float64) bool { return f >= cur-0.001 })
	if i < 0 {
		i = len(zoomFactors) - 1
	}
	switch {
	case step == 0:
		i = slices.Index(zoomFactors, 1)
	case step > 0 && zoomFactors[i] <= cur+0.001:
		i = min(i+1, len(zoomFactors)-1)
	case step < 0:
		i = max(i-1, 0)
	}
	prefs.Zoom.Set(a.prefs, zoomFactors[i])
	a.applyPrefs()
}
