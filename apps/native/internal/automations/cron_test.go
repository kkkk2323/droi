package automations

import (
	"strings"
	"testing"
	"time"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The wanted times are worked out by hand from a calendar: 2026-11-05 is a
// Thursday, 2026-12-13 a Sunday, 2028-02-29 the next 29 February.
func TestCronNext(t *testing.T) {
	for _, c := range []struct {
		expr, zone, after, want string
	}{
		{"0 9 * * *", "Asia/Shanghai", "2026-11-05T08:59:59+08:00", "2026-11-05T09:00:00+08:00"},
		{"0 9 * * *", "Asia/Shanghai", "2026-11-05T09:00:00+08:00", "2026-11-06T09:00:00+08:00"},
		{"0 9 * * *", "Asia/Kolkata", "2026-11-05T03:00:00Z", "2026-11-05T03:30:00Z"},
		{"*/15 * * * *", "UTC", "2026-11-05T10:07:00Z", "2026-11-05T10:15:00Z"},
		{"*/15 * * * *", "UTC", "2026-11-05T10:45:00Z", "2026-11-05T11:00:00Z"},
		{"1-30/5 * * * *", "UTC", "2026-11-05T10:27:00Z", "2026-11-05T11:01:00Z"},
		{"5,35 8-10 * * *", "UTC", "2026-11-05T10:36:00Z", "2026-11-06T08:05:00Z"},
		{"0 9 * * 1-5", "Asia/Shanghai", "2026-11-06T10:00:00+08:00", "2026-11-09T09:00:00+08:00"},
		{"0 0 * * 7", "UTC", "2026-11-05T12:00:00Z", "2026-11-08T00:00:00Z"},
		{"0 0 * * 0", "UTC", "2026-11-05T12:00:00Z", "2026-11-08T00:00:00Z"},
		{"0 0 * * 5-7", "UTC", "2026-11-05T12:00:00Z", "2026-11-06T00:00:00Z"},
		// Both days restricted: the 13th or a Friday.
		{"0 12 13 * 5", "UTC", "2026-12-12T00:00:00Z", "2026-12-13T12:00:00Z"},
		{"0 12 13 * 5", "UTC", "2026-12-13T12:00:00Z", "2026-12-18T12:00:00Z"},
		// A day field that starts with * does not count as restricted: odd days and Fridays.
		{"0 12 */2 * 5", "UTC", "2026-11-05T00:00:00Z", "2026-11-13T12:00:00Z"},
		{"30 2 29 2 *", "UTC", "2026-01-01T00:00:00Z", "2028-02-29T02:30:00Z"},
		{"0 0 1 1,7 *", "UTC", "2026-02-01T00:00:00Z", "2026-07-01T00:00:00Z"},
		{"* * * * *", "UTC", "2026-11-05T10:07:30Z", "2026-11-05T10:08:00Z"},
		{"59 23 31 12 *", "UTC", "2026-12-31T23:59:00Z", "2027-12-31T23:59:00Z"},
	} {
		cron, err := ParseCron(c.expr)
		if err != nil {
			t.Fatalf("%q: %v", c.expr, err)
		}
		got := cron.Next(at(t, c.after), zone(t, c.zone))
		if want := at(t, c.want); !got.Equal(want) {
			t.Errorf("%q in %s after %s: %s, want %s", c.expr, c.zone, c.after, got.UTC().Format(time.RFC3339), want.UTC().Format(time.RFC3339))
		}
	}
}

// In New York, 2026-03-08 springs from 02:00 EST to 03:00 EDT (07:00 UTC)
// and 2026-11-01 falls back from 02:00 EDT to 01:00 EST (06:00 UTC). A time
// the spring skips runs when the clock jumps over it; a time the fall
// repeats runs at its first occurrence only.
func TestCronNextAcrossDaylightSaving(t *testing.T) {
	ny := zone(t, "America/New_York")
	for _, c := range []struct {
		expr, after string
		want        []string
	}{
		{"30 2 * * *", "2026-03-07T08:00:00Z", []string{"2026-03-08T07:00:00Z", "2026-03-09T06:30:00Z"}},
		{"0 3 * * *", "2026-03-08T06:00:00Z", []string{"2026-03-08T07:00:00Z", "2026-03-09T07:00:00Z"}},
		{"*/30 * * * *", "2026-03-08T06:45:00Z", []string{"2026-03-08T07:00:00Z", "2026-03-08T07:30:00Z"}},
		{"0 9 * * *", "2026-03-07T15:00:00Z", []string{"2026-03-08T13:00:00Z", "2026-03-09T13:00:00Z"}},
		{"30 1 * * *", "2026-10-31T16:00:00Z", []string{"2026-11-01T05:30:00Z", "2026-11-02T06:30:00Z"}},
		{"0 * * * *", "2026-11-01T04:30:00Z", []string{"2026-11-01T05:00:00Z", "2026-11-01T07:00:00Z", "2026-11-01T08:00:00Z"}},
		{"0 9 * * *", "2026-10-31T14:00:00Z", []string{"2026-11-01T14:00:00Z", "2026-11-02T14:00:00Z"}},
	} {
		cron, err := ParseCron(c.expr)
		if err != nil {
			t.Fatal(err)
		}
		after := at(t, c.after)
		for _, w := range c.want {
			got := cron.Next(after, ny)
			if !got.Equal(at(t, w)) {
				t.Errorf("%q after %s: %s, want %s", c.expr, after.UTC().Format(time.RFC3339), got.UTC().Format(time.RFC3339), w)
				break
			}
			after = got
		}
	}
}

func TestCronNextNeverRunsTwiceInTheRepeatedHour(t *testing.T) {
	ny := zone(t, "America/New_York")
	cron, err := ParseCron("*/10 1 * * *")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for after := at(t, "2026-11-01T04:00:00Z"); ; {
		next := cron.Next(after, ny)
		if next.After(at(t, "2026-11-01T12:00:00Z")) {
			break
		}
		wall := next.In(ny).Format("15:04")
		if seen[wall] {
			t.Fatalf("%s runs twice (again at %s)", wall, next.UTC().Format(time.RFC3339))
		}
		seen[wall] = true
		after = next
	}
	if len(seen) != 6 {
		t.Fatalf("ran at %v, want the six times of 01:xx once", seen)
	}
}

func TestParseCronRefuses(t *testing.T) {
	for expr, want := range map[string]string{
		"":              "5 fields",
		"* * * *":       "5 fields",
		"* * * * * *":   "5 fields",
		"@daily":        "@daily",
		"0 9 * * MON":   "MON",
		"0 9 * JAN *":   "JAN",
		"60 * * * *":    "0-59",
		"* 24 * * *":    "0-23",
		"* * 0 * *":     "1-31",
		"* * 32 * *":    "1-31",
		"* * * 13 *":    "1-12",
		"* * * * 8":     "0-7",
		"*/0 * * * *":   "step",
		"5-1 * * * *":   "5-1",
		"5/15 * * * *":  "5/15",
		"1,,2 * * * *":  "1,,2",
		"-5 * * * *":    "-5",
		"? * * * *":     "?",
		"0 0 31 2 *":    "never",
		"0 0 30,31 2 *": "never",
	} {
		_, err := ParseCron(expr)
		if err == nil {
			t.Errorf("%q: no error", expr)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want it to say %q", expr, err, want)
		}
	}
}
