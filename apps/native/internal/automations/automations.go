// Package automations is what the Automations page needs beyond the
// Daemon's answers: schedules between the local clock and the Daemon's UTC
// cron, the words a schedule and a run read as, an id for a new
// Automation, and the Workspace its runs work in.
package automations

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/l10n"
)

// Repeat is how often a Schedule runs.
type Repeat string

const (
	Hourly   Repeat = "hourly"
	Daily    Repeat = "daily"
	Weekdays Repeat = "weekdays"
	Weekly   Repeat = "weekly"
	// Custom is a cron expression, or words the Daemon turns into one.
	Custom Repeat = "custom"
)

// Repeats are the choices in the order the page offers them.
var Repeats = []Repeat{Hourly, Daily, Weekdays, Weekly, Custom}

var RepeatLabels = map[Repeat]string{
	Hourly: l10n.N("Hourly"), Daily: l10n.N("Daily"), Weekdays: l10n.N("Weekdays"), Weekly: l10n.N("Weekly"), Custom: l10n.N("Custom"),
}

// Schedule is a schedule as the user picks it, in local time.
type Schedule struct {
	Repeat Repeat
	// Hour and Minute are local; Hourly uses only Minute.
	Hour, Minute int
	Weekday      time.Weekday
	Custom       string
}

// The Daemon reads every cron expression in UTC; offset is the local
// zone's, in seconds east of UTC.

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func mod(a, b int) int { return a - floorDiv(a, b)*b }

// Expression is the cron expression, in UTC, the Daemon is given.
func (s Schedule) Expression(offset int) string {
	off := offset / 60
	switch s.Repeat {
	case Hourly:
		return fmt.Sprintf("%d * * * *", mod(s.Minute-off, 60))
	case Daily, Weekdays, Weekly:
		total := s.Hour*60 + s.Minute - off
		shift, at := floorDiv(total, 1440), mod(total, 1440)
		dow := "*"
		switch s.Repeat {
		case Weekdays:
			dow = fmt.Sprintf("%d-%d", 1+shift, 5+shift)
		case Weekly:
			dow = strconv.Itoa(mod(int(s.Weekday)+shift, 7))
		}
		return fmt.Sprintf("%d %d * * %s", at%60, at/60, dow)
	}
	return strings.TrimSpace(s.Custom)
}

func number(field string, lo, hi int) (int, bool) {
	n, err := strconv.Atoi(field)
	return n, err == nil && n >= lo && n <= hi && strconv.Itoa(n) == field
}

// Parse reads a schedule the Daemon reports back into local time; what
// the presets cannot say stays Custom.
func Parse(expr string, offset int) Schedule {
	expr = strings.TrimSpace(expr)
	custom := Schedule{Repeat: Custom, Custom: expr, Hour: 9}
	// The Daemon's own words, at 09:00 UTC.
	switch expr {
	case "daily":
		expr = "0 9 * * *"
	case "weekly":
		expr = "0 9 * * 1"
	}
	f := strings.Fields(expr)
	if len(f) != 5 || f[2] != "*" || f[3] != "*" {
		return custom
	}
	off := offset / 60
	m, ok := number(f[0], 0, 59)
	if !ok {
		return custom
	}
	if f[1] == "*" {
		if f[4] != "*" {
			return custom
		}
		return Schedule{Repeat: Hourly, Minute: mod(m+off, 60), Hour: 9}
	}
	h, ok := number(f[1], 0, 23)
	if !ok {
		return custom
	}
	total := h*60 + m + off
	shift, at := floorDiv(total, 1440), mod(total, 1440)
	local := Schedule{Hour: at / 60, Minute: at % 60}
	switch {
	case f[4] == "*":
		local.Repeat = Daily
		return local
	case strings.Contains(f[4], "-"):
		lo, hi, _ := strings.Cut(f[4], "-")
		a, ok1 := number(lo, 0, 7)
		b, ok2 := number(hi, 0, 7)
		if ok1 && ok2 && b-a == 4 && a+shift == 1 {
			local.Repeat = Weekdays
			return local
		}
	default:
		if d, ok := number(f[4], 0, 7); ok {
			local.Repeat, local.Weekday = Weekly, time.Weekday(mod(d+shift, 7))
			return local
		}
	}
	return custom
}

// Clock is a local time of day as the page shows and reads it.
func Clock(hour, minute int) string { return fmt.Sprintf("%02d:%02d", hour, minute) }

