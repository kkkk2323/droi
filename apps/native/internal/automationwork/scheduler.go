// Package automationwork runs Droi's own Automations (ADR 0035): once a
// minute it starts a run of each Automation that is due, through a Starter
// that opens the run's Session, and records how the run ended.
package automationwork

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/automations"
	"github.com/kkkk2323/droi/apps/native/internal/keepawake"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// TagName tags a run's Session, with the Automation's id and name, the
// trigger and the run's id as its metadata.
const TagName = "droi.automation"

// catchUpAfter is how late a scheduled time may start and still count as
// on time rather than a catch-up.
const catchUpAfter = 2 * time.Minute

// ErrRunning refuses a run of an Automation while one of it works.
var ErrRunning = errors.New("a run of this automation is already working")

// Run is what a Starter needs to open a run's Session.
type Run struct {
	RunID          string
	AutomationID   string
	AutomationName string
	Trigger        automations.Trigger
	// Title is "[Automation] <name>".
	Title string
	// Cwd is the Automation's working directory, or its own folder.
	Cwd             string
	ModelID         string
	ReasoningEffort protocol.ReasoningEffort
	AutonomyLevel   protocol.AutonomyLevel
	// SkipPermissions is set when the Automation runs without asking.
	SkipPermissions bool
	Tags            []protocol.SessionTag
	Prompt          string
}

// Starter opens a run's Session and sends its prompt. The caller reports the
// end of the Session's turn with Scheduler.Finished.
type Starter interface {
	Start(ctx context.Context, run Run) (sessionID string, err error)
}

// Config is what a Scheduler works with.
type Config struct {
	// Dir holds the Automations, <UserData>/automations.
	Dir     string
	Starter Starter
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// Location reads a schedule without a time zone; time.Local when nil.
	Location *time.Location
	// Hold keeps the computer awake while a run works; keepawake.Hold when
	// nil.
	Hold func() (release func())
	// Changed is called, outside the Scheduler's lock, when an Automation
	// or a run changes.
	Changed func()
	Log     func(string)
}

// Item is an Automation as a list shows it.
type Item struct {
	automations.Automation
	// NextRunAt is the next scheduled time, zero when paused or Invalid; a
	// past one is due and waits for its start.
	NextRunAt time.Time `json:"nextRunAt,omitzero"`
	// LastRun is the newest run, nil before the first.
	LastRun *automations.Run `json:"lastRun,omitempty"`
	Running bool             `json:"running"`
	// Invalid is why its schedule or time zone cannot be read.
	Invalid string `json:"invalid,omitempty"`
}

// Scheduler starts the runs of Droi's Automations and records them.
type Scheduler struct {
	cfg   Config
	store automations.Store

	mu     sync.Mutex
	state  automations.State
	active map[string]*active
}

// active is a run that is starting or working.
type active struct {
	automationID string
	run          automations.Run
	started      bool
	release      func()
	// ended is a Finished that came before Start returned.
	ended *automations.Run
}

// New is a Scheduler over cfg.Dir. A run still recorded as running worked
// when Droi quit: it is recorded as interrupted.
func New(cfg Config) *Scheduler {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Location == nil {
		cfg.Location = time.Local
	}
	if cfg.Hold == nil {
		cfg.Hold = keepawake.Hold
	}
	if cfg.Changed == nil {
		cfg.Changed = func() {}
	}
	if cfg.Log == nil {
		cfg.Log = func(string) {}
	}
	s := &Scheduler{cfg: cfg, store: automations.Store{Dir: cfg.Dir}, active: map[string]*active{}}
	st, err := s.store.State()
	if err != nil {
		s.logf("automations: %v", err)
	}
	s.state = st
	s.interruptLeftovers()
	return s
}

func (s *Scheduler) logf(format string, args ...any) { s.cfg.Log(fmt.Sprintf(format, args...)) }

