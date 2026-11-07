// Package fakekernel runs a real Kernel against a scripted model, for Go tests.
//
// There is no test-only mode in the Kernel: this starts the production sidecar and points its provider at
// an OpenAI-compatible endpoint the test itself serves. So a test drives the real protocol — real
// conversations, real transcripts, real view pushes — and the model is the only thing that is fake. The
// script is Go, which means the compiler checks it and the test can also read back what the Kernel sent.
//
// The binary comes from DROI_KERNEL_BIN, or else the build in apps/native/assets/kernel/<goos>-<goarch>
// that `pnpm build:kernel` writes; without one the test is skipped rather than failed, so `go test
// ./apps/native/...` works on a checkout with no Kernel built.
package fakekernel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// ProviderID and ModelID are what a scripted Kernel's model is called.
const (
	ProviderID = "fake"
	ModelID    = "fake-1"
)

// Call is one tool the model asks the Kernel to run.
type Call struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	ID        string         `json:"id,omitempty"`
}

// Failure is a model request that fails the way a provider does.
type Failure struct {
	// Status is the HTTP status to answer with; 0 means the connection is closed instead, which is what a
	// network drop looks like to the Kernel.
	Status int    `json:"status,omitempty"`
	Body   string `json:"body,omitempty"`
}

// Step is one model answer, matched to the request that asks for it.
type Step struct {
	// Match is a substring of the last user message; "" matches any request. A request takes the first
	// step that matches it and never takes it again.
	Match string `json:"match,omitempty"`
	// Text is the reply, streamed as deltas.
	Text string `json:"text,omitempty"`
	// Chunks splits Text into these deltas instead of a fixed 40-character split, so a test can assert
	// what a viewer sees between deltas.
	Chunks []string `json:"chunks,omitempty"`
	// Call, when set, is a tool call instead of text.
	Call *Call `json:"call,omitempty"`
	// Failure, when set, makes this request fail (status, or a dropped connection).
	Failure *Failure `json:"failure,omitempty"`
	// Delay is waited before each delta: a slow stream, for a test of incremental rendering.
	Delay time.Duration `json:"delay,omitempty"`
}

// Scenario is everything the scripted model answers, in order.
type Scenario struct {
	Steps []Step `json:"steps,omitempty"`
	// Fallback is the text for a request no step matched; "" is "done". A test that would rather know
	// reads Unmatched.
	Fallback string `json:"fallback,omitempty"`
}

// Request is one model request, as the Kernel sent it.
type Request struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	Stream bool `json:"stream"`
}

// Prompt is the text of the last user message: what a Step's Match is tested against.
func (r Request) Prompt() string {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role != "user" {
			continue
		}
		return textOf(r.Messages[i].Content)
	}
	return ""
}

// Kernel is a running Kernel and the scripted model it talks to.
type Kernel struct {
	// Port is the port the Kernel reported in its handshake.
	Port int
	// Token is the launch token; Dial passes it.
	Token string
	// DataDir is the throwaway data directory, kept until the test ends.
	DataDir string
	// Version is the Kernel's version, from its handshake.
	Version string

	model     *scriptedModel
	cmd       *exec.Cmd
	logs      *lockedBuffer
	wait      chan struct{}
	closeOnce sync.Once
}

// Start runs a Kernel with the scenario's model and stops both when the test ends.
//
// It skips the test when no Kernel binary is available: that is a checkout without `pnpm build:kernel`,
// not a failure of the code under test.
func Start(t testing.TB, sc Scenario) *Kernel {
	t.Helper()
	bin := binary(t)
	if bin == "" {
		t.Skip("no Kernel binary: set DROI_KERNEL_BIN or run pnpm build:kernel")
	}

	model := startScriptedModel(t, sc)
	data := t.TempDir()
	token, err := kernel.NewToken()
	if err != nil {
		t.Fatalf("fakekernel: token: %v", err)
	}

	cmd := exec.Command(bin, "--data", data, "--port", "0", "--host", "127.0.0.1")
	cmd.Env = append(os.Environ(),
		kernel.TokenEnv+"="+token,
		"DROI_KERNEL_PROVIDERS="+providersJSON(model.baseURL),
		"FAKE_PROVIDER_KEY=fake-key",
	)
	logs := &lockedBuffer{}
	cmd.Stderr = logs
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("fakekernel: stdout: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("fakekernel: start %s: %v", bin, err)
	}

	k := &Kernel{Token: token, DataDir: data, model: model, cmd: cmd, logs: logs, wait: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(k.wait)
	}()
	t.Cleanup(k.Close)

	// The handshake is the first line of stdout: the port is the Kernel's to choose, so no test has to
	// probe for a free one (and race whoever takes it next).
	handshake, err := readHandshake(stdout, 30*time.Second)
	if err != nil {
		t.Fatalf("fakekernel: %v\n%s", err, logs.String())
	}
	k.Port = handshake.Port
	k.Version = handshake.Version
	return k
}

