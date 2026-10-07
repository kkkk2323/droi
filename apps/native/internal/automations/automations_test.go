package automations

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	beijing = 8 * 3600
	denver  = -6 * 3600
	delhi   = 5*3600 + 1800
)

func TestExpressionIsInUTC(t *testing.T) {
	for _, c := range []struct {
		s      Schedule
		offset int
		want   string
	}{
		{Schedule{Repeat: Daily, Hour: 17}, beijing, "0 9 * * *"},
		{Schedule{Repeat: Daily, Hour: 7, Minute: 30}, beijing, "30 23 * * *"},
		{Schedule{Repeat: Daily, Hour: 20}, denver, "0 2 * * *"},
		{Schedule{Repeat: Weekdays, Hour: 9}, beijing, "0 1 * * 1-5"},
		{Schedule{Repeat: Weekdays, Hour: 7}, beijing, "0 23 * * 0-4"},
		{Schedule{Repeat: Weekdays, Hour: 20}, denver, "0 2 * * 2-6"},
		{Schedule{Repeat: Weekly, Hour: 7, Weekday: time.Sunday}, beijing, "0 23 * * 6"},
		{Schedule{Repeat: Weekly, Hour: 12, Weekday: time.Monday}, 0, "0 12 * * 1"},
		{Schedule{Repeat: Hourly, Minute: 5}, delhi, "35 * * * *"},
		{Schedule{Repeat: Hourly, Minute: 0}, beijing, "0 * * * *"},
		{Schedule{Repeat: Custom, Custom: " 0 9 1 * * "}, beijing, "0 9 1 * *"},
	} {
		if got := c.s.Expression(c.offset); got != c.want {
			t.Errorf("%+v at %d: %q, want %q", c.s, c.offset, got, c.want)
		}
	}
}

func TestParseReadsBackWhatExpressionWrote(t *testing.T) {
	for _, off := range []int{0, beijing, denver, delhi, -12 * 3600, 14 * 3600} {
		for _, r := range []Repeat{Daily, Weekdays, Weekly} {
			for h := 0; h < 24; h += 1 {
				for _, m := range []int{0, 15, 45} {
					for wd := time.Sunday; wd <= time.Saturday; wd++ {
						s := Schedule{Repeat: r, Hour: h, Minute: m}
						if r == Weekly {
							s.Weekday = wd
						} else if wd > time.Sunday {
							continue
						}
						if got := Parse(s.Expression(off), off); got != s {
							t.Fatalf("%+v at %d: wrote %q, read %+v", s, off, s.Expression(off), got)
						}
					}
				}
			}
		}
		s := Schedule{Repeat: Hourly, Minute: 20, Hour: 9}
		if got := Parse(s.Expression(off), off); got != s {
			t.Fatalf("%+v at %d: read %+v", s, off, got)
		}
	}
}

func TestParseLeavesOtherSchedulesCustom(t *testing.T) {
	for _, expr := range []string{"* * * * *", "0 9 1 * *", "*/15 * * * *", "0 9 * * 1,3", "0 9 * * 2-4", "every Monday at 9am PST", "monthly", ""} {
		if got := Parse(expr, beijing); got.Repeat != Custom || got.Custom != expr {
			t.Errorf("%q read as %+v", expr, got)
		}
	}
	if got := Parse("daily", beijing); got != (Schedule{Repeat: Daily, Hour: 17}) {
		t.Errorf("daily read as %+v", got)
	}
}

func TestDescribe(t *testing.T) {
	for expr, want := range map[string]string{
		"0 9 * * *":     "Every day at 17:00",
		"30 23 * * 0-4": "Weekdays at 07:30",
		"0 23 * * 6":    "Every Sunday at 07:00",
		"5 * * * *":     "Every hour at :05",
		"* * * * *":     "Every minute",
		"0 9 1 * *":     "0 9 1 * * (UTC)",
		"":              "No schedule",
	} {
		if got := Describe(expr, beijing); got != want {
			t.Errorf("%q: %q, want %q", expr, got, want)
		}
	}
}

func TestParseClock(t *testing.T) {
	for in, ok := range map[string]bool{"9:30": true, "09:05": true, "23:59": true, "24:00": false, "9:5": false, "9": false, "ab:cd": false} {
		if _, _, got := ParseClock(in); got != ok {
			t.Errorf("%q: %v", in, got)
		}
	}
}

func TestWhen(t *testing.T) {
	loc := time.FixedZone("CST", beijing)
	now := time.Date(2026, 10, 7, 21, 0, 0, 0, loc)
	for iso, want := range map[string]string{
		"2026-10-07T09:00:00.000Z": "today 17:00",
		"2026-10-08T01:00:00Z":     "tomorrow 09:00",
		"2026-10-06T15:59:00Z":     "yesterday 23:59",
		"2026-10-12T09:00:00Z":     "Oct 12, 17:00",
		"2025-12-31T09:00:00Z":     "Dec 31 2025, 17:00",
		"nope":                     "",
	} {
		if got := When(iso, now); got != want {
			t.Errorf("%s: %q, want %q", iso, got, want)
		}
	}
}

func TestID(t *testing.T) {
	taken := map[string]bool{"daily-review": true, "daily-review-2": true}
	for name, want := range map[string]string{
		"Daily review":      "daily-review-3",
		"  Triage: issues!": "triage-issues",
		"每日总结":              "automation",
		"a--b__c":           "a-b-c",
	} {
		if got := ID(name, taken); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
}

func TestWorkingDirectoryComesFromTheFrontMatter(t *testing.T) {
	dir := t.TempDir()
	if got := WorkingDirectory(dir); got != "" {
		t.Fatalf("no file: %q", got)
	}
	body := "---\nname: x\nschedule: '0 9 * * *'\nworkingDirectory: /Users/dev/acme-web\n---\n\nworkingDirectory: /not/this\n"
	_ = os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte(body), 0o644)
	if got := WorkingDirectory(dir); got != "/Users/dev/acme-web" {
		t.Fatalf("got %q", got)
	}
	_ = os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("---\nname: x\n---\nworkingDirectory: /no\n"), 0o644)
	if got := WorkingDirectory(dir); got != "" {
		t.Fatalf("got %q", got)
	}
}
