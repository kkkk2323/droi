package llmusage

import (
	"fmt"
	"math"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/dashkit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

// RunwayView is today's Standard spend against what the pool can give a
// day, and how long it lasts at that spend. Amounts are credits.
type RunwayView struct {
	Available bool
	Empty     string
	Help      string
	// Metrics are, in order: today so far, today's projection, what is left
	// for today, and how long today's spend lasts.
	Metrics []MetricView
	Budget  BudgetView
	Lines   []RunwayLine
	// Chart is the spend of the last days, one bar each, against the daily
	// limit; its values are percent of one key's month.
	Chart dashkit.ChartView
}

// BudgetView is today's spend as a share of the daily limit, for the menu
// bar panel.
type BudgetView struct {
	Spent      string
	SpentShare string
	Limit      string
	Bar        Bar
	// Usual marks what is usually spent by this hour, 0…100 of the limit;
	// HasUsual is false without a whole day of log.
	Usual    float64
	HasUsual bool
	Note     string
	NoteTone Tone
}

// RunwayLine is a pace and what the pool holds after RunwayDays at it, with
// Note the room against the daily limit.
type RunwayLine struct {
	Label string
	Value string
	Note  string
	Tone  Tone
}

// hotFactor: today is "more than usual" past this multiple of the usual
// spend by this hour, and by at least hotMinPercent.
const (
	hotFactor     = 1.25
	hotMinPercent = 5.0
)

func BuildRunway(pool []keys.KeyState, spend *Spend, now time.Time) RunwayView {
	r, ok := NewRunway(pool, now)
	v := RunwayView{
		Help:  l10n.L("Only the Standard pool is counted. Credits are converted from monthly quota percentages and are estimates.\nUsed today: the monthly quota all keys spent from midnight until now. Usually by now: the average at this time of day over the last 7 days.\nWhole day: used today, plus the average use after this time of day over the last 7 days.\nDaily limit: spending this much a day makes the Standard pool last exactly 30 days. It counts keys not started yet and monthly quotas that reset within 30 days.\nThe percentages in brackets are of the whole Standard pool's month: the monthly quotas of %d keys add up to 100%%.", r.Keys()),
		Empty: l10n.L("No quota log yet. Once the Key Pool has run for a while, today's use and how long the pool lasts show here."),
	}
	if spend == nil || !ok {
		return v
	}
	s := *spend
	v.Available = true
	limit := r.DailyLimit()
	projected, hasProjection := s.ProjectedToday()
	// share is v as a part of the whole pool's month; amount is v in
	// credits with that share after it.
	share := func(v float64) string { return shareText(v / 100 / float64(r.Keys())) }
	amount := func(v float64) string { return l10n.L("%s (%s)", monthCredits(v), share(v)) }

	today := MetricView{Title: l10n.L("Used today"), Value: monthCredits(s.Today), Sub: share(s.Today), Tone: TonePrimary, Note: l10n.L("Compares once the log holds a whole day")}
	if s.Days > 0 {
		today.Note = l10n.L("Usually by now %s", amount(s.UsualByNow))
		if s.Today > s.UsualByNow*hotFactor && s.Today-s.UsualByNow >= hotMinPercent {
			today.Tone = ToneWarning
			today.Note = l10n.L("%s more than usual by now", amount(s.Today-s.UsualByNow))
		}
	}

	whole := MetricView{Title: l10n.L("Whole day"), Value: "—", Tone: ToneTertiary, Note: l10n.L("Projects once the log holds a whole day")}
	if hasProjection {
		whole = MetricView{Title: l10n.L("Whole day"), Value: l10n.L("about %s", monthCredits(projected)), Sub: share(projected), Tone: TonePrimary, Note: l10n.L("The rest of the day at the usual use")}
		if projected > limit {
			whole.Tone = ToneWarning
		}
	}

	room := math.Max(limit-s.Today, 0)
	left := MetricView{Title: l10n.L("Left for today"), Value: monthCredits(room), Sub: share(room), Tone: TonePrimary, Note: l10n.L("Daily limit %s", amount(limit))}
	switch {
	case s.Today > limit:
		room = s.Today - limit
		left = MetricView{Title: l10n.L("Over today"), Value: monthCredits(room), Sub: share(room), Tone: ToneDanger, Note: l10n.L("Daily limit %s", amount(limit))}
	case hasProjection && projected > limit:
		left.Tone = ToneWarning
	}

	rate, rateLabel := s.Today, l10n.L("At today's use so far")
	if hasProjection {
		rate, rateLabel = projected, l10n.L("At today's pace")
	}
	lasts := MetricView{Title: rateLabel}
	lasts.Value, lasts.Note, lasts.Tone = runwayOutcome(r, rate, now, amount)
	v.Metrics = []MetricView{today, whole, left, lasts}

	v.Budget = BudgetView{Spent: monthCredits(s.Today), SpentShare: share(s.Today), Limit: amount(limit), Bar: Bar{Value: percentOf(s.Today, limit), Tone: ToneAccent}}
	if s.Days > 0 {
		v.Budget.Usual, v.Budget.HasUsual = percentOf(s.UsualByNow, limit), true
	}
	v.Budget.Note, v.Budget.NoteTone = l10n.L("%s left for today", amount(room)), ToneSecondary
	switch {
	case s.Today > limit:
		v.Budget.Bar.Tone = ToneDanger
		v.Budget.Note, v.Budget.NoteTone = l10n.L("%s over today's limit", amount(room)), ToneDanger
	case hasProjection && projected > limit:
		v.Budget.Bar.Tone = ToneWarning
		v.Budget.Note, v.Budget.NoteTone = l10n.L("%s left for today; at the usual pace you will go over", amount(room)), ToneWarning
	}

	line := func(label string, perDay float64) RunwayLine {
		l := RunwayLine{Label: l10n.L("%s (%s a day · %s)", label, monthCredits(perDay), share(perDay))}
		var date string
		date, _, l.Tone = runwayOutcome(r, perDay, now, amount)
		if _, ok := r.Lasts(perDay); ok {
			l.Value = l10n.L("In %d days, %s left", RunwayDays, amount(r.LeftAfter(perDay)))
			l.Note = l10n.L("%s more a day still fits", amount(limit-perDay))
		} else {
			l.Value = l10n.L("Runs out around %s", date)
			l.Note = l10n.L("Use %s less a day to last %d days", amount(perDay-limit), RunwayDays)
		}
		return l
	}
	v.Lines = append(v.Lines, line(rateLabel, rate))
	if s.HasRecent {
		v.Lines = append(v.Lines, line(l10n.L("At the last 7 days' average"), s.Recent))
	}

	v.Chart = buildChart(s.Daily, limit, amount)
	return v
}

// percentOf is v as a percent of of; a limit of zero is reached at once.
func percentOf(v, of float64) float64 {
	if of <= 0 {
		return 100
	}
	return v / of * 100
}

// shareText is a share of the pool's month (0.012 is 1.2% of it): one
// decimal under 10%, whole points above.
func shareText(fraction float64) string {
	p := fraction * 100
	if math.Abs(p) < 10 {
		return fmt.Sprintf("%.1f%%", p)
	}
	return fmt.Sprintf("%.0f%%", p)
}

// runwayOutcome is how long the pool lasts at perDay: the date it runs out
// ("11/7") or that it lasts the whole runway.
func runwayOutcome(r Runway, perDay float64, now time.Time, amount func(float64) string) (value, note string, tone Tone) {
	days, ok := r.Lasts(perDay)
	if ok {
		return l10n.L("Lasts"), l10n.L("In %d days, %s left", RunwayDays, amount(r.LeftAfter(perDay))), ToneGreen
	}
	tone = ToneWarning
	if days < 7 {
		tone = ToneDanger
	}
	out := now.Add(time.Duration(days * 24 * float64(time.Hour)))
	return out.Local().Format("1/2"), l10n.L("Runs out in about %s", FormatSpan(out.Sub(now))), tone
}

// Weekday is a day's name.
func Weekday(d time.Weekday) string {
	switch d {
	case time.Monday:
		return l10n.L("Monday")
	case time.Tuesday:
		return l10n.L("Tuesday")
	case time.Wednesday:
		return l10n.L("Wednesday")
	case time.Thursday:
		return l10n.L("Thursday")
	case time.Friday:
		return l10n.L("Friday")
	case time.Saturday:
		return l10n.L("Saturday")
	}
	return l10n.L("Sunday")
}

func buildChart(days []DaySpend, limit float64, amount func(float64) string) dashkit.ChartView {
	// The axis counts credits: one of the values, a percent of a key's
	// month, is Scale of them.
	c := dashkit.ChartView{Limit: limit, LimitLabel: l10n.L("Daily limit %s", amount(limit)), Top: limit, Scale: MonthlyStandardCredits / 100, Tick: FormatCredits}
	for i, d := range days {
		today := i == len(days)-1
		help := l10n.L("%s %s: %s", d.Day.Format("1/2"), Weekday(d.Day.Weekday()), amount(d.Spent))
		if today {
			help = l10n.L("Today so far: %s", amount(d.Spent))
		}
		over := d.Spent > limit
		if over {
			help = l10n.L("%s, over the daily limit", help)
		}
		c.Bars = append(c.Bars, dashkit.ChartBar{Value: d.Spent, Today: today, Over: over, Help: help})
		c.Top = math.Max(c.Top, d.Spent)
	}
	c.Top *= 1.1
	c.Label = l10n.L("Last %d days", len(days))
	if len(days) > 0 {
		c.First, c.Last = days[0].Day.Format("1/2"), l10n.L("Today")
	}
	return c
}
