package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
)

type fakeAutomations struct {
	mu     sync.Mutex
	list   []Automation
	saved  []Automation
	paused map[string]bool
	ran    []string
	gone   []string
	moved  []string
	limit  int
}

func (f *fakeAutomations) find(id string) (Automation, error) {
	for _, a := range f.list {
		if a.ID == id {
			return a, nil
		}
	}
	return Automation{}, ErrAutomationNotFound
}

func (f *fakeAutomations) List(context.Context) ([]Automation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.list, nil
}

func (f *fakeAutomations) Runs(_ context.Context, id string, limit int) ([]AutomationRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limit = limit
	if _, err := f.find(id); err != nil {
		return nil, err
	}
	return []AutomationRun{{RunID: "r2", Trigger: "manual", Status: "running"}, {RunID: "r1", Trigger: "catch-up", Status: "interrupted", Error: "Droi quit during the run."}}, nil
}

func (f *fakeAutomations) Save(_ context.Context, a Automation) (Automation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a.Schedule == "@daily" {
		return Automation{}, &RefusedRequest{Reason: "@daily: write the schedule as 5 fields"}
	}
	f.saved = append(f.saved, a)
	if a.ID == "" {
		a.ID = "daily-report"
	}
	a.NextRunAt = "2026-11-06T09:00:00+08:00"
	return a, nil
}

func (f *fakeAutomations) SetPaused(_ context.Context, id string, paused bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.find(id); err != nil {
		return err
	}
	f.paused[id] = paused
	return nil
}

func (f *fakeAutomations) Run(_ context.Context, id string) (AutomationRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if slices.Contains(f.ran, id) {
		return AutomationRun{}, ErrAutomationRunning
	}
	f.ran = append(f.ran, id)
	return AutomationRun{RunID: "r3", SessionID: "s3", Trigger: "manual", Status: "running"}, nil
}

func (f *fakeAutomations) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gone = append(f.gone, id)
	return nil
}

func (f *fakeAutomations) Factory(context.Context) ([]FactoryAutomation, error) {
	return []FactoryAutomation{
		{ID: "droid", Name: "Droid二进制逆向", Schedule: "0 1 * * *", Path: "/Users/dev/.factory/automations/droid", LocalSchedule: "0 9 * * *"},
		{ID: "digest", Name: "Digest", Schedule: "daily", TargetsSession: true, LocalSchedule: "0 9 * * *", LocalTimezone: "UTC"},
		{ID: "odd", Name: "Odd", Schedule: "0 9 L * *", NotMovable: "the schedule never ran in Factory"},
	}, nil
}

func (f *fakeAutomations) MoveFactory(_ context.Context, id string) (AutomationMoved, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moved = append(f.moved, id)
	if id == "broken" {
		return AutomationMoved{}, errors.New("the Daemon is not running")
	}
	return AutomationMoved{Automation: Automation{ID: "droid", Name: "Droid二进制逆向", Schedule: "0 9 * * *"}, DeleteError: "Automation not found"}, nil
}

