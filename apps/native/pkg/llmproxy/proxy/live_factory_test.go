package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

// droidOpening is the sentence droid puts first in a system prompt. Factory
// refuses a request that says it is something else, so the pi-shaped request
// below carries it until the Go rewrite layer (M4) adds it (design §5).
const droidOpening = "You are Droid, an AI software engineering agent built by Factory."

// defaultLiveModel is the model this test asks for when DROI_LIVE_MODEL says
// nothing: the cheapest one that answered 200 on the Anthropic route with the
// Key Pool's key on 2026-11-07. The chat route's models need x-api-provider
// and a route of their own (DROI_LIVE_ROUTE, DROI_LIVE_API_PROVIDER).
const defaultLiveModel = "claude-haiku-5-5"

// TestLiveFactoryThroughTheProxy sends one real request through the LLM proxy
// with a Key Pool key: the cheapest model, one word, no tools. It answers
// whether the proxy's forwarding and authentication still hold against
// Factory, and what the request shape needs before M4's rewrite layer exists.
//
// Only with DROI_LIVE_FACTORY_KEY set (a pool key, never logged); DROI_LIVE_MODEL
// picks the model, DROI_LIVE_FACTORY_BASE the upstream.
func TestLiveFactoryThroughTheProxy(t *testing.T) {
	key := os.Getenv("DROI_LIVE_FACTORY_KEY")
	if key == "" {
		t.Skip("DROI_LIVE_FACTORY_KEY is not set")
	}
	model := os.Getenv("DROI_LIVE_MODEL")
	if model == "" {
		model = defaultLiveModel
	}
	upstream := os.Getenv("DROI_LIVE_FACTORY_BASE")
	if upstream == "" {
		upstream = "https://api.factory.ai"
	}
	cfg := config.New()
	cfg.UpstreamBaseURL = upstream
	rotator := keys.NewKeyRotator(cfg, []string{key})
	px := NewProxy(upstream, rotator, nil)
	base, err := px.Start("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = px.Shutdown(context.Background()) }()

	// Two routes: Anthropic's Messages (/api/llm/a) and OpenAI's chat
	// completions (/api/llm/o). A model belongs to one of them — GLM and
	// DeepSeek answer only on chat completions — which is why the plugin's
	// model table names a wire per model.
	route := os.Getenv("DROI_LIVE_ROUTE")
	if route == "" {
		route = "a"
	}
	path := "/api/llm/a/v1/messages"
	body := map[string]any{
		"model":      model,
		"max_tokens": 16,
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	if route == "o" {
		path = "/api/llm/o/v1/chat/completions"
		delete(body, "max_tokens")
		body["max_completion_tokens"] = 16
	}
	// DROI_LIVE_NO_OPENING leaves the droid sentence out, to see whether
	// Factory refuses a prompt that does not say it is droid.
	if os.Getenv("DROI_LIVE_NO_OPENING") == "" {
		if route == "o" {
			body["messages"] = append([]any{map[string]any{"role": "system", "content": droidOpening}}, body["messages"].([]any)...)
		} else {
			body["system"] = droidOpening
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	// The headers droid sends, which the rewrite layer will add (design §5.1
	// "factoryHeaders"): Factory refuses a request that names no client
	// version, and routes it by the organization when one is given.
	req.Header.Set("X-Factory-Client", "cli")
	version := os.Getenv("DROI_LIVE_CLIENT_VERSION")
	if version == "" {
		version = "0.240.0"
	}
	req.Header.Set("X-Client-Version", version)
	if org := os.Getenv("DROI_LIVE_ORG_ID"); org != "" {
		req.Header.Set("X-Factory-Org-Id", org)
	}
	// The routing headers magpie's plugin sends: x-api-provider names the
	// upstream the model runs on (fireworks for GLM, anthropic for Claude) and
	// x-provider-routing-source says the choice came from the model registry.
	// Without x-api-provider Factory answers "Provider not available in this
	// region" for a model it cannot route by its id alone.
	if source := os.Getenv("DROI_LIVE_ROUTING_SOURCE"); source != "" {
		req.Header.Set("X-Provider-Routing-Source", source)
	}
	if provider := os.Getenv("DROI_LIVE_API_PROVIDER"); provider != "" {
		req.Header.Set("X-Api-Provider", provider)
	}
	if route == "a" {
		// droid's Anthropic client sends the SDK's placeholder beside the
		// bearer token; the plugin keeps that.
		req.Header.Set("X-Api-Key", "placeholder")
	}
	for _, header := range extraLiveHeaders() {
		req.Header.Set(header[0], header[1])
	}

	started := time.Now()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("the proxy never answered: %v", err)
	}
	defer res.Body.Close()
	elapsed := time.Since(started)
	answer, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))

	// Never print the key: the body may echo a request id, not a token.
	redacted := strings.ReplaceAll(string(answer), key, "«key»")
	if len(redacted) > 2000 {
		redacted = redacted[:2000] + "…"
	}
	t.Logf("model %s on %s: HTTP %d in %s", model, path, res.StatusCode, elapsed.Round(time.Millisecond))
	for _, name := range []string{"X-Factory-Request-Id", "X-Request-Id", "Retry-After", "Content-Type"} {
		if v := res.Header.Get(name); v != "" {
			t.Logf("header %s: %s", name, v)
		}
	}
	var parsed struct {
		Usage map[string]any `json:"usage"`
		Error any            `json:"error"`
	}
	if json.Unmarshal(answer, &parsed) == nil && len(parsed.Usage) > 0 {
		t.Logf("usage: %v", parsed.Usage)
	}
	if res.StatusCode != http.StatusOK {
		t.Logf("body: %s", redacted)
		t.Errorf("Factory answered %d", res.StatusCode)
	}
}

// extraLiveHeaders reads DROI_LIVE_HEADERS, one "Name: value" per line, for
// probing what Factory's routes want without a code change.
func extraLiveHeaders() [][2]string {
	var out [][2]string
	for _, line := range strings.Split(os.Getenv("DROI_LIVE_HEADERS"), "\n") {
		name, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		out = append(out, [2]string{strings.TrimSpace(name), strings.TrimSpace(value)})
	}
	return out
}
