package proxy

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/factory"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

// The Kernel holds no Factory key (decision Q15): its model requests reach the proxy with a token of the Host's own in
// place of one, and the proxy sends them as droid would, with a key of the Key Pool, Factory's headers and the host
// its organization is served from. A request with any other credential is droid's, and goes the way it always went.

// conversationHeader names the conversation a Kernel model request belongs to. Like every X-Droi- header it never
// leaves for an upstream.
const conversationHeader = "X-Droi-Conversation"

// kernelAttempts bounds the keys one Kernel request tries. A key Factory turns away (out of quota, or refused) is
// left for the next before anything reaches the Kernel, which does not ask again by itself.
const kernelAttempts = 3

// kernelKeysCap bounds the conversations whose key is remembered; past it the memory starts over, and each
// conversation goes back to the Key Pool's first choice.
const kernelKeysCap = 1024

// kernelClient is the Kernel's token, and the seed its conversations' session ids are made from.
type kernelClient struct {
	token string
	seed  string
}

// SetKernelToken lets the Kernel's model requests in with token for a key. An empty token turns that off.
func (p *Proxy) SetKernelToken(token string) {
	var seed [16]byte
	_, _ = rand.Read(seed[:])
	p.mu.Lock()
	p.kernel = kernelClient{token: token, seed: hex.EncodeToString(seed[:])}
	p.kernelKeys = nil
	p.mu.Unlock()
}

// fromKernel reports whether req carries the Kernel's token: as a bearer token, or in X-Api-Key, where Anthropic's
// clients send their key.
func (p *Proxy) fromKernel(req *http.Request) (kernelClient, bool) {
	p.mu.Lock()
	k := p.kernel
	p.mu.Unlock()
	if k.token == "" {
		return k, false
	}
	for _, got := range []string{keys.NormalizeBearerToken(req.Header.Get("Authorization")), req.Header.Get("X-Api-Key")} {
		if subtle.ConstantTimeCompare([]byte(got), []byte(k.token)) == 1 {
			return k, true
		}
	}
	return k, false
}

// kernelKey is the key conversation session went out with last: the Key Pool keeps a conversation on it, as it keeps
// droid on the key it sends, so its prompt cache stays warm.
func (p *Proxy) kernelKey(session string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.kernelKeys[session]
}

func (p *Proxy) noteKernelKey(session, key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.kernelKeys == nil || len(p.kernelKeys) >= kernelKeysCap {
		p.kernelKeys = map[string]string{}
	}
	p.kernelKeys[session] = key
}

