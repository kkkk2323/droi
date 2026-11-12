package llmusage

import (
	"math"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
)

// LoggedCredits are what the request log holds of some Sessions' calls.
type LoggedCredits struct {
	Calls int
	// Credits are the calls priced as Factory bills them (see Credits).
	Credits float64
	// Recount is what droid's factoryCredits adds to Credits: droid counts a
	// Gemini call's cache reads in its input tokens as well, and prices them
	// again at the full input weight. Factory's billing counter does not
	// (measured with droid 1.7.1 and 1.9.0 against chat-usage's userTokens).
	Recount float64
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
			continue
		}
		out.Calls++
		out.Credits += c
		if geminiCall(r) {
			w, _ := weightOf(r.Model, r.Time)
			out.Recount += w.Input * float64(r.Usage.CacheRead)
		}
	}
	return out
}

func geminiCall(r diag.RequestRecord) bool {
	return r.Provider == "google" || r.Provider == "" && strings.HasPrefix(strings.ToLower(r.Model), "gemini-")
}

// Recounted is the Daemon's credits for calls the log holds, less droid's
// recount, when that matches the log better than daemon itself does; it
// keeps daemon as it is otherwise, for a droid that stops recounting, or a
// log that misses calls.
func (l LoggedCredits) Recounted(daemon float64) (float64, bool) {
	fixed := daemon - l.Recount
	if l.Recount <= 0 || fixed < 0 || math.Abs(fixed-l.Credits) >= math.Abs(daemon-l.Credits) {
		return daemon, false
	}
	return fixed, true
}
