package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/internal/automations"
)

func automationsScenario() fakedaemon.Scenario {
	started := time.Now().Add(-26 * time.Hour).UTC()
	return fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{
			{Title: "Fix the login race", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "Fix it"}}},
			{Title: "[Automation] Daily review", Cwd: "/Users/dev/.factory/automations/daily-review",
				Messages: []fakedaemon.Message{{Role: "user", Text: "Review yesterday's commits."}, {Role: "assistant", Text: "Nothing risky landed."}},
				Extra: map[string]any{"sessionId": "run-1", "tags": []map[string]any{{"name": "automation", "metadata": map[string]string{
					"automationId": "daily-review", "automationName": "Daily review", "triggerSource": "scheduled", "type": "run",
				}}}}},
		},
		Input: map[string]any{"automations": []map[string]any{{
			"id": "daily-review", "name": "Daily review", "prompt": "Review yesterday's commits.", "schedule": "0 9 * * *",
			"lastRunAt": started.Format(time.RFC3339), "lastRunStatus": "success", "nextRunAt": time.Now().Add(5 * time.Hour).UTC().Format(time.RFC3339),
			"runs": []map[string]any{{"sessionId": "run-1", "status": "success", "startedAt": started.Format(time.RFC3339)}},
		}}},
	}
}

// A run at work shows in the sidebar, under Automations, until it ends.
func TestARunningAutomationShowsInTheSidebar(t *testing.T) {
	h := newHarness(t, automationsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	if h.hasText("Running automations") {
		t.Fatal("a section before any run works")
	}
	if err := h.d.Notify("run-1", map[string]any{"type": "droid_working_state_changed", "newState": "thinking"}); err != nil {
		t.Fatal(err)
	}
	h.until("the running run", func() bool { return h.hasText("Running automations") && h.hasText("[Automation] Daily review") })
	h.click("[Automation] Daily review")
	if r := h.a.Route(); r.SessionID != "run-1" {
		t.Fatalf("route %+v", r)
	}
	if err := h.d.Notify("run-1", map[string]any{"type": "droid_working_state_changed", "newState": "idle"}); err != nil {
		t.Fatal(err)
	}
	h.until("the section gone", func() bool { return !h.hasText("Running automations") })
}

func offsetNow() int {
	_, off := time.Now().Zone()
	return off
}

// A run's Session leaves the sidebar for the Automations page, which
// lists the Automation with its schedule in local time and its runs, and
// runs, pauses and opens them through the Daemon.
func TestTheAutomationsPageRunsPausesAndOpensAnAutomation(t *testing.T) {
	h := newHarnessWith(t, automationsScenario(), "", func(cfg *Config) { cfg.FactoryAppRunning = func() bool { return true } })
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	if h.hasText("[Automation] Daily review") {
		t.Fatal("the run is in the sidebar")
	}
	h.click("Automations")
	h.until("the list", func() bool { return h.hasText("Daily review") })
	if want := automations.Describe("0 9 * * *", offsetNow()); !h.hasText(want) {
		t.Errorf("no %q in %q", want, h.tt.Texts())
	}
	h.until("the Factory App notice", func() bool { return h.hasText("The Factory App is running.") })

	h.click("Daily review")
	h.until("the run", func() bool { return h.hasText("Succeeded") })
	if !h.hasText("Review yesterday's commits.") {
		t.Fatal("no prompt")
	}
	h.click("Run now")
	req, err := h.d.WaitForRequest("daemon.dispatch_automation_run", 1)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		AutomationID string `json:"automationId"`
	}
	_ = json.Unmarshal(req.Params, &p)
	if p.AutomationID != "daily-review" {
		t.Fatalf("dispatched %s", req.Params)
	}
	h.until("the new run", func() bool { return h.hasText("Running") && h.hasText("Started a run.") })

	h.click("More actions")
	h.click("Pause")
	h.until("the paused status", func() bool { return h.hasText("Paused.") && h.hasText("Paused") })
	h.click("More actions")
	h.click("Resume")
	h.until("the active status", func() bool { return h.hasText("Resumed.") })

	h.click("Edit automation")
	h.until("the editor", func() bool { _, ok := h.tt.Find("Automation prompt"); return ok })
	h.click("Automation name")
	h.tt.Type(" 2")
	h.frame()
	h.click("Save changes")
	req, err = h.d.WaitForRequest("daemon.update_automation", 1)
	if err != nil {
		t.Fatal(err)
	}
	var u struct{ Name, Schedule string }
	_ = json.Unmarshal(req.Params, &u)
	if u.Name != "Daily review 2" || u.Schedule != "0 9 * * *" {
		t.Fatalf("updated %s", req.Params)
	}
	h.until("the saved automation", func() bool {
		return h.a.Route().Tab == "" && h.hasText("Saved.") && h.hasText("Daily review 2")
	})

	name, ok := h.findPrefix("Open run from yesterday")
	if !ok {
		t.Fatalf("no row for the old run in %q", h.tt.Texts())
	}
	h.click(name)
	h.until("the run's Session", func() bool { return h.hasText("Nothing risky landed.") })
	if r := h.a.Route(); r.Name != "session" || r.SessionID != "run-1" {
		t.Fatalf("went to %+v", r)
	}
}

// A new Automation goes to the Daemon with an id from its name and the
// schedule turned into UTC, then opens.
func TestCreatingAnAutomation(t *testing.T) {
	h := newHarness(t, automationsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("Automations")
	h.until("the list", func() bool { return h.hasText("Daily review") })
	h.click("New automation")
	h.until("the form", func() bool { _, ok := h.tt.Find("Automation name"); return ok })
	h.click("Automation name")
	h.tt.Type("Daily review")
	h.frame()
	h.click("Create automation")
	h.until("the prompt error", func() bool { return h.hasText("cannot be empty") })
	h.click("Automation prompt")
	h.tt.Type("List open pull requests.")
	h.frame()
	h.click("Create automation")
	req, err := h.d.WaitForRequest("daemon.create_automation", 1)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		ID, Name, Instructions, Schedule string
		SkipFirstRun                     bool `json:"skipFirstRun"`
	}
	_ = json.Unmarshal(req.Params, &p)
	want := automations.Schedule{Repeat: automations.Daily, Hour: 9}.Expression(offsetNow())
	if p.ID != "daily-review-2" || p.Name != "Daily review" || p.Instructions != "List open pull requests." || p.Schedule != want || !p.SkipFirstRun {
		t.Fatalf("created %s, want schedule %q", req.Params, want)
	}
	h.until("the new automation's page", func() bool {
		return h.a.Route().Automation == "daily-review-2" && h.hasText("Created.") && h.hasText("No runs yet")
	})
}
