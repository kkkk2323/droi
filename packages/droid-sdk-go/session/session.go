package session

import (
	"slices"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// Session is a handle on one Session of a Store (the TypeScript
// SessionStateManager). It looks the Session up on every call: once the
// Session is removed, getters return zero values and mutators do nothing.
// Returned slices and message Content are copies; the bytes of content
// blocks and the other slices and pointers inside messages and settings are
// shared and must not be modified.
type Session struct {
	s  *Store
	id string
}

func (h *Session) read(fn func(st *state)) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	if st := h.s.sessions[h.id]; st != nil {
		fn(st)
	}
}

func (h *Session) update(fn func(st *state)) {
	h.s.update(func() {
		if st := h.s.sessions[h.id]; st != nil {
			fn(st)
		}
	})
}

// ID is the Session id.
func (h *Session) ID() string { return h.id }

// LoadState is where the Session is in being loaded.
func (h *Session) LoadState() (ls LoadState) {
	ls = NotLoaded
	h.read(func(st *state) { ls = st.load })
	return ls
}

// WorkingState is what the agent is doing.
func (h *Session) WorkingState() (ws protocol.DroidWorkingState) {
	ws = protocol.DroidWorkingStateIdle
	h.read(func(st *state) { ws = st.working })
	return ws
}

// WorkingStateChangedAt is when WorkingState last changed.
func (h *Session) WorkingStateChangedAt() (t time.Time) {
	h.read(func(st *state) { t = st.workingChangedAt })
	return t
}

// WorkingStateRevision counts the droid_working_state_changed
// notifications applied (see LoadToken).
func (h *Session) WorkingStateRevision() (n int) {
	h.read(func(st *state) { n = st.workingRevision })
	return n
}

// Messages returns every stored message in transcript order, unfiltered.
func (h *Session) Messages() (msgs []protocol.FactoryDroidMessage) {
	h.read(func(st *state) { msgs = st.thread.list() })
	return msgs
}

// Message returns one stored message.
func (h *Session) Message(id string) (m protocol.FactoryDroidMessage, ok bool) {
	h.read(func(st *state) {
		if m, ok = st.thread.get(id); ok {
			m.Content = slices.Clone(m.Content)
		}
	})
	return m, ok
}

// MessageCount is the number of stored messages.
func (h *Session) MessageCount() (n int) {
	h.read(func(st *state) { n = st.thread.len() })
	return n
}

// DisplayMessages returns what a transcript shows: the stored messages
// through FilterMessagesForUI with the optimistic ones merged in.
func (h *Session) DisplayMessages() (msgs []protocol.FactoryDroidMessage) {
	h.read(func(st *state) { msgs = st.mergeOptimistic(FilterMessagesForUI(st.thread.list())) })
	return msgs
}

// IsConversationEmpty reports whether there is no message, stored or
// optimistic.
func (h *Session) IsConversationEmpty() (empty bool) {
	empty = true
	h.read(func(st *state) { empty = st.thread.len() == 0 && len(st.optimistic) == 0 })
	return empty
}

// HasOlderMessages reports whether the Daemon holds messages before the
// first loaded one.
func (h *Session) HasOlderMessages() (b bool) {
	h.read(func(st *state) { b = st.hasOlder })
	return b
}

// OlderMessagesCursor is the get_session_messages cursor of the next older
// page, when there is one.
func (h *Session) OlderMessagesCursor() (cursor string, ok bool) {
	h.read(func(st *state) {
		if st.hasOlder && len(st.thread.order) > 0 {
			cursor, ok = st.thread.order[0], true
		}
	})
	return cursor, ok
}

// LoadedMessageLimit is the messageLimit of the last load.
func (h *Session) LoadedMessageLimit() (n int) {
	h.read(func(st *state) { n = st.msgLimit })
	return n
}

// Title is the Session title.
func (h *Session) Title() (t string) {
	h.read(func(st *state) { t = st.title })
	return t
}

// Cwd is the directory the Session works in.
func (h *Session) Cwd() (cwd string) {
	h.read(func(st *state) { cwd = st.cwd })
	return cwd
}

