package proxy

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

func poolUsage(key string, stdPct, corePct float64) *keys.KeyUsage {
	std, core, total := stdPct, corePct, 100.0
	used := stdPct
	if stdPct >= 100 {
		used = corePct
	}
	return &keys.KeyUsage{Key: key, StandardPercent: &std, CorePercent: &core, Total: &total, Used: &used}
}

type switches struct {
	mu   sync.Mutex
	keys []string
	ok   bool
}

func (s *switches) switchKey(key, _ string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = append(s.keys, key)
	return s.ok
}

func (s *switches) got() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.keys...)
}

func startSwitchProxy(t *testing.T, upstream string, sw *switches, usages ...*keys.KeyUsage) *Proxy {
	t.Helper()
	var names []string
	for _, u := range usages {
		names = append(names, u.Key)
	}
	rotator := keys.NewKeyRotator(config.New(), names)
	for _, u := range usages {
		rotator.SetUsageForTest(u)
	}
	px := NewProxy(upstream, rotator, sw.switchKey)
	if _, err := px.Start("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = px.Shutdown(t.Context()) })
	return px
}

func postLLM(t *testing.T, base, key string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/api/llm/a/v1/messages", strings.NewReader(`{"model":"claude-opus-5-5","messages":[]}`))
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

// The proxy never rewrites a credential: the hook decides the key.
func TestProxyKeepsTheKeyDroidSent(t *testing.T) {
	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		io.WriteString(w, `{"usage":{"input_tokens":3,"output_tokens":2}}`)
	}))
	defer upstream.Close()
	sw := &switches{ok: true}
	px := startSwitchProxy(t, upstream.URL, sw, poolUsage("fk-spent", 99.5, 99.5), poolUsage("fk-fresh", 0, 0))

	resp, _ := postLLM(t, px.BaseURL(), "fk-spent")
	if resp.StatusCode != 200 || len(seen) != 1 || seen[0] != "Bearer fk-spent" {
		t.Fatalf("status %d, upstream saw %v", resp.StatusCode, seen)
	}
	// The next requests get the key with room.
	if got := sw.got(); len(got) != 1 || got[0] != "fk-fresh" {
		t.Fatalf("switched to %v", got)
	}
	recs := px.Recorder().Requests()
	if len(recs) != 1 || recs[0].Key != "fk-spent" || recs[0].Usage == nil {
		t.Fatalf("record = %+v", recs)
	}
}

// A 402 droid would not retry becomes a 429 it does, once the key file names
// another key.
func TestProxyTurnsAnExhaustedKeyIntoARetry(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer fk-a" {
			w.WriteHeader(http.StatusPaymentRequired)
			io.WriteString(w, `{"detail":"You've reached your 5-hour standard usage limit"}`)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	sw := &switches{ok: true}
	px := startSwitchProxy(t, upstream.URL, sw, poolUsage("fk-a", 10, 10), poolUsage("fk-b", 20, 20))

	resp, body := postLLM(t, px.BaseURL(), "fk-a")
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After-Ms") == "" {
		t.Fatalf("status %d %q", resp.StatusCode, body)
	}
	if got := sw.got(); len(got) == 0 || got[len(got)-1] != "fk-b" {
		t.Fatalf("switched to %v", got)
	}
	if resp, body := postLLM(t, px.BaseURL(), "fk-b"); resp.StatusCode != 200 || body != "ok" {
		t.Fatalf("retry: %d %q", resp.StatusCode, body)
	}
}

// Without another key, or in manual mode, droid sees the upstream's answer.
func TestProxyPassesTheErrorThroughWhenItCannotSwitch(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		io.WriteString(w, `{"detail":"limit"}`)
	}))
	defer upstream.Close()
	for name, sw := range map[string]*switches{"manual": {ok: false}, "alone": {ok: true}} {
		t.Run(name, func(t *testing.T) {
			usages := []*keys.KeyUsage{poolUsage("fk-a", 10, 10)}
			if name == "manual" {
				usages = append(usages, poolUsage("fk-b", 10, 10))
			}
			px := startSwitchProxy(t, upstream.URL, sw, usages...)
			resp, body := postLLM(t, px.BaseURL(), "fk-a")
			if resp.StatusCode != http.StatusPaymentRequired || !strings.Contains(body, "limit") {
				t.Fatalf("status %d %q", resp.StatusCode, body)
			}
		})
	}
}

