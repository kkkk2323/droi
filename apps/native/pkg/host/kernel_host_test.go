package host

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// The Kernel's providers are Factory's models on the proxy, on the wire droid sends each, with the Host's token for a
// key: never a key of the pool.
func TestTheKernelsProvidersAreFactorysModelsOnTheProxy(t *testing.T) {
	var providers []kernelProvider
	if err := json.Unmarshal([]byte(kernelProviders("http://127.0.0.1:4000/")), &providers); err != nil {
		t.Fatal(err)
	}
	byID := map[string]kernelProvider{}
	for _, p := range providers {
		byID[p.ID] = p
		if p.APIKeyEnv != kernelLLMTokenEnv {
			t.Fatalf("%s reads its key from %q", p.ID, p.APIKeyEnv)
		}
	}
	for id, want := range map[string][2]string{
		"factory-anthropic": {"anthropic-messages", "http://127.0.0.1:4000/api/llm/a"},
		"factory-openai":    {"openai-responses", "http://127.0.0.1:4000/api/llm/o/v1"},
		"factory-chat":      {"openai-completions", "http://127.0.0.1:4000/api/llm/o/v1"},
		"factory-google":    {"google-generative-ai", "http://127.0.0.1:4000/api/llm/g/v1"},
	} {
		p, ok := byID[id]
		if !ok || p.API != want[0] || p.BaseURL != want[1] || len(p.Models) == 0 {
			t.Fatalf("%s: %+v", id, p)
		}
	}
	if len(providers) != 4 {
		t.Fatalf("%d providers, want one per wire", len(providers))
	}
	find := func(provider, id string) (kernelModel, bool) {
		for _, m := range byID[provider].Models {
			if m.ID == id {
				return m, true
			}
		}
		return kernelModel{}, false
	}
	if _, ok := find("factory-anthropic", "claude-haiku-4-5-20251001"); ok {
		t.Fatal("a model with no limits is offered")
	}
	if m, ok := find("factory-anthropic", "minimax-m2.7"); !ok || m.ContextWindow != 196600 {
		t.Fatalf("MiniMax M2.7 goes on Anthropic's wire: %+v", m)
	}
	if g, ok := find("factory-google", "gemini-3.5-flash"); !ok || strings.Join(g.Thinking, ",") != "minimal,low,medium,high" || g.ContextWindow != 1000000 {
		t.Fatalf("gemini-3.5-flash: %+v", g)
	}
	m, ok := find("factory-chat", "kimi-k3")
	if !ok || !m.Reasoning || strings.Join(m.Thinking, ",") != "off,low,high,max" || strings.Join(m.Input, ",") != "text,image" {
		t.Fatalf("kimi-k3: %+v", m)
	}
}

// pi-ai sends a level the model's thinkingLevelMap leaves out as another: xhigh and max as high, and off as effort
// "none" on Responses; and Claude's thinking budgeted unless the model is marked adaptive, which Haiku 5.5 and Opus
// 4.7 and later refuse with a 400. Each model says what Factory takes of it.
func TestKernelModelsSayWhichThinkingLevelsFactoryTakes(t *testing.T) {
	var providers []kernelProvider
	if err := json.Unmarshal([]byte(kernelProviders("http://127.0.0.1:4000")), &providers); err != nil {
		t.Fatal(err)
	}
	models := map[string]kernelModel{}
	for _, p := range providers {
		for _, m := range p.Models {
			models[m.ID] = m
		}
	}
	levels := func(m kernelModel) string {
		var out []string
		for _, l := range []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"} {
			v, ok := m.ThinkingLevelMap[l]
			switch {
			case !ok:
				out = append(out, l+"=?")
			case v == nil:
				out = append(out, l+"=-")
			default:
				out = append(out, l+"="+*v)
			}
		}
		return strings.Join(out, " ")
	}
	for id, want := range map[string]string{
		"claude-haiku-5-5":  "off=- minimal=- low=low medium=medium high=high xhigh=xhigh max=max",
		"claude-sonnet-4-6": "off=- minimal=- low=low medium=medium high=high xhigh=- max=max",
		"kimi-k3":           "off=none minimal=- low=low medium=- high=high xhigh=- max=max",
		"gpt-6-luna":        "off=none minimal=- low=low medium=medium high=high xhigh=xhigh max=max",
		"gpt-5.5":           "off=- minimal=- low=low medium=medium high=high xhigh=xhigh max=-",
		"gemini-3.5-flash":  "off=- minimal=minimal low=low medium=medium high=high xhigh=- max=-",
		"minimax-m2.7":      "off=- minimal=- low=- medium=- high=high xhigh=- max=-",
	} {
		m, ok := models[id]
		if !ok {
			t.Fatalf("no %s", id)
		}
		if got := levels(m); got != want {
			t.Errorf("%s: %s, want %s", id, got, want)
		}
	}
	for id, m := range models {
		adaptive := m.Compat != nil && m.Compat.ForceAdaptiveThinking
		if want := strings.HasPrefix(id, "claude-"); adaptive != want {
			t.Errorf("%s: adaptive thinking %v, want %v", id, adaptive, want)
		}
	}
}

