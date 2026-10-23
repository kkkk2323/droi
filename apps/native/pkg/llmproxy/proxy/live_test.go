package proxy

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// While a reply streams, the proxy shows it as a live call of its Session,
// with its first token and the tokens so far; once it ends, the Session's
// sums have it, with its time to the first token.
func TestProxy_ShowsAStreamingCallLiveThenSumsIt(t *testing.T) {
	release := make(chan struct{})
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
	px, baseURL := startPremTestProxy(t, upstream.URL)

	req, _ := http.NewRequest("POST", baseURL+"/api/llm/a/v1/messages", strings.NewReader(`{"model":"claude-test","stream":true}`))
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
	live := px.Live()
	if len(live) != 1 {
		t.Fatalf("live calls %+v, want the one streaming", live)
	}
	l := live[0]
	if l.SessionID != "s-live" || l.Model != "claude-test" || l.FirstToken.IsZero() || l.Output != 3 || l.Usage.Input != 100 {
		t.Fatalf("live call %+v", l)
	}

	close(release)
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
