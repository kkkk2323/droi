package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
)

// ScriptModelList is where the models that may use Script stand (ADR 0028):
// Factory's list, as droid last had it, and what Settings added.
//
// Factory's list comes from the proxy while the Key Pool is on, which sees
// the reply before Settings' models go into it. Otherwise, and until the
// proxy has seen one, it is droid's own copy of the feature flags, in the
// cache folder of the Factory home the Daemon runs as, less what Settings
// added, since a Key Pool that was on left those in it.
func (h *Host) ScriptModelList() gateway.ScriptModelList {
	var added []string
	if h.cfg.ScriptModels != nil {
		added = slices.Clone(h.cfg.ScriptModels())
	}
	if h.usesDroiDroid() {
		if models, ok := h.KeyPool().FactoryScriptModels(); ok {
			return gateway.ScriptModelList{Factory: models, Added: added, Known: true}
		}
	}
	models, ok := cachedScriptModels(filepath.Join(h.FactoryHome(), "cache", "feature-flags.json"))
	return gateway.ScriptModelList{
		Factory: slices.DeleteFunc(models, func(m string) bool { return slices.Contains(added, m) }),
		Added:   added,
		Known:   ok,
	}
}

// cachedScriptModels are the models droid's feature flags cache at file
// lists for Script; false when it has none.
func cachedScriptModels(file string) ([]string, bool) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, false
	}
	var cache struct {
		Configs struct {
			Section *struct {
				Models []string `json:"models"`
			} `json:"script_tool_models"`
		} `json:"configs"`
	}
	if json.Unmarshal(b, &cache) != nil || cache.Configs.Section == nil {
		return nil, false
	}
	return cache.Configs.Section.Models, true
}
