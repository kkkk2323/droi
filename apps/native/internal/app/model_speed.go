package app

import (
	"strconv"
	"time"

	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
)

// liveRefresh is how often the speed under the composer is read again while
// a model call streams.
const liveRefresh = 500 * time.Millisecond

// modelCalls is what the Key Pool measured of Session id's model calls, and
// the one under way; ok is false when it measured none, as when the Key Pool
// is off or the window follows another computer, whose model calls go
// through that computer's own Key Pool.
func (a *App) modelCalls(id string) (calls diag.SessionCalls, live *diag.LiveCall, ok bool) {
	if a.cfg.ModelCalls != nil {
		return a.cfg.ModelCalls(id)
	}
	pool := a.keyPool()
	if pool == nil || a.following() {
		return diag.SessionCalls{}, nil, false
	}
	return pool.Session(id)
}

// liveRate is the speed of a call under way since its first token; 0 before
// it has one.
func liveRate(l *diag.LiveCall, now time.Time) float64 {
	if l == nil || l.FirstToken.IsZero() || l.Output <= 0 {
		return 0
	}
	d := now.Sub(l.FirstToken)
	if d < 300*time.Millisecond {
		return 0
	}
	return l.Output / d.Seconds()
}

type speedRow struct{ label, value string }

type speedSection struct {
	title string
	rows  []speedRow
}

// speedSections are the details of the speed under the composer from what
// the Key Pool measured; the time in tools comes from the Session's own
// stats, since the Key Pool sees only the model calls.
func speedSections(calls diag.SessionCalls, live *diag.LiveCall, st session.Stats, now time.Time) []speedSection {
	var out []speedSection
	if live != nil {
		rows := []speedRow{{L("Model"), live.Model + " · " + live.Key}}
		if live.FirstToken.IsZero() {
			rows = append(rows, speedRow{L("First token after"), L("Waiting %s", formatSeconds(msSince(live.Start, now)))})
		} else {
			rows = append(rows,
				speedRow{L("First token after"), formatSeconds(float64(live.FirstToken.Sub(live.Start).Milliseconds()))},
				speedRow{L("Speed"), formatRate(liveRate(live, now)) + " tok/s"},
				speedRow{L("Output tokens"), "≈ " + groupDigits(live.Output)})
		}
		if live.Usage.Input > 0 {
			rows = append(rows, speedRow{L("Input tokens"), inputValue(live.Usage)})
		}
		out = append(out, speedSection{L("Now"), rows})
	}
	if calls.Calls > 0 {
		last := calls.Last
		rows := []speedRow{{L("Model"), last.Model + " · " + last.Key},
			{L("First token after"), formatSeconds(float64(last.TTFTMs))},
			{L("Speed"), formatRate(last.TokensPerSecond()) + " tok/s"}}
		if u := last.Usage; u != nil {
			output := groupDigits(float64(u.Output))
			if u.Reasoning > 0 {
				output = L("%s · %s reasoning", output, groupDigits(float64(u.Reasoning)))
			}
			rows = append(rows, speedRow{L("Output tokens"), output}, speedRow{L("Input tokens"), inputValue(*u)})
			if u.CacheWrite > 0 {
				rows = append(rows, speedRow{L("Cache written"), groupDigits(float64(u.CacheWrite))})
			}
		}
		rows = append(rows, speedRow{L("Took"), formatDuration(float64(last.DurationMs))})
		out = append(out, speedSection{L("Last model call"), rows})

		out = append(out, speedSection{L("This Session"), []speedRow{
			{L("Model calls"), itoa(calls.Calls)},
			{L("Input tokens"), inputValue(calls.Usage)},
			{L("Output tokens"), groupDigits(float64(calls.Usage.Output))},
			{L("First token after, on average"), formatSeconds(calls.MeanTTFTMs())},
			{L("Average speed"), formatRate(calls.TokensPerSecond()) + " tok/s"},
			{L("Time in the model"), formatSpan(float64(calls.DurationMs))},
			{L("Time in tools"), formatSpan(st.ToolMs)},
		}})
	}
	return out
}

// inputValue is the input tokens and the share of them read from the
// prompt cache: 100,000 · 98% cached.
func inputValue(u diag.TokenUsage) string {
	return L("%s · %s cached", groupDigits(float64(u.Input)), percent(u.CacheHit()))
}
func msSince(t, now time.Time) float64 { return float64(now.Sub(t).Milliseconds()) }

// formatDuration is a call's length: 0.8s, 12s, 1m 30s.
func formatDuration(ms float64) string {
	if ms < 60_000 {
		return formatSeconds(ms)
	}
	return formatSpan(ms)
}

// percent is a share as a whole percentage: 98%.
func percent(f float64) string { return strconv.Itoa(int(f*100+0.5)) + "%" }
