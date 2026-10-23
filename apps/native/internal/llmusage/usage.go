package llmusage

import (
	"math"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

// A key's quota is counted in two pools, each in three windows.
type (
	Pool   string
	Window string
)

const (
	Standard Pool = "standard"
	Core     Pool = "core"
)

var Pools = []Pool{Standard, Core}

func (p Pool) Title() string {
	if p == Core {
		return "Core"
	}
	return "Standard"
}

const (
	FiveHour Window = "fiveHour"
	Weekly   Window = "weekly"
	Monthly  Window = "monthly"
)

var Windows = []Window{FiveHour, Weekly, Monthly}

// Label is the window's short name, for a column.
func (w Window) Label() string {
	switch w {
	case FiveHour:
		return l10n.L("5 h")
	case Weekly:
		return l10n.L("7 d")
	}
	return l10n.L("30 d")
}

// Name is the window's quota in a sentence.
func (w Window) Name() string {
	switch w {
	case FiveHour:
		return l10n.L("5-hour quota")
	case Weekly:
		return l10n.L("Weekly quota")
	}
	return l10n.L("Monthly quota")
}

// Length is how long a window of this kind runs; a month is counted from
// its end, so Start is exact for it.
func (w Window) Length() time.Duration {
	switch w {
	case FiveHour:
		return 5 * time.Hour
	case Weekly:
		return 7 * 24 * time.Hour
	}
	return 30 * 24 * time.Hour
}

// Start is when a window of this kind that ends at end began.
func (w Window) Start(end time.Time) time.Time {
	if w == Monthly {
		return end.AddDate(0, -1, 0)
	}
	return end.Add(-w.Length())
}

// WarningRemaining and DangerRemaining are where remaining headroom turns
// yellow and red.
const (
	DangerRemaining  = 10.0
	WarningRemaining = 30.0
)

func clampPercent(v float64) float64 { return math.Min(math.Max(v, 0), 100) }

// PoolOf is the per-window breakdown of one pool of a key, or nil.
func PoolOf(k keys.KeyState, pool Pool) *keys.PoolLimits {
	if k.Limits == nil {
		return nil
	}
	if pool == Core {
		return k.Limits.Core
	}
	return k.Limits.Standard
}

// WindowOf is one window of a pool, or nil.
func WindowOf(p *keys.PoolLimits, w Window) *keys.BucketLimit {
	if p == nil {
		return nil
	}
	switch w {
	case FiveHour:
		return p.FiveHour
	case Weekly:
		return p.Weekly
	}
	return p.Monthly
}

// UsedPercent is the headline used percent of a pool (the highest of its
// windows), as the Key Pool routes on.
func UsedPercent(k keys.KeyState, pool Pool) *float64 {
	if pool == Core {
		return k.CorePercent
	}
	return k.StandardPercent
}

func CooldownUntil(k keys.KeyState, pool Pool) *time.Time {
	if pool == Core {
		return k.CoreCooldownUntil
	}
	return k.StandardCooldownUntil
}

// Remaining is 100 - used, clamped to 0…100.
func Remaining(b *keys.BucketLimit) float64 {
	if b == nil {
		return 100
	}
	return clampPercent(100 - b.UsedPercent)
}

// ResetAt is when the window ends; ok is false for a window Factory has not
// started yet.
func ResetAt(b *keys.BucketLimit) (time.Time, bool) {
	if b == nil || b.WindowEnd == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, b.WindowEnd)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// IsStaleExhausted reports an exhausted window whose end has passed: the
// quota is back, but Factory keeps reporting 100% until the next request.
func IsStaleExhausted(b *keys.BucketLimit, now time.Time) bool {
	if b == nil || b.UsedPercent < 100 {
		return false
	}
	reset, ok := ResetAt(b)
	return ok && !reset.After(now)
}

// IsLive is a window that has started and is not stale-exhausted. Factory
// opens the next window lazily on the first request, so a window without an
// end has not started and carries no usage.
func IsLive(b *keys.BucketLimit, now time.Time) bool {
	if b == nil {
		return false
	}
	_, started := ResetAt(b)
	return started && !IsStaleExhausted(b, now)
}

// Pace is how far a live window is from an even pace, in points of the
// window: +12 is twelve points ahead, so at this rate it runs out before it
// resets; -8 is eight points to spare.
func Pace(b *keys.BucketLimit, w Window, now time.Time) (float64, bool) {
	if !IsLive(b, now) {
		return 0, false
	}
	end, _ := ResetAt(b)
	start := w.Start(end)
	if !end.After(now) || now.Before(start) {
		return 0, false
	}
	even := 100 * float64(now.Sub(start)) / float64(end.Sub(start))
	return clampPercent(b.UsedPercent) - even, true
}

// WindowSummary aggregates one window of one pool across the keys the pool
// can route to.
type WindowSummary struct {
	// Measured counts keys whose window was started and current;
	// Unmeasured those whose window is missing, not started or
	// stale-exhausted, counted at a full 100.
	Measured, Unmeasured int
	TotalRemaining       float64
	SoonestReset         *time.Time
}

func (s WindowSummary) KeyCount() int { return s.Measured + s.Unmeasured }

func (s WindowSummary) AverageRemaining() float64 {
	if s.KeyCount() == 0 {
		return 0
	}
	return s.TotalRemaining / float64(s.KeyCount())
}

// KeyEquivalent is the remaining capacity as whole-key equivalents.
func (s WindowSummary) KeyEquivalent() float64 { return s.TotalRemaining / 100 }

// SummarizeWindow aggregates one window of one pool. Keys the pool cannot
// route to (probe errors, no usage data) are left out.
func SummarizeWindow(pool []keys.KeyState, p Pool, w Window, now time.Time) WindowSummary {
	var s WindowSummary
	for _, key := range pool {
		if key.Status != "ok" {
			continue
		}
		limit := WindowOf(PoolOf(key, p), w)
		if !IsLive(limit, now) {
			s.Unmeasured++
			s.TotalRemaining += 100
			continue
		}
		s.Measured++
		s.TotalRemaining += Remaining(limit)
		if reset, ok := ResetAt(limit); ok && reset.After(now) {
			if s.SoonestReset == nil || reset.Before(*s.SoonestReset) {
				r := reset
				s.SoonestReset = &r
			}
		}
	}
	return s
}

// Shares of one key's monthly Standard quota that one weekly and one 5-hour
// window hold.
var (
	weeklyShare   = float64(WeeklyStandardCredits) / MonthlyStandardCredits * 100
	fiveHourShare = float64(FiveHourStandardCredits) / MonthlyStandardCredits * 100
)

// MonthlyOutlook is a live monthly Standard window: what is left, when it
// ends, and the most of it the weekly and 5-hour caps still let through
// before then if use starts now (Usable) or only after the key rests
// (UsableAfterRest). Percentages are of the monthly quota.
type MonthlyOutlook struct {
	Left            float64
	Usable          float64
	UsableAfterRest float64
	End             time.Time
}

func MonthlyOutlookOf(p *keys.PoolLimits, now time.Time, rest time.Duration) (MonthlyOutlook, bool) {
	if p == nil || !IsLive(p.Monthly, now) {
		return MonthlyOutlook{}, false
	}
	end, _ := ResetAt(p.Monthly)
	if !end.After(now) {
		return MonthlyOutlook{}, false
	}
	left := Remaining(p.Monthly)
	usable := func(from time.Time) float64 {
		return math.Min(left, math.Min(
			capacityUntil(p.Weekly, weeklyShare, Weekly.Length(), from, end),
			capacityUntil(p.FiveHour, fiveHourShare, FiveHour.Length(), from, end),
		))
	}
	return MonthlyOutlook{Left: left, Usable: usable(now), UsableAfterRest: usable(now.Add(rest)), End: end}, true
}

// capacityUntil is the most a window of share and length lets through from
// from until end when the key is not used before from: what the current
// window has left, then one full window after another. Factory opens the
// next window on the first request, so it can follow the last one at once.
func capacityUntil(w *keys.BucketLimit, share float64, length time.Duration, from, end time.Time) float64 {
	if !end.After(from) {
		return 0
	}
	total := 0.0
	if IsLive(w, from) {
		if reset, _ := ResetAt(w); reset.After(from) {
			total = Remaining(w) / 100 * share
			from = reset
		}
	}
	if end.After(from) {
		total += math.Ceil(float64(end.Sub(from))/float64(length)) * share
	}
	return total
}

// UsageCheckedAt is when the quota of any key was last read, or nil.
func UsageCheckedAt(pool []keys.KeyState) *time.Time {
	var latest *time.Time
	for _, k := range pool {
		if k.UsageCheckedAt != nil && (latest == nil || k.UsageCheckedAt.After(*latest)) {
			latest = k.UsageCheckedAt
		}
	}
	return latest
}

// KeyKind is the display state of a key.
type KeyKind string

const (
	KeyOK        KeyKind = "ok"
	KeyNoData    KeyKind = "noData"
	KeyError     KeyKind = "error"
	KeyCooldown  KeyKind = "cooldown"
	KeyExhausted KeyKind = "exhausted"
)

// KeyDisplay is how a key reads in the list.
type KeyDisplay struct {
	Kind KeyKind
	// Code is the probe status for KeyError.
	Code string
	// Pools are the cooling pools for KeyCooldown.
	Pools []Pool
}

func (s KeyDisplay) Label() string {
	switch s.Kind {
	case KeyOK:
		return l10n.L("OK")
	case KeyNoData:
		return l10n.L("No quota yet")
	case KeyError:
		switch s.Code {
		case "http_401", "http_403":
			return l10n.L("Key rejected")
		case "fetch_error", "read_error":
			return l10n.L("Lookup failed")
		case "parse_error":
			return l10n.L("Unexpected answer")
		}
		return s.Code
	case KeyCooldown:
		titles := make([]string, len(s.Pools))
		for i, p := range s.Pools {
			titles[i] = p.Title()
		}
		return l10n.L("%s cooling down", strings.Join(titles, "/"))
	case KeyExhausted:
		return l10n.L("Quota used up")
	}
	return ""
}

func (s KeyDisplay) IsProblem() bool { return s.Kind != KeyOK }

func KeyDisplayOf(key keys.KeyState, now time.Time) KeyDisplay {
	switch key.Status {
	case "ok":
	case "no_usage_data", "":
		return KeyDisplay{Kind: KeyNoData}
	default:
		return KeyDisplay{Kind: KeyError, Code: key.Status}
	}
	var cooling []Pool
	for _, pool := range Pools {
		if until := CooldownUntil(key, pool); until != nil && until.After(now) {
			cooling = append(cooling, pool)
		}
	}
	if len(cooling) > 0 {
		return KeyDisplay{Kind: KeyCooldown, Pools: cooling}
	}
	if !key.StandardAvailable && !key.CoreAvailable {
		return KeyDisplay{Kind: KeyExhausted}
	}
	return KeyDisplay{Kind: KeyOK}
}

// IsSuccess is a request the upstream answered.
func IsSuccess(r diag.RequestRecord) bool { return r.Status < 400 && r.Error == "" }

// TokensPerSecond is output tokens per second over the generation phase,
// after the first byte.
func TokensPerSecond(r diag.RequestRecord) (float64, bool) {
	if r.Usage == nil || r.Usage.Output <= 0 {
		return 0, false
	}
	ms := r.DurationMs - r.TTFBMs
	if ms <= 0 {
		return 0, false
	}
	return float64(r.Usage.Output) / (float64(ms) / 1000), true
}