func (s *Scheduler) interruptLeftovers() {
	list, err := s.store.List()
	if err != nil {
		s.logf("automations: %v", err)
		return
	}
	now := s.cfg.Now()
	for _, a := range list {
		runs, err := s.store.Runs(a.ID)
		if err != nil {
			s.logf("automations: runs of %s: %v", a.ID, err)
			continue
		}
		for _, r := range runs {
			if r.Status != automations.RunRunning {
				continue
			}
			r.Status, r.EndedAt, r.Error = automations.RunInterrupted, now, l10n.N("Droi quit during the run.")
			if err := s.store.SaveRun(a.ID, r); err != nil {
				s.logf("automations: run %s of %s: %v", r.RunID, a.ID, err)
			}
		}
	}
}

// Run ticks at the start of every minute until ctx ends, and once at once.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		s.Tick(ctx)
		now := s.cfg.Now()
		timer := time.NewTimer(now.Truncate(time.Minute).Add(time.Minute).Sub(now))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Scheduler) schedule(a automations.Automation) (automations.Cron, *time.Location, error) {
	cron, err := automations.ParseCron(a.Schedule)
	if err != nil {
		return cron, nil, err
	}
	loc := s.cfg.Location
	if a.Timezone != "" {
		if loc, err = time.LoadLocation(a.Timezone); err != nil {
			return cron, nil, fmt.Errorf("unknown time zone %q", a.Timezone)
		}
	}
	return cron, loc, nil
}

// handled is the time up to which a's scheduled times are handled: the
// last one recorded, else its creation, else (for a folder Droi did not
// write) now, which it records.
func (s *Scheduler) handled(a automations.Automation, now time.Time) (time.Time, bool) {
	if t, ok := s.state.LastScheduled[a.ID]; ok {
		return t, false
	}
	if !a.CreatedAt.IsZero() {
		return a.CreatedAt, false
	}
	s.state.LastScheduled[a.ID] = now
	return now, true
}

func (s *Scheduler) saveState() {
	if err := s.store.SaveState(s.state); err != nil {
		s.logf("automations: %v", err)
	}
}

// Tick starts a run of each active Automation with a scheduled time after
// the last one handled: one run however many times passed, a catch-up when
// the newest is more than two minutes late. It returns once those runs have
// started or failed to; a failed start stays due for the next tick.
func (s *Scheduler) Tick(ctx context.Context) {
	now := s.cfg.Now()
	list, err := s.store.List()
	if err != nil {
		s.logf("automations: %v", err)
		return
	}
	type due struct {
		a   automations.Automation
		act *active
	}
	var starts []due
	dirty := false
	s.mu.Lock()
	for _, a := range list {
		if a.Paused {
			continue
		}
		cron, loc, err := s.schedule(a)
		if err != nil {
			continue
		}
		last, added := s.handled(a, now)
		dirty = dirty || added
		next := cron.Next(last, loc)
		if next.IsZero() || next.After(now) {
			continue
		}
		if act := s.active[a.ID]; act != nil {
			if act.started {
				s.logf("automations: %s is still running; skipped its run of %s", a.ID, next.Format(time.RFC3339))
				s.state.LastScheduled[a.ID] = now
				dirty = true
			}
			continue
		}
		trigger := automations.TriggerSchedule
		if newest := cron.Next(now.Add(-catchUpAfter-time.Nanosecond), loc); newest.IsZero() || newest.After(now) {
			trigger = automations.TriggerCatchUp
		}
		starts = append(starts, due{a, s.begin(a, trigger, now)})
	}
	if dirty {
		s.saveState()
	}
	s.mu.Unlock()
	if len(starts) == 0 {
		return
	}
	s.cfg.Changed()
	var wg sync.WaitGroup
	for _, d := range starts {
		wg.Go(func() {
			if _, err := s.start(ctx, d.a, d.act, now); err != nil {
				s.logf("automations: could not start %s: %v", d.a.ID, err)
			}
		})
	}
	wg.Wait()
}

