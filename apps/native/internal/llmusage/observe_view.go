package llmusage

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
)

// ObserveMetric is what the trend and the heat map count.
type ObserveMetric int

const (
	MetricTokens ObserveMetric = iota
	MetricCredits
	MetricCalls
	MetricSpeed
)

var ObserveMetrics = []ObserveMetric{MetricTokens, MetricCredits, MetricCalls, MetricSpeed}

func (m ObserveMetric) Label() string {
	switch m {
	case MetricCredits:
		return l10n.L("Credits")
	case MetricCalls:
		return l10n.L("Request count")
	case MetricSpeed:
		return l10n.L("Speed")
	}
	return l10n.L("Tokens")
}

// Value is the metric of t; ok is false when t has none (a speed without a
// timed call).
func (m ObserveMetric) Value(t Totals) (float64, bool) {
	switch m {
	case MetricCredits:
		return t.Credits, t.Credits > 0
	case MetricCalls:
		return float64(t.Calls), t.Calls > 0
	case MetricSpeed:
		return t.Speed()
	}
	return float64(t.Tokens()), t.Tokens() > 0
}

func (m ObserveMetric) Format(v float64) string {
	switch m {
	case MetricCredits:
		return l10n.L("≈ %s credits", FormatCredits(v))
	case MetricCalls:
		return countL(int(v), l10n.N("%d request"), l10n.N("%d requests"))
	case MetricSpeed:
		return l10n.L("%.0f tok/s", v)
	}
	return l10n.L("%s tokens", compactTokens(int64(v)))
}

// compactTokens is a total in three figures at most, "36M", "2.32B", so
// the page's narrow cells hold it.
func compactTokens(n int64) string { return FormatCredits(float64(n)) }

// keyMonths is credits as a share of one key's monthly quota: "91% of a
// key's month", "25.3 key-months".
func keyMonths(credits float64) string {
	share := credits / MonthlyStandardCredits
	if share < 1 {
		return l10n.L("%s of a key's month", shareText(share))
	}
	return l10n.L("%.1f key-months", share)
}

// stacked: a speed is no sum, so its bars are not split by model.
func (m ObserveMetric) stacked() bool { return m != MetricSpeed }

// countL is a count with its noun; one and many are l10n.N keys holding a
// %d.
func countL(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf(l10n.T(one), n)
	}
	return fmt.Sprintf(l10n.T(many), n)
}

func PeriodLabel(p Period) string {
	switch p {
	case PeriodWeek:
		return l10n.L("7 days")
	case PeriodMonth:
		return l10n.L("30 days")
	case PeriodAll:
		return l10n.L("All")
	}
	return l10n.L("Today")
}

func GroupingLabel(g Grouping) string {
	switch g {
	case ByKey:
		return l10n.L("Key")
	case ByKind:
		return l10n.L("Type")
	case BySession:
		return l10n.L("Session")
	}
	return l10n.L("Model")
}

// KindLabel names what a call was for, by the list filter's ID.
func KindLabel(id string) string {
	switch id {
	case "cache_warm":
		return l10n.L("Cache warming")
	case "key_open":
		return l10n.L("Key start")
	case "request", "":
		return l10n.L("Model request")
	}
	return id
}

// Option is one choice of a filter.
type Option struct{ Value, Label string }

// ObserveView is the Observe page.
type ObserveView struct {
	Range     string
	Source    string
	Empty     bool
	EmptyText string
	Summary   []MetricView
	Trend     TrendView
	Heat      HeatView
	Groups    []GroupRowView
	// GroupsNote says what the breakdown counts, and why sessions may be
	// missing.
	GroupsNote string
	Rows       []RequestRowView
	ListNote   string
	Models     []Option
	Kinds      []Option
}

// TrendView is a bar a bucket, each split among the top models (Legend,
// "Other" last) unless the metric is a speed.
type TrendView struct {
	Bars    []TrendBar
	Legend  []string
	Stacked bool
	Top     float64
	Peak    string
	Axis    []string
}

type TrendBar struct {
	Parts   []float64
	Help    string
	Current bool
}

// HeatView is the calls by weekday and hour, each cell at one of five
// levels: 0 for none, then the quartiles of the cells with any.
type HeatView struct {
	Rows []HeatRow
	Note string
}

type HeatRow struct {
	Label string
	Cells [24]HeatCell
}

type HeatCell struct {
	Level int
	Help  string
}

// GroupRowView is one row of the breakdown.
type GroupRowView struct {
	ID, Name, Help string
	Mono           bool
	Calls          string
	Failed         Cell
	Share          float64
	ShareText      string
	Input, Output  Cell
	Cache          Cell
	Credits        Cell
	TTFT, Speed    Cell
}

