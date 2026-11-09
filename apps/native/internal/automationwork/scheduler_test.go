package automationwork

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/internal/automations"
)

type fakeStarter struct {
	mu   sync.Mutex
	runs []RunSpec
	fail []error
	// hang makes the next Start wait for its context's end.
	hang    bool
	onStart func(RunSpec)
}

func (f *fakeStarter) Start(ctx context.Context, r RunSpec) (string, error) {
	f.mu.Lock()
	f.runs = append(f.runs, r)
	n := len(f.runs)
	var err error
	if len(f.fail) > 0 {
		err, f.fail = f.fail[0], f.fail[1:]
	}
	hang := f.hang
	f.hang = false
	hook := f.onStart
	f.mu.Unlock()
	if hang {
		<-ctx.Done()
		return "", ctx.Err()
	}
	if err != nil {
		return "", err
	}
	if hook != nil {
		hook(r)
	}
	return fmt.Sprint("session-", n), nil
}

func (f *fakeStarter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.runs)
}

func (f *fakeStarter) last() RunSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs[len(f.runs)-1]
}

type harness struct {
	t     *testing.T
	dir   string
	mu    sync.Mutex
	clock time.Time
	held  int
	// holdCalls counts every Hold, to see none is taken for a failed start.
	holdCalls int
	changed   int
	// hidden is the Config's HiddenTools.
	hidden  func() []string
	starter *fakeStarter
	s       *Scheduler
}

var shanghai = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	return loc
}()

func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// newHarness is a Scheduler over a fresh folder, in Shanghai's time, with
// the clock at now (a time on 2026-11-05 when only a time of day is given).
func newHarness(t *testing.T, now string) *harness {
	h := &harness{t: t, dir: filepath.Join(t.TempDir(), "automations"), starter: &fakeStarter{}}
	h.set(now)
	h.open()
	return h
}

func (h *harness) open() {
	h.s = New(Config{
		Dir:          h.dir,
		Starter:      h.starter,
		StartTimeout: 50 * time.Millisecond,
		Location:     shanghai,
		HiddenTools:  h.hidden,
		Now: func() time.Time {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.clock
		},
		Hold: func() func() {
			h.mu.Lock()
			h.held++
			h.holdCalls++
			h.mu.Unlock()
			return func() {
				h.mu.Lock()
				h.held--
				h.mu.Unlock()
			}
		},
		Changed: func() {
			h.mu.Lock()
			h.changed++
			h.mu.Unlock()
		},
	})
}

func (h *harness) set(when string) {
	h.t.Helper()
	if len(when) <= 8 {
		when = "2026-11-05T" + when
		if len(when) == 16 {
			when += ":00"
		}
		when += "+08:00"
	}
	v := at(h.t, when)
	h.mu.Lock()
	h.clock = v
	h.mu.Unlock()
}

func (h *harness) tick(when string) {
	h.t.Helper()
	h.set(when)
	h.s.Tick(context.Background())
}

func (h *harness) holds() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.held
}

func (h *harness) holdsTaken() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.holdCalls
}

func (h *harness) save(a automations.Automation) automations.Automation {
	h.t.Helper()
	if a.Prompt == "" {
		a.Prompt = "Report."
	}
	saved, err := h.s.Save(a)
	if err != nil {
		h.t.Fatal(err)
	}
	return saved
}

func (h *harness) runs(id string) []automations.Run {
	h.t.Helper()
	runs, err := h.s.Runs(id, 0)
	if err != nil {
		h.t.Fatal(err)
	}
	return runs
}

func (h *harness) item(id string) Item {
	h.t.Helper()
	items, err := h.s.List()
	if err != nil {
		h.t.Fatal(err)
	}
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	h.t.Fatalf("no %s in the list", id)
	return Item{}
}

