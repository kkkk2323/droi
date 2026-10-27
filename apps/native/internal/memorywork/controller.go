package memorywork

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/memory"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// The Host's side of Memory (ADR 0031): a pass when Memory starts, every
// PassEvery after, and soon after a new Memory Note; Settings → Memory's
// status and Update now.

const (
	// PassEvery is how often the Host looks for Sessions to summarize.
	PassEvery = 15 * time.Minute
	// tick is how often it looks for a new Memory Note.
	tick = time.Minute
)

// Options configure a Controller.
type Options struct {
	MemoryDir string
	// Runner answers nil while there is no Daemon to run a Memory Session on.
	Runner func() Run
	Models func() Models
	// OnChange says something Settings → Memory shows changed.
	OnChange func()
	Log      func(string)
	// Home is where a Memory Session runs without a Workspace on disk.
	Home string
	Now  func() time.Time
}

// Controller runs the Host's Memory work.
type Controller struct {
	o Options

	// passing is held for a whole pass: one at a time.
	passing sync.Mutex

	mu      sync.Mutex
	working bool
	stop    chan struct{}
	// stopped is closed when the running loop ends.
	stopped  chan struct{}
	poke     chan struct{}
	lastPass time.Time
	ctx      context.Context
	cancel   context.CancelFunc
}

// New makes a Controller; nothing runs until Start.
func New(o Options) *Controller {
	if o.OnChange == nil {
		o.OnChange = func() {}
	}
	if o.Log == nil {
		o.Log = func(string) {}
	}
	if o.Models == nil {
		o.Models = func() Models { return Models{} }
	}
	if o.Runner == nil {
		o.Runner = func() Run { return nil }
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Controller{o: o, poke: make(chan struct{}, 1), ctx: ctx, cancel: cancel}
}

// Folder is the Memory folder.
func (c *Controller) Folder() string { return c.o.MemoryDir }

// ResetPrompts copies the shipped prompts into the Memory folder again.
func (c *Controller) ResetPrompts() error { return ResetPrompts(c.o.MemoryDir) }

// ErrNoDaemon: a Memory Session needs the Daemon.
var ErrNoDaemon = errors.New("the Daemon is not running")

func (c *Controller) work(run Run) (Work, error) {
	sessionPrompt, err := ReadPrompt(c.o.MemoryDir, PromptSessionSummary)
	if err != nil {
		return Work{}, err
	}
	memoryPrompt, err := ReadPrompt(c.o.MemoryDir, PromptMemorySummary)
	if err != nil {
		return Work{}, err
	}
	return Work{Dir: c.o.MemoryDir, Run: run, Models: c.o.Models(), SessionPrompt: sessionPrompt, MemoryPrompt: memoryPrompt, Home: c.o.Home, Now: c.o.Now, Log: c.o.Log}, nil
}

// pass runs one pass, after any pass already running.
func (c *Controller) pass(minIdle time.Duration, force bool) error {
	run := c.o.Runner()
	if run == nil {
		return ErrNoDaemon
	}
	c.passing.Lock()
	defer c.passing.Unlock()
	c.mu.Lock()
	c.working = true
	ctx := c.ctx
	c.mu.Unlock()
	c.o.OnChange()
	defer func() {
		c.mu.Lock()
		c.working = false
		c.lastPass = c.o.Now()
		c.mu.Unlock()
		c.o.OnChange()
	}()
	w, err := c.work(run)
	if err != nil {
		return err
	}
	return w.Pass(ctx, minIdle, force)
}

// UpdateNow summarizes the Sessions idle for UpdateNowIdle and updates the
// Memory Summary, even before a failed update's retry time; it returns once
// done.
func (c *Controller) UpdateNow() error {
	err := c.pass(UpdateNowIdle, true)
	if errors.Is(err, ErrNoDaemon) {
		return errors.New(l10n.L("The Daemon is not running."))
	}
	return err
}

// Start runs passes until Stop: one now, then every PassEvery, and one soon
// after a new Memory Note or a Poke.
func (c *Controller) Start() {
	c.mu.Lock()
	if c.stop != nil {
		c.mu.Unlock()
		return
	}
	stop, stopped := make(chan struct{}), make(chan struct{})
	c.stop, c.stopped = stop, stopped
	c.mu.Unlock()
	go func() {
		defer close(stopped)
		passNow := func() {
			if err := c.pass(MinIdle, false); err != nil && !errors.Is(err, ErrNoDaemon) && !errors.Is(err, context.Canceled) {
				c.o.Log("Memory pass: " + err.Error())
			}
		}
		passNow()
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-c.poke:
				passNow()
			case <-ticker.C:
				c.mu.Lock()
				due := c.o.Now().Sub(c.lastPass) >= PassEvery
				c.mu.Unlock()
				if due || c.newNotes() {
					passNow()
				}
			}
		}
	}()
}

