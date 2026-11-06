package automations

import (
	"fmt"
	"slices"
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
	// A minute or hour field that starts with "*" makes an interval
	// schedule, which follows the clock across a daylight-saving change
	// (Vixie cron's rule); see Next.
	minStar, hourStar bool
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

// searchYears bounds Next. A date falls on each day of the week within 28
// years, so a schedule that runs at all runs within it.
const searchYears = 30

// ParseCron reads five fields of numbers, "*", lists ("1,15"), ranges
// ("1-5") and steps ("*/15", "1-30/5"); 0 and 7 are both Sunday. Names
// (MON, JAN), macros (@daily), a sixth field and a schedule that never
// runs are refused.
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
		minStar: strings.HasPrefix(f[0], "*"), hourStar: strings.HasPrefix(f[1], "*"),
	}
	if c.Next(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), time.UTC).IsZero() {
		return Cron{}, fmt.Errorf("%q never runs: no day matches both its day and its month", expr)
	}
	return c, nil
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

// dstSlack is more than any daylight-saving shift: an instant of a wall
// time this much later than another's cannot come before it.
const dstSlack = 3 * time.Hour

// Next is the first time after after that the expression names, read as
// wall-clock times in loc; zero when nothing comes within 30 years.
//
// Across a daylight-saving change it follows Vixie cron. A fixed-time
// schedule (minute and hour fields without "*") runs a wall time the clock
// skips at the moment of the jump, and one the clock repeats at its first
// occurrence only. An interval schedule (minute or hour field starting with
// "*") follows the clock: a skipped wall time does not run, a repeated one
// runs at both occurrences.
func (c Cron) Next(after time.Time, loc *time.Location) time.Time {
	local := after.In(loc)
	y, m, d := local.Date()
	floor := wall(local).Add(-dstSlack)
	interval := c.minStar || c.hourStar
	var best, bestWall time.Time
	day := time.Date(y, m, d-1, 0, 0, 0, 0, time.UTC)
	for end := day.AddDate(searchYears, 0, 0); day.Before(end); day = day.AddDate(0, 0, 1) {
		if !best.IsZero() && day.After(bestWall.Add(dstSlack)) {
			return best
		}
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
				w := day.Add(time.Duration(h)*time.Hour + time.Duration(mi)*time.Minute)
				if !best.IsZero() && w.After(bestWall.Add(dstSlack)) {
					return best
				}
				for _, t := range instants(w, loc, interval) {
					if t.After(after) && (best.IsZero() || t.Before(best)) {
						best, bestWall = t, w
					}
				}
			}
		}
	}
	return best
}

// wall is t's wall clock as a UTC time, to compare wall times.
func wall(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}

func offset(t time.Time) int {
	_, off := t.Zone()
	return off
}

// instants are when wall time w (given as UTC) happens in loc, earliest
// first: one, or two when the clock falls back over it. When the clock
// springs forward over it, an interval schedule (all) has none and a
// fixed-time one the moment of the jump; a fixed-time one keeps only the
// first of two.
func instants(w time.Time, loc *time.Location, all bool) []time.Time {
	t := time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), 0, 0, loc)
	start, end := t.ZoneBounds()
	offsets := []int{offset(t)}
	if !start.IsZero() {
		offsets = append(offsets, offset(start.Add(-time.Nanosecond)))
	}
	if !end.IsZero() {
		offsets = append(offsets, offset(end))
	}
	var out []time.Time
	for _, off := range offsets {
		u := w.Add(-time.Duration(off) * time.Second).In(loc)
		if wall(u).Equal(w) && !slices.ContainsFunc(out, u.Equal) {
			out = append(out, u)
		}
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	switch {
	case len(out) == 0 && all:
		return nil
	case len(out) == 0 && wall(t).Before(w):
		return []time.Time{end}
	case len(out) == 0:
		return []time.Time{start}
	case !all:
		return out[:1]
	}
	return out
}
