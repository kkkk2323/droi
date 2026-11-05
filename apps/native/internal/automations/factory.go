package automations

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// FactorySchedule is a Factory automation's schedule as one of Droi's, for
// exactly the times the Daemon ran it (droid 1.4.1's daemon-core
// automations/schedule.ts, which matches in UTC, quirks included): a
// schedule the Automations page has a preset for (hourly, daily, weekdays,
// weekly) in local time, given the local zone's offset in seconds east of
// UTC, with timezone ""; any other with timezone "UTC". A schedule the
// Daemon did not read, or read but never matched, is refused.
func FactorySchedule(expr string, offset int) (schedule, timezone string, err error) {
	utc, err := factoryCron(expr)
	if err != nil {
		return "", "", fmt.Errorf("the schedule %q never ran in Factory: %s", expr, err)
	}
	if s := Parse(utc, offset); s.Repeat != Custom {
		return s.Expression(0), "", nil
	}
	return utc, "UTC", nil
}

// The Daemon's named cadences, matched as written: 09:00 UTC every day, on
// Mondays, on the 1st.
var factoryCadences = map[string]string{
	"daily":   "0 9 * * *",
	"weekly":  "0 9 * * 1",
	"monthly": "0 9 1 * *",
}

var (
	factoryMonths = map[string]int{"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6, "JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12}
	factoryDays   = map[string]int{"SUN": 0, "MON": 1, "TUE": 2, "WED": 3, "THU": 4, "FRI": 5, "SAT": 6}
)

func factoryCron(expr string) (string, error) {
	if cron, ok := factoryCadences[expr]; ok {
		return cron, nil
	}
	f := strings.Fields(expr)
	if len(f) != 5 || !jsValidField(f[0], 0, 59, nil) || !jsValidField(f[1], 0, 23, nil) || !jsValidField(f[2], 1, 31, nil) ||
		!jsValidField(f[3], 1, 12, factoryMonths) || !jsValidField(f[4], 0, 7, factoryDays) {
		return "", fmt.Errorf("the Daemon does not read it as a schedule")
	}
	minutes := daemonSteps(f[0], 0, 59)
	hours := daemonSteps(f[1], 0, 23)
	var doms, months []int
	if f[2] != "*" {
		doms = daemonSteps(f[2], 1, 31)
	}
	if f[3] != "*" {
		months = daemonMonths(f[3])
	}
	dows := daemonDays(f[4])
	never := fmt.Errorf("it names no time the Daemon matches")
	if len(minutes) == 0 || len(hours) == 0 || f[3] != "*" && len(months) == 0 || f[2] == "*" && len(dows) == 0 {
		return "", never
	}
	dom, dow := "*", "*"
	switch {
	case f[2] == "*":
		dow = compress(dows, 0, 6, true)
	case len(dows) == 7 || len(dows) == 0:
		if len(doms) == 0 {
			return "", never
		}
		dom = compress(doms, 1, 31, true)
	default:
		// Either day matches, which Droi's cron reads only when neither
		// field starts with "*".
		dom, dow = compress(doms, 1, 31, false), compress(dows, 0, 6, false)
		if len(doms) == 0 {
			dom = "*"
		}
	}
	month := "*"
	if f[3] != "*" {
		month = compress(months, 1, 12, true)
	}
	cron := strings.Join([]string{compress(minutes, 0, 59, true), compress(hours, 0, 23, true), dom, month, dow}, " ")
	if _, err := ParseCron(cron); err != nil {
		return "", never
	}
	return cron, nil
}

// jsInt is JavaScript's parseInt(s, 10): an optional sign and the digits
// that start s; ok is false for NaN.
func jsInt(s string) (int, bool) {
	s = strings.TrimLeft(s, " \t\n\r\v\f")
	i, sign := 0, 1
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		if s[i] == '-' {
			sign = -1
		}
		i++
	}
	j := i
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == i {
		return 0, false
	}
	n, err := strconv.Atoi(s[i:j])
	if err != nil {
		return 0, false
	}
	return sign * n, true
}