func TestARunStartsOnTimeAndEnds(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Daily report", Schedule: "0 9 * * *", Prompt: "Report.", Model: "glm-5.3-flash", ReasoningEffort: "high"})

	h.tick("08:59:30")
	if h.starter.count() != 0 {
		t.Fatal("started early")
	}
	h.tick("09:00")
	if h.starter.count() != 1 {
		t.Fatalf("%d starts at 09:00", h.starter.count())
	}
	r := h.starter.last()
	if r.Title != "[Automation] Daily report" || r.Cwd != filepath.Join(h.dir, "daily-report") || r.Trigger != automations.TriggerSchedule ||
		r.ModelID != "glm-5.3-flash" || r.ReasoningEffort != "high" || !r.SkipPermissions || r.AutonomyLevel != "" || r.Prompt != "Report." ||
		r.AutomationID != a.ID || r.RunID == "" {
		t.Fatalf("run %+v", r)
	}
	if want := []string{"CreateAutomation", "ListAutomations", "ReadAutomation", "EditAutomation", "DeleteAutomation", "RunAutomation", "ListAutomationTemplates", "mcp:droi-workstream", "mcp:droi-automations"}; !slices.Equal(r.DisabledTools, want) {
		t.Fatalf("disabled tools %v", r.DisabledTools)
	}
	if len(r.Tags) != 1 || r.Tags[0].Name != TagName || r.Tags[0].Metadata["automationId"] != a.ID ||
		r.Tags[0].Metadata["automationName"] != "Daily report" || r.Tags[0].Metadata["trigger"] != "schedule" || r.Tags[0].Metadata["runId"] != r.RunID {
		t.Fatalf("tags %+v", r.Tags)
	}
	if h.holds() != 1 {
		t.Fatalf("%d holds while the run works", h.holds())
	}
	runs := h.runs(a.ID)
	if len(runs) != 1 || runs[0].RunID != r.RunID || runs[0].SessionID != "session-1" || runs[0].Status != automations.RunRunning ||
		!runs[0].StartedAt.Equal(at(t, "2026-11-05T09:00:00+08:00")) {
		t.Fatalf("records %+v", runs)
	}
	it := h.item(a.ID)
	if !it.Running || it.LastRun == nil || it.LastRun.RunID != r.RunID || !it.NextRunAt.Equal(at(t, "2026-11-06T09:00:00+08:00")) {
		t.Fatalf("item %+v", it)
	}

	h.tick("09:01")
	h.set("09:05")
	h.s.Finished(r.RunID, automations.RunSucceeded, "")
	if h.holds() != 0 {
		t.Fatalf("%d holds after the run", h.holds())
	}
	runs = h.runs(a.ID)
	if len(runs) != 1 || runs[0].Status != automations.RunSucceeded || !runs[0].EndedAt.Equal(at(t, "2026-11-05T09:05:00+08:00")) {
		t.Fatalf("records %+v", runs)
	}
	if h.item(a.ID).Running {
		t.Fatal("still running")
	}
	h.tick("09:06")
	h.tick("2026-11-06T09:00:00+08:00")
	if h.starter.count() != 2 || h.starter.last().Trigger != automations.TriggerSchedule {
		t.Fatalf("%d starts by the next day", h.starter.count())
	}
	if h.changed == 0 {
		t.Fatal("Changed never called")
	}
}

func TestTheRunSpecFollowsTheAutomation(t *testing.T) {
	h := newHarness(t, "08:00")
	h.save(automations.Automation{Name: "In a repo", Schedule: "0 9 * * *", Autonomy: "medium", WorkingDirectory: "/Users/me/dev/app"})
	h.tick("09:00")
	r := h.starter.last()
	if r.Cwd != "/Users/me/dev/app" || r.SkipPermissions || r.AutonomyLevel != "medium" {
		t.Fatalf("run %+v", r)
	}
}

