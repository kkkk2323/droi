package sessionview

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// A View draws a conversation; it does not keep one. Kernel pushes a whole view after every commit — that is what
// subscribing to "view" means — and this file holds exactly what a window needs of it: the window of entries, the
// run, the queued input, the usage, and the failure of a read. There is no state machine here: every push replaces
// what it names, and nothing is inferred from what is missing.
//
// The old path did the opposite. droid-sdk-go's session store folded 25 kinds of notification into a session —
// messages assembled from text deltas, working state from a string, usage from a counter — and Droi's window read
// that. The Kernel hands over the answer instead of the ingredients.

// Kernel is the conversation half of the client: the calls a View makes. It is an interface so a test can answer
// them, and so this package does not depend on how a client is dialled.
type Kernel interface {
	Open(ctx context.Context, p kernel.OpenParams) (kernel.OpenResult, error)
	Unwatch(ctx context.Context, id kernel.ID) error
	Submit(ctx context.Context, p kernel.SubmitParams) (kernel.SubmitResult, error)
	Abort(ctx context.Context, p kernel.AbortParams) error
	AbortSubmission(ctx context.Context, p kernel.AbortSubmissionParams) (kernel.Status, error)
	Configure(ctx context.Context, p kernel.ConfigureParams) (kernel.ConfigureResult, error)
	Entries(ctx context.Context, p kernel.EntriesParams) (kernel.EntriesResult, error)
	Usage(ctx context.Context, id kernel.ID) (kernel.Usage, error)
	Processes(ctx context.Context, id kernel.ID) (kernel.ProcessesResult, error)
	StopProcess(ctx context.Context, p kernel.StopProcessParams) (kernel.Status, error)
}

// State is one conversation as the window draws it: the view Kernel last pushed, and the few documents it carries.
//
// It is written on the UI thread only — the app's pump hands each push over there — so it needs no lock, and a
// reader never sees half of one.
type State struct {
	// ID is the conversation's Kernel id.
	ID kernel.ID
	// Revision counts every change a View must draw again: a new view, a run starting or ending, new usage.
	Revision int64
	// LoadErr is Kernel's sentence for a view it could not read, cleared by the next one it can.
	LoadErr string
	// Run is pi.live's run, or nil when nothing runs: a conversation is busy exactly while this names one.
	Run json.RawMessage
	// Failure is why the run that ended failed, or nil.
	Failure json.RawMessage
	// Inbox is pi.inbox: the input waiting for the turn in progress.
	Inbox json.RawMessage

	view     kernel.View
	opened   bool
	retired  bool
	usageDoc json.RawMessage
	usage    kernel.Usage
}

// NewState makes the state of conversation id, empty until a view arrives.
func NewState(id kernel.ID) *State { return &State{ID: id} }

// Opened reports whether a view arrived: a conversation nobody opened has nothing to draw.
func (s *State) Opened() bool { return s.opened }

// View is the window of entries Kernel last pushed, and the documents with it.
func (s *State) View() kernel.View { return s.view }

// Entries are the window's entries, oldest first.
func (s *State) Entries() []kernel.Entry { return s.view.Entries }

// Before is the cursor for the page before this window, or nil at the conversation's start.
func (s *State) Before() json.RawMessage { return s.view.Before }

// Busy reports whether a run is in progress: one is exactly while pi.live names one.
func (s *State) Busy() bool { return namesARun(s.Run) }

// Retired reports that Kernel ended this conversation's watch, so the window must open it again to keep hearing
// about it.
func (s *State) Retired() bool { return s.retired }

// UsageDocument is the view's pi.usage: every model and tool the conversation used, with its counters.
func (s *State) UsageDocument() json.RawMessage { return s.usageDoc }

// Usage is what conversation.usage last answered: the same counters by model and tool, plus how full the context
// is.
func (s *State) Usage() kernel.Usage { return s.usage }

// UsageContext is conversation.usage's context: {used, limit, compactionAt}. The composer's meter reads it.
func (s *State) UsageContext() json.RawMessage { return s.usage.Context }

