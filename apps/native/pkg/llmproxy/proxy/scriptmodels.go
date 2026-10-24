package proxy

import (
	"encoding/json"
	"net/http"
	"slices"
)

// featureFlagsPath is droid's dynamic config. Among other flags it carries
// configs.script_tool_models: the models whose Sessions may call tools from a
// Script program. droid resolves a model outside that list to direct_only,
// even when the Session asks for script_only.
const featureFlagsPath = "/api/feature-flags"

// maxFeatureFlagsBody bounds the reply the rewrite holds in memory; a larger
// one passes through unchanged.
const maxFeatureFlagsBody = 4 << 20

// SetScriptModels sets what answers the model IDs Settings lets use Script in
// addition to Factory's list; nil adds none.
func (p *Proxy) SetScriptModels(models func() []string) {
	p.mu.Lock()
	p.scriptModels = models
	p.mu.Unlock()
}

func (p *Proxy) scriptModelsNow() []string {
	p.mu.Lock()
	fn := p.scriptModels
	p.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn()
}

func isFeatureFlags(method, path string) bool {
	return method == http.MethodGet && path == featureFlagsPath
}

// addScriptModels adds models to configs.script_tool_models.models and answers
// the reply with the models it added. Every other part of the reply stays as
// Factory sent it; a reply without that config is returned unchanged.
func addScriptModels(body []byte, models []string) (out []byte, added []string) {
	if len(models) == 0 {
		return body, nil
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil {
		return body, nil
	}
	var configs map[string]json.RawMessage
	raw, ok := top["configs"]
	if !ok || json.Unmarshal(raw, &configs) != nil {
		return body, nil
	}
	raw, ok = configs["script_tool_models"]
	if !ok {
		return body, nil
	}
	section := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &section) != nil {
		return body, nil
	}
	var listed []string
	if raw, ok := section["models"]; ok && json.Unmarshal(raw, &listed) != nil {
		return body, nil
	}
	for _, m := range models {
		if m != "" && !slices.Contains(listed, m) {
			listed = append(listed, m)
			added = append(added, m)
		}
	}
	if len(added) == 0 {
		return body, nil
	}
	var err error
	if section["models"], err = json.Marshal(listed); err != nil {
		return body, nil
	}
	if configs["script_tool_models"], err = json.Marshal(section); err != nil {
		return body, nil
	}
	if top["configs"], err = json.Marshal(configs); err != nil {
		return body, nil
	}
	if out, err = json.Marshal(top); err != nil {
		return body, nil
	}
	return out, added
}
