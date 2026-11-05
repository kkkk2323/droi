package automations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFactorySchedule(t *testing.T) {
	const shanghai, kolkata = 8 * 3600, 5*3600 + 1800
	for _, c := range []struct {
		expr                   string
		offset                 int
		wantSchedule, wantZone string
	}{
		{"daily", shanghai, "0 17 * * *", ""},
		{" Weekly ", shanghai, "0 17 * * 1", ""},
		{"monthly", shanghai, "0 9 1 * *", "UTC"},
		{"0 1 * * *", shanghai, "0 9 * * *", ""},
		{"0 9 * * mon-fri", shanghai, "0 17 * * 1-5", ""},
		{"0 9 * * MON-FRI", 0, "0 9 * * 1-5", ""},
		{"0 9 * * SUN", shanghai, "0 17 * * 0", ""},
		{"30 23 * * MON-FRI", shanghai, "30 23 * * 1-5", "UTC"},
		{"15 * * * *", kolkata, "45 * * * *", ""},
		{"5/15 * * * *", shanghai, "5-59/15 * * * *", "UTC"},
		{"0 9/2 * * *", shanghai, "0 9-23/2 * * *", "UTC"},
		{"*/30 * * * *", shanghai, "*/30 * * * *", "UTC"},
		{"0 9 * JAN,jul *", shanghai, "0 9 * 1,7 *", "UTC"},
		{"0 9 1 * *", shanghai, "0 9 1 * *", "UTC"},
	} {
		s, z, err := FactorySchedule(c.expr, c.offset)
		if err != nil || s != c.wantSchedule || z != c.wantZone {
			t.Errorf("FactorySchedule(%q, %d) = %q, %q, %v; want %q, %q", c.expr, c.offset, s, z, err, c.wantSchedule, c.wantZone)
		}
	}
	for _, expr := range []string{"", "@daily", "every day at 9am", "0 0 9 * * *", "0 9 ? * *", "0 9 L * *", "0 9 * * 1#2", "0 9 * * FUNDAY"} {
		if s, _, err := FactorySchedule(expr, shanghai); err == nil || !strings.Contains(err.Error(), "schedule") {
			t.Errorf("FactorySchedule(%q) = %q, %v; want a refusal", expr, s, err)
		}
	}
}

func TestReadHeartbeat(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("---\nname: Daily\nschedule: daily\nreasoningEffort: high\nworkingDirectory: /Users/me/app\n---\n\nDo it.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := ReadHeartbeat(dir)
	if err != nil || a.Name != "Daily" || a.ReasoningEffort != "high" || a.WorkingDirectory != "/Users/me/app" || a.Prompt != "Do it." {
		t.Fatalf("%+v %v", a, err)
	}
	if _, err := ReadHeartbeat(t.TempDir()); err == nil {
		t.Fatal("no file, no error")
	}
}
