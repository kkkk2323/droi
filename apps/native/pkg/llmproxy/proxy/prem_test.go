package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

func startPremTestProxy(t *testing.T, upstreamURL string, apiKeys ...string) (*Proxy, string) {
	t.Helper()
	if len(apiKeys) == 0 {
		apiKeys = []string{"fk-pool"}
	}
	rotator := keys.NewKeyRotator(config.New(), apiKeys)
	for _, k := range apiKeys {
		rotator.SetUsageForTest(poolUsage(k, 0, 0))
	}
	px := NewProxy(upstreamURL, rotator, nil)
	baseURL, err := px.Start("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = px.Shutdown(context.Background()) })
	return px, baseURL
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer cli-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestPrem_DefaultUpstreamBeforeWhoami(t *testing.T) {
	px, _ := startPremTestProxy(t, "http://127.0.0.1:1")
	if got := px.PremUpstream(); got != defaultPremUpstream {
		t.Fatalf("PremUpstream() = %q, want %q", got, defaultPremUpstream)
	}
}

func TestPrem_WhoamiPremHostRewrittenToProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"userId":"u1","orgId":"o1","inferenceRegion":"global","premBaseHostV2":"https://prem.example","isEnterprise":false}`)
	}))
	defer upstream.Close()
	px, baseURL := startPremTestProxy(t, upstream.URL)

	var who map[string]any
	if err := json.Unmarshal([]byte(getBody(t, baseURL+"/api/cli/whoami")), &who); err != nil {
		t.Fatalf("rewritten whoami is not valid JSON: %v", err)
	}
	if got, want := who["premBaseHostV2"], baseURL+premPathPrefix; got != want {
		t.Fatalf("premBaseHostV2 = %v, want %v", got, want)
	}
	if who["orgId"] != "o1" || who["inferenceRegion"] != "global" || who["isEnterprise"] != false {
		t.Fatalf("other whoami fields changed: %v", who)
	}
	if got := px.PremUpstream(); got != "https://prem.example" {
		t.Fatalf("PremUpstream() = %q, want learned host", got)
	}
}

// The identity channel frame carries whoami as JSON text inside a string.
func TestPrem_EscapedPremHostInChannelFrameRewritten(t *testing.T) {
	inner := `{"outcome":"served","whoami":{"userId":"u1","orgId":"o1","premBaseHostV2":"https://prem.example"}}`
	frame, _ := json.Marshal(map[string]any{"channel": "identity", "index": 0, "data": inner, "final": true})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write(append(frame, '\n'))
	}))
	defer upstream.Close()
	px, baseURL := startPremTestProxy(t, upstream.URL)

	var got struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(getBody(t, baseURL+"/api/anything"))), &got); err != nil {
		t.Fatalf("frame is not valid JSON: %v", err)
	}
	var payload struct {
		Whoami map[string]any `json:"whoami"`
	}
	if err := json.Unmarshal([]byte(got.Data), &payload); err != nil {
		t.Fatalf("embedded data is not valid JSON: %v", err)
	}
	if want := baseURL + premPathPrefix; payload.Whoami["premBaseHostV2"] != want {
		t.Fatalf("embedded premBaseHostV2 = %v, want %v", payload.Whoami["premBaseHostV2"], want)
	}
	if px.PremUpstream() != "https://prem.example" {
		t.Fatalf("PremUpstream() = %q", px.PremUpstream())
	}
}

func TestPrem_UnrelatedContentNotRewritten(t *testing.T) {
	const body = `{"note":"see https://prem.factory.ai","premBaseHost":"https://legacy.example"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, body)
	}))
	defer upstream.Close()
	_, baseURL := startPremTestProxy(t, upstream.URL)
	if got := getBody(t, baseURL+"/api/cli/whoami"); got != body {
		t.Fatalf("body changed:\n got %s\nwant %s", got, body)
	}
}

