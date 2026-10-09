// This file is the Go counterpart of the TypeScript SDK's
// MultiSessionStateManager (@factory/droid-sdk 0.9.1); state.go, messages.go,
// todos.go and queue.go port SessionStateManager and SessionStore.
//
// Deliberately left out or simplified, against the TypeScript SDK:
//   - Missions, terminals, MCP status, hook execution rows, progressive UI
//     rendering (render limits, compaction cutoff and pruning), LRU eviction,
//     the pending/default settings stores, pre-init and initial-prompt
//     markers, machine ids (one local machine; MarkAllNotLoaded replaces
//     markSessionsNotLoadedForMachine), OpenTelemetry and logging.
//   - Change notification: events are delivered synchronously after each
//     mutation, once per kind and Session per mutation; there is no frame or
//     timer coalescing. Delta-driven updates are flagged Event.Streaming for
//     a UI to throttle.
//   - Loading or merging messages does not clear the settings, model list,
//     token usage or title first (the TypeScript store clears them and
//     restores only some); results overwrite what they carry.
//   - create_message replaces a stored message whole; the TypeScript spread
//     kept fields the new copy lacked.
//   - The streamingMessageIds set (only fed by the unused streaming-start
//     path) and the settled-hydration floor are gone.
//   - Notifications the controller turns into state (token usage, agent turn
//     completion, errors, working directory, inactivity, session_closed,
//     child_session_available) are applied by HandleNotification itself; see
//     doc.go for what the controller still owns.
//   - InitializeSession creates the Session when it is not registered
//     instead of failing.
//   - Queue reconciliation compares image and document sources as whole
//     JSON values rather than field by field; the queued-kind display group
//     and review priority helpers are gone, as are message getters no
//     consumer used (by role, recent, truncate, streaming start/end).
package session

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// EventKind names a kind of change.
type EventKind string

const (
	EventRegistered                  EventKind = "registered"
	EventRemoved                     EventKind = "removed"
	EventLoadStateChanged            EventKind = "load_state_changed"
	EventMessagesUpdated             EventKind = "message_thread_updated"
	EventWorkingStateChanged         EventKind = "working_state_changed"
	EventSettingsUpdated             EventKind = "settings_updated"
	EventMetadataUpdated             EventKind = "metadata_updated"
	EventTodosUpdated                EventKind = "todo_list_updated"
	EventQueuedMessagesUpdated       EventKind = "queued_messages_updated"
	EventStreamingPlaceholderUpdated EventKind = "streaming_placeholder_updated"
	EventSubagentSummaryUpdated      EventKind = "subagent_invocation_summary_updated"
	EventStatsUpdated                EventKind = "stats_updated"
)

// Event says what changed in which Session. Read the new state from the
// Store; it is already applied when the event arrives.
type Event struct {
	Kind      EventKind
	SessionID string
	// LoadState is the new state of an EventLoadStateChanged.
	LoadState LoadState
	// Streaming marks an EventMessagesUpdated caused by a text or thinking
	// delta or tool progress, which arrive many times a second.
	Streaming bool
}

// ErrSubmitTimeout is passed to OptimisticSubmit.OnError when the Daemon
// never confirms a submit.
var ErrSubmitTimeout = errors.New("session: optimistic submit timed out without server confirmation")

const (
	defaultSubmitTimeout = 20 * time.Second
	defaultProgressTTL   = time.Minute
	subagentTag          = "subagent"
)

// Store holds the state of every Session a Client follows. Its methods and
// those of Session are safe from any goroutine; subscribers run after the
// mutation, outside the lock, and may call back into the Store.
type Store struct {
	mu       sync.Mutex
	sessions map[string]*state
	pending  map[string][]pendingNotification

	notFound   map[string]bool
	inactive   map[string]bool
	concurrent map[string]bool
	spawn      map[string]SpawnOptions

	summaries       map[string]protocol.SubagentInvocationSummary
	summaryRev      int
	summaryEpoch    int
	summaryModified map[string]int

	submits map[string]*submit

	subs    []subscriber
	nextSub int
	events  []Event
	after   []func()

	statsSeed func(sessionID string) (Stats, bool)

	now           func() time.Time
	submitTimeout time.Duration
	progressTTL   time.Duration
}

type subscriber struct {
	id int
	fn func(Event)
}

