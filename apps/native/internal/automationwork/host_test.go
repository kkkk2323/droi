package automationwork

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/v2/internal/automations"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/gateway"
)

type fakeFactory struct {
	mu        sync.Mutex
	entries   []protocol.AutomationEntry
	deleteErr string
	deleted   []string
}

func (f *fakeFactory) ListAutomations(context.Context, protocol.ListAutomationsParams) (*protocol.ListAutomationsResult, error) {
	return &protocol.ListAutomationsResult{Automations: f.entries}, nil
}

func (f *fakeFactory) DeleteAutomation(_ context.Context, p protocol.DeleteAutomationParams) (*protocol.DeleteAutomationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, p.AutomationID)
	if f.deleteErr != "" {
		return &protocol.DeleteAutomationResult{Error: f.deleteErr}, nil
	}
	return &protocol.DeleteAutomationResult{Success: true}, nil
}

// factoryFolder is a Factory automation's folder with its HEARTBEAT.md.
func factoryFolder(t *testing.T, heartbeat string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "droid")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte(heartbeat), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func gatewayHost(h *harness, f FactoryDaemon) *GatewayHost {
	return &GatewayHost{Scheduler: h.s, Daemon: func() (FactoryDaemon, error) { return f, nil }, Offset: func() int { return 8 * 3600 }}
}

func TestMovingAFactoryAutomationCopiesItInLocalTimeAndDeletesIt(t *testing.T) {
	h := newHarness(t, "08:00")
	path := factoryFolder(t, "---\nname: Droid二进制逆向\nschedule: 0 1 * * *\nmodel: deepseek-v4.1-flash-1017\nreasoningEffort: high\nworkingDirectory: /Users/me/re\ncreationSource: desktop\n---\n\n任务：逆向分析。\n")
	f := &fakeFactory{entries: []protocol.AutomationEntry{{ID: "droid", Name: "Droid二进制逆向", Schedule: "0 1 * * *", Model: "deepseek-v4.1-flash-1017", Status: "paused", Path: path}}}
	g := gatewayHost(h, f)
	ctx := context.Background()

	listed, err := g.Factory(ctx)
	if err != nil || len(listed) != 1 || listed[0].Prompt != "任务：逆向分析。" || listed[0].WorkingDirectory != "/Users/me/re" || listed[0].ReasoningEffort != "high" || !listed[0].Paused {
		t.Fatalf("factory %+v %v", listed, err)
	}
	moved, err := g.MoveFactory(ctx, "droid")
	if err != nil || moved.DeleteError != "" || !slices.Equal(f.deleted, []string{"droid"}) {
		t.Fatalf("moved %+v %v, deleted %v", moved, err, f.deleted)
	}
	a := moved.Automation
	if a.Name != "Droid二进制逆向" || a.ForkedFrom != "droid" || a.Schedule != "0 9 * * *" || a.Timezone != "" || a.Autonomy != "" || a.Model != "deepseek-v4.1-flash-1017" ||
		a.ReasoningEffort != "high" || a.WorkingDirectory != "/Users/me/re" || !a.Paused || a.Prompt != "任务：逆向分析。" || a.Folder != filepath.Join(h.dir, a.ID) {
		t.Fatalf("copy %+v", a)
	}
	if _, err := g.MoveFactory(ctx, "nope"); !errors.Is(err, gateway.ErrAutomationNotFound) {
		t.Fatalf("an unknown one: %v", err)
	}
}

func TestAMoveTheDaemonDidNotDeleteKeepsTheCopy(t *testing.T) {
	h := newHarness(t, "08:00")
	f := &fakeFactory{deleteErr: "permission denied", entries: []protocol.AutomationEntry{{ID: "weekly", Name: "Weekly", Schedule: "monthly", Prompt: "Sum up.", Path: t.TempDir()}}}
	moved, err := gatewayHost(h, f).MoveFactory(context.Background(), "weekly")
	if err != nil || moved.DeleteError != "permission denied" || moved.Automation.Schedule != "0 9 1 * *" || moved.Automation.Timezone != "UTC" {
		t.Fatalf("moved %+v %v", moved, err)
	}
	if items, _ := h.s.List(); len(items) != 1 {
		t.Fatalf("the copy is gone: %+v", items)
	}
}

