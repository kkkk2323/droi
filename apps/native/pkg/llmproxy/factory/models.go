// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: ANTHROPIC/RESPONSES/CHAT/GEMINI 33-39, MODELS 59-116, byId 118.

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

// Model is one model droid's picker offers: the wire droid sends it on and the vendor it names in x-api-provider.
// Display names, limits, efforts and prices come with the model picker (4F).
type Model struct {
	ID       string
	Wire     Wire
	Upstream string
}

// models are droid's picker, less auto, in upstream's order. Left out as upstream leaves them out: GPT-6 Astra Ultra
// Fast (offered only after Factory's high-cost terms are accepted), Opus 4.6/4.7 Fast and GPT-5.4 Mini Fast (their
// base models are not offered), GPT-5.6 Sol Fast (unreleased) and GLM-5.2 Fast (behind a feature flag).
var models = []Model{
	{"claude-fable-5.1", WireAnthropic, "anthropic"},
	{"claude-fable-5", WireAnthropic, "anthropic"},
	{"claude-opus-5-5", WireAnthropic, "anthropic"},
	{"claude-opus-5", WireAnthropic, "anthropic"},
	{"claude-opus-4-8", WireAnthropic, "anthropic"},
	{"claude-sonnet-5-5", WireAnthropic, "anthropic"},
	{"claude-sonnet-5", WireAnthropic, "anthropic"},
	{"claude-sonnet-4-6", WireAnthropic, "anthropic"},
	{"claude-haiku-5-5", WireAnthropic, "anthropic"},
	{"claude-haiku-4-5-20251001", WireAnthropic, "anthropic"},
	{"gpt-6.1-sol", WireResponses, "openai"},
	{"gpt-6-sol", WireResponses, "openai"},
	{"gpt-6-astra", WireResponses, "openai"},
	{"gpt-6-luna", WireResponses, "openai"},
	{"gpt-5.6-sol", WireResponses, "openai"},
	{"gpt-5.6-terra", WireResponses, "openai"},
	{"gpt-5.6-luna", WireResponses, "openai"},
	{"gpt-5.5", WireResponses, "openai"},
	{"gpt-5.4", WireResponses, "openai"},
	{"gpt-5.3-codex", WireResponses, "openai"},
	{"grok-4.7", WireResponses, "xai"},
	{"grok-4.6", WireResponses, "xai"},
	{"glm-5.3", WireChat, "fireworks"},
	{"glm-5.3-flash", WireChat, "fireworks"},
	{"glm-5.2", WireChat, "baseten"},
	{"kimi-k3", WireChat, "fireworks"},
	{"deepseek-v4.1-flash", WireChat, "fireworks"},
	{"deepseek-v4.1-flash-1017", WireChat, "fireworks"},
	{"qwen3.8-max", WireChat, "fireworks"},
	{"minimax-m3", WireChat, "fireworks"},
	{"minimax-m2.7", WireAnthropic, "fireworks"},
	{"mistral-large-4", WireChat, "mistral"},
	{"mistral-medium-3.5", WireChat, "mistral"},
	{"nemotron-3-ultra", WireChat, "baseten"},
	{"gemini-4-argon", WireGemini, "google"},
	{"gemini-3.1-pro-preview", WireGemini, "google"},
	{"gemini-3.8-flash", WireGemini, "google"},
	{"gemini-3.7-flash", WireGemini, "google"},
	{"gemini-3.6-flash", WireGemini, "google"},
	{"gemini-3.5-flash", WireGemini, "google"},
	{"gemini-3-flash-preview", WireGemini, "google"},
	{"claude-opus-5-5-fast", WireAnthropic, "anthropic"},
	{"claude-opus-5-fast", WireAnthropic, "anthropic"},
	{"claude-opus-4-8-fast", WireAnthropic, "anthropic"},
	{"gpt-5.5-fast", WireResponses, "openai"},
	{"gpt-5.4-fast", WireResponses, "openai"},
	{"gpt-5.3-codex-fast", WireResponses, "openai"},
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