type pendingNotification struct {
	typ  string
	v    any
	opts HandleOptions
}

// SpawnOptions are the load_session flags a Session was loaded with, kept
// so a later reload or self-resume spawns it the same way.
type SpawnOptions struct {
	DisableInactivityTimeout *bool
	DisableBuiltinSkills     *bool
	SkipPermissionsUnsafe    *bool
	RuntimeSettingsPath      string
	StructuredOutputFormat   *protocol.OutputFormat
	TaskSubagentProcess      *bool
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{
		sessions:        map[string]*state{},
		pending:         map[string][]pendingNotification{},
		notFound:        map[string]bool{},
		inactive:        map[string]bool{},
		concurrent:      map[string]bool{},
		spawn:           map[string]SpawnOptions{},
		summaries:       map[string]protocol.SubagentInvocationSummary{},
		summaryModified: map[string]int{},
		submits:         map[string]*submit{},
		now:             time.Now,
		submitTimeout:   defaultSubmitTimeout,
		progressTTL:     defaultProgressTTL,
	}
}

// Subscribe calls fn with every change, on the goroutine that made it.
// fn must not block. The returned function unsubscribes.
func (s *Store) Subscribe(fn func(Event)) (unsubscribe func()) {
	s.mu.Lock()
	id := s.nextSub
	s.nextSub++
	s.subs = append(s.subs, subscriber{id, fn})
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.subs = slices.DeleteFunc(s.subs, func(sub subscriber) bool { return sub.id == id })
		s.mu.Unlock()
	}
}

// update runs fn under the lock, then delivers the events and callbacks fn
// produced.
func (s *Store) update(fn func()) {
	events, after, subs := s.locked(fn)
	for _, e := range dedupe(events) {
		for _, sub := range subs {
			sub.fn(e)
		}
	}
	for _, f := range after {
		f()
	}
}

func (s *Store) locked(fn func()) (events []Event, after []func(), subs []subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn()
	events, after = s.events, s.after
	s.events, s.after = nil, nil
	return events, after, slices.Clone(s.subs)
}

func (s *Store) withLock(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn()
}

func (s *Store) emit(e Event) { s.events = append(s.events, e) }