// BuildObserve is the page of o, its trend and heat map counting metric,
// its breakdown by grouping. sessionName names a session by its ID, "" when
// unknown.
func BuildObserve(o Observation, metric ObserveMetric, grouping Grouping, sessionName func(string) string) ObserveView {
	v := ObserveView{
		Range:  observeRange(o),
		Source: l10n.L("From this computer's request log · %d calls in all", o.Records),
	}
	if o.Totals.Calls == 0 {
		v.Empty = true
		v.EmptyText = map[Period]string{
			PeriodToday: l10n.L("No model requests today. They show here once a session sends one through the Key Pool."),
			PeriodWeek:  l10n.L("No model requests in the last 7 days."),
			PeriodMonth: l10n.L("No model requests in the last 30 days."),
			PeriodAll:   l10n.L("No model requests yet. They show here once a session sends one through the Key Pool."),
		}[o.Query.Period]
	}
	v.Summary = observeSummary(o.Totals)
	v.Trend = buildTrend(o, metric)
	v.Heat = buildHeat(o, metric)
	v.Groups, v.GroupsNote = buildGroups(o, grouping, sessionName)
	for _, r := range o.Listed {
		v.Rows = append(v.Rows, BuildRequestRow(r))
	}
	v.ListNote = l10n.L("%d match", o.Matching)
	if o.Matching > len(o.Listed) {
		v.ListNote = l10n.L("%d match · the newest %d shown", o.Matching, len(o.Listed))
	}
	v.Models = []Option{{"", l10n.L("All models")}}
	for _, m := range o.Models {
		v.Models = append(v.Models, Option{m, orDash(m)})
	}
	v.Kinds = []Option{{"", l10n.L("All types")}}
	for _, k := range o.Kinds {
		v.Kinds = append(v.Kinds, Option{k, KindLabel(k)})
	}
	return v
}

func observeRange(o Observation) string {
	to := o.To.Local()
	if o.Hourly {
		return l10n.L("Today, %s", to.Format("1/2"))
	}
	return l10n.L("%s – %s", o.From.Local().Format("1/2"), to.Format("1/2"))
}

func observeSummary(t Totals) []MetricView {
	calls := MetricView{Title: l10n.L("Request count"), Value: fmt.Sprintf("%d", t.Calls), Note: l10n.L("All succeeded"), Tone: TonePrimary}
	if t.Failed > 0 || t.Switched > 0 {
		calls.Note = l10n.L("%d failed · %d switched keys", t.Failed, t.Switched)
	}
	if t.Calls > 0 && float64(t.Failed)/float64(t.Calls) > 0.1 {
		calls.Tone = ToneWarning
	}
	tokens := MetricView{Title: l10n.L("Tokens"), Value: compactTokens(t.Tokens()), Tone: TonePrimary,
		Note: l10n.L("%s of them output", compactTokens(t.Output))}
	cache := MetricView{Title: l10n.L("Cache hits"), Value: "—", Tone: TonePrimary, Note: l10n.L("No input yet")}
	if rate, ok := t.CacheRate(); ok {
		cache.Value = FormatPercent(rate)
		cache.Note = l10n.L("%s written to cache", compactTokens(t.CacheWrite))
	}
	credits := MetricView{Title: l10n.L("Credits"), Value: "≈ " + FormatCredits(t.Credits), Tone: TonePrimary, Note: l10n.L("Standard models only")}
	if t.Unpriced > 0 {
		credits.Note = l10n.L("%d calls unpriced", t.Unpriced)
	}
	speed := MetricView{Title: l10n.L("Output speed"), Value: "—", Tone: TonePrimary, Note: l10n.L("No timed reply yet")}
	if s, ok := t.Speed(); ok {
		speed.Value, speed.Sub = fmt.Sprintf("%.0f", s), "token/s"
	}
	if ms, ok := t.MeanTTFT(); ok {
		speed.Note = l10n.L("First token after %s on average", FormatDuration(ms))
	}
	return []MetricView{calls, tokens, cache, credits, speed}
}

