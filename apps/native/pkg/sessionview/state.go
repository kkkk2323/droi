package sessionview

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"

	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// A View draws a conversation; it does not keep one. Kernel pushes a whole view after every commit — that is what
// subscribing to "view" means — and this file holds exactly what a window needs of it: the window of entries, the
// run, the queued input, the live document the streaming half reads, the usage, and the failure of a read. There
// is no state machine here: every push replaces what it names, and nothing is inferred from what is missing.
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
	SubmissionGet(ctx context.Context, p kernel.AbortSubmissionParams) (kernel.SubmissionGet, error)
	Configure(ctx context.Context, p kernel.ConfigureParams) (kernel.ConfigureResult, error)
	Entries(ctx context.Context, p kernel.EntriesParams) (kernel.EntriesResult, error)
	Usage(ctx context.Context, id kernel.ID) (kernel.Usage, error)
	Processes(ctx context.Context, id kernel.ID) (kernel.ProcessesResult, error)
	StopProcess(ctx context.Context, p kernel.StopProcessParams) (kernel.Status, error)
}

// InboxItem is one input waiting for the run in progress: pi.inbox's items[], in submission-id order. Mode is
// "steer" or "followUp" for input the user queued and "write" for an entry a client asked to append; Content is
// the conversation.submit input and Entry the draft a write will be placed as; ID is the submission that
// conversation.submit answered, which is how a queued message is paired with submission.abort.
type InboxItem struct {
	ID      int64           `json:"id"`
	Mode    string          `json:"mode"`
	Content json.RawMessage `json:"content,omitempty"`
	Entry   json.RawMessage `json:"entry,omitempty"`
}

// State is one conversation as the window draws it: the view Kernel last pushed, and the few documents it carries.
//
// A push arrives on the connection's read loop, a usage answer comes back on whichever goroutine asked for it, and
// the window reads while both happen, so every field is behind the lock and a caller reads through the accessors.
// The store takes its own lock before a state's lock; a state never calls back into the store. Accessor results
// are read-only snapshots: later pushes replace them rather than mutating their slices or maps.
type State struct {
	mu sync.RWMutex

	id       kernel.ID
	revision int64
	loadErr  string
	run      json.RawMessage
	failure  json.RawMessage
	inbox    json.RawMessage
	agent    json.RawMessage
	live     json.RawMessage
	usageDoc json.RawMessage
	usage    kernel.Usage
	view     kernel.View
	opened   bool
	// held is every entry this window has of the conversation, in ID order: the view's window, the entries the
	// window has dropped since, and the older pages loaded above it. Entries never change once committed, so a
	// later window only adds to it (the rules are Pi's ConversationTranscript's, without its gaps: a window that
	// does not continue what is held replaces it).
	held []kernel.Entry
	// low is held's exclusive lower bound: entries at or below it are not loaded. Entry IDs start at 1, so 0 says
	// held starts at the conversation's first entry.
	low kernel.ID
	// landed says a whole view has been applied since the last conversation.open asked for one. Kernel starts
	// the subscription before it answers, so a push that arrives during the round trip is the newer of the two.
	landed bool
}

// NewState makes the state of conversation id, empty until a view arrives.
func NewState(id kernel.ID) *State { return &State{id: id} }

// ID is the conversation's Kernel id.
func (s *State) ID() kernel.ID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.id
}

// Opened reports whether a view arrived: a conversation whose open is still in flight has nothing to draw yet.
func (s *State) Opened() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.opened
}

// Revision counts every change a View must draw again: a new view, a run starting or ending, new usage.
func (s *State) Revision() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revision
}

// LoadErr is Kernel's sentence for a view it could not read, cleared by the next one it can.
func (s *State) LoadErr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadErr
}

// View is the window of entries Kernel last pushed, and the documents with it.
func (s *State) View() kernel.View {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.view
}

// Entries are the window's entries, oldest first.
func (s *State) Entries() []kernel.Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.view.Entries
}

// Before is the cursor for the page before this window, or nil at the conversation's start.
func (s *State) Before() json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.view.Before
}

