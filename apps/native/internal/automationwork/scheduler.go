// Package automationwork runs Droi's own Automations (ADR 0035): once a
// minute it starts a run of each Automation that is due, through a Starter
// that opens the run's Session, and records how the run ended.
package automationwork

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/automations"
	"github.com/kkkk2323/droi/apps/native/internal/keepawake"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// TagName tags a run's Session, with the Automation's id and name, the
// trigger and the run's id as its metadata.
const TagName = sessions.DroiAutomationTag

// FactoryAutomationTools are the llm ids of Factory's own Automation tools.
// An Automation they make syncs to Factory, so Droi's Sessions leave them
// out (disabledToolIds).
var FactoryAutomationTools = []string{
	"CreateAutomation", "ListAutomations", "ReadAutomation", "EditAutomation",
	"DeleteAutomation", "RunAutomation", "ListAutomationTemplates",
}

// workstreamTools is Droi's Workstream server, for a Workstream's Sessions
// only.
const workstreamTools = "mcp:droi-workstream"

// RunHiddenTools are the tools a run's Session leaves out: Factory's
// Automation tools, Droi's Workstream tools, and Droi's Automation tools,
// with which a run could rewrite what an Automation that runs without
// asking does.
func RunHiddenTools() []string {
	return slices.Concat(FactoryAutomationTools, []string{workstreamTools, "mcp:" + ServerName})
}

// halfStartTries is how many starts in a row of a scheduled run may end in
// a HalfStartedError before the run fails with the last one's error.
const halfStartTries = 3

// catchUpAfter is how late a scheduled time may start and still count as
// on time rather than a catch-up.
const catchUpAfter = 2 * time.Minute

// ErrRunning refuses a run of an Automation while one of it works.
var ErrRunning = errors.New("a run of this automation is already working")

// ErrUnavailable is a start that could not reach the Daemon or timed out.
// The run stays due and is tried again at the next tick; nothing is
// recorded.
var ErrUnavailable = errors.New("the Daemon is not available")

// ErrCopy is an Adopt that could not copy the folder it was given; its
// text names the paths.
var ErrCopy = errors.New("could not copy the automation's folder")

// HalfStartedError is an unavailable start (it is ErrUnavailable) that got
// as far as opening the run's Session, which the Starter closed again,
// before the Daemon failed to answer. Each try opens a Session, so a
// scheduled run is tried halfStartTries times, then fails with Err.
type HalfStartedError struct{ Err error }

func (e *HalfStartedError) Error() string   { return e.Err.Error() }
func (e *HalfStartedError) Unwrap() []error { return []error{ErrUnavailable, e.Err} }

// lost is the Error of a run the Starter abandoned.
var lost = l10n.N("Droi lost touch with the Daemon during the run.")

// RunSpec is what a Starter needs to open a run's Session.
type RunSpec struct {
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
	// DisabledTools are the tools the run's Session leaves out: Factory's
	// Automation tools and Droi's Workstream tools.
	DisabledTools []string
	Prompt        string
}

// Starter opens a run's Session and sends its prompt. The caller reports the
// end of the Session's turn with Scheduler.Finished.
//
// A Starter that is also an Abandoner is asked at every tick about each
// started run whose end has not come.
//
// A Start that fails must leave no Session behind. It wraps ErrUnavailable
// when the Daemon cannot be reached, so the run is tried again a minute
// later; any other error is recorded as a failed run and the scheduled
// time is not tried again. A Start that outlives its context's deadline
// counts as unavailable.
type Starter interface {
	Start(ctx context.Context, run RunSpec) (sessionID string, err error)
}

// Abandoner gives up the runs whose end will not come.
type Abandoner interface {
	// Abandon reports that the run's Session is no longer followed and
	// its end will not be reported, and forgets the run; the Scheduler
	// records it as interrupted.
	Abandon(run automations.Run) bool
}