// RunNow starts a run of the Automation at once, paused or not; refused
// with ErrRunning while one of it works.
func (s *Scheduler) RunNow(ctx context.Context, id string) (automations.Run, error) {
	a, err := s.store.Get(id)
	if err != nil {
		return automations.Run{}, err
	}
	s.mu.Lock()
	if s.active[id] != nil {
		s.mu.Unlock()
		return automations.Run{}, ErrRunning
	}
	act := s.begin(a, automations.TriggerManual, s.cfg.Now())
	s.mu.Unlock()
	s.cfg.Changed()
	return s.start(ctx, a, act, time.Time{})
}

// begin marks a run of a as starting and keeps the computer awake for it;
// s.mu is held.
func (s *Scheduler) begin(a automations.Automation, trigger automations.Trigger, now time.Time) *active {
	act := &active{
		automationID: a.ID,
		run:          automations.Run{RunID: uuid.NewString(), Trigger: trigger, Status: automations.RunRunning, StartedAt: now},
		release:      s.cfg.Hold(),
	}
	s.active[a.ID] = act
	return act
}

// start opens the run's Session. scheduled is the tick's time for a
// scheduled run, recorded as handled once it starts; zero for Run now.
func (s *Scheduler) start(ctx context.Context, a automations.Automation, act *active, scheduled time.Time) (automations.Run, error) {
	sessionID, err := s.cfg.Starter.Start(ctx, s.spec(a, act.run))
	s.mu.Lock()
	if err != nil {
		delete(s.active, a.ID)
		act.release()
		s.mu.Unlock()
		s.cfg.Changed()
		return automations.Run{}, err
	}
	act.run.SessionID, act.started = sessionID, true
	if !scheduled.IsZero() {
		s.state.LastScheduled[a.ID] = scheduled
		s.saveState()
	}
	s.record(act.automationID, act.run)
	run := act.run
	if act.ended != nil {
		s.end(act, *act.ended)
	}
	s.mu.Unlock()
	s.cfg.Changed()
	return run, nil
}

func (s *Scheduler) record(id string, r automations.Run) {
	if err := s.store.SaveRun(id, r); err != nil {
		s.logf("automations: run %s of %s: %v", r.RunID, id, err)
	}
}

func (s *Scheduler) spec(a automations.Automation, r automations.Run) Run {
	cwd := a.WorkingDirectory
	if cwd == "" {
		cwd = s.store.Folder(a.ID)
	}
	return Run{
		RunID:           r.RunID,
		AutomationID:    a.ID,
		AutomationName:  a.Name,
		Trigger:         r.Trigger,
		Title:           "[Automation] " + a.Name,
		Cwd:             cwd,
		ModelID:         a.Model,
		ReasoningEffort: protocol.ReasoningEffort(a.ReasoningEffort),
		AutonomyLevel:   protocol.AutonomyLevel(a.Autonomy),
		SkipPermissions: a.Autonomy == "",
		Tags: []protocol.SessionTag{{Name: TagName, Metadata: map[string]string{
			"automationId": a.ID, "automationName": a.Name, "trigger": string(r.Trigger), "runId": r.RunID,
		}}},
		Prompt: a.Prompt,
	}
}

// Finished records how a run ended (succeeded or failed, with the error's
// text), when its Session's turn ends. A run this Scheduler did not start
// is ignored.
func (s *Scheduler) Finished(runID string, status automations.RunStatus, errText string) {
	s.mu.Lock()
	var act *active
	for _, x := range s.active {
		if x.run.RunID == runID {
			act = x
		}
	}
	if act == nil {
		s.mu.Unlock()
		s.logf("automations: run %s is not running", runID)
		return
	}
	ended := act.run
	ended.Status, ended.EndedAt, ended.Error = status, s.cfg.Now(), errText
	if !act.started {
		act.ended = &ended
		s.mu.Unlock()
		return
	}
	s.end(act, ended)
	s.mu.Unlock()
	s.cfg.Changed()
}