func TestAScheduleDroiCannotReadIsNotMoved(t *testing.T) {
	h := newHarness(t, "08:00")
	f := &fakeFactory{entries: []protocol.AutomationEntry{{ID: "odd", Name: "Odd", Schedule: "0 9 L * *", Prompt: "x", Path: t.TempDir()}}}
	_, err := gatewayHost(h, f).MoveFactory(context.Background(), "odd")
	var refused *gateway.RefusedRequest
	if !errors.As(err, &refused) || len(f.deleted) != 0 {
		t.Fatalf("%v, deleted %v", err, f.deleted)
	}
	if items, _ := h.s.List(); len(items) != 0 {
		t.Fatalf("copied %+v", items)
	}
}

func TestMovingThroughTheFakeDaemon(t *testing.T) {
	path := factoryFolder(t, "---\nname: Daily review\nschedule: 0 9 * * *\nworkingDirectory: /Users/dev/acme\n---\n\nReview.\n")
	d := fakedaemon.Start(t, fakedaemon.Scenario{Input: map[string]any{"automations": []map[string]any{{
		"id": "daily-review", "name": "Daily review", "prompt": "Review yesterday's commits.", "schedule": "0 9 * * *", "path": path,
	}}}})
	r := newRunner(t, d)
	connect(t, r)
	h := newHarness(t, "08:00")
	g := &GatewayHost{Scheduler: h.s, Offset: func() int { return 8 * 3600 }, Daemon: func() (FactoryDaemon, error) {
		cl, err := r.Client()
		if err != nil {
			return nil, err
		}
		return cl, nil
	}}
	moved, err := g.MoveFactory(context.Background(), "daily-review")
	if err != nil || moved.DeleteError != "" || moved.Automation.Schedule != "0 17 * * *" || moved.Automation.WorkingDirectory != "/Users/dev/acme" || moved.Automation.Prompt != "Review yesterday's commits." {
		t.Fatalf("moved %+v %v", moved, err)
	}
	left, err := g.Factory(context.Background())
	if err != nil || len(left) != 0 {
		t.Fatalf("still in Factory's: %+v %v", left, err)
	}
	// Moved again, it is gone.
	if _, err := g.MoveFactory(context.Background(), "daily-review"); !errors.Is(err, gateway.ErrAutomationNotFound) {
		t.Fatalf("a second move: %v", err)
	}
}

