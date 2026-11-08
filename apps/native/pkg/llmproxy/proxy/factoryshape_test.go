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

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/factory"
)

// recorded is what the upstream got: the proxy must hand it a request opened
// as droid opens one, whatever the client sent.
type recorded struct {
	mu      sync.Mutex
	path    []string
	body    [][]byte
	head    []http.Header
	length  []int64
	handler func(w http.ResponseWriter, req *http.Request)
}

func newRecorded() *recorded {
	r := &recorded{}
	r.handler = func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.path = append(r.path, req.URL.Path)
		r.body = append(r.body, body)
		r.head = append(r.head, req.Header.Clone())
		r.length = append(r.length, req.ContentLength)
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}
	return r
}

// last answers the request the upstream got most recently.
func (r *recorded) last(t *testing.T) (path string, body []byte, head http.Header, length int64) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.body) == 0 {
		t.Fatal("the upstream saw no request")
	}
	i := len(r.body) - 1
	return r.path[i], r.body[i], r.head[i], r.length[i]
}

// kernelChat is the Kernel's own prompt, which Factory refused with 403
// content_policy_rejected before this layer existed.
const kernelChat = `{"model":"deepseek-v4.1-flash-1017","messages":[{"role":"system","content":"<environment>\nYou are running as the Kernel of Droi, on macOS.\n</environment>"},{"role":"user","content":"hi"}],"stream":true}`

func TestProxy_OpensAFactoryRequestAsDroidDoes(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		// The path the upstream gets, when the request moves routes (Gemini's
		// Google path becomes Factory's /generate).
		wantPath string
		want     func(t *testing.T, got map[string]any)
	}{
		{
			name: "chat completions", path: "/api/llm/o/v1/chat/completions", body: kernelChat,
			want: func(t *testing.T, got map[string]any) {
				messages, _ := got["messages"].([]any)
				if len(messages) != 2 {
					t.Fatalf("messages %v", got["messages"])
				}
				first, _ := messages[0].(map[string]any)
				content, _ := first["content"].(string)
				if !strings.HasPrefix(content, factory.DroidLine+"\n<environment>") {
					t.Fatalf("first system message %q", content)
				}
				if got["model"] != "deepseek-v4.1-flash-1017" || got["stream"] != true {
					t.Fatalf("the rest of the request changed: %v", got)
				}
			},
		},
		{
			name: "responses", path: "/api/llm/o/v1/responses",
			body: `{"model":"gpt-5.5","instructions":"You are Codex, a coding agent.","input":[{"role":"user","content":"hi"}]}`,
			want: func(t *testing.T, got map[string]any) {
				if want := factory.DroidLine + "\nYou are Codex, a coding agent."; got["instructions"] != want {
					t.Fatalf("instructions %v", got["instructions"])
				}
			},
		},
		{
			name: "messages", path: "/api/llm/a/v1/messages",
			body: `{"model":"claude-opus-5-5","system":"You are Claude Code.","messages":[{"role":"user","content":"hi"}]}`,
			want: func(t *testing.T, got map[string]any) {
				system, _ := got["system"].([]any)
				if len(system) != 2 {
					t.Fatalf("system %v", got["system"])
				}
				first, _ := system[0].(map[string]any)
				if first["text"] != factory.DroidLine {
					t.Fatalf("system %v", got["system"])
				}
			},
		},
		{
			name: "gemini", path: "/api/llm/g/v1/models/gemini-3-pro:streamGenerateContent",
			wantPath: "/api/llm/g/v1/generate",
			body:     `{"contents":[],"systemInstruction":{"parts":[{"text":"You are a coding agent."}]}}`,
			want: func(t *testing.T, got map[string]any) {
				si, _ := got["systemInstruction"].(map[string]any)
				parts, _ := si["parts"].([]any)
				first, _ := parts[0].(map[string]any)
				if want := factory.DroidLine + "\nYou are a coding agent."; first["text"] != want {
					t.Fatalf("systemInstruction %v", si)
				}
				if got["model"] != "gemini-3-pro" {
					t.Fatalf("the model in the body is %v", got["model"])
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up := newRecorded()
			upstream := httptest.NewServer(http.HandlerFunc(up.handler))
			defer upstream.Close()
			px := startSwitchProxy(t, upstream.URL, &switches{ok: true}, poolUsage("fk-a", 0, 0))

			resp, _ := postFactory(t, px.BaseURL(), tc.path, strings.NewReader(tc.body), nil)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d", resp.StatusCode)
			}
			path, body, _, length := up.last(t)
			wantPath := tc.wantPath
			if wantPath == "" {
				wantPath = tc.path
			}
			if path != wantPath {
				t.Fatalf("upstream path %q, want %q", path, wantPath)
			}
			tc.want(t, decodeJSON(t, body))
			if length != int64(len(body)) {
				t.Fatalf("Content-Length %d, body %d bytes", length, len(body))
			}
		})
	}
}

