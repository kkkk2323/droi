package factory

import (
	"encoding/json"
	"testing"
	"time"
)

// usage.test.mjs's reading of a window's end: droid hands windowEnd to new Date, so an ISO time and epoch
// milliseconds, a number or a string of digits, all name the same instant.
func TestWindowEnd(t *testing.T) {
	want := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for raw, at := range map[string]time.Time{
		`"2026-10-01T05:00:00Z"`:      want("2026-10-01T05:00:00Z"),
		`"2099-10-15T09:24:36.485Z"`:  want("2099-10-15T09:24:36.485Z"),
		`"2026-10-01T07:00:00+02:00"`: want("2026-10-01T05:00:00Z"),
		`"2026-10-01t05:00:00z"`:      want("2026-10-01T05:00:00Z"),
		`1791763200000`:               want("2026-10-12T00:00:00Z"),
		`1791763200000.9`:             want("2026-10-12T00:00:00Z"),
		`"1793000000000"`:             want("2026-10-26T07:33:20Z"),
	} {
		got, ok := WindowEnd(json.RawMessage(raw))
		if !ok || !got.Equal(at) {
			t.Errorf("%s: %v %v, want %v", raw, got, ok, at)
		}
	}
	for _, raw := range []string{`"soon"`, `""`, `null`, `0`, `-5`, `"0"`, `"-12"`, `true`, `{}`, ``, `"2026-10-01"`} {
		if got, ok := WindowEnd(json.RawMessage(raw)); ok {
			t.Errorf("%s read as %v", raw, got)
		}
	}
}
