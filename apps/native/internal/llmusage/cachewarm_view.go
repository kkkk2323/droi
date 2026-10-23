package llmusage

import (
	"fmt"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/proxy"
)

// CacheWarmingView is the cache warming mode and its numbers. Available is
// false while the LLM proxy, which warms, is not running.
type CacheWarmingView struct {
	Available bool
	Mode      string
	Options   []WarmModeOption
	// Summary is the state in one line.
	Summary      string
	Help         string
	Metrics      []MetricView
	SessionsNote string
	Sessions     []WarmSessionRow
	EmptyText    string
}

type WarmModeOption struct {
	Value    string
	Label    string
	Help     string
	Selected bool
}

type WarmSessionRow struct {
	ID        string
	Session   string
	Model     string
	Context   string
	Refreshes string
	Next      string
	State     string
	StateTone Tone
}

// WarmModes are the cache warming modes in the order they are offered.
var WarmModes = []string{proxy.CacheWarmingOff, proxy.CacheWarmingDryRun, proxy.CacheWarmingStreaming, proxy.CacheWarmingIdle, proxy.CacheWarmingSmart}

// warmMode is a mode's label and what it does; ok is false for a mode this
// window does not know.
func warmMode(mode string) (label, help string, ok bool) {
	switch mode {
	case proxy.CacheWarmingOff:
		return l10n.L("Off"), l10n.L("Caches are not renewed, and no request is recorded for warming."), true
	case proxy.CacheWarmingDryRun:
		return l10n.L("Dry run"), l10n.L("Only decides whether to renew and writes the result to the event log. Sends no renewal and spends no quota."), true
	case proxy.CacheWarmingStreaming:
		return l10n.L("While tools run"), l10n.L("Renews caches while Droid runs tools, not while a session is idle."), true
	case proxy.CacheWarmingIdle:
		return l10n.L("Including idle"), l10n.L("Renews while tools run and while a session is idle. Idle renewal has a time limit, 30 minutes by default."), true
	case proxy.CacheWarmingSmart:
		return l10n.L("Smart"), l10n.L("Estimates how long each wait is worth renewing from the past 90 days of sessions. Computed on this computer only; no conversation is uploaded."), true
	}
	return "", "", false
}

func BuildCacheWarming(cw *proxy.CacheWarmingStatus, now time.Time) CacheWarmingView {
	if cw == nil {
		return CacheWarmingView{Summary: l10n.L("Unavailable")}
	}
	v := CacheWarmingView{Available: true, Mode: cw.Mode, Sessions: []WarmSessionRow{}}
	for _, m := range WarmModes {
		label, help, _ := warmMode(m)
		v.Options = append(v.Options, WarmModeOption{Value: m, Label: label, Help: help, Selected: m == cw.Mode})
	}
	if _, help, ok := warmMode(cw.Mode); ok {
		v.Help = help
	} else {
		v.Help = l10n.L("Unknown mode: %s", cw.Mode)
	}
	if cw.Mode == proxy.CacheWarmingSmart {
		if cw.Smart != nil {
			v.Help += l10n.L("%d waits analyzed, updated %s.", cw.Smart.Waits, FormatRelative(cw.Smart.BuiltAt, now))
		} else {
			v.Help += l10n.L("Reading the session history; until it is read, renews as Including idle does.")
		}
	}

	t := cw.Totals
	switch {
	case cw.Mode == proxy.CacheWarmingOff:
		v.Summary = l10n.L("Turned off")
	case t.Failures > 0:
		v.Summary = l10n.L("%d sessions · %d renewals · %d failures", len(cw.Sessions), t.Refreshes, t.Failures)
	default:
		v.Summary = l10n.L("%d sessions · %d renewals", len(cw.Sessions), t.Refreshes)
	}

	failTone := ToneSecondary
	if t.Failures > 0 {
		failTone = ToneDanger
	}
	v.Metrics = []MetricView{
		{Title: l10n.L("Renewals"), Value: fmt.Sprintf("%d", t.Refreshes), Tone: TonePrimary, Note: l10n.L("%s tokens read from cache", FormatTokens(t.CacheReadTokens))},
		{Title: l10n.L("Failures"), Value: fmt.Sprintf("%d", t.Failures), Tone: failTone, Note: l10n.L("%s tokens written to cache", FormatTokens(t.CacheWriteTokens))},
		{Title: l10n.L("Spent"), Value: FormatTokens(int64(t.SpentUnits)), Tone: TonePrimary, Note: l10n.L("Tokens billed for renewals")},
		{Title: l10n.L("Saved (estimate)"), Value: FormatTokens(int64(t.SavedUnits)), Tone: ToneGreen, Note: l10n.L("Cache tokens not written again")},
	}

	v.SessionsNote = fmt.Sprintf("%d", len(cw.Sessions))
	for _, s := range cw.Sessions {
		row := WarmSessionRow{
			ID:        s.SessionID,
			Session:   shortID(s.SessionID),
			Model:     s.Model,
			Context:   FormatTokens(s.PromptTokens),
			Refreshes: fmt.Sprintf("%d", s.Refreshes),
			Next:      "—",
			State:     s.State,
		}
		if !s.NextWarmAt.IsZero() {
			row.Next = FormatCountdown(s.NextWarmAt, now)
		}
		switch s.State {
		case "scheduled":
			row.State = l10n.L("Waiting")
		case "refreshing":
			row.State, row.StateTone = l10n.L("Renewing"), ToneAccent
		}
		v.Sessions = append(v.Sessions, row)
	}
	if cw.Mode == proxy.CacheWarmingOff {
		v.EmptyText = l10n.L("Cache warming is off. Choose another mode to start renewing.")
	} else {
		v.EmptyText = l10n.L("No session to warm yet. A session with more than 50K tokens of context shows here after it has sent a request.")
	}
	return v
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