// The Kernel runs when a person asks for it and there is one to run: the one DROI_KERNEL_BIN names, or the one beside
// the app.
func TestTheKernelRunsOnlyWhenAskedAndPresent(t *testing.T) {
	dir := t.TempDir()
	name := "kernel"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	beside := filepath.Join(dir, name)
	if err := os.WriteFile(beside, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		what string
		env  map[string]string
		exe  string
		want bool
	}{
		{"not asked", map[string]string{KernelBinEnv: beside}, "", false},
		{"asked, named", map[string]string{KernelEnv: "1", KernelBinEnv: beside}, "", true},
		{"asked, beside the app", map[string]string{KernelEnv: "1"}, filepath.Join(dir, "droi"), true},
		{"asked, none", map[string]string{KernelEnv: "1", KernelBinEnv: filepath.Join(dir, "missing")}, filepath.Join(t.TempDir(), "droi"), false},
	} {
		h := &Host{cfg: Config{Env: func(k string) string { return tc.env[k] }, Executable: tc.exe}}
		if got := h.KernelEnabled(); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.what, got, tc.want)
		}
	}
}

// kernelBinaryForTest is the Kernel a test runs: DROI_KERNEL_BIN, or the one `pnpm build:kernel` built. A test that
// needs it skips without it, unless DROI_FAKEKERNEL_REQUIRED says it must run.
func kernelBinaryForTest(t *testing.T) string {
	t.Helper()
	bin := os.Getenv(KernelBinEnv)
	if bin == "" {
		name := "kernel"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		bin, _ = filepath.Abs(filepath.Join("..", "..", "assets", "kernel", runtime.GOOS+"-"+runtime.GOARCH, name))
	}
	if _, err := os.Stat(bin); err != nil {
		if os.Getenv("DROI_FAKEKERNEL_REQUIRED") != "" {
			t.Fatalf("no Kernel binary at %s: run pnpm build:kernel or set %s", bin, KernelBinEnv)
		}
		t.Skip("no Kernel binary: run pnpm build:kernel or set " + KernelBinEnv)
	}
	return bin
}

// fakeFactory is enough of Factory for the Key Pool and two models: billing, whoami, a streamed chat completion and a
// streamed Anthropic message.
type fakeFactory struct {
	mu   sync.Mutex
	seen []http.Header
	*httptest.Server
}

