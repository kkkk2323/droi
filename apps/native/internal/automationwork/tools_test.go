package automationwork

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/automations"
)

func call(t *testing.T, h *harness, tool string, args map[string]any) (string, error) {
	t.Helper()
	srv := ToolServer(h.s, func() string { return "Asia/Shanghai" })
	res, err := srv.Call(context.Background(), tool, args, "session-1")
	if err != nil {
		return "", err
	}
	if res.IsError || len(res.Content) != 1 {
		t.Fatalf("%s: %+v", tool, res)
	}
	return res.Content[0].Text, nil
}

func TestTheToolsNameTheZoneAndRunAtTheirAutonomy(t *testing.T) {
	h := newHarness(t, "08:00")
	srv := ToolServer(h.s, func() string { return "Asia/Shanghai" })
	var list []struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		InputSchema map[string]any `json:"inputSchema"`
	}
	if err := json.Unmarshal(srv.Tools(), &list); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range list {
		names = append(names, tool.Name)
		if tool.Name == ToolCreate {
			schedule := tool.InputSchema["properties"].(map[string]any)["schedule"].(map[string]any)["description"].(string)
			if !strings.Contains(schedule, "Asia/Shanghai") {
				t.Fatalf("the schedule's description %q names no zone", schedule)
			}
			if req := tool.InputSchema["required"]; len(req.([]any)) != 3 {
				t.Fatalf("create requires %v", req)
			}
		}
	}
	if strings.Join(names, " ") != "list_automations create_automation update_automation pause_automation run_automation" {
		t.Fatalf("tools %v", names)
	}
	if srv.Autonomy != "medium" || srv.ToolAutonomy[ToolList] != "low" || len(srv.ToolAutonomy) != 1 {
		t.Fatalf("autonomy %q %v", srv.Autonomy, srv.ToolAutonomy)
	}
}

func TestDroidCreatesAndChangesAnAutomationWithTheTools(t *testing.T) {
	h := newHarness(t, "08:00")
	if _, err := call(t, h, ToolCreate, map[string]any{"name": "Daily report", "prompt": "Report.", "schedule": "every morning"}); err == nil ||
		!strings.Contains(err.Error(), `The schedule "every morning" is not understood`) || !strings.Contains(err.Error(), "Asia/Shanghai") {
		t.Fatalf("a schedule in words: %v", err)
	}
	if _, err := call(t, h, ToolCreate, map[string]any{"name": "Daily report", "prompt": "Report.", "schedule": "0 9 * * *", "autonomy": ""}); err == nil {
		t.Fatal("a tool made an automation that runs without asking")
	}
	text, err := call(t, h, ToolCreate, map[string]any{"name": "Daily report", "prompt": "Report.", "schedule": "0 9 * * 1-5", "workingDirectory": "/Users/me/app"})
	if err != nil || !strings.Contains(text, "id daily-report") || !strings.Contains(text, "Next run: 2026-11-05T09:00:00+08:00") {
		t.Fatalf("create: %q %v", text, err)
	}
	it, err := h.s.Get("daily-report")
	if err != nil || it.Autonomy != "high" || it.WorkingDirectory != "/Users/me/app" || it.Schedule != "0 9 * * 1-5" || it.Timezone != "" {
		t.Fatalf("created %+v %v", it, err)
	}

	if _, err := call(t, h, ToolUpdate, map[string]any{"id": "daily-report", "schedule": "30 8 * * *", "autonomy": "low"}); err != nil {
		t.Fatal(err)
	}
	it, _ = h.s.Get("daily-report")
	if it.Schedule != "30 8 * * *" || it.Autonomy != "low" || it.Prompt != "Report." || it.Name != "Daily report" {
		t.Fatalf("updated %+v", it)
	}
	if _, err := call(t, h, ToolUpdate, map[string]any{"id": "nope", "name": "x"}); err == nil || !strings.Contains(err.Error(), "list_automations") {
		t.Fatalf("update an unknown one: %v", err)
	}

	if text, err := call(t, h, ToolPause, map[string]any{"id": "daily-report", "paused": true}); err != nil || text != "Paused daily-report." {
		t.Fatalf("pause: %q %v", text, err)
	}
	text, err = call(t, h, ToolList, map[string]any{})
	if err != nil || !strings.Contains(text, `"id": "daily-report"`) || !strings.Contains(text, `"paused": true`) || !strings.Contains(text, `"autonomy": "low"`) {
		t.Fatalf("list: %q %v", text, err)
	}

	text, err = call(t, h, ToolRun, map[string]any{"id": "daily-report"})
	if err != nil || !strings.Contains(text, "in session session-1") || h.starter.last().AutonomyLevel != "low" || h.starter.last().SkipPermissions {
		t.Fatalf("run: %q %v, spec %+v", text, err, h.starter.last())
	}
	if _, err := call(t, h, ToolRun, map[string]any{"id": "daily-report"}); err == nil || !strings.Contains(err.Error(), "already working") {
		t.Fatalf("a second run: %v", err)
	}
	if _, err := ToolServer(h.s, func() string { return "UTC" }).Call(context.Background(), "delete_automation", map[string]any{"id": "daily-report"}, ""); err == nil {
		t.Fatal("a tool deleted an automation")
	}
}

