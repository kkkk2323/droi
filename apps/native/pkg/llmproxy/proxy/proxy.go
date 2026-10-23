// Package proxy is Droi's LLM proxy (ADR 0027): the Daemon's Factory
// traffic goes through it so Droi can record every LLM request, keep the
// prompt caches of idle Sessions warm and move the Patched Droid to another
// key of the Key Pool. It never changes a request's Authorization: which key
// an LLM request carries is decided inside droid, by the key file the patch
// reads. Ported from droid-proxy, which rotates keys itself.
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

var hopByHopHeaders = map[string]bool{
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"te":                  true,
	"trailer":             true,
	"transfer-encoding":   true,
	"upgrade":             true,
}

func isLLMEndpoint(method string, path string) bool {
	if strings.ToUpper(method) != "POST" {
		return false
	}
	// /api/llm/{provider}/v1/{action}
	parts := strings.Split(path, "/")
	return len(parts) >= 6 && parts[1] == "api" && parts[2] == "llm" && parts[4] == "v1"
}

func copyRequestHeaders(src http.Header) http.Header {
	out := make(http.Header)
	for name, vals := range src {
		lower := strings.ToLower(name)
		if hopByHopHeaders[lower] || lower == "host" || lower == "content-length" {
			continue
		}
		for _, v := range vals {
			out.Add(name, v)
		}
	}
	return out
}

func copyResponseHeaders(src http.Header) http.Header {
	out := make(http.Header)
	for name, vals := range src {
		lower := strings.ToLower(name)
		if hopByHopHeaders[lower] || lower == "content-length" || lower == "content-encoding" {
			continue
		}
		for _, v := range vals {
			out.Add(name, v)
		}
	}
	return out
}

// KeySwitcher moves the Patched Droid to another key: it writes the key
// file the patch reads. It answers false when it did not (the Key Pool is
// in manual mode, or the file could not be written).
type KeySwitcher func(key, reason string) bool

// retryAfterSwitch is the delay a converted 402 asks droid to wait before it
// retries with the new key. droid waits at least 5 s on a 429 anyway.
const retryAfterSwitch = "500"

type Proxy struct {
	upstreamBaseURL string
	rotator         *keys.KeyRotator
	switchKey       KeySwitcher
	server          *http.Server
	listener        net.Listener
	baseURL         string
	premUpstream    string
	transport       http.RoundTripper
	cacheWarmerRef  *CacheWarmer
	recorder        *diag.Recorder
	startedAt       time.Time
	mu              sync.Mutex
	live            map[*liveCall]struct{}
}

// liveCall is an LLM request whose reply is streaming through the proxy.
type liveCall struct {
	call     diag.LiveCall
	observer *diag.UsageObserver
}

// NewProxy forwards to upstreamBaseURL (https://api.factory.ai say).
// switchKey may be nil: the proxy then only records.
func NewProxy(upstreamBaseURL string, rotator *keys.KeyRotator, switchKey KeySwitcher) *Proxy {
	return &Proxy{
		upstreamBaseURL: strings.TrimRight(upstreamBaseURL, "/"),
		rotator:         rotator,
		switchKey:       switchKey,
		recorder:        diag.NewRecorder(diag.DefaultCapacity),
		startedAt:       time.Now(),
	}
}

// SetTransport replaces the transport to the upstreams (for tests).
func (p *Proxy) SetTransport(t http.RoundTripper) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.transport = t
}

func (p *Proxy) outboundTransport() http.RoundTripper {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.transport != nil {
		return p.transport
	}
	return http.DefaultTransport
}

// SetCacheWarmer attaches the cache warmer; call it before Start.
func (p *Proxy) SetCacheWarmer(w *CacheWarmer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cacheWarmerRef = w
}

func (p *Proxy) cacheWarmer() *CacheWarmer {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cacheWarmerRef
}

// Recorder is the proxy's record of requests and events.
func (p *Proxy) Recorder() *diag.Recorder { return p.recorder }