// A request the Daemon itself sends already opens with the line: it reaches
// Factory byte for byte, prompt cache and all.
func TestProxy_PassesDroidsOwnRequestByteForByte(t *testing.T) {
	for _, tc := range []struct{ path, body string }{
		{"/api/llm/o/v1/chat/completions", `{"model":"glm-5.3-flash","messages":[{"role":"system","content":"` + factory.DroidLine + `\nYou work in the user's terminal."},{"role":"user","content":"hi"}],"stream":true,"n":1.0}`},
		{"/api/llm/o/v1/responses", `{"model":"gpt-5.5","input":[],"store":false,"instructions":"` + factory.DroidLine + `","stream":true}`},
		{"/api/llm/a/v1/messages", `{"model":"claude-opus-5-5","system":[{"type":"text","text":"` + factory.DroidLine + `"}],"messages":[{"role":"user","content":"hi"}]}`},
		{"/api/llm/a/v1/messages", `not json`},
	} {
		up := newRecorded()
		upstream := httptest.NewServer(http.HandlerFunc(up.handler))
		px := startSwitchProxy(t, upstream.URL, &switches{ok: true}, poolUsage("fk-a", 0, 0))
		resp, _ := postFactory(t, px.BaseURL(), tc.path, strings.NewReader(tc.body), nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", tc.path, resp.StatusCode)
		}
		_, body, head, length := up.last(t)
		if string(body) != tc.body || length != int64(len(tc.body)) {
			t.Fatalf("%s:\n got %s\nwant %s", tc.path, body, tc.body)
		}
		if head.Get("Content-Encoding") != "" {
			t.Fatalf("%s: Content-Encoding %q", tc.path, head.Get("Content-Encoding"))
		}
		upstream.Close()
	}
}

// A gzipped request body is opened and goes out gzipped, as the client sent
// it, with the length of what is sent.
func TestProxy_OpensAGzippedRequestBody(t *testing.T) {
	up := newRecorded()
	upstream := httptest.NewServer(http.HandlerFunc(up.handler))
	defer upstream.Close()
	px := startSwitchProxy(t, upstream.URL, &switches{ok: true}, poolUsage("fk-a", 0, 0))

	wire, err := gzipBody([]byte(kernelChat))
	if err != nil {
		t.Fatal(err)
	}
	header := http.Header{"Content-Encoding": []string{"gzip"}, "Content-Type": []string{"application/json"}}
	resp, _ := postFactory(t, px.BaseURL(), "/api/llm/o/v1/chat/completions", bytes.NewReader(wire), header)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	_, body, head, length := up.last(t)
	if head.Get("Content-Encoding") != "gzip" || length != int64(len(body)) {
		t.Fatalf("Content-Encoding %q, Content-Length %d, %d bytes on the wire", head.Get("Content-Encoding"), length, len(body))
	}
	plain, err := gunzipBody(body)
	if err != nil {
		t.Fatalf("the upstream got something that is not gzip: %v", err)
	}
	got := decodeJSON(t, plain)
	messages, _ := got["messages"].([]any)
	first, _ := messages[0].(map[string]any)
	if content, _ := first["content"].(string); !strings.HasPrefix(content, factory.DroidLine+"\n<environment>") {
		t.Fatalf("first system message %q", content)
	}
}

// postFactory sends body to the proxy as a client does, with a pool key.
func postFactory(t *testing.T, base, path string, body io.Reader, header http.Header) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+path, body)
	if err != nil {
		t.Fatal(err)
	}
	for name, values := range header {
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}
	req.Header.Set("Authorization", "Bearer fk-a")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	answer, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(answer)
}

func decodeJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("not a JSON object: %v: %s", err, body)
	}
	return m
}

// A request that asks a model for fast reaches its Fast Mode model with the fast-mode beta, as droid sends one.
func TestProxy_SendsAFastRequestToItsFastModeModel(t *testing.T) {
	up := newRecorded()
	upstream := httptest.NewServer(http.HandlerFunc(up.handler))
	defer upstream.Close()
	px := startSwitchProxy(t, upstream.URL, &switches{ok: true}, poolUsage("fk-a", 0, 0))
	body := `{"model":"claude-opus-5-5","speed":"fast","system":[{"type":"text","text":"` + factory.DroidLine + `"}],"messages":[{"role":"user","content":"hi"}]}`
	resp, _ := postFactory(t, px.BaseURL(), "/api/llm/a/v1/messages", strings.NewReader(body), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	_, sent, head, _ := up.last(t)
	if got := decodeJSON(t, sent); got["model"] != "claude-opus-5-5-fast" || got["speed"] != "fast" {
		t.Fatalf("sent %s", sent)
	}
	if head.Get("Anthropic-Beta") != factory.FastModeBeta {
		t.Fatalf("anthropic-beta %q", head.Get("Anthropic-Beta"))
	}
}
