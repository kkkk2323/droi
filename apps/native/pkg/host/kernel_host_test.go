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
	} {
		p, ok := byID[id]
		if !ok || p.API != want[0] || p.BaseURL != want[1] || len(p.Models) == 0 {
			t.Fatalf("%s: %+v", id, p)
		}
	}
	if len(providers) != 3 {
		t.Fatalf("%d providers: Gemini's wait for their request shape", len(providers))
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
	m, ok := find("factory-chat", "kimi-k3")
	if !ok || !m.Reasoning || strings.Join(m.Thinking, ",") != "off,low,high,max" || strings.Join(m.Input, ",") != "text,image" {
		t.Fatalf("kimi-k3: %+v", m)
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

// fakeFactory is enough of Factory for the Key Pool and one model: billing, whoami and a streamed chat completion.
type fakeFactory struct {
	mu   sync.Mutex
	seen []http.Header
	*httptest.Server
}

func newFakeFactory(t *testing.T) *fakeFactory {
	f := &fakeFactory{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/billing/limits"):
			io.WriteString(w, `{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"fiveHour":{"usedPercent":10,"windowEnd":"2099-06-19T15:00:00Z"}},"core":{"fiveHour":{"usedPercent":5,"windowEnd":"2099-06-19T15:00:00Z"}}}}`)
		case r.URL.Path == "/api/cli/whoami":
			io.WriteString(w, `{"orgId":"org_test"}`)
		case r.URL.Path == "/api/llm/o/v1/chat/completions":
			_, _ = io.Copy(io.Discard, r.Body)
			f.mu.Lock()
			f.seen = append(f.seen, r.Header.Clone())
			f.mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			for _, chunk := range []string{
				`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4.1-flash-1017","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello from Factory"},"finish_reason":null}]}`,
				`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4.1-flash-1017","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
			} {
				io.WriteString(w, "data: "+chunk+"\n\n")
			}
			io.WriteString(w, "data: [DONE]\n\n")
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

// The Host's Kernel answers with a Factory model through the Key Pool: the request leaves the proxy with the pool's
// key and droid's headers, and the token the Kernel holds in place of a key goes nowhere.
func TestTheHostsKernelReachesFactoryThroughTheKeyPool(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the Kernel")
	}
	bin := kernelBinaryForTest(t)
	f := newFakeFactory(t)
	env := map[string]string{KernelEnv: "1", KernelBinEnv: bin}
	h := New(Config{UserData: t.TempDir(), Home: t.TempDir(), Env: func(k string) string { return env[k] }, KeyPoolUpstream: f.URL})
	if !h.KernelEnabled() {
		t.Fatal("the Kernel is not enabled")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	const key = "fk-test-0000000000"
	if err := h.KeyPool().Add(ctx, key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.KeyPool().Stop)

	clients := make(chan *kernel.Client, 4)
	link := NewKernelLink(KernelLinkOptions{
		Client:   "window",
		Token:    h.KernelToken,
		OnClient: func(c *kernel.Client, _ int) { clients <- c },
		Log:      func(s string) { t.Log("link:", s) },
	})
	unhear := h.OnKernelState(link.Use)
	h.startKernel()
	t.Cleanup(func() {
		unhear()
		link.Close()
		h.stopKernel()
	})
	var c *kernel.Client
	select {
	case c = <-clients:
	case <-ctx.Done():
		t.Fatalf("no Kernel connection: %+v", h.kernel.last)
	}

	info, err := c.Harness(ctx)
	if err != nil {
		t.Fatal(err)
	}
	offered := false
	for _, m := range info.Models {
		if m.Provider == "factory-chat" && m.ID == "deepseek-v4.1-flash-1017" {
			offered = true
		}
	}
	if !offered {
		t.Fatalf("harness.info offers %+v", info.Models)
	}
	model := kernel.ModelRef{Provider: "factory-chat", ModelID: "deepseek-v4.1-flash-1017"}
	created, err := c.Create(ctx, kernel.CreateParams{Agent: &kernel.AgentChange{Model: kernel.Some(model)}, Cwd: t.TempDir(), Kind: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Submit(ctx, kernel.SubmitParams{ID: created.ConversationID, Input: "hi"}); err != nil {
		t.Fatal(err)
	}
	answered := false
	for !answered && ctx.Err() == nil {
		page, err := c.Entries(ctx, kernel.EntriesParams{ID: created.ConversationID, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Items {
			if e.Kind == "pi.assistant" {
				if !strings.Contains(string(e.Model), "Hello from Factory") {
					t.Fatalf("the answer is %s", e.Model)
				}
				answered = true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !answered {
		t.Fatal("no answer")
	}

	seen := f.requests()
	if len(seen) != 1 {
		t.Fatalf("Factory saw %d model requests, want 1", len(seen))
	}
	got := seen[0]
	if got.Get("Authorization") != "Bearer "+key || got.Get("X-Api-Provider") != "fireworks" || got.Get("X-Factory-Org-Id") != "org_test" {
		t.Fatalf("the request went out as %v", got)
	}
	h.kernel.mu.Lock()
	token := h.kernel.llmToken
	h.kernel.mu.Unlock()
	for name, vals := range got {
		if strings.Contains(strings.Join(vals, " "), token) {
			t.Fatalf("the Kernel's token left in %s", name)
		}
	}
}
