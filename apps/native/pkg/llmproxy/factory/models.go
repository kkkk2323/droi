// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: ANTHROPIC/RESPONSES/CHAT/GEMINI 33-39, E5/E6/E4 41-43, MODELS 59-116, byId 118.

package factory

// Wire is the API Factory serves a model on, as droid sends it: the route under /api/llm and the shape of the body.
type Wire string

const (
	// WireAnthropic is Anthropic's Messages, under /api/llm/a/v1.
	WireAnthropic Wire = "anthropic"
	// WireResponses is OpenAI's Responses, under /api/llm/o/v1.
	WireResponses Wire = "responses"
	// WireChat is OpenAI's chat completions, under /api/llm/o/v1.
	WireChat Wire = "chat"
	// WireGemini is Google's generateContent, under /api/llm/g/v1.
	WireGemini Wire = "gemini"
)

// Model is one model droid's picker offers: its name, the wire droid sends it on, the vendor it names in
// x-api-provider, its context window and output limit in tokens (0 where droid's registry has none), the reasoning
// efforts it takes, whether it reads images, and its price as a multiplier of Factory's token rate.
type Model struct {
	ID       string
	Name     string
	Wire     Wire
	Upstream string
	Context  int
	Output   int
	Efforts  []string
	Images   bool
	Rate     float64
}

var (
	e4 = []string{"low", "medium", "high", "xhigh"}
	e5 = []string{"low", "medium", "high", "xhigh", "max"}
	e6 = []string{"none", "low", "medium", "high", "xhigh", "max"}
)