func TestAToolCannotChangeWhatAnAutomationThatRunsWithoutAskingDoes(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Nightly", Schedule: "0 2 * * *", Prompt: "Clean up.", Timezone: "UTC"})
	const refusal = "This automation runs without asking for approval, so its prompt, folder and model can only be changed in Droi's Automations page."
	for _, args := range []map[string]any{{"prompt": "Delete everything."}, {"workingDirectory": "/"}, {"model": "other"}, {"name": "x", "prompt": "Other."}} {
		args["id"] = a.ID
		if _, err := call(t, h, ToolUpdate, args); err == nil || err.Error() != refusal {
			t.Fatalf("%v: %v", args, err)
		}
	}
	it, _ := h.s.Get(a.ID)
	if it.Prompt != "Clean up." || it.WorkingDirectory != "" || it.Model != "" || it.Name != "Nightly" {
		t.Fatalf("changed %+v", it)
	}
	// What it does is given back unchanged; its name and schedule change,
	// the schedule then in this computer's zone.
	if _, err := call(t, h, ToolUpdate, map[string]any{"id": a.ID, "prompt": "Clean up.", "name": "Nightly clean", "schedule": "0 3 * * *"}); err != nil {
		t.Fatal(err)
	}
	it, _ = h.s.Get(a.ID)
	if it.Name != "Nightly clean" || it.Schedule != "0 3 * * *" || it.Timezone != "" || it.Autonomy != "" {
		t.Fatalf("updated %+v", it)
	}
	if _, err := call(t, h, ToolPause, map[string]any{"id": a.ID, "paused": true}); err != nil {
		t.Fatal(err)
	}
}

func TestAToolScheduleIsInThisComputersZone(t *testing.T) {
	h := newHarness(t, "08:00")
	a := h.save(automations.Automation{Name: "Report", Schedule: "0 9 * * *", Timezone: "UTC", Autonomy: "high"})
	if _, err := call(t, h, ToolUpdate, map[string]any{"id": a.ID, "name": "Morning report"}); err != nil {
		t.Fatal(err)
	}
	if it, _ := h.s.Get(a.ID); it.Timezone != "UTC" {
		t.Fatalf("a rename dropped the zone: %+v", it)
	}
	if _, err := call(t, h, ToolUpdate, map[string]any{"id": a.ID, "schedule": "0 10 * * *"}); err != nil {
		t.Fatal(err)
	}
	if it, _ := h.s.Get(a.ID); it.Schedule != "0 10 * * *" || it.Timezone != "" || it.NextRunAt.Format(time.RFC3339) != "2026-11-05T10:00:00+08:00" {
		t.Fatalf("rescheduled %+v", it)
	}
}
