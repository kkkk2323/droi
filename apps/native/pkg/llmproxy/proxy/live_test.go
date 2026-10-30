package proxy

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
)

// waitLive answers the live calls once ok takes them, or after five seconds:
// the proxy hands a chunk to the observer after writing it to the client, so
// the client can read a line before the live call counts it.
func waitLive(px *Proxy, ok func([]diag.LiveCall) bool) []diag.LiveCall {
	live := px.Live()
	for deadline := time.Now().Add(5 * time.Second); !ok(live) && time.Now().Before(deadline); live = px.Live() {
		time.Sleep(5 * time.Millisecond)
	}
	return live
}

// While a reply streams, the proxy shows it as a live call of its Session,
// with its first token and the tokens so far; once it ends, the Session's
// sums have it, with its time to the first token.
func TestProxy_ShowsAStreamingCallLiveThenSumsIt(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flush := w.(http.Flusher).Flush
		w.Write([]byte(`data: {"type":"message_start","message":{"usage":{"input_tokens":10,"cache_read_input_tokens":90,"output_tokens":1}}}` + "\n\n"))
		w.Write([]byte(`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hello world!"}}` + "\n\n"))
		flush()
		<-release
		w.Write([]byte(`data: {"type":"message_delta","usage":{"output_tokens":42}}` + "\n\n"))
	}))
	defer upstream.Close()
	// A failure before the release must not leave the upstream waiting.
	defer free()
	px, baseURL := startPremTestProxy(t, upstream.URL)

	req, _ := http.NewRequest("POST", baseURL+"/api/llm/a/v1/messages", strings.NewReader(`{"model":"claude-test","stream":true}`))
	req.Header.Set("Authorization", "Bearer fk-pool")
	req.Header.Set("X-Session-Id", "s-live")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := bufio.NewReader(resp.Body)
	for {
		line, err := body.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(line, "hello world") {
			break
		}
	}
	liveOK := func(l diag.LiveCall) bool {
		return l.SessionID == "s-live" && l.Model == "claude-test" && !l.FirstToken.IsZero() && l.Output == 3 && l.Usage.Input == 100
	}
	live := waitLive(px, func(live []diag.LiveCall) bool { return len(live) == 1 && liveOK(live[0]) })
	if len(live) != 1 {
		t.Fatalf("live calls %+v, want the one streaming", live)
	}
	l := live[0]
	if !liveOK(l) {
		t.Fatalf("live call %+v", l)
	}

	free()
	for {
		if _, err := body.ReadString('\n'); err != nil {
			break
		}
	}
	resp.Body.Close()
	var sums struct {
		calls int
		ttft  int64
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s, ok := px.Recorder().Session("s-live"); ok && len(px.Live()) == 0 {
			sums.calls, sums.ttft = s.Calls, s.Last.TTFTMs
			if s.Usage.Output != 42 {
				t.Fatalf("the Session's output %d, want 42", s.Usage.Output)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if sums.calls != 1 || sums.ttft <= 0 {
		t.Fatalf("the Session's sums: %d calls, first token after %d ms", sums.calls, sums.ttft)
	}
}

// Each upstream's stream shape, as droid gets it through Factory: while
// the reply streams the live call has its first token and the tokens so far,
// and once it ends the Session's sums have its usage, reasoning included,
// its time to the first token and its speed.
func TestProxy_MeasuresEachProvidersStream(t *testing.T) {
	for _, tc := range []struct {
		name, path          string
		before, after       []string
		output, input, read int64
		reasoning           int64
	}{
		{name: "anthropic", path: "/api/llm/a/v1/messages",
			before: []string{
				`{"type":"message_start","message":{"usage":{"input_tokens":200,"cache_read_input_tokens":1000,"output_tokens":1}}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me look at the file first."}}`,
				`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Reading it now."}}`,
			},
			after: []string{
				`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"/a/b.go\"}"}}`,
				`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":64}}`,
			},
			output: 64, input: 1200, read: 1000},
		{name: "openai responses", path: "/api/llm/o/v1/responses",
			before: []string{
				`{"type":"response.created","response":{"id":"r1"}}`,
				`{"type":"response.reasoning_summary_text.delta","delta":"Looking at the file."}`,
				`{"type":"response.output_text.delta","delta":"Reading it now."}`,
			},
			after: []string{
				`{"type":"response.function_call_arguments.delta","delta":"{\"path\":\"/a/b.go\"}"}`,
				`{"type":"response.completed","response":{"usage":{"input_tokens":1200,"input_tokens_details":{"cached_tokens":1000},"output_tokens":80,"output_tokens_details":{"reasoning_tokens":40}}}}`,
			},
			output: 80, input: 1200, read: 1000, reasoning: 40},
		{name: "openai chat", path: "/api/llm/o/v1/chat/completions",
			before: []string{
				`{"choices":[{"delta":{"reasoning_content":"Looking at the file."}}]}`,
				`{"choices":[{"delta":{"content":"Reading it now."}}]}`,
			},
			after: []string{
				`{"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"{\"path\":\"/a/b.go\"}"}}]}}]}`,
				`{"choices":[],"usage":{"prompt_tokens":1200,"completion_tokens":70,"prompt_tokens_details":{"cached_tokens":1000},"completion_tokens_details":{"reasoning_tokens":30}}}`,
			},
			output: 70, input: 1200, read: 1000, reasoning: 30},
		{name: "gemini", path: "/api/llm/g/v1/models/gemini-test:streamGenerateContent",
			before: []string{
				`{"candidates":[{"content":{"parts":[{"text":"Looking at the file.","thought":true}],"role":"model"}}],"usageMetadata":{"promptTokenCount":1200,"cachedContentTokenCount":1000,"totalTokenCount":1200}}`,
				`{"candidates":[{"content":{"parts":[{"text":"Reading it now."}],"role":"model"}}],"usageMetadata":{"promptTokenCount":1200,"cachedContentTokenCount":1000,"totalTokenCount":1200}}`,
			},
			after: []string{
				`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"Read","args":{"file_path":"/a/b.go"}}}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1200,"candidatesTokenCount":30,"cachedContentTokenCount":1000,"thoughtsTokenCount":50,"totalTokenCount":1280}}`,
			},
			output: 80, input: 1200, read: 1000, reasoning: 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				flush := w.(http.Flusher).Flush
				for _, f := range tc.before {
					w.Write([]byte("data: " + f + "\n\n"))
				}
				w.Write([]byte(": gate\n\n"))
				flush()
				<-release
				// Long enough after the first token for the call to have a speed.
				time.Sleep(150 * time.Millisecond)
				for _, f := range tc.after {
					w.Write([]byte("data: " + f + "\n\n"))
				}
			}))
			defer upstream.Close()
			defer free()
			px, baseURL := startPremTestProxy(t, upstream.URL)
			session := "s-" + strings.ReplaceAll(tc.name, " ", "-")

			req, _ := http.NewRequest("POST", baseURL+tc.path, strings.NewReader(`{"model":"m-test","stream":true}`))
			req.Header.Set("Authorization", "Bearer fk-pool")
			req.Header.Set("X-Session-Id", session)
			resp, err := http.DefaultTransport.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body := bufio.NewReader(resp.Body)
			for {
				line, err := body.ReadString('\n')
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(line, ": gate") {
					break
				}
			}
			liveOK := func(live []diag.LiveCall) bool {
				return len(live) == 1 && live[0].SessionID == session && !live[0].FirstToken.IsZero() && live[0].Output >= 5
			}
			live := waitLive(px, liveOK)
			if !liveOK(live) {
				t.Fatalf("mid-stream live calls %+v, want this one with its first token and ~8 tokens so far", live)
			}
			if live[0].Usage.Input != 0 && live[0].Usage.Input != tc.input {
				t.Fatalf("mid-stream input %d, want %d once reported", live[0].Usage.Input, tc.input)
			}

			free()
			for {
				if _, err := body.ReadString('\n'); err != nil {
					break
				}
			}
			resp.Body.Close()
			var s diag.SessionCalls
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				var ok bool
				if s, ok = px.Recorder().Session(session); ok && len(px.Live()) == 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if s.Calls != 1 || s.Usage.Output != tc.output || s.Usage.Input != tc.input || s.Usage.CacheRead != tc.read || s.Usage.Reasoning != tc.reasoning {
				t.Fatalf("the Session's sums %+v, want output %d (reasoning %d), input %d, cache read %d", s, tc.output, tc.reasoning, tc.input, tc.read)
			}
			if s.Last.TTFTMs <= 0 || s.TokensPerSecond() <= 0 {
				t.Fatalf("first token after %d ms, speed %v: want both", s.Last.TTFTMs, s.TokensPerSecond())
			}
		})
	}
}