func newFakeFactory(t *testing.T) *fakeFactory {
	f := &fakeFactory{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		note := func() {
			_, _ = io.Copy(io.Discard, r.Body)
			f.mu.Lock()
			f.seen = append(f.seen, r.Header.Clone())
			f.mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/billing/limits"):
			io.WriteString(w, `{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"fiveHour":{"usedPercent":10,"windowEnd":"2099-06-19T15:00:00Z"}},"core":{"fiveHour":{"usedPercent":5,"windowEnd":"2099-06-19T15:00:00Z"}}}}`)
		case r.URL.Path == "/api/cli/whoami":
			io.WriteString(w, `{"orgId":"org_test"}`)
		case r.URL.Path == "/api/llm/o/v1/chat/completions":
			note()
			for _, chunk := range []string{
				`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4.1-flash-1017","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello from Factory"},"finish_reason":null}]}`,
				`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4.1-flash-1017","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
			} {
				io.WriteString(w, "data: "+chunk+"\n\n")
			}
			io.WriteString(w, "data: [DONE]\n\n")
		case r.URL.Path == "/api/llm/a/v1/messages":
			note()
			for _, ev := range [][2]string{
				{"message_start", `{"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","model":"claude","content":[],"stop_reason":null,"usage":{"input_tokens":5,"output_tokens":0}}}`},
				{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello from Factory"}}`},
				{"content_block_stop", `{"type":"content_block_stop","index":0}`},
				{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`},
				{"message_stop", `{"type":"message_stop"}`},
			} {
				io.WriteString(w, "event: "+ev[0]+"\ndata: "+ev[1]+"\n\n")
			}
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeFactory) requests() []http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]http.Header(nil), f.seen...)
}

// poolKey is the Key Pool key the fake Factory serves.
const poolKey = "fk-test-0000000000"

// hostKernel is a Host whose Kernel runs against f, and the window's connection to each generation of it.
type hostKernel struct {
	h       *Host
	clients chan *kernel.Client
}

func startHostKernel(t *testing.T, f *fakeFactory, withKey bool) *hostKernel {
	t.Helper()
	if testing.Short() {
		t.Skip("runs the Kernel")
	}
	bin := kernelBinaryForTest(t)
	env := map[string]string{KernelEnv: "1", KernelBinEnv: bin}
	h := New(Config{UserData: t.TempDir(), Home: t.TempDir(), Env: func(k string) string { return env[k] }, KeyPoolUpstream: f.URL})
	if !h.KernelEnabled() {
		t.Fatal("the Kernel is not enabled")
	}
	if withKey {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := h.KeyPool().Add(ctx, poolKey); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(h.KeyPool().Stop)
	hk := &hostKernel{h: h, clients: make(chan *kernel.Client, 8)}
	link := NewKernelLink(KernelLinkOptions{
		Client:   "window",
		Token:    h.KernelToken,
		OnClient: func(c *kernel.Client, _ int) { hk.clients <- c },
		Log:      func(s string) { t.Log("link:", s) },
	})
	unhear := h.OnKernelState(link.Use)
	h.startKernel()
	t.Cleanup(func() {
		unhear()
		link.Close()
		h.stopKernel()
	})
	return hk
}

// next is the window's connection to the next generation of the Kernel.
func (hk *hostKernel) next(t *testing.T, ctx context.Context) *kernel.Client {
	t.Helper()
	select {
	case c := <-hk.clients:
		return c
	case <-ctx.Done():
		k := &hk.h.kernel
		k.mu.Lock()
		last := k.last
		k.mu.Unlock()
		t.Fatalf("no Kernel connection: %+v", last)
		return nil
	}
}

// offered is the model provider offers on c: model when it is offered, else the first of the provider's.
func offered(t *testing.T, ctx context.Context, c *kernel.Client, provider, model string) (kernel.ModelRef, bool) {
	t.Helper()
	info, err := c.Harness(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var first *kernel.ModelRef
	for _, m := range info.Models {
		if m.Provider != provider {
			continue
		}
		if m.ID == model || model == "" {
			return kernel.ModelRef{Provider: m.Provider, ModelID: m.ID}, true
		}
		if first == nil {
			first = &kernel.ModelRef{Provider: m.Provider, ModelID: m.ID}
		}
	}
	if first != nil && model == "" {
		return *first, true
	}
	return kernel.ModelRef{}, false
}

// answer asks model one question on c and waits for Factory's answer.
func answer(t *testing.T, ctx context.Context, c *kernel.Client, model kernel.ModelRef) {
	t.Helper()
	created, err := c.Create(ctx, kernel.CreateParams{Agent: &kernel.AgentChange{Model: kernel.Some(model)}, Cwd: t.TempDir(), Kind: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Submit(ctx, kernel.SubmitParams{ID: created.ConversationID, Input: "hi"}); err != nil {
		t.Fatal(err)
	}
	for ctx.Err() == nil {
		page, err := c.Entries(ctx, kernel.EntriesParams{ID: created.ConversationID, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Items {
			if e.Kind == "pi.assistant" {
				if !strings.Contains(string(e.Model), "Hello from Factory") {
					t.Fatalf("%s answered %s", model.ModelID, e.Model)
				}
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s: no answer", model.ModelID)
}

// The Host's Kernel answers with Factory's models through the Key Pool, on the chat wire and on Anthropic's: the
// request leaves the proxy with the pool's key and droid's headers, and the token the Kernel holds in place of a key
// goes nowhere.
func TestTheHostsKernelReachesFactoryThroughTheKeyPool(t *testing.T) {
	f := newFakeFactory(t)
	hk := startHostKernel(t, f, true)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c := hk.next(t, ctx)
	chat, ok := offered(t, ctx, c, "factory-chat", "deepseek-v4.1-flash-1017")
	if !ok {
		t.Fatal("harness.info does not offer deepseek-v4.1-flash-1017")
	}
	claude, ok := offered(t, ctx, c, "factory-anthropic", "")
	if !ok {
		t.Fatal("harness.info offers no Anthropic model")
	}
	answer(t, ctx, c, chat)
	answer(t, ctx, c, claude)

	seen := f.requests()
	if len(seen) != 2 {
		t.Fatalf("Factory saw %d model requests, want 2", len(seen))
	}
	if got := seen[0]; got.Get("Authorization") != "Bearer "+poolKey || got.Get("X-Api-Provider") != "fireworks" || got.Get("X-Factory-Org-Id") != "org_test" {
		t.Fatalf("the chat request went out as %v", got)
	}
	if got := seen[1]; got.Get("Authorization") != "Bearer "+poolKey || got.Get("X-Api-Key") != "placeholder" {
		t.Fatalf("the Anthropic request went out as %v", got)
	}
	k := &hk.h.kernel
	k.mu.Lock()
	token := k.llmToken
	k.mu.Unlock()
	for _, got := range seen {
		for name, vals := range got {
			if strings.Contains(strings.Join(vals, " "), token) {
				t.Fatalf("the Kernel's token left in %s", name)
			}
		}
	}
}

// A Daemon whose droid does not send through the Key Pool stops its proxy when it starts, but not while the Kernel's
// models point at it.
func TestADaemonStartingWithoutTheKeyPoolLeavesTheKernelsModels(t *testing.T) {
	f := newFakeFactory(t)
	hk := startHostKernel(t, f, true)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c := hk.next(t, ctx)
	chat, _ := offered(t, ctx, c, "factory-chat", "deepseek-v4.1-flash-1017")
	answer(t, ctx, c, chat)
	hk.h.releaseKeyPool()
	answer(t, ctx, c, chat)
}

// A Key Pool proxy that stopped, or moved to another address, under the Kernel starts the Kernel again on the one
// that runs now.
func TestTheHostsKernelFollowsTheKeyPoolsProxy(t *testing.T) {
	f := newFakeFactory(t)
	hk := startHostKernel(t, f, true)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	c := hk.next(t, ctx)
	chat, _ := offered(t, ctx, c, "factory-chat", "deepseek-v4.1-flash-1017")
	answer(t, ctx, c, chat)
	hk.h.KeyPool().Stop()
	c = hk.next(t, ctx)
	answer(t, ctx, c, chat)
}

// A Kernel started before the Key Pool had a key has no models; the first key starts it again with them.
func TestTheHostsKernelGetsFactorysModelsOnceAKeyIsAdded(t *testing.T) {
	f := newFakeFactory(t)
	hk := startHostKernel(t, f, false)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	c := hk.next(t, ctx)
	if _, ok := offered(t, ctx, c, "factory-chat", ""); ok {
		t.Fatal("a Kernel with no key behind it offers Factory's models")
	}
	if err := hk.h.KeyPool().Add(ctx, poolKey); err != nil {
		t.Fatal(err)
	}
	c = hk.next(t, ctx)
	chat, ok := offered(t, ctx, c, "factory-chat", "deepseek-v4.1-flash-1017")
	if !ok {
		t.Fatal("no Factory models after the first key")
	}
	answer(t, ctx, c, chat)
}

// A launch that was preparing while the Kernel stopped gives the proxy no token: once stopped, the Kernel's token
// opens nothing.
func TestALateLaunchAfterTheKernelStoppedLetsNoTokenIn(t *testing.T) {
	f := newFakeFactory(t)
	hk := startHostKernel(t, f, true)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	hk.next(t, ctx)
	hk.h.stopKernel()
	for _, kv := range hk.h.kernelEnv() {
		if strings.HasPrefix(kv, kernelProvidersEnv+"=") {
			t.Fatal("a stopped Kernel's launch got Factory's models")
		}
	}
	k := &hk.h.kernel
	k.mu.Lock()
	token := k.llmToken
	k.mu.Unlock()
	if token != "" {
		t.Fatal("a stopped Kernel's token is kept")
	}
}
