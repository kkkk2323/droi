package llmusage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

// SpendChartDays is how many days Spend.Daily goes back, today included.
const SpendChartDays = 14

// Spend is the Standard quota the keys spent, read from the Key Pool's usage
// log, in percent of a Pro key's monthly quota (a sum over keys can pass
// 100, and a percent of a bigger plan's month counts as several).
type Spend struct {
	// Today is what was spent since local midnight.
	Today float64
	// UsualByNow and UsualDay average the Days whole days before today (up
	// to 7): up to this time of day, and the whole day. Days is 0 when the
	// log does not reach back to yesterday's midnight.
	UsualByNow, UsualDay float64
	Days                 int
	// Recent is the spend per day over the last 7×24 hours; HasRecent is
	// false with less than a day of log.
	Recent    float64
	HasRecent bool
	// Daily is each local day's spend, oldest first and ending with today,
	// for the days the log covers from their start, at most SpendChartDays.
	Daily []DaySpend
}

type DaySpend struct {
	Day   time.Time
	Spent float64
}

// ProjectedToday is today's spend if the rest of the day goes as the last
// days went from this hour on.
func (s Spend) ProjectedToday() (float64, bool) {
	if s.Days == 0 {
		return 0, false
	}
	return s.Today + math.Max(0, s.UsualDay-s.UsualByNow), true
}

type spendDelta struct {
	at    time.Time
	spent float64
}

// ReadSpend reads the usage log at path as of now. ok is false when the log
// is missing or holds no monthly Standard reading.
func ReadSpend(path string, now time.Time) (Spend, bool) {
	deltas, first, ok := readSpendDeltas(path)
	if !ok {
		return Spend{}, false
	}
	sum := func(from, to time.Time) float64 {
		total := 0.0
		for _, d := range deltas {
			if !d.at.Before(from) && d.at.Before(to) {
				total += d.spent
			}
		}
		return total
	}
	var s Spend
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	s.Today = sum(midnight, now)
	elapsed := now.Sub(midnight)
	for k := 1; k <= 7; k++ {
		start := midnight.AddDate(0, 0, -k)
		if start.Before(first) {
			break
		}
		s.UsualByNow += sum(start, start.Add(elapsed))
		s.UsualDay += sum(start, start.AddDate(0, 0, 1))
		s.Days++
	}
	if s.Days > 0 {
		s.UsualByNow /= float64(s.Days)
		s.UsualDay /= float64(s.Days)
	}
	since := now.Add(-7 * 24 * time.Hour)
	if first.After(since) {
		since = first
	}
	if span := now.Sub(since); span >= 24*time.Hour {
		s.Recent, s.HasRecent = sum(since, now)/span.Hours()*24, true
	}
	for k := SpendChartDays - 1; k >= 0; k-- {
		start := midnight.AddDate(0, 0, -k)
		if start.Before(first) && k > 0 {
			continue
		}
		end := start.AddDate(0, 0, 1)
		if end.After(now) {
			end = now
		}
		s.Daily = append(s.Daily, DaySpend{Day: start, Spent: sum(start, end)})
	}
	return s, true
}

// readSpendDeltas turns the log into rises of each key's monthly used
// percent, at the reading that saw them; first is the earliest reading.
func readSpendDeltas(path string) (deltas []spendDelta, first time.Time, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, time.Time{}, false
	}
	defer f.Close()
	last := map[string]float64{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var line keys.UsageSnapshot
		if json.Unmarshal(scanner.Bytes(), &line) != nil || line.Standard == nil || line.Standard.Monthly == nil {
			continue
		}
		m := line.Standard.Monthly
		end, ok := ResetAt(m)
		if !ok {
			continue
		}
		if first.IsZero() || line.Time.Before(first) {
			first = line.Time
		}
		// One series per key and monthly window.
		series := fmt.Sprintf("%d@%d", line.Index, end.Unix()/60)
		if prev, seen := last[series]; seen {
			if m.UsedPercent > prev {
				deltas = append(deltas, spendDelta{line.Time, (m.UsedPercent - prev) * keys.PlanScale(line.Plan)})
			}
			if m.UsedPercent < prev {
				continue
			}
		}
		last[series] = m.UsedPercent
	}
	return deltas, first, !first.IsZero()
}