func TestPrem_LLMResponseNotRewritten(t *testing.T) {
	const body = `{"content":[{"type":"text","text":"\"premBaseHostV2\":\"https://prem.example\""}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}))
	defer upstream.Close()
	_, baseURL := startPremTestProxy(t, upstream.URL)

	req, _ := http.NewRequest("POST", baseURL+"/api/llm/a/v1/messages", strings.NewReader(`{"model":"claude-fable-5.1","messages":[]}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if string(got) != body {
		t.Fatalf("LLM body changed: %s", got)
	}
}

func TestPrem_PrefixRoutesLLMToPremWithTheKeyDroidSent(t *testing.T) {
	var premHits []string
	prem := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		premHits = append(premHits, r.URL.Path+" "+r.Header.Get("Authorization"))
		io.WriteString(w, "ok")
	}))
	defer prem.Close()
	hosted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/whoami" {
			io.WriteString(w, `{"userId":"u1","orgId":"o1","premBaseHostV2":"`+prem.URL+`"}`)
			return
		}
		t.Errorf("LLM request reached the hosted upstream: %s", r.URL.Path)
	}))
	defer hosted.Close()

	px, baseURL := startPremTestProxy(t, hosted.URL, "fk-a")
	getBody(t, baseURL+"/api/cli/whoami") // learn the Prem host

	req, _ := http.NewRequest("POST", baseURL+premPathPrefix+"/api/llm/a/v1/messages",
		strings.NewReader(`{"model":"claude-fable-5.1","messages":[]}`))
	req.Header.Set("Authorization", "Bearer fk-a")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
	if len(premHits) != 1 || premHits[0] != "/api/llm/a/v1/messages Bearer fk-a" {
		t.Fatalf("prem hits = %v", premHits)
	}
	recs := px.Recorder().Requests()
	if len(recs) != 1 || recs[0].Upstream != "prem" || recs[0].Key != "fk-a" {
		t.Fatalf("request record = %+v", recs)
	}
}
func TestPrem_NonLLMPremRequestKeepsClientToken(t *testing.T) {
	var gotPath, gotAuth string
	prem := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.RequestURI(), r.Header.Get("Authorization")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer prem.Close()
	hosted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"premBaseHostV2":"`+prem.URL+`"}`)
	}))
	defer hosted.Close()

	_, baseURL := startPremTestProxy(t, hosted.URL)
	getBody(t, baseURL+"/api/cli/whoami")
	getBody(t, baseURL+premPathPrefix+"/api/apps/cloud-sync/sessions/s1/messages?limit=5")

	if gotPath != "/api/apps/cloud-sync/sessions/s1/messages?limit=5" {
		t.Fatalf("prem path = %q", gotPath)
	}
	if gotAuth != "Bearer cli-token" {
		t.Fatalf("session sync auth = %q, want the client's own token", gotAuth)
	}
}

// A streamed non-LLM response must reach the client line by line, not only
// once the upstream finishes.
func TestPrem_StreamedLinesForwardedIncrementally(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		io.WriteString(w, `{"channel":"identity","data":"{\"premBaseHostV2\":\"https://prem.example\"}"}`+"\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
		io.WriteString(w, `{"channel":"featureFlags","final":true}`+"\n")
	}))
	defer upstream.Close()
	_, baseURL := startPremTestProxy(t, upstream.URL)

	resp, err := http.Get(baseURL + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)

	lineCh := make(chan string, 1)
	go func() {
		line, _ := br.ReadString('\n')
		lineCh <- line
	}()
	select {
	case line := <-lineCh:
		if !strings.Contains(line, baseURL+premPathPrefix) {
			t.Fatalf("first line not rewritten: %s", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first line was held back until the stream ended")
	}
	close(release)
	if rest, _ := io.ReadAll(br); !strings.Contains(string(rest), "featureFlags") {
		t.Fatalf("second line missing: %q", rest)
	}
}