// Poke asks for a pass soon, such as when the Daemon is back.
func (c *Controller) Poke() {
	select {
	case c.poke <- struct{}{}:
	default:
	}
}

// newNotes says whether a Memory Note is new to the Memory Summary.
func (c *Controller) newNotes() bool {
	entries, err := os.ReadDir(filepath.Join(c.o.MemoryDir, memory.NotesDir))
	if err != nil {
		return false
	}
	taken := memory.LoadState(c.o.MemoryDir).Phase2.Inputs
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(c.o.MemoryDir, memory.NotesDir, e.Name()))
		if err == nil && taken[memory.NotesDir+"/"+e.Name()] != memory.Hash(b) {
			return true
		}
	}
	return false
}

// Stop cancels the running Memory Sessions and returns once the passes have
// stopped; Start runs them again.
func (c *Controller) Stop() {
	c.mu.Lock()
	stopped := c.stopped
	if c.stop != nil {
		close(c.stop)
		c.stop, c.stopped = nil, nil
	}
	c.cancel()
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.mu.Unlock()
	if stopped != nil {
		<-stopped
	}
}

// Status is Memory as Settings → Memory shows it.
type Status struct {
	// Working: a pass is running.
	Working bool
	// SummaryBytes is the Memory Summary's size, 0 before the first.
	SummaryBytes     int
	SummaryUpdatedAt string
	// Summaries are the Session Summaries, Notes the Memory Notes.
	Summaries, Notes int
	// Waiting are the Sessions that changed since they were summarized.
	Waiting int
	// LastError is the last failed Memory Summary update's, "" since one
	// succeeded.
	LastError, LastErrorAt string
	// Runs are the latest Memory Sessions, newest first.
	Runs []memory.RunRecord
}

// Status reads Memory's state.
func (c *Controller) Status() (Status, error) {
	dir := c.o.MemoryDir
	st := memory.LoadState(dir)
	// Sessions the hook recorded since the last pass wait too.
	started, _ := memory.ReadStarted(dir)
	for _, x := range started {
		if _, known := st.Sessions[x.SessionID]; !known {
			st.Sessions[x.SessionID] = &memory.SessionRecord{Transcript: x.Transcript}
		}
	}
	s := Status{
		SummaryBytes:     len(memory.ReadSummary(dir)),
		SummaryUpdatedAt: st.Phase2.UpdatedAt,
		Waiting:          Waiting(st, c.o.Now()),
		LastError:        st.Phase2.LastError,
		LastErrorAt:      st.Phase2.LastErrorAt,
		Runs:             st.Runs,
	}
	files, err := memory.Files(dir)
	if err != nil {
		return s, err
	}
	for _, f := range files {
		switch {
		case strings.HasPrefix(f.Path, memory.SessionSummariesDir+"/"):
			s.Summaries++
		case strings.HasPrefix(f.Path, memory.NotesDir+"/"):
			s.Notes++
		}
	}
	c.mu.Lock()
	s.Working = c.working
	c.mu.Unlock()
	return s, nil
}