// An LLM request without a key of the pool, such as one with droid's login
// when the hook did not run, never reaches Factory: it would bill the
// signed-in account. A BYOK model's and every other request pass.
func TestProxyStopsAnLLMRequestWithoutAPoolKey(t *testing.T) {
	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		io.WriteString(w, `{}`)
	}))
	defer upstream.Close()
	px := startSwitchProxy(t, upstream.URL, &switches{ok: true}, poolUsage("fk-pool", 0, 0))
	for _, key := range []string{"login-jwt", "fk-removed", ""} {
		resp, body := postLLM(t, px.BaseURL(), key)
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "Key Pool key") {
			t.Fatalf("%q: %d %s", key, resp.StatusCode, body)
		}
	}
	get := func(path string) int {
		req, _ := http.NewRequest("GET", px.BaseURL()+path, nil)
		req.Header.Set("Authorization", "Bearer login-jwt")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if s := get("/api/llm/decision/route"); s != http.StatusBadRequest {
		t.Fatalf("an LLM GET passed: %d", s)
	}
	if s := get("/api/cli/whoami"); s != 200 {
		t.Fatalf("whoami: %d", s)
	}
	req, _ := http.NewRequest("POST", px.BaseURL()+"/api/llm/custom/v1/messages", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer byok-key")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 200 {
		t.Fatalf("a BYOK request: %v %v", resp, err)
	}
	if want := []string{"GET /api/cli/whoami", "POST /api/llm/custom/v1/messages"}; strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("upstream saw %v", seen)
	}
	if resp, _ := postLLM(t, px.BaseURL(), "fk-pool"); resp.StatusCode != 200 {
		t.Fatalf("a pool key: %d", resp.StatusCode)
	}
}

// droid reads a 401 as its login having expired. When Factory rejects a pool
// key, droid retries on another key, or sees why from Droi.
func TestProxyAnswersARejectedPoolKey(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer fk-a" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"detail":"Invalid API key"}`)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	sw := &switches{ok: true}
	px := startSwitchProxy(t, upstream.URL, sw, poolUsage("fk-a", 10, 10), poolUsage("fk-b", 20, 20))
	resp, body := postLLM(t, px.BaseURL(), "fk-a")
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After-Ms") == "" {
		t.Fatalf("status %d %q", resp.StatusCode, body)
	}
	if got := sw.got(); len(got) == 0 || got[len(got)-1] != "fk-b" {
		t.Fatalf("switched to %v", got)
	}
	for name, sw := range map[string]*switches{"manual": {ok: false}, "alone": {ok: true}} {
		t.Run(name, func(t *testing.T) {
			usages := []*keys.KeyUsage{poolUsage("fk-a", 10, 10)}
			if name == "manual" {
				usages = append(usages, poolUsage("fk-b", 10, 10))
			}
			px := startSwitchProxy(t, upstream.URL, sw, usages...)
			resp, body := postLLM(t, px.BaseURL(), "fk-a")
			if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "fk-a") || !strings.Contains(body, "login is fine") {
				t.Fatalf("status %d %q", resp.StatusCode, body)
			}
		})
	}
}

// Factory answers 200 and then ends the stream with a failure frame: the
// record says so, and droid gets the stream as it came.
func TestProxyRecordsAStreamThatFailed(t *testing.T) {
	stream := `data: {"type":"response.created","response":{"output":[]}}` + "\n\n" +
		`data: {"type":"response.failed","response":{"status":"failed","error":null}}` + "\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, stream)
	}))
	defer upstream.Close()
	px := startSwitchProxy(t, upstream.URL, &switches{ok: true}, poolUsage("fk-a", 0, 0))
	resp, body := postLLM(t, px.BaseURL(), "fk-a")
	if resp.StatusCode != 200 || body != stream {
		t.Fatalf("status %d %q", resp.StatusCode, body)
	}
	recs := px.Recorder().Requests()
	if len(recs) != 1 || recs[0].Error != "stream_failed" || recs[0].ErrorDetail != "response.failed" {
		t.Fatalf("record = %+v", recs)
	}
}