// Settings returns the Session settings.
func (h *Session) Settings() (s Settings) {
	h.read(func(st *state) {
		s = st.settings
		s.Tags = slices.Clone(s.Tags)
	})
	return s
}

// AvailableModels is the model list the Daemon sent with the Session.
func (h *Session) AvailableModels() (models []protocol.ModelMetadata) {
	h.read(func(st *state) { models = slices.Clone(st.models) })
	return models
}

// Usage returns the Session's token usage.
func (h *Session) Usage() (u Usage) {
	h.read(func(st *state) {
		u.Session, u.Inclusive = copyUsage(st.usage.Session), copyUsage(st.usage.Inclusive)
		if lc := st.usage.LastCall; lc != nil {
			c := *lc
			u.LastCall = &c
		}
	})
	return u
}

// Stats is the Session's model speed and time, as this Store saw it
// stream, on top of any SetStatsSeed gave it.
func (h *Session) Stats() (s Stats) {
	h.read(func(st *state) { s = st.stats.totals.clone() })
	return s
}

// Todos is the current task list; nil when the agent wrote none.
func (h *Session) Todos() (todos []TodoItem) {
	h.read(func(st *state) { todos = slices.Clone(st.todos.current) })
	return todos
}

// QueuedMessages returns the queued user messages, oldest first.
func (h *Session) QueuedMessages() (q []QueuedMessage) {
	h.read(func(st *state) { q = cloneQueued(st.queued) })
	return q
}

// QueuedMessage returns one queued message.
func (h *Session) QueuedMessage(requestID string) (q QueuedMessage, ok bool) {
	h.read(func(st *state) {
		if i := st.indexOfQueued(requestID); i >= 0 {
			q, ok = cloneQueued(st.queued[i : i+1])[0], true
		}
	})
	return q, ok
}

func cloneQueued(qs []QueuedMessage) []QueuedMessage {
	out := slices.Clone(qs)
	for i := range out {
		out[i].Content = slices.Clone(out[i].Content)
	}
	return out
}

// OptimisticMessage returns the first message of an optimistic entry.
func (h *Session) OptimisticMessage(requestID string) (m protocol.FactoryDroidMessage, ok bool) {
	h.read(func(st *state) {
		var e optimisticEntry
		if e, ok = st.optimistic[requestID]; ok {
			m = cloneMessages(e.messages[:1])[0]
		}
	})
	return m, ok
}

// AssistantBubbleID is the reply placeholder of the submit in flight, or
// "".
func (h *Session) AssistantBubbleID() (id string) {
	h.read(func(st *state) { id = st.bubble })
	return id
}

// CallingSession is the Session and tool use that spawned this subagent
// Session; empty for a top-level one.
func (h *Session) CallingSession() (sessionID, toolUseID string) {
	h.read(func(st *state) { sessionID, toolUseID = st.callingSessionID, st.callingToolUseID })
	return sessionID, toolUseID
}

// DecompSessionType is the load result's decompSessionType.
func (h *Session) DecompSessionType() (t protocol.DecompSessionType) {
	h.read(func(st *state) { t = st.decomp })
	return t
}

// AgentTurnCompletionReason is how the last turn ended; "" while a turn
// runs or before the first one ends.
func (h *Session) AgentTurnCompletionReason() (r protocol.AgentTurnCompletionReason) {
	h.read(func(st *state) { r = st.completion })
	return r
}

// LLMRetry is the model call being retried, or nil.
func (h *Session) LLMRetry() (r *LLMRetry) {
	h.read(func(st *state) {
		if st.retry != nil {
			c := *st.retry
			r = &c
		}
	})
	return r
}

// ToolPhase is a tool call's execution phase, or "" when unknown.
func (h *Session) ToolPhase(toolUseID string) (p protocol.ToolExecutionLifecyclePhase) {
	h.read(func(st *state) { p = st.phases[toolUseID] })
	return p
}

// ToolProgress returns a tool call's progress updates; they are dropped a
// minute after its result.
func (h *Session) ToolProgress(toolUseID string) (u []protocol.ToolProgressUpdate) {
	h.read(func(st *state) { u = slices.Clone(st.progress[toolUseID]) })
	return u
}

