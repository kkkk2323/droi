package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
)

// parityCase is one request the Kernel sends (factory/testdata/parity/requests.mjs) and what upstream's plugin sends
// Factory for it (upstream.mjs): its first request, and the one it sends again when Factory answers that with a 413.
type parityCase struct {
	Name    string            `json:"name"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	Sent    struct {
		Path    string            `json:"path"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	} `json:"sent"`
	Resent *struct {
		Path string `json:"path"`
		Body string `json:"body"`
	} `json:"resent"`
}

// parityUnlike are the headers upstream's request has and the proxy's has not, or with another value, by design:
// Droi's own headers never leave the proxy, the bearer is a Key Pool key, and Go's client asks for compression.
var parityUnlike = map[string]bool{"x-droi-conversation": true, "authorization": true, "x-session-id": true, "x-assistant-message-id": true, "content-length": true, "accept-encoding": true}

// Each request pi-ai sends for the models the Host registers, and a few written by hand, goes to Factory as
// upstream's plugin sends it (magpie-community/plugins, packages/factory, at the commit PORTED-FROM.md names), signed
// in with a key as the Key Pool is: the same path, the same headers, the same body byte for byte. A 413 is asked again
// as upstream asks it, or not at all where upstream does not.
func TestKernelRequestsGoOutAsUpstreamSendsThem(t *testing.T) {
	raw, err := os.ReadFile("../factory/testdata/parity/fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []parityCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	f := newFakeFactory(t)
	var mu sync.Mutex
	tooLarge := false
	f.replyTo = func(string, []byte) (int, string) {
		mu.Lock()
		defer mu.Unlock()
		if tooLarge {
			tooLarge = false
			return http.StatusRequestEntityTooLarge, `{"error":{"message":"Request Entity Too Large"}}`
		}
		return http.StatusOK, `{"id":"ok"}`
	}
	px := startKernelProxy(t, f.upstream.URL, f, &switches{ok: true}, poolUsage("fk-a", 0, 0))
	send := func(t *testing.T, c parityCase, large bool) []*http.Request {
		t.Helper()
		mu.Lock()
		tooLarge = large
		mu.Unlock()
		before := len(f.requests())
		req, _ := http.NewRequest("POST", px.BaseURL()+c.Path, strings.NewReader(c.Body))
		for k, v := range c.Headers {
			req.Header.Set(k, strings.ReplaceAll(v, "KTOKEN", kernelToken))
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return f.requests()[before:]
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			seen := send(t, c, false)
			if len(seen) != 1 {
				t.Fatalf("Factory saw %d requests", len(seen))
			}
			r := seen[0]
			if got := pathOf(r); got != c.Sent.Path {
				t.Errorf("went to %s, upstream to %s", got, c.Sent.Path)
			}
			if r.Header.Get("Authorization") != "Bearer fk-a" {
				t.Errorf("went out with %q", r.Header.Get("Authorization"))
			}
			got := map[string]string{}
			for k := range r.Header {
				got[strings.ToLower(k)] = strings.Join(r.Header.Values(k), ", ")
			}
			for _, h := range []map[string]string{c.Sent.Headers, got} {
				for k := range h {
					if !parityUnlike[k] && got[k] != c.Sent.Headers[k] {
						t.Errorf("%s: %q, upstream %q", k, got[k], c.Sent.Headers[k])
					}
				}
			}
			if body := readBody(t, r); body != c.Sent.Body {
				t.Errorf("the body differs from upstream's:\n got %s\nwant %s", body, c.Sent.Body)
			}

			seen = send(t, c, true)
			switch {
			case c.Resent == nil && len(seen) != 1:
				t.Errorf("asked again on a 413 where upstream does not")
			case c.Resent != nil && len(seen) != 2:
				t.Errorf("Factory saw %d requests for a 413 upstream asks again", len(seen))
			case c.Resent != nil:
				if got := pathOf(seen[1]); got != c.Resent.Path {
					t.Errorf("asked again at %s, upstream at %s", got, c.Resent.Path)
				}
				if body := readBody(t, seen[1]); body != c.Resent.Body {
					t.Errorf("asked again with another body than upstream's:\n got %s\nwant %s", body, c.Resent.Body)
				}
			}
		})
	}
}

func pathOf(r *http.Request) string {
	if r.URL.RawQuery != "" {
		return r.URL.Path + "?" + r.URL.RawQuery
	}
	return r.URL.Path
}

func readBody(t *testing.T, r *http.Request) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