// Close stops the Kernel and the scripted model. It is called when the test ends; calling it twice is
// harmless.
func (k *Kernel) Close() {
	k.closeOnce.Do(func() {
		if k.cmd.Process != nil {
			_ = k.cmd.Process.Kill()
		}
		select {
		case <-k.wait:
		case <-time.After(10 * time.Second):
		}
		k.model.stop()
	})
}

// Logs is what the Kernel wrote to stderr so far; a failing assertion prints it.
func (k *Kernel) Logs() string { return k.logs.String() }

// Requests are the model requests the Kernel made, in order.
func (k *Kernel) Requests() []Request { return k.model.requests() }

// Unmatched is how many requests no step matched and the fallback answered: a test that expects every
// turn to be scripted asserts it is zero.
func (k *Kernel) Unmatched() int { return k.model.unmatched() }

// Dial connects a client to the Kernel, waits for its health check, and closes the client when the test
// ends.
func (k *Kernel) Dial(t testing.TB) *kernel.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	for {
		if _, err := kernel.CheckHealth(ctx, k.Port); err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("fakekernel: no health answer in 60 s:\n%s", k.Logs())
		}
		time.Sleep(20 * time.Millisecond)
	}
	client, err := kernel.Dial(ctx, kernel.Options{
		URL:            kernel.URL(k.Port),
		Token:          k.Token,
		RequestTimeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("fakekernel: dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// ── the scripted model ────────────────────────────────────────────────────────────────────────────

type scriptedModel struct {
	scenario Scenario
	server   *http.Server
	listener net.Listener
	baseURL  string

	mu     sync.Mutex
	steps  []Step
	seen   []Request
	misses int
}

func startScriptedModel(t testing.TB, sc Scenario) *scriptedModel {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakekernel: listen: %v", err)
	}
	m := &scriptedModel{scenario: sc, steps: append([]Step(nil), sc.Steps...), listener: listener}
	m.baseURL = "http://" + listener.Addr().String() + "/v1"
	m.server = &http.Server{Handler: http.HandlerFunc(m.serve), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = m.server.Serve(listener) }()
	t.Cleanup(m.stop)
	return m
}

func (m *scriptedModel) stop() {
	if m.server != nil {
		_ = m.server.Close()
		m.server = nil
	}
}

func (m *scriptedModel) requests() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Request(nil), m.seen...)
}

func (m *scriptedModel) unmatched() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.misses
}

// take picks the first step whose Match the prompt satisfies, and consumes it.
func (m *scriptedModel) take(prompt string) (Step, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, step := range m.steps {
		if step.Match != "" && !strings.Contains(prompt, step.Match) {
			continue
		}
		m.steps = append(m.steps[:i], m.steps[i+1:]...)
		return step, true
	}
	m.misses++
	return Step{Text: m.scenario.Fallback}, false
}

func (m *scriptedModel) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.NotFound(w, r)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var request Request
	if err := json.Unmarshal(body, &request); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	m.seen = append(m.seen, request)
	m.mu.Unlock()

	step, matched := m.take(request.Prompt())
	if step.Failure != nil {
		if step.Failure.Status == 0 {
			// A dropped connection: no response at all, which the Kernel sees as the network failing.
			if hijacker, ok := w.(http.Hijacker); ok {
				if conn, _, err := hijacker.Hijack(); err == nil {
					_ = conn.Close()
					return
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(step.Failure.Status)
		body := step.Failure.Body
		if body == "" {
			body = http.StatusText(step.Failure.Status)
		}
		_, _ = io.WriteString(w, `{"error":{"message":`+quote(body)+`}}`)
		return
	}
	_ = matched
	if !request.Stream {
		m.whole(w, step)
		return
	}
	m.stream(w, step)
}

// whole answers without SSE, which a Kernel that asks for stream=false reads.
func (m *scriptedModel) whole(w http.ResponseWriter, step Step) {
	message := map[string]any{"role": "assistant", "content": step.Text}
	finish := "stop"
	if step.Call != nil {
		args, _ := json.Marshal(step.Call.Arguments)
		message = map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{
			"id": callID(step), "type": "function",
			"function": map[string]any{"name": step.Call.Name, "arguments": string(args)},
		}}}
		finish = "tool_calls"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "chatcmpl-fake", "object": "chat.completion", "created": 1, "model": ModelID,
		"choices": []any{map[string]any{"index": 0, "finish_reason": finish, "message": message}},
		"usage":   usage(),
	})
}