// Live is the LLM requests whose replies are coming through now, oldest
// first.
func (p *Proxy) Live() []diag.LiveCall {
	p.mu.Lock()
	out := make([]diag.LiveCall, 0, len(p.live))
	for l := range p.live {
		c := l.call
		c.FirstToken, c.Output, c.Usage = l.observer.Progress()
		out = append(out, c)
	}
	p.mu.Unlock()
	slices.SortFunc(out, func(a, b diag.LiveCall) int { return a.Start.Compare(b.Start) })
	return out
}

func (p *Proxy) startLive(l *liveCall) func() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.live == nil {
		p.live = map[*liveCall]struct{}{}
	}
	p.live[l] = struct{}{}
	return func() {
		p.mu.Lock()
		delete(p.live, l)
		p.mu.Unlock()
	}
}

// Start listens on host:port (port 0 picks one) and answers the base URL.
func (p *Proxy) Start(host string, port int) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", p.handleHealth)
	mux.HandleFunc("/", p.handleRequest)
	p.server = &http.Server{Handler: mux}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return "", fmt.Errorf("failed to listen: %w", err)
	}
	p.listener = ln
	p.baseURL = "http://" + ln.Addr().String()
	go func() { _ = p.server.Serve(ln) }()
	return p.baseURL, nil
}

func (p *Proxy) BaseURL() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.baseURL
}

func (p *Proxy) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	srv := p.server
	p.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// isCapacityStatus reports an upstream status that means the key's billing
// pool is out of quota, so a different key may still succeed.
func isCapacityStatus(status int) bool {
	return status == http.StatusPaymentRequired || status == http.StatusTooManyRequests
}

func (p *Proxy) event(ev diag.Event) {
	ev.Time = time.Now()
	p.recorder.RecordEvent(ev)
}

