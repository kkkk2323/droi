// Package diag keeps a bounded in-memory record of proxy activity so a local
// client (Droi's Usage page) can show request history and operational events
// without scraping stderr.
package diag

import (
	"encoding/json"
	"sync"
	"time"
)

// RequestRecord is the outcome of one proxied LLM request.
type RequestRecord struct {
	Seq  int       `json:"seq"`
	Time time.Time `json:"time"`
	// Kind is "cache_warm" for a cache-warming refresh and empty for a
	// proxied client request.
	Kind     string `json:"kind,omitempty"`
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`
	// Upstream is "prem" when the request went to the Prem host.
	Upstream      string `json:"upstream,omitempty"`
	RequestedPool string `json:"requestedPool,omitempty"`
	Key           string `json:"key,omitempty"` // masked
	Pool          string `json:"pool,omitempty"`
	Status        int    `json:"status"`
	Stream        bool   `json:"stream"`
	DurationMs    int64  `json:"durationMs"`
	// TTFBMs is the time until upstream response headers arrived.
	TTFBMs       int64       `json:"ttfbMs"`
	RequestBytes int64       `json:"requestBytes"`
	Bytes        int64       `json:"bytes"`
	Usage        *TokenUsage `json:"usage,omitempty"`
	Attempts     int         `json:"attempts"`
	Rotated      bool        `json:"rotated"`
	Error        string      `json:"error,omitempty"`
	// ErrorDetail is a short, single-line excerpt of an upstream error body.
	ErrorDetail string `json:"errorDetail,omitempty"`
	// BodyFile is the base name of the request/response body dumps
	// (<BodyFile>.req.json, <BodyFile>.resp.txt) when body logging is on.
	BodyFile string `json:"bodyFile,omitempty"`
}

// Event is one operational log line (key exhaustion, cooldown, upstream error,
// ...).
type Event struct {
	Seq    int       `json:"seq"`
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"`
	Key    string    `json:"key,omitempty"` // masked
	Pool   string    `json:"pool,omitempty"`
	Model  string    `json:"model,omitempty"`
	Status int       `json:"status,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

// Recorder is a fixed-capacity ring buffer of recent activity. The zero value
// is not usable; call NewRecorder.
type Recorder struct {
	mu          sync.Mutex
	max         int
	requests    []RequestRecord
	events      []Event
	requestSink *FileSink
	eventSink   *FileSink
	nextSeq     int
}

// DefaultCapacity is the per-buffer record limit.
const DefaultCapacity = 500

func NewRecorder(max int) *Recorder {
	if max <= 0 {
		max = DefaultCapacity
	}
	return &Recorder{max: max}
}

func (r *Recorder) RecordRequest(rec RequestRecord) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextSeq++
	rec.Seq = r.nextSeq
	r.requests = append(r.requests, rec)
	if len(r.requests) > r.max {
		r.requests = r.requests[len(r.requests)-r.max:]
	}
	if r.requestSink != nil {
		if line, err := json.Marshal(rec); err == nil {
			_ = r.requestSink.Write(line)
		}
	}
}

func (r *Recorder) RecordEvent(ev Event) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextSeq++
	ev.Seq = r.nextSeq
	r.events = append(r.events, ev)
	if len(r.events) > r.max {
		r.events = r.events[len(r.events)-r.max:]
	}
	if r.eventSink != nil {
		if line, err := json.Marshal(ev); err == nil {
			_ = r.eventSink.Write(line)
		}
	}
}

// PersistRequests writes subsequent request records to the given JSONL sink.
func (r *Recorder) PersistRequests(s *FileSink) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requestSink = s
}

// PersistEvents writes subsequent events to the given JSONL sink.
func (r *Recorder) PersistEvents(s *FileSink) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventSink = s
}

// Close flushes and closes any attached file sinks.
func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	reqSink, evtSink := r.requestSink, r.eventSink
	r.mu.Unlock()
	var firstErr error
	if err := reqSink.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := evtSink.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// Requests returns the most recent requests, newest first.
func (r *Recorder) Requests() []RequestRecord {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RequestRecord, len(r.requests))
	for i, rec := range r.requests {
		out[len(r.requests)-1-i] = rec
	}
	return out
}

// Events returns the most recent events, newest first.
func (r *Recorder) Events() []Event {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, len(r.events))
	for i, ev := range r.events {
		out[len(r.events)-1-i] = ev
	}
	return out
}
