package workstream

import (
	"slices"
	"sync"
	"time"
)

// changeDelay gathers a burst of changes into one call of the subscribers.
// A var for the tests.
var changeDelay = 100 * time.Millisecond

// startingMax is how long an agent that is being started stays listed: a
// start that never ends, such as one whose Host call was lost, leaves it no
// longer.
const startingMax = 60 * time.Second

// starting is an agent being started: spawn_agent has asked for it and its
// Session is not recorded yet.
type starting struct {
	workstream, title string
	at                time.Time
}

// OnChange calls fn, on a goroutine of the Coordinator's, after a change to
// what List answers: a record saved, a Workstream's Session working, waiting
// or changing its settings, an agent being started. A burst of changes is one
// call. The returned function unsubscribes.
func (c *Coordinator) OnChange(fn func()) (unsubscribe func()) {
	c.subsMu.Lock()
	defer c.subsMu.Unlock()
	id := c.nextSub
	c.nextSub++
	c.subs[id] = fn
	return func() {
		c.subsMu.Lock()
		delete(c.subs, id)
		c.subsMu.Unlock()
	}
}

// changed asks for the subscribers to be called. It never waits, so that it
// can be called under c.mu.
func (c *Coordinator) changed() {
	select {
	case c.dirty <- struct{}{}:
	default:
	}
}

// announce calls the subscribers after each burst of changes, until Close.
func (c *Coordinator) announce() {
	for {
		select {
		case <-c.life.Done():
			return
		case <-c.dirty:
		}
		select {
		case <-c.life.Done():
			return
		case <-time.After(changeDelay):
		}
		// A change from here on asks for the next call.
		select {
		case <-c.dirty:
		default:
		}
		c.subsMu.Lock()
		fns := make([]func(), 0, len(c.subs))
		for _, fn := range c.subs {
			fns = append(fns, fn)
		}
		c.subsMu.Unlock()
		for _, fn := range fns {
			fn()
		}
	}
}

// startAgent lists an agent of workstream as being started until done is
// called, which may be more than once.
func (c *Coordinator) startAgent(workstream, title string) (done func()) {
	s := &starting{workstream: workstream, title: title, at: c.cfg.Now()}
	c.mu.Lock()
	c.starting = append(c.starting, s)
	c.mu.Unlock()
	c.changed()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			c.starting = slices.DeleteFunc(c.starting, func(o *starting) bool { return o == s })
			c.mu.Unlock()
			c.changed()
		})
	}
}

// memberLocked reports that sessionID is the Session of an active
// Workstream's orchestrator or agent; under c.mu.
func (c *Coordinator) memberLocked(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	for _, w := range c.all {
		if w.ArchivedAt != "" {
			continue
		}
		if w.OrchestratorID == sessionID {
			return true
		}
		for _, a := range w.Agents {
			if a.ArchivedAt == "" && a.SessionID == sessionID {
				return true
			}
		}
	}
	return false
}
