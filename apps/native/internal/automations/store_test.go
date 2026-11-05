package automations

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestStoreSavesAndReadsAnAutomation(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "automations")}
	created := at(t, "2026-11-05T09:30:00+08:00")
	a, err := s.Save(Automation{
		Name: "Daily report", Schedule: " 0 9  * * 1-5 ", Timezone: "Asia/Shanghai",
		Model: "glm-5.3-flash", ReasoningEffort: "high", Autonomy: "medium",
		WorkingDirectory: "/Users/me/dev/app", Paused: true, CreatedAt: created,
		Prompt: "\nSummarize yesterday's commits.\n\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "daily-report" {
		t.Fatalf("id %q", a.ID)
	}
	b, err := os.ReadFile(filepath.Join(s.Dir, "daily-report", "HEARTBEAT.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := `---
name: Daily report
schedule: 0 9 * * 1-5
timezone: Asia/Shanghai
model: glm-5.3-flash
reasoningEffort: high
autonomy: medium
workingDirectory: /Users/me/dev/app
paused: true
createdAt: 2026-11-05T09:30:00+08:00
---

Summarize yesterday's commits.
`
	if string(b) != want {
		t.Fatalf("HEARTBEAT.md:\n%s\nwant:\n%s", b, want)
	}
	got, err := s.Get("daily-report")
	if err != nil {
		t.Fatal(err)
	}
	a.CreatedAt, got.CreatedAt = a.CreatedAt.UTC(), got.CreatedAt.UTC()
	if got != a {
		t.Fatalf("read %+v\nwant %+v", got, a)
	}

	// A second one by the same name gets its own folder; an edit keeps the id
	// and the creation time.
	other, err := s.Save(Automation{Name: "Daily report", Schedule: "*/30 * * * *", Prompt: "Again", CreatedAt: created})
	if err != nil || other.ID != "daily-report-2" {
		t.Fatalf("%+v %v", other, err)
	}
	edit := got
	edit.Name, edit.CreatedAt, edit.Paused = `Report: "weekly" #1`, time.Time{}, false
	if _, err := s.Save(edit); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get("daily-report")
	if got.Name != edit.Name || !got.CreatedAt.Equal(created) || got.Paused {
		t.Fatalf("after edit %+v", got)
	}
	if err := s.SetPaused("daily-report-2", true); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "daily-report-2" || !list[0].Paused || list[1].ID != "daily-report" || list[0].Schedule != "*/30 * * * *" {
		t.Fatalf("list %+v", list)
	}
	if err := s.Delete("daily-report"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("daily-report"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if _, err := s.Save(Automation{ID: "daily-report", Name: "x", Schedule: "* * * * *", Prompt: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("edit of a deleted one: %v", err)
	}
}

func TestStoreRefusesWhatCannotRun(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	ok := Automation{Name: "n", Schedule: "0 9 * * *", Prompt: "p"}
	for name, change := range map[string]func(*Automation){
		"no name":      func(a *Automation) { a.Name = " " },
		"no prompt":    func(a *Automation) { a.Prompt = "\n" },
		"bad schedule": func(a *Automation) { a.Schedule = "@daily" },
		"bad zone":     func(a *Automation) { a.Timezone = "Mars/Olympus" },
		"bad autonomy": func(a *Automation) { a.Autonomy = "off" },
	} {
		a := ok
		change(&a)
		if _, err := s.Save(a); err == nil {
			t.Errorf("%s: saved", name)
		}
	}
	for _, id := range []string{"", ".", "..", "../x", `a\b`, ".hidden"} {
		if _, err := s.Get(id); err == nil {
			t.Errorf("Get(%q): no error", id)
		}
		if err := s.Delete(id); err == nil {
			t.Errorf("Delete(%q): no error", id)
		}
	}
}

// The shape the Daemon writes in ~/.factory/automations/<id>/HEARTBEAT.md
// (js-yaml): keys Droi does not know, quoted values, nested ones.
func TestStoreReadsAFactoryHeartbeat(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	write := func(id, text string) {
		if err := os.MkdirAll(filepath.Join(s.Dir, id), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(s.Dir, id, "HEARTBEAT.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("droid", "---\nname: Droid二进制逆向\nschedule: 0 1 * * *\nmodel: deepseek-v4.1-flash-1017\ncreationSource: desktop\n---\n\n任务：每天对 Droid CLI 最新的 Linux x86_64 二进制做静态逆向差异分析，把结果写成报告。\n\n## 规则（必须遵守）\n1. 只分析官方二进制。\n")
	write("quoted", "---\r\nid: 0b9a3c52-3f7e-4a3e-9d55-6a1b1f0d9c11\r\nname: 'It''s: weekly'\r\ndescription: |\r\n  several\r\n  lines\r\nschedule: '*/30 * * * *'\r\nreasoningEffort: high\r\nworkingDirectory: \"/Users/me/dev/my app\"\r\ntags:\r\n  - a\r\n  - b\r\ncreatedBy:\r\n  name: Me\r\npaused: true # by Factory\r\n---\r\n\r\nDo it.\r\n")

	a, err := s.Get("droid")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Droid二进制逆向" || a.Schedule != "0 1 * * *" || a.Model != "deepseek-v4.1-flash-1017" || a.Paused || a.Timezone != "" ||
		!strings.HasPrefix(a.Prompt, "任务：每天") || !strings.HasSuffix(a.Prompt, "1. 只分析官方二进制。") {
		t.Fatalf("%+v", a)
	}
	q, err := s.Get("quoted")
	if err != nil {
		t.Fatal(err)
	}
	if q.Name != "It's: weekly" || q.Schedule != "*/30 * * * *" || q.ReasoningEffort != "high" ||
		q.WorkingDirectory != "/Users/me/dev/my app" || !q.Paused || q.Prompt != "Do it." {
		t.Fatalf("%+v", q)
	}
	// The existing helper reads the same file.
	if WorkingDirectory(filepath.Join(s.Dir, "droid")) != "" {
		t.Fatal("WorkingDirectory")
	}
}

func TestStoreKeepsTheLast200Runs(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	a, err := s.Save(Automation{Name: "n", Schedule: "* * * * *", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	start := at(t, "2026-11-05T09:00:00Z")
	for i := range 205 {
		r := Run{RunID: fmt.Sprint("r", i), SessionID: fmt.Sprint("s", i), Trigger: TriggerSchedule, Status: RunRunning, StartedAt: start.Add(time.Duration(i) * time.Minute)}
		if err := s.SaveRun(a.ID, r); err != nil {
			t.Fatal(err)
		}
	}
	end := start.Add(300 * time.Minute)
	if err := s.SaveRun(a.ID, Run{RunID: "r204", SessionID: "s204", Trigger: TriggerSchedule, Status: RunFailed, StartedAt: start.Add(204 * time.Minute), EndedAt: end, Error: "boom"}); err != nil {
		t.Fatal(err)
	}
	runs, err := s.Runs(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != MaxRuns || runs[0].RunID != "r5" || runs[199].RunID != "r204" || runs[199].Status != RunFailed || !runs[199].EndedAt.Equal(end) || runs[199].Error != "boom" {
		t.Fatalf("%d runs, first %+v, last %+v", len(runs), runs[0], runs[len(runs)-1])
	}
	b, _ := os.ReadFile(filepath.Join(s.Dir, a.ID, "runs.jsonl"))
	if line, _, _ := strings.Cut(string(b), "\n"); line != `{"runId":"r5","sessionId":"s5","trigger":"schedule","status":"running","startedAt":"2026-11-05T09:05:00Z"}` {
		t.Fatalf("first line %s", line)
	}
	// A run of an Automation deleted meanwhile does not bring its folder back.
	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRun(a.ID, Run{RunID: "late"}); err == nil {
		t.Fatal("saved a run of a deleted automation")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, a.ID)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("folder: %v", err)
	}
}

func TestStoreState(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "automations")}
	st, err := s.State()
	if err != nil || len(st.LastScheduled) != 0 {
		t.Fatalf("%+v %v", st, err)
	}
	when := at(t, "2026-11-05T09:00:00+08:00")
	st.LastScheduled = map[string]time.Time{"a": when}
	if err := s.SaveState(st); err != nil {
		t.Fatal(err)
	}
	st, err = s.State()
	if err != nil || !st.LastScheduled["a"].Equal(when) {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestStoreWritesLeaveNoTemporaryFiles(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	a, err := s.Save(Automation{Name: "n", Schedule: "* * * * *", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(a); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPaused(a.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRun(a.ID, Run{RunID: "r"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveState(State{LastScheduled: map[string]time.Time{a.ID: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	var names []string
	_ = filepath.WalkDir(s.Dir, func(path string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			names = append(names, d.Name())
		}
		return nil
	})
	if strings.Join(names, " ") != "HEARTBEAT.md runs.jsonl state.json" {
		t.Fatalf("files %v", names)
	}
}

func TestStoreListsAFolderItCannotRead(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	ok, err := s.Save(Automation{Name: "Good", Schedule: "0 9 * * *", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	for id, text := range map[string]string{
		"broken": "no front matter here\n",
		"bom":    "\ufeff--- \r\nname: With a BOM\r\nschedule: 0 9 * * *\r\n---\t\r\n\r\nHi.\r\n",
	} {
		if err := os.MkdirAll(filepath.Join(s.Dir, id), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(s.Dir, id, "HEARTBEAT.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(s.Dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Automation{}
	for _, a := range list {
		byID[a.ID] = a
	}
	if len(list) != 4 || byID[ok.ID].Invalid != "" || byID["bom"].Name != "With a BOM" || byID["bom"].Prompt != "Hi." || byID["bom"].Invalid != "" ||
		byID["broken"].Invalid != "HEARTBEAT.md has no front matter" || byID["empty"].Invalid != "HEARTBEAT.md is missing" {
		t.Fatalf("list %+v", list)
	}
	for _, id := range []string{"broken", "empty"} {
		if err := s.Delete(id); err != nil {
			t.Fatalf("delete %s: %v", id, err)
		}
	}
	if err := s.Delete("broken"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete again: %v", err)
	}
}

func TestStoreRefusesIDsThatAreNotAFolderName(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for _, id := range []string{"a.", "a ", "a:b", "c:", "/abs"} {
		if _, err := s.Get(id); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q): %v", id, err)
		}
	}
}

func TestIDAvoidsWindowsDeviceNames(t *testing.T) {
	for name, want := range map[string]string{"CON": "con-2", "nul": "nul-2", "Com1": "com1-2", "LPT9": "lpt9-2", "com0": "com0", "console": "console"} {
		if got := ID(name, nil); got != want {
			t.Errorf("ID(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestStoreSavesAPromptWithUnixLineEnds(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	a, err := s.Save(Automation{Name: "n", Schedule: "0 9 * * *", Prompt: "one\r\ntwo\r\n"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(s.Dir, a.ID, "HEARTBEAT.md"))
	if strings.Contains(string(b), "\r") || a.Prompt != "one\ntwo" {
		t.Fatalf("%q", b)
	}
}

func TestStoreCutsALongRunError(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	a, err := s.Save(Automation{Name: "n", Schedule: "0 9 * * *", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	long := "x" + strings.Repeat("错", 3000)
	if err := s.SaveRun(a.ID, Run{RunID: "r", Status: RunFailed, Error: long}); err != nil {
		t.Fatal(err)
	}
	runs, err := s.Runs(a.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("%+v %v", runs, err)
	}
	if e := runs[0].Error; len(e) > 4096 || len(e) < 4090 || !utf8.ValidString(e) || !strings.HasPrefix(long, e) || runs[0].AutomationID != a.ID {
		t.Fatalf("error of %d bytes, valid %v, run %q", len(e), utf8.ValidString(e), runs[0].AutomationID)
	}
}
