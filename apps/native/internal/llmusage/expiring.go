package llmusage

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

const (
	// minExpiringPercent: the Key Pool stops routing a key at 98% used, so
	// the last 2% of a month cannot be spent and is not listed.
	minExpiringPercent = 2.0
	// expiringHorizon lists months that end this soon, plus any key that
	// needs attention earlier.
	expiringHorizon = keys.ExpiringWithin
	// alertRest is the warning lead: a key is flagged when resting it this
	// long would lose part of its month.
	alertRest = 48 * time.Hour
	// minLossPercent hides losses too small to act on.
	minLossPercent = 1.0
	// expiringGroupSpan merges deadlines this close into one group within
	// the next day; later deadlines group by date.
	expiringGroupSpan = 30 * time.Minute
)

// ExpiringView is the monthly Standard quota each key still has before its
// month ends. Spending a key shrinks its number; a key leaves the list once
// used up or past its month.
type ExpiringView struct {
	Pools []ExpiringPoolView
	// Summary is the one-line version; empty when nothing expires.
	Summary     string
	SummaryTone Tone
	// Credits is the monthly credits left on the listed keys; Loss is what
	// is lost even if use starts now, empty when all of it can be used.
	Credits string
	Loss    string
	// Alert: some key must start being used within alertRest, or part of
	// its month is lost.
	Alert bool
	Help  string
	// Empty explains an empty list.
	Empty string
}

// ExpiringPoolView is one pool: the daily pace the deadlines need, key
// counts by how soon they expire, then the keys grouped by deadline.
type ExpiringPoolView struct {
	Title    string
	Note     string
	Pace     string
	Recent   string
	PaceTone Tone
	PaceHelp string
	Buckets  []ExpiringBucketView
	Groups   []ExpiringGroupView
}

type ExpiringBucketView struct {
	Label string
	// Value is the monthly credits left in the bucket, or "—" when nothing
	// expires in it; Note counts the keys and sums what is lost.
	Value string
	Note  string
	Tone  Tone
}

type ExpiringGroupView struct {
	// Countdown is "in 4 h 19 min" within a day, else the date.
	Countdown string
	At        string
	Note      string
	Tone      Tone
	Keys      []ExpiringChipView
}

// ExpiringChipView is one key in a group, most left first.
type ExpiringChipView struct {
	Index  int
	Key    string
	Active bool
	Bar    Bar
	// Percent is what is left of the monthly quota.
	Percent string
	// Credits is "≈ 38.5M"; Tokens spells it out per model.
	Credits string
	Tokens  string
	// Note is what is lost, or that the key must start soon; empty when the
	// month can be used up.
	Note     string
	NoteTone Tone
	Help     string
}

// expiringTone: red within a day, orange within three days.
func expiringTone(deadline, now time.Time) Tone {
	switch left := deadline.Sub(now); {
	case left < 24*time.Hour:
		return ToneDanger
	case left < 72*time.Hour:
		return ToneWarning
	}
	return ToneSecondary
}

// expiringItem is a key's month: o in percent of the key's own month, the
// methods in percent of a Pro key's (Scale).
type expiringItem struct {
	key   keys.KeyState
	o     MonthlyOutlook
	scale float64
}

func (it expiringItem) left() float64   { return it.o.Left * it.scale }
func (it expiringItem) usable() float64 { return it.o.Usable * it.scale }

// loss is what is lost even if use starts now.
func (it expiringItem) loss() float64 { return (it.o.Left - it.o.Usable) * it.scale }

// restLoss is what resting the key for alertRest would lose.
func (it expiringItem) restLoss() float64 { return (it.o.Left - it.o.UsableAfterRest) * it.scale }

// atRisk: nothing is lost yet, but resting the key for alertRest would lose
// some.
func (it expiringItem) atRisk() bool {
	return it.loss() < minLossPercent && it.restLoss() >= minLossPercent
}