// ParseClock reads "9:30" or "09:30".
func ParseClock(s string) (hour, minute int, ok bool) {
	h, m, found := strings.Cut(strings.TrimSpace(s), ":")
	if !found {
		return 0, 0, false
	}
	hour, err1 := strconv.Atoi(h)
	minute, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || len(m) != 2 || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

// Describe is a schedule in words, in local time.
func Describe(expr string, offset int) string {
	if strings.TrimSpace(expr) == "* * * * *" {
		return l10n.L("Every minute")
	}
	s := Parse(expr, offset)
	at := Clock(s.Hour, s.Minute)
	switch s.Repeat {
	case Hourly:
		return l10n.L("Every hour at :%02d", s.Minute)
	case Daily:
		return l10n.L("Every day at %s", at)
	case Weekdays:
		return l10n.L("Weekdays at %s", at)
	case Weekly:
		return everyWeekday(s.Weekday, at)
	}
	if expr == "" {
		return l10n.L("No schedule")
	}
	return l10n.L("%s (UTC)", expr)
}

// everyWeekday is one sentence per day, so a language can order its words.
func everyWeekday(d time.Weekday, at string) string {
	switch d {
	case time.Monday:
		return l10n.L("Every Monday at %s", at)
	case time.Tuesday:
		return l10n.L("Every Tuesday at %s", at)
	case time.Wednesday:
		return l10n.L("Every Wednesday at %s", at)
	case time.Thursday:
		return l10n.L("Every Thursday at %s", at)
	case time.Friday:
		return l10n.L("Every Friday at %s", at)
	case time.Saturday:
		return l10n.L("Every Saturday at %s", at)
	}
	return l10n.L("Every Sunday at %s", at)
}

// WeekdayShort is a day's short name, for the picker.
func WeekdayShort(d time.Weekday) string {
	switch d {
	case time.Monday:
		return l10n.L("Mon")
	case time.Tuesday:
		return l10n.L("Tue")
	case time.Wednesday:
		return l10n.L("Wed")
	case time.Thursday:
		return l10n.L("Thu")
	case time.Friday:
		return l10n.L("Fri")
	case time.Saturday:
		return l10n.L("Sat")
	}
	return l10n.L("Sun")
}

// When is a time from the Daemon as the page shows it: the day in words
// when near, and the local time.
func When(iso string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	t = t.In(now.Location())
	day := func(x time.Time) time.Time { y, m, d := x.Date(); return time.Date(y, m, d, 0, 0, 0, 0, x.Location()) }
	switch day(t).Sub(day(now)).Round(time.Hour) / (24 * time.Hour) {
	case 0:
		return l10n.L("today %s", t.Format("15:04"))
	case 1:
		return l10n.L("tomorrow %s", t.Format("15:04"))
	case -1:
		return l10n.L("yesterday %s", t.Format("15:04"))
	}
	// The layouts are catalog keys, so a language can give its own order.
	if t.Year() != now.Year() {
		return t.Format(l10n.L("Jan 2 2006, 15:04"))
	}
	return t.Format(l10n.L("Jan 2, 15:04"))
}

// StatusLabel is an Automation's status in words.
func StatusLabel(status string) string {
	switch status {
	case "active":
		return l10n.L("Active")
	case "paused":
		return l10n.L("Paused")
	case "invalid":
		return l10n.L("Invalid")
	case "":
		return l10n.L("Unknown")
	}
	return strings.ToUpper(status[:1]) + status[1:]
}

// RunLabel is a run's status in words, in English and marked for the
// catalog: the page compares it with "Running" and "Failed", and translates
// it with l10n.T where it shows it.
func RunLabel(status string) string {
	switch status {
	case "success", "completed":
		return l10n.N("Succeeded")
	case "in_progress", "running":
		return l10n.N("Running")
	case "failed", "failure", "error":
		return l10n.N("Failed")
	case "":
		return l10n.N("Unknown")
	}
	return strings.ToUpper(status[:1]) + strings.ReplaceAll(status[1:], "_", " ")
}

// ID is the folder name for a new Automation called name, unlike any taken.
func ID(name string, taken map[string]bool) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	id := strings.TrimRight(b.String(), "-")
	if len(id) > 48 {
		id = strings.TrimRight(id[:48], "-")
	}
	if id == "" {
		id = "automation"
	}
	if !taken[id] {
		return id
	}
	for n := 2; ; n++ {
		if next := id + "-" + strconv.Itoa(n); !taken[next] {
			return next
		}
	}
}

// WorkingDirectory is the Workspace an Automation's runs work in, from
// the front matter of its HEARTBEAT.md, which daemon.list_automations
// leaves out; "" means the Automation's own folder.
func WorkingDirectory(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "HEARTBEAT.md"))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			break
		}
		if v, ok := strings.CutPrefix(l, "workingDirectory:"); ok {
			return strings.Trim(strings.TrimSpace(v), `'"`)
		}
	}
	return ""
}
