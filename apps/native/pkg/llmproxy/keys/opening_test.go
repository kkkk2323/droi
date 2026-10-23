package keys

import (
	"testing"
	"time"
)

// monthUsage is a Standard key whose month ends at end ("" when it has not
// started), used percent of it.
func monthUsage(key string, used float64, end string, checked time.Time) *KeyUsage {
	u := usageWithPools(key, used, 0)
	u.LastCheckedAt = checked
	u.StandardLimits = &PoolLimits{Monthly: &BucketLimit{UsedPercent: used, WindowEnd: end}}
	return u
}

func openingRotator(t *testing.T, now time.Time) *KeyRotator {
	t.Helper()
	r := testRotator("open", "ended", "never", "never-2")
	r.SetClockForTest(func() time.Time { return now })
	day := 24 * time.Hour
	r.usages["open"] = monthUsage("open", 50, now.Add(20*day).Format(time.RFC3339), now)
	r.usages["ended"] = monthUsage("ended", 0, now.Add(-day).Format(time.RFC3339), now)
	r.usages["never"] = monthUsage("never", 0, "", now)
	r.usages["never-2"] = monthUsage("never-2", 0, "", now)
	return r
}

func TestOpeningOpensTwoKeysADayInQueueOrder(t *testing.T) {
	now := time.Date(2026, 10, 16, 10, 0, 0, 0, time.Local)
	r := openingRotator(t, now)
	if st := r.OpeningStatus(); st.OpenedToday != 0 || len(st.Queue) != 3 || st.Queue[0] != 3 || st.Queue[1] != 4 || st.Queue[2] != 2 {
		t.Fatalf("status = %+v", st)
	}
	var got []string
	for i := 0; i < 4; i++ {
		key, _ := r.SelectKeyForModel("", "claude-fable-5.1", nil)
		got = append(got, key)
	}
	// Never-opened keys first, then the open key once today's two are done.
	if got[0] != "never" || got[1] != "never-2" || got[2] != "open" || got[3] != "open" {
		t.Fatalf("routed %v", got)
	}
	if st := r.OpeningStatus(); st.OpenedToday != 2 || len(st.Queue) != 1 || st.Queue[0] != 2 {
		t.Fatalf("status after = %+v", st)
	}

	// Next day the queue moves on.
	r.SetClockForTest(func() time.Time { return now.Add(24 * time.Hour) })
	r.usages["never"] = monthUsage("never", 5, now.Add(30*24*time.Hour).Format(time.RFC3339), now.Add(24*time.Hour))
	r.usages["never-2"] = monthUsage("never-2", 5, now.Add(30*24*time.Hour).Format(time.RFC3339), now.Add(24*time.Hour))
	if key, _ := r.SelectKeyForModel("", "claude-fable-5.1", nil); key != "ended" {
		t.Fatalf("next day routed %s", key)
	}
}

func TestOpeningOpensEarlyWhenOpenKeysAreSpent(t *testing.T) {
	now := time.Date(2026, 10, 16, 10, 0, 0, 0, time.Local)
	r := openingRotator(t, now)
	started := now.Add(-time.Hour).Add(30 * 24 * time.Hour).Format(time.RFC3339)
	// Two months already started today.
	r.usages["never"] = monthUsage("never", 1, started, now)
	r.usages["never-2"] = monthUsage("never-2", 1, started, now)
	if key, _ := r.SelectKeyForModel("", "claude-fable-5.1", nil); key == "ended" {
		t.Fatal("opened a third key while open keys have room")
	}
	for _, k := range []string{"open", "never", "never-2"} {
		r.usages[k].StandardPercent = ptrTo(98)
	}
	if key, _ := r.SelectKeyForModel("", "claude-fable-5.1", nil); key != "ended" {
		t.Fatalf("every open key is spent; routed %s", key)
	}
}

func TestOpeningNeedsARealRequest(t *testing.T) {
	now := time.Date(2026, 10, 16, 10, 0, 0, 0, time.Local)
	r := openingRotator(t, now)
	if key := r.selectActiveKey(); key != "open" {
		t.Fatalf("the active key must not open a month; got %s", key)
	}
	if key, _ := r.PreviewKeyForModel("claude-fable-5.1"); key != "never" {
		t.Fatalf("preview = %s", key)
	}
	if st := r.OpeningStatus(); st.OpenedToday != 0 {
		t.Fatalf("a preview counted as an opening: %+v", st)
	}
	// Legacy keys without a monthly bucket are not planned.
	legacy := testRotator("a")
	legacy.usages["a"] = usageWithPools("a", 0, 0)
	if st := legacy.OpeningStatus(); len(st.Queue) != 0 {
		t.Fatalf("legacy queued: %+v", st)
	}
}

func ptrTo(v float64) *float64 { return &v }

func TestScheduledOpeningLeavesRequestsOnOpenKeys(t *testing.T) {
	now := time.Date(2026, 10, 16, 10, 0, 0, 0, time.Local)
	r := openingRotator(t, now)
	r.SetScheduledOpening(true)
	if key, _ := r.SelectKeyForModel("", "claude-fable-5.1", nil); key != "open" {
		t.Fatalf("routed %s; the scheduled opener opens months", key)
	}
	if key, ok := r.ClaimNextOpening(); !ok || key != "never" {
		t.Fatalf("claim = %s %v", key, ok)
	}
	if key, ok := r.ClaimNextOpening(); !ok || key != "never-2" {
		t.Fatalf("second claim = %s %v", key, ok)
	}
	if _, ok := r.ClaimNextOpening(); ok {
		t.Fatal("claimed past the daily quota")
	}
	r.ReleaseOpening("never-2")
	if st := r.OpeningStatus(); st.OpenedToday != 1 || !st.Scheduled {
		t.Fatalf("status = %+v", st)
	}
	// Every open key spent: a request still opens one at once.
	for _, k := range []string{"open", "never"} {
		r.usages[k].StandardPercent = ptrTo(98)
	}
	if key, _ := r.SelectKeyForModel("", "claude-fable-5.1", nil); key != "never-2" {
		t.Fatalf("spent pool routed %s", key)
	}
}