// BuildExpiring lists the keys whose month ends soon. spend, when known,
// gives the recent spend per day to compare the pace with.
func BuildExpiring(pool []keys.KeyState, now time.Time, spend *Spend) ExpiringView {
	v := ExpiringView{
		Help:  l10n.L("What each key's monthly quota still holds before it is lost. Spending a key lowers it; a key leaves the list once it is used up or its month is over.\nThe weekly quota caps what a key can spend each week, so a key with much left has to start early. A key that loses part of its month unless it is used within 2 days is marked.\nOnly the Standard pool is counted."),
		Empty: l10n.L("No monthly quota is lost in the next 7 days."),
	}
	var items []expiringItem
	alerts := 0
	for _, key := range pool {
		if key.Status != "ok" {
			continue
		}
		o, ok := MonthlyOutlookOf(PoolOf(key, Standard), now, alertRest)
		if !ok || o.Left <= minExpiringPercent {
			continue
		}
		it := expiringItem{key, o, Scale(key)}
		if it.atRisk() {
			alerts++
		}
		if o.End.Sub(now) <= expiringHorizon || it.atRisk() || it.loss() >= minLossPercent {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return v
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].o.End.Before(items[j].o.End) })
	first := items[0].o.End
	p := ExpiringPoolView{
		Title:    Standard.Title(),
		Note:     KeyCount(len(items)),
		PaceTone: ToneSecondary,
		PaceHelp: l10n.L("At least a day: add up what the listed keys can still use before they are lost, deadline by deadline from the earliest, divide by the days until each deadline, and take the largest.\nLast 7 days: the monthly quota all keys spent each day, from the Key Pool's quota log.\nCredits are converted from monthly quota percentages and are estimates."),
		Buckets:  expiringBuckets(items, now),
		Groups:   expiringGroups(items, now),
	}
	need := requiredPace(items, now)
	p.Pace = l10n.L("Use at least %s a day to lose none", monthCredits(need))
	if spend != nil && spend.HasRecent {
		p.Recent = l10n.L("Last 7 days: %s a day on average", monthCredits(spend.Recent))
		if need > spend.Recent {
			p.PaceTone = ToneDanger
		}
	}
	v.Pools = []ExpiringPoolView{p}
	left, loss := 0.0, 0.0
	for _, it := range items {
		left += it.left()
		if it.loss() >= minLossPercent {
			loss += it.loss()
		}
	}
	v.Credits = monthCredits(left)
	if loss > 0 {
		v.Loss = l10n.L("%s will be lost", monthCredits(loss))
	}
	v.Summary = l10n.L("%s · first %s", KeyCount(len(items)), whenLost(first, now))
	v.SummaryTone = expiringTone(first, now)
	if alerts > 0 {
		v.Alert = true
		v.Summary = l10n.L("%s must start within 2 days", KeyCount(alerts))
		v.SummaryTone = ToneDanger
	}
	return v
}

// requiredPace is the least spend per day that uses every listed key's usable
// rest before its month ends: for each deadline, everything due by then over the
// days until then; the largest wins. items are sorted by end.
func requiredPace(items []expiringItem, now time.Time) float64 {
	need, due := 0.0, 0.0
	for _, it := range items {
		due += it.usable()
		days := math.Max(it.o.End.Sub(now).Hours(), 1) / 24
		need = math.Max(need, due/days)
	}
	return need
}

func expiringBuckets(items []expiringItem, now time.Time) []ExpiringBucketView {
	buckets := []struct {
		label string
		until time.Duration
		tone  Tone
	}{
		{l10n.L("Lost within 24 hours"), 24 * time.Hour, ToneDanger},
		{l10n.L("Lost within 3 days"), 72 * time.Hour, ToneWarning},
		{l10n.L("Lost later"), 1 << 62, TonePrimary},
	}
	out := make([]ExpiringBucketView, len(buckets))
	from := time.Duration(-1)
	for i, b := range buckets {
		n, sum, loss := 0, 0.0, 0.0
		var first time.Time
		for _, it := range items {
			if left := it.o.End.Sub(now); left > from && left <= b.until {
				if n == 0 {
					first = it.o.End
				}
				n++
				sum += it.left()
				if it.loss() >= minLossPercent {
					loss += it.loss()
				}
			}
		}
		out[i] = ExpiringBucketView{Label: b.label, Value: "—", Note: l10n.L("None"), Tone: ToneTertiary}
		if n > 0 {
			note := KeyCount(n)
			if loss > 0 {
				note += " · " + l10n.L("%s will be lost", monthCredits(loss))
			}
			out[i] = ExpiringBucketView{Label: b.label, Value: monthCredits(sum), Note: note + " · " + l10n.L("first %s", whenLost(first, now)), Tone: b.tone}
		}
		from = b.until
	}
	return out
}

