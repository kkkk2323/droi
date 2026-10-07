package models

import "strings"

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
	Other:     "Other",
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
}

// BrandOf reads the brand off the model id; provider is "" for the Auto router.
func BrandOf(modelID, provider string) Brand {
	id := strings.ToLower(modelID)
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