func automationsClient(t *testing.T, au Automations) *Client {
	t.Helper()
	opts := baseOptions(nil)
	if au != nil {
		opts.Automations = au
	}
	g := startGateway(t, opts)
	c, err := NewClient(daemonURL(g.URL(), testToken))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTheAutomationEndpointsReachTheHost(t *testing.T) {
	f := &fakeAutomations{list: []Automation{{ID: "daily-report", Name: "Daily report", Schedule: "0 9 * * *", Running: true, LastRun: &AutomationRun{RunID: "r2", Status: "running"}}}, paused: map[string]bool{}}
	c := automationsClient(t, f)
	ctx := context.Background()

	list, err := c.Automations(ctx)
	if err != nil || len(list) != 1 || list[0].ID != "daily-report" || !list[0].Running || list[0].LastRun.RunID != "r2" {
		t.Fatalf("list %+v %v", list, err)
	}
	runs, err := c.AutomationRuns(ctx, "daily-report")
	if err != nil || len(runs) != 2 || runs[1].Error != "Droi quit during the run." || f.limit != 50 {
		t.Fatalf("runs %+v %v, limit %d", runs, err, f.limit)
	}
	if _, err := c.AutomationRuns(ctx, "gone"); !errors.Is(err, ErrAutomationNotFound) {
		t.Fatalf("runs of an unknown one: %v", err)
	}

	saved, err := c.SaveAutomation(ctx, Automation{Name: "Daily report", Prompt: "Report.", Schedule: "0 9 * * 1-5", Autonomy: "medium", WorkingDirectory: "/Users/me/app"})
	if err != nil || saved.ID != "daily-report" || saved.NextRunAt == "" || len(f.saved) != 1 || f.saved[0].Autonomy != "medium" || f.saved[0].WorkingDirectory != "/Users/me/app" {
		t.Fatalf("saved %+v %v, host got %+v", saved, err, f.saved)
	}
	_, err = c.SaveAutomation(ctx, Automation{Name: "x", Schedule: "@daily"})
	var status *StatusError
	if !errors.As(err, &status) || status.Status != http.StatusBadRequest || status.Message != "@daily: write the schedule as 5 fields" {
		t.Fatalf("a refused save: %v", err)
	}

	if err := c.PauseAutomation(ctx, "daily-report", true); err != nil || !f.paused["daily-report"] {
		t.Fatalf("pause: %v %v", err, f.paused)
	}
	if err := c.PauseAutomation(ctx, "daily-report", false); err != nil || f.paused["daily-report"] {
		t.Fatalf("resume: %v %v", err, f.paused)
	}
	if err := c.PauseAutomation(ctx, "gone", true); !errors.Is(err, ErrAutomationNotFound) {
		t.Fatalf("pause an unknown one: %v", err)
	}

	run, err := c.RunAutomation(ctx, "daily-report")
	if err != nil || run.RunID != "r3" || run.SessionID != "s3" {
		t.Fatalf("run %+v %v", run, err)
	}
	if _, err := c.RunAutomation(ctx, "daily-report"); !errors.Is(err, ErrAutomationRunning) {
		t.Fatalf("a second run: %v", err)
	}
	if err := c.DeleteAutomation(ctx, "daily-report"); err != nil || !slices.Equal(f.gone, []string{"daily-report"}) {
		t.Fatalf("delete: %v %v", err, f.gone)
	}

	factory, err := c.FactoryAutomations(ctx)
	if err != nil || len(factory) != 3 || factory[0].ID != "droid" || factory[0].Path == "" || factory[0].TargetsSession || !factory[1].TargetsSession {
		t.Fatalf("factory %+v %v", factory, err)
	}
	moved, err := c.MoveFactoryAutomation(ctx, "droid")
	if err != nil || moved.Automation.Schedule != "0 9 * * *" || moved.DeleteError != "Automation not found" {
		t.Fatalf("moved %+v %v", moved, err)
	}
	if _, err := c.MoveFactoryAutomation(ctx, "broken"); !errors.As(err, &status) || status.Status != http.StatusInternalServerError || status.Message != "the Daemon is not running" {
		t.Fatalf("a failed move: %v", err)
	}
}

func TestTheAutomationEndpointsAskForTheTokenTheMethodAndTheID(t *testing.T) {
	opts := baseOptions(nil)
	opts.Automations = &fakeAutomations{paused: map[string]bool{}}
	g := startGateway(t, opts)
	for _, c := range []struct {
		method, path, query string
		want                int
	}{
		{http.MethodGet, AutomationsPath, "", http.StatusUnauthorized},
		{http.MethodPost, AutomationsPath, "token", http.StatusMethodNotAllowed},
		{http.MethodGet, AutomationRunPath, "token", http.StatusMethodNotAllowed},
		{http.MethodPost, AutomationRunPath, "token", http.StatusBadRequest},
		{http.MethodPost, AutomationPausePath, "token&id=a&paused=maybe", http.StatusBadRequest},
		{http.MethodPost, AutomationSavePath, "token", http.StatusBadRequest},
		{http.MethodGet, FactoryAutomationsPath, "token", http.StatusOK},
		{http.MethodGet, AutomationZonePath, "token", http.StatusOK},
		{http.MethodPost, AutomationZonePath, "token", http.StatusMethodNotAllowed},
	} {
		q := ""
		if c.query != "" {
			q = "?token=" + testToken + c.query[len("token"):]
		}
		req, _ := http.NewRequest(c.method, g.URL()+c.path+q, nil)
		resp, err := freshClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("%s %s%s: %d, want %d", c.method, c.path, q, resp.StatusCode, c.want)
		}
	}
	// A Host without Automations: the endpoints are not there.
	if _, err := automationsClient(t, nil).Automations(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an older Host: %v", err)
	}
}

