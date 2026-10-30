package llmusage

import (
	"strings"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// Credits are Factory's billable tokens, what the quota windows count:
//
//	credits = mult × (input + 1.25×cacheWrite + crMult×cacheRead + outMult×output)
//
// Factory does not publish the size of a window. These sizes, a Pro key's,
// were fitted from droid-proxy's request log against the keys' usedPercent
// snapshots (2026-10, 9 to 15 windows each, error under 4%); a bigger plan's
// are its keys.PlanScale times them. The same traffic moves
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

// TokenWeight is a model's credit weight per input, output and cache-read
// token; a cache write weighs 1.25 input tokens. ID is the start of the
// model's ID in requests, and the first prefix match wins, so a longer ID
// comes first and a variant with its own price (a "-fast") needs its own row
// above its base model. Common marks the models TokenEquivalents spells out:
// a few, so that its tooltip stays short.
type TokenWeight struct {
	Model, ID                string
	Input, Output, CacheRead float64
	Common                   bool
}

// StandardTokenWeights are Factory's weights of the models the request log
// has shown, from the droid CLI's cost table: an input token weighs the
// model's multiplier, an output token that times its output multiplier, and
// a cache read that times its cache-read multiplier. The CLI applies a
// running promotion on top, which this table leaves out, and a model that is
// not here is counted as unpriced.
var StandardTokenWeights = []TokenWeight{
	{"Opus 5.5", "claude-opus-5-5", 1.6, 8, 0.08, true},
	{"Opus 5", "claude-opus-5", 2, 10, 0.2, false},
	{"Fable 5.1", "claude-fable-5.1", 4, 20, 0.1, true},
	{"Sonnet 5.5", "claude-sonnet-5-5", 0.8, 4, 0.04, true},
	{"Haiku 5.5", "claude-haiku-5-5", 0.04, 0.2, 0.004, false},
	{"Haiku 4.5", "claude-haiku-4-5", 0.4, 2, 0.04, true},
	{"DeepSeek V4.1 Flash", "deepseek-v4.1-flash", 0.12, 0.48, 0.00288, false},
	{"DeepSeek V4 Flash 0731", "deepseek-v4-flash-0731", 0.176, 0.528, 0.005632, false},
	{"GLM-5.3-Flash", "glm-5.3-flash", 0.06, 0.2004, 0.012, false},
	{"GLM-5.3", "glm-5.3", 0.56, 1.764, 0.056, false},
	{"GPT-6.1 Sol", "gpt-6.1-sol", 0.8, 4, 0.04, false},
	{"GPT-6 Astra", "gpt-6-astra", 4, 20, 0.4, false},
	{"GPT-6 Luna", "gpt-6-luna", 0.04, 0.2, 0.004, false},
	{"GPT-6 Sol", "gpt-6-sol", 0.8, 4, 0.08, false},
	{"GPT-5.6 Luna", "gpt-5.6-luna", 0.08, 0.48, 0.008, false},
	{"GPT-5.6 Sol", "gpt-5.6-sol", 2, 10, 0.2, false},
	{"Gemini 4 Argon", "gemini-4-argon", 1.6, 8, 0.08, false},
	{"Gemini 3.8 Flash", "gemini-3.8-flash", 0.6, 3, 0.06, false},
	{"Kimi K3", "kimi-k3", 1.2, 6, 0.12, false},
	{"Qwen3.8 Max", "qwen3.8-max", 0.8, 2.4, 0.08, false},
	{"Muse Spark 1.2", "muse-spark-1.2", 0.5, 1.7, 0.06, false},
}

// TokenEquivalents spells out what credits buy on each common model.
func TokenEquivalents(credits float64) string {
	lines := []string{l10n.L("About %s credits, which is:", FormatCredits(credits))}
	for _, w := range StandardTokenWeights {
		if !w.Common {
			continue
		}
		lines = append(lines, l10n.L("%s: %s input tokens, or %s output tokens", w.Model, FormatCredits(credits/w.Input), FormatCredits(credits/w.Output)))
	}
	lines = append(lines, l10n.L("All cache reads: about %s tokens on Opus 5.5", FormatCredits(credits/0.08)))
	lines = append(lines, l10n.L("A Pro key's weekly quota is about %s credits, estimated from the request log; a Plus key's holds 5 times that, a Max key's 10.", FormatCredits(WeeklyStandardCredits)))
	return strings.Join(lines, "\n")
}
