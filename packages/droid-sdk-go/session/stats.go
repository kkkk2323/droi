package session

import (
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// minDecode is the shortest decode a speed is taken from: a message that
// arrives in one burst would otherwise read as thousands of tokens a second.
const minDecode = 100 * time.Millisecond

// CallStats is one model call: how long its first token took, how long the
// rest streamed, and the output tokens the Daemon counted for it.
type CallStats struct {
	TTFTMs       float64 `json:"ttftMs"`
	DecodeMs     float64 `json:"decodeMs"`
	OutputTokens float64 `json:"outputTokens"`
}

// TokensPerSecond is the call's decode speed; 0 when unknown.
func (c CallStats) TokensPerSecond() float64 {
	if c.DecodeMs < float64(minDecode.Milliseconds()) || c.OutputTokens <= 0 {
		return 0
	}
	return c.OutputTokens / (c.DecodeMs / 1000)
}

// Stats is a Session's model speed and time, timed from its notifications:
// a call starts when the working state enters thinking or streaming, its
// first token is the first text or thinking delta, and it ends with the
// assistant message. Tool time is time spent in executing_tool.
type Stats struct {
	Calls        int     `json:"calls"`
	ModelMs      float64 `json:"modelMs"`
	ToolMs       float64 `json:"toolMs"`
	TTFTMs       float64 `json:"ttftMs"`
	TTFTCalls    int     `json:"ttftCalls"`
	DecodeMs     float64 `json:"decodeMs"`
	DecodeTokens float64 `json:"decodeTokens"`
	// Last is the latest call; nil before one finished.
	Last *CallStats `json:"last,omitempty"`
}

// MeanTTFTMs is the mean first-token latency; 0 when unknown.
func (s Stats) MeanTTFTMs() float64 {
	if s.TTFTCalls == 0 {
		return 0
	}
	return s.TTFTMs / float64(s.TTFTCalls)
}

// TokensPerSecond is the Session's decode speed; 0 when unknown.
func (s Stats) TokensPerSecond() float64 {
	if s.DecodeMs <= 0 || s.DecodeTokens <= 0 {
		return 0
	}
	return s.DecodeTokens / (s.DecodeMs / 1000)
}

func (s Stats) clone() Stats {
	if s.Last != nil {
		c := *s.Last
		s.Last = &c
	}
	return s
}

// statsFold is Stats plus the call and tool run still under way.
type statsFold struct {
	totals Stats

	open         bool
	start, first time.Time
	// unclaimed are output tokens a usage notification reported while the
	// call was still open; the call takes them when it ends.
	unclaimed float64
	// awaiting: the last call ended and waits for its output tokens.
	awaiting bool

	toolStart time.Time
}

// SetStatsSeed gives the Store the Stats a Session had before, such as
// ones saved in an earlier run; fn runs when the Store first registers the
// Session, under the Store's lock, and must not call the Store.
func (s *Store) SetStatsSeed(fn func(sessionID string) (Stats, bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statsSeed = fn
}

func (st *state) statsWorking(ws protocol.DroidWorkingState) {
	f := &st.stats
	now := st.s.now()
	if ws != protocol.DroidWorkingStateExecutingTool && !f.toolStart.IsZero() {
		f.totals.ToolMs += msBetween(f.toolStart, now)
		f.toolStart = time.Time{}
		st.emit(EventStatsUpdated)
	}
	switch ws {
	case protocol.DroidWorkingStateThinking, protocol.DroidWorkingStateStreamingAssistantMessage:
		if !f.open {
			f.open, f.start, f.first, f.unclaimed = true, now, time.Time{}, 0
		}
	case protocol.DroidWorkingStateExecutingTool:
		if f.toolStart.IsZero() {
			f.toolStart = now
		}
		f.open = false
	default:
		// A call that ends without a message (an error, an interrupt, a
		// prompt to approve a tool) is not counted.
		f.open = false
	}
}

func (st *state) statsFirstToken() {
	f := &st.stats
	if f.open && f.first.IsZero() {
		f.first = st.s.now()
	}
}

func (st *state) statsMessage() {
	f := &st.stats
	if !f.open {
		return
	}
	now := st.s.now()
	f.open = false
	f.totals.Calls++
	f.totals.ModelMs += msBetween(f.start, now)
	last := &CallStats{}
	if !f.first.IsZero() {
		last.TTFTMs = msBetween(f.start, f.first)
		last.DecodeMs = msBetween(f.first, now)
		f.totals.TTFTMs += last.TTFTMs
		f.totals.TTFTCalls++
	}
	f.totals.Last = last
	f.awaiting = true
	if f.unclaimed > 0 {
		st.claimTokens(f.unclaimed)
	}
	st.emit(EventStatsUpdated)
}

// statsUsage gives the output tokens a usage notification adds to the call
// that just ended; it runs before the notification replaces the usage.
func (st *state) statsUsage(n *protocol.SessionTokenUsageChangedNotification) {
	f := &st.stats
	var tokens float64
	if prev := st.usage.Session; prev != nil {
		tokens = n.TokenUsage.OutputTokens - prev.OutputTokens
	} else if lc := n.LastCallTokenUsage; lc != nil && lc.OutputTokens != nil {
		tokens = *lc.OutputTokens
	}
	if tokens <= 0 {
		return
	}
	switch {
	case f.awaiting:
		st.claimTokens(tokens)
		st.emit(EventStatsUpdated)
	case f.open:
		f.unclaimed += tokens
	}
}

func (st *state) claimTokens(tokens float64) {
	f := &st.stats
	f.awaiting, f.unclaimed = false, 0
	last := f.totals.Last
	if last == nil {
		return
	}
	last.OutputTokens = tokens
	if last.DecodeMs >= float64(minDecode.Milliseconds()) {
		f.totals.DecodeMs += last.DecodeMs
		f.totals.DecodeTokens += tokens
	}
}

func msBetween(a, b time.Time) float64 {
	return float64(max(0, b.Sub(a).Milliseconds()))
}
