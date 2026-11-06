package automations

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreKeepsWhereAMovedAutomationCameFrom(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	a, err := s.Save(Automation{Name: "Daily", Schedule: "0 9 * * *", Prompt: "p", ForkedFrom: "droid"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(s.Dir, a.ID, "HEARTBEAT.md"))
	if !strings.Contains(string(b), "\nforkedFrom: droid\n") {
		t.Fatalf("HEARTBEAT.md:\n%s", b)
	}
	a.ForkedFrom = ""
	a.Name = "Daily, renamed"
	if _, err := s.Save(a); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(a.ID); got.ForkedFrom != "droid" || got.Name != "Daily, renamed" {
		t.Fatalf("after an edit %+v", got)
	}
}

func TestStoreTellsAnUnreadableAutomationAndRepairsIt(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(s.Dir, "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "broken", "HEARTBEAT.md"), []byte("not front matter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("broken"); !errors.Is(err, ErrUnreadable) || !strings.Contains(err.Error(), "HEARTBEAT.md has no front matter") {
		t.Fatalf("Get: %v", err)
	}
	if err := s.SetPaused("broken", true); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("SetPaused: %v", err)
	}
	if _, err := s.Save(Automation{ID: "broken", Name: "Fixed", Schedule: "0 9 * * *", Prompt: "p"}); err != nil {
		t.Fatalf("saving over it: %v", err)
	}
	if got, err := s.Get("broken"); err != nil || got.Name != "Fixed" {
		t.Fatalf("after the repair %+v %v", got, err)
	}
}

func TestStoreRefusalsAreInvalidErrors(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	var invalid *InvalidError
	if _, err := s.Save(Automation{Name: "x", Schedule: "@daily", Prompt: "p"}); !errors.As(err, &invalid) || !strings.Contains(invalid.Reason, "@daily") {
		t.Fatalf("a bad schedule: %v", err)
	}
	for _, id := range []string{"../x", ".hidden", "a:b"} {
		if _, err := s.Save(Automation{ID: id, Name: "x", Schedule: "0 9 * * *", Prompt: "p"}); !errors.As(err, &invalid) {
			t.Errorf("Save id %q: %v", id, err)
		}
	}
}

func TestStoreAdoptsAPreparedFolder(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	prepared := filepath.Join(s.Dir, ".move-1")
	if err := os.MkdirAll(filepath.Join(prepared, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared, "memory", "notes.md"), []byte("notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC)
	a, err := s.Adopt(Automation{Name: "Droid binary diff", Schedule: "0 9 * * *", Prompt: "p", CreatedAt: created}, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "droid-binary-diff" {
		t.Fatalf("id %q", a.ID)
	}
	if b, err := os.ReadFile(filepath.Join(s.Dir, "droid-binary-diff", "memory", "notes.md")); err != nil || string(b) != "notes" {
		t.Fatalf("notes %q %v", b, err)
	}
	if got, err := s.Get(a.ID); err != nil || got.Name != "Droid binary diff" || !got.CreatedAt.Equal(created) {
		t.Fatalf("got %+v %v", got, err)
	}
	if _, err := os.Stat(prepared); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the prepared folder is still there: %v", err)
	}
	if _, err := s.Adopt(Automation{Name: "x", Schedule: "@daily", Prompt: "p"}, filepath.Join(s.Dir, ".move-2")); err == nil {
		t.Fatal("adopted a bad schedule")
	}
}

func TestStoreKeepsTheFactoryNameOfAMovedAutomation(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	a, err := s.Save(Automation{Name: "Daily", Schedule: "0 9 * * *", Prompt: "p", ForkedFrom: "0b9c-uuid", ForkedFromName: "daily-report"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(s.Dir, a.ID, "HEARTBEAT.md"))
	if !strings.Contains(string(b), "\nforkedFrom: 0b9c-uuid\nforkedFromName: daily-report\n") {
		t.Fatalf("HEARTBEAT.md:\n%s", b)
	}
	a.ForkedFrom, a.ForkedFromName = "", ""
	if _, err := s.Save(a); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(a.ID); got.ForkedFrom != "0b9c-uuid" || got.ForkedFromName != "daily-report" {
		t.Fatalf("after an edit %+v", got)
	}
}

func TestStoreCleansAFolderAndAModel(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for in, want := range map[string]string{
		" /Users/me/re ": "/Users/me/re", "/Users/me/re/": "/Users/me/re", "/Users/me/./re": "/Users/me/re", "/Users/me/x/../re": "/Users/me/re", "": "",
	} {
		a, err := s.Save(Automation{Name: "Daily", Schedule: "0 9 * * *", Prompt: "p", WorkingDirectory: in, Model: " glm-5.3-flash\t", ReasoningEffort: " high "})
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		got, _ := s.Get(a.ID)
		if got.WorkingDirectory != want || got.Model != "glm-5.3-flash" || got.ReasoningEffort != "high" {
			t.Errorf("%q: %+v", in, got)
		}
		if err := s.Delete(a.ID); err != nil {
			t.Fatal(err)
		}
	}
	var invalid *InvalidError
	for _, rel := range []string{"re", "./re", "~/re"} {
		if _, err := s.Save(Automation{Name: "Daily", Schedule: "0 9 * * *", Prompt: "p", WorkingDirectory: rel}); !errors.As(err, &invalid) || !strings.Contains(invalid.Reason, "full path") {
			t.Errorf("%q: %v", rel, err)
		}
	}
}

func TestTheZoneDatabaseIsThere(t *testing.T) {
	if _, err := time.LoadLocation("Asia/Shanghai"); err != nil {
		t.Fatal(err)
	}
}