// elsewhere is a Host whose Automations another Droi of the same data runs.
type elsewhere struct{ Automations }

func (elsewhere) List(context.Context) ([]Automation, error) { return nil, ErrAutomationsElsewhere }
func (elsewhere) Run(context.Context, string) (AutomationRun, error) {
	return AutomationRun{}, ErrAutomationsElsewhere
}

func TestAHostWhoseAutomationsRunElsewhereAnswers503(t *testing.T) {
	c := automationsClient(t, elsewhere{})
	if _, err := c.Automations(context.Background()); !errors.Is(err, ErrAutomationsElsewhere) {
		t.Fatalf("list: %v", err)
	}
	if _, err := c.RunAutomation(context.Background(), "a"); !errors.Is(err, ErrAutomationsElsewhere) {
		t.Fatalf("run: %v", err)
	}
}

func TestASaveBodyMustBeJSONOfAModestSize(t *testing.T) {
	opts := baseOptions(nil)
	opts.Automations = &fakeAutomations{paused: map[string]bool{}}
	g := startGateway(t, opts)
	for _, c := range []struct {
		body       string
		status     int
		wantReason string
	}{
		{"{not json", http.StatusBadRequest, "The request is not JSON"},
		{`{"name":"` + strings.Repeat("x", 2<<20) + `"}`, http.StatusRequestEntityTooLarge, "The request is too large"},
	} {
		resp, err := freshClient.Post(g.URL()+AutomationSavePath+"?token="+testToken, "application/json", strings.NewReader(c.body))
		if err != nil {
			t.Fatal(err)
		}
		var body errorBody
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != c.status || body.Error != c.wantReason {
			t.Errorf("%.20s: %d %q, want %d %q", c.body, resp.StatusCode, body.Error, c.status, c.wantReason)
		}
	}
}

func (f *fakeAutomations) Zone(context.Context) (string, error) { return "Asia/Shanghai", nil }

func TestTheHostsZoneAndTheZonesOfItsSchedulesReachTheClient(t *testing.T) {
	f := &fakeAutomations{list: []Automation{{ID: "monthly", Name: "Monthly", Schedule: "0 9 1 * *", Timezone: "UTC", Zone: "UTC"}}, paused: map[string]bool{}}
	c := automationsClient(t, f)
	ctx := context.Background()
	if z, err := c.AutomationZone(ctx); err != nil || z != "Asia/Shanghai" {
		t.Fatalf("zone %q %v", z, err)
	}
	if list, err := c.Automations(ctx); err != nil || list[0].Zone != "UTC" {
		t.Fatalf("list %+v %v", list, err)
	}
	factory, err := c.FactoryAutomations(ctx)
	if err != nil || factory[0].LocalSchedule != "0 9 * * *" || factory[0].LocalTimezone != "" || factory[1].LocalSchedule != "0 9 * * *" || factory[1].LocalTimezone != "UTC" ||
		factory[2].NotMovable != "the schedule never ran in Factory" || factory[0].NotMovable != "" {
		t.Fatalf("factory %+v %v", factory, err)
	}
	if _, err := automationsClient(t, nil).AutomationZone(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an older Host: %v", err)
	}
}
