package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/v2/internal/automations"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/sessions"
)

// hostZone is the time zone of the fake Host's computer.
const hostZone = "Asia/Shanghai"

// fakeAutomations is the Host's side of the Automation endpoints, in memory,
// with the calls the window made.
type fakeAutomations struct {
	mu      sync.Mutex
	list    []gateway.Automation
	runs    map[string][]gateway.AutomationRun
	factory []gateway.FactoryAutomation

	saved   []gateway.Automation
	paused  []string
	ran     []string
	deleted []string
	moved   []string

	// listErr answers a List, factoryErr a Factory, runErr a Run; moveErr
	// and deleteError a MoveFactory.
	listErr     error
	factoryErr  error
	runErr      error
	moveErr     error
	deleteError string
	// block, when set, holds each Run and MoveFactory until it is sent to.
	block chan struct{}
	// reads counts the reads of runs.
	reads int
}

func (f *fakeAutomations) wait() {
	f.mu.Lock()
	block := f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
}

func (f *fakeAutomations) runsRead() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

var _ gateway.Automations = (*fakeAutomations)(nil)

func (f *fakeAutomations) find(id string) int {
	return slices.IndexFunc(f.list, func(a gateway.Automation) bool { return a.ID == id })
}

// zoned is a as the Host answers it, with the zone its schedule is read in.
func zoned(a gateway.Automation) gateway.Automation {
	a.Zone = a.Timezone
	if a.Zone == "" {
		a.Zone = hostZone
	}
	return a
}

func (f *fakeAutomations) List(context.Context) ([]gateway.Automation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]gateway.Automation, len(f.list))
	for i, a := range f.list {
		out[i] = zoned(a)
	}
	return out, nil
}

func (f *fakeAutomations) Runs(_ context.Context, id string, _ int) ([]gateway.AutomationRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.find(id) < 0 {
		return nil, gateway.ErrAutomationNotFound
	}
	return slices.Clone(f.runs[id]), nil
}

func (f *fakeAutomations) Save(_ context.Context, a gateway.Automation) (gateway.Automation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, a)
	a.Zone = ""
	if a.ID == "" {
		taken := map[string]bool{}
		for _, x := range f.list {
			taken[x.ID] = true
		}
		a.ID = automations.ID(a.Name, taken)
		f.list = append(f.list, a)
		return zoned(a), nil
	}
	i := f.find(a.ID)
	if i < 0 {
		return gateway.Automation{}, gateway.ErrAutomationNotFound
	}
	a.NextRunAt, a.LastRun, a.ForkedFrom, a.ForkedFromName = f.list[i].NextRunAt, f.list[i].LastRun, f.list[i].ForkedFrom, f.list[i].ForkedFromName
	f.list[i] = a
	return zoned(a), nil
}

func (f *fakeAutomations) SetPaused(_ context.Context, id string, paused bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.find(id)
	if i < 0 {
		return gateway.ErrAutomationNotFound
	}
	f.list[i].Paused = paused
	what := "resume"
	if paused {
		what = "pause"
	}
	f.paused = append(f.paused, what+" "+id)
	return nil
}

func (f *fakeAutomations) Run(_ context.Context, id string) (gateway.AutomationRun, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ran = append(f.ran, id)
	if f.runErr != nil {
		return gateway.AutomationRun{}, f.runErr
	}
	r := gateway.AutomationRun{RunID: "manual-" + id, Trigger: "manual", Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339)}
	f.runs[id] = append([]gateway.AutomationRun{r}, f.runs[id]...)
	return r, nil
}

func (f *fakeAutomations) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.find(id)
	if i < 0 {
		return gateway.ErrAutomationNotFound
	}
	f.list = slices.Delete(f.list, i, i+1)
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeAutomations) Factory(context.Context) ([]gateway.FactoryAutomation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.factoryErr != nil {
		return nil, f.factoryErr
	}
	return slices.Clone(f.factory), nil
}

func (f *fakeAutomations) MoveFactory(_ context.Context, id string) (gateway.AutomationMoved, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moved = append(f.moved, id)
	if f.moveErr != nil {
		return gateway.AutomationMoved{}, f.moveErr
	}
	i := slices.IndexFunc(f.factory, func(x gateway.FactoryAutomation) bool { return x.ID == id })
	if i < 0 {
		return gateway.AutomationMoved{}, gateway.ErrAutomationNotFound
	}
	fa := f.factory[i]
	a := gateway.Automation{ID: fa.ID, Name: fa.Name, Prompt: fa.Prompt, Schedule: fa.LocalSchedule, Timezone: fa.LocalTimezone, Paused: fa.Paused, ForkedFrom: fa.ID}
	f.list = append(f.list, a)
	if f.deleteError == "" {
		f.factory = slices.Delete(f.factory, i, i+1)
	}
	return gateway.AutomationMoved{Automation: zoned(a), DeleteError: f.deleteError}, nil
}

func (f *fakeAutomations) Zone(context.Context) (string, error) { return hostZone, nil }

func (f *fakeAutomations) calls(of *[]string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(*of)
}

func (f *fakeAutomations) lastSaved() (gateway.Automation, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.saved) == 0 {
		return gateway.Automation{}, false
	}
	return f.saved[len(f.saved)-1], true
}

func (f *fakeAutomations) add(a gateway.Automation) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.list = append(f.list, a)
}

// yesterdayNoon is yesterday at noon: 26 hours ago, in the first two hours
// of a day, is the day before yesterday, which the runs list names by its
// date.
func yesterdayNoon() string {
	y := time.Now().AddDate(0, 0, -1)
	return time.Date(y.Year(), y.Month(), y.Day(), 12, 0, 0, 0, time.Local).UTC().Format(time.RFC3339)
}

