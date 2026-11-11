package llmusage

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/droidhook"
)

// droidTable is droid's own model registry (droidhook.Models) by model ID:
// the weights of the build Droi runs, with its promotions, so a model is
// priced as soon as Droi runs a droid that offers it, with no Droi release.
// StandardTokenWeights stays the fallback for a droid whose registry could
// not be read.
type droidTable struct {
	src  *droidhook.Models
	byID map[string]droidhook.Model
}

var droidModels atomic.Pointer[droidTable]

// UseDroidModels prices the request log with droid's registry; nil keeps
// what was there.
func UseDroidModels(ms *droidhook.Models) {
	if ms == nil {
		return
	}
	if cur := droidModels.Load(); cur != nil && cur.src == ms {
		return
	}
	t := &droidTable{src: ms, byID: make(map[string]droidhook.Model, len(ms.Models))}
	for _, m := range ms.Models {
		t.byID[strings.ToLower(m.ID)] = m
	}
	droidModels.Store(t)
}

// droidWeightAt is model's weight in droid's registry at the time of a
// call, the promotion running then taken off: by its ID, or with a date on
// one side and not the other.
func droidWeightAt(m string, at time.Time) (TokenWeight, bool) {
	t := droidModels.Load()
	if t == nil {
		return TokenWeight{}, false
	}
	mod, ok := t.byID[m]
	if !ok {
		if i := strings.LastIndexByte(m, '-'); i > 0 && datedModelSuffix(m[i:]) {
			mod, ok = t.byID[m[:i]]
		}
	}
	if !ok {
		for id, d := range t.byID {
			if s, dated := strings.CutPrefix(id, m); dated && datedModelSuffix(s) {
				mod, ok = d, true
				break
			}
		}
	}
	if !ok {
		return TokenWeight{}, false
	}
	mult := mod.MultiplierAt(at)
	return TokenWeight{
		Model: mod.Name, ID: mod.ID,
		Input: mult, Output: mult * t.src.OutputMultiplier(mod), CacheRead: mult * mod.CacheReadMultiplier(),
	}, true
}
