package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/internal/automations"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

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

	// runErr answers a Run; moveErr and deleteError a MoveFactory.
	runErr      error
	moveErr     error
	deleteError string
}

var _ gateway.Automations = (*fakeAutomations)(nil)

func (f *fakeAutomations) find(id string) int {
	return slices.IndexFunc(f.list, func(a gateway.Automation) bool { return a.ID == id })
}

func (f *fakeAutomations) List(context.Context) ([]gateway.Automation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.list), nil
}

func (f *fakeAutomations) Runs(_ context.Context, id string, _ int) ([]gateway.AutomationRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.find(id) < 0 {
		return nil, gateway.ErrAutomationNotFound
	}
	return slices.Clone(f.runs[id]), nil
}

func (f *fakeAutomations) Save(_ context.Context, a gateway.Automation) (gateway.Automation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, a)
	if a.ID == "" {
		taken := map[string]bool{}
		for _, x := range f.list {
			taken[x.ID] = true
		}
		a.ID = automations.ID(a.Name, taken)
		f.list = append(f.list, a)
		return a, nil
	}
	i := f.find(a.ID)
	if i < 0 {
		return gateway.Automation{}, gateway.ErrAutomationNotFound
	}
	a.NextRunAt, a.LastRun = f.list[i].NextRunAt, f.list[i].LastRun
	f.list[i] = a
	return a, nil
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
	return slices.Clone(f.factory), nil
}

func (f *fakeAutomations) MoveFactory(_ context.Context, id string) (gateway.AutomationMoved, error) {
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
	a := gateway.Automation{ID: fa.ID, Name: fa.Name, Prompt: fa.Prompt, Schedule: "0 9 * * *", Paused: fa.Paused}
	f.list = append(f.list, a)
	if f.deleteError == "" {
		f.factory = slices.Delete(f.factory, i, i+1)
	}
	return gateway.AutomationMoved{Automation: a, DeleteError: f.deleteError}, nil
}

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

// yesterdayNoon is yesterday at noon: 26 hours ago, in the first two hours
// of a day, is the day before yesterday, which the runs list names by its
// date.
func yesterdayNoon() string {
	y := time.Now().AddDate(0, 0, -1)
	return time.Date(y.Year(), y.Month(), y.Day(), 12, 0, 0, 0, time.Local).UTC().Format(time.RFC3339)
}

// newFakeAutomations is the Host's Daily review, with a run whose Session
// the Fake Daemon has, and an interrupted catch-up run before it.
func newFakeAutomations() *fakeAutomations {
	started := yesterdayNoon()
	last := gateway.AutomationRun{RunID: "r2", SessionID: "run-1", Trigger: "schedule", Status: "succeeded", StartedAt: started, EndedAt: started}
	return &fakeAutomations{
		list: []gateway.Automation{{
			ID: "daily-review", Name: "Daily review", Prompt: "Review yesterday's commits.", Schedule: "0 9 * * *",
			NextRunAt: time.Now().Add(5 * time.Hour).UTC().Format(time.RFC3339), LastRun: &last, Folder: "/data/automations/daily-review",
		}},
		runs: map[string][]gateway.AutomationRun{"daily-review": {
			last,
			{RunID: "r1", Trigger: "catch-up", Status: "interrupted", Error: "Droi quit during the run.",
				StartedAt: time.Now().AddDate(0, 0, -3).UTC().Format(time.RFC3339)},
		}},
	}
}

func automationsScenario() fakedaemon.Scenario {
	return fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{
			{Title: "Fix the login race", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "Fix it"}}},
			{Title: "[Automation] Daily review", Cwd: "/data/automations/daily-review",
				Messages: []fakedaemon.Message{{Role: "user", Text: "Review yesterday's commits."}, {Role: "assistant", Text: "Nothing risky landed."}},
				Extra: map[string]any{"sessionId": "run-1", "tags": []map[string]any{{"name": sessions.DroiAutomationTag, "metadata": map[string]string{
					"automationId": "daily-review", "automationName": "Daily review", "trigger": "schedule", "runId": "r2",
				}}}}},
		},
	}
}

// automationsHarness is the window on a Host whose Gateway answers the
// Automation endpoints with fake (nil: an older Host, without them).
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

