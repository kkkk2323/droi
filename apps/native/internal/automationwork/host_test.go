package automationwork

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/automations"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
)

type fakeFactory struct {
	entries   []protocol.AutomationEntry
	deleteErr string
	deleted   []string
}

func (f *fakeFactory) ListAutomations(context.Context, protocol.ListAutomationsParams) (*protocol.ListAutomationsResult, error) {
	return &protocol.ListAutomationsResult{Automations: f.entries}, nil
}

func (f *fakeFactory) DeleteAutomation(_ context.Context, p protocol.DeleteAutomationParams) (*protocol.DeleteAutomationResult, error) {
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
	if a.Name != "Droid二进制逆向" || a.Schedule != "0 9 * * *" || a.Timezone != "" || a.Autonomy != "" || a.Model != "deepseek-v4.1-flash-1017" ||
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
	if _, err := g.Run(ctx, saved.ID); !errors.As(err, &refused) || refused.Reason != "A run of “Daily report” is already working." {
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
