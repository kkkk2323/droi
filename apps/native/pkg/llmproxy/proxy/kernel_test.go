package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/factory"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

const kernelToken = "droi-kernel-test-token"

// fakeFactory is Factory as the Kernel path meets it: whoami for each key, and a model route that answers what reply
// says for the key a request carries.
type fakeFactory struct {
	mu       sync.Mutex
	seen     []*http.Request
	whoami   string
	reply    func(key string) (int, string)
	upstream *httptest.Server
}

func newFakeFactory(t *testing.T) *fakeFactory {
	f := &fakeFactory{
		whoami: `{"orgId":"org_1"}`,
		reply:  func(string) (int, string) { return 200, `{"usage":{"input_tokens":1,"output_tokens":1}}` },
	}
	f.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/whoami" {
			io.WriteString(w, f.whoami)
			return
		}
		r2 := r.Clone(r.Context())
		sent, _ := io.ReadAll(r.Body)
		r2.Body = io.NopCloser(bytes.NewReader(sent))
		f.mu.Lock()
		f.seen = append(f.seen, r2)
		f.mu.Unlock()
		status, body := f.reply(keys.NormalizeBearerToken(r.Header.Get("Authorization")))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(f.upstream.Close)
	return f
}

func (f *fakeFactory) requests() []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*http.Request(nil), f.seen...)
}

