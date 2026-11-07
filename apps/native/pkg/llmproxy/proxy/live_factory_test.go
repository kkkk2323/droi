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
		model = "glm-5.3-flash"
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

	body := map[string]any{
		"model":      model,
		"max_tokens": 16,
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	// DROI_LIVE_NO_OPENING leaves the droid sentence out, to see whether
	// Factory refuses a prompt that does not say it is droid.
	if os.Getenv("DROI_LIVE_NO_OPENING") == "" {
		body["system"] = droidOpening
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, base+"/api/llm/a/v1/messages", strings.NewReader(string(raw)))
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
	t.Logf("model %s: HTTP %d in %s", model, res.StatusCode, elapsed.Round(time.Millisecond))
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