// whenLost is when a month is lost: "lost in 3 h 5 min" within a day, else
// "lost 10/24 11:56".
func whenLost(t, now time.Time) string {
	if t.Sub(now) < 24*time.Hour {
		return l10n.L("lost in %s", FormatCountdown(t, now))
	}
	return l10n.L("lost %s", FormatShortDate(t))
}

// sameGroup: within the next day deadlines group by a short span, later by
// local date.
func sameGroup(first, t, now time.Time) bool {
	if first.Sub(now) < 24*time.Hour {
		return t.Sub(first) < expiringGroupSpan
	}
	return t.Local().Format("2006-01-02") == first.Local().Format("2006-01-02")
}

func expiringGroups(items []expiringItem, now time.Time) []ExpiringGroupView {
	var groups []ExpiringGroupView
	for start := 0; start < len(items); {
		first := items[start].o.End
		end := start
		for end < len(items) && sameGroup(first, items[end].o.End, now) {
			end++
		}
		members := append([]expiringItem(nil), items[start:end]...)
		sort.SliceStable(members, func(i, j int) bool { return members[i].left() > members[j].left() })
		left := first.Sub(now)
		sum := 0.0
		for _, it := range members {
			sum += it.left()
		}
		g := ExpiringGroupView{
			Countdown: first.Local().Format("1/2"),
			At:        l10n.L("lost in %d d %d h", int(left.Hours())/24, int(left.Hours())%24),
			Note:      KeyCount(len(members)) + " · " + monthCredits(sum),
			Tone:      expiringTone(first, now),
		}
		if left < 24*time.Hour {
			g.Countdown = l10n.L("in %s", FormatCountdown(first, now))
			g.At = l10n.L("lost at %s", first.Local().Format("15:04"))
		}
		for _, it := range members {
			g.Keys = append(g.Keys, expiringChip(it))
		}
		groups = append(groups, g)
		start = end
	}
	return groups
}

func expiringChip(it expiringItem) ExpiringChipView {
	o := it.o
	credits, tokens := standardCredits(Standard, Monthly, it.left())
	chip := ExpiringChipView{
		Index:   it.key.Index,
		Key:     it.key.Key,
		Active:  it.key.Active,
		Bar:     Bar{Value: o.Left, Tone: ToneSecondary},
		Percent: FormatPercent(o.Left),
		Credits: credits,
		Tokens:  tokens,
	}
	help := []string{
		it.key.Key,
		l10n.L("%s of the monthly quota left (about %s credits), lost %s", FormatPercent(o.Left), monthCredits(it.left()), o.End.Local().Format("1/2 15:04")),
		l10n.L("The weekly and 5-hour quotas let at most %s through before then", monthCredits(it.usable())),
	}
	switch {
	case it.loss() >= minLossPercent:
		chip.Note, chip.NoteTone = l10n.L("%s will be lost", monthCredits(it.loss())), ToneDanger
		chip.Bar.Tone = ToneDanger
	case it.atRisk():
		chip.Note, chip.NoteTone = l10n.L("Start within 2 days"), ToneWarning
		chip.Bar.Tone = ToneWarning
		help = append(help, l10n.L("Unless used within 2 days, %s will be lost", monthCredits(it.restLoss())))
	}
	chip.Help = strings.Join(help, "\n")
	return chip
}