func TestTheGatewayHostAnswersInTheGatewaysTerms(t *testing.T) {
	h := newHarness(t, "08:00")
	g := gatewayHost(h, &fakeFactory{})
	ctx := context.Background()
	var refused *gateway.RefusedRequest
	if _, err := g.Save(ctx, gateway.Automation{Name: "x", Prompt: "p", Schedule: "@daily"}); !errors.As(err, &refused) {
		t.Fatalf("a bad schedule: %v", err)
	}
	if _, err := g.Save(ctx, gateway.Automation{ID: "gone", Name: "x", Prompt: "p", Schedule: "0 9 * * *"}); !errors.Is(err, gateway.ErrAutomationNotFound) {
		t.Fatalf("an edit of a deleted one: %v", err)
	}
	saved, err := g.Save(ctx, gateway.Automation{Name: "Daily report", Prompt: "Report.", Schedule: "0 9 * * *", Autonomy: "medium"})
	if err != nil || saved.ID != "daily-report" || saved.NextRunAt != "2026-11-05T09:00:00+08:00" || saved.CreatedAt == "" || saved.Folder == "" {
		t.Fatalf("saved %+v %v", saved, err)
	}
	run, err := g.Run(ctx, saved.ID)
	if err != nil || run.Trigger != "manual" || run.Status != "running" {
		t.Fatalf("run %+v %v", run, err)
	}
	if _, err := g.Run(ctx, saved.ID); !errors.Is(err, gateway.ErrAutomationRunning) {
		t.Fatalf("a second run: %v", err)
	}
	h.s.Finished(run.RunID, automations.RunSucceeded, "")
	runs, err := g.Runs(ctx, saved.ID, 50)
	if err != nil || len(runs) != 1 || runs[0].Status != "succeeded" || runs[0].EndedAt == "" {
		t.Fatalf("runs %+v %v", runs, err)
	}
	list, err := g.List(ctx)
	if err != nil || len(list) != 1 || list[0].LastRun == nil || list[0].LastRun.RunID != run.RunID {
		t.Fatalf("list %+v %v", list, err)
	}
	if err := g.SetPaused(ctx, "gone", true); !errors.Is(err, gateway.ErrAutomationNotFound) {
		t.Fatalf("pause a deleted one: %v", err)
	}
	if err := g.Delete(ctx, saved.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Runs(ctx, saved.ID, 50); !errors.Is(err, gateway.ErrAutomationNotFound) {
		t.Fatalf("runs of a deleted one: %v", err)
	}
}

func TestAMoveBringsTheFolderAlongButNotTheDaemonsBookkeeping(t *testing.T) {
	h := newHarness(t, "08:00")
	path := factoryFolder(t, "---\nname: Notes\nschedule: 0 1 * * *\n---\n\nTake notes.\n")
	for name, body := range map[string]string{
		"memory/notes.md": "kept", "memory/state.json": "{}", "memory/runs.json": "[]", "reports/2026-11-04.md": "a report",
		"VISUAL.html": "<p>", ".memory-edit.lock": "", ".memory-edit.0123456789abcdef0123456789abcdef.pending": "",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(path, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f := &fakeFactory{entries: []protocol.AutomationEntry{{ID: "notes", Name: "Notes", Schedule: "0 1 * * *", Path: path}}}
	moved, err := gatewayHost(h, f).MoveFactory(context.Background(), "notes")
	if err != nil || moved.Automation.ForkedFrom != "notes" {
		t.Fatalf("moved %+v %v", moved, err)
	}
	dir := moved.Automation.Folder
	for name, want := range map[string]string{"memory/notes.md": "kept", "reports/2026-11-04.md": "a report", "VISUAL.html": "<p>"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(b) != want {
			t.Errorf("%s: %q %v", name, b, err)
		}
	}
	for _, name := range []string{"memory/state.json", "memory/runs.json", ".memory-edit.lock", ".memory-edit.0123456789abcdef0123456789abcdef.pending"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("copied %s: %v", name, err)
		}
	}
	if hb, err := automations.ReadHeartbeat(dir); err != nil || hb.ForkedFrom != "notes" || hb.Prompt != "Take notes." {
		t.Fatalf("HEARTBEAT.md %+v %v", hb, err)
	}
	entries, _ := os.ReadDir(h.dir)
	for _, e := range entries {
		if e.Name() != moved.Automation.ID && e.Name() != ".lock" && e.Name() != "state.json" {
			t.Errorf("left %s in the Automations folder", e.Name())
		}
	}
}

func TestAMovedAutomationIsNotCopiedTwice(t *testing.T) {
	h := newHarness(t, "08:00")
	f := &fakeFactory{deleteErr: "permission denied", entries: []protocol.AutomationEntry{{ID: "weekly", Name: "Weekly", Schedule: "0 9 * * 1", Prompt: "Sum up.", Path: t.TempDir()}}}
	g := gatewayHost(h, f)
	first, err := g.MoveFactory(context.Background(), "weekly")
	if err != nil || first.DeleteError != "permission denied" {
		t.Fatalf("moved %+v %v", first, err)
	}
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			again, err := g.MoveFactory(context.Background(), "weekly")
			if err != nil || again.Automation.ID != first.Automation.ID || again.DeleteError != "permission denied" {
				t.Errorf("moved again %+v %v", again, err)
			}
		})
	}
	wg.Wait()
	f.mu.Lock()
	f.deleteErr = ""
	f.mu.Unlock()
	last, err := g.MoveFactory(context.Background(), "weekly")
	if err != nil || last.Automation.ID != first.Automation.ID || last.DeleteError != "" || len(f.deleted) != 5 {
		t.Fatalf("moved %+v %v, deleted %v", last, err, f.deleted)
	}
	if items, _ := h.s.List(); len(items) != 1 {
		t.Fatalf("%d copies", len(items))
	}
}

func TestAFactoryAutomationThatPostsIntoASessionDoesNotMove(t *testing.T) {
	h := newHarness(t, "08:00")
	f := &fakeFactory{entries: []protocol.AutomationEntry{{ID: "nudge", Name: "Nudge", Schedule: "0 9 * * *", Prompt: "Go on.", TargetSessionID: "s-1", Path: t.TempDir()}}}
	g := gatewayHost(h, f)
	if listed, err := g.Factory(context.Background()); err != nil || len(listed) != 1 || !listed[0].TargetsSession {
		t.Fatalf("factory %+v %v", listed, err)
	}
	_, err := g.MoveFactory(context.Background(), "nudge")
	var refused *gateway.RefusedRequest
	if !errors.As(err, &refused) || len(f.deleted) != 0 {
		t.Fatalf("%v, deleted %v", err, f.deleted)
	}
	if items, _ := h.s.List(); len(items) != 0 {
		t.Fatalf("copied %+v", items)
	}
}

