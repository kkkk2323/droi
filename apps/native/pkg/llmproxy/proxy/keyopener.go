package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

// The key opener starts the Standard months of queued keys (see
// keys/opening.go) with the smallest request the proxy has seen: a recent
// Haiku request from droid (session titles), replayed under the queued key
// with max_tokens 1. While it is off, or until one is seen, real requests
// open months instead. Ported from droid-proxy.

const (
	openTemplateMaxBytes = 64 << 10
	// keyOpenFromHour: months open from this local hour, when the day's use
	// starts (droid-proxy's request log, 2026-10).
	keyOpenFromHour = 8
	keyOpenTick     = time.Minute
	keyOpenTimeout  = time.Minute
)

type openTemplate struct {
	url      string
	header   http.Header
	encoding string
	plain    []byte
	model    string
	provider string
}

type keyOpener struct {
	rotator   *keys.KeyRotator
	recorder  *diag.Recorder
	transport func() http.RoundTripper
	now       func() time.Time

	mu   sync.Mutex
	on   bool
	tmpl *openTemplate
}

// syncLocked tells the rotator whether the opener starts the planned months:
// only while it is on and has a request to copy.
func (o *keyOpener) syncLocked() {
	o.rotator.SetScheduledOpening(o.on && o.tmpl != nil)
}

func (o *keyOpener) setOn(on bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.on = on
	o.syncLocked()
}

func (o *keyOpener) isOn() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.on
}

// observe keeps a finished 200 request as the template when it is a small
// Standard Haiku request. It keeps one while the opener is off too, so
// turning it on can open at once.
func (o *keyOpener) observe(url string, header http.Header, encoding string, wire []byte, model, provider string) {
	if o == nil || !strings.Contains(strings.ToLower(model), "haiku") || keys.ModelBillingPool(model) != keys.PoolStandard || len(wire) > openTemplateMaxBytes {
		return
	}
	plain, err := decodeWireBody(encoding, wire)
	if err != nil {
		return
	}
	if _, err := warmReplayBody(plain, 1); err != nil {
		return
	}
	h := header.Clone()
	h.Del("Authorization")
	o.mu.Lock()
	defer o.mu.Unlock()
	o.tmpl = &openTemplate{url: url, header: h, encoding: encoding, plain: append([]byte(nil), plain...), model: model, provider: provider}
	o.syncLocked()
}

func (o *keyOpener) template() *openTemplate {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.tmpl
}

func (o *keyOpener) dropTemplate() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.tmpl = nil
	o.syncLocked()
}

func (o *keyOpener) run(ctx context.Context) {
	ticker := time.NewTicker(keyOpenTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.tick(ctx)
		}
	}
}

// tick opens at most one key; the next tick opens the next one.
func (o *keyOpener) tick(ctx context.Context) {
	if !o.isOn() || o.now().Hour() < keyOpenFromHour {
		return
	}
	t := o.template()
	if t == nil {
		return
	}
	key, ok := o.rotator.ClaimNextOpening()
	if !ok {
		return
	}
	status, err := o.send(ctx, t, key)
	if err == nil && status == http.StatusOK {
		o.event("key_opened", key, status, "model="+t.model)
		// While the proxy is idle no refresh round would show the month
		// opening.
		o.rotator.RecheckKey(key)
		return
	}
	o.rotator.ReleaseOpening(key)
	detail := fmt.Sprintf("model=%s", t.model)
	if err != nil {
		detail += " error=" + err.Error()
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		o.rotator.MarkKeyHTTPError(key, status)
	case isCapacityStatus(status):
		o.rotator.MarkKeyPoolError(key, keys.PoolStandard, status)
	default:
		// The template itself may be stale; real requests open months until
		// a fresh one is seen.
		o.dropTemplate()
		detail += "; template dropped"
	}
	o.event("key_open_failed", key, status, detail)
}

func (o *keyOpener) send(ctx context.Context, t *openTemplate, key string) (int, error) {
	start := o.now()
	body, err := warmReplayBody(t.plain, 1)
	if err == nil && t.encoding == "gzip" {
		body, err = gzipBody(body)
	}
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, keyOpenTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header = t.header.Clone()
	req.Header.Set("Authorization", "Bearer "+key)
	if req.Header.Get(assistantMessageIDHeader) != "" {
		req.Header.Set(assistantMessageIDHeader, newRequestID())
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	resp, err := (&http.Client{Transport: o.transport()}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	observer := diag.NewUsageObserver(resp.Header.Get("Content-Type"))
	openBody, decoder, _ := decodeUpstreamBody(resp.Header.Get("Content-Encoding"), resp.Body)
	if decoder != nil {
		defer decoder.Close()
	}
	_, copyErr := io.Copy(observer, io.LimitReader(openBody, warmResponseLimit))

	rec := diag.RequestRecord{
		Time:          o.now(),
		Kind:          "key_open",
		Model:         t.model,
		Provider:      t.provider,
		RequestedPool: keys.PoolStandard,
		Key:           keys.MaskKey(key),
		Pool:          keys.PoolStandard,
		Status:        resp.StatusCode,
		Stream:        observer.IsStream(),
		RequestBytes:  int64(len(body)),
		Attempts:      1,
	}
	rec.DurationMs = rec.Time.Sub(start).Milliseconds()
	if u, ok := observer.Usage(); ok {
		rec.Usage = &u
	}
	if copyErr != nil {
		rec.Error = "key_open_error"
	}
	o.recorder.RecordRequest(rec)
	return resp.StatusCode, copyErr
}

func (o *keyOpener) event(kind, key string, status int, detail string) {
	o.recorder.RecordEvent(diag.Event{Time: o.now(), Kind: kind, Key: keys.MaskKey(key), Pool: keys.PoolStandard, Status: status, Detail: detail})
	log.Printf("%s key=%s status=%d %s", kind, keys.MaskKey(key), status, detail)
}

// SetKeyOpening turns the scheduled key opener on or off. Off, a waiting
// key's month starts with the next real request.
func (p *Proxy) SetKeyOpening(on bool) { p.keyOpener.setOn(on) }

// StartKeyOpener opens queued keys' months on schedule until ctx is done.
func (p *Proxy) StartKeyOpener(ctx context.Context) {
	go p.keyOpener.run(ctx)
}