// models are droid's picker, less auto, in upstream's order. Left out as upstream leaves them out: GPT-6 Astra Ultra
// Fast (offered only after Factory's high-cost terms are accepted), Opus 4.6/4.7 Fast and GPT-5.4 Mini Fast (their
// base models are not offered), GPT-5.6 Sol Fast (unreleased) and GLM-5.2 Fast (behind a feature flag).
var models = []Model{
	{ID: "claude-fable-5.1", Name: "Fable 5.1", Wire: WireAnthropic, Upstream: "anthropic", Context: 867000, Output: 128000, Efforts: e5, Images: true, Rate: 4},
	{ID: "claude-fable-5", Name: "Fable 5", Wire: WireAnthropic, Upstream: "anthropic", Context: 867000, Output: 128000, Efforts: e5, Images: true, Rate: 4},
	{ID: "claude-opus-5-5", Name: "Opus 5.5", Wire: WireAnthropic, Upstream: "anthropic", Context: 872000, Output: 128000, Efforts: e5, Images: true, Rate: 1.6},
	{ID: "claude-opus-5", Name: "Opus 5", Wire: WireAnthropic, Upstream: "anthropic", Context: 867000, Output: 128000, Efforts: e5, Images: true, Rate: 2},
	{ID: "claude-opus-4-8", Name: "Opus 4.8", Wire: WireAnthropic, Upstream: "anthropic", Context: 867000, Output: 128000, Efforts: e5, Images: true, Rate: 2},
	{ID: "claude-sonnet-5-5", Name: "Sonnet 5.5", Wire: WireAnthropic, Upstream: "anthropic", Context: 872000, Output: 128000, Efforts: e5, Images: true, Rate: 0.8},
	{ID: "claude-sonnet-5", Name: "Sonnet 5", Wire: WireAnthropic, Upstream: "anthropic", Context: 872000, Output: 128000, Efforts: e5, Images: true, Rate: 0.8},
	{ID: "claude-sonnet-4-6", Name: "Sonnet 4.6", Wire: WireAnthropic, Upstream: "anthropic", Context: 931000, Output: 64000, Efforts: []string{"low", "medium", "high", "max"}, Images: true, Rate: 1.2},
	{ID: "claude-haiku-5-5", Name: "Haiku 5.5", Wire: WireAnthropic, Upstream: "anthropic", Context: 872000, Output: 128000, Efforts: e5, Images: true, Rate: 0.04},
	{ID: "claude-haiku-4-5-20251001", Name: "Haiku 4.5", Wire: WireAnthropic, Upstream: "anthropic", Context: 0, Output: 0, Efforts: []string{"low", "medium", "high"}, Images: true, Rate: 0.4},
	{ID: "gpt-6.1-sol", Name: "GPT-6.1 Sol", Wire: WireResponses, Upstream: "openai", Context: 1050000, Output: 128000, Efforts: e5, Images: true, Rate: 0.8},
	{ID: "gpt-6-sol", Name: "GPT-6 Sol", Wire: WireResponses, Upstream: "openai", Context: 1050000, Output: 128000, Efforts: e6, Images: true, Rate: 0.8},
	{ID: "gpt-6-astra", Name: "GPT-6 Astra", Wire: WireResponses, Upstream: "openai", Context: 1050000, Output: 128000, Efforts: e5, Images: true, Rate: 4},
	{ID: "gpt-6-luna", Name: "GPT-6 Luna", Wire: WireResponses, Upstream: "openai", Context: 1050000, Output: 128000, Efforts: e6, Images: true, Rate: 0.04},
	{ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol", Wire: WireResponses, Upstream: "openai", Context: 1050000, Output: 128000, Efforts: e6, Images: true, Rate: 2},
	{ID: "gpt-5.6-terra", Name: "GPT-5.6 Terra", Wire: WireResponses, Upstream: "openai", Context: 1050000, Output: 128000, Efforts: e6, Images: true, Rate: 0.8},
	{ID: "gpt-5.6-luna", Name: "GPT-5.6 Luna", Wire: WireResponses, Upstream: "openai", Context: 1050000, Output: 128000, Efforts: e6, Images: true, Rate: 0.08},
	{ID: "gpt-5.5", Name: "GPT-5.5", Wire: WireResponses, Upstream: "openai", Context: 1050000, Output: 128000, Efforts: e4, Images: true, Rate: 2},
	{ID: "gpt-5.4", Name: "GPT-5.4", Wire: WireResponses, Upstream: "openai", Context: 1050000, Output: 128000, Efforts: e4, Images: true, Rate: 1},
	{ID: "gpt-5.3-codex", Name: "GPT-5.3-Codex", Wire: WireResponses, Upstream: "openai", Context: 400000, Output: 128000, Efforts: e4, Images: true, Rate: 0.7},
	{ID: "grok-4.7", Name: "Grok 4.7", Wire: WireResponses, Upstream: "xai", Context: 500000, Output: 63356, Efforts: e4, Images: true, Rate: 0.8},
	{ID: "grok-4.6", Name: "Grok 4.6", Wire: WireResponses, Upstream: "xai", Context: 200000, Output: 63356, Efforts: e4, Images: true, Rate: 0.8},
	{ID: "glm-5.3", Name: "GLM-5.3", Wire: WireChat, Upstream: "fireworks", Context: 1040000, Output: 131072, Efforts: []string{"low", "high", "max"}, Images: false, Rate: 0.56},
	{ID: "glm-5.3-flash", Name: "GLM-5.3-Flash", Wire: WireChat, Upstream: "fireworks", Context: 1048576, Output: 131072, Efforts: []string{"low", "high", "max"}, Images: true, Rate: 0.06},
	{ID: "glm-5.2", Name: "GLM-5.2", Wire: WireChat, Upstream: "baseten", Context: 1040000, Output: 131072, Efforts: []string{"none", "high", "max"}, Images: false, Rate: 0.56},
	{ID: "kimi-k3", Name: "Kimi K3", Wire: WireChat, Upstream: "fireworks", Context: 262144, Output: 65536, Efforts: []string{"none", "low", "high", "max"}, Images: true, Rate: 1.2},
	{ID: "deepseek-v4.1-flash", Name: "DeepSeek V4.1 Flash", Wire: WireChat, Upstream: "fireworks", Context: 1040000, Output: 131072, Efforts: []string{"none", "low", "high", "max"}, Images: true, Rate: 0.12},
	{ID: "deepseek-v4.1-flash-1017", Name: "DeepSeek V4.1 Flash 1017", Wire: WireChat, Upstream: "fireworks", Context: 1040000, Output: 131072, Efforts: []string{"none", "low", "high", "max"}, Images: true, Rate: 0.12},
	{ID: "qwen3.8-max", Name: "Qwen3.8 Max", Wire: WireChat, Upstream: "fireworks", Context: 262144, Output: 131072, Efforts: []string{"low", "medium", "xhigh"}, Images: false, Rate: 0.8},
	{ID: "minimax-m3", Name: "MiniMax M3", Wire: WireChat, Upstream: "fireworks", Context: 512000, Output: 64000, Efforts: []string{"high"}, Images: true, Rate: 0.12},
	{ID: "minimax-m2.7", Name: "MiniMax M2.7", Wire: WireAnthropic, Upstream: "fireworks", Context: 196600, Output: 64000, Efforts: []string{"high"}, Images: false, Rate: 0.12},
	{ID: "mistral-large-4", Name: "Mistral Large 4", Wire: WireChat, Upstream: "mistral", Context: 524288, Output: 64000, Efforts: []string{"high"}, Images: true, Rate: 0.544},
	{ID: "mistral-medium-3.5", Name: "Mistral Medium 3.5", Wire: WireChat, Upstream: "mistral", Context: 256000, Output: 64000, Efforts: []string{"high"}, Images: true, Rate: 0.6},
	{ID: "nemotron-3-ultra", Name: "Nemotron 3 Ultra", Wire: WireChat, Upstream: "baseten", Context: 202000, Output: 65536, Efforts: []string{"high"}, Images: false, Rate: 0.24},
	{ID: "gemini-4-argon", Name: "Gemini 4 Argon", Wire: WireGemini, Upstream: "google", Context: 1000000, Output: 65536, Efforts: []string{"low", "medium", "high"}, Images: true, Rate: 1.6},
	{ID: "gemini-3.1-pro-preview", Name: "Gemini 3.1 Pro", Wire: WireGemini, Upstream: "google", Context: 1000000, Output: 65536, Efforts: []string{"low", "medium", "high"}, Images: true, Rate: 0.8},
	{ID: "gemini-3.8-flash", Name: "Gemini 3.8 Flash", Wire: WireGemini, Upstream: "google", Context: 1000000, Output: 65536, Efforts: []string{"low", "medium", "high"}, Images: true, Rate: 0.6},
	{ID: "gemini-3.7-flash", Name: "Gemini 3.7 Flash", Wire: WireGemini, Upstream: "google", Context: 1000000, Output: 65536, Efforts: []string{"low", "medium", "high"}, Images: true, Rate: 0.6},
	{ID: "gemini-3.6-flash", Name: "Gemini 3.6 Flash", Wire: WireGemini, Upstream: "google", Context: 1000000, Output: 65536, Efforts: []string{"low", "medium", "high"}, Images: true, Rate: 0.6},
	{ID: "gemini-3.5-flash", Name: "Gemini 3.5 Flash", Wire: WireGemini, Upstream: "google", Context: 1000000, Output: 65536, Efforts: []string{"minimal", "low", "medium", "high"}, Images: true, Rate: 0.6},
	{ID: "gemini-3-flash-preview", Name: "Gemini 3 Flash", Wire: WireGemini, Upstream: "google", Context: 1000000, Output: 65536, Efforts: []string{"minimal", "low", "medium", "high"}, Images: true, Rate: 0.2},
	{ID: "claude-opus-5-5-fast", Name: "Opus 5.5 Fast Mode", Wire: WireAnthropic, Upstream: "anthropic", Context: 872000, Output: 128000, Efforts: e5, Images: true, Rate: 3.2},
	{ID: "claude-opus-5-fast", Name: "Opus 5 Fast Mode", Wire: WireAnthropic, Upstream: "anthropic", Context: 867000, Output: 128000, Efforts: e5, Images: true, Rate: 4},
	{ID: "claude-opus-4-8-fast", Name: "Opus 4.8 Fast Mode", Wire: WireAnthropic, Upstream: "anthropic", Context: 867000, Output: 128000, Efforts: e5, Images: true, Rate: 4},
	{ID: "gpt-5.5-fast", Name: "GPT-5.5 Fast Mode", Wire: WireResponses, Upstream: "openai", Context: 1050000, Output: 128000, Efforts: e4, Images: true, Rate: 5},
	{ID: "gpt-5.4-fast", Name: "GPT-5.4 Fast Mode", Wire: WireResponses, Upstream: "openai", Context: 1050000, Output: 128000, Efforts: e4, Images: true, Rate: 2},
	{ID: "gpt-5.3-codex-fast", Name: "GPT-5.3-Codex Fast Mode", Wire: WireResponses, Upstream: "openai", Context: 400000, Output: 128000, Efforts: e4, Images: true, Rate: 1.4},
}

var byID = func() map[string]Model {
	m := make(map[string]Model, len(models))
	for _, x := range models {
		m[x.ID] = x
	}
	return m
}()

// Models are the models droid's picker offers, in its order. The caller must not change the slice.
func Models() []Model { return models }

// ModelByID is the model with id, if droid's picker offers it.
func ModelByID(id string) (Model, bool) {
	m, ok := byID[id]
	return m, ok
}