// startKernelProxy is a proxy with the Key Pool of usages, whose whoami is f's, that lets the Kernel's token in.
func startKernelProxy(t *testing.T, upstream string, f *fakeFactory, sw *switches, usages ...*keys.KeyUsage) *Proxy {
	t.Helper()
	cfg := config.New()
	cfg.UsageAPIURL = f.upstream.URL + "/api/billing/limits"
	var names []string
	for _, u := range usages {
		names = append(names, u.Key)
	}
	rotator := keys.NewKeyRotator(cfg, names)
	for _, u := range usages {
		rotator.SetUsageForTest(u)
	}
	px := NewProxy(upstream, rotator, sw.switchKey)
	px.SetKernelToken(kernelToken)
	if _, err := px.Start("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = px.Shutdown(t.Context()) })
	return px
}

func postKernel(t *testing.T, base, path, body, conversation string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", base+path, strings.NewReader(body))
	// As the Kernel's clients send their key: Anthropic's in X-Api-Key, the others' as a bearer token.
	if strings.Contains(path, "/llm/a/") {
		req.Header.Set("X-Api-Key", kernelToken)
	} else {
		req.Header.Set("Authorization", "Bearer "+kernelToken)
	}
	req.Header.Set("Content-Type", "application/json")
	if conversation != "" {
		req.Header.Set(conversationHeader, conversation)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// The Kernel holds no key: its model request leaves the proxy with a key of the Key Pool, the headers droid sends for
// that model, the key's organization, and a session id of its conversation. Its own token, and Droi's own headers,
// never leave.
func TestAKernelRequestGoesOutAsDroidsWithAPoolKey(t *testing.T) {
	f := newFakeFactory(t)
	sw := &switches{ok: true}
	px := startKernelProxy(t, f.upstream.URL, f, sw, poolUsage("fk-a", 0, 0))

	chat := `{"model":"deepseek-v4.1-flash-1017","messages":[{"role":"user","content":"hi"}]}`
	if status, body := postKernel(t, px.BaseURL(), "/api/llm/o/v1/chat/completions", chat, "7"); status != 200 {
		t.Fatalf("chat: %d %s", status, body)
	}
	postKernel(t, px.BaseURL(), "/api/llm/o/v1/chat/completions", chat, "7")
	postKernel(t, px.BaseURL(), "/api/llm/o/v1/chat/completions", chat, "8")
	messages := `{"model":"claude-haiku-5-5","system":"You are Droid, an AI software engineering agent built by Factory.","messages":[]}`
	if status, body := postKernel(t, px.BaseURL(), "/api/llm/a/v1/messages", messages, "7"); status != 200 {
		t.Fatalf("messages: %d %s", status, body)
	}

	seen := f.requests()
	if len(seen) != 4 {
		t.Fatalf("Factory saw %d requests, want 4", len(seen))
	}
	for i, r := range seen {
		h := r.Header
		if got := h.Get("Authorization"); got != "Bearer fk-a" {
			t.Fatalf("request %d went out with %q, not the pool's key", i, got)
		}
		for name, vals := range h {
			if strings.HasPrefix(strings.ToLower(name), "x-droi-") || strings.Contains(strings.Join(vals, " "), kernelToken) {
				t.Fatalf("request %d carried %s: %v", i, name, vals)
			}
		}
		for name, want := range map[string]string{
			"X-Factory-Client": "cli", "X-Client-Version": factory.ClientVersion, "User-Agent": "factory-cli/" + factory.ClientVersion,
			"X-Factory-Org-Id": "org_1", "X-Provider-Routing-Source": "registry_default",
		} {
			if got := h.Get(name); got != want {
				t.Fatalf("request %d: %s = %q, want %q", i, name, got, want)
			}
		}
		if h.Get("X-Assistant-Message-Id") == "" || h.Get("X-Session-Id") == "" {
			t.Fatalf("request %d has no message or session id: %v", i, h)
		}
	}
	chatHeader, anthropicHeader := seen[0].Header, seen[3].Header
	if chatHeader.Get("X-Api-Provider") != "fireworks" || chatHeader.Get("X-Api-Key") != "" || chatHeader.Get("OpenAI-Platform") != "" {
		t.Fatalf("a Fireworks model's headers: %v", chatHeader)
	}
	if anthropicHeader.Get("X-Api-Provider") != "anthropic" || anthropicHeader.Get("X-Api-Key") != "placeholder" {
		t.Fatalf("an Anthropic model's headers: %v", anthropicHeader)
	}
	if seen[0].Header.Get("X-Session-Id") != seen[1].Header.Get("X-Session-Id") {
		t.Fatal("one conversation's requests have two session ids")
	}
	if seen[0].Header.Get("X-Session-Id") == seen[2].Header.Get("X-Session-Id") {
		t.Fatal("two conversations share a session id")
	}
	if seen[0].Header.Get("X-Assistant-Message-Id") == seen[1].Header.Get("X-Assistant-Message-Id") {
		t.Fatal("two requests share a message id")
	}
	if got := sw.got(); len(got) != 0 {
		t.Fatalf("droid's key file was written for a Kernel request: %v", got)
	}
}

// The Kernel's token is for model requests only: anything else it is sent with is refused here, and never reaches
// Factory.
func TestTheKernelTokenIsOnlyForModelRequests(t *testing.T) {
	f := newFakeFactory(t)
	px := startKernelProxy(t, f.upstream.URL, f, &switches{ok: true}, poolUsage("fk-a", 0, 0))
	req, _ := http.NewRequest("GET", px.BaseURL()+"/api/organization/members", nil)
	req.Header.Set("Authorization", "Bearer "+kernelToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
	if n := len(f.requests()); n != 0 {
		t.Fatalf("Factory saw %d requests", n)
	}
}

// Without the Kernel's token set, a request carrying it is one with no key of the pool, and is stopped as before.
func TestAnUnknownTokenIsStillStopped(t *testing.T) {
	f := newFakeFactory(t)
	px := startKernelProxy(t, f.upstream.URL, f, &switches{ok: true}, poolUsage("fk-a", 0, 0))
	px.SetKernelToken("")
	if status, _ := postKernel(t, px.BaseURL(), "/api/llm/o/v1/chat/completions", `{"model":"glm-5.3"}`, ""); status != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", status)
	}
	if n := len(f.requests()); n != 0 {
		t.Fatalf("Factory saw %d requests", n)
	}
}

// A key out of quota is left for the next one inside the proxy: the Kernel sees one answer, the one that worked.
func TestAKernelRequestMovesToAnotherKeyWhenOneIsSpent(t *testing.T) {
	f := newFakeFactory(t)
	spent := ""
	f.reply = func(key string) (int, string) {
		if spent == "" || spent == key {
			spent = key
			return http.StatusPaymentRequired, `{"error":{"message":"out of tokens"}}`
		}
		return 200, `{"usage":{"input_tokens":1,"output_tokens":1}}`
	}
	sw := &switches{ok: true}
	px := startKernelProxy(t, f.upstream.URL, f, sw, poolUsage("fk-a", 0, 0), poolUsage("fk-b", 10, 0))
	status, body := postKernel(t, px.BaseURL(), "/api/llm/o/v1/chat/completions", `{"model":"glm-5.3","messages":[]}`, "1")
	if status != 200 {
		t.Fatalf("status %d %s", status, body)
	}
	var used []string
	for _, r := range f.requests() {
		used = append(used, keys.NormalizeBearerToken(r.Header.Get("Authorization")))
	}
	if len(used) != 2 || used[0] != spent || used[1] == spent {
		t.Fatalf("keys tried %v, want the spent key %s and then another", used, spent)
	}
	if got := sw.got(); len(got) != 0 {
		t.Fatalf("droid's key file was written: %v", got)
	}
}

// Factory's usage policy refusing what a conversation says is no fault of the key: the refusal goes back as it came,
// once, and the next request still goes out with the same key.
func TestAContentRefusalIsNotBlamedOnTheKey(t *testing.T) {
	f := newFakeFactory(t)
	refused := `{"error":{"code":"content_policy_rejected","message":"refused"}}`
	f.reply = func(string) (int, string) { return http.StatusForbidden, refused }
	px := startKernelProxy(t, f.upstream.URL, f, &switches{ok: true}, poolUsage("fk-a", 0, 0), poolUsage("fk-b", 10, 0))
	status, body := postKernel(t, px.BaseURL(), "/api/llm/o/v1/chat/completions", `{"model":"glm-5.3","messages":[]}`, "1")
	if status != http.StatusForbidden || body != refused {
		t.Fatalf("got %d %s, want Factory's refusal as it came", status, body)
	}
	f.reply = func(string) (int, string) { return 200, `{}` }
	postKernel(t, px.BaseURL(), "/api/llm/o/v1/chat/completions", `{"model":"glm-5.3","messages":[]}`, "1")
	seen := f.requests()
	if len(seen) != 2 {
		t.Fatalf("Factory saw %d requests, want 2: a refusal is not sent again", len(seen))
	}
	if a, b := seen[0].Header.Get("Authorization"), seen[1].Header.Get("Authorization"); a != b {
		t.Fatalf("the key changed after a content refusal: %s then %s", a, b)
	}
}

// A Kernel Gemini request in Google's shape goes to Factory's own route: the model in the body, droid's line in
// the system instruction, the query (with the token Google's clients put there) left behind, and Google's headers.
func TestAKernelGeminiRequestGoesToFactorysRoute(t *testing.T) {
	f := newFakeFactory(t)
	px := startKernelProxy(t, f.upstream.URL, f, &switches{ok: true}, poolUsage("fk-a", 0, 0))

	body := `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"systemInstruction":{"role":"user","parts":[{"text":"Be brief."}]}}`
	status, out := postKernel(t, px.BaseURL(), "/api/llm/g/v1/models/gemini-4-argon:streamGenerateContent?alt=sse&key="+kernelToken, body, "9")
	if status != 200 {
		t.Fatalf("%d %s", status, out)
	}
	seen := f.requests()
	if len(seen) != 1 {
		t.Fatalf("Factory saw %d requests", len(seen))
	}
	r := seen[0]
	if r.URL.Path != "/api/llm/g/v1/generate" || r.URL.RawQuery != "" {
		t.Fatalf("it went to %s?%s", r.URL.Path, r.URL.RawQuery)
	}
	var got struct {
		Model             string `json:"model"`
		SystemInstruction struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"systemInstruction"`
	}
	raw, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "gemini-4-argon" {
		t.Fatalf("the model in the body is %q", got.Model)
	}
	if got.SystemInstruction.Role != "" || len(got.SystemInstruction.Parts) != 1 ||
		got.SystemInstruction.Parts[0].Text != factory.DroidLine+"\nBe brief." {
		t.Fatalf("the system instruction is %+v", got.SystemInstruction)
	}
	h := r.Header
	if h.Get("X-Api-Provider") != "google" || h.Get("X-Goog-Api-Key") != "" || h.Get("X-Api-Key") != "" {
		t.Fatalf("a Gemini request's headers: %v", h)
	}
}

// A key whose organization lives in Factory's EU region, or on a host of its own, sends its model requests there.
func TestAKernelRequestGoesWhereItsOrganizationIsServed(t *testing.T) {
	for _, tc := range []struct{ whoami, host string }{
		{`{"orgId":"org_eu","region":"eu"}`, "api.eu.factory.ai"},
		{`{"orgId":"org_p","premBaseHostV2":"prem.acme.example"}`, "prem.acme.example"},
		{`{"orgId":"org_us"}`, "api.factory.ai"},
	} {
		f := newFakeFactory(t)
		f.whoami = tc.whoami
		var mu sync.Mutex
		var hosts []string
		px := startKernelProxy(t, factory.API, f, &switches{ok: true}, poolUsage("fk-a", 0, 0))
		px.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
			mu.Lock()
			hosts = append(hosts, r.URL.Host)
			mu.Unlock()
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
		}))
		postKernel(t, px.BaseURL(), "/api/llm/o/v1/chat/completions", `{"model":"glm-5.3","messages":[]}`, "1")
		mu.Lock()
		got := strings.Join(hosts, ",")
		mu.Unlock()
		if got != tc.host {
			t.Fatalf("whoami %s: the request went to %q, want %q", tc.whoami, got, tc.host)
		}
	}
}

// Every key the proxy tries turned away: the Kernel gets the Key Pool's one sentence, not the last key's refusal, and
// no more than kernelAttempts keys are tried.
func TestAKernelRequestNoKeyCanServeGetsOneAnswer(t *testing.T) {
	for _, n := range []int{2, 4} {
		f := newFakeFactory(t)
		f.reply = func(string) (int, string) { return http.StatusPaymentRequired, `{"error":{"message":"out of tokens"}}` }
		var usages []*keys.KeyUsage
		for i := range n {
			usages = append(usages, poolUsage("fk-"+string(rune('a'+i)), float64(i), 0))
		}
		px := startKernelProxy(t, f.upstream.URL, f, &switches{ok: true}, usages...)
		status, body := postKernel(t, px.BaseURL(), "/api/llm/o/v1/chat/completions", `{"model":"glm-5.3","messages":[]}`, "1")
		if status != http.StatusServiceUnavailable || !strings.Contains(body, "No key of Droi's Key Pool can serve glm-5.3") {
			t.Fatalf("%d keys: got %d %s", n, status, body)
		}
		if got, want := len(f.requests()), min(n, kernelAttempts); got != want {
			t.Fatalf("%d keys: Factory saw %d requests, want %d", n, got, want)
		}
	}
}

// A key in the query is the Kernel's token, and stays here; the rest of the query goes on.
func TestAKernelRequestsQueryKeepsNoKey(t *testing.T) {
	f := newFakeFactory(t)
	px := startKernelProxy(t, f.upstream.URL, f, &switches{ok: true}, poolUsage("fk-a", 0, 0))
	postKernel(t, px.BaseURL(), "/api/llm/o/v1/chat/completions?alt=sse&key="+kernelToken, `{"model":"glm-5.3","messages":[]}`, "1")
	seen := f.requests()
	if len(seen) != 1 || seen[0].URL.RawQuery != "alt=sse" {
		t.Fatalf("Factory saw %v", seen)
	}
}

// The cache warmer refreshes Daemon Sessions only: a Kernel conversation is not kept, so it never takes a Session's
// place.
func TestTheCacheWarmerKeepsNoKernelConversation(t *testing.T) {
	f := newFakeFactory(t)
	f.reply = func(string) (int, string) {
		return 200, `{"type":"message","content":[],"usage":{"input_tokens":10,"cache_read_input_tokens":99000,"cache_creation_input_tokens":990,"output_tokens":5}}`
	}
	px := startKernelProxy(t, f.upstream.URL, f, &switches{ok: true}, poolUsage("fk-a", 0, 0))
	w, _ := newTestWarmer(CacheWarmingIdle, working())
	px.SetCacheWarmer(w)

	postKernel(t, px.BaseURL(), "/api/llm/a/v1/messages", agentRequestJSON, "1")
	if n := len(w.Status().Sessions); n != 0 {
		t.Fatalf("the warmer kept %d Kernel conversations", n)
	}
	req, _ := http.NewRequest(http.MethodPost, px.BaseURL()+"/api/llm/a/v1/messages", strings.NewReader(agentRequestJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer fk-a")
	req.Header.Set(sessionIDHeader, "s1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if n := len(w.Status().Sessions); n != 1 {
		t.Fatalf("the warmer kept %d Sessions, want droid's one", n)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A Kernel conversation on a Fast Mode model is served fast: speed "fast" in the body, the fast-mode beta beside it.
func TestAKernelFastModeRequestIsServedFast(t *testing.T) {
	f := newFakeFactory(t)
	px := startKernelProxy(t, f.upstream.URL, f, &switches{ok: true}, poolUsage("fk-a", 0, 0))

	body := `{"model":"claude-opus-5-5-fast","max_tokens":8,"stream":true,"system":[{"type":"text","text":"Be brief."}],"messages":[{"role":"user","content":"hi"}]}`
	status, out := postKernel(t, px.BaseURL(), "/api/llm/a/v1/messages", body, "9")
	if status != 200 {
		t.Fatalf("%d %s", status, out)
	}
	seen := f.requests()
	if len(seen) != 1 {
		t.Fatalf("Factory saw %d requests", len(seen))
	}
	var got struct {
		Model string `json:"model"`
		Speed string `json:"speed"`
	}
	raw, _ := io.ReadAll(seen[0].Body)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "claude-opus-5-5-fast" || got.Speed != "fast" {
		t.Fatalf("sent %s", raw)
	}
	if h := seen[0].Header; h.Get("Anthropic-Beta") != factory.FastModeBeta || h.Get("X-Api-Provider") != "anthropic" {
		t.Fatalf("headers %v", h)
	}
}