func TestAScheduleKeepsItsTimeZone(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "NY", Schedule: "0 9 * * *", Timezone: "America/New_York"})
	if it := h.item(a.ID); !it.NextRunAt.Equal(at(t, "2026-11-05T14:00:00Z")) {
		t.Fatalf("next %s", it.NextRunAt)
	}
	h.tick("2026-11-05T13:59:00Z")
	h.tick("2026-11-05T14:00:00Z")
	if h.starter.count() != 1 {
		t.Fatalf("%d starts", h.starter.count())
	}
}

func TestOneCatchUpRunAfterAGap(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Hourly", Schedule: "0 * * * *"})
	h.tick("2026-11-07T12:30:00+08:00")
	if h.starter.count() != 1 || h.starter.last().Trigger != automations.TriggerCatchUp {
		t.Fatalf("%d starts, %+v", h.starter.count(), h.starter.runs)
	}
	if runs := h.runs(a.ID); len(runs) != 1 || runs[0].Trigger != automations.TriggerCatchUp {
		t.Fatalf("records %+v", runs)
	}
	h.s.Finished(h.starter.last().RunID, automations.RunSucceeded, "")
	h.tick("2026-11-07T12:31:00+08:00")
	if h.starter.count() != 1 {
		t.Fatal("caught up twice")
	}
	h.tick("2026-11-07T13:00:00+08:00")
	if h.starter.count() != 2 || h.starter.last().Trigger != automations.TriggerSchedule {
		t.Fatalf("%d starts at 13:00", h.starter.count())
	}
}

func TestARunMoreThanTwoMinutesLateIsACatchUp(t *testing.T) {
	for tickAt, want := range map[string]automations.Trigger{
		"09:01:59": automations.TriggerSchedule,
		"09:02:00": automations.TriggerSchedule,
		"09:02:01": automations.TriggerCatchUp,
	} {
		h := newHarness(t, "08:00")
		h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *"})
		h.tick(tickAt)
		if h.starter.count() != 1 || h.starter.last().Trigger != want {
			t.Errorf("at %s: %d starts, %+v; want %s", tickAt, h.starter.count(), h.starter.runs, want)
		}
	}
}

func TestNoSecondRunWhileOneWorks(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Often", Schedule: "* * * * *"})
	h.tick("09:00")
	first := h.starter.last()
	h.tick("09:01")
	h.tick("09:02")
	if h.starter.count() != 1 {
		t.Fatalf("%d starts while the first works", h.starter.count())
	}
	if _, err := h.s.RunNow(context.Background(), a.ID); !errors.Is(err, ErrRunning) {
		t.Fatalf("Run now while it works: %v", err)
	}
	h.s.Finished(first.RunID, automations.RunSucceeded, "")
	h.tick("09:02:30")
	if h.starter.count() != 1 {
		t.Fatal("ran the times it skipped")
	}
	h.tick("09:03")
	if h.starter.count() != 2 || h.starter.last().Trigger != automations.TriggerSchedule {
		t.Fatalf("%d starts at 09:03", h.starter.count())
	}
}

func TestAFailedStartIsTriedAgain(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *"})
	h.starter.fail = []error{fmt.Errorf("connect: %w", ErrUnavailable)}
	h.tick("09:00")
	if h.starter.count() != 1 || h.holdsTaken() != 0 || len(h.runs(a.ID)) != 0 || h.item(a.ID).Running {
		t.Fatalf("after a failed start: %d starts, %d holds, %+v", h.starter.count(), h.holds(), h.runs(a.ID))
	}
	h.tick("09:01")
	if h.starter.count() != 2 || h.starter.last().Trigger != automations.TriggerSchedule || len(h.runs(a.ID)) != 1 {
		t.Fatalf("retry: %d starts, %+v", h.starter.count(), h.runs(a.ID))
	}
	h.tick("09:02")
	if h.starter.count() != 2 {
		t.Fatal("started again after the retry")
	}
}