// nextNineInHostZone is the next 09:00 in the Host's zone: Daily review's
// next run.
func nextNineInHostZone(t *testing.T) string {
	loc, err := time.LoadLocation(hostZone)
	if err != nil {
		t.Skip("no time zone data")
	}
	now := time.Now().In(loc)
	next := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, loc)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next.UTC().Format(time.RFC3339)
}

// newFakeAutomations is the Host's Daily review, with a run whose Session
// the Fake Daemon has, and an interrupted catch-up run before it.
func newFakeAutomations(t *testing.T) *fakeAutomations {
	started := yesterdayNoon()
	last := gateway.AutomationRun{RunID: "r2", SessionID: "run-1", Trigger: "schedule", Status: "succeeded", StartedAt: started, EndedAt: started}
	return &fakeAutomations{
		list: []gateway.Automation{{
			ID: "daily-review", Name: "Daily review", Prompt: "Review yesterday's commits.", Schedule: "0 9 * * *",
			NextRunAt: nextNineInHostZone(t), LastRun: &last, Folder: "/data/automations/daily-review",
		}},
		runs: map[string][]gateway.AutomationRun{"daily-review": {
			last,
			{RunID: "r1", Trigger: "catch-up", Status: "interrupted", Error: "Droi quit during the run.",
				StartedAt: time.Now().AddDate(0, 0, -3).UTC().Format(time.RFC3339)},
		}},
	}
}

// The Fake Daemon's Sessions: one of the user's, a run of Daily review, and
// a run the Daemon made of the Factory automation Nightly lint.
func automationsScenario() fakedaemon.Scenario {
	return fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{
			{Title: "Fix the login race", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "Fix it"}}},
			{Title: "[Automation] Daily review", Cwd: "/data/automations/daily-review",
				Messages: []fakedaemon.Message{{Role: "user", Text: "Review yesterday's commits."}, {Role: "assistant", Text: "Nothing risky landed."}},
				Extra: map[string]any{"sessionId": "run-1", "tags": []map[string]any{{"name": sessions.DroiAutomationTag, "metadata": map[string]string{
					"automationId": "daily-review", "automationName": "Daily review", "trigger": "schedule", "runId": "r2",
				}}}}},
			{Title: "Nightly lint run", Cwd: "/Users/dev/.factory/automations/nightly-lint",
				Messages: []fakedaemon.Message{{Role: "user", Text: "Lint everything."}, {Role: "assistant", Text: "Two warnings in auth.go."}},
				Extra: map[string]any{"sessionId": "factory-run-1", "tags": []map[string]any{{"name": sessions.AutomationTag, "metadata": map[string]string{
					"automationId": "nightly-lint", "automationName": "Nightly lint", "triggerSource": "scheduled", "type": "run",
				}}}}},
		},
	}
}

// automationsHarness is the window on a Host whose Gateway answers the
// Automation endpoints with fake (nil: a Host without them).
func automationsHarness(t *testing.T, fake *fakeAutomations, with func(*Config)) *harness {
	t.Helper()
	return automationsHarnessOn(t, automationsScenario(), fake, with)
}

func automationsHarnessOn(t *testing.T, sc fakedaemon.Scenario, fake *fakeAutomations, with func(*Config)) *harness {
	t.Helper()
	h := startHarnessGateway(t, sc, "", "", func(o *gateway.Options) {
		if fake != nil {
			o.Automations = fake
		}
	}, with)
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	return h
}

// followedAutomations is the window following the Ubuntu box, whose Host
// answers the Automation endpoints with fake.
func followedAutomations(t *testing.T, fake *fakeAutomations) *harness {
	t.Helper()
	o := startOtherComputerWith(t, automationsScenario(), "", func(opts *gateway.Options) { opts.Automations = fake })
	h := followHarness(t, localSessions(), o.paired(), nil, nil)
	h.until("the other computer's Session", func() bool { return h.hasText("Fix the login race") })
	return h
}

func (h *harness) openAutomations() {
	h.t.Helper()
	h.click("Automations")
	h.until("the list", func() bool { return h.hasText("Daily review") })
}

func (h *harness) openDailyReview() {
	h.t.Helper()
	h.openAutomations()
	h.click("Daily review")
	h.until("the runs", func() bool { return h.hasText("Interrupted") })
}

// clickConfirm clicks a dialog's confirming button, which stands right of
// its Cancel: its words can also be those of a button the dialog covers.
func (h *harness) clickConfirm() {
	h.t.Helper()
	r, ok := h.tt.Find("Cancel")
	if !ok {
		h.t.Fatalf("no dialog: %q", h.tt.Texts())
	}
	keepTime(h.tt)
	h.tt.ClickAt(r.X+r.W+30, r.Y+r.H/2)
	h.frame()
}

// replaceText selects what the focused field holds and types s over it.
func (h *harness) replaceText(field, s string) {
	h.t.Helper()
	h.click(field)
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Type(s)
	h.frame()
}

// A run at work shows in the sidebar, under Automations, until it ends.
func TestARunningAutomationShowsInTheSidebar(t *testing.T) {
	h := automationsHarness(t, newFakeAutomations(t), nil)
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
	// The Fake Daemon's load reports no working state: an idle sent while
	// the Session loads can leave it working.
	h.until("the run's transcript", func() bool { return h.hasText("Nothing risky landed.") })
	if err := h.d.Notify("run-1", map[string]any{"type": "droid_working_state_changed", "newState": "idle"}); err != nil {
		t.Fatal(err)
	}
	h.until("the section gone", func() bool { return !h.hasText("Running automations") })
}