// splitTwo is JavaScript's const [a, b] = s.split(sep).
func splitTwo(s, sep string) (a, b string, hasB bool) {
	parts := strings.Split(s, sep)
	if len(parts) > 1 {
		return parts[0], parts[1], true
	}
	return parts[0], "", false
}

// jsToken is common's resolveCronToken: a number, else a name.
func jsToken(s string, names map[string]int) (int, bool) {
	if n, ok := jsInt(s); ok {
		return n, true
	}
	n, ok := names[strings.ToUpper(s)]
	return n, ok
}

// jsValidField is common's isValidCronFieldToken, which the Daemon checks a
// schedule with before it runs it.
func jsValidField(field string, lo, hi int, names map[string]int) bool {
	switch {
	case field == "*":
		return true
	case strings.Contains(field, "/"):
		rng, step, _ := splitTwo(field, "/")
		n, ok := jsInt(step)
		return ok && n > 0 && jsValidField(rng, lo, hi, names)
	case strings.Contains(field, "-"):
		a, b, hasB := splitTwo(field, "-")
		x, ok1 := jsToken(a, names)
		y, ok2 := jsToken(b, names)
		return hasB && ok1 && ok2 && x >= lo && x <= hi && y >= lo && y <= hi && x <= y
	case strings.Contains(field, ","):
		for _, part := range strings.Split(field, ",") {
			if !jsValidField(part, lo, hi, names) {
				return false
			}
		}
		return true
	}
	n, ok := jsToken(field, names)
	return ok && n >= lo && n <= hi
}

// set gathers values once, sorted.
type set map[int]bool