// Config is what a Scheduler works with.
type Config struct {
	// Dir holds the Automations, <UserData>/automations.
	Dir     string
	Starter Starter
	// StartTimeout bounds a Starter's Start; 90 seconds when zero, so that
	// Run now answers within the Gateway's 100 seconds.
	StartTimeout time.Duration
	// HiddenTools, read at every start, are the tools a run's Session
	// leaves out; RunHiddenTools when nil.
	HiddenTools func() []string
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

// Item is an Automation as a list shows it; Invalid also says why a
// schedule never runs.
type Item struct {
	automations.Automation
	// NextRunAt is the next scheduled time, zero when paused or Invalid; a
	// past one is due and waits for its start.
	NextRunAt time.Time `json:"nextRunAt,omitzero"`
	// LastRun is the newest run, nil before the first.
	LastRun *automations.Run `json:"lastRun,omitempty"`
	Running bool             `json:"running"`
}

// Scheduler starts the runs of Droi's Automations and records them. Its
// lock covers every read and write of the folder, which Windows will not
// replace while it is open.
type Scheduler struct {
	cfg   Config
	store automations.Store

	mu     sync.Mutex
	state  automations.State
	active map[string]*active
	// orphans are the runs of deleted Automations, by run id, until they
	// end; their end is not recorded.
	orphans map[string]*active
	logs    []string
	// halfStarts count the HalfStartedErrors in a row of a scheduled run,
	// by Automation.
	halfStarts map[string]halfStarts
}

type halfStarts struct {
	due time.Time
	n   int
}

// active is a run that is starting or working.
type active struct {
	run     automations.Run
	started bool
	deleted bool
	release func()
	// ended is a Finished that came before Start returned.
	ended *automations.Run
	// due is the scheduled time a scheduled run is for.
	due time.Time
}

// New is a Scheduler over cfg.Dir. A run still recorded as running worked
// when Droi quit: it is recorded as interrupted.
func New(cfg Config) *Scheduler {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.StartTimeout <= 0 {
		cfg.StartTimeout = 90 * time.Second
	}
	if cfg.HiddenTools == nil {
		cfg.HiddenTools = RunHiddenTools
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
	s := &Scheduler{cfg: cfg, store: automations.Store{Dir: cfg.Dir}, active: map[string]*active{}, orphans: map[string]*active{}, halfStarts: map[string]halfStarts{}}
	s.mu.Lock()
	st, err := s.store.State()
	if err != nil {
		s.logf("automations: %v", err)
	}
	s.state = st
	s.removeCutShort()
	s.interruptLeftovers()
	s.unlock(false)
	return s
}

// logf logs once s.mu is unlocked; s.mu is held.
func (s *Scheduler) logf(format string, args ...any) {
	s.logs = append(s.logs, fmt.Sprintf(format, args...))
}

// unlock unlocks s.mu, then calls the releases, logs what was logged
// under it, and calls Changed when changed.
func (s *Scheduler) unlock(changed bool, releases ...func()) {
	logs := s.logs
	s.logs = nil
	s.mu.Unlock()
	for _, release := range releases {
		if release != nil {
			release()
		}
	}
	for _, l := range logs {
		s.cfg.Log(l)
	}
	if changed {
		s.cfg.Changed()
	}
}

// adoptPrefix starts the name of a copy Adopt prepares, which the Store
// lists as no Automation for its leading dot.
const adoptPrefix = ".adopt-"

// removeCutShort removes the copies an Adopt left when Droi quit during it;
// s.mu is held, and the caller holds the folder's lock.
func (s *Scheduler) removeCutShort() {
	entries, err := os.ReadDir(s.cfg.Dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), adoptPrefix) {
			if err := os.RemoveAll(filepath.Join(s.cfg.Dir, e.Name())); err != nil {
				s.logf("automations: %v", err)
			}
		}
	}
}

// interruptLeftovers; s.mu is held.
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
			s.record(r)
		}
	}
}

// Run ticks at the start of every minute until ctx ends, and once at once.
// A start that takes long does not hold up the next tick.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		s.tick(ctx)
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
// write) now, which it records; s.mu is held.
func (s *Scheduler) handled(a automations.Automation, now time.Time) (time.Time, bool) {
	if t, ok := s.state.LastScheduled[a.ID]; ok {
		return t, false
	}
	if !a.CreatedAt.IsZero() {
		return a.CreatedAt, false
	}
	return now, s.markHandled(a.ID, now)
}

// markHandled moves an Automation's handled time to t, never back; s.mu is
// held. It reports whether the state changed.
func (s *Scheduler) markHandled(id string, t time.Time) bool {
	if old, ok := s.state.LastScheduled[id]; ok && !t.After(old) {
		return false
	}
	s.state.LastScheduled[id] = t
	return true
}