func TestRunNow(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Paused one", Schedule: "0 9 * * *", Paused: true})
	run, err := h.s.RunNow(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Trigger != automations.TriggerManual || run.Status != automations.RunRunning || run.SessionID != "session-1" ||
		h.starter.last().Tags[0].Metadata["trigger"] != "manual" || h.holds() != 1 {
		t.Fatalf("run %+v, spec %+v", run, h.starter.last())
	}
	if _, err := h.s.RunNow(context.Background(), a.ID); !errors.Is(err, ErrRunning) {
		t.Fatalf("second Run now: %v", err)
	}
	h.s.Finished(run.RunID, automations.RunFailed, "boom")
	if runs := h.runs(a.ID); len(runs) != 1 || runs[0].Status != automations.RunFailed || runs[0].Error != "boom" || h.holds() != 0 {
		t.Fatalf("records %+v, holds %d", runs, h.holds())
	}
	if _, err := h.s.RunNow(context.Background(), "nope"); !errors.Is(err, automations.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	h.starter.fail = []error{ErrUnavailable}
	if _, err := h.s.RunNow(context.Background(), a.ID); !errors.Is(err, ErrUnavailable) || h.holds() != 0 || len(h.runs(a.ID)) != 1 {
		t.Fatalf("unavailable: %v, holds %d, %d records", err, h.holds(), len(h.runs(a.ID)))
	}
	h.starter.fail = []error{errors.New("no such model")}
	failed, err := h.s.RunNow(context.Background(), a.ID)
	if err == nil || failed.Status != automations.RunFailed || failed.Error != "no such model" || h.holds() != 0 {
		t.Fatalf("failed start: %+v %v, holds %d", failed, err, h.holds())
	}
	if runs := h.runs(a.ID); len(runs) != 2 || runs[0].RunID != failed.RunID || runs[0].Status != automations.RunFailed || runs[0].Trigger != automations.TriggerManual {
		t.Fatalf("records %+v", runs)
	}
}

func TestAStartThatFailsForGoodIsRecordedOnce(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *", WorkingDirectory: "/no/such/folder"})
	h.starter.fail = []error{errors.New("no such folder")}
	h.tick("09:00")
	runs := h.runs(a.ID)
	if len(runs) != 1 || runs[0].Status != automations.RunFailed || runs[0].Error != "no such folder" || runs[0].SessionID != "" ||
		!runs[0].EndedAt.Equal(at(t, "2026-11-05T09:00:00+08:00")) || h.holdsTaken() != 0 || h.item(a.ID).Running {
		t.Fatalf("records %+v, holds taken %d", runs, h.holdsTaken())
	}
	h.tick("09:01")
	h.tick("09:02")
	if h.starter.count() != 1 {
		t.Fatalf("tried %d times", h.starter.count())
	}
}

func TestAStartThatTimesOutIsTriedAgain(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *"})
	h.starter.hang = true
	h.tick("09:00")
	if h.starter.count() != 1 || len(h.runs(a.ID)) != 0 || h.item(a.ID).Running || h.holdsTaken() != 0 {
		t.Fatalf("after a timeout: %d starts, %+v", h.starter.count(), h.runs(a.ID))
	}
	h.tick("09:01")
	if h.starter.count() != 2 || len(h.runs(a.ID)) != 1 || !h.item(a.ID).Running {
		t.Fatalf("retry: %d starts, %+v", h.starter.count(), h.runs(a.ID))
	}
}

func TestActiveRunsCanBeEndedFromOutside(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "A", Schedule: "0 9 * * *"})
	b := h.save(automations.Automation{Name: "B", Schedule: "0 9 * * *"})
	h.tick("09:00")
	active := h.s.Active()
	if len(active) != 2 || active[0].SessionID == "" || active[1].SessionID == "" ||
		!slices.ContainsFunc(active, func(r automations.Run) bool { return r.AutomationID == a.ID }) ||
		!slices.ContainsFunc(active, func(r automations.Run) bool { return r.AutomationID == b.ID }) {
		t.Fatalf("active %+v", active)
	}
	for _, r := range active {
		h.s.Finished(r.RunID, automations.RunInterrupted, "The Daemon restarted during the run.")
	}
	if len(h.s.Active()) != 0 || h.holds() != 0 || h.runs(a.ID)[0].Status != automations.RunInterrupted {
		t.Fatalf("after: %+v, holds %d", h.s.Active(), h.holds())
	}
}