// Transcript is the view with every entry held instead of the window: what the window draws, oldest first.
func (s *State) Transcript() kernel.View {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := s.view
	v.Entries = s.held
	v.Before = nil
	if s.low > 0 {
		v.Before = json.RawMessage(strconv.FormatInt(int64(s.low), 10))
	}
	return v
}

// HasOlder reports whether entries exist above the ones held.
func (s *State) HasOlder() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.low > 0
}

// Low is the newest entry not held: the page above starts there. 0 when held starts the conversation.
func (s *State) Low() kernel.ID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.low
}

// Run is pi.live's run, or nil when nothing runs: a conversation is busy exactly while this names one.
func (s *State) Run() json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.run
}

// RunInputs are the submissions the run in progress placed: they are in the transcript, not waiting.
func (s *State) RunInputs() []int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var run struct {
		Inputs []int64 `json:"inputs"`
	}
	if !namesARun(s.run) || json.Unmarshal(s.run, &run) != nil {
		return nil
	}
	return run.Inputs
}

// Agent is the view's pi.agent: the model, thinking level and working directory the conversation runs with. It is
// the zero Agent until a view arrives.
func (s *State) Agent() kernel.Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var a kernel.Agent
	if len(s.agent) > 0 {
		_ = json.Unmarshal(s.agent, &a)
	}
	return a
}

// Failure is why the run that ended failed, or nil.
func (s *State) Failure() json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.failure
}

// LiveDocument is the view's whole pi.live: the run, the streaming generation with its retries, the tool calls in
// progress with their output, the nested calls and the live compactions. The accessors above are the common
// questions; this is the document for the ones they do not cover.
func (s *State) LiveDocument() json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live
}

// InboxItems are the inputs waiting for the run in progress: pi.inbox's items, which is exactly what is not yet in
// the transcript. It is nil when no view arrived and empty when nothing is queued.
func (s *State) InboxItems() []InboxItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.inbox) == 0 {
		return nil
	}
	var doc struct {
		Items []InboxItem `json:"items"`
	}
	if json.Unmarshal(s.inbox, &doc) != nil {
		return nil
	}
	return doc.Items
}

// Busy reports whether a run is in progress: one is exactly while pi.live names one.
func (s *State) Busy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return namesARun(s.run)
}

// UsageDocument is the view's pi.usage: every model and tool the conversation used, with its counters.
func (s *State) UsageDocument() json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.usageDoc
}

// Usage is what conversation.usage last answered: the same counters by model and tool, plus how full the context
// is.
func (s *State) Usage() kernel.Usage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.usage
}

// UsageContext is conversation.usage's context: {used, limit, compactionAt}. The composer's meter reads it.
func (s *State) UsageContext() json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.usage.Context
}

// Apply takes a conversation.view push and reports whether anything changed.
//
// A whole view replaces the state, docs and all: a push is a snapshot of what Kernel reads, so keeping pieces of
// the previous one would be guessing. A failed read keeps the entries the window shows — a transient failure must
// not blank a transcript — and records Kernel's sentence. A delta push has no value, and a window subscriber that
// receives one is misconfigured: the state changes nothing rather than applying ops it never asked for.
func (s *State) Apply(n kernel.ViewNotification) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.apply(n)
}

func (s *State) apply(n kernel.ViewNotification) bool {
	switch {
	case n.Failed():
		message := messageOf(n.Error)
		if message == "" {
			message = "the conversation could not be read"
		}
		if s.loadErr == message {
			return false
		}
		s.loadErr = message
		s.revision++
		return true
	case n.Value == nil:
		return false
	}
	s.take(*n.Value)
	return true
}

// take replaces the state with a whole view. It is called with the lock held.
func (s *State) take(v kernel.View) {
	s.hold(v)
	s.view = v
	s.opened = true
	s.landed = true
	s.loadErr = ""
	s.readDocs()
	s.revision++
}

// expectOpen marks the start of a conversation.open round trip for this state.
func (s *State) expectOpen() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.landed = false
}

// takeOpen takes the whole view conversation.open answered with, unless a push landed while the call was in
// flight: Kernel subscribes before it answers, so what it pushed is at least as new as what it answered, and
// applying the older answer over it would lose the last push of a turn.
func (s *State) takeOpen(v kernel.View) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.landed {
		return false
	}
	s.take(v)
	return true
}

