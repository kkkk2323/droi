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
	// SessionID is droid's Session the request was for (X-Session-Id), and
	// CallType what a side call was for (X-Message-Call-Type: a title, a
	// compaction...); empty for the conversation itself.
	SessionID string `json:"sessionId,omitempty"`
	CallType  string `json:"callType,omitempty"`
	// TTFTMs is the time until the first generated token of a stream; 0
	// when none came or the reply was not streamed.
	TTFTMs int64 `json:"ttftMs,omitempty"`
}

// TokensPerSecond is the call's output speed, from its first token to its
// end; 0 when unknown.
func (r RequestRecord) TokensPerSecond() float64 {
	if r.Usage == nil || r.TTFTMs <= 0 || r.Usage.Output <= 0 {
		return 0
	}
	decode := r.DurationMs - r.TTFTMs
	if decode < minDecodeMs {
		return 0
	}
	return float64(r.Usage.Output) / (float64(decode) / 1000)
}

// minDecodeMs is the shortest output a speed is taken from: one that came in
// a single burst would read as thousands of tokens a second.
const minDecodeMs = 100

// SessionCalls sums the model calls one Session made through the proxy: the
// ones that answered 200, cache warming left out.
type SessionCalls struct {
	Calls int
	Usage TokenUsage
	// DurationMs is the time the calls took, from request to last byte.
	DurationMs int64
	TTFTMs     int64
	TTFTCalls  int
	// DecodeMs and DecodeTokens are the output of the calls with a speed.
	DecodeMs     int64
	DecodeTokens int64
	// Last is the latest call.
	Last RequestRecord
}

func (s *SessionCalls) add(rec RequestRecord) {
	s.Calls++
	if u := rec.Usage; u != nil {
		s.Usage.Input += u.Input
		s.Usage.Output += u.Output
		s.Usage.CacheRead += u.CacheRead
		s.Usage.CacheWrite += u.CacheWrite
		s.Usage.Reasoning += u.Reasoning
	}
	s.DurationMs += rec.DurationMs
	if rec.TTFTMs > 0 {
		s.TTFTMs += rec.TTFTMs
		s.TTFTCalls++
	}
	if rec.TokensPerSecond() > 0 {
		s.DecodeMs += rec.DurationMs - rec.TTFTMs
		s.DecodeTokens += rec.Usage.Output
	}
	s.Last = rec
}

// MeanTTFTMs is the mean time to the first token; 0 when unknown.
func (s SessionCalls) MeanTTFTMs() float64 {
	if s.TTFTCalls == 0 {
		return 0
	}
	return float64(s.TTFTMs) / float64(s.TTFTCalls)
}

// TokensPerSecond is the calls' output speed; 0 when unknown.
func (s SessionCalls) TokensPerSecond() float64 {
	if s.DecodeMs <= 0 || s.DecodeTokens <= 0 {
		return 0
	}
	return float64(s.DecodeTokens) / (float64(s.DecodeMs) / 1000)
}

// CacheHit is the share of the input read from the prompt cache, 0 to 1.
func (u TokenUsage) CacheHit() float64 {
	if u.Input <= 0 {
		return 0
	}
	return float64(u.CacheRead) / float64(u.Input)
}

// LiveCall is a model call still under way.
type LiveCall struct {
	SessionID string
	CallType  string
	Model     string
	Key       string // masked
	Start     time.Time
	// FirstToken is zero until the first generated token came.
	FirstToken time.Time
	// Output is the tokens generated so far: the upstream's count once it
	// gave one, an estimate from the text until then.
	Output float64
	// Usage is what the upstream reported so far, the input and cache
	// usually.
	Usage TokenUsage
}

// maxSessions is how many Sessions' sums the Recorder keeps; the one
// updated longest ago goes first.
const maxSessions = 256

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
	sessions    map[string]*SessionCalls
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
	if rec.SessionID != "" && rec.Kind == "" && rec.Status == 200 {
		r.addSessionLocked(rec)
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

func (r *Recorder) addSessionLocked(rec RequestRecord) {
	if r.sessions == nil {
		r.sessions = map[string]*SessionCalls{}
	}
	s := r.sessions[rec.SessionID]
	if s == nil {
		if len(r.sessions) >= maxSessions {
			oldest := ""
			for id, o := range r.sessions {
				if oldest == "" || o.Last.Seq < r.sessions[oldest].Last.Seq {
					oldest = id
				}
			}
			delete(r.sessions, oldest)
		}
		s = &SessionCalls{}
		r.sessions[rec.SessionID] = s
	}
	s.add(rec)
}

// Session is what the Recorder has summed of Session id's calls since it
// started.
func (r *Recorder) Session(id string) (SessionCalls, bool) {
	if r == nil {
		return SessionCalls{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.sessions[id]
	if s == nil {
		return SessionCalls{}, false
	}
	return *s, true
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