func (h *harness) openAutomations() {
	h.t.Helper()
	h.click("Automations")
	h.until("the list", func() bool { return h.hasText("Daily review") })
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

// A run at work shows in the sidebar, under Automations, until it ends.
func TestARunningAutomationShowsInTheSidebar(t *testing.T) {
	h := automationsHarness(t, newFakeAutomations(), nil)
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
	h := automationsHarnessOn(t, sc, newFakeAutomations(), nil)
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
// the Host's Automations with their schedules in local time and their runs,
// and runs, pauses, edits and opens them through the Gateway.
func TestTheAutomationsPageRunsPausesAndOpensAnAutomation(t *testing.T) {
	fake := newFakeAutomations()
	h := automationsHarness(t, fake, nil)
	if h.hasText("[Automation] Daily review") {
		t.Fatal("the run is in the sidebar")
	}
	h.openAutomations()
	if want := automations.Describe("0 9 * * *", 0); !h.hasText(want) {
		t.Errorf("no %q in %q", want, h.tt.Texts())
	}
	if h.hasText("Factory automations on this computer") {
		t.Fatal("a Factory section without Factory automations")
	}

	h.click("Daily review")
	h.until("the runs", func() bool { return h.hasText("Interrupted") })
	for _, want := range []string{"Review yesterday's commits.", "0 9 * * *  " + h.a.zoneName(""), "Don’t ask", "Its own folder in Droi’s data", "Catch-up", "Droi quit during the run.", "Succeeded"} {
		if !h.hasText(want) {
			t.Errorf("no %q in %q", want, h.tt.Texts())
		}
	}
	if h.hasText("Show full prompt") {
		t.Fatal("a short prompt folds")
	}
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
	if !h.hasText("in this computer’s time (" + h.a.zoneName("") + ")") {
		t.Errorf("no preview in local time: %q", h.tt.Texts())
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

// A new Automation goes to the Host with its schedule in local time, the
// model and effort and the permissions picked, then opens.
func TestCreatingAnAutomation(t *testing.T) {
	fake := newFakeAutomations()
	h := automationsHarness(t, fake, nil)
	// The whole form, the Permissions below the fold too.
	h.tt.SetSize(refW, 1200)
	h.openAutomations()
	h.click("New automation")
	h.until("the form", func() bool { _, ok := h.tt.Find("Automation name"); return ok })
	if h.hasText("UTC") {
		t.Fatalf("the form speaks of UTC: %q", h.tt.Texts())
	}
	h.click("Automation name")
	h.tt.Type("Daily review")
	h.frame()
	h.click("Create automation")
	h.until("the prompt error", func() bool { return h.hasText("Enter a prompt for the automation to run.") })
	h.click("Automation prompt")
	h.tt.Type("List open pull requests.")
	h.frame()

	h.click("Custom")
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

// A refused run says the Host's reason; a run already working is named.
func TestRunNowRefusedWhileARunWorks(t *testing.T) {
	fake := newFakeAutomations()
	fake.runErr = &gateway.RefusedRequest{Reason: "A run of “Daily review” is already working."}
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	h.click("Daily review")
	h.until("the runs", func() bool { return h.hasText("Interrupted") })
	h.click("Run now")
	h.until("the refusal", func() bool { return h.hasText("A run of “Daily review” is already working.") })
	if h.hasText("Could not") {
		t.Fatalf("the refusal is wrapped: %q", h.tt.Texts())
	}
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
		{&gateway.StatusError{Status: http.StatusBadRequest, Message: "No."}, troubleRefused},
		{&gateway.StatusError{Status: http.StatusInternalServerError, Message: "boom"}, troubleOther},
		{errors.New("offline"), troubleOther},
	} {
		if got := troubleOf(c.err); got != c.want {
			t.Errorf("troubleOf(%v) = %v, want %v", c.err, got, c.want)
		}
	}
	if got := failure("run", "Daily review", gateway.ErrAutomationRunning); got != "A run of “Daily review” is already working." {
		t.Errorf("a run already working: %q", got)
	}
	if got := failure("delete", "Daily review", gateway.ErrAutomationNotFound); got != "It may have been deleted." {
		t.Errorf("a deleted automation: %q", got)
	}
}

// Delete asks first, then the Automation goes and the list shows.
func TestDeletingAnAutomation(t *testing.T) {
	fake := newFakeAutomations()
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	h.click("Daily review")
	h.until("the runs", func() bool { return h.hasText("Interrupted") })
	h.click("More actions")
	h.click("Delete…")
	h.until("the dialog", func() bool {
		return h.hasText("It stops running and its folder in Droi’s data is removed. The sessions of its past runs stay.")
	})
	h.click("Delete")
	h.until("the list", func() bool { return h.a.Route().Automation == "" && h.hasText("Deleted “Daily review”.") })
	if got := fake.calls(&fake.deleted); !slices.Equal(got, []string{"daily-review"}) {
		t.Fatalf("deleted %q", got)
	}
	h.until("no automations", func() bool { return h.hasText("No automations yet") })
}

// An Automation the Host does not have says it may have been deleted.
func TestAnAutomationNotFound(t *testing.T) {
	h := automationsHarness(t, newFakeAutomations(), nil)
	h.openAutomations()
	h.a.Go(Route{Name: "automations", Automation: "gone"})
	h.until("the page", func() bool { return h.hasText("Automation not found") && h.hasText("It may have been deleted.") })
}

func factoryFake() *fakeAutomations {
	fake := newFakeAutomations()
	fake.factory = []gateway.FactoryAutomation{
		{ID: "nightly-lint", Name: "Nightly lint", Schedule: "0 9 * * *"},
		{ID: "standup", Name: "Standup notes", Schedule: "0 8 * * 1-5", TargetsSession: true},
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

// The Factory automations on the Host's computer show below Droi's, and
// one moves to Droi; one that posts into a Session cannot.
func TestMovingAFactoryAutomationToDroi(t *testing.T) {
	fake := factoryFake()
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	h.until("the Factory section", func() bool { return h.hasText("Factory automations on this computer") })
	if !h.hasText("Posts into an existing session, so it can’t move to Droi yet") || h.hasButton("Move Standup notes to Droi") {
		t.Fatalf("the Standup row offers a move: %q", h.tt.Texts())
	}
	if dir := os.Getenv("DROI_NATIVE_SHOTS"); dir != "" {
		h.settle()
		_ = os.MkdirAll(dir, 0o755)
		writePNG(filepath.Join(dir, "automations-factory.png"), h.tt.Image())
	}
	h.moveNightlyLint()
	h.until("the notice", func() bool { return h.hasText("Moved “Nightly lint” to Droi.") })
	if got := fake.calls(&fake.moved); !slices.Equal(got, []string{"nightly-lint"}) {
		t.Fatalf("moved %q", got)
	}
	h.until("both lists read again", func() bool {
		return !h.hasButton("Move Nightly lint to Droi") && slices.Contains(h.tt.Texts(), "Nightly lint")
	})
}

func TestAMovedPausedFactoryAutomationStaysPaused(t *testing.T) {
	fake := factoryFake()
	fake.factory[0].Paused = true
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	h.moveNightlyLint()
	h.until("the notice", func() bool { return h.hasText("Moved “Nightly lint” to Droi. It stays paused.") })
}

// A copy the Daemon did not delete says so, and the move that failed says
// why.
func TestAFactoryMoveThatDidNotDeleteOrFailedSaysSo(t *testing.T) {
	fake := factoryFake()
	fake.deleteError = "the Daemon is busy."
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	h.moveNightlyLint()
	h.until("the notice", func() bool {
		return h.hasText("Copied “Nightly lint” to Droi, but it is still in Factory’s automations: the Daemon is busy. Delete it there so it does not run twice.")
	})

	failed := factoryFake()
	failed.moveErr = &gateway.RefusedRequest{Reason: "its schedule is not five cron fields"}
	h = automationsHarness(t, failed, nil)
	h.openAutomations()
	h.moveNightlyLint()
	h.until("the notice", func() bool {
		return h.hasText("Could not move “Nightly lint”: its schedule is not five cron fields")
	})
}

// A run whose Session waits for the user's approval says so in the runs.
func TestARunWaitingForApprovalSaysSo(t *testing.T) {
	fake := newFakeAutomations()
	fake.runs["daily-review"][0].Status = "running"
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	h.click("Daily review")
	h.until("the runs", func() bool { return h.hasText("Interrupted") })
	if err := h.d.Notify("run-1", map[string]any{"type": "droid_working_state_changed", "newState": "waiting_for_tool_confirmation"}); err != nil {
		t.Fatal(err)
	}
	h.until("the waiting run", func() bool { return h.hasText("Waiting for your approval") })
}

// This computer's Host tells the page when its Automations change.
func TestThePageReadsTheAutomationsAgainWhenTheHostSaysTheyChanged(t *testing.T) {
	fake := newFakeAutomations()
	var changed func()
	h := automationsHarness(t, fake, func(cfg *Config) {
		cfg.AutomationsChanged = func(fn func()) func() { changed = fn; return func() {} }
	})
	h.openAutomations()
	fake.mu.Lock()
	fake.list = append(fake.list, gateway.Automation{ID: "weekly", Name: "Weekly digest", Schedule: "0 9 * * 1"})
	fake.mu.Unlock()
	if changed == nil {
		t.Fatal("the window does not hear the Host")
	}
	changed()
	h.until("the new automation", func() bool { return h.hasText("Weekly digest") })
}

// A Host without the Automation endpoints: on this computer another Droi
// runs them; another computer runs an older Droi.
func TestAHostWithoutAutomationsSaysWhy(t *testing.T) {
	h := automationsHarness(t, nil, nil)
	h.click("Automations")
	h.until("the text", func() bool {
		return h.hasText("Another Droi on this computer runs these automations. Quit it to manage them here.")
	})

	o := startOtherComputer(t, automationsScenario(), "")
	f := followHarness(t, localSessions(), o.paired(), nil, nil)
	f.until("the other computer's Session", func() bool { return f.hasText("Fix the login race") })
	f.click("Automations")
	f.until("the text", func() bool {
		return f.hasText("Ubuntu box runs an older Droi that cannot show its automations here. Update Droi there.")
	})
}

// A long prompt shows its first lines until it is opened, so the runs
// below it stay in view.
func TestALongPromptFoldsUntilOpened(t *testing.T) {
	fake := newFakeAutomations()
	prompt := strings.TrimSpace(strings.Repeat("Check one more thing in yesterday's commits.\n", 12))
	fake.list[0].Prompt = prompt
	h := automationsHarness(t, fake, nil)
	h.openAutomations()
	h.click("Daily review")
	h.until("the runs", func() bool { return h.hasText("Interrupted") })
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