// The Host starts a run before the Daemon lists it; the sidebar shows the
// run once the list has it, with no other change to read the list again.
func TestARunListedLateShowsInTheSidebar(t *testing.T) {
	sc := automationsScenario()
	sc.Sessions[1].Extra["unwritten"] = true
	h := automationsHarnessOn(t, sc, newFakeAutomations(t), nil)
	reqs, err := h.d.Requests()
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	for _, r := range reqs {
		if r.Method == "daemon.list_available_sessions" {
			reads++
		}
	}
	if err := h.d.Notify("run-1", map[string]any{"type": "droid_working_state_changed", "newState": "thinking"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.d.WaitForRequest("daemon.list_available_sessions", reads+1); err != nil {
		t.Fatal(err)
	}
	if err := h.d.Write("run-1"); err != nil {
		t.Fatal(err)
	}
	h.until("the running run", func() bool { return h.hasText("Running automations") && h.hasText("[Automation] Daily review") })
}

// A run's Session leaves the sidebar for the Automations page, which lists
// the Host's Automations with their schedules in the Host's time and their
// runs, and runs, pauses, edits and opens them through the Gateway.
func TestTheAutomationsPageRunsPausesAndOpensAnAutomation(t *testing.T) {
	fake := newFakeAutomations(t)
	h := automationsHarness(t, fake, nil)
	if h.hasText("[Automation] Daily review") || h.hasText("Nightly lint run") {
		t.Fatal("a run is in the sidebar")
	}
	h.openAutomations()
	if !h.hasText("Every day at 09:00") {
		t.Errorf("no schedule in %q", h.tt.Texts())
	}
	if h.hasText("Factory automations on this computer") {
		t.Fatal("a Factory section without Factory automations")
	}

	h.click("Daily review")
	h.until("the runs", func() bool { return h.hasText("Interrupted") })
	for _, want := range []string{"Review yesterday's commits.", "Every day at 09:00", "0 9 * * *  Asia/Shanghai", "Don’t ask", "Its own folder in Droi’s data", "Catch-up", "Droi quit during the run.", "Succeeded"} {
		if !h.hasText(want) {
			t.Errorf("no %q in %q", want, h.tt.Texts())
		}
	}
	if h.hasText("Show full prompt") {
		t.Fatal("a short prompt folds")
	}
	h.shoot("automation-detail")
	h.click("Run now")
	h.until("the run's notice", func() bool { return h.hasText("Started a run.") })
	if got := fake.calls(&fake.ran); !slices.Equal(got, []string{"daily-review"}) {
		t.Fatalf("ran %q", got)
	}
	h.until("the new run", func() bool { return h.hasText("Running") })

	h.click("More actions")
	h.click("Pause")
	h.until("the paused notice", func() bool { return h.hasText("Paused.") })
	h.until("the paused status", func() bool { return slices.Contains(h.tt.Texts(), "Paused") })
	h.click("More actions")
	h.click("Resume")
	h.until("the active status", func() bool { return h.hasText("Resumed.") })
	if got := fake.calls(&fake.paused); !slices.Equal(got, []string{"pause daily-review", "resume daily-review"}) {
		t.Fatalf("paused %q", got)
	}

	h.click("Edit automation")
	h.until("the editor", func() bool { _, ok := h.tt.Find("Automation prompt"); return ok })
	if !h.hasText("Every day at 09:00, in this computer’s time (Asia/Shanghai).") {
		t.Errorf("no preview in the Host's time: %q", h.tt.Texts())
	}
	h.click("Automation name")
	h.tt.Type(" 2")
	h.frame()
	h.click("Save changes")
	h.until("the saved automation", func() bool {
		return h.a.Route().Tab == "" && h.hasText("Saved.") && h.hasText("Daily review 2")
	})
	u, _ := fake.lastSaved()
	if u.ID != "daily-review" || u.Name != "Daily review 2" || u.Schedule != "0 9 * * *" || u.Timezone != "" || u.Autonomy != "" {
		t.Fatalf("saved %+v", u)
	}

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

// A new Automation goes to the Host with its schedule in the Host's time,
// the model and effort and the permissions picked, then opens.
func TestCreatingAnAutomation(t *testing.T) {
	fake := newFakeAutomations(t)
	h := automationsHarness(t, fake, nil)
	// The whole form, the Permissions below the fold too.
	h.tt.SetSize(refW, 1200)
	h.openAutomations()
	h.click("New automation")
	h.until("the form", func() bool { _, ok := h.tt.Find("Automation name"); return ok })
	if h.hasText("UTC") {
		t.Fatalf("the form speaks of UTC: %q", h.tt.Texts())
	}
	h.until("the preview", func() bool { return h.hasText("Every day at 09:00, in this computer’s time (Asia/Shanghai).") })
	h.click("Automation name")
	h.tt.Type("Daily review")
	h.frame()
	h.click("Create automation")
	h.until("the prompt error", func() bool { return h.hasText("Enter a prompt for the automation to run.") })
	h.click("Automation prompt")
	h.tt.Type("List open pull requests.")
	h.frame()

	h.click("Custom")
	if !h.hasText("Five cron fields, in this computer’s time, such as 0 9 * * 1-5.") {
		t.Errorf("no custom help: %q", h.tt.Texts())
	}
	h.click("Create automation")
	h.until("the empty cron error", func() bool { return h.hasText("Enter five cron fields, such as 0 9 * * 1-5.") })
	h.click("Cron expression")
	h.tt.Type("0 25 * * *")
	h.frame()
	h.click("Create automation")
	h.until("the cron error", func() bool { return h.hasText("Cron expression not understood:") })
	h.click("Daily")

	h.click("Automation model")
	h.until("the model list", func() bool { _, ok := h.tt.Find("Choose a model"); return ok })
	h.click("GPT-5")
	h.click("Automation model")
	h.until("the model list", func() bool { _, ok := h.tt.Find("Reasoning effort"); return ok })
	h.click("High")
	h.tt.Key(0, ui.KeyEscape)
	h.frame()
	h.click("Don’t ask")
	h.click("Medium autonomy")
	h.until("the levels' help", func() bool { return h.hasText("A run asks before tools above this level") })
	h.shoot("automation-editor")

	h.click("Create automation")
	h.until("the new automation's page", func() bool {
		return h.a.Route().Automation == "daily-review-2" && h.hasText("Created.") && h.hasText("No runs yet")
	})
	p, _ := fake.lastSaved()
	if p.ID != "" || p.Name != "Daily review" || p.Prompt != "List open pull requests." || p.Schedule != "0 9 * * *" || p.Timezone != "" ||
		p.Model != "gpt-5" || p.ReasoningEffort != "high" || p.Autonomy != "medium" {
		t.Fatalf("created %+v", p)
	}
	if !h.hasText("Medium autonomy") {
		t.Fatalf("no permissions fact: %q", h.tt.Texts())
	}
}

// A level turned back to Don’t ask saves no autonomy.
func TestAPermissionTurnedBackToDontAsk(t *testing.T) {
	fake := newFakeAutomations(t)
	fake.list[0].Autonomy = "medium"
	h := automationsHarness(t, fake, nil)
	h.tt.SetSize(refW, 1200)
	h.openDailyReview()
	h.click("Edit automation")
	h.until("the editor", func() bool { _, ok := h.tt.Find("Automation prompt"); return ok })
	h.click("Medium autonomy")
	h.click("Don’t ask")
	h.until("the help", func() bool { return h.hasText("Runs use every tool without asking.") })
	h.click("Save changes")
	h.until("the saved automation", func() bool { return h.hasText("Saved.") })
	if p, _ := fake.lastSaved(); p.Autonomy != "" {
		t.Fatalf("saved autonomy %q", p.Autonomy)
	}
}

// Automations in a zone of their own say it in the list, on their page and
// in the editor, which can put one back in the Host's time.
func TestAutomationsInAZoneOfTheirOwn(t *testing.T) {
	fake := newFakeAutomations(t)
	fake.list = append(fake.list,
		gateway.Automation{ID: "monthly-report", Name: "Monthly report", Prompt: "Report.", Schedule: "0 9 1 * *", Timezone: "UTC"},
		gateway.Automation{ID: "paris-standup", Name: "Paris standup", Prompt: "Stand up.", Schedule: "30 8 * * 1-5", Timezone: "Europe/Paris"},
	)
	fake.runs["monthly-report"], fake.runs["paris-standup"] = nil, nil
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	for _, want := range []string{"0 9 1 * *, in the UTC time zone", "Weekdays at 08:30, in the Europe/Paris time zone"} {
		if !h.hasText(want) {
			t.Errorf("no %q in %q", want, h.tt.Texts())
		}
	}

	h.click("Monthly report")
	h.until("its page", func() bool { return h.hasText("0 9 1 * *  UTC") })
	if slices.Contains(h.tt.Texts(), "0 9 1 * *") {
		t.Errorf("the expression is said twice: %q", h.tt.Texts())
	}
	h.click("Edit automation")
	h.until("the editor", func() bool { return h.hasText("Use this computer’s time") })
	h.replaceText("Cron expression", "x")
	if !h.hasText("Five cron fields, in the UTC time zone, such as 0 9 * * 1-5.") {
		t.Errorf("no help in its zone: %q", h.tt.Texts())
	}
	h.click("Cancel")

	h.a.Go(Route{Name: "automations", Automation: "paris-standup"})
	h.until("its page", func() bool { return h.hasText("30 8 * * 1-5  Europe/Paris") && h.hasText("Weekdays at 08:30") })
	h.click("Edit automation")
	h.until("the preview", func() bool { return h.hasText("Weekdays at 08:30, in the Europe/Paris time zone.") })
	h.click("Use this computer’s time")
	h.until("the preview", func() bool { return h.hasText("Weekdays at 08:30, in this computer’s time (Asia/Shanghai).") })
	if h.hasText("Use this computer’s time") {
		t.Fatal("the action stays")
	}
	h.click("Save changes")
	h.until("the saved automation", func() bool { return h.hasText("Saved.") })
	if p, _ := fake.lastSaved(); p.Schedule != "30 8 * * 1-5" || p.Timezone != "" {
		t.Fatalf("saved %+v", p)
	}
}

// A run asked for while one works is named; another refusal says the
// Host's reason.
func TestRunNowRefused(t *testing.T) {
	fake := newFakeAutomations(t)
	fake.runErr = gateway.ErrAutomationRunning
	h := automationsHarness(t, fake, nil)
	h.openDailyReview()
	h.click("Run now")
	h.until("the refusal", func() bool { return h.hasText("A run of “Daily review” is already working.") })

	fake.mu.Lock()
	fake.runErr = &gateway.RefusedRequest{Reason: "The Daemon is not running."}
	fake.mu.Unlock()
	h.click("Run now")
	h.until("the reason", func() bool { return slices.Contains(h.tt.Texts(), "The Daemon is not running.") })
}

func TestTheHostsAutomationErrorsAreReadInOnePlace(t *testing.T) {
	for _, c := range []struct {
		err  error
		want automationTrouble
	}{
		{nil, troubleNone},
		{gateway.ErrNotFound, troubleOlderHost},
		{gateway.ErrAutomationNotFound, troubleGone},
		{gateway.ErrAutomationsElsewhere, troubleElsewhere},
		{gateway.ErrAutomationRunning, troubleRunning},
		{&gateway.StatusError{Status: 400, Message: "No."}, troubleRefused},
		{&gateway.StatusError{Status: 500, Message: "boom"}, troubleOther},
		{errors.New("offline"), troubleOther},
	} {
		if got := troubleOf(c.err); got != c.want {
			t.Errorf("troubleOf(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// Delete asks first, then the Automation goes and the list shows.
func TestDeletingAnAutomation(t *testing.T) {
	fake := newFakeAutomations(t)
	h := automationsHarness(t, fake, nil)
	h.openDailyReview()
	h.click("More actions")
	h.click("Delete…")
	h.until("the dialog", func() bool {
		return h.hasText("It stops running and its folder in Droi’s data is removed. The sessions of its past runs stay.")
	})
	h.clickConfirm()
	h.until("the list", func() bool { return h.a.Route().Automation == "" && h.hasText("Deleted “Daily review”.") })
	if got := fake.calls(&fake.deleted); !slices.Equal(got, []string{"daily-review"}) {
		t.Fatalf("deleted %q", got)
	}
	h.until("no automations", func() bool { return h.hasText("No automations yet") })
}

// An Automation the Host does not have says it may have been deleted.
func TestAnAutomationNotFound(t *testing.T) {
	h := automationsHarness(t, newFakeAutomations(t), nil)
	h.openAutomations()
	h.a.Go(Route{Name: "automations", Automation: "gone"})
	h.until("the page", func() bool { return h.hasText("Automation not found") && h.hasText("It may have been deleted.") })
}

func factoryFake(t *testing.T) *fakeAutomations {
	fake := newFakeAutomations(t)
	fake.factory = []gateway.FactoryAutomation{
		{ID: "nightly-lint", Name: "Nightly lint", Prompt: "Lint everything.", Schedule: "0 1 * * *", LocalSchedule: "0 9 * * *",
			Path: "/Users/dev/.factory/automations/nightly-lint"},
		{ID: "standup", Name: "Standup notes", Schedule: "0 8 * * 1-5", LocalSchedule: "0 16 * * 1-5", TargetsSession: true, Paused: true},
	}
	return fake
}

func (h *harness) moveNightlyLint() {
	h.t.Helper()
	h.until("the Factory section", func() bool { return h.hasText("Factory automations on this computer") })
	h.click("Move Nightly lint to Droi")
	h.until("the dialog", func() bool { return h.hasText("Move “Nightly lint” to Droi?") })
	h.clickConfirm()
}

// The Factory automations on the Host's computer show below Droi's, with
// their schedules as Droi would keep them, and one moves to Droi; one that
// posts into a Session cannot.
func TestMovingAFactoryAutomationToDroi(t *testing.T) {
	fake := factoryFake(t)
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	h.until("the Factory section", func() bool { return h.hasText("Factory automations on this computer") })
	for _, want := range []string{"Weekdays at 16:00", "Posts into an existing session, so it can’t move to Droi yet"} {
		if !h.hasText(want) {
			t.Errorf("no %q in %q", want, h.tt.Texts())
		}
	}
	if h.hasButton("Move Standup notes to Droi") {
		t.Fatal("the Standup row offers a move")
	}
	if _, ok := h.tt.Find("Paused"); !ok {
		t.Error("the paused Factory automation's mark has no name")
	}
	h.click("Move Nightly lint to Droi")
	h.until("the dialog", func() bool { return h.hasText("Move “Nightly lint” to Droi?") })
	if !h.hasText("Droi copies it, with its schedule in this computer’s time, and deletes it from Factory’s automations. The sessions of its past runs stay.") {
		t.Errorf("no dialog text: %q", h.tt.Texts())
	}
	h.shoot("automation-move")
	h.clickConfirm()
	h.until("the notice", func() bool { return h.hasText("Moved “Nightly lint” to Droi.") })
	if got := fake.calls(&fake.moved); !slices.Equal(got, []string{"nightly-lint"}) {
		t.Fatalf("moved %q", got)
	}
	h.until("both lists read again", func() bool {
		return !h.hasButton("Move Nightly lint to Droi") && slices.Contains(h.tt.Texts(), "Nightly lint")
	})
}

func TestAMovedPausedFactoryAutomationStaysPaused(t *testing.T) {
	fake := factoryFake(t)
	fake.factory[0].Paused = true
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	h.moveNightlyLint()
	h.until("the notice", func() bool { return h.hasText("Moved “Nightly lint” to Droi. It stays paused.") })
}

// A copy the Daemon did not delete says so, and the move that failed says
// why.
func TestAFactoryMoveThatDidNotDeleteOrFailedSaysSo(t *testing.T) {
	fake := factoryFake(t)
	fake.deleteError = "the Daemon is busy."
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	h.moveNightlyLint()
	h.until("the notice", func() bool {
		return h.hasText("Copied “Nightly lint” to Droi, but it is still in Factory’s automations: the Daemon is busy. Delete it there so it does not run twice.")
	})

	failed := factoryFake(t)
	failed.moveErr = &gateway.RefusedRequest{Reason: "its schedule is not five cron fields"}
	h = automationsHarness(t, failed, nil)
	h.openAutomations()
	h.moveNightlyLint()
	h.until("the notice", func() bool {
		return h.hasText("Could not move “Nightly lint”: its schedule is not five cron fields")
	})
}

// The Factory section stays out when only the Factory automations do not
// load.
func TestTheFactorySectionStaysOutWhenItDoesNotLoad(t *testing.T) {
	fake := factoryFake(t)
	fake.factoryErr = errors.New("the Daemon is not running")
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	h.settle()
	if h.hasText("Factory automations") || h.hasText("did not load") {
		t.Fatalf("a Factory section or an error: %q", h.tt.Texts())
	}
}

// A Factory automation opens a page of its own with its runs, which the
// Daemon tagged, and each opens its Session.
func TestAFactoryAutomationsRunsAreOnItsPage(t *testing.T) {
	fake := factoryFake(t)
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	h.until("the Factory section", func() bool { return h.hasText("Factory automations on this computer") })
	h.click("Nightly lint")
	h.until("its page", func() bool { return h.a.Route().Factory == "nightly-lint" && h.hasText("Factory automation") })
	for _, want := range []string{"Every day at 09:00", "0 9 * * *  Asia/Shanghai", "/Users/dev/.factory/automations/nightly-lint", "Lint everything.", "Nightly lint run"} {
		if !h.hasText(want) {
			t.Errorf("no %q in %q", want, h.tt.Texts())
		}
	}
	h.shoot("automation-factory")
	name, ok := h.findPrefix("Open run from")
	if !ok {
		t.Fatalf("no run row in %q", h.tt.Texts())
	}
	h.click(name)
	h.until("the run's Session", func() bool { return h.hasText("Two warnings in auth.go.") })
	if r := h.a.Route(); r.Name != "session" || r.SessionID != "factory-run-1" {
		t.Fatalf("went to %+v", r)
	}
}

// A Droi Automation moved from Factory lists the Daemon's runs of it below
// its own.
func TestAMovedAutomationListsItsEarlierRunsInFactory(t *testing.T) {
	fake := newFakeAutomations(t)
	fake.list[0].ForkedFrom = "nightly-lint"
	h := automationsHarness(t, fake, nil)
	h.openDailyReview()
	h.until("the earlier runs", func() bool { return h.hasText("Earlier runs in Factory") && h.hasText("Nightly lint run") })
}

// The Daemon tags its runs with the automation's folder name and, when it
// has one, its uuid (droid 1.4.1 daemon-core, local-history.ts): a moved
// Automation lists the runs of its uuid only, not those of another that had
// the same folder name.
func TestAMovedAutomationListsTheRunsOfItsUUID(t *testing.T) {
	sc := automationsScenario()
	run := func(title, id, uuid string) fakedaemon.SessionSpec {
		meta := map[string]string{"automationId": "nightly-lint", "automationName": "Nightly lint", "triggerSource": "scheduled", "type": "run"}
		if uuid != "" {
			meta["automationUuid"] = uuid
		}
		return fakedaemon.SessionSpec{Title: title, Cwd: "/Users/dev/.factory/automations/nightly-lint",
			Messages: []fakedaemon.Message{{Role: "user", Text: "Lint everything."}},
			Extra:    map[string]any{"sessionId": id, "tags": []map[string]any{{"name": sessions.AutomationTag, "metadata": meta}}}}
	}
	sc.Sessions = append(sc.Sessions[:2], run("This lint run", "factory-run-1", "uuid-nl"), run("Deleted lint run", "factory-run-2", "uuid-old"))
	fake := newFakeAutomations(t)
	fake.list[0].ForkedFrom, fake.list[0].ForkedFromName = "uuid-nl", "nightly-lint"
	h := automationsHarnessOn(t, sc, fake, nil)
	h.openDailyReview()
	h.until("the earlier runs", func() bool { return h.hasText("Earlier runs in Factory") && h.hasText("This lint run") })
	if h.hasText("Deleted lint run") {
		t.Fatal("listed the run of another automation of the same folder name")
	}
}

// A run whose Session waits for the user's approval says so in the runs.
func TestARunWaitingForApprovalSaysSo(t *testing.T) {
	fake := newFakeAutomations(t)
	fake.runs["daily-review"][0].Status = "running"
	h := automationsHarness(t, fake, nil)
	h.openDailyReview()
	if err := h.d.Notify("run-1", map[string]any{"type": "droid_working_state_changed", "newState": "waiting_for_tool_confirmation"}); err != nil {
		t.Fatal(err)
	}
	h.until("the waiting run", func() bool { return h.hasText("Waiting for your approval") })
}

// This computer's Host tells the page when its Automations change, also
// while another page shows.
func TestThePageReadsTheAutomationsAgainWhenTheHostSaysTheyChanged(t *testing.T) {
	fake := newFakeAutomations(t)
	var changed func()
	h := automationsHarness(t, fake, func(cfg *Config) {
		cfg.AutomationsChanged = func(fn func()) func() { changed = fn; return func() {} }
	})
	h.openAutomations()
	if changed == nil {
		t.Fatal("the window does not hear the Host")
	}
	fake.add(gateway.Automation{ID: "weekly", Name: "Weekly digest", Schedule: "0 9 * * 1"})
	changed()
	h.until("the new automation", func() bool { return h.hasText("Weekly digest") })

	h.a.Go(Route{Name: "home"})
	h.frame()
	fake.add(gateway.Automation{ID: "monthly", Name: "Monthly digest", Schedule: "0 9 1 * *"})
	changed()
	h.frame()
	h.click("Automations")
	h.until("the second one", func() bool { return h.hasText("Monthly digest") })
}

// A Host without the Automation endpoints is an older Droi on a followed
// computer, and a failure on this one; another Droi running them is named.
func TestAHostWithoutAutomationsSaysWhy(t *testing.T) {
	h := automationsHarness(t, nil, nil)
	h.click("Automations")
	h.until("the failure", func() bool { return h.hasText("Automations did not load:") })

	elsewhere := newFakeAutomations(t)
	elsewhere.listErr = gateway.ErrAutomationsElsewhere
	h = automationsHarness(t, elsewhere, nil)
	h.click("Automations")
	h.until("the text", func() bool {
		return h.hasText("Another Droi on this computer runs these automations. Quit it to manage them here.")
	})
	h.shoot("automations-unavailable")

	o := startOtherComputer(t, automationsScenario(), "")
	f := followHarness(t, localSessions(), o.paired(), nil, nil)
	f.until("the other computer's Session", func() bool { return f.hasText("Fix the login race") })
	f.click("Automations")
	f.until("the text", func() bool {
		return f.hasText("Ubuntu box runs an older Droi that cannot show its automations here. Update Droi there.")
	})

	f = followedAutomations(t, elsewhere)
	f.click("Automations")
	f.until("the text", func() bool {
		return f.hasText("Another Droi on Ubuntu box runs these automations. Quit it to manage them there.")
	})
}

// A followed computer's Automations are read in its time, named by its
// name.
func TestAFollowedComputersAutomationsAreInItsTime(t *testing.T) {
	h := followedAutomations(t, factoryFake(t))
	h.tt.SetSize(refW, 1200)
	h.openAutomations()
	h.until("the Factory section", func() bool { return h.hasText("Factory automations on Ubuntu box") })
	h.click("Move Nightly lint to Droi")
	h.until("the dialog", func() bool {
		return h.hasText("Droi copies it, with its schedule in Ubuntu box’s time, and deletes it from Factory’s automations. The sessions of its past runs stay.")
	})
	h.click("Cancel")
	h.click("New automation")
	h.until("the preview", func() bool { return h.hasText("Every day at 09:00, in Ubuntu box’s time (Asia/Shanghai).") })
	h.click("Custom")
	if !h.hasText("Five cron fields, in Ubuntu box’s time, such as 0 9 * * 1-5.") {
		t.Errorf("no custom help: %q", h.tt.Texts())
	}
}

// A long prompt shows its first lines until it is opened, so the runs
// below it stay in view.
func TestALongPromptFoldsUntilOpened(t *testing.T) {
	fake := newFakeAutomations(t)
	prompt := strings.TrimSpace(strings.Repeat("Check one more thing in yesterday's commits.\n", 12))
	fake.list[0].Prompt = prompt
	h := automationsHarness(t, fake, nil)
	h.openDailyReview()
	folded, ok := h.tt.Find(prompt)
	if !ok {
		t.Fatalf("no prompt in %q", h.tt.Texts())
	}
	h.click("Show full prompt")
	h.settle()
	open, _ := h.tt.Find(prompt)
	if open.H < folded.H*2 {
		t.Fatalf("the prompt is %v high opened, %v folded", open.H, folded.H)
	}
	h.click("Show less")
	h.settle()
	if again, _ := h.tt.Find(prompt); again.H != folded.H {
		t.Fatalf("the prompt is %v high folded again, %v at first", again.H, folded.H)
	}
}

// A Factory automation whose schedule never ran in Factory says why on its
// row and its page, which offer no move.
func TestAFactoryAutomationThatCannotMoveForItsScheduleSaysWhy(t *testing.T) {
	fake := factoryFake(t)
	fake.factory = append(fake.factory, gateway.FactoryAutomation{ID: "odd", Name: "Odd hours", Schedule: "0 9 L * *",
		NotMovable: `the schedule "0 9 L * *" never ran in Factory: L is not a number`})
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	h.until("the Factory section", func() bool { return h.hasText("Odd hours") })
	if !h.hasText("Its schedule never ran in Factory, so it can’t move to Droi") || h.hasButton("Move Odd hours to Droi") {
		t.Fatalf("the row offers a move: %q", h.tt.Texts())
	}
	h.click("Odd hours")
	h.until("its page", func() bool { return h.hasText("Factory automation") && h.hasText("Details") })
	if !h.finds("Its schedule never ran in Factory, so it can’t move to Droi") {
		t.Fatalf("the page does not say it in the window's language: %q", h.tt.Texts())
	}
	if h.hasText("Can’t move to Droi") || h.hasText("L is not a number") {
		t.Fatalf("the page shows the Host's English reason: %q", h.tt.Texts())
	}
	if h.hasButton("Move Odd hours to Droi") {
		t.Fatal("the page offers a move")
	}
}

// A Factory page reached while the Factory automations did not load says
// so, not that the automation is gone.
func TestAFactoryPageWhoseListDidNotLoadSaysSo(t *testing.T) {
	fake := factoryFake(t)
	fake.factoryErr = errors.New("the Daemon is not running")
	h := automationsHarness(t, fake, nil)
	h.click("Automations")
	h.a.Go(Route{Name: "automations", Factory: "nightly-lint"})
	h.until("the failure", func() bool { return h.hasText("Automations did not load: the Daemon is not running") })
	if h.hasText("It may have been deleted.") {
		t.Fatalf("says it is gone: %q", h.tt.Texts())
	}
}

// A failure after another Droi ran the Automations says the failure.
func TestAFailureAfterElsewhereSaysTheFailure(t *testing.T) {
	fake := newFakeAutomations(t)
	fake.listErr = gateway.ErrAutomationsElsewhere
	var changed func()
	h := automationsHarness(t, fake, func(cfg *Config) {
		cfg.AutomationsChanged = func(fn func()) func() { changed = fn; return func() {} }
	})
	h.click("Automations")
	h.until("the elsewhere text", func() bool { return h.hasText("Another Droi on this computer runs these automations.") })
	fake.mu.Lock()
	fake.listErr = errors.New("disk on fire")
	fake.mu.Unlock()
	changed()
	h.until("the failure", func() bool {
		return h.hasText("Automations did not load: disk on fire") && !h.hasText("Another Droi on this computer")
	})
}

// A Factory automation without runs says so on its page.
func TestAFactoryPageWithoutRunsSaysSo(t *testing.T) {
	h := automationsHarness(t, factoryFake(t), nil)
	h.openAutomations()
	h.until("the Factory section", func() bool { return h.hasText("Standup notes") })
	h.click("Standup notes")
	h.until("its page", func() bool { return h.a.Route().Factory == "standup" && h.hasText("No runs yet.") })
}

// Moving a Factory automation from its page opens the copy's page.
func TestMovingFromTheFactoryPageOpensTheCopy(t *testing.T) {
	fake := factoryFake(t)
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	h.until("the Factory section", func() bool { return h.hasText("Nightly lint") })
	h.click("Nightly lint")
	h.until("its page", func() bool { return h.a.Route().Factory == "nightly-lint" })
	h.click("Move Nightly lint to Droi")
	h.until("the dialog", func() bool { return h.hasText("Move “Nightly lint” to Droi?") })
	h.clickConfirm()
	h.until("the copy's page", func() bool {
		r := h.a.Route()
		return r.Factory == "" && r.Automation == "nightly-lint" && h.hasText("Moved “Nightly lint” to Droi.")
	})
}

// A run and a move under way have named spinners.
func TestTheSpinnersOfARunAndAMoveAreNamed(t *testing.T) {
	fake := factoryFake(t)
	fake.block = make(chan struct{})
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	r, _ := h.tt.Find("Daily review")
	h.tt.Move(r.X+r.W/2, r.Y+r.H/2)
	h.frame()
	h.click("Run Daily review now")
	h.until("the run's spinner", func() bool { _, ok := h.tt.Find("Starting…"); return ok })
	fake.block <- struct{}{}
	h.until("the run's notice", func() bool { return h.hasText("Started a run.") })

	h.click("Move Nightly lint to Droi")
	h.until("the dialog", func() bool { return h.hasText("Move “Nightly lint” to Droi?") })
	h.clickConfirm()
	h.until("the move's spinner", func() bool { _, ok := h.tt.Find("Moving…"); return ok })
	fake.block <- struct{}{}
	h.until("the move's notice", func() bool { return h.hasText("Moved “Nightly lint” to Droi.") })
}

// On a followed computer, an Automation in a zone of its own can be put in
// that computer's time.
func TestAFollowedComputersZoneActionNamesIt(t *testing.T) {
	fake := newFakeAutomations(t)
	fake.list = append(fake.list, gateway.Automation{ID: "paris-standup", Name: "Paris standup", Prompt: "Stand up.", Schedule: "30 8 * * 1-5", Timezone: "Europe/Paris"})
	h := followedAutomations(t, fake)
	h.openAutomations()
	h.a.Go(Route{Name: "automations", Automation: "paris-standup", Tab: editAutomationTab})
	h.until("the action", func() bool { return h.hasText("Use Ubuntu box’s time") })
	h.click("Use Ubuntu box’s time")
	h.until("the preview", func() bool { return h.hasText("Weekdays at 08:30, in Ubuntu box’s time (Asia/Shanghai).") })
}

// A followed computer tells of no change: while a run runs, its runs are
// read again every 3 seconds of the window's clock.
func TestAFollowedComputersRunningRunsAreReadAgain(t *testing.T) {
	fake := newFakeAutomations(t)
	fake.runs["daily-review"][0].Status = "running"
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	o := startOtherComputerWith(t, automationsScenario(), "", func(opts *gateway.Options) { opts.Automations = fake })
	h := followHarness(t, localSessions(), o.paired(), nil, func(cfg *Config) { cfg.Now = clock })
	h.until("the other computer's Session", func() bool { return h.hasText("Fix the login race") })
	h.openDailyReview()
	h.settle()
	read := fake.runsRead()
	for range 20 {
		h.frame()
		time.Sleep(20 * time.Millisecond)
	}
	if got := fake.runsRead(); got != read {
		t.Fatalf("read again before 3 s: %d, then %d", read, got)
	}
	mu.Lock()
	now = now.Add(3 * time.Second)
	mu.Unlock()
	h.until("the runs read again", func() bool { return fake.runsRead() > read })
}

// The Daemon's runs are indexed once per list, not per frame.
func TestTheDaemonRunsAreIndexedOncePerList(t *testing.T) {
	a := New(Config{})
	run := func(id, automation string) sessions.Summary {
		return sessions.Summary{SessionID: id, Tags: []sessions.Tag{{Name: sessions.AutomationTag, Metadata: map[string]string{"automationId": automation, "type": "run"}}}}
	}
	a.shownRev = 1
	a.noteLiveRuns([]sessions.Summary{run("s1", "nightly-lint")}, nil)
	a.noteLiveRuns([]sessions.Summary{run("s1", "nightly-lint"), run("s2", "nightly-lint")}, nil)
	if got := a.daemonRunsOf("nightly-lint"); len(got) != 1 {
		t.Fatalf("indexed again for the same list: %v", got)
	}
	a.shownRev = 2
	a.noteLiveRuns([]sessions.Summary{run("s1", "nightly-lint"), run("s2", "nightly-lint")}, nil)
	if got := a.daemonRunsOf("nightly-lint"); len(got) != 2 {
		t.Fatalf("not indexed for a new list: %v", got)
	}
	if n := testing.AllocsPerRun(100, func() { a.daemonRunsOf("nightly-lint") }); n != 0 {
		t.Fatalf("%v allocations a call", n)
	}
}