// dedupe keeps the first of identical events; load-state changes all stay
// so the last one tells the final state.
func dedupe(events []Event) []Event {
	out := events[:0:0]
	for _, e := range events {
		if e.Kind != EventLoadStateChanged && slices.Contains(out, e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func valid(id string) bool { return strings.TrimSpace(id) != "" }

// ensure returns the Session, registering it when new.
func (s *Store) ensure(id string) (st *state, created bool) {
	if st := s.sessions[id]; st != nil {
		return st, false
	}
	st = newState(s, id)
	if s.statsSeed != nil {
		if seed, ok := s.statsSeed(id); ok {
			st.stats.totals = seed.clone()
		}
	}
	s.sessions[id] = st
	s.emit(Event{Kind: EventRegistered, SessionID: id})
	return st, true
}

// Session returns a handle on a registered Session, or nil.
func (s *Store) Session(id string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions[id] == nil {
		return nil
	}
	return &Session{s: s, id: id}
}

// EnsureSession registers a Session if needed, for seeding state (an
// optimistic submit) before it is loaded.
func (s *Store) EnsureSession(id string) *Session {
	if !valid(id) {
		return nil
	}
	s.update(func() { s.ensure(id) })
	return &Session{s: s, id: id}
}

// SessionIDs returns the registered Sessions.
func (s *Store) SessionIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// LoadState returns a Session's load state; NotLoaded when unknown.
func (s *Store) LoadState(id string) LoadState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.sessions[id]; st != nil {
		return st.load
	}
	return NotLoaded
}

// MarkLoading registers a Session and marks it Loading, unless it is
// already Loaded.
func (s *Store) MarkLoading(id string) {
	if !valid(id) {
		return
	}
	s.update(func() {
		st, _ := s.ensure(id)
		if st.load != Loaded {
			st.setLoadState(Loading, false)
		}
	})
}

// MarkNotLoaded rolls a Session back to NotLoaded (a failed load); a
// running turn is shown as stopped.
func (s *Store) MarkNotLoaded(id string) {
	s.update(func() {
		if st := s.sessions[id]; st != nil && st.load != NotLoaded {
			st.setLoadState(NotLoaded, false)
		}
	})
}

// MarkAllNotLoaded rolls every Session back to NotLoaded, as when the
// connection drops, so the next connection loads them again.
func (s *Store) MarkAllNotLoaded() {
	s.update(func() {
		for _, st := range s.sessions {
			if st.load != NotLoaded {
				st.setLoadState(NotLoaded, false)
			}
		}
	})
}

// MarkNotFound records that the Daemon does not know the Session and
// removes it.
func (s *Store) MarkNotFound(id string) {
	s.update(func() {
		s.notFound[id] = true
		s.removeSession(id)
		s.emit(Event{Kind: EventLoadStateChanged, SessionID: id, LoadState: NotLoaded})
	})
}

// IsNotFound reports whether MarkNotFound was called for the Session.
func (s *Store) IsNotFound(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notFound[id]
}

// ClearNotFound forgets a MarkNotFound.
func (s *Store) ClearNotFound(id string) { s.withLock(func() { delete(s.notFound, id) }) }

// MarkInactive records that the Daemon paused the Session's worker
// (HandleNotification does it on session_inactivity and
// session_process_exited).
func (s *Store) MarkInactive(id string) { s.withLock(func() { s.inactive[id] = true }) }

// MarkActive clears MarkInactive, after a load resumed the Session.
func (s *Store) MarkActive(id string) { s.withLock(func() { delete(s.inactive, id) }) }

// IsInactive reports whether the Session's worker is paused.
func (s *Store) IsInactive(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inactive[id]
}

// MarkConcurrentPrompts records that the Session is answering a batch of
// concurrent prompts, so a resume does not clear the survivors.
func (s *Store) MarkConcurrentPrompts(id string) { s.withLock(func() { s.concurrent[id] = true }) }

// ClearConcurrentPrompts forgets MarkConcurrentPrompts.
func (s *Store) ClearConcurrentPrompts(id string) { s.withLock(func() { delete(s.concurrent, id) }) }

// HasConcurrentPrompts reports MarkConcurrentPrompts.
func (s *Store) HasConcurrentPrompts(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.concurrent[id]
}

// SetSpawnOptions keeps the flags a Session was loaded with.
func (s *Store) SetSpawnOptions(id string, o SpawnOptions) { s.withLock(func() { s.spawn[id] = o }) }

// SpawnOptions returns what SetSpawnOptions kept.
func (s *Store) SpawnOptions(id string) (SpawnOptions, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.spawn[id]
	return o, ok
}

// DeleteSpawnOptions forgets SetSpawnOptions (the Session was closed).
func (s *Store) DeleteSpawnOptions(id string) { s.withLock(func() { delete(s.spawn, id) }) }

// RemoveSession forgets a Session that is gone for good (closed, not
// found), with its pending submits and flags.
func (s *Store) RemoveSession(id string) { s.update(func() { s.removeSession(id) }) }

func (s *Store) removeSession(id string) {
	st := s.sessions[id]
	if st != nil {
		st.stopTimers()
	}
	for key, sub := range s.submits {
		if sub.sessionID == id {
			s.cancelSubmit(key)
		}
	}
	delete(s.sessions, id)
	delete(s.pending, id)
	delete(s.inactive, id)
	delete(s.concurrent, id)
	delete(s.spawn, id)
	if st != nil {
		s.emit(Event{Kind: EventRemoved, SessionID: id})
	}
}

// ClearAllQueues empties every Session's queue, as on disconnect.
func (s *Store) ClearAllQueues() {
	s.update(func() {
		for _, st := range s.sessions {
			st.clearQueuedKinds(nil)
		}
	})
}

// ---- notifications ----

// HandleOptions tune HandleNotification.
type HandleOptions struct {
	// PreserveWorkingStateOnInactive keeps the working state when the
	// Session goes inactive (session_inactivity, session_process_exited);
	// the controller sets it while the Session still owes the user a
	// prompt.
	PreserveWorkingStateOnInactive bool
}

// HandleNotification applies a daemon.session_notification. One for a
// Session not registered yet is kept and replayed when the Session is
// initialized, loaded or registered as a child. It fails only when the
// notification does not decode.
func (s *Store) HandleNotification(p protocol.SessionNotificationParams, opts HandleOptions) error {
	if !valid(p.SessionID) {
		return errors.New("session: notification without a session id")
	}
	v, err := p.Notification.Value()
	if err != nil {
		return fmt.Errorf("session: decode %s notification: %w", p.Notification.Type, err)
	}
	s.update(func() { s.handle(p.SessionID, pendingNotification{p.Notification.Type, v, opts}) })
	return nil
}

func (s *Store) handle(id string, n pendingNotification) {
	st := s.sessions[id]
	if st == nil {
		s.pending[id] = append(s.pending[id], n)
		return
	}
	st.handle(n.typ, n.v, n.opts)
	if c, ok := n.v.(*protocol.CreateMessageNotification); ok && c.RequestID != "" {
		s.confirmSubmit(c.RequestID)
	}
}

func (s *Store) replay(id string) {
	queued := s.pending[id]
	delete(s.pending, id)
	for _, n := range queued {
		s.handle(id, n)
	}
}

// ---- initialize and load ----

// InitializeSession replaces a Session's transcript with that of a new
// Session and marks it Loaded.
func (s *Store) InitializeSession(id string, messages []protocol.FactoryDroidMessage) {
	if !valid(id) {
		return
	}
	s.update(func() { s.initialize(id, messages) })
}

func (s *Store) initialize(id string, messages []protocol.FactoryDroidMessage) *state {
	st, _ := s.ensure(id)
	st.initialize(messages)
	s.replay(id)
	return st
}

// ApplyInitializeResult takes a daemon.initialize_session result: the
// transcript, title, settings and model list. cwd is the directory the
// Session runs in (the worktree when one was made) and tags those it was
// created with; a subagent tag links it to its calling Session.
func (s *Store) ApplyInitializeResult(res *protocol.InitializeSessionResult, cwd string, tags []protocol.SessionTag) {
	if !valid(res.SessionID) {
		return
	}
	s.update(func() {
		st := s.initialize(res.SessionID, res.Session.Messages)
		st.setCwd(cwd)
		if res.Session.Title != "" {
			st.setTitle(res.Session.Title)
		}
		if callingSession, callingTool := subagentCaller(tags); callingSession != "" || callingTool != "" {
			st.setCalling(callingSession, callingTool)
		}
		set := res.Settings
		if set.Tags == nil {
			set.Tags = tags
		}
		st.applySessionSettings(set)
		if len(res.AvailableModels) > 0 {
			st.setModels(res.AvailableModels)
		}
	})
}

func subagentCaller(tags []protocol.SessionTag) (sessionID, toolUseID string) {
	for _, t := range tags {
		if t.Name == subagentTag {
			return t.Metadata["callingSessionId"], t.Metadata["callingToolUseId"]
		}
	}
	return "", ""
}

// LoadOptions tune LoadSession.
type LoadOptions struct {
	// Cwd, when set, becomes the Session's working directory ("" clears).
	Cwd                *string
	HasOlderMessages   bool
	LoadedMessageLimit int
	// WorkingState, when set, is applied before the messages are published.
	WorkingState protocol.DroidWorkingState
}

// LoadSession merges loaded messages into a Session (registering it when
// new) and marks it Loaded. Messages it already holds are replaced by the
// loaded copies; the merged transcript is ordered by parent chain when it
// forms one tree, by time otherwise.
func (s *Store) LoadSession(id string, messages []protocol.FactoryDroidMessage, opts LoadOptions) {
	if !valid(id) {
		return
	}
	s.update(func() { s.load(id, messages, opts) })
}

func (s *Store) load(id string, messages []protocol.FactoryDroidMessage, opts LoadOptions) *state {
	st, _ := s.ensure(id)
	prior := make(map[string]bool, st.thread.len())
	for _, m := range st.thread.all() {
		prior[m.ID] = true
	}
	st.hasOlder, st.msgLimit = opts.HasOlderMessages, opts.LoadedMessageLimit
	if opts.WorkingState != "" && st.working != opts.WorkingState {
		st.transition(opts.WorkingState)
	}
	st.merge(messages)
	if opts.Cwd != nil {
		st.setCwd(*opts.Cwd)
	}
	s.reconcileSubmits(st, messages, prior)
	s.replay(id)
	return st
}

// PrependOlderMessages merges a page of older messages (oldest first) from
// daemon.get_session_messages; the cursor of the next page is the first
// message's id while HasOlderMessages holds.
func (s *Store) PrependOlderMessages(id string, page []protocol.FactoryDroidMessage, hasOlder bool) {
	s.update(func() {
		st := s.sessions[id]
		if st == nil {
			return
		}
		s.load(id, page, LoadOptions{HasOlderMessages: hasOlder, LoadedMessageLimit: st.msgLimit})
	})
}

// LoadToken is taken before a daemon.load_session call; ApplyLoadResult uses
// it to tell state that changed while the call was out.
type LoadToken struct {
	SessionID string
	// WorkingStateRevision counts the live working-state notifications seen
	// before the call. When Session.WorkingStateRevision has moved past it,
	// a live notification is fresher than the load result's working state.
	WorkingStateRevision int
	summaryEpoch         int
	summaryRev           int
}

// BeginLoad marks a Session Loading (see MarkLoading) and returns the token
// for ApplyLoadResult.
func (s *Store) BeginLoad(id string) LoadToken {
	tok := LoadToken{SessionID: id}
	if !valid(id) {
		return tok
	}
	s.update(func() {
		st, _ := s.ensure(id)
		if st.load != Loaded {
			st.setLoadState(Loading, false)
		}
		tok.WorkingStateRevision = st.workingRevision
		tok.summaryEpoch, tok.summaryRev = s.summaryEpoch, s.summaryRev
	})
	return tok
}

// LoadOutcome is what ApplyLoadResult leaves to the controller.
type LoadOutcome struct {
	// ReportedWorkingState is the working state the result reported (the
	// agent loop flag counts as streaming). The controller applies it, or
	// WaitingForToolConfirmation when prompts are pending, through
	// ApplyLoadedWorkingState.
	ReportedWorkingState protocol.DroidWorkingState
	// Resubmit are queued messages the Daemon lost; send each again with
	// ResubmitParams and put those that fail back with
	// Session.RestoreQueuedMessagesToFront as KindLocalPausedAfterEsc.
	Resubmit []QueuedMessage
}

// ApplyLoadResult takes a daemon.load_session result: it merges the
// transcript, hydrates the working state unless a live notification
// overtook the call, and applies settings, token usage, model list, title,
// calling Session, subagent summaries and the Daemon's queue.
// messageLimit is the messageLimit the call asked for.
func (s *Store) ApplyLoadResult(tok LoadToken, res *protocol.LoadSessionResult, messageLimit int) LoadOutcome {
	out := LoadOutcome{ReportedWorkingState: reportedWorkingState(res)}
	if !valid(tok.SessionID) {
		return out
	}
	s.update(func() {
		opts := LoadOptions{
			Cwd:                &res.Cwd,
			HasOlderMessages:   res.HasOlderMessages != nil && *res.HasOlderMessages,
			LoadedMessageLimit: messageLimit,
		}
		if st := s.sessions[tok.SessionID]; st == nil || st.workingRevision == tok.WorkingStateRevision {
			opts.WorkingState = out.ReportedWorkingState
		}
		st := s.load(tok.SessionID, res.Session.Messages, opts)
		s.hydrateSummaries(res.SubagentInvocations, tok)
		st.applySessionSettings(res.Settings)
		if res.TokenUsage != nil {
			st.setUsage(res.TokenUsage, res.InclusiveTokenUsage)
		}
		if res.LastCallTokenUsage != nil {
			lc := LastCallTokenUsage(*res.LastCallTokenUsage)
			st.setLastCall(&lc)
		}
		out.Resubmit = st.reconcileQueue(res)
		if len(res.AvailableModels) > 0 {
			st.setModels(res.AvailableModels)
		}
		if res.Session.Title != "" {
			st.setTitle(res.Session.Title)
		}
		if res.DecompSessionType != "" {
			st.decomp = res.DecompSessionType
		}
		if res.CallingSessionID != "" || res.CallingToolUseID != "" {
			st.setCalling(res.CallingSessionID, res.CallingToolUseID)
		}
	})
	return out
}

func reportedWorkingState(res *protocol.LoadSessionResult) protocol.DroidWorkingState {
	if res.WorkingState != "" && res.WorkingState != protocol.DroidWorkingStateIdle {
		return res.WorkingState
	}
	if res.IsAgentLoopInProgress != nil && *res.IsAgentLoopInProgress {
		return protocol.DroidWorkingStateStreamingAssistantMessage
	}
	return protocol.DroidWorkingStateIdle
}

// ApplyLoadedWorkingState sets the working state a load settled on unless
// a live working-state notification arrived since BeginLoad; it reports
// whether it applied.
func (s *Store) ApplyLoadedWorkingState(tok LoadToken, ws protocol.DroidWorkingState) bool {
	applied := false
	s.update(func() {
		st := s.sessions[tok.SessionID]
		if st == nil || st.workingRevision != tok.WorkingStateRevision {
			return
		}
		st.transition(ws)
		applied = true
	})
	return applied
}

// ---- optimistic submits ----

// OptimisticSubmit shows a user message before the Daemon records it.
type OptimisticSubmit struct {
	SessionID string
	// ExternalKey identifies the submit; send it as the add_user_message
	// request id so the create_message echoing it confirms the submit.
	ExternalKey string
	UserMessage protocol.FactoryDroidMessage
	// AssistantBubbleID names the placeholder a UI shows for the reply.
	AssistantBubbleID string
	OnConfirm         func()
	// OnError is called with ErrSubmitTimeout when no confirmation comes in
	// time; the message stays shown.
	OnError      func(error)
	ErrorTimeout time.Duration // 20s when zero
}

type submit struct {
	sessionID string
	bubble    string
	onConfirm func()
	onError   func(error)
	timer     *time.Timer
	gen       int
}

// RegisterOptimisticSubmit shows the user message and the reply
// placeholder. Registering the same key and bubble again only refreshes
// the callbacks and the timeout.
func (s *Store) RegisterOptimisticSubmit(p OptimisticSubmit) {
	if !valid(p.SessionID) || p.ExternalKey == "" {
		return
	}
	s.update(func() {
		if cur := s.submits[p.ExternalKey]; cur != nil {
			if cur.bubble == p.AssistantBubbleID {
				if p.OnConfirm != nil {
					cur.onConfirm = p.OnConfirm
				}
				if p.OnError != nil {
					cur.onError = p.OnError
				}
				s.armSubmit(p.ExternalKey, cur, p.ErrorTimeout)
				return
			}
			s.cancelSubmit(p.ExternalKey)
		}
		st, _ := s.ensure(p.SessionID)
		st.addOptimistic(p.ExternalKey, []message{p.UserMessage}, nil)
		st.setBubble(p.AssistantBubbleID)
		sub := &submit{sessionID: p.SessionID, bubble: p.AssistantBubbleID, onConfirm: p.OnConfirm, onError: p.OnError}
		s.submits[p.ExternalKey] = sub
		s.armSubmit(p.ExternalKey, sub, p.ErrorTimeout)
	})
}

func (s *Store) armSubmit(key string, sub *submit, timeout time.Duration) {
	if timeout <= 0 {
		timeout = s.submitTimeout
	}
	if sub.timer != nil {
		sub.timer.Stop()
	}
	sub.gen++
	gen := sub.gen
	sub.timer = time.AfterFunc(timeout, func() {
		s.update(func() {
			if s.submits[key] != sub || sub.gen != gen || sub.onError == nil {
				return
			}
			onError := sub.onError
			s.after = append(s.after, func() { onError(ErrSubmitTimeout) })
		})
	})
}

// ConfirmOptimisticSubmit swaps a submit's stand-ins for the recorded
// message (HandleNotification does it on the matching create_message) and
// calls OnConfirm. It reports whether the submit was pending.
func (s *Store) ConfirmOptimisticSubmit(key string) bool {
	ok := false
	s.update(func() { ok = s.confirmSubmit(key) })
	return ok
}

func (s *Store) confirmSubmit(key string) bool {
	sub := s.submits[key]
	if sub == nil {
		return false
	}
	sub.timer.Stop()
	if st := s.sessions[sub.sessionID]; st != nil {
		st.removeOptimistic(key)
		st.clearQueued(key)
		st.rememberProcessed(key)
		if st.bubble == sub.bubble {
			st.setBubble("")
		}
	}
	delete(s.submits, key)
	if sub.onConfirm != nil {
		s.after = append(s.after, sub.onConfirm)
	}
	return true
}

// CancelOptimisticSubmit removes a submit's stand-ins without calling
// OnError (the send failed or was interrupted).
func (s *Store) CancelOptimisticSubmit(key string) bool {
	ok := false
	s.update(func() { ok = s.cancelSubmit(key) })
	return ok
}

func (s *Store) cancelSubmit(key string) bool {
	sub := s.submits[key]
	if sub == nil {
		return false
	}
	sub.timer.Stop()
	if st := s.sessions[sub.sessionID]; st != nil {
		st.removeOptimistic(key)
		if st.bubble == sub.bubble {
			st.setBubble("")
		}
	}
	delete(s.submits, key)
	return true
}

// CancelOptimisticSubmitsForSession cancels every submit of a Session and
// returns how many there were.
func (s *Store) CancelOptimisticSubmitsForSession(id string) int {
	n := 0
	s.update(func() {
		for key, sub := range s.submits {
			if sub.sessionID == id && s.cancelSubmit(key) {
				n++
			}
		}
	})
	return n
}

// HasPendingOptimisticSubmit reports whether a Session has an unconfirmed
// submit (the client is about to create it, so loading it would fail).
func (s *Store) HasPendingOptimisticSubmit(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.submits {
		if sub.sessionID == id {
			return true
		}
	}
	return false
}

// reconcileSubmits confirms submits whose create_message was lost: a load
// brought a new user message with the same text. Only messages new to the
// Session count, so an earlier turn with the same text cannot confirm a
// resubmit still in flight.
func (s *Store) reconcileSubmits(st *state, loaded []message, prior map[string]bool) {
	var keys []string
	for key, sub := range s.submits {
		if sub.sessionID == st.id {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return
	}
	var fresh []message
	for _, m := range loaded {
		if m.Role == roleUser && !prior[m.ID] {
			fresh = append(fresh, m)
		}
	}
	slices.SortStableFunc(fresh, func(a, b message) int {
		switch {
		case a.CreatedAt < b.CreatedAt:
			return -1
		case a.CreatedAt > b.CreatedAt:
			return 1
		}
		return 0
	})
	texts := map[string]bool{}
	for _, m := range fresh[max(0, len(fresh)-len(keys)):] {
		if t := userText(m); t != "" {
			texts[t] = true
		}
	}
	for _, key := range keys {
		if e, ok := st.optimistic[key]; ok {
			if t := userText(e.messages[0]); t != "" && texts[t] {
				s.confirmSubmit(key)
			}
		}
	}
}

func userText(m message) string {
	var b strings.Builder
	for _, c := range m.Content {
		if c.Type == blockText {
			b.WriteString(decode[textView](c).Text)
		}
	}
	return b.String()
}

// ---- subagents ----

// RegisterChildSession links a subagent Session to the Session and Task
// tool use that spawned it, as soon as its id is known, and shows it
// working until it is loaded (HandleNotification does it on
// child_session_available).
func (s *Store) RegisterChildSession(parentID, childID, toolUseID string) {
	s.update(func() { s.registerChild(parentID, childID, toolUseID) })
}

func (s *Store) registerChild(parentID, childID, toolUseID string) {
	if !valid(parentID) || !valid(childID) || parentID == childID {
		return
	}
	parentCwd := ""
	if p := s.sessions[parentID]; p != nil {
		parentCwd = p.cwd
	}
	prev := s.sessions[childID]
	seed := prev == nil || prev.load != Loaded && prev.working == protocol.DroidWorkingStateIdle
	st, _ := s.ensure(childID)
	callingSession, callingTool := "", ""
	if st.callingSessionID == "" {
		callingSession = parentID
	}
	if toolUseID != "" && st.callingToolUseID == "" {
		callingTool = toolUseID
	}
	if callingSession != "" || callingTool != "" {
		st.setCalling(callingSession, callingTool)
	}
	if parentCwd != "" && st.cwd == "" {
		st.setCwd(parentCwd)
	}
	if seed {
		st.transition(protocol.DroidWorkingStateStreamingAssistantMessage)
	}
	s.replay(childID)
}

func (s *Store) childSessionAvailable(parentID string, n *protocol.ChildSessionAvailableNotification) {
	s.registerChild(parentID, n.ChildSessionID, n.ToolUseID)
	existing, ok := s.summaries[n.ChildSessionID]
	switch {
	case !ok && n.SubagentType != "" && n.Description != "":
		s.setSummary(protocol.SubagentInvocationSummary{
			ChildSessionID: n.ChildSessionID, Status: protocol.TaskInvocationStatusRunning,
			SubagentType: n.SubagentType, Description: n.Description,
		})
	case ok && terminalStatus(existing.Status):
		existing.Status = protocol.TaskInvocationStatusRunning
		s.setSummary(existing)
	}
}

// FindSubagentSessionID returns the child Session a parent's tool use
// spawned, when it is registered.
func (s *Store) FindSubagentSessionID(parentID, toolUseID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, st := range s.sessions {
		if st.callingSessionID == parentID && st.callingToolUseID == toolUseID {
			return id, true
		}
	}
	return "", false
}

// SubagentSessionIDsByParent maps each parent Session to its children by
// the tool use that spawned them.
func (s *Store) SubagentSessionIDsByParent() map[string]map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]map[string]string{}
	for id, st := range s.sessions {
		if st.callingSessionID == "" || st.callingToolUseID == "" {
			continue
		}
		m := out[st.callingSessionID]
		if m == nil {
			m = map[string]string{}
			out[st.callingSessionID] = m
		}
		if _, dup := m[st.callingToolUseID]; !dup {
			m[st.callingToolUseID] = id
		}
	}
	return out
}

// SetSubagentInvocationSummary records a subagent's tool-use summary,
// which outlives the child Session.
func (s *Store) SetSubagentInvocationSummary(sum protocol.SubagentInvocationSummary) {
	if !valid(sum.ChildSessionID) {
		return
	}
	s.update(func() { s.setSummary(sum) })
}

func (s *Store) setSummary(sum protocol.SubagentInvocationSummary) {
	s.summaries[sum.ChildSessionID] = sum
	s.summaryRev++
	s.summaryModified[sum.ChildSessionID] = s.summaryRev
	s.emit(Event{Kind: EventSubagentSummaryUpdated, SessionID: sum.ChildSessionID})
}

// SubagentInvocationSummary returns a child Session's summary.
func (s *Store) SubagentInvocationSummary(childID string) (protocol.SubagentInvocationSummary, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sum, ok := s.summaries[childID]
	return sum, ok
}

// hydrateSummaries takes the summaries a load reported, except those
// updated live since the load began.
func (s *Store) hydrateSummaries(sums []protocol.SubagentInvocationSummary, tok LoadToken) {
	if tok.summaryEpoch != s.summaryEpoch {
		return
	}
	for _, sum := range sums {
		if valid(sum.ChildSessionID) && s.summaryModified[sum.ChildSessionID] <= tok.summaryRev {
			s.setSummary(sum)
		}
	}
}

func terminalStatus(st protocol.TaskInvocationStatus) bool {
	return st == protocol.TaskInvocationStatusCompleted || st == protocol.TaskInvocationStatusFailed || st == protocol.TaskInvocationStatusCancelled
}

func statusForCompletion(r protocol.AgentTurnCompletionReason) protocol.TaskInvocationStatus {
	switch r {
	case protocol.AgentTurnCompletionReasonCompleted, protocol.AgentTurnCompletionReasonSpecHandoff:
		return protocol.TaskInvocationStatusCompleted
	case protocol.AgentTurnCompletionReasonCancelled, protocol.AgentTurnCompletionReasonProcessExit:
		return protocol.TaskInvocationStatusCancelled
	}
	return protocol.TaskInvocationStatusFailed
}

// updateSummaryFromState settles a subagent's summary when its turn ends.
func (s *Store) updateSummaryFromState(st *state, r protocol.AgentTurnCompletionReason) {
	sum, ok := s.summaries[st.id]
	if !ok {
		return
	}
	sum.Status = statusForCompletion(r)
	if msgs := st.thread.all(); len(msgs) > 0 {
		count, duration := summarizeToolUsage(msgs)
		sum.ToolUseCount = &count
		if duration > 0 {
			sum.DurationMs = &duration
		}
	}
	s.setSummary(sum)
}

func summarizeToolUsage(msgs []message) (count, durationMs float64) {
	var first, last float64
	for _, m := range msgs {
		ts := m.UpdatedAt
		if ts == 0 {
			ts = m.CreatedAt
		}
		if ts != 0 {
			if first == 0 {
				first = ts
			}
			last = ts
		}
		if m.Role == roleAssistant {
			for _, b := range m.Content {
				if b.Type == blockToolUse {
					count++
				}
			}
		}
	}
	return count, last - first
}