// SetWorkingState applies a working state as a live notification would
// (the controller's startStreaming, setWaitingForConfirmation, ...), without
// counting as one in WorkingStateRevision.
func (h *Session) SetWorkingState(ws protocol.DroidWorkingState) {
	h.update(func(st *state) { st.transition(ws) })
}

// SetTitle sets the title.
func (h *Session) SetTitle(title string) { h.update(func(st *state) { st.setTitle(title) }) }

// SetCwd sets the working directory.
func (h *Session) SetCwd(cwd string) { h.update(func(st *state) { st.setCwd(cwd) }) }

// SetAvailableModels sets the model list.
func (h *Session) SetAvailableModels(models []protocol.ModelMetadata) {
	h.update(func(st *state) { st.setModels(models) })
}

// SetCallingSession links a subagent Session to the Session and tool use
// that spawned it; an empty argument keeps its current value.
func (h *Session) SetCallingSession(sessionID, toolUseID string) {
	h.update(func(st *state) { st.setCalling(sessionID, toolUseID) })
}

// SetAgentTurnCompletionReason records how the last turn ended.
func (h *Session) SetAgentTurnCompletionReason(r protocol.AgentTurnCompletionReason) {
	h.update(func(st *state) { st.setCompletion(r) })
}

// QueueUserMessages queues messages (an add_user_message sent while the
// agent works), replacing one with the same request id in place. A request
// id a create_message already confirmed is ignored, and an optimistic
// message under the same id is dropped.
func (h *Session) QueueUserMessages(msgs ...QueuedMessage) {
	h.update(func(st *state) { st.queueMessages(cloneQueued(msgs)) })
}

// ReplaceDaemonQueuedMessages swaps the Daemon-held part of the queue,
// keeping the local entries.
func (h *Session) ReplaceDaemonQueuedMessages(msgs []QueuedMessage) {
	h.update(func(st *state) { st.replaceDaemonQueued(cloneQueued(msgs)) })
}

// ClearQueuedMessage removes one queued message.
func (h *Session) ClearQueuedMessage(requestID string) {
	h.update(func(st *state) { st.clearQueued(requestID) })
}

// ClearQueuedMessages removes the queued messages of the given kinds, or
// all of them.
func (h *Session) ClearQueuedMessages(kinds ...QueuedMessageKind) {
	h.update(func(st *state) { st.clearQueuedKinds(kinds) })
}

// DequeueQueuedMessages removes and returns the queued messages of a kind
// ("" for any), oldest first; at most limit when limit > 0.
func (h *Session) DequeueQueuedMessages(kind QueuedMessageKind, limit int) (out []QueuedMessage) {
	h.update(func(st *state) { out = st.dequeue(kind, limit) })
	return out
}

// RestoreQueuedMessagesToFront puts messages back at the head of the
// queue in the given order.
func (h *Session) RestoreQueuedMessagesToFront(msgs []QueuedMessage) {
	h.update(func(st *state) { st.restoreToFront(cloneQueued(msgs)) })
}

// PauseDaemonQueuedMessagesAfterEsc keeps on this Client what an
// interrupt makes the Daemon drop (HandleNotification does it on
// queued_messages_discarded).
func (h *Session) PauseDaemonQueuedMessagesAfterEsc(restoredRequestID string) {
	h.update(func(st *state) { st.pauseDaemonQueued(restoredRequestID) })
}

// MarkRequestIDProcessed records that a request id was delivered, so a
// late QueueUserMessages for it is ignored.
func (h *Session) MarkRequestIDProcessed(requestID string) {
	h.update(func(st *state) { st.rememberProcessed(requestID) })
}

// AddOptimisticMessages shows messages under a request id until a
// create_message with that id records them; a staged exchange stays
// together. leadingOrder, when set, renders them ahead of the transcript
// at that position among other leading messages (session-launch steps).
func (h *Session) AddOptimisticMessages(requestID string, msgs []protocol.FactoryDroidMessage, leadingOrder *int) {
	h.update(func(st *state) { st.addOptimistic(requestID, cloneMessages(msgs), leadingOrder) })
}

// RemoveOptimisticMessage removes an optimistic entry.
func (h *Session) RemoveOptimisticMessage(requestID string) {
	h.update(func(st *state) { st.removeOptimistic(requestID) })
}
