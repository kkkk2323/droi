package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
	"github.com/klauspost/compress/zstd"
)

// droidAcceptEncoding is what droid, running on Bun, asks for.
const droidAcceptEncoding = "gzip, deflate, br, zstd"

func brotliBytes(t *testing.T, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := brotli.NewWriter(&buf)
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gzipBytes(t *testing.T, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zstdBytes(t *testing.T, plain []byte) []byte {
	t.Helper()
	w, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	return w.EncodeAll(plain, nil)
}

// startProxy runs a proxy in front of upstream and returns its base URL.
func startProxy(t *testing.T, upstream string) string {
	t.Helper()
	px := NewProxy(upstream, &keys.KeyRotator{}, nil)
	baseURL, err := px.Start("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { px.Shutdown(context.Background()) })
	return baseURL
}

func TestProxy_ForwardsClientAcceptEncoding(t *testing.T) {
	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Accept-Encoding")
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	req, _ := http.NewRequest("GET", startProxy(t, upstream.URL)+"/api/test", nil)
	req.Header.Set("Accept-Encoding", droidAcceptEncoding)
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got != droidAcceptEncoding {
		t.Fatalf("upstream saw Accept-Encoding %q, want %q", got, droidAcceptEncoding)
	}
}

func TestProxy_DecodesCompressedResponses(t *testing.T) {
	plain := []byte(`{"ok":true,"note":"a body long enough to compress"}`)
	for _, tc := range []struct {
		encoding string
		body     func(*testing.T, []byte) []byte
	}{
		{"gzip", gzipBytes},
		{"br", brotliBytes},
		{"zstd", zstdBytes},
	} {
		t.Run(tc.encoding, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Encoding", tc.encoding)
				w.Header().Set("Content-Type", "application/json")
				w.Write(tc.body(t, plain))
			}))
			defer upstream.Close()

			req, _ := http.NewRequest("GET", startProxy(t, upstream.URL)+"/api/test", nil)
			req.Header.Set("Accept-Encoding", droidAcceptEncoding)
			resp, err := http.DefaultTransport.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if ce := resp.Header.Get("Content-Encoding"); ce != "" {
				t.Fatalf("decoded body should carry no Content-Encoding, got %q", ce)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(body, plain) {
				t.Fatalf("body = %q, want %q", body, plain)
			}
		})
	}
}

// An encoding the proxy cannot decode must reach the client as the upstream
// sent it, with its Content-Encoding intact.
func TestProxy_ForwardsUnknownEncodingUntouched(t *testing.T) {
	wire := []byte("opaque-bytes")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "exotic")
		w.Write(wire)
	}))
	defer upstream.Close()

	req, _ := http.NewRequest("GET", startProxy(t, upstream.URL)+"/api/test", nil)
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ce := resp.Header.Get("Content-Encoding"); ce != "exotic" {
		t.Fatalf("Content-Encoding = %q, want exotic", ce)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(body, wire) {
		t.Fatalf("body = %q, want %q", body, wire)
	}
}

// The prem rewrite reads the response body, so it must still fire when the
// upstream compresses it; without decoding, droid would learn the real prem
// host and send its LLM traffic straight there.
func TestProxy_RewritesPremHostInCompressedResponse(t *testing.T) {
	whoami := []byte(`{"premBaseHostV2":"https://prem.example"}`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "br")
		w.Header().Set("Content-Type", "application/json")
		w.Write(brotliBytes(t, whoami))
	}))
	defer upstream.Close()

	baseURL := startProxy(t, upstream.URL)
	req, _ := http.NewRequest("GET", baseURL+"/api/cli/whoami", nil)
	req.Header.Set("Accept-Encoding", droidAcceptEncoding)
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if want := baseURL + premPathPrefix; !strings.Contains(string(body), want) {
		t.Fatalf("whoami body %q does not carry %q", body, want)
	}
	if strings.Contains(string(body), "prem.example") {
		t.Fatalf("whoami body still names the real prem host: %s", body)
	}
}

func TestProxy_ForwardsContentLength(t *testing.T) {
	body := `{"flags":[]}`
	for _, tc := range []struct {
		name, method, body string
		wantLength         string
	}{
		{"post", "POST", body, "12"},
		{"get", "GET", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var length, transfer string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				length = r.Header.Get("Content-Length")
				transfer = strings.Join(r.TransferEncoding, ",")
				w.Write([]byte("ok"))
			}))
			defer upstream.Close()

			var reader io.Reader
			if tc.body != "" {
				reader = strings.NewReader(tc.body)
			}
			req, _ := http.NewRequest(tc.method, startProxy(t, upstream.URL)+"/api/test", reader)
			resp, err := http.DefaultTransport.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if transfer != "" {
				t.Fatalf("upstream saw Transfer-Encoding %q, want none", transfer)
			}
			if length != tc.wantLength {
				t.Fatalf("upstream saw Content-Length %q, want %q", length, tc.wantLength)
			}
		})
	}
}
