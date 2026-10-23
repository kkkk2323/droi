package llmusage

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

// Tone is a semantic colour the Usage page maps to its palette.
type Tone uint8

const (
	ToneSecondary Tone = iota
	TonePrimary
	ToneTertiary
	ToneAccent
	ToneGreen
	ToneWarning
	ToneDanger
	ToneTrack
)

// RemainingTone is a bar's fill: danger under DangerRemaining, warning
// under WarningRemaining, otherwise the accent.
func RemainingTone(remaining float64) Tone {
	if remaining < DangerRemaining {
		return ToneDanger
	}
	if remaining < WarningRemaining {
		return ToneWarning
	}
	return ToneAccent
}

// RemainingTextTone is the text next to a bar: only problems get colour.
func RemainingTextTone(remaining float64) Tone {
	if remaining < WarningRemaining {
		return RemainingTone(remaining)
	}
	return ToneSecondary
}

// Bar is a capacity bar filled to Value (0…100).
type Bar struct {
	Value float64
	Tone  Tone
}

func remainingBar(v float64) Bar { return Bar{Value: v, Tone: RemainingTone(v)} }

type Badge struct {
	Label string
	Tone  Tone
}

func stateBadge(s KeyDisplay) Badge {
	tone := ToneDanger
	switch s.Kind {
	case KeyOK:
		tone = ToneGreen
	case KeyNoData:
		tone = ToneSecondary
	case KeyCooldown:
		tone = ToneWarning
	}
	return Badge{Label: s.Label(), Tone: tone}
}

// MetricView is one figure of a strip: Value, a smaller Sub after it
// ("1.2%"), its Title, then a Note.
type MetricView struct {
	Title, Value, Sub, Note string
	Tone                    Tone
}

// Cell is a right-aligned numeric table cell; Text "—" means no value.
type Cell struct {
	Text string
	Tone Tone
	Help string
}

