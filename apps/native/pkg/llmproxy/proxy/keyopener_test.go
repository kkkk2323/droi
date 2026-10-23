package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

const haikuModel = "claude-haiku-4-5-20251001"

func monthPoolUsage(key string, used float64, end string, checked time.Time) *keys.KeyUsage {
	u := poolUsage(key, used, 0)
	u.LastCheckedAt = checked
	u.StandardLimits = &keys.PoolLimits{Monthly: &keys.BucketLimit{UsedPercent: used, WindowEnd: end}}
	return u
}

type seenRequest struct {
	auth      string
	maxTokens int
}

// openerFixture is a pool of one open key and two waiting, at 10:00, with
// the opener on.
func openerFixture(t *testing.T, status int) (*Proxy, *keys.KeyRotator, *httptest.Server, func() []seenRequest, time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 16, 10, 0, 0, 0, time.Local)
	rotator := keys.NewKeyRotator(config.New(), []string{"fk-open", "fk-q1", "fk-q2"})
	rotator.SetClockForTest(func() time.Time { return now })
	checked := now.Add(-time.Hour)
	rotator.SetUsageForTest(monthPoolUsage("fk-open", 50, now.Add(20*24*time.Hour).Format(time.RFC3339), checked))
	rotator.SetUsageForTest(monthPoolUsage("fk-q1", 0, "", checked))
	rotator.SetUsageForTest(monthPoolUsage("fk-q2", 0, "", checked))

	var mu sync.Mutex
	var seen []seenRequest
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed struct {
			MaxTokens int `json:"max_tokens"`
		}
		_ = json.Unmarshal(body, &parsed)
		mu.Lock()
		seen = append(seen, seenRequest{auth: r.Header.Get("Authorization"), maxTokens: parsed.MaxTokens})
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"usage":{"input_tokens":10,"output_tokens":1}}`))
	}))
	t.Cleanup(upstream.Close)
	px := NewProxy(upstream.URL, rotator, nil)
	px.keyOpener.now = func() time.Time { return now }
	px.SetKeyOpening(true)
	get := func() []seenRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]seenRequest(nil), seen...)
	}
	return px, rotator, upstream, get, now
}

func observeHaiku(px *Proxy, url string) {
	h := http.Header{}
	h.Set("Authorization", "Bearer fk-open")
	h.Set("Content-Type", "application/json")
	px.keyOpener.observe(url, h, "", []byte(`{"model":"`+haikuModel+`","max_tokens":200,"messages":[{"role":"user","content":"hi"}]}`), haikuModel, "anthropic")
}

func TestKeyOpenerOpensQueuedKeysWithOneTokenHaikuRequests(t *testing.T) {
	px, rotator, upstream, seen, now := openerFixture(t, http.StatusOK)
	ctx := context.Background()

	px.keyOpener.tick(ctx)
	if len(seen()) != 0 {
		t.Fatal("opened without a template")
	}
	observeHaiku(px, upstream.URL+"/api/llm/a/v1/messages")
	if st := rotator.OpeningStatus(); !st.Scheduled {
		t.Fatalf("a template makes opening scheduled: %+v", st)
	}

	px.keyOpener.now = func() time.Time { return now.Add(-3 * time.Hour) }
	px.keyOpener.tick(ctx)
	if len(seen()) != 0 {
		t.Fatal("opened before 08:00")
	}
	px.keyOpener.now = func() time.Time { return now }
	for range 3 {
		px.keyOpener.tick(ctx)
	}
	got := seen()
	if len(got) != 2 || got[0].auth != "Bearer fk-q1" || got[1].auth != "Bearer fk-q2" || got[0].maxTokens != 1 || got[1].maxTokens != 1 {
		t.Fatalf("opening requests = %+v", got)
	}
	if st := rotator.OpeningStatus(); st.OpenedToday != 2 || len(st.Queue) != 0 {
		t.Fatalf("status = %+v", st)
	}
	recs := px.Recorder().Requests()
	if len(recs) == 0 || recs[0].Kind != "key_open" || recs[0].Model != haikuModel {
		t.Fatalf("records = %+v", recs)
	}
}

// Off, the opener sends nothing and real requests open months again; the
// template it kept lets it open at once when it is turned on.
func TestKeyOpenerOffLeavesOpeningToRealRequests(t *testing.T) {
	px, rotator, upstream, seen, _ := openerFixture(t, http.StatusOK)
	px.SetKeyOpening(false)
	observeHaiku(px, upstream.URL+"/api/llm/a/v1/messages")
	if st := rotator.OpeningStatus(); st.Scheduled {
		t.Fatalf("an opener that is off plans the openings: %+v", st)
	}
	px.keyOpener.tick(context.Background())
	if len(seen()) != 0 {
		t.Fatal("opened while off")
	}
	px.SetKeyOpening(true)
	if st := rotator.OpeningStatus(); !st.Scheduled {
		t.Fatalf("turned on with a template: %+v", st)
	}
	px.keyOpener.tick(context.Background())
	if got := seen(); len(got) != 1 || got[0].auth != "Bearer fk-q1" {
		t.Fatalf("opening requests = %+v", got)
	}
}

func TestKeyOpenerDropsAFailingTemplate(t *testing.T) {
	px, rotator, upstream, seen, _ := openerFixture(t, http.StatusBadRequest)
	observeHaiku(px, upstream.URL+"/api/llm/a/v1/messages")
	px.keyOpener.tick(context.Background())
	if len(seen()) != 1 || px.keyOpener.template() != nil {
		t.Fatalf("seen %d, template kept %v", len(seen()), px.keyOpener.template() != nil)
	}
	if st := rotator.OpeningStatus(); st.Scheduled || st.OpenedToday != 0 || len(st.Queue) != 2 {
		t.Fatalf("a failed opening is released: %+v", st)
	}
}

func TestProxyKeepsHaikuRequestAsOpeningTemplate(t *testing.T) {
	px, _, _, _, _ := openerFixture(t, http.StatusOK)
	baseURL, err := px.Start("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = px.Shutdown(context.Background()) })
	for _, model := range []string{"claude-opus-5-5", haikuModel} {
		req, _ := http.NewRequest("POST", baseURL+"/api/llm/a/v1/messages", strings.NewReader(`{"model":"`+model+`","max_tokens":50,"messages":[]}`))
		req.Header.Set("Authorization", "Bearer fk-open")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if tmpl := px.keyOpener.template(); (tmpl != nil) != (model == haikuModel) {
			t.Fatalf("after %s template = %+v", model, tmpl)
		}
	}
	if auth := px.keyOpener.template().header.Get("Authorization"); auth != "" {
		t.Fatalf("the template keeps no key: %q", auth)
	}
}