// Apply takes a conversation.view push and reports whether anything changed.
//
// A whole view replaces the state, docs and all: a push is a snapshot of what Kernel reads, so keeping pieces of
// the previous one would be guessing. A failed read keeps the entries the window shows — a transient failure must
// not blank a transcript — and records Kernel's sentence. A delta push has no value, and a window subscriber that
// receives one is misconfigured: the state changes nothing rather than applying ops it never asked for.
func (s *State) Apply(n kernel.ViewNotification) bool {
	switch {
	case n.Failed():
		message := messageOf(n.Error)
		if message == "" {
			message = "the conversation could not be read"
		}
		if s.LoadErr == message {
			return false
		}
		s.LoadErr = message
		s.Revision++
		return true
	case n.Value == nil:
		return false
	}
	s.view = *n.Value
	s.opened = true
	s.LoadErr = ""
	s.retired = n.Retired
	s.readDocs()
	s.Revision++
	return true
}

// ApplyBusy takes conversation.busy: the run that started, or the fact that the conversation went idle. A failure
// comes with the end of a run and belongs to the turn that ended, so a new run clears it.
func (s *State) ApplyBusy(n kernel.BusyNotification) bool {
	run := n.Run
	if !namesARun(run) {
		run = nil
	}
	failure := s.Failure
	if len(run) > 0 && len(n.RunFailure) == 0 {
		// A run that starts is not a failure of this turn, whatever the last one did.
		failure = nil
	}
	if len(n.RunFailure) > 0 && string(n.RunFailure) != "null" {
		failure = n.RunFailure
	}
	if bytes.Equal(runOrNil(s.Run), runOrNil(run)) && bytes.Equal(failureOrNil(s.Failure), failureOrNil(failure)) {
		return false
	}
	s.Run, s.Failure = run, failure
	s.Revision++
	return true
}

// ApplyUsage takes conversation.usage's answer and reports whether it differs from the last one.
func (s *State) ApplyUsage(u kernel.Usage) bool {
	if bytes.Equal(s.usage.Models, u.Models) && bytes.Equal(s.usage.Tools, u.Tools) && bytes.Equal(s.usage.Context, u.Context) {
		return false
	}
	s.usage = u
	s.Revision++
	return true
}

// readDocs reads the three documents a window asks about out of the view it just took, and clears the ones the view
// no longer carries: an idle conversation must not keep the last run it had.
func (s *State) readDocs() {
	s.Run, s.Failure = nil, nil
	s.Inbox, s.usageDoc = nil, nil
	if raw := s.view.Doc("pi.live"); len(raw) > 0 {
		var live struct {
			Run        json.RawMessage `json:"run"`
			RunFailure json.RawMessage `json:"runFailure"`
		}
		if json.Unmarshal(raw, &live) == nil {
			if namesARun(live.Run) {
				s.Run = live.Run
			}
			if len(live.RunFailure) > 0 && string(live.RunFailure) != "null" {
				s.Failure = live.RunFailure
			}
		}
	}
	if raw := s.view.Doc("pi.inbox"); len(raw) > 0 {
		s.Inbox = raw
	}
	if raw := s.view.Doc("pi.usage"); len(raw) > 0 {
		s.usageDoc = raw
	}
}

// namesARun says whether a run document names one: absent, empty and the JSON null are all "nothing runs".
func namesARun(run json.RawMessage) bool {
	return len(run) > 0 && string(run) != "null"
}

func runOrNil(v json.RawMessage) []byte {
	if !namesARun(v) {
		return nil
	}
	return v
}

func failureOrNil(v json.RawMessage) []byte {
	if len(v) == 0 || string(v) == "null" {
		return nil
	}
	return v
}