// Droi's own request headers say which of Droi's parts a request came from
// (X-Droi-Call: model.complete, say). They never reach an upstream: Factory
// and every other provider see a client's request, not Droi's. A request that
// carries one is forwarded as it would be without it.
func TestProxy_StripsDroisOwnRequestHeaders(t *testing.T) {
	var mu sync.Mutex
	var seen []http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Clone())
		mu.Unlock()
		io.WriteString(w, `{"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()
	px := startSwitchProxy(t, upstream.URL, &switches{ok: true}, poolUsage("fk-a", 0, 0))

	for _, tc := range []struct {
		method, path, body string
	}{
		{"POST", "/api/llm/o/v1/chat/completions", `{"model":"deepseek-v4.1-flash-1017","messages":[{"role":"user","content":"hi"}]}`},
		{"POST", "/api/llm/a/v1/messages", `{"model":"claude-opus-5-5","messages":[{"role":"user","content":"hi"}]}`},
		{"GET", "/api/cli/whoami", ""},
	} {
		req, err := http.NewRequest(tc.method, px.BaseURL()+tc.path, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer fk-a")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Droi-Call", "model.complete")
		req.Header.Set("X-Droi-Session", "s-1")
		req.Header.Set("x-droi-lower", "still dropped")
		// A header that is not Droi's goes on, so the rule is Droi's prefix
		// and nothing wider.
		req.Header.Set("X-Message-Call-Type", "session-title-generation")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 {
		t.Fatalf("the upstream saw %d requests, want 3", len(seen))
	}
	for i, h := range seen {
		for name := range h {
			if strings.HasPrefix(strings.ToLower(name), "x-droi-") {
				t.Fatalf("request %d carried %s: %v", i, name, h)
			}
		}
		if h.Get("X-Message-Call-Type") != "session-title-generation" {
			t.Fatalf("request %d lost the headers that are not Droi's: %v", i, h)
		}
	}
}

// A client that leaves once it has the last frame ends the copy: Go cancels
// the request's context, the read of the upstream body fails with
// context.Canceled, and the record must not call that a failed request. The
// Kernel closes a stream exactly so, after Factory's [DONE].
func TestProxy_DoesNotRecordAClientThatHungUp(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range []string{
			`{"choices":[{"delta":{"content":"hello"}}]}`,
			`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2}}`,
			`[DONE]`,
		} {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
		}
		w.(http.Flusher).Flush()
		// Factory keeps the connection until the client leaves.
		<-release
	}))
	defer upstream.Close()
	defer free()
	px := startSwitchProxy(t, upstream.URL, &switches{ok: true}, poolUsage("fk-a", 0, 0))

	req, _ := http.NewRequest("POST", px.BaseURL()+"/api/llm/a/v1/messages", strings.NewReader(`{"model":"m-test","stream":true}`))
	req.Header.Set("Authorization", "Bearer fk-a")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	body := bufio.NewReader(resp.Body)
	var got []string
	for {
		line, err := body.ReadString('\n')
		if err != nil {
			t.Fatalf("the stream ended before [DONE]: %v, frames %v", err, got)
		}
		if strings.HasPrefix(line, "data: ") {
			got = append(got, strings.TrimSpace(strings.TrimPrefix(line, "data: ")))
			if strings.Contains(line, "[DONE]") {
				break
			}
		}
	}
	resp.Body.Close() // the client leaves, as the Kernel does

	if want := 3; len(got) != want {
		t.Fatalf("read %d frames, want %d: %v", len(got), want, got)
	}
	rec := waitRecord(t, px)
	if rec.Error != "" || rec.ErrorDetail != "" {
		t.Fatalf("a client that left was recorded as a failure: %q %q", rec.Error, rec.ErrorDetail)
	}
	if rec.Bytes == 0 || rec.Usage == nil || rec.Usage.Output != 2 {
		t.Fatalf("the record lost what the stream carried: %+v", rec)
	}
}

// A client that stops in the middle of a stream leaves on purpose (the user
// pressed stop): the copy ends the same way, and the record stays clean.
func TestProxy_DoesNotRecordAClientThatStopsHalfway(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"first"}}]}` + "\n\n"))
		w.(http.Flusher).Flush()
		<-release
	}))
	defer upstream.Close()
	defer free()
	px := startSwitchProxy(t, upstream.URL, &switches{ok: true}, poolUsage("fk-a", 0, 0))

	req, _ := http.NewRequest("POST", px.BaseURL()+"/api/llm/a/v1/messages", strings.NewReader(`{"model":"m-test","stream":true}`))
	req.Header.Set("Authorization", "Bearer fk-a")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(resp.Body).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if rec := waitRecord(t, px); rec.Error != "" || rec.ErrorDetail != "" {
		t.Fatalf("a client that stopped was recorded as a failure: %q %q", rec.Error, rec.ErrorDetail)
	}
}

// A stream the upstream cuts short did lose bytes, and the record must say
// so, with what happened.
func TestProxy_RecordsAStreamTheUpstreamCutShort(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"half"}}]}` + "\n\n"))
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler) // the connection drops mid-stream
	}))
	defer upstream.Close()
	px := startSwitchProxy(t, upstream.URL, &switches{ok: true}, poolUsage("fk-a", 0, 0))

	req, _ := http.NewRequest("POST", px.BaseURL()+"/api/llm/a/v1/messages", strings.NewReader(`{"model":"m-test","stream":true}`))
	req.Header.Set("Authorization", "Bearer fk-a")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	rec := waitRecord(t, px)
	if rec.Error != "response_copy_error" {
		t.Fatalf("error %q, want response_copy_error", rec.Error)
	}
	if rec.ErrorDetail == "" {
		t.Fatal("the record should say what the copy met")
	}
}

// waitRecord answers the one request the proxy recorded, once it has.
func waitRecord(t *testing.T, px *Proxy) diag.RequestRecord {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if recs := px.Recorder().Requests(); len(recs) == 1 {
			return recs[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the proxy recorded %d requests, want 1", len(px.Recorder().Requests()))
	return diag.RequestRecord{}
}