// failOpen records Kernel's reason for an open it refused. The entries the window shows stay: a conversation that
// cannot be read again is still the conversation it was.
func (s *State) failOpen(err error) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	message := err.Error()
	if s.loadErr == message {
		return false
	}
	s.loadErr = message
	s.revision++
	return true
}

// ApplyBusy takes conversation.busy: the run that started, or the fact that the conversation went idle. A failure
// comes with the end of a run and belongs to the turn that ended, so a new run clears it.
func (s *State) ApplyBusy(n kernel.BusyNotification) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyBusy(n)
}

func (s *State) applyBusy(n kernel.BusyNotification) bool {
	run := n.Run
	if !namesARun(run) {
		run = nil
	}
	failure := s.failure
	if len(run) > 0 && len(n.RunFailure) == 0 {
		// A run that starts is not a failure of this turn, whatever the last one did.
		failure = nil
	}
	if len(n.RunFailure) > 0 && string(n.RunFailure) != "null" {
		failure = n.RunFailure
	}
	if bytes.Equal(runOrNil(s.run), runOrNil(run)) && bytes.Equal(failureOrNil(s.failure), failureOrNil(failure)) {
		return false
	}
	s.run, s.failure = run, failure
	s.revision++
	return true
}

// hold adds a window's entries to the ones held. It is called with the lock held, before the view is replaced.
//
// A window with no before starts the conversation, so it is everything there is. One whose before lies past the
// newest entry held does not continue it — pushes coalesced past a whole window, or a new connection — and the
// entries in between are not known, so the window replaces what is held rather than leaving a hole in it.
// Otherwise it continues what is held and only adds: the entries it dropped from its front stay.
func (s *State) hold(v kernel.View) {
	before := entryIDOf(v.Before)
	tail := s.low
	if n := len(s.held); n > 0 {
		tail = s.held[n-1].ID
	}
	if before == 0 || !s.opened || before > tail {
		s.held = slices.Clone(v.Entries)
		s.low = before
		return
	}
	s.held = mergeEntries(s.held, v.Entries)
	s.low = min(s.low, before)
}

// PrependOlder puts a page of the entries just above the held ones on top of them, and reports whether it did.
//
// low is the Low the page was asked for: a page asked before a window replaced what is held, or before another
// page landed, belongs to a transcript this state no longer has, and is dropped. The page is newest first, as
// conversation.entries reads backwards. A page without a cursor, or an empty one, reaches the conversation's start.
func (s *State) PrependOlder(low kernel.ID, page kernel.EntriesResult) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.opened || low == 0 || s.low != low {
		return false
	}
	older := make([]kernel.Entry, 0, len(page.Items)+len(s.held))
	for i := len(page.Items) - 1; i >= 0; i-- {
		if e := page.Items[i]; e.ID <= low {
			older = append(older, e)
		}
	}
	if len(older) == 0 || len(page.Cursor) == 0 || string(page.Cursor) == "null" {
		s.low = 0
	} else {
		s.low = older[0].ID - 1
	}
	s.held = append(older, s.held...)
	s.revision++
	return true
}

// entryIDOf reads a view's before: an entry ID, or 0 when the window starts the conversation.
func entryIDOf(raw json.RawMessage) kernel.ID {
	var id kernel.ID
	if len(raw) == 0 || json.Unmarshal(raw, &id) != nil {
		return 0
	}
	return id
}

// mergeEntries is held with the entries of window it lacks, in ID order. The window is newest last, so in the
// common case it only appends.
func mergeEntries(held, window []kernel.Entry) []kernel.Entry {
	var fresh []kernel.Entry
	for _, e := range window {
		if _, found := slices.BinarySearchFunc(held, e.ID, func(h kernel.Entry, id kernel.ID) int { return cmp.Compare(h.ID, id) }); !found {
			fresh = append(fresh, e)
		}
	}
	if len(fresh) == 0 {
		return held
	}
	if len(held) == 0 || fresh[0].ID > held[len(held)-1].ID {
		return append(slices.Clip(held), fresh...)
	}
	merged := append(slices.Clone(held), fresh...)
	slices.SortStableFunc(merged, func(a, b kernel.Entry) int { return cmp.Compare(a.ID, b.ID) })
	return merged
}