// saveState; s.mu is held.
func (s *Scheduler) saveState() {
	if err := s.store.SaveState(s.state); err != nil {
		s.logf("automations: %v", err)
	}
}

// Tick records as interrupted the runs the Starter abandons, then starts a
// run of each active Automation with a scheduled time after the last one
// handled: one run however many times passed, a catch-up when the newest is
// more than two minutes late. It returns once those runs have started or
// failed to; an unavailable Daemon keeps them due for the next tick.
func (s *Scheduler) Tick(ctx context.Context) { s.tick(ctx).Wait() }

// abandon records the runs the Starter gives up as interrupted.
func (s *Scheduler) abandon() {
	ab, ok := s.cfg.Starter.(Abandoner)
	if !ok {
		return
	}
	for _, r := range s.Active() {
		if ab.Abandon(r) {
			s.Finished(r.RunID, automations.RunInterrupted, lost)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) *sync.WaitGroup {
	s.abandon()
	var wg sync.WaitGroup
	now := s.cfg.Now()
	type due struct {
		a   automations.Automation
		act *active
	}
	var starts []due
	dirty := false
	s.mu.Lock()
	list, err := s.store.List()
	if err != nil {
		s.logf("automations: %v", err)
	}
	for _, a := range list {
		if a.Paused || a.Invalid != "" {
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
				dirty = s.markHandled(a.ID, now) || dirty
			}
			continue
		}
		trigger := automations.TriggerSchedule
		if newest := cron.Next(now.Add(-catchUpAfter-time.Nanosecond), loc); newest.IsZero() || newest.After(now) {
			trigger = automations.TriggerCatchUp
		}
		act := s.begin(a, trigger, now)
		act.due = next
		starts = append(starts, due{a, act})
	}
	if dirty {
		s.saveState()
	}
	s.unlock(len(starts) > 0)
	for _, d := range starts {
		wg.Go(func() { _, _ = s.start(ctx, d.a, d.act, now) })
	}
	return &wg
}

// RunNow starts a run of the Automation at once, paused or not; refused
// with ErrRunning while one of it works. A start that fails other than
// with ErrUnavailable is recorded as a failed run, which it returns with
// the error.
func (s *Scheduler) RunNow(ctx context.Context, id string) (automations.Run, error) {
	s.mu.Lock()
	a, err := s.store.Get(id)
	if err != nil {
		s.unlock(false)
		return automations.Run{}, err
	}
	if s.active[id] != nil {
		s.unlock(false)
		return automations.Run{}, ErrRunning
	}
	act := s.begin(a, automations.TriggerManual, s.cfg.Now())
	s.unlock(true)
	return s.start(ctx, a, act, time.Time{})
}

// begin marks a run of a as starting; s.mu is held.
func (s *Scheduler) begin(a automations.Automation, trigger automations.Trigger, now time.Time) *active {
	act := &active{run: automations.Run{
		RunID: uuid.NewString(), AutomationID: a.ID, Trigger: trigger, Status: automations.RunRunning, StartedAt: now,
	}}
	s.active[a.ID] = act
	return act
}

// drop forgets a run; s.mu is held.
func (s *Scheduler) drop(act *active) {
	if act.deleted {
		delete(s.orphans, act.run.RunID)
	} else if s.active[act.run.AutomationID] == act {
		delete(s.active, act.run.AutomationID)
	}
}

// start opens the run's Session. scheduled is the tick's time for a
// scheduled run, recorded as handled once the run starts or fails for
// good; zero for Run now.
func (s *Scheduler) start(ctx context.Context, a automations.Automation, act *active, scheduled time.Time) (automations.Run, error) {
	startCtx, cancel := context.WithTimeout(ctx, s.cfg.StartTimeout)
	sessionID, err := s.cfg.Starter.Start(startCtx, s.spec(a, act.run))
	if err != nil && startCtx.Err() != nil && !errors.Is(err, ErrUnavailable) {
		err = fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	cancel()
	var release func()
	if err == nil {
		release = s.cfg.Hold()
	}
	s.mu.Lock()
	id := act.run.AutomationID
	var half *HalfStartedError
	if !errors.As(err, &half) || act.due.IsZero() {
		delete(s.halfStarts, id)
	} else if h := s.halfStarts[id]; h.due.Equal(act.due) && h.n+1 >= halfStartTries {
		delete(s.halfStarts, id)
		err = half.Err
	} else {
		if !h.due.Equal(act.due) {
			h = halfStarts{due: act.due}
		}
		h.n++
		s.halfStarts[id] = h
	}
	if err != nil {
		s.drop(act)
		if errors.Is(err, ErrUnavailable) {
			s.logf("automations: could not start %s, trying again in a minute: %v", id, err)
			s.unlock(true)
			return automations.Run{}, err
		}
		s.logf("automations: could not start %s: %v", id, err)
		failed := act.run
		failed.Status, failed.EndedAt, failed.Error = automations.RunFailed, s.cfg.Now(), err.Error()
		if !act.deleted {
			if !scheduled.IsZero() && s.markHandled(id, scheduled) {
				s.saveState()
			}
			s.record(failed)
		}
		s.unlock(true)
		return failed, err
	}
	act.run.SessionID, act.started, act.release = sessionID, true, release
	if !act.deleted {
		if !scheduled.IsZero() && s.markHandled(id, scheduled) {
			s.saveState()
		}
		s.record(act.run)
	}
	run := act.run
	var releases []func()
	if act.ended != nil {
		releases = append(releases, s.end(act, *act.ended))
	}
	s.unlock(true, releases...)
	return run, nil
}

// record writes a run's record; s.mu is held.
func (s *Scheduler) record(r automations.Run) {
	if err := s.store.SaveRun(r.AutomationID, r); err != nil {
		s.logf("automations: run %s of %s: %v", r.RunID, r.AutomationID, err)
	}
}

func (s *Scheduler) spec(a automations.Automation, r automations.Run) RunSpec {
	cwd := a.WorkingDirectory
	if cwd == "" {
		cwd = s.store.Folder(a.ID)
	}
	return RunSpec{
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
		DisabledTools: s.cfg.HiddenTools(),
		Prompt:        a.Prompt,
	}
}

// find is the starting or working run with runID; s.mu is held.
func (s *Scheduler) find(runID string) *active {
	if act := s.orphans[runID]; act != nil {
		return act
	}
	for _, act := range s.active {
		if act.run.RunID == runID {
			return act
		}
	}
	return nil
}

// Finished records how a run ended, when its Session's turn ends:
// succeeded, or failed with the error's text. The caller also calls it with
// RunInterrupted for a run of Active whose Session it finds gone or idle
// (after the Daemon restarted, say), since no turn end will come for it.
// The end of a run whose Automation was deleted is not recorded; a run
// this Scheduler does not know is ignored.
func (s *Scheduler) Finished(runID string, status automations.RunStatus, errText string) {
	s.mu.Lock()
	act := s.find(runID)
	if act == nil {
		s.logf("automations: run %s is not running", runID)
		s.unlock(false)
		return
	}
	ended := act.run
	ended.Status, ended.EndedAt, ended.Error = status, s.cfg.Now(), errText
	if !act.started {
		act.ended = &ended
		s.unlock(false)
		return
	}
	release := s.end(act, ended)
	s.unlock(true, release)
}

// end records a started run's end and forgets it; s.mu is held. The caller
// calls the release it returns once s.mu is unlocked.
func (s *Scheduler) end(act *active, ended automations.Run) func() {
	ended.SessionID = act.run.SessionID
	act.run = ended
	if !act.deleted {
		s.record(ended)
	}
	s.drop(act)
	return act.release
}

// Active are the runs whose Session is open and whose end has not come,
// with their Automation and Session ids, those of deleted Automations
// included. After the Daemon restarts, the caller checks their Sessions
// and calls Finished for each that no longer works.
func (s *Scheduler) Active() []automations.Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []automations.Run
	for _, m := range []map[string]*active{s.active, s.orphans} {
		for _, act := range m {
			if act.started {
				out = append(out, act.run)
			}
		}
	}
	slices.SortFunc(out, func(a, b automations.Run) int { return a.StartedAt.Compare(b.StartedAt) })
	return out
}

// List is every Automation with its next scheduled time, its last run and
// whether one works.
func (s *Scheduler) List() ([]Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.store.List()
	if err != nil {
		return nil, err
	}
	now := s.cfg.Now()
	out := make([]Item, 0, len(list))
	for _, a := range list {
		out = append(out, s.item(a, now))
	}
	return out, nil
}

// Get is one Automation as List shows it.
func (s *Scheduler) Get(id string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.store.Get(id)
	if err != nil {
		return Item{}, err
	}
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
	if a.Invalid != "" {
		return it
	}
	cron, loc, err := s.schedule(a)
	if err != nil {
		it.Invalid = err.Error()
		return it
	}
	if cron.Next(now, loc).IsZero() {
		it.Invalid = "the schedule never runs"
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

// Folder is an Automation's own folder, its runs' Workspace when it has
// no working directory.
func (s *Scheduler) Folder(id string) string { return s.store.Folder(id) }

// Runs are an Automation's runs, newest first, at most limit (all when
// limit is 0 or less).
func (s *Scheduler) Runs(id string, limit int) ([]automations.Run, error) {
	s.mu.Lock()
	runs, err := s.store.Runs(id)
	s.mu.Unlock()
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
		// Saving over an unreadable one repairs it.
		if old, err = s.store.Get(a.ID); err != nil && !errors.Is(err, automations.ErrUnreadable) {
			s.unlock(false)
			return automations.Automation{}, err
		}
	}
	saved, err := s.store.Save(a)
	if err == nil && old.ID != "" && (old.Schedule != saved.Schedule || old.Timezone != saved.Timezone || old.Paused && !saved.Paused) {
		if s.markHandled(saved.ID, now) {
			s.saveState()
		}
	}
	s.unlock(err == nil)
	return saved, err
}

// Adopt creates a, without an ID, in a copy of the folder src: its files
// become the new Automation's own, its HEARTBEAT.md a's
// (automations.CopyFactoryFolder). Its scheduled times start from its
// creation.
func (s *Scheduler) Adopt(a automations.Automation, src string) (automations.Automation, error) {
	s.mu.Lock()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = s.cfg.Now()
	}
	saved, err := s.adopt(a, src)
	s.unlock(err == nil)
	return saved, err
}

// adopt; s.mu is held.
func (s *Scheduler) adopt(a automations.Automation, src string) (automations.Automation, error) {
	if err := automations.Validate(a); err != nil {
		return automations.Automation{}, err
	}
	if err := os.MkdirAll(s.cfg.Dir, 0o755); err != nil {
		return automations.Automation{}, err
	}
	// A name with a leading dot is no Automation's, so a copy cut short
	// is never listed.
	tmp := filepath.Join(s.cfg.Dir, adoptPrefix+uuid.NewString())
	if err := automations.CopyFactoryFolder(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return automations.Automation{}, fmt.Errorf("%w: %w", ErrCopy, err)
	}
	saved, err := s.store.Adopt(a, tmp)
	if err != nil {
		_ = os.RemoveAll(tmp)
	}
	return saved, err
}

// ForkedFrom is the Automation moved from the Factory automation key
// names (Automation.ForkedFrom), if there is one.
func (s *Scheduler) ForkedFrom(key string) (Item, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.store.List()
	if err != nil {
		return Item{}, false, err
	}
	for _, a := range list {
		if a.ForkedFrom == key {
			return s.item(a, s.cfg.Now()), true, nil
		}
	}
	return Item{}, false, nil
}

// SetPaused pauses or resumes an Automation; a resumed one does not catch
// up the times it was paused through.
func (s *Scheduler) SetPaused(id string, paused bool) error {
	s.mu.Lock()
	a, err := s.store.Get(id)
	if err == nil {
		err = s.store.SetPaused(id, paused)
	}
	if err == nil && a.Paused && !paused && s.markHandled(id, s.cfg.Now()) {
		s.saveState()
	}
	s.unlock(err == nil)
	return err
}

// Delete removes an Automation and its records, also one whose
// HEARTBEAT.md cannot be read. A run of it that works goes on until
// Finished, which records nothing; a new Automation with the same id
// starts clean.
func (s *Scheduler) Delete(id string) error {
	s.mu.Lock()
	err := s.store.Delete(id)
	if err == nil {
		if act := s.active[id]; act != nil {
			act.deleted = true
			delete(s.active, id)
			s.orphans[act.run.RunID] = act
		}
		delete(s.state.LastScheduled, id)
		delete(s.halfStarts, id)
		s.saveState()
	}
	s.unlock(err == nil)
	return err
}