func (s set) sorted() []int {
	out := make([]int, 0, len(s))
	for v := range s {
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}

// loop is JavaScript's for (let i = start; i <= end; i += step), with NaN
// as !ok: a NaN step adds start once.
func (s set) loop(start int, startOK bool, end int, endOK bool, step int, stepOK bool) {
	if !startOK || !endOK {
		return
	}
	for i := start; i <= end; i += step {
		s[i] = true
		if !stepOK || step <= 0 {
			return
		}
	}
}

// daemonSteps is the Daemon's parseMinutes, parseHours and
// parseDaysOfMonth.
func daemonSteps(field string, lo, hi int) []int {
	if field == "*" {
		s := set{}
		s.loop(lo, true, hi, true, 1, true)
		return s.sorted()
	}
	s := set{}
	for _, part := range strings.Split(field, ",") {
		switch {
		case strings.Contains(part, "/"):
			rng, step, _ := splitTwo(part, "/")
			rs, re, hasRe := splitTwo(rng, "-")
			n, nOK := jsInt(step)
			start, startOK := lo, true
			end, endOK := hi, true
			if rng != "*" {
				start, startOK = jsInt(rs)
				if hasRe {
					end, endOK = jsInt(re)
				}
			}
			s.loop(start, startOK, end, endOK, n, nOK)
		case strings.Contains(part, "-"):
			a, b, _ := splitTwo(part, "-")
			x, xOK := jsInt(a)
			y, yOK := jsInt(b)
			s.loop(x, xOK, y, yOK, 1, true)
		default:
			if n, ok := jsInt(part); ok && n >= lo && n <= hi {
				s[n] = true
			}
		}
	}
	return s.sorted()
}

// daemonMonths is the Daemon's parseMonths: names in ranges and single
// values, not in steps.
func daemonMonths(field string) []int {
	s := set{}
	named := func(t string) (int, bool) {
		if n, ok := factoryMonths[strings.ToUpper(t)]; ok {
			return n, true
		}
		return jsInt(t)
	}
	for _, part := range strings.Split(field, ",") {
		switch {
		case strings.Contains(part, "/"):
			rng, step, _ := splitTwo(part, "/")
			rs, re, hasRe := splitTwo(rng, "-")
			n, nOK := jsInt(step)
			start, startOK := 1, true
			end, endOK := 12, true
			if rng != "*" {
				start, startOK = jsInt(rs)
				if hasRe {
					end, endOK = jsInt(re)
				}
			}
			s.loop(start, startOK, end, endOK, n, nOK)
		case strings.Contains(part, "-"):
			a, b, _ := splitTwo(part, "-")
			x, xOK := named(a)
			y, yOK := named(b)
			s.loop(x, xOK, y, yOK, 1, true)
		default:
			if n, ok := named(part); ok && n >= 1 && n <= 12 {
				s[n] = true
			}
		}
	}
	return s.sorted()
}

// daemonDays is the Daemon's parseDaysOfWeek: no steps (a part with one is
// read as the number it starts with), and 7 read as 0 at a range's ends.
func daemonDays(field string) []int {
	if field == "*" {
		return []int{0, 1, 2, 3, 4, 5, 6}
	}
	s := set{}
	named := func(t string) (int, bool) {
		if n, ok := factoryDays[strings.ToUpper(t)]; ok {
			return n, true
		}
		return jsInt(t)
	}
	for _, part := range strings.Split(field, ",") {
		if strings.Contains(part, "-") {
			a, b, _ := splitTwo(part, "-")
			x, xOK := named(a)
			y, yOK := named(b)
			if x == 7 {
				x = 0
			}
			if y == 7 {
				y = 0
			}
			s.loop(x, xOK, y, yOK, 1, true)
			continue
		}
		n, ok := named(part)
		if n == 7 {
			n = 0
		}
		if ok && n >= 0 && n <= 6 {
			s[n] = true
		}
	}
	return s.sorted()
}

// compress writes values (sorted, of lo-hi) as a cron field: "*", a step
// ("*/15", "5-50/15"), else ranges and numbers. Without star, it never
// starts with "*", which Droi's cron reads as no restriction of a day.
func compress(values []int, lo, hi int, star bool) string {
	if star && len(values) == hi-lo+1 {
		return "*"
	}
	if n := len(values); n >= 2 {
		step := values[1] - values[0]
		even := step > 1
		for i := 2; i < n && even; i++ {
			even = values[i]-values[i-1] == step
		}
		switch {
		case even && n >= 3 && star && values[0] == lo && values[n-1]+step > hi:
			return "*/" + strconv.Itoa(step)
		case even && n >= 3:
			return fmt.Sprintf("%d-%d/%d", values[0], values[n-1], step)
		}
	}
	var parts []string
	for i := 0; i < len(values); {
		j := i
		for j+1 < len(values) && values[j+1] == values[j]+1 {
			j++
		}
		if j > i {
			parts = append(parts, fmt.Sprintf("%d-%d", values[i], values[j]))
		} else {
			parts = append(parts, strconv.Itoa(values[i]))
		}
		i = j + 1
	}
	return strings.Join(parts, ",")
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

var factoryBookkeeping = []string{"HEARTBEAT.md", "memory/state.json", "memory/runs.json", ".memory-edit.lock"}

var factoryLockAttempt = regexp.MustCompile(`^\.memory-edit\.[0-9a-f]{32}\.pending$`)

// CopyFactoryFolder copies a Factory automation's folder to dst, which it
// makes: its memory, reports, skills, visuals and the rest, but not its
// HEARTBEAT.md (Droi writes its own) nor what the Daemon keeps there for
// itself (the identity and run records in memory/, the memory lock and its
// attempts). Links are copied as links, never followed. Anything it cannot
// copy fails the copy.
func CopyFactoryFolder(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.Mkdir(dst, info.Mode().Perm()|0o700); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		slash := filepath.ToSlash(rel)
		if slices.Contains(factoryBookkeeping, slash) || factoryLockAttempt.MatchString(slash) {
			return nil
		}
		to := filepath.Join(dst, rel)
		switch t := d.Type(); {
		case t&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(target, to)
		case d.IsDir():
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.Mkdir(to, info.Mode().Perm()|0o700)
		case t.IsRegular():
			return copyFile(path, to)
		}
		return fmt.Errorf("cannot copy %s: it is no file, folder or link", slash)
	})
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}