// ApplyUsage takes conversation.usage's answer and reports whether it differs from the last one.
func (s *State) ApplyUsage(u kernel.Usage) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyUsage(u)
}

func (s *State) applyUsage(u kernel.Usage) bool {
	if bytes.Equal(s.usage.Models, u.Models) && bytes.Equal(s.usage.Tools, u.Tools) && bytes.Equal(s.usage.Context, u.Context) {
		return false
	}
	s.usage = u
	s.revision++
	return true
}

// readDocs reads the documents a window asks about out of the view it just took, and clears the ones the view no
// longer carries: an idle conversation must not keep the last run it had. It is called with the lock held.
func (s *State) readDocs() {
	s.run, s.failure = nil, nil
	s.inbox, s.live, s.usageDoc, s.agent = nil, nil, nil, nil
	if raw := s.view.Doc("pi.live"); len(raw) > 0 {
		s.live = raw
		var live struct {
			Run        json.RawMessage `json:"run"`
			RunFailure json.RawMessage `json:"runFailure"`
		}
		if json.Unmarshal(raw, &live) == nil {
			if namesARun(live.Run) {
				s.run = live.Run
			}
			if len(live.RunFailure) > 0 && string(live.RunFailure) != "null" {
				s.failure = live.RunFailure
			}
		}
	}
	if raw := s.view.Doc("pi.inbox"); len(raw) > 0 {
		s.inbox = raw
	}
	if raw := s.view.Doc("pi.usage"); len(raw) > 0 {
		s.usageDoc = raw
	}
	if raw := s.view.Doc("pi.agent"); len(raw) > 0 {
		s.agent = raw
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
// connection, so closing has to unsubscribe, or Kernel keeps sending views for a conversation nobody draws. Views
// are per connection (protocol/README.md, conversation.open and conversation.unwatch), so one Store owns one
// connection: a second window sharing a client would have its subscriptions taken from it.
//
// A Kernel restart ends the connection, and the states here outlive it. Reopen is what brings them onto the new
// one; every push says which connection generation it arrived on, and one from an earlier generation is dropped.
//
// The subscription calls of one conversation on one connection reach Kernel in the order the store decided them,
// each after the one before has answered. An unwatch that evicts a conversation and an open that brings it back are
// decided in that order, but they run outside the lock, and an unwatch that landed second would end the new
// subscription. Calls on a newer connection do not wait for the old one's: subscriptions are per connection.
type Store struct {
	max int

	mu      sync.Mutex
	gen     int64
	order   []kernel.ID
	windows map[kernel.ID]kernel.Window
	states  map[kernel.ID]*State
	subs    map[int]func(kernel.ID)
	nextID  int
	// turns is, per connection and conversation, the subscription call decided last: closed once it has answered.
	turns map[turnKey]chan struct{}
	// paused and sent are the inputs the window holds for a conversation (store_inputs.go). Unlike states, they are
	// never evicted: they are the user's words.
	paused   map[kernel.ID][]session.QueuedMessage
	sent     map[kernel.ID]map[int64]sentInput
	inflight map[kernel.ID][]*inflight
	disposed map[kernel.ID]map[int64]bool
	// placed are the submissions a run placed while sends were in flight; removedSends are the local ids of sends
	// whose paused copy the user removed before Kernel answered them.
	placed       map[kernel.ID]map[int64]bool
	removedSends map[string]bool
	// seq counts the submissions recorded in sent.
	seq uint64
}

// turnKey is a conversation on one connection generation.
type turnKey struct {
	gen int64
	id  kernel.ID
}

// NewStore keeps the max most recently used conversations plus older busy ones. max below 1 keeps one.
func NewStore(max int) *Store { return &Store{max: max} }

// ready fills in what the zero value lacks, so a store built as a struct literal behaves as NewStore's does. It is
// called with the lock held.
func (st *Store) ready() {
	if st.states == nil {
		st.states = map[kernel.ID]*State{}
	}
	if st.windows == nil {
		st.windows = map[kernel.ID]kernel.Window{}
	}
	if st.subs == nil {
		st.subs = map[int]func(kernel.ID){}
	}
	if st.turns == nil {
		st.turns = map[turnKey]chan struct{}{}
	}
	if st.paused == nil {
		st.paused = map[kernel.ID][]session.QueuedMessage{}
	}
	if st.sent == nil {
		st.sent = map[kernel.ID]map[int64]sentInput{}
	}
	if st.inflight == nil {
		st.inflight = map[kernel.ID][]*inflight{}
	}
	if st.disposed == nil {
		st.disposed = map[kernel.ID]map[int64]bool{}
	}
	if st.placed == nil {
		st.placed = map[kernel.ID]map[int64]bool{}
	}
	if st.removedSends == nil {
		st.removedSends = map[string]bool{}
	}
	if st.max < 1 {
		st.max = 1
	}
}

// Max is how many recently used conversations keep their state, in addition to older busy ones.
func (st *Store) Max() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.ready()
	return st.max
}

// Generation is the connection this store's subscriptions are on: 0 until Reopen names one.
func (st *Store) Generation() int64 {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.gen
}

// Open subscribes this connection to a conversation and keeps the view Kernel answers with.
//
// Opening one that is already open asks Kernel nothing — it would answer with the same window — and only records
// the use, so the conversations kept are the ones used last. The state exists as soon as this returns, before the
// answer does: Opened reports whether a view has arrived yet, and a push that lands during the call is kept rather
// than dropped.
func (st *Store) Open(ctx context.Context, k Kernel, id kernel.ID, window kernel.Window) error {
	st.mu.Lock()
	st.ready()
	if _, held := st.states[id]; held {
		st.touchLocked(id)
		evicted := st.unwatchesLocked(st.trimLocked(id))
		st.mu.Unlock()
		st.unwatch(ctx, k, evicted)
		return nil
	}
	// The state goes in before the round trip, not after: Kernel subscribes before it answers, so a push for this
	// conversation can arrive while the call is in flight. A state inserted afterwards would drop it, and the
	// push it drops can be the last one of a turn — the one the page cannot recover from.
	state := NewState(id)
	st.states[id] = state
	st.order = append(st.order, id)
	st.windows[id] = window
	gen := st.gen
	// The answer is read after the call goes out: if it is idle, every submission recorded before now was placed.
	mark := st.seq
	evicted := st.unwatchesLocked(st.trimLocked(id))
	turn := st.turnLocked(id)
	st.mu.Unlock()

	st.unwatch(ctx, k, evicted)

	var opened kernel.OpenResult
	err := turn.run(ctx, func() (err error) {
		opened, err = k.Open(ctx, kernel.OpenParams{ID: id, Window: &window, Subscribe: kernel.SubscribeView})
		return err
	})

	st.mu.Lock()
	if st.gen != gen || st.states[id] != state {
		// A new connection or a later open owns this state, even if the old call failed.
		st.mu.Unlock()
		return nil
	}
	if err != nil {
		delete(st.states, id)
		delete(st.windows, id)
		st.order = withoutID(st.order, id)
		st.mu.Unlock()
		return err
	}
	changed := state.takeOpen(opened.View)
	st.settleLocked(id, mark)
	st.mu.Unlock()
	if changed {
		st.notify(id)
	}
	return nil
}

// Use records that conversation id was used again, as Open does for one the store holds, and reports whether the
// store holds it. It does not wait for the unwatches of the conversations that leaves out: their order among the
// other calls of each conversation is settled here, under the lock.
func (st *Store) Use(ctx context.Context, k Kernel, id kernel.ID) bool {
	st.mu.Lock()
	st.ready()
	if _, held := st.states[id]; !held {
		st.mu.Unlock()
		return false
	}
	st.touchLocked(id)
	evicted := st.unwatchesLocked(st.trimLocked(id))
	st.mu.Unlock()
	if len(evicted) > 0 {
		go st.unwatch(ctx, k, evicted)
	}
	return true
}

// Close stops this connection's subscription and drops the state.
func (st *Store) Close(ctx context.Context, k Kernel, id kernel.ID) {
	st.mu.Lock()
	st.ready()
	_, held := st.states[id]
	delete(st.states, id)
	delete(st.windows, id)
	st.order = withoutID(st.order, id)
	var evicted []unwatchCall
	if held {
		evicted = st.unwatchesLocked([]kernel.ID{id})
	}
	st.mu.Unlock()
	st.unwatch(ctx, k, evicted)
}

// Reopen takes the store onto a new connection: generation gen, answering on k. The connection the subscriptions
// were made on is gone, and they went with it, so this opens every held conversation again on the new one and
// replaces each state with the whole view the new Kernel answers. Pushes from an earlier generation are dropped
// (the Apply methods take the generation they arrived on), and one from a Reopen that has since been superseded
// stops where it is.
//
// A conversation the new Kernel refuses keeps the entries it had and records the sentence, the same as a failed
// read: a conversation that cannot be read again is still the conversation the window was showing.
func (st *Store) Reopen(ctx context.Context, k Kernel, gen int64) error {
	st.mu.Lock()
	st.ready()
	st.gen = gen
	ids := append([]kernel.ID(nil), st.order...)
	st.mu.Unlock()

	var failures []error
	for _, id := range ids {
		if err := st.reopen(ctx, k, id, gen); err != nil {
			failures = append(failures, fmt.Errorf("conversation %d: %w", id, err))
		}
	}
	return errors.Join(failures...)
}

// reopen opens one held conversation again on generation gen, and replaces its state with the answer.
func (st *Store) reopen(ctx context.Context, k Kernel, id kernel.ID, gen int64) error {
	st.mu.Lock()
	if st.gen != gen {
		st.mu.Unlock()
		return nil
	}
	state := st.states[id]
	if state == nil {
		st.mu.Unlock()
		return nil
	}
	window := st.windows[id]
	state.expectOpen()
	mark := st.seq
	turn := st.turnLocked(id)
	st.mu.Unlock()

	var opened kernel.OpenResult
	err := turn.run(ctx, func() (err error) {
		opened, err = k.Open(ctx, kernel.OpenParams{ID: id, Window: &window, Subscribe: kernel.SubscribeView})
		return err
	})

	st.mu.Lock()
	if st.gen != gen || st.states[id] != state {
		st.mu.Unlock()
		return nil
	}
	changed := false
	if err != nil {
		changed = state.failOpen(err)
	} else {
		changed = state.takeOpen(opened.View)
		st.settleLocked(id, mark)
	}
	st.mu.Unlock()
	if changed {
		st.notify(id)
	}
	return err
}

// LoadEarlier brings the page of up to limit entries above the ones the conversation's state holds. It does
// nothing for a conversation that is not open or whose state already starts at its first entry.
//
// The page is asked by entry ID, not by the view's cursor: the view's before is the entry just above its window,
// and conversation.entries takes it as maxEntryId. A page that arrives after the state moved on is dropped.
func (st *Store) LoadEarlier(ctx context.Context, k Kernel, id kernel.ID, limit int) error {
	state := st.State(id)
	if state == nil {
		return nil
	}
	low := state.Low()
	if low == 0 {
		return nil
	}
	query, err := json.Marshal(struct {
		Order      string    `json:"order"`
		MaxEntryID kernel.ID `json:"maxEntryId"`
	}{"descending", low})
	if err != nil {
		return err
	}
	page, err := k.Entries(ctx, kernel.EntriesParams{ID: id, Query: query, Limit: limit})
	if err != nil {
		return err
	}
	st.mu.Lock()
	changed := st.states[id] == state && state.PrependOlder(low, page)
	st.mu.Unlock()
	if changed {
		st.notify(id)
	}
	return nil
}

// State is the conversation's state, nil when it is not open.
func (st *Store) State(id kernel.ID) *State {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.states[id]
}

// ApplyView routes a conversation.view push that arrived on connection generation gen, and reports whether it
// changed anything. A push for a conversation this connection did not open, or one from a connection that is gone,
// is ignored: another window, or the Kernel that has since restarted, is not this one's to draw.
func (st *Store) ApplyView(gen int64, n kernel.ViewNotification) bool {
	st.mu.Lock()
	changed := false
	if gen == st.gen {
		if state := st.states[n.ID]; state != nil {
			changed = state.Apply(n)
			st.settleLocked(n.ID, 0)
		}
	}
	st.mu.Unlock()
	if changed {
		st.notify(n.ID)
	}
	return changed
}

// ApplyBusy routes conversation.busy, which reaches every connection.
func (st *Store) ApplyBusy(gen int64, n kernel.BusyNotification) bool {
	st.mu.Lock()
	changed := false
	if gen == st.gen {
		if state := st.states[n.ID]; state != nil {
			changed = state.ApplyBusy(n)
		}
		if n.Busy() {
			st.settleLocked(n.ID, 0)
		} else {
			st.idleLocked(n.ID)
		}
	}
	st.mu.Unlock()
	if changed {
		st.notify(n.ID)
	}
	return changed
}

// ApplyUsage routes conversation.usage's answer.
func (st *Store) ApplyUsage(gen int64, id kernel.ID, u kernel.Usage) bool {
	st.mu.Lock()
	changed := false
	if gen == st.gen {
		if state := st.states[id]; state != nil {
			changed = state.ApplyUsage(u)
		}
	}
	st.mu.Unlock()
	if changed {
		st.notify(id)
	}
	return changed
}

// Subscribe calls fn with each conversation whose state changed, until the returned function is called. The app
// passes a function that bumps its Revisions and redraws.
func (st *Store) Subscribe(fn func(kernel.ID)) (stop func()) {
	st.mu.Lock()
	st.ready()
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

// touchLocked moves a conversation to the end of the order, which is what makes the eviction order the order of
// use rather than the order of first open: going back to a session is the commonest thing a window does.
func (st *Store) touchLocked(id kernel.ID) {
	st.order = append(withoutID(st.order, id), id)
}

// trimLocked keeps the most recently used max conversations plus older busy ones, as keptViews does. Busy
// exemptions do not consume a recent slot. An open still in flight is also exempt: its answer has not yet told
// us whether it is busy. The caller unwatches the dropped conversations outside the lock.
func (st *Store) trimLocked(protect kernel.ID) []kernel.ID {
	var evicted []kernel.ID
	cut := len(st.order) - st.max
	var kept []kernel.ID
	for i, id := range st.order {
		state := st.states[id]
		if i >= cut || id == protect || (state != nil && (!state.Opened() || state.Busy())) {
			kept = append(kept, id)
			continue
		}
		delete(st.states, id)
		delete(st.windows, id)
		evicted = append(evicted, id)
	}
	st.order = kept
	return evicted
}

// turn is one subscription call's place in its conversation's order: it runs once the call before has answered,
// and the call after waits for it.
type turn struct {
	after <-chan struct{}
	done  func()
}

// turnLocked takes the next place in conversation id's order on the current connection. It is called with the
// lock held, at the moment the store decides the call, which is what makes the order the order of the decisions.
func (st *Store) turnLocked(id kernel.ID) turn {
	key := turnKey{st.gen, id}
	after := st.turns[key]
	mine := make(chan struct{})
	st.turns[key] = mine
	return turn{after: after, done: func() {
		close(mine)
		st.mu.Lock()
		if st.turns[key] == mine {
			delete(st.turns, key)
		}
		st.mu.Unlock()
	}}
}

// run makes call once the call before has answered. A context that ends first returns its error without the
// call, and the place is held until the call before answers, so the one after still cannot overtake it.
func (t turn) run(ctx context.Context, call func() error) error {
	if t.after != nil {
		select {
		case <-t.after:
		case <-ctx.Done():
			go func() {
				<-t.after
				t.done()
			}()
			return ctx.Err()
		}
	}
	defer t.done()
	return call()
}

// unwatchCall is an unwatch the store decided, with its place in the conversation's order.
type unwatchCall struct {
	id   kernel.ID
	turn turn
}

// unwatchesLocked takes a place for an unwatch of each id. It is called with the lock held.
func (st *Store) unwatchesLocked(ids []kernel.ID) []unwatchCall {
	calls := make([]unwatchCall, len(ids))
	for i, id := range ids {
		calls[i] = unwatchCall{id: id, turn: st.turnLocked(id)}
	}
	return calls
}

// unwatch stops the subscriptions of conversations this store dropped. Each is a round trip, so none of them runs
// under the lock.
func (st *Store) unwatch(ctx context.Context, k Kernel, calls []unwatchCall) {
	for _, c := range calls {
		_ = c.turn.run(ctx, func() error { return k.Unwatch(ctx, c.id) })
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
