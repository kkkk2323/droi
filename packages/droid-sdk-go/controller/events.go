package controller

import (
	"encoding/json"
	"sync"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// Event is something that happened on the Controller: one of the types
// below.
type Event interface{ event() }

// StatusChanged: the connection status changed.
type StatusChanged struct{ Status Status }

// SessionNotification is a daemon.session_notification, after the
// Controller applied it. Value is the decoded notification (a pointer to one
// of protocol's *Notification types), nil for a type this module does not
// know.
type SessionNotification struct {
	SessionID string
	Value     any
	Raw       protocol.SessionNotificationParamsNotification
}

// DaemonNotification is any other notification the Daemon sent.
type DaemonNotification struct {
	Method string
	Params json.RawMessage
}

// PermissionRequested: the agent asks to run tools; answer with
// RespondToPermission.
type PermissionRequested struct{ Permission Permission }

// PermissionResolved: a pending permission was answered, here or elsewhere,
// or dropped (SelectedOption empty).
type PermissionResolved struct {
	RequestID      string
	SessionID      string
	SelectedOption protocol.ToolConfirmationOutcome
}

// AskUserRequested: the agent asks the user questions; answer with
// RespondToAskUser.
type AskUserRequested struct{ AskUser AskUser }

// AskUserResolved: a pending AskUser prompt was answered or dropped
// (Result nil).
type AskUserResolved struct {
	RequestID string
	SessionID string
	Result    *protocol.AskUserResult
}

// SessionLoaded: daemon.load_session succeeded.
type SessionLoaded struct {
	SessionID string
	Result    *protocol.LoadSessionResult
}

// SessionInitialized: daemon.initialize_session created a Session.
type SessionInitialized struct {
	SessionID string
	Result    *protocol.InitializeSessionResult
}

// SessionNotFound: the Daemon does not know a Session that was loaded.
type SessionNotFound struct{ SessionID string }

func (StatusChanged) event()       {}
func (SessionNotification) event() {}
func (DaemonNotification) event()  {}
func (PermissionRequested) event() {}
func (PermissionResolved) event()  {}
func (AskUserRequested) event()    {}
func (AskUserResolved) event()     {}
func (SessionLoaded) event()       {}
func (SessionInitialized) event()  {}
func (SessionNotFound) event()     {}

// eventQueue delivers events in order on its own goroutine, so a subscriber
// may call the Controller (and block on the Daemon) without stalling the
// connection's read loop.
type eventQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []Event
	subs   map[int]func(Event)
	next   int
	closed bool
	done   chan struct{}
}

func newEventQueue() *eventQueue {
	q := &eventQueue{subs: map[int]func(Event){}, done: make(chan struct{})}
	q.cond = sync.NewCond(&q.mu)
	go q.run()
	return q
}

func (q *eventQueue) push(e Event) {
	q.mu.Lock()
	if !q.closed {
		q.items = append(q.items, e)
		q.cond.Signal()
	}
	q.mu.Unlock()
}

func (q *eventQueue) subscribe(fn func(Event)) func() {
	q.mu.Lock()
	id := q.next
	q.next++
	q.subs[id] = fn
	q.mu.Unlock()
	return func() {
		q.mu.Lock()
		delete(q.subs, id)
		q.mu.Unlock()
	}
}

// close stops taking events; the ones queued are still delivered.
func (q *eventQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

func (q *eventQueue) run() {
	defer close(q.done)
	for {
		q.mu.Lock()
		for len(q.items) == 0 && !q.closed {
			q.cond.Wait()
		}
		if len(q.items) == 0 {
			q.mu.Unlock()
			return
		}
		e := q.items[0]
		q.items[0] = nil
		q.items = q.items[1:]
		subs := make([]func(Event), 0, len(q.subs))
		for _, fn := range q.subs {
			subs = append(subs, fn)
		}
		q.mu.Unlock()
		for _, fn := range subs {
			fn(e)
		}
	}
}
