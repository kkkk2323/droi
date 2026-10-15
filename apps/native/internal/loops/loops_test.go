package loops

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/protocol"
)

func TestParseIntervalReadsDroidsUnits(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"30s": 30 * time.Second, "10m": 10 * time.Minute, "30min": 30 * time.Minute, "2 hours": 2 * time.Hour,
		"1.5h": 90 * time.Minute, "1d": 24 * time.Hour, "5M": 5 * time.Minute,
	} {
		if got, ok := ParseInterval(in); !ok || got != want {
			t.Errorf("%q: %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "ten minutes", "5", "m5", "1.555h"} {
		if got, ok := ParseInterval(in); ok {
			t.Errorf("%q read as %v", in, got)
		}
	}
}

func TestCheckTakesOnlyWhatACronRepeatsExactly(t *testing.T) {
	for d, want := range map[time.Duration]error{
		30 * time.Second: ErrTooShort, 25 * time.Hour: ErrTooLong, 7 * time.Minute: ErrUneven, 90 * time.Minute: ErrUneven,
		5 * time.Hour: ErrUneven, time.Minute: nil, 15 * time.Minute: nil, 8 * time.Hour: nil, 24 * time.Hour: nil,
	} {
		if got := Check(d); !errors.Is(got, want) && got != want {
			t.Errorf("%v: %v, want %v", d, got, want)
		}
	}
}

func TestCronIsAnchoredToWhenTheLoopIsMade(t *testing.T) {
	at := time.Date(2026, 10, 15, 13, 55, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		15 * time.Minute: "10,25,40,55 * * * *",
		5 * time.Minute:  "*/5 * * * *",
		2 * time.Hour:    "55 1,3,5,7,9,11,13,15,17,19,21,23 * * *",
		24 * time.Hour:   "55 13 * * *",
	} {
		if got := Cron(d, at); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
		if back, _, _, ok := Interval(Cron(d, at)); !ok || back != d {
			t.Errorf("%v read back as %v %v", d, back, ok)
		}
	}
	// In UTC whatever the zone the time is in.
	if got := Cron(time.Hour, at.In(time.FixedZone("CST", 8*3600))); got != "55 */1 * * *" {
		t.Errorf("zone: %q", got)
	}
}

func TestIntervalReadsEvenSchedulesOnly(t *testing.T) {
	if d, h, m, ok := Interval("30 9 * * *"); !ok || d != 24*time.Hour || h != 9 || m != 30 {
		t.Errorf("daily: %v %d %d %v", d, h, m, ok)
	}
	if d, _, _, ok := Interval("* * * * *"); !ok || d != time.Minute {
		t.Errorf("every minute: %v %v", d, ok)
	}
	for _, e := range []string{"0 9 * * 1", "5,20 * * * *", "*/7 * * * *", "0,30 1 * * *", "nonsense"} {
		if d, _, _, ok := Interval(e); ok {
			t.Errorf("%q read as %v", e, d)
		}
	}
}

func TestParseCommandTakesBothOrders(t *testing.T) {
	for in, want := range map[string]Command{
		"10m check the build":            {Interval: 10 * time.Minute, Prompt: "check the build"},
		"check the build every 2h":       {Interval: 2 * time.Hour, Prompt: "check the build"},
		"30min":                          {Interval: 30 * time.Minute},
		"stop":                           {Stop: true},
		"5m 检查构建":                        {Interval: 5 * time.Minute, Prompt: "检查构建"},
		"run tests\nand report every 1h": {Interval: time.Hour, Prompt: "run tests\nand report"},
	} {
		if got, err := ParseCommand(in); err != nil || got != want {
			t.Errorf("%q: %+v %v", in, got, err)
		}
	}
	for in, want := range map[string]error{"": ErrNoInterval, "check the build": ErrNoInterval, "7m tidy": ErrUneven, "10s tidy": ErrTooShort} {
		if _, err := ParseCommand(in); !errors.Is(err, want) {
			t.Errorf("%q: %v, want %v", in, err, want)
		}
	}
}

func TestDefaultPromptFindsTheNearestLoopFile(t *testing.T) {
	cwd, home := t.TempDir(), t.TempDir()
	if got := DefaultPrompt(cwd, home); got != BuiltInPrompt {
		t.Errorf("none: %q", got)
	}
	write := func(root, dir, text string) {
		_ = os.MkdirAll(filepath.Join(root, dir), 0o755)
		if err := os.WriteFile(filepath.Join(root, dir, "loop.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(home, ".factory", "home prompt\n")
	if got := DefaultPrompt(cwd, home); got != "home prompt" {
		t.Errorf("home: %q", got)
	}
	write(cwd, ".claude", "project prompt")
	if got := DefaultPrompt(cwd, home); got != "project prompt" {
		t.Errorf("project: %q", got)
	}
}

func TestFromRecordKeepsSessionLoopsThatAreNotOver(t *testing.T) {
	rec := func(kind, status string) protocol.CronRecord {
		raw, _ := json.Marshal(map[string]any{
			"kind": kind, "id": "c1", "status": status, "source": "loop_command", "createdAt": "2026-10-15T10:00:00Z",
			"schedule": map[string]any{"expression": "*/10 * * * *", "recurring": true, "timezone": "UTC", "nextRunAt": "2026-10-15T10:10:00.000Z"},
			"stats":    map[string]any{"fireCount": 1},
			"scope":    map[string]any{"type": "session", "sessionId": "s1", "sessionCwd": "/w"},
			"payload":  map[string]any{"type": "prompt", "prompt": "check", "target": map[string]any{"type": "same_session"}},
		})
		var r protocol.CronRecord
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	l, ok := FromRecord(rec("session_prompt", "held"))
	if !ok || l.SessionID != "s1" || l.Prompt != "check" || !l.Live() || l.NextRunAt.IsZero() {
		t.Errorf("held: %+v %v", l, ok)
	}
	if l, _ := FromRecord(rec("session_prompt", "paused")); l.Live() {
		t.Error("a paused loop is live")
	}
	if _, ok := FromRecord(rec("session_prompt", "expired")); ok {
		t.Error("an expired loop is kept")
	}
}