func (p *Proxy) handleRequest(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	upstreamPath := req.URL.Path
	upstreamBase := p.upstreamBaseURL
	upstreamName := ""
	if premPath, isPrem := stripPremPrefix(upstreamPath); isPrem {
		upstreamBase = p.PremUpstream()
		upstreamName = "prem"
		upstreamPath = premPath
	}
	upstreamURL := upstreamBase + upstreamPath
	if req.URL.RawQuery != "" {
		upstreamURL += "?" + req.URL.RawQuery
	}

	p.rotator.NoteClientHeaders(req.Header)
	outHeaders := copyRequestHeaders(req.Header)
	llm := isLLMEndpoint(req.Method, upstreamPath)
	if req.Body != nil {
		defer req.Body.Close()
	}

	var raw []byte
	var bodyReader io.Reader = req.Body
	model := ""
	key := ""
	pool := keys.PoolAny
	requestedPool := keys.PoolAny
	if llm {
		p.rotator.NoteLLMActivity(start)
		key = keys.NormalizeBearerToken(req.Header.Get("Authorization"))
		p.rotator.NoteKeyUsed(key)
		var err error
		raw, err = io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			p.event(diag.Event{Kind: "body_read_error", Detail: err.Error()})
			return
		}
		p.cacheWarmer().RealRequestStarted(req.Header)
		plain := raw
		if dec, err := decodeWireBody(req.Header.Get("Content-Encoding"), raw); err == nil {
			plain = dec
		}
		model = extractModel(plain)
		requestedPool = keys.ModelBillingPool(model)
		pool = requestedPool
		bodyReader = bytes.NewReader(raw)
		outHeaders.Set("Content-Length", strconv.Itoa(len(raw)))
		// The key this request would get from the Key Pool. When it is not
		// the one droid sent, the next requests get it: this one keeps its
		// key, since the proxy never rewrites a credential.
		if want, wantPool := p.rotator.SelectKeyForModel(key, model, nil); want != "" {
			pool = wantPool
			if want != key && p.switchKey != nil && p.switchKey(want, "the Key Pool prefers another key for "+model) {
				p.event(diag.Event{Kind: "key_switched", Key: keys.MaskKey(want), Pool: wantPool, Model: model, Detail: "from " + keys.MaskKey(key)})
			}
		}
	}

	upstreamReq, err := http.NewRequestWithContext(req.Context(), req.Method, upstreamURL, bodyReader)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		p.event(diag.Event{Kind: "request_build_error", Model: model, Detail: err.Error()})
		return
	}
	upstreamReq.Header = outHeaders
	if !llm {
		upstreamReq.ContentLength = req.ContentLength
	}
	client := &http.Client{Transport: p.outboundTransport()}
	resp, err := client.Do(upstreamReq)
	headersAt := time.Now()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		p.event(diag.Event{Kind: "upstream_error", Model: model, Key: keys.MaskKey(key), Pool: pool, Detail: err.Error()})
		if llm {
			p.recordRequest(req, upstreamPath, upstreamName, model, requestedPool, pool, key, http.StatusBadGateway, start, headersAt, raw, 0, nil, nil, false, "upstream_error", "")
		}
		return
	}
	defer resp.Body.Close()

	if !llm && resp.StatusCode == http.StatusForbidden {
		// Factory's edge has kept answering 403 on a pooled connection for
		// hours while a fresh one got 200; dial anew next time.
		client.CloseIdleConnections()
	}

	upstreamEncoding := resp.Header.Get("Content-Encoding")
	upstreamBody, decoder, plainBody := decodeUpstreamBody(upstreamEncoding, resp.Body)
	if decoder != nil {
		defer decoder.Close()
	}

	errorDetail := ""
	var respBody io.Reader = upstreamBody
	if llm && resp.StatusCode >= 400 {
		if sniff, err := io.ReadAll(io.LimitReader(upstreamBody, 256)); err == nil {
			errorDetail = snippetForLog(sniff)
			respBody = io.MultiReader(bytes.NewReader(sniff), upstreamBody)
		}
	}

	if llm && key != "" {
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			p.rotator.MarkKeyHTTPError(key, resp.StatusCode)
			p.event(diag.Event{Kind: "key_cooled", Key: keys.MaskKey(key), Pool: pool, Model: model, Status: resp.StatusCode, Detail: "credential rejected; key cooled down"})
			p.switchAway(key, model, "credential rejected")
		case isCapacityStatus(resp.StatusCode):
			p.rotator.MarkKeyPoolError(key, pool, resp.StatusCode)
			if next := p.switchAway(key, model, fmt.Sprintf("HTTP %d", resp.StatusCode)); next != "" {
				p.event(diag.Event{Kind: "key_exhausted", Key: keys.MaskKey(key), Pool: pool, Model: model, Status: resp.StatusCode, Detail: "switched to " + keys.MaskKey(next) + "; droid retries"})
				// droid does not retry a 402. A 429 it retries, and its retry
				// reads the key file anew.
				_, _ = io.Copy(io.Discard, io.LimitReader(respBody, 1<<16))
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After-Ms", retryAfterSwitch)
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Droi moved to another key of the Key Pool; retrying"}}`))
				p.recordRequest(req, upstreamPath, upstreamName, model, requestedPool, pool, key, resp.StatusCode, start, headersAt, raw, 0, nil, nil, true, "", errorDetail)
				return
			}
			p.event(diag.Event{Kind: "key_exhausted", Key: keys.MaskKey(key), Pool: pool, Model: model, Status: resp.StatusCode, Detail: "no other key; returning the upstream error"})
		}
	}

	for name, vals := range copyResponseHeaders(resp.Header) {
		for _, v := range vals {
			w.Header().Add(name, v)
		}
	}
	if !plainBody && upstreamEncoding != "" {
		w.Header().Set("Content-Encoding", upstreamEncoding)
	}
	w.WriteHeader(resp.StatusCode)

	flusher, canFlush := w.(http.Flusher)
	flush := func() {}
	if canFlush {
		flush = flusher.Flush
	}
	var observer *diag.UsageObserver
	var dst io.Writer = &flushWriter{w: w, flush: flush}
	if llm {
		observer = diag.NewUsageObserver(resp.Header.Get("Content-Type"))
		dst = io.MultiWriter(dst, observer)
		defer p.startLive(&liveCall{observer: observer, call: diag.LiveCall{
			SessionID: strings.TrimSpace(req.Header.Get(sessionIDHeader)),
			CallType:  strings.TrimSpace(req.Header.Get(messageCallTypeHeader)),
			Model:     model,
			Key:       keys.MaskKey(key),
			Start:     start,
		}})()
	}
	var written int64
	var copyErr error
	if !llm && plainBody {
		written, copyErr = p.copyRewritingPremHosts(dst, respBody, flush)
	} else {
		written, copyErr = io.Copy(dst, respBody)
	}
	flush()
	if !llm {
		return
	}
	logErr := ""
	if copyErr != nil {
		logErr = "response_copy_error"
	}
	var usage *diag.TokenUsage
	if u, ok := observer.Usage(); ok {
		usage = &u
	}
	p.recordRequest(req, upstreamPath, upstreamName, model, requestedPool, pool, key, resp.StatusCode, start, headersAt, raw, written, usage, observer, false, logErr, errorDetail)
	if warmer := p.cacheWarmer(); warmer != nil && resp.StatusCode == http.StatusOK && copyErr == nil && usage != nil {
		warmer.Observe(warmCapture{
			sessionID: strings.TrimSpace(req.Header.Get(sessionIDHeader)),
			upstream:  upstreamBase,
			path:      upstreamPath,
			rawQuery:  req.URL.RawQuery,
			header:    outHeaders,
			encoding:  req.Header.Get("Content-Encoding"),
			wire:      raw,
			model:     model,
			provider:  providerForRequest(req, upstreamPath),
			key:       key,
			usage:     *usage,
			sentAt:    start,
		})
	}
}

