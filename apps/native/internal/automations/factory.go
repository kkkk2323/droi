package automations

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The Daemon's named cadences, at 09:00 UTC (droid's daemon-core
// automations/schedule.ts: daily every day, weekly on Mondays, monthly on
// the 1st).
var factoryCadences = map[string]string{
	"daily":   "0 9 * * *",
	"weekly":  "0 9 * * 1",
	"monthly": "0 9 1 * *",
}

var (
	factoryMonths = map[string]int{"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6, "JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12}
	factoryDays   = map[string]int{"SUN": 0, "MON": 1, "TUE": 2, "WED": 3, "THU": 4, "FRI": 5, "SAT": 6}
	letters       = regexp.MustCompile(`[A-Za-z]+`)
)

// FactorySchedule is a Factory automation's schedule, which the Daemon reads
// in UTC, as one of Droi's: a schedule the Automations page has a preset for
// (hourly, daily, weekdays, weekly) in local time, given the local zone's
// offset in seconds east of UTC, with timezone ""; any other kept as it is,
// with timezone "UTC". It reads the Daemon's daily, weekly and monthly,
// month and day names (JAN, MON-FRI) and steps from a number (5/15, which
// the Daemon reads as 5-59/15); anything else Droi's cron does not read is
// refused.
func FactorySchedule(expr string, offset int) (schedule, timezone string, err error) {
	utc, err := factoryCron(expr)
	if err != nil {
		return "", "", err
	}
	if s := Parse(utc, offset); s.Repeat != Custom {
		return s.Expression(0), "", nil
	}
	return utc, "UTC", nil
}

func factoryCron(expr string) (string, error) {
	trimmed := strings.TrimSpace(expr)
	if cron, ok := factoryCadences[strings.ToLower(trimmed)]; ok {
		return cron, nil
	}
	f := strings.Fields(trimmed)
	if len(f) != 5 {
		return "", fmt.Errorf("the schedule %q is not five cron fields", expr)
	}
	f[3] = named(f[3], factoryMonths)
	f[4] = named(f[4], factoryDays)
	for i, field := range f {
		f[i] = stepsFromNumbers(field, cronFields[i].hi)
	}
	cron := strings.Join(f, " ")
	if _, err := ParseCron(cron); err != nil {
		return "", fmt.Errorf("the schedule %q cannot be read: %w", expr, err)
	}
	return cron, nil
}

// named is field with the names in names as numbers, in any case.
func named(field string, names map[string]int) string {
	return letters.ReplaceAllStringFunc(field, func(w string) string {
		if n, ok := names[strings.ToUpper(w)]; ok {
			return strconv.Itoa(n)
		}
		return w
	})
}

// stepsFromNumbers writes a step from a number, "5/15", as the range up to
// the field's end, "5-59/15", as the Daemon reads it.
func stepsFromNumbers(field string, hi int) string {
	items := strings.Split(field, ",")
	for i, item := range items {
		start, step, ok := strings.Cut(item, "/")
		if !ok || start == "*" || strings.Contains(start, "-") {
			continue
		}
		if _, err := strconv.Atoi(start); err == nil {
			items[i] = start + "-" + strconv.Itoa(hi) + "/" + step
		}
	}
	return strings.Join(items, ",")
}

// ReadHeartbeat reads the HEARTBEAT.md in dir, a Factory automation's
// folder or one of Droi's.
func ReadHeartbeat(dir string) (Automation, error) {
	b, err := os.ReadFile(filepath.Join(dir, heartbeatFile))
	if err != nil {
		return Automation{}, err
	}
	return parseHeartbeat(string(b))
}