func numeric(text string, ok bool, help string, tone Tone) Cell {
	if !ok {
		return Cell{Text: "—", Tone: ToneTertiary, Help: help}
	}
	return Cell{Text: text, Tone: tone, Help: help}
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// KeyCount is "1 key", "3 keys".
func KeyCount(n int) string {
	if n == 1 {
		return l10n.L("%d key", n)
	}
	return l10n.L("%d keys", n)
}

func clamp(v float64) float64 { return math.Min(math.Max(v, 0), 100) }

// PoolSummaryView is one pool across the routable keys: the average left of
// every window.
type PoolSummaryView struct {
	Title string
	// Note is "2/3 keys usable".
	Note    string
	Windows []PoolSummaryWindow
}

type PoolSummaryWindow struct {
	Label       string
	Bar         Bar
	Percent     string
	PercentTone Tone
	// Name is the window's quota in a sentence ("Weekly quota").
	Name string
	// Reset is the soonest reset among the keys.
	Reset string
	Help  string
}

func BuildPoolSummary(p Pool, pool []keys.KeyState, now time.Time) PoolSummaryView {
	v := PoolSummaryView{Title: p.Title()}
	for _, w := range Windows {
		s := SummarizeWindow(pool, p, w, now)
		v.Note = l10n.L("%d/%d keys usable", s.KeyCount(), len(pool))
		row := PoolSummaryWindow{Label: w.Label(), Name: w.Name(), Help: poolHelp(w, s), Reset: l10n.L("No key has started this window")}
		if s.KeyCount() == 0 {
			row.Bar, row.Percent, row.PercentTone = Bar{Tone: ToneTrack}, "—", ToneTertiary
		} else {
			avg := s.AverageRemaining()
			row.Bar, row.Percent, row.PercentTone = remainingBar(avg), FormatPercent(avg), RemainingTextTone(avg)
		}
		if s.SoonestReset != nil {
			row.Reset = l10n.L("first %s", FormatReset(*s.SoonestReset, now))
		}
		v.Windows = append(v.Windows, row)
	}
	return v
}

func poolHelp(w Window, s WindowSummary) string {
	parts := []string{l10n.L("%s averages %s left", w.Name(), FormatPercent(s.AverageRemaining()))}
	if s.Unmeasured > 0 {
		parts = append(parts, l10n.L("%d keys have data, %d have not started it: counted as full", s.Measured, s.Unmeasured))
	}
	if s.SoonestReset != nil {
		parts = append(parts, l10n.L("The first resets at %s", FormatShortDate(*s.SoonestReset)))
	}
	return strings.Join(parts, "\n")
}

// PoolSummaries are both pools.
func PoolSummaries(pool []keys.KeyState, now time.Time) []PoolSummaryView {
	out := make([]PoolSummaryView, 0, len(Pools))
	for _, p := range Pools {
		out = append(out, BuildPoolSummary(p, pool, now))
	}
	return out
}

// KeyCardView is one key of the list: identity and state on its line, one
// column per pool below it with every window spelled out.
type KeyCardView struct {
	Index    int
	Key      string
	Active   bool
	Badge    Badge
	Cooldown string
	Checked  string
	Pools    []PoolWindowsView
}

type PoolWindowsView struct {
	Title         string
	Available     string
	AvailableTone Tone
	HasLimits     bool
	Rows          []WindowRowView
}

type WindowRowView struct {
	Label       string
	Live        bool
	Bar         Bar
	Percent     string
	PercentTone Tone
	// Credits ("≈ 38.5M") and Tokens, which spells them out per model.
	Credits string
	Tokens  string
	Note    string
	// Even is where Bar would be at an even pace through the window (what
	// would be left), and HasPace whether it is known; Pace says how far
	// off it is.
	HasPace  bool
	Even     float64
	Pace     string
	PaceTone Tone
}

// standardCredits renders percent of a Standard weekly or monthly window as
// credits and per-model tokens; empty for windows of unknown size.
func standardCredits(p Pool, w Window, percent float64) (credits, tokens string) {
	if p != Standard {
		return "", ""
	}
	c, ok := WindowCredits(w, percent)
	if !ok {
		return "", ""
	}
	return "≈ " + FormatCredits(c), TokenEquivalents(c)
}

// monthCredits renders percent of one Standard monthly quota, or a sum of
// such percents over keys, as credits.
func monthCredits(percent float64) string {
	c, _ := WindowCredits(Monthly, percent)
	return FormatCredits(c)
}

func BuildKeyCard(key keys.KeyState, now time.Time) KeyCardView {
	card := KeyCardView{
		Index:  key.Index,
		Key:    key.Key,
		Active: key.Active,
		Badge:  stateBadge(KeyDisplayOf(key, now)),
	}
	var cooldowns []string
	for _, p := range Pools {
		if until := CooldownUntil(key, p); until != nil && until.After(now) {
			cooldowns = append(cooldowns, l10n.L("%s is cooling down, back in %s", p.Title(), FormatCountdown(*until, now)))
		}
	}
	card.Cooldown = strings.Join(cooldowns, " · ")
	if key.UsageCheckedAt != nil {
		card.Checked = l10n.L("Quota checked %s", FormatRelative(*key.UsageCheckedAt, now))
	}
	for _, p := range Pools {
		card.Pools = append(card.Pools, buildPoolWindows(key, p, now))
	}
	return card
}

func buildPoolWindows(key keys.KeyState, p Pool, now time.Time) PoolWindowsView {
	v := PoolWindowsView{Title: p.Title()}
	if used := UsedPercent(key, p); used != nil {
		v.Available = l10n.L("%s left", FormatPercent(clamp(100-*used)))
		v.AvailableTone = RemainingTextTone(100 - *used)
	}
	limits := PoolOf(key, p)
	if limits == nil {
		return v
	}
	v.HasLimits = true
	for _, w := range Windows {
		limit := WindowOf(limits, w)
		row := WindowRowView{Label: w.Label()}
		if IsLive(limit, now) {
			remaining := Remaining(limit)
			row.Live = true
			row.Bar = remainingBar(remaining)
			row.Percent = FormatPercent(remaining)
			row.PercentTone = RemainingTextTone(remaining)
			row.Credits, row.Tokens = standardCredits(p, w, remaining)
			if reset, ok := ResetAt(limit); ok {
				row.Note = FormatReset(reset, now)
			}
			if pace, ok := Pace(limit, w, now); ok {
				row.HasPace, row.Even = true, clamp(remaining+pace)
				row.Pace, row.PaceTone = paceText(pace)
			}
		} else {
			row.Bar = Bar{Value: 100, Tone: ToneTrack}
			row.Percent = "—"
			row.PercentTone = ToneTertiary
			switch {
			case limit == nil:
				row.Note = l10n.L("No data yet")
			case IsStaleExhausted(limit, now):
				row.Note = l10n.L("Starts on the next request")
			default:
				row.Note = l10n.L("Not started yet")
			}
		}
		v.Rows = append(v.Rows, row)
	}
	return v
}

// paceText says how far a window is from an even pace, in points: ahead is
// a warning, since at this rate it runs out before it resets.
func paceText(pace float64) (string, Tone) {
	n := int(math.Round(math.Abs(pace)))
	switch {
	case n == 0:
		return l10n.L("Even with an even pace"), ToneSecondary
	case pace > 0:
		return l10n.L("%d points ahead of an even pace", n), ToneWarning
	}
	return l10n.L("%d points behind an even pace", n), ToneSecondary
}

type EventRowView struct {
	ID         string
	Time       string
	Kind       string
	Key        string
	Pool       string
	Model      string
	Status     string
	StatusTone Tone
	Detail     string
}

func BuildEventRow(e diag.Event) EventRowView {
	row := EventRowView{
		ID:     fmt.Sprintf("%d@%d", e.Seq, e.Time.UnixNano()),
		Time:   FormatClock(e.Time),
		Kind:   e.Kind,
		Key:    orDash(e.Key),
		Pool:   orDash(e.Pool),
		Model:  orDash(e.Model),
		Status: "—",
		Detail: orDash(e.Detail),
	}
	if e.Status != 0 {
		row.Status = fmt.Sprintf("%d", e.Status)
		if e.Status >= 400 {
			row.StatusTone = ToneDanger
		}
	}
	return row
}

// EventRows are the events newest first.
func EventRows(events []diag.Event) []EventRowView {
	out := make([]EventRowView, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		out = append(out, BuildEventRow(events[i]))
	}
	return out
}
