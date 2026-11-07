package kernel

import (
	"context"
	"encoding/json"
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

// kernelBinary is the sidecar the tests run: DROI_KERNEL_BIN when set, else the
// build the release packs (design §8).
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

// fakeProvider answers the sidecar's model requests the way an OpenAI
// completions endpoint does, and echoes the last user message so a test can
// tell whose turn produced what.
//
// The base URL goes to Kernel in DROI_KERNEL_PROVIDERS, which Kernel reads to
// register the provider a test wants; see shared/interfaces/kernel-protocol.md
// for the shape this asks for.
func fakeProvider(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
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
		reply := "echo: " + last
		if !body.Stream {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "chatcmpl-fake", "object": "chat.completion", "created": time.Now().Unix(),
				"model": "fake-echo",
				"choices": []any{map[string]any{
					"index": 0, "finish_reason": "stop",
					"message": map[string]any{"role": "assistant", "content": reply},
				}},
				"usage": map[string]any{"prompt_tokens": 4, "completion_tokens": 3, "total_tokens": 7},
			})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flush := w.(http.Flusher).Flush
		send := func(delta map[string]any, finish any, usage any) {
			chunk := map[string]any{
				"id": "chatcmpl-fake", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": "fake-echo",
				"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
			}
			if usage != nil {
				chunk["usage"] = usage
			}
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flush()
		}
		send(map[string]any{"role": "assistant"}, nil, nil)
		send(map[string]any{"content": reply}, nil, nil)
		send(map[string]any{}, "stop", map[string]any{"prompt_tokens": 4, "completion_tokens": 3, "total_tokens": 7})
		fmt.Fprint(w, "data: [DONE]\n\n")
		flush()
	}))
	t.Cleanup(server.Close)
	return server, server.URL
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

// TestLiveKernelThreeConversations is M1's first acceptance: three
// conversations on one Kernel, two turns each, at the same time, and each
// transcript holds its own turns only.
//
// It runs only with a Kernel binary (DROI_KERNEL_BIN, or the release's build),
// and the sidecar must read DROI_KERNEL_PROVIDERS to point its provider at the
// fake one below.
func TestLiveKernelThreeConversations(t *testing.T) {
	bin := kernelBinary(t)
	if bin == "" {
		t.Skip("no kernel binary: set DROI_KERNEL_BIN, or build apps/kernel")
	}

	_, providerURL := fakeProvider(t)
	model := os.Getenv("DROI_KERNEL_MODEL")
	if model == "" {
		model = "fake-echo"
	}
	// The provider is described here, not by the caller: its port is chosen when the fake provider starts, so
	// a DROI_KERNEL_PROVIDERS value from the environment could not name it. The shape is the protocol's, and
	// FAKE_PROVIDER_KEY is the throwaway key pi-ai resolves from the environment.
	providers := fmt.Sprintf(
		`[{"id":"fake","baseUrl":%q,"api":"openai-completions","apiKeyEnv":"FAKE_PROVIDER_KEY","models":[{"id":%q,"contextWindow":200000,"maxTokens":8000,"input":["text","image"]}]}]`,
		providerURL+"/v1", model,
	)

	data := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--port", fmt.Sprint(port), "--host", "127.0.0.1", "--data", data)
	cmd.Env = append(os.Environ(), TokenEnv+"="+token, "DROI_KERNEL_PROVIDERS="+providers,
		"FAKE_PROVIDER_KEY=fake-provider-key")
	logs := &strings.Builder{}
	cmd.Stderr = logs
	cmd.Stdout = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("start kernel: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	// Readiness is the contract's health check, which needs no token.
	for deadline := time.Now().Add(30 * time.Second); ; {
		if _, err := CheckHealth(t.Context(), port); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("kernel never answered /health:\n%s", logs.String())
		}
		time.Sleep(100 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	c, err := Dial(ctx, Options{URL: URL(port), Token: token, RequestTimeout: 60 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	go func() {
		for range c.Notifications() {
		}
	}()

	info, err := c.Harness(ctx)
	if err != nil {
		t.Fatalf("harness.info: %v", err)
	}
	if len(info.Models) == 0 {
		t.Fatal("harness.info lists no models")
	}
	modelRef := ModelRef{Provider: info.Models[0].Provider, ModelID: info.Models[0].ID}
	for _, m := range info.Models {
		if m.ID == model {
			modelRef = ModelRef{Provider: m.Provider, ModelID: m.ID}
		}
	}

	const conversations, turns = 3, 2
	ids := make([]ID, conversations)
	for i := range ids {
		created, err := c.Create(ctx, CreateParams{Agent: &AgentChange{Model: &modelRef}, Kind: "chat", Cwd: t.TempDir()})
		if err != nil {
			t.Fatalf("conversation %d: %v", i, err)
		}
		ids[i] = created.ConversationID
		if _, err := c.Open(ctx, OpenParams{ID: ids[i], Window: &Window{Min: 20, Max: 100}}); err != nil {
			t.Fatalf("open %d: %v", ids[i], err)
		}
	}

	// Each conversation sends its own turns, all at the same time.
	var wg sync.WaitGroup
	errs := make(chan error, conversations)
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for turn := 1; turn <= turns; turn++ {
				text := fmt.Sprintf("conversation %d turn %d", i+1, turn)
				// No WhenBusy: an input that arrives while the conversation runs is queued, which is
				// what this test wants, and "queue" is not a value the Kernel accepts (-32602).
				if _, err := c.Submit(ctx, SubmitParams{ID: id, Input: text}); err != nil {
					errs <- fmt.Errorf("%s: %w", text, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	// Wait until every conversation has said its turns back, which only
	// happens if every model request was answered.
	deadline := time.Now().Add(2 * time.Minute)
	for _, id := range ids {
		for {
			if time.Now().After(deadline) {
				t.Fatalf("conversation %d never finished its turns", id)
			}
			entries, err := c.Entries(ctx, EntriesParams{ID: id, Limit: 100})
			if err == nil {
				echoes := 0
				for _, entry := range entries.Items {
					if entry.Kind == EntryAssistant && strings.Contains(string(entry.Model), "echo:") {
						echoes++
					}
				}
				if echoes >= turns {
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	// No conversation's transcript may hold another's words.
	for i, id := range ids {
		entries, err := c.Entries(ctx, EntriesParams{ID: id, Limit: 200})
		if err != nil {
			t.Fatalf("entries %d: %v", id, err)
		}
		body := fmt.Sprint(entries.Items)
		for other := 1; other <= conversations; other++ {
			marker := fmt.Sprintf("conversation %d turn", other)
			has := strings.Contains(body, marker)
			if has != (other == i+1) {
				t.Fatalf("conversation %d has %q: %v\n%s", id, marker, has, body)
			}
		}
	}
}
