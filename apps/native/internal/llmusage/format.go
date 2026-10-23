// Package llmusage is the display side of the Key Pool's numbers: how much
// of a key's quota is left, what the pool spent, how long it lasts, and the
// words and shapes the Usage page shows them in. The Key Pool's own routing
// is the source of truth; nothing here decides anything.
package llmusage

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

func FormatClock(t time.Time) string { return t.Local().Format("15:04:05") }

func FormatShortDate(t time.Time) string { return t.Local().Format("1/2 15:04") }

// FormatDuration is a request's length: "840ms", "1.4s", "2m05s".
func FormatDuration(ms int64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms < 60_000:
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	return fmt.Sprintf("%dm%02ds", ms/60_000, (ms%60_000)/1000)
}

// FormatTokens: 950 → "950", 12_345 → "12.3K", 4_560_000 → "4.56M".
func FormatTokens(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
}

func FormatBytes(bytes int64) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%dB", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1fK", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1fM", float64(bytes)/(1024*1024))
}

func FormatPercent(v float64) string { return fmt.Sprintf("%d%%", int(math.Round(v))) }

// FormatSpan is a length of time in two units at most: "45 sec", "12 min",
// "3 h 5 min", "2 d 4 h".
func FormatSpan(d time.Duration) string {
	seconds := int(math.Round(d.Seconds()))
	switch {
	case seconds < 60:
		return l10n.L("%d sec", max(seconds, 0))
	case seconds < 3600:
		return l10n.L("%d min", seconds/60)
	case seconds < 86400:
		hours, minutes := seconds/3600, seconds%3600/60
		if minutes == 0 {
			return l10n.L("%d h", hours)
		}
		return l10n.L("%d h %d min", hours, minutes)
	}
	days, hours := seconds/86400, seconds%86400/3600
	if hours == 0 {
		return l10n.L("%d d", days)
	}
	return l10n.L("%d d %d h", days, hours)
}

// FormatCountdown is the time until t within a day ("3 h 5 min"), and t's
// date beyond it ("1/2 15:04").
func FormatCountdown(t, now time.Time) string {
	if !t.After(now) {
		return l10n.L("due")
	}
	if t.Sub(now) < 24*time.Hour {
		return FormatSpan(t.Sub(now))
	}
	return FormatShortDate(t)
}

// FormatReset says when a window resets: "resets in 3 h 5 min" within a
// day, "resets 1/2 15:04" beyond it, "reset" once it has passed.
func FormatReset(t, now time.Time) string {
	switch {
	case !t.After(now):
		return l10n.L("reset")
	case t.Sub(now) < 24*time.Hour:
		return l10n.L("resets in %s", FormatSpan(t.Sub(now)))
	}
	return l10n.L("resets %s", FormatShortDate(t))
}

// FormatRelative: "just now", "%d sec ago", "%d min ago", "%d h ago".
func FormatRelative(t, now time.Time) string {
	seconds := int(math.Round(now.Sub(t).Seconds()))
	switch {
	case seconds < 5:
		return l10n.L("just now")
	case seconds < 60:
		return l10n.L("%d sec ago", seconds)
	}
	if minutes := seconds / 60; minutes < 60 {
		return l10n.L("%d min ago", minutes)
	}
	return l10n.L("%d h ago", seconds/3600)
}

// FormatCredits is a compact count in three figures at most: "950",
// "12.3K", "38.5M", "1.56B".
func FormatCredits(v float64) string {
	unit, div := "", 1.0
	switch {
	case math.Abs(v) >= 1e9:
		unit, div = "B", 1e9
	case math.Abs(v) >= 1e6:
		unit, div = "M", 1e6
	case math.Abs(v) >= 1e3:
		unit, div = "K", 1e3
	default:
		return fmt.Sprintf("%d", int(math.Round(v)))
	}
	n := v / div
	digits := 0
	switch {
	case math.Abs(n) < 10:
		digits = 2
	case math.Abs(n) < 100:
		digits = 1
	}
	s := strconv.FormatFloat(n, 'f', digits, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s + unit
}
