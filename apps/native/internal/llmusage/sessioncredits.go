package llmusage

import (
	"math"
	"time"
)

// LoggedCredits are what the request log holds of some Sessions' calls.
type LoggedCredits struct {
	Calls int
	// Unpriced counts calls with tokens whose model has no known weight.
	Unpriced int
	// Credits are the priced calls as Factory bills them (see Credits).
	Credits float64
	// Output, CacheRead and CacheWrite are the calls' tokens, as the
	// responses reported them.
	Output, CacheRead, CacheWrite int64
}

// SessionCredits adds up the calls of the Sessions in ids made from from
// to to; a zero time leaves that end open.
func (l *RequestLog) SessionCredits(ids map[string]bool, from, to time.Time) LoggedCredits {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out LoggedCredits
	for _, r := range l.records {
		if r.Usage == nil || !ids[r.SessionID] || !from.IsZero() && r.Time.Before(from) || !to.IsZero() && r.Time.After(to) {
			continue
		}
		c, ok := Credits(r.Model, *r.Usage, r.Time)
		if !ok {
			if !r.Usage.IsZero() {
				out.Unpriced++
			}
			continue
		}
		out.Calls++
		out.Credits += c
		out.Output += r.Usage.Output
		out.CacheRead += r.Usage.CacheRead
		out.CacheWrite += r.Usage.CacheWrite
	}
	return out
}

// Covers says the log holds every call behind a Daemon figure with these
// token counts, as droid read them from the responses, and priced them all.
// A log that misses calls (an archive gone, a Session older than the Key
// Pool) misses output tokens; one written before the proxy read a field
// (OpenAI's cache writes) misses those.
func (l LoggedCredits) Covers(output, cacheRead, cacheWrite float64) bool {
	if l.Calls == 0 || l.Unpriced > 0 || output <= 0 {
		return false
	}
	same := func(logged int64, daemon float64) bool {
		return math.Abs(float64(logged)-daemon) <= max(0.01*daemon, 1)
	}
	return same(l.Output, output) && same(l.CacheRead, cacheRead) && same(l.CacheWrite, cacheWrite)
}
