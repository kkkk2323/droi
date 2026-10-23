package llmusage

import (
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

// KeyRowView is one key of the menu bar panel: its number, the masked key
// and whether droid sends with it, then a small bar per pool or, for a key
// that cannot serve, a badge.
type KeyRowView struct {
	Index   int
	Key     string
	Active  bool
	Problem bool
	Badge   Badge
	Pools   []MiniBarView
	Help    string
}

// MiniBarView is what one pool of a key has left.
type MiniBarView struct {
	HasValue    bool
	Bar         Bar
	Percent     string
	PercentTone Tone
}

func BuildKeyRow(key keys.KeyState, now time.Time) KeyRowView {
	state := KeyDisplayOf(key, now)
	row := KeyRowView{
		Index:   key.Index,
		Key:     key.Key,
		Active:  key.Active,
		Problem: state.IsProblem(),
		Badge:   stateBadge(state),
		Help:    keyRowHelp(key, now),
	}
	for _, p := range Pools {
		bar := MiniBarView{Percent: "—", PercentTone: ToneTertiary}
		if used := UsedPercent(key, p); used != nil {
			remaining := clampPercent(100 - *used)
			bar = MiniBarView{HasValue: true, Bar: remainingBar(remaining), Percent: FormatPercent(remaining), PercentTone: RemainingTextTone(remaining)}
		}
		row.Pools = append(row.Pools, bar)
	}
	return row
}

// keyRowHelp spells out every window of the key, for the row's tooltip.
func keyRowHelp(key keys.KeyState, now time.Time) string {
	var lines []string
	for _, p := range Pools {
		limits := PoolOf(key, p)
		if limits == nil {
			continue
		}
		var windows []string
		for _, w := range Windows {
			limit := WindowOf(limits, w)
			if limit == nil {
				continue
			}
			reset, started := ResetAt(limit)
			switch {
			case !started:
				windows = append(windows, l10n.L("%s not started", w.Name()))
			case IsStaleExhausted(limit, now):
				windows = append(windows, l10n.L("%s reset, starts with the next request", w.Name()))
			default:
				windows = append(windows, l10n.L("%s: %s left, %s", w.Name(), FormatPercent(Remaining(limit)), FormatReset(reset, now)))
			}
		}
		if len(windows) > 0 {
			lines = append(lines, l10n.L("%s: %s", p.Title(), strings.Join(windows, l10n.L("; "))))
		}
	}
	if key.UsageCheckedAt != nil {
		lines = append(lines, l10n.L("Quota checked %s", FormatRelative(*key.UsageCheckedAt, now)))
	}
	if len(lines) == 0 {
		return l10n.L("No data yet")
	}
	return strings.Join(lines, "\n")
}

// TrayIcon is the menu bar icon's state.
type TrayIcon string

const (
	TrayUnknown TrayIcon = "unknown"
	TrayOK      TrayIcon = "ok"
	TrayWarning TrayIcon = "warning"
	TrayDanger  TrayIcon = "danger"
	// TrayExpiring: a key must start being used soon, or part of its month
	// is lost.
	TrayExpiring TrayIcon = "expiring"
)

// MenuBarView is the menu bar item: the Standard pool's monthly quota left
// on average as its title, an icon for how much that is, and a tooltip.
type MenuBarView struct {
	Title   string
	Icon    TrayIcon
	ToolTip string
}

// MonthlyRemaining is what the Standard pool has left of its month on
// average across the keys it can route to.
func MonthlyRemaining(pool []keys.KeyState, now time.Time) (float64, bool) {
	s := SummarizeWindow(pool, Standard, Monthly, now)
	if s.KeyCount() == 0 {
		return 0, false
	}
	return s.AverageRemaining(), true
}

func BuildMenuBar(pool []keys.KeyState, expiring ExpiringView, now time.Time) MenuBarView {
	v := MenuBarView{Title: "--", Icon: TrayUnknown, ToolTip: l10n.L("Droi · Key Pool")}
	if remaining, ok := MonthlyRemaining(pool, now); ok {
		v.Title = FormatPercent(remaining)
		v.ToolTip = l10n.L("Droi · Key Pool: %s of the monthly quota left", v.Title)
		switch {
		case remaining < DangerRemaining:
			v.Icon = TrayDanger
		case remaining < WarningRemaining:
			v.Icon = TrayWarning
		default:
			v.Icon = TrayOK
		}
	}
	if expiring.Alert && v.Icon != TrayDanger {
		v.Icon = TrayExpiring
	}
	return v
}
