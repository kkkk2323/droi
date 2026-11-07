package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// kernelBinary is the sidecar these tests run: DROI_KERNEL_BIN when set, else
// the build the release packs (design §8).
func kernelBinary(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("DROI_KERNEL_BIN"); bin != "" {
		return bin
	}
	exe := "kernel"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	bin := filepath.Join("..", "..", "..", "assets", "kernel", runtime.GOOS+"-"+runtime.GOARCH, exe)
	if st, err := os.Stat(bin); err == nil && !st.IsDir() {
		return bin
	}
	return ""
}

// fakeProvider answers a model call the way an OpenAI chat completions
// endpoint does. Each request takes its place in a script: the reply echoes
// the conversation's own words back, so a test can tell whose turn it was.
type fakeProvider struct {
	server *httptest.Server
	calls  atomic.Int64
	mu     sync.Mutex
	seen   []string
}

func startFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	p := &fakeProvider{}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		p.calls.Add(1)
		var body struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		last := ""
		for _, m := range body.Messages {
			if m.Role == "user" {
				last = textOf(m.Content)
			}
		}
		p.mu.Lock()
		p.seen = append(p.seen, last)
		p.mu.Unlock()
		reply := "echo: " + last
		if !body.Stream {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "chatcmpl-fake", "object": "chat.completion", "created": time.Now().Unix(),
				"model":   "fake-1",
				"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": reply}}},
				"usage":   map[string]any{"prompt_tokens": 8, "completion_tokens": 4, "total_tokens": 12},
			})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flush := w.(http.Flusher).Flush
		send := func(delta map[string]any, finish any) {
			chunk := map[string]any{"id": "chatcmpl-fake", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": "fake-1",
				"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flush()
		}
		send(map[string]any{"role": "assistant"}, nil)
		send(map[string]any{"content": reply}, nil)
		send(map[string]any{}, "stop")
		// Usage arrives in a chunk of its own, as OpenAI sends it.
		b, _ := json.Marshal(map[string]any{"id": "chatcmpl-fake", "object": "chat.completion.chunk", "model": "fake-1", "choices": []any{},
			"usage": map[string]any{"prompt_tokens": 8, "completion_tokens": 4, "total_tokens": 12}})
		fmt.Fprintf(w, "data: %s\n\n", b)
		fmt.Fprint(w, "data: [DONE]\n\n")
		flush()
	}))
	t.Cleanup(p.server.Close)
	return p
}

func (p *fakeProvider) baseURL() string { return p.server.URL + "/v1" }

// texts are the user messages the provider was asked about, in order.
func (p *fakeProvider) texts() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.seen...)
}

func textOf(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// liveKernel is a real Kernel process these tests drive through the Client.
type liveKernel struct {
	Client *Client
	Port   int
	Token  string
	Log    *strings.Builder
	cmd    *exec.Cmd
}

// fakeProviders is the JSON DROI_KERNEL_PROVIDERS carries: one provider that
// answers from the local server, with one model.
func fakeProviders(provider *fakeProvider) string {
	raw, _ := json.Marshal([]any{map[string]any{
		"id":        "fake",
		"name":      "Fake provider",
		"baseUrl":   provider.baseURL(),
		"api":       "openai-completions",
		"apiKeyEnv": "FAKE_PROVIDER_KEY",
		"models": []any{map[string]any{
			"id": "fake-1", "name": "Fake one", "contextWindow": 200000, "maxTokens": 8000, "input": []string{"text"},
		}},
	}})
	return string(raw)
}

// startLiveKernel launches the sidecar against a fake provider, pointed there
// by DROI_KERNEL_PROVIDERS: a built binary cannot be handed a fake provider
// any other way. A config file names the default agent, which is the other
// half of the contract — a conversation that names no agent runs with it.
func startLiveKernel(t *testing.T, provider *fakeProvider) *liveKernel {
	t.Helper()
	bin := kernelBinary(t)
	if bin == "" {
		t.Skip("no kernel binary: set DROI_KERNEL_BIN, or build apps/kernel (design §8)")
	}
	data := t.TempDir()
	defaults, err := json.Marshal(map[string]any{
		"agent": map[string]any{"provider": "fake", "modelId": "fake-1", "thinkingLevel": "off"},
	})
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "kernel.json")
	if err := os.WriteFile(configPath, defaults, 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--data", data, "--port", "0", "--host", "127.0.0.1", "--config", configPath)
	cmd.Env = append(os.Environ(), TokenEnv+"="+token, "FAKE_PROVIDER_KEY=fake-key",
		"DROI_KERNEL_PROVIDERS="+fakeProviders(provider))
	log := &strings.Builder{}
	cmd.Stderr = log
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start kernel: %v", err)
	}
	handshake := make(chan Handshake, 1)
	go func() {
		decoder := json.NewDecoder(stdout)
		var h Handshake
		if decoder.Decode(&h) == nil {
			handshake <- h
		}
		close(handshake)
	}()
	k := &liveKernel{Token: token, Log: log, cmd: cmd}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	select {
	case h, ok := <-handshake:
		if !ok {
			t.Fatalf("the kernel printed no handshake:\n%s", log.String())
		}
		k.Port = h.Port
	case <-time.After(60 * time.Second):
		t.Fatalf("no handshake in 60 s:\n%s", log.String())
	}
	for deadline := time.Now().Add(30 * time.Second); ; {
		if _, err := CheckHealth(t.Context(), k.Port); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the kernel never answered /health on %d:\n%s", k.Port, log.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	t.Cleanup(cancel)
	c, err := Dial(ctx, Options{URL: URL(k.Port), Token: token, RequestTimeout: 90 * time.Second})
	if err != nil {
		t.Fatalf("dial: %v\n%s", err, log.String())
	}
	t.Cleanup(func() { _ = c.Close() })
	k.Client = c
	return k
}

// freePort asks the system for a port nothing listens on.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

// isUnsupported reports the contract's answer for a method this build does not
// implement.
func isUnsupported(err error) bool { return Is(err, CodeUnsupported) }

var _ = errors.Is

// timeoutAfter bounds a live test's wait for a turn to settle.
func timeoutAfter() <-chan time.Time { return time.After(60 * time.Second) }
