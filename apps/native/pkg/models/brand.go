package models

import (
	"strings"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// Which company a model comes from, for grouping and the brand mark. The
// Daemon's modelProvider is the wire dialect (a GLM served through Factory
// still says "anthropic" or "generic-chat-completion-api"), so the model id's
// prefix wins and the provider is only a fallback.
type Brand string

const (
	Anthropic Brand = "anthropic"
	OpenAI    Brand = "openai"
	Google    Brand = "google"
	XAI       Brand = "xai"
	Zhipu     Brand = "zhipu"
	Moonshot  Brand = "moonshot"
	DeepSeek  Brand = "deepseek"
	MiniMax   Brand = "minimax"
	Other     Brand = "other"
)

var BrandLabels = map[Brand]string{
	Anthropic: "Anthropic",
	OpenAI:    "OpenAI",
	Google:    "Google",
	XAI:       "xAI",
	Zhipu:     "Zhipu",
	Moonshot:  "Moonshot",
	DeepSeek:  "DeepSeek",
	MiniMax:   "MiniMax",
	Other:     l10n.N("Other"),
}

// BrandOrder is the rail order in the picker; brands the Daemon does not
// offer are skipped.
var BrandOrder = []Brand{Anthropic, OpenAI, Google, XAI, Zhipu, Moonshot, DeepSeek, MiniMax, Other}

var idPrefixes = []struct {
	prefix string
	brand  Brand
}{
	{"claude", Anthropic},
	{"gpt", OpenAI},
	{"o1", OpenAI},
	{"o3", OpenAI},
	{"o4", OpenAI},
	{"gemini", Google},
	{"grok", XAI},
	{"glm", Zhipu},
	{"kimi", Moonshot},
	{"deepseek", DeepSeek},
	{"minimax", MiniMax},
}

var providerBrands = map[string]Brand{
	"anthropic": Anthropic,
	"openai":    OpenAI,
	"google":    Google,
	"xai":       XAI,
	// A gateway such as Magpie names a model "provider/model", where the
	// provider is its own name for the company the model comes from.
	"claude":   Anthropic,
	"codex":    OpenAI,
	"copilot":  OpenAI,
	"grok":     XAI,
	"glm":      Zhipu,
	"zhipu":    Zhipu,
	"kimi":     Moonshot,
	"moonshot": Moonshot,
	"deepseek": DeepSeek,
	"minimax":  MiniMax,
}

// BrandOf reads the brand off the model id; provider is "" for the Auto router.
func BrandOf(modelID, provider string) Brand {
	id := strings.ToLower(modelID)
	// Droi names a Magpie model "custom:magpie-<provider>-<model>-<hash>", and
	// the Daemon reports that id, not the gateway's "provider/model": the
	// provider after the prefix names the company.
	if rest, ok := strings.CutPrefix(id, magpieIDPrefix); ok {
		if b, ok := gatewayBrand(rest); ok {
			return b
		}
		// A gateway provider Droi does not know: the model's own name may still
		// say, as in "workbuddy-glm-5-3-…".
		if _, name, ok := strings.Cut(rest, "-"); ok {
			for _, p := range idPrefixes {
				if strings.HasPrefix(name, p.prefix) {
					return p.brand
				}
			}
		}
		return Other
	}
	// "provider/model", as a gateway names one: the provider names the company,
	// and the model keeps its own name after the last slash.
	if i := strings.Index(id, "/"); i > 0 {
		if b, ok := providerBrands[id[:i]]; ok {
			return b
		}
		id = id[strings.LastIndex(id, "/")+1:]
	}
	for _, p := range idPrefixes {
		if strings.HasPrefix(id, p.prefix) {
			return p.brand
		}
	}
	if b, ok := providerBrands[provider]; ok {
		return b
	}
	return Other
}

// magpieIDPrefix starts the id Droi gives a Magpie model (package host builds
// it); the rest is "<provider>-<model>-<hash>".
const magpieIDPrefix = "custom:magpie-"

// IsMagpie reports that a model id names a Magpie gateway model, which the
// picker keeps in a tab of its own: the gateway offers models of the same
// names as the Daemon's own, and beside them the list reads as one crowd.
func IsMagpie(modelID string) bool {
	return strings.HasPrefix(strings.ToLower(modelID), magpieIDPrefix)
}

// gatewayBrand reads a gateway provider's brand; its name may hold dashes, so
// the longest known one that the name starts with wins.
func gatewayBrand(rest string) (Brand, bool) {
	var best string
	var brand Brand
	for name, b := range providerBrands {
		if strings.HasPrefix(rest, name+"-") && len(name) > len(best) {
			best, brand = name, b
		}
	}
	return brand, best != ""
}
