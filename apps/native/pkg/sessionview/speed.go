package sessionview

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// The Kernel's model speed, for the figure under the composer the Daemon's Sessions had: Pi records on each answer
// when its request started (timestamp), how long it took (durationMs) and the tokens it wrote (usage.output), but
// not when its first token came. A window watching the answer stream sees that: the first push whose generation has
// any text, thinking or call in it. So a call this window watched is timed as the Daemon's were, its decode time
// without the wait for the first token; one it did not watch (an older one, or one written while it was closed)
// counts its whole duration.

// now is the clock the first token is timed by; a test sets it.
var now = time.Now

// charsPerToken is the estimate of the tokens a streaming answer wrote so far, before the provider counts them.
const charsPerToken = 4

// liveCall is the answer streaming now, as this window has seen it.
type liveCall struct {
	// started is the request's start, the message's timestamp, in Unix milliseconds: it names the call.
	started float64
	first   time.Time
	chars   int
}

// callSample is what one entry adds to the speed: an answer's call, or a tool result's time.
type callSample struct {
	answer     bool
	started    float64
	durationMs float64
	output     float64
	toolMs     float64
}

// speed is State's: the call streaming now, the first tokens it saw, and the figures last added up.
type speed struct {
	live liveCall
	// firstTokenMs are the calls this window timed, by their start: how long the first token took.
	firstTokenMs map[float64]float64
	samples      map[kernel.ID]callSample
	statsAt      int64
	stats        session.Stats
}

// trackLive notes the generation pi.live holds, called with the lock held whenever a view lands.
func (s *State) trackLive() {
	var doc struct {
		Generation *struct {
			Message json.RawMessage `json:"message"`
		} `json:"generation"`
	}
	if len(s.live) == 0 || json.Unmarshal(s.live, &doc) != nil || doc.Generation == nil || len(doc.Generation.Message) == 0 {
		s.speed.live = liveCall{}
		return
	}
	var msg struct {
		Role      string  `json:"role"`
		Timestamp float64 `json:"timestamp"`
		Content   []struct {
			Text      string          `json:"text"`
			Thinking  string          `json:"thinking"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"content"`
	}
	if json.Unmarshal(doc.Generation.Message, &msg) != nil || msg.Role != "assistant" || msg.Timestamp == 0 {
		s.speed.live = liveCall{}
		return
	}
	chars := 0
	for _, c := range msg.Content {
		chars += len([]rune(c.Text)) + len([]rune(c.Thinking)) + len(c.Arguments)
	}
	if s.speed.live.started != msg.Timestamp {
		s.speed.live = liveCall{started: msg.Timestamp}
	}
	s.speed.live.chars = chars
	if chars > 0 && s.speed.live.first.IsZero() {
		t := now()
		s.speed.live.first = t
		if s.speed.firstTokenMs == nil {
			s.speed.firstTokenMs = map[float64]float64{}
		}
		s.speed.firstTokenMs[msg.Timestamp] = max(0, float64(t.UnixMilli())-msg.Timestamp)
	}
}

// LiveRate is the speed of the answer streaming now since its first token, estimated from what it wrote so far;
// 0 when none streams, or in its first second, when the burst that comes with the first token would read as twice
// the speed. Streaming says one is streaming, so the window draws the figure again while it does.
func (s *State) LiveRate(at time.Time) (rate float64, streaming bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l := s.speed.live
	if l.started == 0 {
		return 0, false
	}
	if l.first.IsZero() || l.chars == 0 {
		return 0, true
	}
	d := at.Sub(l.first)
	if d < time.Second {
		return 0, true
	}
	return float64(l.chars) / charsPerToken / d.Seconds(), true
}

// Stats are the conversation's model speed and time from the entries this window holds, as the Daemon's Stats
// were: its calls, their first tokens where this window saw them, their decode speed, and the time in tools.
func (s *State) Stats() session.Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.speed.statsAt == s.revision && s.speed.samples != nil {
		return s.speed.stats
	}
	if s.speed.samples == nil {
		s.speed.samples = map[kernel.ID]callSample{}
	}
	var st session.Stats
	for _, e := range s.held {
		if e.Kind != kernel.EntryAssistant && e.Kind != kernel.EntryToolResult {
			continue
		}
		sample, ok := s.speed.samples[e.ID]
		if !ok {
			sample = sampleOf(e)
			s.speed.samples[e.ID] = sample
		}
		st.ToolMs += sample.toolMs
		if !sample.answer {
			continue
		}
		call := session.CallStats{DecodeMs: sample.durationMs, OutputTokens: sample.output}
		if ttft, seen := s.speed.firstTokenMs[sample.started]; seen && ttft < sample.durationMs {
			call.TTFTMs = ttft
			call.DecodeMs = sample.durationMs - ttft
			st.TTFTMs += ttft
			st.TTFTCalls++
		}
		st.Calls++
		st.ModelMs += sample.durationMs
		st.DecodeMs += call.DecodeMs
		st.DecodeTokens += call.OutputTokens
		st.Last = &call
	}
	s.speed.stats, s.speed.statsAt = st, s.revision
	return st
}

// sampleOf reads what one entry adds: an answer that recorded its duration and output, or tool results' time.
func sampleOf(e kernel.Entry) callSample {
	var messages []struct {
		Role       string  `json:"role"`
		Timestamp  float64 `json:"timestamp"`
		DurationMs float64 `json:"durationMs"`
		Usage      *struct {
			Output float64 `json:"output"`
		} `json:"usage"`
	}
	if json.Unmarshal(e.Model, &messages) != nil {
		return callSample{}
	}
	var out callSample
	for _, m := range messages {
		switch m.Role {
		case "toolResult":
			out.toolMs += m.DurationMs
		case "assistant":
			if m.DurationMs > 0 && m.Usage != nil && m.Usage.Output > 0 {
				out = callSample{answer: true, started: m.Timestamp, durationMs: m.DurationMs, output: m.Usage.Output, toolMs: out.toolMs}
			}
		}
	}
	return out
}

// SpeedReport says what the speed under the composer was made from, for the window's log when it shows none: the
// entries held, the answers among them and how many recorded a duration and output, and the call streaming now.
func (s *State) SpeedReport() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	answers, timed := 0, 0
	for _, e := range s.held {
		if e.Kind != kernel.EntryAssistant {
			continue
		}
		answers++
		if sampleOf(e).answer {
			timed++
		}
	}
	return fmt.Sprintf("opened %t, held %d, answers %d, timed %d, streaming %t, pi.live %t",
		s.opened, len(s.held), answers, timed, s.speed.live.started != 0, len(s.live) > 0)
}
