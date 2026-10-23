package keypool

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const usageBody = `{"usesTokenRateLimitsBilling": true, "limits": {
	"standard": {"fiveHour": {"usedPercent": 10, "windowEnd": "2099-06-19T15:00:00Z"}},
	"core": {"fiveHour": {"usedPercent": 5, "windowEnd": "2099-06-19T15:00:00Z"}}}}`

type factory struct {
	mu   sync.Mutex
	seen []string
	*httptest.Server
}

func (f *factory) auth() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seen...)
}

// newFactory answers billing for every key but fk-bad, and LLM requests
// with 402 for fk-spent.
func newFactory(t *testing.T) *factory {
	f := &factory{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		switch {
		case strings.HasPrefix(r.URL.Path, "/billing"):
			if strings.Contains(auth, "fk-bad") {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			io.WriteString(w, usageBody)
		case strings.HasPrefix(r.URL.Path, "/api/llm/"):
			f.mu.Lock()
			f.seen = append(f.seen, auth)
			f.mu.Unlock()
			if auth == "Bearer fk-spent-0000000000" {
				w.WriteHeader(http.StatusPaymentRequired)
				io.WriteString(w, `{"detail":"limit"}`)
				return
			}
			io.WriteString(w, `{"usage":{"input_tokens":1,"output_tokens":1}}`)
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func openTestPool(t *testing.T, f *factory) *Pool {
	t.Helper()
	p := Open(Options{Dir: filepath.Join(t.TempDir(), "llm"), Upstream: f.URL, UsageURL: f.URL + "/billing"})
	t.Cleanup(p.Stop)
	return p
}

func TestAddChecksTheKey(t *testing.T) {
	p := openTestPool(t, newFactory(t))
	ctx := context.Background()
	if err := p.Add(ctx, "sk-not-factory"); err == nil {
		t.Fatal("accepted a key that is not a Factory key")
	}
	if err := p.Add(ctx, "fk-bad-0000000000"); err == nil {
		t.Fatal("accepted a key Factory rejects")
	}
	if err := p.Add(ctx, "fk-good-0000000000"); err != nil {
		t.Fatal(err)
	}
	if err := p.Add(ctx, "fk-good-0000000000"); err == nil {
		t.Fatal("accepted the same key twice")
	}
	info, err := os.Stat(p.poolFile())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("keys.json: %v %v", info, err)
	}
	b, _ := os.ReadFile(p.poolFile())
	if !strings.Contains(string(b), "fk-good-0000000000") {
		t.Fatalf("keys.json = %s", b)
	}
	reopened := Open(Options{Dir: p.opts.Dir})
	if reopened.Len() != 1 {
		t.Fatalf("reopened pool has %d keys", reopened.Len())
	}
	if st := reopened.State(); strings.Contains(st.Keys[0].Masked, "0000000000") {
		t.Fatalf("State shows the key: %+v", st.Keys[0])
	}
}

func TestStartWritesTheKeyFileAndForwards(t *testing.T) {
	f := newFactory(t)
	p := openTestPool(t, f)
	if _, err := p.Start(); err != ErrNoKeys {
		t.Fatalf("empty pool started: %v", err)
	}
	ctx := context.Background()
	for _, k := range []string{"fk-spent-0000000000", "fk-fresh-0000000000"} {
		if err := p.Add(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	base, err := p.Start()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p.KeyFile())
	info, _ := os.Stat(p.KeyFile())
	if err != nil || string(b) != "fk-spent-0000000000" || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file %q %v %v", b, info.Mode(), err)
	}

	post := func(key string) int {
		req, _ := http.NewRequest("POST", base+"/api/llm/a/v1/messages", strings.NewReader(`{"model":"claude-opus-5-5"}`))
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := post("fk-spent-0000000000"); got != http.StatusTooManyRequests {
		t.Fatalf("exhausted key answered %d", got)
	}
	b, _ = os.ReadFile(p.KeyFile())
	if string(b) != "fk-fresh-0000000000" {
		t.Fatalf("key file after the 402 = %q", b)
	}
	if got := post("fk-fresh-0000000000"); got != 200 {
		t.Fatalf("retry answered %d", got)
	}
	if seen := f.auth(); len(seen) != 2 || seen[0] != "Bearer fk-spent-0000000000" || seen[1] != "Bearer fk-fresh-0000000000" {
		t.Fatalf("Factory saw %v", seen)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		page, err := p.Requests(10, "")
		if err == nil && len(page.Requests) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("request history: %+v %v", page, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st := p.State(); !st.Running || st.Keys[1].ID != st.CurrentID {
		t.Fatalf("state = %+v", st)
	}
}

func TestManualModeKeepsTheKey(t *testing.T) {
	f := newFactory(t)
	p := openTestPool(t, f)
	ctx := context.Background()
	for _, k := range []string{"fk-spent-0000000000", "fk-fresh-0000000000"} {
		if err := p.Add(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.SetMode(Manual); err != nil {
		t.Fatal(err)
	}
	base, err := p.Start()
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", base+"/api/llm/a/v1/messages", strings.NewReader(`{"model":"claude-opus-5-5"}`))
	req.Header.Set("Authorization", "Bearer fk-spent-0000000000")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("manual mode answered %d", resp.StatusCode)
	}
	if b, _ := os.ReadFile(p.KeyFile()); string(b) != "fk-spent-0000000000" {
		t.Fatalf("key file = %q", b)
	}
	if err := p.Use(p.State().Keys[1].ID); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p.KeyFile()); string(b) != "fk-fresh-0000000000" {
		t.Fatalf("key file after Use = %q", b)
	}
	if err := p.Remove(p.State().Keys[1].ID); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p.KeyFile()); string(b) != "fk-spent-0000000000" {
		t.Fatalf("key file after removing the current key = %q", b)
	}
	if err := p.Remove(p.State().Keys[0].ID); err == nil {
		t.Fatal("removed the last key while running")
	}
}