func buildTrend(o Observation, metric ObserveMetric) TrendView {
	v := TrendView{Stacked: metric.stacked()}
	if v.Stacked {
		v.Legend = append(append([]string(nil), o.Top...), l10n.L("Other"))
	}
	peak, peakAt := 0.0, -1
	for i, b := range o.Buckets {
		total := b.Total()
		bar := TrendBar{Current: i == len(o.Buckets)-1}
		value, _ := metric.Value(total)
		if v.Stacked {
			for _, p := range b.Parts {
				pv, _ := metric.Value(p)
				bar.Parts = append(bar.Parts, pv)
			}
		} else {
			bar.Parts = []float64{value}
		}
		lines := []string{bucketName(o, b) + " · " + metric.Format(value)}
		if metric != MetricCalls {
			lines[0] += " · " + MetricCalls.Format(float64(total.Calls))
		}
		if v.Stacked && value > 0 {
			for j, p := range b.Parts {
				if pv, ok := metric.Value(p); ok {
					lines = append(lines, l10n.L("%s: %s", v.Legend[j], metric.Format(pv)))
				}
			}
		}
		bar.Help = strings.Join(lines, "\n")
		if value > peak {
			peak, peakAt = value, i
		}
		v.Bars = append(v.Bars, bar)
	}
	v.Top = peak
	if peakAt >= 0 {
		v.Peak = l10n.L("Peak %s: %s", bucketName(o, o.Buckets[peakAt]), metric.Format(peak))
	}
	if n := len(o.Buckets); n > 0 {
		v.Axis = []string{bucketAxis(o, o.Buckets[0]), bucketAxis(o, o.Buckets[n/2]), bucketAxis(o, o.Buckets[n-1])}
	}
	return v
}

func bucketName(o Observation, b Bucket) string {
	t := b.Start.Local()
	switch {
	case o.Hourly:
		return l10n.L("%s–%s", t.Format("15:04"), t.Add(59*60e9).Format("15:04"))
	case o.Weekly:
		return l10n.L("Week of %s", t.Format("1/2"))
	}
	return l10n.L("%s %s", t.Format("1/2"), Weekday(t.Weekday()))
}

func bucketAxis(o Observation, b Bucket) string {
	t := b.Start.Local()
	if o.Hourly {
		return t.Format("15:00")
	}
	return t.Format("1/2")
}

func buildHeat(o Observation, metric ObserveMetric) HeatView {
	v := HeatView{Note: l10n.L("%s by weekday and hour", metric.Label())}
	var values []float64
	for d := range 7 {
		for h := range 24 {
			if x, ok := metric.Value(o.Heat[d][h]); ok && x > 0 {
				values = append(values, x)
			}
		}
	}
	slices.Sort(values)
	quartile := func(q float64) float64 {
		if len(values) == 0 {
			return 0
		}
		return values[int(q*float64(len(values)-1))]
	}
	cuts := []float64{quartile(0.25), quartile(0.5), quartile(0.75)}
	days := []string{l10n.L("Monday"), l10n.L("Tuesday"), l10n.L("Wednesday"), l10n.L("Thursday"), l10n.L("Friday"), l10n.L("Saturday"), l10n.L("Sunday")}
	for d := range 7 {
		row := HeatRow{Label: days[d]}
		for h := range 24 {
			t := o.Heat[d][h]
			x, ok := metric.Value(t)
			cell := HeatCell{Help: l10n.L("%s %02d:00 · nothing", days[d], h)}
			if ok && x > 0 {
				cell.Level = 1
				for _, c := range cuts {
					if x > c {
						cell.Level++
					}
				}
				cell.Help = l10n.L("%s %02d:00 · %s", days[d], h, metric.Format(x))
				if metric != MetricCalls {
					cell.Help += " · " + MetricCalls.Format(float64(t.Calls))
				}
			}
			row.Cells[h] = cell
		}
		v.Rows = append(v.Rows, row)
	}
	return v
}

func buildGroups(o Observation, g Grouping, sessionName func(string) string) ([]GroupRowView, string) {
	total := float64(o.Totals.Tokens())
	note := l10n.L("By tokens, the most first")
	if g == BySession {
		note = l10n.L("Only calls that name their session; older records do not")
	}
	var rows []GroupRowView
	for _, grp := range o.Groups[g] {
		t := grp.Totals
		row := GroupRowView{ID: grp.ID, Name: orDash(grp.ID), Calls: fmt.Sprintf("%d", t.Calls)}
		switch g {
		case ByModel, ByKey:
			row.Mono = true
		case ByKind:
			row.Name = KindLabel(grp.ID)
		case BySession:
			row.Help = grp.ID
			if name := sessionName(grp.ID); name != "" {
				row.Name = name
			} else {
				row.Name, row.Mono = shortID(grp.ID), true
			}
		}
		row.Failed = numeric(fmt.Sprintf("%d", t.Failed), t.Failed > 0, l10n.L("Failed requests"), ToneDanger)
		if total > 0 {
			row.Share = float64(t.Tokens()) / total * 100
		}
		row.ShareText = shareText(row.Share / 100)
		row.Input = numeric(compactTokens(t.Input), t.Input > 0, l10n.L("Input tokens, cache included"), ToneSecondary)
		row.Output = numeric(compactTokens(t.Output), t.Output > 0, l10n.L("Output tokens"), ToneSecondary)
		rate, ok := t.CacheRate()
		row.Cache = numeric(FormatPercent(rate), ok, l10n.L("Share of the input read from cache"), ToneSecondary)
		row.Credits = creditsCell(t)
		ms, ok := t.MeanTTFT()
		row.TTFT = numeric(FormatDuration(ms), ok, l10n.L("Time to the first token, on average"), ToneSecondary)
		speed, ok := t.Speed()
		row.Speed = numeric(fmt.Sprintf("%.0f", speed), ok, l10n.L("Output tokens per second after the first token"), ToneSecondary)
		rows = append(rows, row)
	}
	return rows, note
}

