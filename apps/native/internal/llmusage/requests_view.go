package llmusage

import (
	"fmt"
	"strings"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
)

type RequestRowView struct {
	ID            string
	Time          string
	TimeHelp      string
	ProviderMark  string
	ProviderClass string
	ProviderHelp  string
	Model         string
	// Kind names what a call other than a model request was for.
	Kind         string
	Status       string
	StatusTone   Tone
	Stream       bool
	StatusHelp   string
	Input        Cell
	Cache        Cell
	Output       Cell
	Credits      Cell
	Duration     string
	DurationTone Tone
	TTFB         string
	TokensPerSec Cell
}

// RequestID identifies a record across restarts: sequence numbers start
// again with the proxy while the history spans runs.
func RequestID(r diag.RequestRecord) string {
	return fmt.Sprintf("%d@%d", r.Seq, r.Time.UnixNano())
}

func BuildRequestRow(r diag.RequestRecord) RequestRowView {
	local := r.Time.Local()
	row := RequestRowView{
		ID:       RequestID(r),
		Time:     local.Format("01/02 15:04:05"),
		TimeHelp: l10n.L("%[1]d-%[2]d-%[3]d %[4]s %[5]s", local.Year(), int(local.Month()), local.Day(), Weekday(local.Weekday()), local.Format("15:04:05 MST")),
		Model:    orDash(r.Model),
		Status:   fmt.Sprintf("%d", r.Status),
		Stream:   r.Stream,
	}
	if r.Kind != "" {
		row.Kind = KindLabel(r.Kind)
	}
	row.ProviderMark, row.ProviderClass, row.ProviderHelp = providerMark(r.Provider)
	row.StatusTone = requestStatusTone(r)
	row.StatusHelp = requestStatusHelp(r)

	cacheHelp := l10n.L("Share of the input read from cache")
	noUsage := l10n.L("Factory returned no usage")
	if u := r.Usage; u != nil {
		rate, hasRate := cacheRate(*u)
		row.Input = numeric(FormatTokens(u.Input), true, inputHelp(*u), ToneSecondary)
		cacheTone := ToneSecondary
		if hasRate && rate >= 50 {
			cacheTone = ToneGreen
		}
		row.Cache = numeric(FormatPercent(rate), hasRate, cacheHelp, cacheTone)
		row.Output = numeric(FormatTokens(u.Output), true, outputHelp(*u), ToneSecondary)
		var t Totals
		t.add(r)
		row.Credits = creditsCell(t)
	} else {
		row.Input = numeric("", false, noUsage, ToneSecondary)
		row.Cache = numeric("", false, cacheHelp, ToneSecondary)
		row.Output = numeric("", false, noUsage, ToneSecondary)
		row.Credits = numeric("", false, noUsage, ToneSecondary)
	}

	row.Duration = FormatDuration(r.DurationMs)
	switch {
	case r.DurationMs >= 60_000:
		row.DurationTone = ToneDanger
	case r.DurationMs >= 20_000:
		row.DurationTone = ToneWarning
	}
	if r.TTFBMs > 0 {
		row.TTFB = l10n.L("%s first byte", FormatDuration(r.TTFBMs))
	}
	tps, hasTPS := TokensPerSecond(r)
	row.TokensPerSec = numeric(fmt.Sprintf("%.0f", tps), hasTPS, l10n.L("Output tokens per second: output tokens ÷ the time after the first byte"), ToneSecondary)
	return row
}

func cacheRate(u diag.TokenUsage) (float64, bool) {
	if u.Input == 0 {
		return 0, false
	}
	return float64(u.CacheRead) / float64(u.Input) * 100, true
}

// providerMark is a two-letter vendor mark so rows scan quickly.
func providerMark(provider string) (mark, class, help string) {
	help = provider
	if provider == "" {
		help = l10n.L("Unknown provider")
	}
	switch provider {
	case "anthropic":
		return "AN", "anthropic", help
	case "openai":
		return "OA", "openai", help
	case "google", "gemini":
		return "GG", "google", help
	case "":
		return "—", "other", help
	}
	r := []rune(provider)
	if len(r) > 2 {
		r = r[:2]
	}
	return strings.ToUpper(string(r)), "other", help
}

func requestStatusTone(r diag.RequestRecord) Tone {
	if r.Error != "" {
		return ToneDanger
	}
	switch {
	case r.Status < 300:
		return ToneGreen
	case r.Status == 402 || r.Status == 429:
		return ToneWarning
	}
	return ToneDanger
}

func requestStatusHelp(r diag.RequestRecord) string {
	var parts []string
	if r.Key != "" {
		parts = append(parts, l10n.L("key %s", r.Key))
	}
	if r.ErrorDetail != "" {
		parts = append(parts, r.ErrorDetail)
	}
	if r.Error != "" {
		parts = append(parts, r.Error)
	}
	if r.Pool != "" && r.RequestedPool != "" && r.Pool != r.RequestedPool {
		parts = append(parts, l10n.L("Asked for the %s pool, used the %s pool", r.RequestedPool, r.Pool))
	}
	bodies := l10n.L("Request body %s · response body %s", FormatBytes(r.RequestBytes), FormatBytes(r.Bytes))
	if r.Stream {
		bodies += " · " + l10n.L("streamed")
	}
	return strings.Join(append(parts, bodies), "\n")
}

func inputHelp(u diag.TokenUsage) string {
	parts := []string{l10n.L("%d input tokens, cache included", u.Input)}
	if u.CacheRead > 0 {
		parts = append(parts, l10n.L("%d read from cache", u.CacheRead))
	}
	if u.CacheWrite > 0 {
		parts = append(parts, l10n.L("%d written to cache", u.CacheWrite))
	}
	return strings.Join(parts, " · ")
}

func outputHelp(u diag.TokenUsage) string {
	parts := []string{l10n.L("%d output tokens", u.Output)}
	if u.Reasoning > 0 {
		parts = append(parts, l10n.L("%d of them reasoning", u.Reasoning))
	}
	return strings.Join(parts, " · ")
}