// serveKernel sends one Kernel model request upstream, with a key of the Key Pool, moving to another key when Factory
// turns one away.
func (p *Proxy) serveKernel(w http.ResponseWriter, req *http.Request, k kernelClient, start time.Time) {
	path := req.URL.Path
	if req.Body != nil {
		defer req.Body.Close()
	}
	// The token is the Host's own and goes nowhere but here.
	if !isLLMEndpoint(req.Method, path) || !isFactoryLLMPath(path) {
		p.event(diag.Event{Kind: "llm_blocked", Detail: "kernel " + req.Method + " " + path})
		writeLLMError(w, http.StatusBadRequest, "invalid_request_error", "Droi's Kernel token is only for model requests to Factory.")
		return
	}
	p.rotator.NoteLLMActivity(start)
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		p.event(diag.Event{Kind: "body_read_error", Detail: err.Error()})
		return
	}
	encoding := req.Header.Get("Content-Encoding")
	plain := raw
	if dec, err := decodeWireBody(encoding, raw); err == nil {
		plain = dec
		if to, shaped, changed := factory.Request(path, plain); changed {
			if wire, err := encodeWireBody(encoding, shaped); err == nil {
				raw, plain, path = wire, shaped, to
			}
		}
	}
	model := extractModel(plain)
	requestedPool := keys.ModelBillingPool(model)
	session := factory.SessionID(k.seed, req.Header.Get(conversationHeader))
	// Google's clients may put their key in the query; here that is the Kernel's token, which never leaves. A
	// request Factory takes on another route (Gemini's /generate) leaves its query behind.
	q := req.URL.Query()
	q.Del("key")
	query := q.Encode()
	if path != req.URL.Path {
		query = ""
	}

	tried := map[string]struct{}{}
	refreshed := false
	// A 413 is asked again once, with the older images and files left out (left of them).
	shrunk, left := false, 0
	presented := p.kernelKey(session)
	for attempt := 1; ; attempt++ {
		key, pool := p.rotator.SelectKeyForModel(presented, model, tried)
		presented = ""
		if key == "" {
			p.event(diag.Event{Kind: "llm_no_key", Model: model, Detail: "kernel"})
			writeNoKernelKey(w, model)
			return
		}
		p.noteKernelKey(session, key)
		id := factory.Identity(p.rotator.Identity(req.Context(), key))
		header := copyRequestHeaders(req.Header)
		header.Set("Authorization", "Bearer "+key)
		header.Set("Content-Length", strconv.Itoa(len(raw)))
		factory.Headers(header, id, path, model, session)
		base := factory.LLMBase(p.upstreamBaseURL, id)
		upstreamName := ""
		switch {
		case base == p.upstreamBaseURL:
		case base == factory.APIEU:
			upstreamName = "eu"
		default:
			upstreamName = "prem"
		}
		url := base + path
		if query != "" {
			url += "?" + query
		}
		// The record, the live list and the key opener read the request as it went upstream.
		sent := req.Clone(req.Context())
		sent.Header = header
		sent.URL.Path = path
		sent.URL.RawQuery = query

		upstreamReq, err := http.NewRequestWithContext(req.Context(), req.Method, url, bytes.NewReader(raw))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			p.event(diag.Event{Kind: "request_build_error", Model: model, Detail: err.Error()})
			return
		}
		upstreamReq.Header = header
		client := &http.Client{Transport: p.outboundTransport()}
		resp, err := client.Do(upstreamReq)
		headersAt := time.Now()
		if err != nil {
			writeLLMError(w, http.StatusBadGateway, "api_error", "Droi could not reach Factory: "+err.Error())
			p.event(diag.Event{Kind: "upstream_error", Model: model, Key: keys.MaskKey(key), Pool: pool, Detail: err.Error()})
			p.recordRequest(sent, path, upstreamName, model, requestedPool, pool, key, http.StatusBadGateway, start, headersAt, raw, 0, nil, nil, attempt > 1, "upstream_error", "")
			return
		}

		upstreamEncoding := resp.Header.Get("Content-Encoding")
		upstreamBody, decoder, plainBody := decodeUpstreamBody(upstreamEncoding, resp.Body)
		var respBody io.Reader = upstreamBody
		errorDetail := ""
		var refusal []byte
		if resp.StatusCode >= 400 {
			// Enough of an error to tell what Factory refused, and to send on whole when it is the answer.
			refusal, _ = io.ReadAll(io.LimitReader(upstreamBody, 1<<16))
			errorDetail = snippetForLog(refusal)
			respBody = io.MultiReader(bytes.NewReader(refusal), upstreamBody)
		}

		// A body over Factory's byte limit, and a conversation its usage policy check refuses, are no fault of the key:
		// the Kernel gets them in words a person can act on.
		if plainBody && resp.StatusCode == http.StatusRequestEntityTooLarge {
			resp.Body.Close()
			if decoder != nil {
				decoder.Close()
			}
			if !shrunk {
				shrunk = true
				if less, n := factory.LeaveOutAttachments(path, plain); n > 0 {
					if wire, err := encodeWireBody(encoding, less); err == nil {
						p.recordRequest(sent, path, upstreamName, model, requestedPool, pool, key, resp.StatusCode, start, headersAt, raw, 0, nil, nil, true, "", errorDetail)
						p.event(diag.Event{Kind: "request_shrunk", Key: keys.MaskKey(key), Pool: pool, Model: model, Status: resp.StatusCode, Detail: fmt.Sprintf("kernel: %d older images or files left out", n)})
						raw, plain, left = wire, less, n
						presented = key
						continue
					}
				}
			}
			p.recordRequest(sent, path, upstreamName, model, requestedPool, pool, key, resp.StatusCode, start, headersAt, raw, 0, nil, nil, attempt > 1, "", errorDetail)
			kind := "invalid_request_error"
			if strings.Contains(path, "/llm/a/") {
				kind = "request_too_large"
			}
			writeKernelError(w, path, http.StatusRequestEntityTooLarge, kind, factory.TooLarge(refusal, left), "", "")
			return
		}
		if plainBody && factory.ContentRefused(resp.StatusCode, resp.Header, refusal) {
			resp.Body.Close()
			if decoder != nil {
				decoder.Close()
			}
			p.recordRequest(sent, path, upstreamName, model, requestedPool, pool, key, resp.StatusCode, start, headersAt, raw, 0, nil, nil, attempt > 1, "", errorDetail)
			// A 400 with Factory's code, as OpenAI words a content refusal: a 403 reads to clients as a failed sign-in.
			id, said := factory.UpstreamIDs(resp.Header, refusal)
			msg := factory.VendorError(refusal, statusLine(resp.StatusCode)) + said + ". " + factory.ContentPolicyHint
			writeKernelError(w, path, http.StatusBadRequest, "invalid_request_error", msg, id, "content_policy_rejected")
			return
		}

		why := ""
		switch status := resp.StatusCode; {
		case status == http.StatusForbidden && plainBody && factory.OrgRefused(status, refusal) && !refreshed:
			// The organization whoami named is stale: ask again, and send once more with the same key.
			refreshed = true
			p.rotator.ForgetIdentity(key)
			presented = key
			why = "organization refused; whoami asked again"
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			p.rotator.MarkKeyHTTPError(key, status)
			tried[key] = struct{}{}
			why = fmt.Sprintf("credential rejected (HTTP %d)", status)
		case isCapacityStatus(status):
			p.rotator.MarkKeyPoolError(key, pool, status)
			tried[key] = struct{}{}
			why = fmt.Sprintf("HTTP %d", status)
		}
		if why != "" {
			resp.Body.Close()
			if decoder != nil {
				decoder.Close()
			}
			p.recordRequest(sent, path, upstreamName, model, requestedPool, pool, key, resp.StatusCode, start, headersAt, raw, 0, nil, nil, true, "", errorDetail)
			if attempt < kernelAttempts {
				p.event(diag.Event{Kind: "key_retried", Key: keys.MaskKey(key), Pool: pool, Model: model, Status: resp.StatusCode, Detail: "kernel: " + why})
				continue
			}
			// What Factory said stays in the record; the Kernel gets the one answer it gets when no key is left.
			p.event(diag.Event{Kind: "llm_no_key", Key: keys.MaskKey(key), Pool: pool, Model: model, Status: resp.StatusCode, Detail: "kernel: " + why + " on the last key tried"})
			writeNoKernelKey(w, model)
			return
		}

		p.relay(w, sent, resp, respBody, relayed{
			llm: true, plainBody: plainBody, upstreamEncoding: upstreamEncoding, upstreamURL: url, upstreamBase: base,
			upstreamPath: path, upstreamName: upstreamName, header: header, model: model, requestedPool: requestedPool,
			pool: pool, key: key, start: start, headersAt: headersAt, raw: raw, errorDetail: errorDetail,
		})
		resp.Body.Close()
		if decoder != nil {
			decoder.Close()
		}
		return
	}
}

// writeKernelError answers the Kernel with an error in the words of the API of path (factory.ErrorBody).
func writeKernelError(w http.ResponseWriter, path string, status int, kind, message, requestID, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(factory.ErrorBody(path, kind, message, requestID, code))
}

// statusLine is a status as Go's HTTP client names it, "403 Forbidden".
func statusLine(status int) string {
	return strings.TrimSpace(fmt.Sprintf("%d %s", status, http.StatusText(status)))
}

func writeNoKernelKey(w http.ResponseWriter, model string) {
	writeLLMError(w, http.StatusServiceUnavailable, "api_error", "No key of Droi's Key Pool can serve "+model+" now. Add a key or check the keys on Droi's Usage tab.")
}
