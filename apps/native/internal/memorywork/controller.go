package memorywork

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/memory"
)

// The Host's side of Memory: what Settings → Memory shows, the manual
// consolidation, and the automatic extraction the hooks ask for through
// request files (ADR 0011).

// PollInterval is how often WatchRequests looks for request files.
const PollInterval = time.Second

// Options configure a Controller.
type Options struct {
	MemoryDir string
	// Runner answers nil while there is no Daemon to run a Memory Session on.
	Runner  func() Run
	ModelID func() string
	// OnChange says something Settings → Memory shows changed.
	OnChange func()
	Log      func(string)
	// Home is where a Memory Session without a Workspace on disk runs; the
	// user's home folder when "".
	Home string
}

// Controller runs the Host's Memory work.
type Controller struct {
	o Options

	mu            sync.Mutex
	store         *memory.Store
	consolidating map[string]bool
	ctx           context.Context
	cancel        context.CancelFunc
	// drained is closed when the running drain loop ends; nil while none runs.
	drained   chan struct{}
	again     bool
	stopWatch chan struct{}
}

// New makes a Controller; nothing is opened until it is first used.
func New(o Options) *Controller {
	if o.OnChange == nil {
		o.OnChange = func() {}
	}
	if o.Log == nil {
		o.Log = func(string) {}
	}
	if o.ModelID == nil {
		o.ModelID = func() string { return "" }
	}
	if o.Runner == nil {
		o.Runner = func() Run { return nil }
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Controller{o: o, consolidating: map[string]bool{}, ctx: ctx, cancel: cancel}
}

// Folder is the Memory folder.
func (c *Controller) Folder() string { return c.o.MemoryDir }

func (c *Controller) open() (*memory.Store, context.Context, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.store == nil {
		s, err := memory.OpenStore(c.o.MemoryDir)
		if err != nil {
			return nil, nil, err
		}
		// Best effort: what is not merged now is tried again on the next launch.
		_ = s.MergeWorktrees()
		c.store = s
	}
	return c.store, c.ctx, nil
}

func (c *Controller) work(prompt PromptName, run Run) (Work, context.Context, error) {
	store, ctx, err := c.open()
	if err != nil {
		return Work{}, nil, err
	}
	text, err := ReadPrompt(c.o.MemoryDir, prompt)
	if err != nil {
		return Work{}, nil, err
	}
	return Work{Store: store, Run: run, ModelID: c.o.ModelID(), Prompt: text, Home: c.o.Home}, ctx, nil
}

// Overview is every Memory on this computer, as Settings → Memory shows it.
func (c *Controller) Overview() (Overview, error) {
	store, _, err := c.open()
	if err != nil {
		return Overview{}, err
	}
	summaries, err := store.Summaries()
	if err != nil {
		return Overview{}, err
	}
	rows := make([]Row, 0, len(summaries))
	for _, s := range summaries {
		slot := memory.GlobalSlot
		if s.Scope == memory.ScopeProject {
			slot = memory.Slot{Scope: memory.ScopeProject, Workspace: s.Workspace}
		}
		usage, err := store.Usage(slot)
		if err != nil {
			return Overview{}, err
		}
		c.mu.Lock()
		busy := c.consolidating[s.Workspace]
		c.mu.Unlock()
		rows = append(rows, Row{
			Workspace:        s.Workspace,
			Entries:          s.Entries,
			Chars:            s.Chars,
			SoftLimit:        memory.Limits[s.Scope].Soft,
			OverSoftLimit:    s.OverSoftLimit,
			LastConsolidated: s.LastConsolidated,
			Consolidating:    busy,
			Searches:         usage.Searches,
			EmptySearches:    usage.EmptySearches,
			NeverFound:       usage.NeverFound,
		})
	}
	since, err := store.LoggedSince()
	if err != nil {
		return Overview{}, err
	}
	return Overview{Rows: rows, LoggedSince: since}, nil
}

// Consolidate consolidates one Memory, the Global Memory when workspace is
// "", and blocks until it is done.
func (c *Controller) Consolidate(workspace string) (Result, error) {
	run := c.o.Runner()
	if run == nil {
		return Result{}, errors.New("The Daemon is not running.")
	}
	c.mu.Lock()
	if c.consolidating[workspace] {
		c.mu.Unlock()
		return Result{}, errors.New("This Memory is already being consolidated.")
	}
	c.consolidating[workspace] = true
	c.mu.Unlock()
	slot := memory.GlobalSlot
	if workspace != "" {
		slot = memory.ProjectSlot(workspace)
	}
	c.o.OnChange()
	defer func() {
		c.mu.Lock()
		delete(c.consolidating, workspace)
		c.mu.Unlock()
		c.o.OnChange()
	}()
	w, ctx, err := c.work(PromptConsolidation, run)
	if err != nil {
		return Result{}, err
	}
	outcomes, err := Consolidate(ctx, w, slot)
	if err != nil {
		return Result{}, err
	}
	var result Result
	for _, o := range outcomes {
		if o.OK {
			result.Applied++
		} else {
			result.Rejected++
			c.o.Log(fmt.Sprintf("consolidation of %s rejected: %s", o.Category, o.Reason))
		}
	}
	return result, nil
}

// ResetPrompts copies the shipped prompts into the Memory folder again.
func (c *Controller) ResetPrompts() error { return ResetPrompts(c.o.MemoryDir) }

func (c *Controller) drain() {
	run := c.o.Runner()
	if run == nil {
		return
	}
	files, err := memory.ExtractionRequestFiles(c.o.MemoryDir)
	if err != nil {
		return
	}
	for _, file := range files {
		request, err := memory.ReadExtractionRequest(file)
		if err != nil {
			_ = os.Remove(file)
			continue
		}
		w, ctx, err := c.work(PromptExtraction, run)
		added, refused := 0, 0
		if err == nil {
			added, refused, err = Extract(ctx, w, request)
		}
		// One attempt only: a request that keeps failing must not loop forever.
		if err != nil {
			c.o.Log(fmt.Sprintf("extraction from %s failed: %s", request.SessionID, err))
		} else {
			c.o.Log(fmt.Sprintf("extracted %d entries (%d refused) from %s", added, refused, request.SessionID))
			if added > 0 {
				c.o.OnChange()
			}
		}
		_ = os.Remove(file)
	}
}

// ProcessRequests handles the waiting extraction requests, e.g. when the
// Daemon is back, and returns once they are done. A call while requests are
// being handled makes that run look again and waits for it.
func (c *Controller) ProcessRequests() {
	c.mu.Lock()
	if c.drained != nil {
		c.again = true
		done := c.drained
		c.mu.Unlock()
		<-done
		return
	}
	done := make(chan struct{})
	c.drained = done
	c.mu.Unlock()
	defer close(done)
	for {
		c.mu.Lock()
		c.again = false
		c.mu.Unlock()
		c.drain()
		c.mu.Lock()
		if !c.again {
			c.drained = nil
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
	}
}

// WatchRequests handles the extraction requests waiting now and every one
// that arrives until Stop, looking for them every PollInterval.
func (c *Controller) WatchRequests() {
	c.mu.Lock()
	if c.stopWatch != nil {
		c.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	c.stopWatch = stop
	c.mu.Unlock()
	_ = os.MkdirAll(filepath.Join(c.o.MemoryDir, memory.RequestsFolder), 0o777)
	go func() {
		c.ProcessRequests()
		ticker := time.NewTicker(PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if files, _ := memory.ExtractionRequestFiles(c.o.MemoryDir); len(files) > 0 {
					c.ProcessRequests()
				}
			}
		}
	}()
}

// Stop stops watching for requests, cancels the running Memory Sessions and
// closes the store; a later call opens it again.
func (c *Controller) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopWatch != nil {
		close(c.stopWatch)
		c.stopWatch = nil
	}
	c.cancel()
	c.ctx, c.cancel = context.WithCancel(context.Background())
	if c.store != nil {
		_ = c.store.Close()
		c.store = nil
	}
}