// switchAway moves droid off key when the Key Pool has another key for
// model; it answers that key, or "".
func (p *Proxy) switchAway(key, model, why string) string {
	if p.switchKey == nil {
		return ""
	}
	next, _ := p.rotator.SelectKeyForModel("", model, map[string]struct{}{key: {}})
	if next == "" || next == key || !p.switchKey(next, why) {
		return ""
	}
	return next
}

// recordRequest records an LLM request; observer is nil when no reply was
// copied.
func (p *Proxy) recordRequest(req *http.Request, path, upstream, model, requestedPool, pool, key string, status int, start, headersAt time.Time, raw []byte, written int64, usage *diag.TokenUsage, observer *diag.UsageObserver, switched bool, logErr, errorDetail string) {
	stream, ttft := false, int64(0)
	if observer != nil {
		stream = observer.IsStream()
		if first, _, _ := observer.Progress(); !first.IsZero() {
			ttft = max(first.Sub(start).Milliseconds(), 1)
		}
	}
	p.recorder.RecordRequest(diag.RequestRecord{
		SessionID:     strings.TrimSpace(req.Header.Get(sessionIDHeader)),
		CallType:      strings.TrimSpace(req.Header.Get(messageCallTypeHeader)),
		TTFTMs:        ttft,
		Time:          time.Now(),
		Model:         model,
		Provider:      providerForRequest(req, path),
		Upstream:      upstream,
		RequestedPool: requestedPool,
		Key:           keys.MaskKey(key),
		Pool:          pool,
		Status:        status,
		Stream:        stream,
		DurationMs:    time.Since(start).Milliseconds(),
		TTFBMs:        headersAt.Sub(start).Milliseconds(),
		RequestBytes:  int64(len(raw)),
		Bytes:         written,
		Usage:         usage,
		Attempts:      1,
		Rotated:       switched,
		Error:         logErr,
		ErrorDetail:   errorDetail,
	})
}

// flushWriter flushes after every write so a streamed reply reaches droid
// as it arrives.
type flushWriter struct {
	w     io.Writer
	flush func()
}

func (f *flushWriter) Write(b []byte) (int, error) {
	n, err := f.w.Write(b)
	f.flush()
	return n, err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (p *Proxy) handleHealth(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "uptimeSec": int(time.Since(p.startedAt).Seconds())})
}

// extractModel returns the "model" field of an LLM request body.
func extractModel(body []byte) string {
	var m struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &m) == nil {
		return m.Model
	}
	return ""
}

// providerForRequest names the upstream vendor: the explicit header wins,
// otherwise it is inferred from the LLM path segment.
func providerForRequest(req *http.Request, upstreamPath string) string {
	if v := req.Header.Get("x-api-provider"); v != "" {
		return v
	}
	switch {
	case strings.Contains(upstreamPath, "/anthropic/"):
		return "anthropic"
	case strings.Contains(upstreamPath, "/openai/"):
		return "openai"
	}
	return ""
}

// snippetForLog collapses an upstream error body into one capped line.
func snippetForLog(b []byte) string {
	s := strings.ReplaceAll(string(b), "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
