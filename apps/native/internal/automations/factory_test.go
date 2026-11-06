package automations

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// The Daemon (droid 1.4.1, daemon-core automations/schedule.ts) matches in
// UTC; daily, weekly and monthly are 09:00 UTC every day, on Mondays and on
// the 1st. The wanted schedules are worked out by hand from what it matched.
func TestFactorySchedule(t *testing.T) {
	const (
		shanghai  = 8 * 3600
		kolkata   = 5*3600 + 1800
		kathmandu = 5*3600 + 2700
		newYork   = -5 * 3600
		honolulu  = -10 * 3600
	)
	for _, c := range []struct {
		expr                   string
		offset                 int
		wantSchedule, wantZone string
	}{
		{"daily", shanghai, "0 17 * * *", ""},
		{"weekly", shanghai, "0 17 * * 1", ""},
		{"monthly", shanghai, "0 9 1 * *", "UTC"},
		{"0 1 * * *", shanghai, "0 9 * * *", ""},
		{"0 9 * * mon-fri", shanghai, "0 17 * * 1-5", ""},
		{"0 9 * * MON-FRI", 0, "0 9 * * 1-5", ""},
		{"0 9 * * SUN", shanghai, "0 17 * * 0", ""},
		{"30 23 * * MON-FRI", shanghai, "30 23 * * 1-5", "UTC"},
		{"15 * * * *", kolkata, "45 * * * *", ""},
		{"0 9 * * *", kathmandu, "45 14 * * *", ""},
		{"15 * * * *", kathmandu, "0 * * * *", ""},
		{"0 9 * * *", newYork, "0 4 * * *", ""},
		{"0 2 * * 2-6", newYork, "0 21 * * 1-5", ""},
		{"0 2 * * 1-5", newYork, "0 2 * * 1-5", "UTC"},
		{"0 3 * * 1", honolulu, "0 17 * * 0", ""},
		{"0 9 * * *", honolulu, "0 23 * * *", ""},
		{"5/15 * * * *", shanghai, "5-50/15 * * * *", "UTC"},
		{"0 9/2 * * *", shanghai, "0 9-23/2 * * *", "UTC"},
		{"*/30 * * * *", shanghai, "0,30 * * * *", "UTC"},
		{"*/15 * * * *", shanghai, "*/15 * * * *", "UTC"},
		{"0 9 * JAN,jul *", shanghai, "0 9 * 1,7 *", "UTC"},
		{"0 9 1 * *", shanghai, "0 9 1 * *", "UTC"},
		// The Daemon's quirks: a day-of-week step is read as its start, a
		// range's step is dropped, a range ending in 7 ends at Sunday (0),
		// "1#2" is read as 1, and a day-of-month step with a day of the
		// week restricted is either-or.
		{"0 9 * * 1/2", shanghai, "0 17 * * 1", ""},
		{"0 9 * * 1-5/2", shanghai, "0 17 * * 1-5", ""},
		{"0 9 * * 0-7", shanghai, "0 17 * * 0", ""},
		{"0 9 * * 1#2", shanghai, "0 17 * * 1", ""},
		{"0 9 */2 * 1", shanghai, "0 9 1-31/2 * 1", "UTC"},
		{"0 9 1-5,7 * *", shanghai, "0 9 1-5,7 * *", "UTC"},
	} {
		s, z, err := FactorySchedule(c.expr, c.offset)
		if err != nil || s != c.wantSchedule || z != c.wantZone {
			t.Errorf("FactorySchedule(%q, %d) = %q, %q, %v; want %q, %q", c.expr, c.offset, s, z, err, c.wantSchedule, c.wantZone)
		}
	}
	for _, expr := range []string{
		"", " Weekly ", "Daily", "@daily", "every day at 9am", "0 0 9 * * *", "0 9 ? * *", "0 9 L * *", "0 9 * * FUNDAY",
		// What the Daemon read but never matched.
		"0 9 * * 5-7", "0 9 * * */2", "0 9 * * 1-7",
	} {
		if s, _, err := FactorySchedule(expr, shanghai); err == nil || !strings.Contains(err.Error(), "never ran in Factory") {
			t.Errorf("FactorySchedule(%q) = %q, %v; want a refusal that says it never ran", expr, s, err)
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

func write(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

func files(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// A Factory automation's folder as droid 1.4.1 keeps it.
func TestCopyFactoryFolderKeepsEverythingButTheDaemonsBookkeeping(t *testing.T) {
	src := filepath.Join(t.TempDir(), "droid")
	write(t, filepath.Join(src, "HEARTBEAT.md"), "---\nname: x\nschedule: daily\n---\n\nx\n", 0o644)
	write(t, filepath.Join(src, "VISUAL.html"), "<html></html>", 0o644)
	write(t, filepath.Join(src, "memory", "state.json"), `{"id":"uuid"}`, 0o644)
	write(t, filepath.Join(src, "memory", "runs.json"), `[]`, 0o644)
	write(t, filepath.Join(src, "memory", "notes.md"), "notes", 0o600)
	write(t, filepath.Join(src, "memory", "deep", "state.json"), "an agent's own", 0o644)
	write(t, filepath.Join(src, "reports", "2026-11-05.md"), "report", 0o644)
	write(t, filepath.Join(src, "skills", "diff", "SKILL.md"), "skill", 0o644)
	write(t, filepath.Join(src, "visuals", "s1.html"), "snapshot", 0o644)
	write(t, filepath.Join(src, ".memory-edit.lock", "owner.json"), `{"pid":1}`, 0o644)
	write(t, filepath.Join(src, ".memory-edit.0123456789abcdef0123456789abcdef.pending", "owner.json"), `{"pid":2}`, 0o644)
	if runtime.GOOS != "windows" {
		if err := os.Symlink("memory/notes.md", filepath.Join(src, "latest-notes")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/etc/hosts", filepath.Join(src, "outside")); err != nil {
			t.Fatal(err)
		}
	}
	dst := filepath.Join(t.TempDir(), "copy")
	if err := CopyFactoryFolder(src, dst); err != nil {
		t.Fatal(err)
	}
	want := []string{"VISUAL.html", "memory/deep/state.json", "memory/notes.md", "reports/2026-11-05.md", "skills/diff/SKILL.md", "visuals/s1.html"}
	if runtime.GOOS != "windows" {
		want = []string{"VISUAL.html", "latest-notes", "memory/deep/state.json", "memory/notes.md", "outside", "reports/2026-11-05.md", "skills/diff/SKILL.md", "visuals/s1.html"}
	}
	if got := files(t, dst); !slices.Equal(got, want) {
		t.Fatalf("copied %v\nwant %v", got, want)
	}
	if info, err := os.Stat(filepath.Join(dst, "memory", "notes.md")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("notes.md mode %v %v", info, err)
	}
	if runtime.GOOS != "windows" {
		for link, target := range map[string]string{"latest-notes": "memory/notes.md", "outside": "/etc/hosts"} {
			if got, err := os.Readlink(filepath.Join(dst, link)); err != nil || got != target {
				t.Errorf("%s links to %q, %v; want %q", link, got, err, target)
			}
		}
	}
}

func TestCopyFactoryFolderFailsOnWhatItCannotRead(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs a file its owner cannot read")
	}
	src := t.TempDir()
	write(t, filepath.Join(src, "memory", "secret.md"), "x", 0o000)
	if err := CopyFactoryFolder(src, filepath.Join(t.TempDir(), "copy")); err == nil || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("copy: %v", err)
	}
}

// droid 1.4.1's memory lock (runtime/file-lock fileLock.ts) and its
// attempts and reclaims are folders with an owner.json, at the top of the
// automation's folder or of one under it.
func TestCopyFactoryFolderLeavesTheDaemonsLockFoldersOut(t *testing.T) {
	src := filepath.Join(t.TempDir(), "droid")
	write(t, filepath.Join(src, "HEARTBEAT.md"), "---\nname: x\nschedule: daily\n---\n\nx\n", 0o644)
	write(t, filepath.Join(src, "memory", "notes.md"), "notes", 0o644)
	for _, lock := range []string{
		".memory-edit.lock", ".memory-edit.0123456789abcdef0123456789abcdef.pending", ".memory-edit.lock.reclaim",
		"memory/.memory-edit.lock", "memory/.memory-edit.fedcba9876543210fedcba9876543210.pending",
	} {
		write(t, filepath.Join(src, filepath.FromSlash(lock), "owner.json"), `{"pid":1}`, 0o644)
	}
	write(t, filepath.Join(src, ".memory-edit.lock.reclaim.tmp"), "", 0o644)
	dst := filepath.Join(t.TempDir(), "copy")
	if err := CopyFactoryFolder(src, dst); err != nil {
		t.Fatal(err)
	}
	if got, want := files(t, dst), []string{"memory/notes.md"}; !slices.Equal(got, want) {
		t.Fatalf("copied %v\nwant %v", got, want)
	}
}
