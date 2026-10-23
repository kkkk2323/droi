package keys

import (
	"strconv"
	"strings"
)

// Billing pools as reported by Factory. Every request is billed against the
// key's Standard pool first and only spills into the Droid Core pool once the
// Standard bucket is exhausted, so a Standard-only model must never be routed
// to a key whose Standard pool is exhausted. Mirrors dproxy-apps'
// modelcatalog.BillingPool classification.
const (
	PoolStandard = "standard"
	PoolCore     = "core"
	PoolAny      = "any"
)

// coreModelIDs lists Factory models billed against the Droid Core pool. The
// list mirrors dproxy-apps' modelcatalog so a model that can run on Core is
// allowed to fall through to a key's Core quota. Any model not listed here is
// treated as Standard-only, which is the safe default: it may only use a key
// whose Standard pool still has headroom.
var coreModelIDs = map[string]bool{
	"deepseek-v4-flash-0731": true,
	"deepseek-v4-pro":        true,
	"garnet-07-15":           true,
	"glm-4.6":                true,
	"glm-4.7":                true,
	"glm-5":                  true,
	"glm-5.1":                true,
	"glm-5.2":                true,
	"glm-5.2-fast":           true,
	"glm-5.3":                true,
	"glm-5.3-flash":          true,
	"kimi-k2.5":              true,
	"kimi-k2.6":              true,
	"kimi-k2.7-code":         true,
	"kimi-k3":                true,
	"minimax-m2.5":           true,
	"minimax-m2.7":           true,
	"minimax-m3":             true,
	"nemotron-3-ultra":       true,
}

// IsCoreModel reports whether modelID is billed against the Droid Core pool.
func IsCoreModel(modelID string) bool {
	return coreModelIDs[normalizeModelID(modelID)]
}

// ModelBillingPool returns the billing pool a request for modelID belongs to.
// An empty model yields PoolAny (prefer a Standard-healthy key but fall back to
// Core, preserving the pre-existing behaviour for callers that cannot tell us
// the model). An unrecognized model yields PoolStandard, the safe default:
// Standard-only models must never be routed onto Core-only capacity.
func ModelBillingPool(modelID string) string {
	if strings.TrimSpace(modelID) == "" {
		return PoolAny
	}
	if IsCoreModel(modelID) {
		return PoolCore
	}
	return PoolStandard
}

// normalizeModelID lowercases a model ID and unwraps Droid's custom-model
// aliases ("custom:kimi-k3-0" -> "kimi-k3") so catalog lookups match.
func normalizeModelID(modelID string) string {
	m := strings.ToLower(strings.TrimSpace(modelID))
	rest, ok := strings.CutPrefix(m, "custom:")
	if !ok {
		return m
	}
	m = rest
	if i := strings.LastIndex(m, "-"); i >= 0 {
		if _, err := strconv.Atoi(m[i+1:]); err == nil {
			m = m[:i]
		}
	}
	return m
}