// stream writes the reply as the SSE chunks pi-ai parses.
func (m *scriptedModel) stream(w http.ResponseWriter, step Step) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	send := func(delta map[string]any, finish any, extra map[string]any) {
		chunk := map[string]any{
			"id": "chatcmpl-fake", "object": "chat.completion.chunk", "created": 1, "model": ModelID,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		}
		for key, value := range extra {
			chunk[key] = value
		}
		encoded, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", encoded)
		if flusher != nil {
			flusher.Flush()
		}
	}
	pause := func() {
		if step.Delay > 0 {
			time.Sleep(step.Delay)
		}
	}

	pause()
	send(map[string]any{"role": "assistant", "content": ""}, nil, nil)
	if step.Call != nil {
		args, _ := json.Marshal(step.Call.Arguments)
		pause()
		send(map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": callID(step), "type": "function",
			"function": map[string]any{"name": step.Call.Name, "arguments": string(args)},
		}}}, nil, nil)
		pause()
		send(map[string]any{}, "tool_calls", nil)
	} else {
		pieces := step.Chunks
		if len(pieces) == 0 {
			pieces = split(step.Text, 40)
		}
		for _, piece := range pieces {
			pause()
			if piece == "" {
				continue
			}
			send(map[string]any{"content": piece}, nil, nil)
		}
		send(map[string]any{}, "stop", nil)
	}
	// Usage comes in a chunk with no choices, which is where OpenAI puts it.
	send(map[string]any{}, nil, map[string]any{"choices": []any{}, "usage": usage()})
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func callID(step Step) string {
	if step.Call != nil && step.Call.ID != "" {
		return step.Call.ID
	}
	return "call_fake"
}

func usage() map[string]any {
	return map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
}

func providersJSON(baseURL string) string {
	encoded, _ := json.Marshal([]map[string]any{{
		"id":        ProviderID,
		"name":      "Fake provider",
		"baseUrl":   baseURL,
		"api":       "openai-completions",
		"apiKeyEnv": "FAKE_PROVIDER_KEY",
		"models": []any{map[string]any{
			"id": ModelID, "name": "Fake one", "contextWindow": 200_000, "maxTokens": 8_000,
			"input": []string{"text", "image"},
		}},
	}})
	return string(encoded)
}

// ── small helpers ─────────────────────────────────────────────────────────────────────────────────

type handshake struct {
	Port    int    `json:"port"`
	Pid     int    `json:"pid"`
	Version string `json:"version"`
}

func readHandshake(stdout io.Reader, within time.Duration) (handshake, error) {
	type result struct {
		h   handshake
		err error
	}
	done := make(chan result, 1)
	go func() {
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadString('\n')
			if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "{") {
				var h handshake
				if err := json.Unmarshal([]byte(trimmed), &h); err != nil {
					done <- result{err: fmt.Errorf("handshake is not JSON: %w", err)}
					return
				}
				done <- result{h: h}
				return
			}
			if err != nil {
				done <- result{err: fmt.Errorf("the kernel printed no handshake: %w", err)}
				return
			}
		}
	}()
	select {
	case r := <-done:
		return r.h, r.err
	case <-time.After(within):
		return handshake{}, fmt.Errorf("no handshake in %s", within)
	}
}

// binary is the Kernel to run: DROI_KERNEL_BIN, or the build the release packs.
func binary(t testing.TB) string {
	t.Helper()
	if bin := os.Getenv("DROI_KERNEL_BIN"); bin != "" {
		return bin
	}
	exe := "kernel"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	// packages run from their own directory: walk up to apps/native, where the release's build sits.
	for dir := "."; ; {
		candidate := filepath.Join(dir, "assets", "kernel", runtime.GOOS+"-"+runtime.GOARCH, exe)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
		next := filepath.Join(dir, "..")
		if abs, err := filepath.Abs(next); err != nil || abs == "/" || strings.HasSuffix(abs, "/..") {
			return ""
		}
		dir = next
	}
}

func textOf(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		parts := make([]string, 0, len(blocks))
		for _, block := range blocks {
			if block.Type == "text" {
				parts = append(parts, block.Text)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// split cuts text into pieces of at most width bytes, on rune boundaries.
func split(text string, width int) []string {
	var pieces []string
	runes := []rune(text)
	for len(runes) > width {
		pieces = append(pieces, string(runes[:width]))
		runes = runes[width:]
	}
	if len(runes) > 0 {
		pieces = append(pieces, string(runes))
	}
	return pieces
}

func quote(text string) string {
	encoded, _ := json.Marshal(text)
	return string(encoded)
}

type lockedBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
