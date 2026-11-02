package llmusage

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

// onPaceBand: a recent spend within this share of the even pace is on it.
const onPaceBand = 0.1

// CycleView is the Standard pool's live months until they reset: how far
// through them it is, the even pace that spends what they can still give
// before each resets, and how the last days' spend compares with it.
// Amounts are credits.
type CycleView struct {
	// Available is false while no key's month is under way.
	Available bool
	// Stage is how much of the live months' time and quota is gone;
	// Waiting counts the keys whose month has not started, which are left
	// out.
	Stage   string
	Waiting string
	// Bar is what the live months have left, Even where it would be at an
	// even pace through them.
	Bar  Bar
	Even float64
	// Target is the even pace a day; Verdict what the recent spend does
	// against it, and Advice the spend to change to.
	Target  string
	Verdict string
	Advice  string
	// Short is Advice in a few words, for the menu bar panel; Tone colours
	// it and Verdict.
	Short string
	Tone  Tone
	Help  string
}

// BuildCycle reads the routable keys' live Standard months. spend, when it
// holds the last days' average, is compared with the even pace.
func BuildCycle(pool []keys.KeyState, spend *Spend, now time.Time) CycleView {
	v := CycleView{
		Help: l10n.L("Only the Standard pool's live months count: keys whose month has started. A waiting key starts a new month on its next request, so it is left out.\nEven pace: the least spend a day that uses what the live months can still give before each resets, deadline by deadline. Spending more uses them up early; spending less loses the rest at the resets.\nThe bar is what the live months have left; the tick is where an even pace through their months would be.\nThe last 7 days' average comes from the Key Pool's quota log. Credits are converted from monthly quota percentages and are estimates."),
		Tone: ToneSecondary,
	}
	var items []expiringItem
	capacity, left, elapsed := 0.0, 0.0, 0.0
	waiting := 0
	for _, key := range pool {
		limits := PoolOf(key, Standard)
		monthly := WindowOf(limits, Monthly)
		if key.Status != "ok" || monthly == nil {
			continue
		}
		if !IsLive(monthly, now) {
			waiting++
			continue
		}
		o, ok := MonthlyOutlookOf(limits, now, 0)
		gone, paced := Elapsed(monthly, Monthly, now)
		if !ok || !paced {
			continue
		}
		scale := Scale(key)
		capacity += 100 * scale
		left += o.Left * scale
		elapsed += gone * scale
		if o.Left > minExpiringPercent {
			items = append(items, expiringItem{key, o, scale})
		}
	}
	if capacity == 0 {
		return v
	}
	v.Available = true
	remaining, gone := left/capacity*100, elapsed/capacity*100
	v.Bar, v.Even = remainingBar(remaining), clamp(100-gone)
	v.Stage = l10n.L("Live months: %s of the time gone, %s of the quota used", FormatPercent(gone), FormatPercent(100-remaining))
	if waiting > 0 {
		v.Waiting = l10n.L("%s waiting, not counted", KeyCount(waiting))
	}
	if len(items) == 0 {
		v.Target = l10n.L("The live months are used up")
		return v
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].o.End.Before(items[j].o.End) })
	even := requiredPace(items, now)
	v.Target = l10n.L("Even pace to the resets: %s a day", monthCredits(even))
	if spend == nil || !spend.HasRecent {
		v.Verdict = l10n.L("Compares once the log holds a whole day")
		return v
	}
	rate := spend.Recent
	label := l10n.L("At the last 7 days' average")
	switch {
	case rate < even*(1-onPaceBand):
		lost := lossAt(items, rate, now)
		v.Verdict = l10n.L("%s (%s a day): %s is lost at the resets", label, monthCredits(rate), monthCredits(lost))
		if lost >= minLossPercent {
			v.Tone = ToneWarning
		}
		if rate < 1e-6 {
			v.Advice, v.Short = l10n.L("Use at least %s a day to lose none", monthCredits(even)), l10n.L("Speed up")
			return v
		}
		times := multiple(even / rate)
		v.Advice = l10n.L("Speed up to %s a day (%s the last 7 days' average, %s more a day)", monthCredits(even), times, monthCredits(even-rate))
		v.Short = l10n.L("Speed up to %s", times)
	case rate > even*(1+onPaceBand):
		total := 0.0
		for _, it := range items {
			total += it.usable()
		}
		out := now.Add(time.Duration(total / rate * 24 * float64(time.Hour)))
		last := items[len(items)-1].o.End
		v.Verdict = l10n.L("%s (%s a day): the live months run out around %s, before the last reset on %s", label, monthCredits(rate), out.Local().Format("1/2"), last.Local().Format("1/2"))
		times := multiple(even / rate)
		v.Advice = l10n.L("Slow down to %s a day (%s the last 7 days' average, %s less a day) to last until the resets", monthCredits(even), times, monthCredits(rate-even))
		v.Short, v.Tone = l10n.L("Slow down to %s", times), ToneWarning
	default:
		v.Verdict = l10n.L("%s (%s a day): on an even pace", label, monthCredits(rate))
		v.Short, v.Tone = l10n.L("On pace"), ToneGreen
	}
	return v
}

// lossAt is what the live months lose at their resets when perDay is spent
// on them, soonest end first: by each end no more than perDay times the days
// until it can have been spent. items are sorted by end, and the days are
// counted as requiredPace counts them.
func lossAt(items []expiringItem, perDay float64, now time.Time) float64 {
	spent, lost := 0.0, 0.0
	for _, it := range items {
		days := math.Max(it.o.End.Sub(now).Hours(), 1) / 24
		take := math.Min(it.usable(), math.Max(perDay*days-spent, 0))
		spent += take
		lost += it.left() - take
	}
	return lost
}

// multiple is a ratio as "1.5×".
func multiple(v float64) string { return fmt.Sprintf("%.1f×", v) }