// messageOf reads the sentence out of an error object: {message, code}.
func messageOf(raw json.RawMessage) string {
	var e struct {
		Message string `json:"message"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &e) != nil {
		return ""
	}
	return e.Message
}

// Store holds the conversations a window has open, and the state each was last pushed.
//
// It replaces the session store the Daemon's path used, with one difference that matters: opening subscribes this
// connection, so closing has to unsubscribe, or Kernel keeps sending views for a conversation nobody draws.
type Store struct {
	// Max is how many conversations keep their state, the open one included. The oldest goes first, as a window
	// that has shown five sessions needs the one it is on.
	Max int

	mu     sync.Mutex
	order  []kernel.ID
	states map[kernel.ID]*State
	subs   map[int]func(kernel.ID)
	nextID int
}

// NewStore makes a store that keeps max conversations' states. max below 1 keeps one.
func NewStore(max int) *Store {
	if max < 1 {
		max = 1
	}
	return &Store{Max: max, states: map[kernel.ID]*State{}, subs: map[int]func(kernel.ID){}}
}

// Open subscribes this connection to a conversation and keeps the view Kernel answers with. Opening one that is
// already open changes nothing: the window is the same, and a second open would only ask for it again.
func (st *Store) Open(ctx context.Context, k Kernel, id kernel.ID, window kernel.Window) error {
	st.mu.Lock()
	_, held := st.states[id]
	st.mu.Unlock()
	if held {
		return nil
	}

	// Not under the lock: this is a round trip, and the window's other conversations must keep drawing while it
	// runs.
	opened, err := k.Open(ctx, kernel.OpenParams{ID: id, Window: &window})
	if err != nil {
		return err
	}

	st.mu.Lock()
	state := NewState(id)
	state.view = opened.View
	state.opened = true
	state.Revision = 1
	state.readDocs()
	st.states[id] = state
	st.order = append(st.order, id)
	var evicted []kernel.ID
	for len(st.order) > st.Max {
		oldest := st.order[0]
		st.order = st.order[1:]
		delete(st.states, oldest)
		evicted = append(evicted, oldest)
	}
	st.mu.Unlock()

	// Outside the lock for the same reason: each is a round trip.
	for _, old := range evicted {
		_ = k.Unwatch(ctx, old)
	}
	return nil
}

// Close stops this connection's subscription and drops the state.
func (st *Store) Close(ctx context.Context, k Kernel, id kernel.ID) {
	st.mu.Lock()
	_, held := st.states[id]
	delete(st.states, id)
	st.order = withoutID(st.order, id)
	st.mu.Unlock()
	if held {
		_ = k.Unwatch(ctx, id)
	}
}

// State is the conversation's state, nil when it is not open.
func (st *Store) State(id kernel.ID) *State {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.states[id]
}

// ApplyView routes a conversation.view push to the conversation it names, and reports whether it changed. A push
// for a conversation this connection did not open is ignored: another window, or one that closed here, is not this
// one's to draw.
func (st *Store) ApplyView(n kernel.ViewNotification) bool {
	st.mu.Lock()
	state := st.states[n.ID]
	st.mu.Unlock()
	if state == nil {
		return false
	}
	if !state.Apply(n) {
		return false
	}
	st.notify(n.ID)
	return true
}

// ApplyBusy routes conversation.busy, which reaches every connection.
func (st *Store) ApplyBusy(n kernel.BusyNotification) bool {
	st.mu.Lock()
	state := st.states[n.ID]
	st.mu.Unlock()
	if state == nil {
		return false
	}
	if !state.ApplyBusy(n) {
		return false
	}
	st.notify(n.ID)
	return true
}

// ApplyUsage routes conversation.usage's answer.
func (st *Store) ApplyUsage(id kernel.ID, u kernel.Usage) bool {
	st.mu.Lock()
	state := st.states[id]
	st.mu.Unlock()
	if state == nil {
		return false
	}
	if !state.ApplyUsage(u) {
		return false
	}
	st.notify(id)
	return true
}

// Subscribe calls fn with each conversation whose state changed, until the returned function is called. The app
// passes a function that bumps its Revisions and redraws.
func (st *Store) Subscribe(fn func(kernel.ID)) (stop func()) {
	st.mu.Lock()
	st.nextID++
	id := st.nextID
	st.subs[id] = fn
	st.mu.Unlock()
	return func() {
		st.mu.Lock()
		delete(st.subs, id)
		st.mu.Unlock()
	}
}

// notify calls the watchers outside the lock: one of them redraws.
func (st *Store) notify(id kernel.ID) {
	st.mu.Lock()
	fns := make([]func(kernel.ID), 0, len(st.subs))
	for _, fn := range st.subs {
		fns = append(fns, fn)
	}
	st.mu.Unlock()
	for _, fn := range fns {
		fn(id)
	}
}

// withoutID is ids without id, in order.
func withoutID(ids []kernel.ID, id kernel.ID) []kernel.ID {
	out := ids[:0]
	for _, each := range ids {
		if each != id {
			out = append(out, each)
		}
	}
	return out
}