func TestAFolderThatCannotBeReadIsListedAndSkipped(t *testing.T) {
	h := newHarness(t, "08:00")
	dir := filepath.Join(h.dir, "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("---\nname: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.tick("09:00")
	if it := h.item("broken"); it.Invalid == "" || !it.NextRunAt.IsZero() {
		t.Fatalf("item %+v", it)
	}
	if err := h.s.Delete("broken"); err != nil {
		t.Fatal(err)
	}
	if h.starter.count() != 0 {
		t.Fatal("started a broken one")
	}
}

func TestAPausedAutomationWaits(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *", Paused: true})
	if !h.item(a.ID).NextRunAt.IsZero() {
		t.Fatal("a paused one has a next run")
	}
	h.tick("09:00")
	h.set("10:30")
	if err := h.s.SetPaused(a.ID, false); err != nil {
		t.Fatal(err)
	}
	h.tick("10:31")
	if h.starter.count() != 0 {
		t.Fatal("ran the time it missed while paused")
	}
	h.tick("2026-11-06T09:00:00+08:00")
	if h.starter.count() != 1 {
		t.Fatal("did not run once resumed")
	}
}

func TestANewOrEditedScheduleStartsFromNow(t *testing.T) {
	h := newHarness(t, "09:30")
	a := h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *"})
	h.tick("09:31")
	if h.starter.count() != 0 {
		t.Fatal("ran a time before it was created")
	}
	h.set("09:40")
	a.Schedule = "35 9 * * *"
	h.save(a)
	h.tick("09:41")
	if h.starter.count() != 0 {
		t.Fatal("ran a time before its schedule was edited")
	}

	// One that Droi did not write, with no creation time: from when Droi
	// first sees it.
	dir := filepath.Join(h.dir, "by-hand")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("---\nname: By hand\nschedule: 0 9 * * *\n---\n\nHi.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.tick("09:42")
	h.tick("09:43")
	if h.starter.count() != 0 {
		t.Fatal("ran a time before it was seen")
	}
	h.tick("2026-11-06T09:00:00+08:00")
	if h.starter.count() != 1 || h.starter.last().AutomationID != "by-hand" {
		t.Fatalf("%d starts the next day", h.starter.count())
	}
}

func TestARunThatWasWorkingWhenDroiQuitIsInterrupted(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *"})
	h.tick("09:00")
	old := h.starter.last()
	h.set("09:10")
	h.open()
	runs := h.runs(a.ID)
	if len(runs) != 1 || runs[0].Status != automations.RunInterrupted || runs[0].Error != "Droi quit during the run." ||
		!runs[0].EndedAt.Equal(at(t, "2026-11-05T09:10:00+08:00")) {
		t.Fatalf("records %+v", runs)
	}
	h.s.Finished(old.RunID, automations.RunSucceeded, "")
	if runs := h.runs(a.ID); runs[0].Status != automations.RunInterrupted {
		t.Fatal("a run from before the restart ended")
	}
	h.tick("09:11")
	if h.starter.count() != 1 {
		t.Fatal("ran 09:00 again after the restart")
	}
}

func TestATurnThatEndsBeforeStartReturns(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Quick", Schedule: "0 9 * * *"})
	h.starter.onStart = func(r RunSpec) { h.s.Finished(r.RunID, automations.RunFailed, "quick") }
	h.tick("09:00")
	runs := h.runs(a.ID)
	if len(runs) != 1 || runs[0].Status != automations.RunFailed || runs[0].Error != "quick" || runs[0].SessionID != "session-1" || h.holds() != 0 || h.item(a.ID).Running {
		t.Fatalf("records %+v, holds %d", runs, h.holds())
	}
}

func TestDeletingAWorkingAutomation(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *"})
	h.tick("09:00")
	old := h.starter.last()
	if err := h.s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h.dir, a.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("folder: %v", err)
	}
	if items, _ := h.s.List(); len(items) != 0 {
		t.Fatalf("list %+v", items)
	}
	if active := h.s.Active(); len(active) != 1 || active[0].RunID != old.RunID {
		t.Fatalf("active %+v", active)
	}

	again := h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *"})
	if again.ID != a.ID || h.item(again.ID).Running {
		t.Fatalf("new one %+v running %v", again, h.item(again.ID).Running)
	}
	run, err := h.s.RunNow(context.Background(), again.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.s.Finished(old.RunID, automations.RunSucceeded, "")
	runs := h.runs(again.ID)
	if len(runs) != 1 || runs[0].RunID != run.RunID || runs[0].Status != automations.RunRunning || !h.item(again.ID).Running || h.holds() != 1 {
		t.Fatalf("records %+v, holds %d", runs, h.holds())
	}
	h.s.Finished(run.RunID, automations.RunSucceeded, "")
	if h.holds() != 0 || len(h.s.Active()) != 0 {
		t.Fatalf("holds %d, active %+v", h.holds(), h.s.Active())
	}
}

func TestRunsAreNewestFirst(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Often", Schedule: "* * * * *"})
	for i, when := range []string{"09:00", "09:01", "09:02"} {
		h.tick(when)
		h.s.Finished(h.starter.last().RunID, automations.RunSucceeded, "")
		if h.starter.count() != i+1 {
			t.Fatalf("%d starts at %s", h.starter.count(), when)
		}
	}
	runs, err := h.s.Runs(a.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].RunID != h.starter.runs[2].RunID || runs[1].RunID != h.starter.runs[1].RunID {
		t.Fatalf("runs %+v", runs)
	}
}

func TestARunHidesWhatTheConfigSays(t *testing.T) {
	h := newHarness(t, "08:00")
	h.hidden = func() []string { return nil }
	h.open()
	a := h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *"})
	if _, err := h.s.RunNow(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	if got := h.starter.last().DisabledTools; got != nil {
		t.Fatalf("a droid that takes no hidden tools got %v", got)
	}
}

func TestAStartThatOpensItsSessionButNotItsTurnFailsTheThirdTime(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *"})
	half := &HalfStartedError{Err: errors.New("write: broken pipe")}
	h.starter.fail = []error{half, ErrUnavailable, half, half, half}
	for _, at := range []string{"09:00", "09:01", "09:02", "09:03"} {
		h.tick(at)
		if runs := h.runs(a.ID); len(runs) != 0 {
			t.Fatalf("at %s: %+v", at, runs)
		}
	}
	h.tick("09:04")
	runs := h.runs(a.ID)
	if len(runs) != 1 || runs[0].Status != automations.RunFailed || runs[0].Error != "write: broken pipe" || h.holdsTaken() != 0 {
		t.Fatalf("records %+v", runs)
	}
	h.tick("09:05")
	if h.starter.count() != 5 || h.item(a.ID).Running {
		t.Fatalf("%d starts", h.starter.count())
	}
	// The next day's run starts afresh.
	h.tick("2026-11-06T09:00:00+08:00")
	if runs := h.runs(a.ID); len(runs) != 2 || runs[0].Status != automations.RunRunning {
		t.Fatalf("the next day %+v", runs)
	}
}

func TestACopyCutShortIsRemovedWhenTheSchedulerStarts(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Daily", Schedule: "0 9 * * *"})
	left := filepath.Join(h.dir, ".adopt-0b9c")
	if err := os.MkdirAll(filepath.Join(left, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.open()
	if _, err := os.Stat(left); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the cut-short copy is still there: %v", err)
	}
	if _, err := h.s.Get(a.ID); err != nil {
		t.Fatal(err)
	}
}
