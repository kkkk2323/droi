package llmusage

import (
	"strings"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// Credits are Factory's billable tokens, what the quota windows count:
//
//	credits = mult × (input + 1.25×cacheWrite + crMult×cacheRead + outMult×output)
//
// Factory does not publish the size of a window. These sizes were fitted
// from droid-proxy's request log against the keys' usedPercent snapshots
// (2026-10, 9 to 15 windows each, error under 4%). The same traffic moves
// the weekly window twice as far as the monthly one, so a month holds about
// two weeks of quota, not four.
const (
	FiveHourStandardCredits = 12_950_000
	WeeklyStandardCredits   = 38_900_000
	MonthlyStandardCredits  = 77_100_000
)

// WindowCredits is the Standard credits behind percent of a window; ok is
// false for the 5-hour window, whose size is the least certain.
func WindowCredits(w Window, percent float64) (float64, bool) {
	switch w {
	case Weekly:
		return percent / 100 * WeeklyStandardCredits, true
	case Monthly:
		return percent / 100 * MonthlyStandardCredits, true
	}
	return 0, false
}

// TokenWeight is a model's credit weight per input and per output token.
type TokenWeight struct {
	Model         string
	Input, Output float64
}

// StandardTokenWeights are the Factory weights of the common Standard
// models, from the droid CLI's cost table.
var StandardTokenWeights = []TokenWeight{
	{"Opus 5.5", 1.6, 8},
	{"Fable 5.1", 4, 20},
	{"Sonnet 5.5", 0.8, 4},
	{"Haiku 4.5", 0.4, 2},
}

// TokenEquivalents spells out what credits buy on each common model.
func TokenEquivalents(credits float64) string {
	lines := []string{l10n.L("About %s credits, which is:", FormatCredits(credits))}
	for _, w := range StandardTokenWeights {
		lines = append(lines, l10n.L("%s: %s input tokens, or %s output tokens", w.Model, FormatCredits(credits/w.Input), FormatCredits(credits/w.Output)))
	}
	lines = append(lines, l10n.L("All cache reads: about %s tokens on Opus 5.5", FormatCredits(credits/0.08)))
	lines = append(lines, l10n.L("One key's weekly quota is about %s credits, estimated from the request log.", FormatCredits(WeeklyStandardCredits)))
	return strings.Join(lines, "\n")
}
