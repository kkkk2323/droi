package automations

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cron is a five-field cron expression: minute, hour, day of the month,
// month and day of the week, read in a time zone the caller gives.
type Cron struct {
	minute, hour, dom, month, dow uint64
	// A day field that starts with "*" is not a restriction, so a day
	// matches when both day fields do; when both are restricted, either is
	// enough (as cron has always read them).
	domStar, dowStar bool
}

type cronField struct {
	name   string
	lo, hi int
}

var cronFields = [5]cronField{
	{"minute", 0, 59},
	{"hour", 0, 23},
	{"day of the month", 1, 31},
	{"month", 1, 12},
	{"day of the week", 0, 7},
}

// ParseCron reads five fields of numbers, "*", lists ("1,15"), ranges
// ("1-5") and steps ("*/15", "1-30/5"); 0 and 7 are both Sunday. Names
// (MON, JAN), macros (@daily) and a sixth field are refused.
func ParseCron(expr string) (Cron, error) {
	expr = strings.TrimSpace(expr)
	if strings.HasPrefix(expr, "@") {
		return Cron{}, fmt.Errorf("%s: write the schedule as 5 fields, such as 0 9 * * *", expr)
	}
	f := strings.Fields(expr)
	if len(f) != 5 {
		return Cron{}, fmt.Errorf("%q has %d fields; a schedule has 5 fields: minute, hour, day of the month, month, day of the week", expr, len(f))
	}
	var sets [5]uint64
	for i, field := range f {
		set, err := parseCronField(field, cronFields[i])
		if err != nil {
			return Cron{}, err
		}
		sets[i] = set
	}
	if sets[4]&(1<<7) != 0 {
		sets[4] = sets[4]&^(1<<7) | 1
	}
	c := Cron{
		minute: sets[0], hour: sets[1], dom: sets[2], month: sets[3], dow: sets[4],
		domStar: strings.HasPrefix(f[2], "*"), dowStar: strings.HasPrefix(f[4], "*"),
	}
	if c.dowStar && !c.anyDate() {
		return Cron{}, fmt.Errorf("%q never runs: no month has that day", expr)
	}
	return c, nil
}

// anyDate is whether the day of the month and the month meet in some year
// (29 February counts).
func (c Cron) anyDate() bool {
	days := [13]int{0, 31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	for m := 1; m <= 12; m++ {
		if c.month&(1<<m) == 0 {
			continue
		}
		for d := 1; d <= days[m]; d++ {
			if c.dom&(1<<d) != 0 {
				return true
			}
		}
	}
	return false
}

func parseCronField(field string, spec cronField) (uint64, error) {
	bad := func(why string) error {
		return fmt.Errorf("%s field %q: %s", spec.name, field, why)
	}
	var set uint64
	for _, item := range strings.Split(field, ",") {
		if item == "" {
			return 0, bad("an empty item in the list")
		}
		rng, stepText, hasStep := strings.Cut(item, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepText)
			if err != nil || n < 1 {
				return 0, bad("the step after / must be a whole number from 1")
			}
			step = n
		}
		lo, hi := spec.lo, spec.hi
		switch {
		case rng == "*":
		case strings.Contains(rng, "-"):
			a, b, _ := strings.Cut(rng, "-")
			var err error
			if lo, err = cronNumber(a, spec); err != nil {
				return 0, bad(err.Error())
			}
			if hi, err = cronNumber(b, spec); err != nil {
				return 0, bad(err.Error())
			}
			if lo > hi {
				return 0, bad(fmt.Sprintf("the range %s runs backwards", rng))
			}
		default:
			if hasStep {
				return 0, bad("a step goes after * or a range, such as */15 or 0-30/5")
			}
			n, err := cronNumber(rng, spec)
			if err != nil {
				return 0, bad(err.Error())
			}
			lo, hi = n, n
		}
		for v := lo; v <= hi; v += step {
			set |= 1 << v
		}
	}
	return set, nil
}

func cronNumber(s string, spec cronField) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || strings.Trim(s, "0123456789") != "" {
		return 0, fmt.Errorf("%q is not a number (names such as MON or JAN are not supported)", s)
	}
	if n < spec.lo || n > spec.hi {
		return 0, fmt.Errorf("%d is outside %d-%d", n, spec.lo, spec.hi)
	}
	return n, nil
}

func (c Cron) dayMatches(t time.Time) bool {
	if c.month&(1<<int(t.Month())) == 0 {
		return false
	}
	dom := c.dom&(1<<t.Day()) != 0
	dow := c.dow&(1<<int(t.Weekday())) != 0
	if c.domStar || c.dowStar {
		return dom && dow
	}
	return dom || dow
}

// Next is the first time after after that the expression names, read as
// wall-clock times in loc. A wall time the clock skips when it springs
// forward runs at the moment of the jump; one it repeats when it falls back
// runs at its first occurrence only. Zero when nothing comes within nine
// years.
func (c Cron) Next(after time.Time, loc *time.Location) time.Time {
	local := after.In(loc)
	y, m, d := local.Date()
	// Wall times more than three hours before after's cannot come after it,
	// whatever the clock did in between.
	floor := wall(local).Add(-3 * time.Hour)
	day := time.Date(y, m, d-1, 0, 0, 0, 0, time.UTC)
	for end := day.AddDate(9, 0, 0); day.Before(end); day = day.AddDate(0, 0, 1) {
		if !c.dayMatches(day) {
			continue
		}
		for h := 0; h < 24; h++ {
			if c.hour&(1<<h) == 0 || day.Add(time.Duration(h+1)*time.Hour).Before(floor) {
				continue
			}
			for mi := 0; mi < 60; mi++ {
				if c.minute&(1<<mi) == 0 {
					continue
				}
				if t := instant(day.Year(), day.Month(), day.Day(), h, mi, loc); t.After(after) {
					return t
				}
			}
		}
	}
	return time.Time{}
}

// wall is t's wall clock as a UTC time, to compare wall times.
func wall(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}

// instant is when a wall time happens in loc: the first of its two
// instants when the clock falls back over it, the moment of the jump when
// the clock springs forward over it.
func instant(y int, mo time.Month, d, h, mi int, loc *time.Location) time.Time {
	t := time.Date(y, mo, d, h, mi, 0, 0, loc)
	want := time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
	got := wall(t)
	if !got.Equal(want) {
		start, end := t.ZoneBounds()
		if got.Before(want) {
			return end
		}
		return start
	}
	start, _ := t.ZoneBounds()
	if start.IsZero() {
		return t
	}
	_, off := t.Zone()
	_, prevOff := start.Add(-time.Nanosecond).Zone()
	if prevOff > off {
		if e := t.Add(-time.Duration(prevOff-off) * time.Second); e.Before(start) && wall(e.In(loc)).Equal(want) {
			return e
		}
	}
	return t
}