// end records a started run's end and lets the computer sleep; s.mu is held.
func (s *Scheduler) end(act *active, ended automations.Run) {
	ended.SessionID = act.run.SessionID
	act.run = ended
	s.record(act.automationID, ended)
	delete(s.active, act.automationID)
	act.release()
}

// List is every Automation with its next scheduled time, its last run and
// whether one works.
func (s *Scheduler) List() ([]Item, error) {
	list, err := s.store.List()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.cfg.Now()
	out := make([]Item, 0, len(list))
	for _, a := range list {
		out = append(out, s.item(a, now))
	}
	return out, nil
}

// Get is one Automation as List shows it.
func (s *Scheduler) Get(id string) (Item, error) {
	a, err := s.store.Get(id)
	if err != nil {
		return Item{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.item(a, s.cfg.Now()), nil
}

// item is a as a list shows it; s.mu is held.
func (s *Scheduler) item(a automations.Automation, now time.Time) Item {
	it := Item{Automation: a}
	if act := s.active[a.ID]; act != nil {
		run := act.run
		it.Running, it.LastRun = true, &run
	} else if runs, err := s.store.Runs(a.ID); err == nil && len(runs) > 0 {
		it.LastRun = &runs[len(runs)-1]
	}
	cron, loc, err := s.schedule(a)
	if err != nil {
		it.Invalid = err.Error()
		return it
	}
	if !a.Paused {
		last, ok := s.state.LastScheduled[a.ID]
		if !ok {
			last = a.CreatedAt
		}
		if last.IsZero() {
			last = now
		}
		it.NextRunAt = cron.Next(last, loc)
	}
	return it
}

// Runs are an Automation's runs, newest first, at most limit (all when
// limit is 0 or less).
func (s *Scheduler) Runs(id string, limit int) ([]automations.Run, error) {
	runs, err := s.store.Runs(id)
	if err != nil {
		return nil, err
	}
	slices.Reverse(runs)
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}

// Save writes an Automation, creating it when its ID is empty. A new one's
// scheduled times start from its creation; an edited schedule or time zone
// starts from now, so the edit does not start a run of a time already past.
func (s *Scheduler) Save(a automations.Automation) (automations.Automation, error) {
	s.mu.Lock()
	now := s.cfg.Now()
	var old automations.Automation
	if a.ID == "" {
		if a.CreatedAt.IsZero() {
			a.CreatedAt = now
		}
	} else {
		var err error
		if old, err = s.store.Get(a.ID); err != nil {
			s.mu.Unlock()
			return automations.Automation{}, err
		}
	}
	saved, err := s.store.Save(a)
	if err == nil && old.ID != "" && (old.Schedule != saved.Schedule || old.Timezone != saved.Timezone || old.Paused && !saved.Paused) {
		s.state.LastScheduled[saved.ID] = now
		s.saveState()
	}
	s.mu.Unlock()
	if err == nil {
		s.cfg.Changed()
	}
	return saved, err
}

// SetPaused pauses or resumes an Automation; a resumed one does not catch
// up the times it was paused through.
func (s *Scheduler) SetPaused(id string, paused bool) error {
	s.mu.Lock()
	a, err := s.store.Get(id)
	if err == nil {
		err = s.store.SetPaused(id, paused)
	}
	if err == nil && a.Paused && !paused {
		s.state.LastScheduled[id] = s.cfg.Now()
		s.saveState()
	}
	s.mu.Unlock()
	if err == nil {
		s.cfg.Changed()
	}
	return err
}

// Delete removes an Automation and its records. A run of it that works
// goes on, and its end is no longer recorded.
func (s *Scheduler) Delete(id string) error {
	s.mu.Lock()
	err := s.store.Delete(id)
	if err == nil {
		delete(s.state.LastScheduled, id)
		s.saveState()
	}
	s.mu.Unlock()
	if err == nil {
		s.cfg.Changed()
	}
	return err
}