func TestTheGatewayHostRefusesWhatItCannotRead(t *testing.T) {
	h := newHarness(t, "08:00")
	g := gatewayHost(h, &fakeFactory{})
	ctx := context.Background()
	var refused *gateway.RefusedRequest
	for _, id := range []string{"../x", ".", "a/b"} {
		if _, err := g.Save(ctx, gateway.Automation{ID: id, Name: "x", Prompt: "p", Schedule: "0 9 * * *"}); !errors.As(err, &refused) {
			t.Fatalf("save %q: %v", id, err)
		}
		if _, err := g.Runs(ctx, id, 10); !errors.As(err, &refused) {
			t.Fatalf("runs of %q: %v", id, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(h.dir, "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dir, "broken", "HEARTBEAT.md"), []byte("not a heartbeat"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Runs(ctx, "broken", 10); !errors.As(err, &refused) || !strings.Contains(refused.Reason, "cannot be read") {
		t.Fatalf("runs: %v", err)
	}
	if _, err := g.Run(ctx, "broken"); !errors.As(err, &refused) {
		t.Fatalf("run: %v", err)
	}
	if err := g.SetPaused(ctx, "broken", true); !errors.As(err, &refused) {
		t.Fatalf("pause: %v", err)
	}
	saved, err := g.Save(ctx, gateway.Automation{ID: "broken", Name: "Fixed", Prompt: "p", Schedule: "0 9 * * *"})
	if err != nil || saved.ID != "broken" || saved.Invalid != "" || saved.Name != "Fixed" {
		t.Fatalf("saving over it: %+v %v", saved, err)
	}
	if err := g.Delete(ctx, "broken"); err != nil {
		t.Fatal(err)
	}
}

func TestTheHostAnswersEachAutomationsZone(t *testing.T) {
	h := newHarness(t, "08:00")
	g := gatewayHost(h, &fakeFactory{})
	g.LocalZone = func() string { return "Asia/Shanghai" }
	for _, a := range []automations.Automation{
		{Name: "Local", Prompt: "p", Schedule: "0 9 * * *"},
		{Name: "Monthly", Prompt: "p", Schedule: "0 9 1 * *", Timezone: "UTC"},
	} {
		if _, err := h.s.Save(a); err != nil {
			t.Fatal(err)
		}
	}
	list, err := g.List(context.Background())
	if err != nil || len(list) != 2 {
		t.Fatalf("%+v %v", list, err)
	}
	zones := map[string]string{}
	for _, a := range list {
		zones[a.Name] = a.Zone
	}
	if zones["Local"] != "Asia/Shanghai" || zones["Monthly"] != "UTC" {
		t.Fatalf("zones %v", zones)
	}
	if z, err := g.Zone(context.Background()); err != nil || z != "Asia/Shanghai" {
		t.Fatalf("zone %q %v", z, err)
	}
	// A save does not keep what the Host answers.
	saved, err := g.Save(context.Background(), gateway.Automation{Name: "Zoned", Prompt: "p", Schedule: "0 9 * * *", Zone: "Europe/Paris"})
	if err != nil || saved.Timezone != "" || saved.Zone != "Asia/Shanghai" {
		t.Fatalf("saved %+v %v", saved, err)
	}
}

func TestFactoryAutomationsComeWithTheScheduleDroiWouldKeep(t *testing.T) {
	h := newHarness(t, "08:00")
	f := &fakeFactory{entries: []protocol.AutomationEntry{
		{ID: "daily", Name: "Daily", Schedule: "0 1 * * *", Path: t.TempDir()},
		{ID: "monthly", Name: "Monthly", Schedule: "monthly", Path: t.TempDir()},
		{ID: "odd", Name: "Odd", Schedule: "0 9 L * *", Path: t.TempDir()},
	}}
	listed, err := gatewayHost(h, f).Factory(context.Background())
	if err != nil || len(listed) != 3 {
		t.Fatalf("%+v %v", listed, err)
	}
	for i, want := range [][2]string{{"0 9 * * *", ""}, {"0 9 1 * *", "UTC"}, {"", ""}} {
		if got := [2]string{listed[i].LocalSchedule, listed[i].LocalTimezone}; got != want {
			t.Errorf("%s: %q, want %q", listed[i].ID, got, want)
		}
	}
	// One that cannot move says why, as its move would.
	if listed[0].NotMovable != "" || listed[1].NotMovable != "" || !strings.HasPrefix(listed[2].NotMovable, `the schedule "0 9 L * *" never ran in Factory: `) {
		t.Fatalf("not movable: %q %q %q", listed[0].NotMovable, listed[1].NotMovable, listed[2].NotMovable)
	}
	_, err = gatewayHost(h, f).MoveFactory(context.Background(), "odd")
	var refused *gateway.RefusedRequest
	if !errors.As(err, &refused) || refused.Reason != listed[2].NotMovable {
		t.Fatalf("the move's refusal %v, the list's %q", err, listed[2].NotMovable)
	}
}

func TestToolsWithoutTheAutomationsSayAnotherDroiRunsThem(t *testing.T) {
	srv := ToolServerOf(func() *Scheduler { return nil }, func() string { return "Asia/Shanghai" })
	if len(srv.Tools()) == 0 {
		t.Fatal("no tools listed")
	}
	_, err := srv.Call(context.Background(), ToolList, map[string]any{}, "s1")
	if err == nil || err.Error() != "Another Droi on this computer runs the automations." {
		t.Fatalf("call: %v", err)
	}
}

func TestAMoveThatCannotCopyTheFolderSaysSoPlainly(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs a file its owner cannot read")
	}
	h := newHarness(t, "08:00")
	path := factoryFolder(t, "---\nname: Notes\nschedule: 0 1 * * *\n---\n\nTake notes.\n")
	if err := os.WriteFile(filepath.Join(path, "secret.md"), []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	f := &fakeFactory{entries: []protocol.AutomationEntry{{ID: "notes", Name: "Notes", Schedule: "0 1 * * *", Path: path}}}
	var logged []string
	g := gatewayHost(h, f)
	g.Log = func(s string) { logged = append(logged, s) }
	_, err := g.MoveFactory(context.Background(), "notes")
	if err == nil || err.Error() != "Droi could not copy the automation's folder." || len(f.deleted) != 0 {
		t.Fatalf("%v, deleted %v", err, f.deleted)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "secret.md") {
		t.Fatalf("logged %q", logged)
	}
	if entries, _ := os.ReadDir(h.dir); len(entries) > 1 || len(entries) == 1 && entries[0].Name() != "state.json" {
		t.Fatalf("left %v", entries)
	}
}

// A new Factory automation may take the folder name of one deleted: a move
// tells them apart by uuid.
func TestAMoveTellsFactoryAutomationsByTheirUUID(t *testing.T) {
	h := newHarness(t, "08:00")
	f := &fakeFactory{entries: []protocol.AutomationEntry{{ID: "daily-report", Uuid: "u-1", Name: "Daily report", Schedule: "0 1 * * *", Prompt: "Report.", Path: t.TempDir()}}}
	g := gatewayHost(h, f)
	first, err := g.MoveFactory(context.Background(), "daily-report")
	if err != nil || first.Automation.ID != "daily-report" || first.Automation.ForkedFrom != "u-1" || first.Automation.ForkedFromName != "daily-report" {
		t.Fatalf("first %+v %v", first, err)
	}
	f.entries = []protocol.AutomationEntry{{ID: "daily-report", Uuid: "u-2", Name: "Daily report", Schedule: "0 2 * * *", Prompt: "Report again.", Path: t.TempDir()}}
	second, err := g.MoveFactory(context.Background(), "daily-report")
	if err != nil || second.Automation.ID != "daily-report-2" || second.Automation.ForkedFrom != "u-2" || second.Automation.Prompt != "Report again." {
		t.Fatalf("second %+v %v", second, err)
	}
	if !slices.Equal(f.deleted, []string{"daily-report", "daily-report"}) {
		t.Fatalf("deleted %v", f.deleted)
	}
	// The second moved again (its delete failed, say) is not copied again.
	again, err := g.MoveFactory(context.Background(), "daily-report")
	if err != nil || again.Automation.ID != "daily-report-2" || len(f.deleted) != 3 {
		t.Fatalf("again %+v %v, deleted %v", again, err, f.deleted)
	}
	if items, _ := h.s.List(); len(items) != 2 {
		t.Fatalf("%d automations", len(items))
	}
}