func creditsCell(t Totals) Cell {
	switch {
	case t.Credits > 0:
		help := keyMonths(t.Credits)
		if t.Unpriced > 0 {
			help += "\n" + l10n.L("%d calls unpriced", t.Unpriced)
		}
		return Cell{Text: "≈ " + FormatCredits(t.Credits), Tone: ToneSecondary, Help: help}
	case t.Unpriced > 0:
		return Cell{Text: l10n.L("Unpriced"), Tone: ToneTertiary, Help: l10n.L("No credit weight is known for this model")}
	}
	return numeric("", false, "", ToneSecondary)
}

// DetailLine is one fact of a request.
type DetailLine struct{ Label, Value string }

// RequestDetail is everything the log knows of one request.
func RequestDetail(r diag.RequestRecord, sessionName func(string) string) []DetailLine {
	local := r.Time.Local()
	lines := []DetailLine{
		{l10n.L("Time"), local.Format("2006-01-02 15:04:05.000")},
		{l10n.L("Type"), KindLabel(kindID(r.Kind))},
		{l10n.L("Model"), orDash(r.Model)},
	}
	provider := orDash(r.Provider)
	if r.Upstream != "" {
		provider += " · " + r.Upstream
	}
	lines = append(lines, DetailLine{l10n.L("Provider"), provider}, DetailLine{l10n.L("Key"), orDash(r.Key)})
	pool := orDash(r.Pool)
	if r.RequestedPool != "" && r.RequestedPool != r.Pool {
		pool = l10n.L("Asked for the %s pool, used the %s pool", r.RequestedPool, r.Pool)
	}
	lines = append(lines, DetailLine{l10n.L("Pool"), pool})
	status := fmt.Sprintf("%d", r.Status)
	for _, s := range []string{r.Error, r.ErrorDetail} {
		if s != "" {
			status += " · " + s
		}
	}
	lines = append(lines, DetailLine{l10n.L("Status"), status})
	if r.SessionID != "" {
		session := r.SessionID
		if name := sessionName(r.SessionID); name != "" {
			session = name + " · " + r.SessionID
		}
		lines = append(lines, DetailLine{l10n.L("Session"), session})
	}
	if r.CallType != "" {
		lines = append(lines, DetailLine{l10n.L("Called for"), r.CallType})
	}
	if u := r.Usage; u != nil {
		tokens := l10n.L("Input %s (cache read %s · cache written %s) · output %s", FormatTokens(u.Input), FormatTokens(u.CacheRead), FormatTokens(u.CacheWrite), FormatTokens(u.Output))
		if u.Reasoning > 0 {
			tokens = l10n.L("Input %s (cache read %s · cache written %s) · output %s (reasoning %s)", FormatTokens(u.Input), FormatTokens(u.CacheRead), FormatTokens(u.CacheWrite), FormatTokens(u.Output), FormatTokens(u.Reasoning))
		}
		lines = append(lines, DetailLine{l10n.L("Tokens"), tokens})
		var t Totals
		t.add(r)
		lines = append(lines, DetailLine{l10n.L("Credits"), creditsCell(t).Text})
	} else {
		lines = append(lines, DetailLine{l10n.L("Tokens"), l10n.L("Factory returned no usage")})
	}
	timing := l10n.L("%s in all", FormatDuration(r.DurationMs))
	if r.TTFBMs > 0 {
		timing += " · " + l10n.L("first byte after %s", FormatDuration(r.TTFBMs))
	}
	if r.TTFTMs > 0 {
		timing += " · " + l10n.L("first token after %s", FormatDuration(r.TTFTMs))
	}
	lines = append(lines, DetailLine{l10n.L("Time taken"), timing})
	var t Totals
	t.add(r)
	if s, ok := t.Speed(); ok {
		lines = append(lines, DetailLine{l10n.L("Speed"), l10n.L("%.0f tok/s", s)})
	}
	if r.Rotated || r.Attempts > 1 {
		lines = append(lines, DetailLine{l10n.L("Key switches"), l10n.L("Tried %d keys", r.Attempts)})
	}
	size := l10n.L("Request body %s · response body %s", FormatBytes(r.RequestBytes), FormatBytes(r.Bytes))
	if r.Stream {
		size += " · " + l10n.L("streamed")
	}
	lines = append(lines, DetailLine{l10n.L("Size"), size})
	return lines
}
