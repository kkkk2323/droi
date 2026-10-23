package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
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

// The proxy never rewrites a credential: the patch decides the key.
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
