package session

import (
	"bytes"
	"encoding/json"
	"slices"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// LoadState is where a Session is in being loaded from the Daemon.
type LoadState string

const (
	NotLoaded LoadState = "NOT_LOADED"
	Loading   LoadState = "LOADING"
	Loaded    LoadState = "LOADED"
)

// Settings are a Session's settings as the Daemon last reported them.
type Settings struct {
	ModelID                         string
	ReasoningEffort                 protocol.ReasoningEffort
	InteractionMode                 protocol.DroidInteractionMode
	AutonomyLevel                   protocol.AutonomyLevel
	SpecModeModelID                 string
	SpecModeReasoningEffort         protocol.ReasoningEffort
	MissionSettings                 *protocol.MissionModelSettings
	Tags                            []protocol.SessionTag
	Sandbox                         *protocol.SandboxStatus
	CompactionThresholdCheckEnabled *bool
}

// LastCallTokenUsage is the size of the latest model call; a context meter
// reads it.
type LastCallTokenUsage struct {
	InputTokens     float64
	CacheReadTokens float64
	OutputTokens    *float64
}

// Usage is a Session's token usage.
type Usage struct {
	Session *protocol.TokenUsage
	// Inclusive is Session rolled up with every subagent it spawned, which
	// is billed to it; nil when the Daemon reported none.
	Inclusive *protocol.TokenUsage
	LastCall  *LastCallTokenUsage
}

// InclusiveOrOwn is the usage a cost readout shows.
func (u Usage) InclusiveOrOwn() *protocol.TokenUsage {
	if u.Inclusive != nil {
		return u.Inclusive
	}
	return u.Session
}

// LLMRetry describes a model call the agent is retrying (overload,
// throttling); present only until the agent makes progress again.
type LLMRetry struct {
	Attempt int
	Reason  protocol.LLMRetryReason
}

// state is one Session; the Store's mutex guards it.
type state struct {
	s  *Store
	id string

	load     LoadState
	thread   thread
	hasOlder bool
	msgLimit int

	working          protocol.DroidWorkingState
	workingChangedAt time.Time
	workingRevision  int

	pendingAssistant string
	orphans          map[string]message
	streamingIDs     map[string]bool
	thinkStart       map[blockKey]time.Time
	thinkIndex       map[blockKey]int

	todos todoState

	queued         []QueuedMessage
	processed      map[string]bool
	processedOrder []string

	optimistic       map[string]optimisticEntry
	optimisticSeq    int
	confirmedLeading map[string]bool
	bubble           string

	settings         Settings
	models           []protocol.ModelMetadata
	title, cwd       string
	usage            Usage
	callingSessionID string
	callingToolUseID string
	decomp           protocol.DecompSessionType
	completion       protocol.AgentTurnCompletionReason
	retry            *LLMRetry

	phases         map[string]protocol.ToolExecutionLifecyclePhase
	progress       map[string][]protocol.ToolProgressUpdate
	progressTimers map[string]*time.Timer
}

func newState(s *Store, id string) *state {
	return &state{
		s: s, id: id,
		load:             NotLoaded,
		thread:           newThread(),
		working:          protocol.DroidWorkingStateIdle,
		workingChangedAt: s.now(),
		orphans:          map[string]message{},
		streamingIDs:     map[string]bool{},
		thinkStart:       map[blockKey]time.Time{},
		thinkIndex:       map[blockKey]int{},
		todos:            newTodoState(),
		processed:        map[string]bool{},
		optimistic:       map[string]optimisticEntry{},
		confirmedLeading: map[string]bool{},
		phases:           map[string]protocol.ToolExecutionLifecyclePhase{},
		progress:         map[string][]protocol.ToolProgressUpdate{},
		progressTimers:   map[string]*time.Timer{},
	}
}

func (st *state) nowMs() float64 { return float64(st.s.now().UnixMilli()) }

func (st *state) emit(kind EventKind) { st.s.emit(Event{Kind: kind, SessionID: st.id}) }

func (st *state) emitMessages(streaming bool) {
	st.s.emit(Event{Kind: EventMessagesUpdated, SessionID: st.id, Streaming: streaming})
}

func (st *state) setLoadState(ls LoadState, preserveWorking bool) {
	if st.load != ls {
		st.load = ls
		st.s.emit(Event{Kind: EventLoadStateChanged, SessionID: st.id, LoadState: ls})
	}
	if ls == NotLoaded && !preserveWorking && st.working != protocol.DroidWorkingStateIdle {
		st.transition(protocol.DroidWorkingStateIdle)
	}
}

func (st *state) setWorking(ws protocol.DroidWorkingState) {
	if st.working == ws {
		return
	}
	st.working = ws
	st.workingChangedAt = st.s.now()
}

// transition moves the working state the way a live notification does:
// leaving a turn (idle, a tool, a prompt) ends any block still streaming,
// and starting work forgets the last turn's completion reason.
func (st *state) transition(ws protocol.DroidWorkingState) {
	switch ws {
	case protocol.DroidWorkingStateIdle, protocol.DroidWorkingStateExecutingTool, protocol.DroidWorkingStateWaitingForToolConfirmation:
		st.completeStreamingBlocks()
	case protocol.DroidWorkingStateThinking, protocol.DroidWorkingStateStreamingAssistantMessage, protocol.DroidWorkingStateCompactingConversation:
	default:
		return
	}
	if ws != protocol.DroidWorkingStateIdle {
		st.setCompletion("")
	}
	if st.working != ws {
		st.setWorking(ws)
		st.emit(EventWorkingStateChanged)
	}
}

func (st *state) setCompletion(r protocol.AgentTurnCompletionReason) {
	if st.completion == r {
		return
	}
	st.completion = r
	st.emit(EventMetadataUpdated)
}

func (st *state) setTitle(t string) {
	st.title = t
	st.emit(EventMetadataUpdated)
}

func (st *state) setCwd(cwd string) {
	st.cwd = cwd
	st.emit(EventMetadataUpdated)
}

func (st *state) setCalling(sessionID, toolUseID string) {
	if sessionID != "" {
		st.callingSessionID = sessionID
	}
	if toolUseID != "" {
		st.callingToolUseID = toolUseID
	}
	st.emit(EventMetadataUpdated)
}

func (st *state) setModels(models []protocol.ModelMetadata) {
	st.models = slices.Clone(models)
	st.emit(EventSettingsUpdated)
}

func (st *state) setRetry(r *LLMRetry) {
	if r == nil && st.retry == nil {
		return
	}
	st.retry = r
	st.emit(EventMetadataUpdated)
}

func copyUsage(u *protocol.TokenUsage) *protocol.TokenUsage {
	if u == nil {
		return nil
	}
	c := *u
	return &c
}

func (st *state) setUsage(own, inclusive *protocol.TokenUsage) {
	st.usage.Session, st.usage.Inclusive = copyUsage(own), copyUsage(inclusive)
	st.emit(EventMetadataUpdated)
}

func (st *state) setLastCall(lc *LastCallTokenUsage) {
	st.usage.LastCall = lc
	st.emit(EventMetadataUpdated)
}

// applyInteraction resolves interaction mode and autonomy level together:
// one given alone keeps the other, defaulting to auto / off.
func (st *state) applyInteraction(mode protocol.DroidInteractionMode, level protocol.AutonomyLevel) {
	if mode == "" && level == "" {
		return
	}
	if mode == "" {
		mode = st.settings.InteractionMode
		if mode == "" {
			mode = "auto"
		}
	}
	if level == "" {
		level = st.settings.AutonomyLevel
		if level == "" {
			level = "off"
		}
	}
	st.settings.InteractionMode, st.settings.AutonomyLevel = mode, level
}

func (st *state) applySettingsUpdate(p protocol.SettingsUpdatedPayload) {
	if p.ModelID != "" {
		st.settings.ModelID = p.ModelID
	}
	if p.ReasoningEffort != "" {
		st.settings.ReasoningEffort = p.ReasoningEffort
	}
	st.applyInteraction(p.InteractionMode, p.AutonomyLevel)
	st.settings.SpecModeModelID = p.SpecModeModelID
	st.settings.SpecModeReasoningEffort = p.SpecModeReasoningEffort
	st.settings.MissionSettings = p.MissionSettings
	if p.Tags != nil {
		st.settings.Tags = slices.Clone(p.Tags)
	}
	if p.CompactionThresholdCheckEnabled != nil {
		st.settings.CompactionThresholdCheckEnabled = p.CompactionThresholdCheckEnabled
	}
	st.emit(EventSettingsUpdated)
}

// applySessionSettings takes the settings of a load or initialize result.
func (st *state) applySessionSettings(p protocol.SessionSettings) {
	if p.ModelID != "" {
		st.settings.ModelID = p.ModelID
	}
	if p.ReasoningEffort != "" {
		st.settings.ReasoningEffort = p.ReasoningEffort
	}
	st.applyInteraction(p.InteractionMode, p.AutonomyLevel)
	if p.SpecModeModelID != "" {
		st.settings.SpecModeModelID = p.SpecModeModelID
	}
	if p.SpecModeReasoningEffort != "" {
		st.settings.SpecModeReasoningEffort = p.SpecModeReasoningEffort
	}
	st.settings.MissionSettings = p.MissionSettings
	if p.Tags != nil {
		st.settings.Tags = slices.Clone(p.Tags)
	}
	if p.CompactionThresholdCheckEnabled != nil {
		st.settings.CompactionThresholdCheckEnabled = p.CompactionThresholdCheckEnabled
	}
	if p.Sandbox != nil {
		st.settings.Sandbox = p.Sandbox
	}
	st.emit(EventSettingsUpdated)
}

// ---- tool phases and progress ----

func phaseRank(p protocol.ToolExecutionLifecyclePhase) int {
	switch p {
	case protocol.ToolExecutionLifecyclePhaseQueued:
		return 1
	case protocol.ToolExecutionLifecyclePhaseExecuting:
		return 2
	case protocol.ToolExecutionLifecyclePhaseSettledAfterExecution,
		protocol.ToolExecutionLifecyclePhaseSettledWithoutExecution,
		protocol.ToolExecutionLifecyclePhaseSettledUnknown:
		return 3
	}
	return 0
}

func settlePhase(p protocol.ToolExecutionLifecyclePhase) protocol.ToolExecutionLifecyclePhase {
	switch p {
	case protocol.ToolExecutionLifecyclePhaseSettledAfterExecution,
		protocol.ToolExecutionLifecyclePhaseSettledWithoutExecution,
		protocol.ToolExecutionLifecyclePhaseSettledUnknown:
		return p
	case protocol.ToolExecutionLifecyclePhaseExecuting:
		return protocol.ToolExecutionLifecyclePhaseSettledAfterExecution
	case protocol.ToolExecutionLifecyclePhaseStreamingInput, protocol.ToolExecutionLifecyclePhaseQueued:
		return protocol.ToolExecutionLifecyclePhaseSettledWithoutExecution
	}
	return protocol.ToolExecutionLifecyclePhaseSettledUnknown
}

// setPhase moves a tool's phase forward only; a late notification for an
// earlier phase is ignored.
func (st *state) setPhase(toolUseID string, p protocol.ToolExecutionLifecyclePhase, notify bool) {
	cur := st.phases[toolUseID]
	next := cur
	if cur == "" || p != "" && phaseRank(p) > phaseRank(cur) {
		next = p
	}
	if next == cur || next == "" {
		return
	}
	st.phases[toolUseID] = next
	if notify {
		st.emitMessages(false)
	}
}

func progressEqual(a, b protocol.ToolProgressUpdate) bool {
	a.Timestamp, b.Timestamp = nil, nil
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func (st *state) addProgress(toolUseID string, u protocol.ToolProgressUpdate) {
	updates := st.progress[toolUseID]
	if len(updates) > 0 && progressEqual(updates[len(updates)-1], u) {
		return
	}
	st.progress[toolUseID] = append(slices.Clip(updates), u)
	if t := st.progressTimers[toolUseID]; t != nil {
		t.Stop()
		delete(st.progressTimers, toolUseID)
	}
	st.emitMessages(true)
}

// scheduleProgressCleanup drops a finished tool's progress after a while,
// so long Sessions do not keep every tool's streamed output.
func (st *state) scheduleProgressCleanup(toolUseID string) {
	if t := st.progressTimers[toolUseID]; t != nil {
		t.Stop()
	}
	s := st.s
	var t *time.Timer
	t = time.AfterFunc(s.progressTTL, func() {
		s.update(func() {
			if s.sessions[st.id] != st || st.progressTimers[toolUseID] != t {
				return
			}
			delete(st.progressTimers, toolUseID)
			if _, ok := st.progress[toolUseID]; ok {
				delete(st.progress, toolUseID)
				st.emitMessages(true)
			}
		})
	})
	st.progressTimers[toolUseID] = t
}

func (st *state) stopTimers() {
	for id, t := range st.progressTimers {
		t.Stop()
		delete(st.progressTimers, id)
	}
}

// ---- loading ----

// resetMessages replaces the transcript and the state derived from it.
func (st *state) resetMessages(msgs []message) {
	st.clearThinkingTracking()
	st.todos.rebuild(msgs)
	st.emit(EventTodosUpdated)
	st.thread.reset(msgs)
	st.stopTimers()
	clear(st.progress)
	clear(st.phases)
	st.setRetry(nil)
	st.emitMessages(false)
}

func (st *state) initialize(msgs []message) {
	st.hasOlder, st.msgLimit = false, 0
	st.pendingAssistant = ""
	clear(st.streamingIDs)
	st.resetMessages(cloneMessages(msgs))
	st.setLoadState(Loaded, false)
}

// merge folds loaded messages into what the Session already holds; a
// loaded copy replaces a cached one.
func (st *state) merge(raw []message) {
	msgs := repairParentChain(cloneMessages(raw))
	cached := st.thread.all()
	byID := make(map[string]message, len(cached)+len(msgs))
	var order []string
	for _, m := range append(cached, msgs...) {
		if _, ok := byID[m.ID]; !ok {
			order = append(order, m.ID)
		}
		byID[m.ID] = m
	}
	merged := make([]message, 0, len(order))
	for _, id := range order {
		merged = append(merged, byID[id])
	}
	st.resetMessages(orderMerged(merged))
	st.setLoadState(Loaded, false)
	if len(cached) == 0 && st.bubble != "" && slices.ContainsFunc(msgs, func(m message) bool { return m.Role == roleAssistant }) {
		st.setBubble("")
	}
}

// ---- notifications ----

// retryClearing are the notifications that show the agent made progress.
var retryClearing = map[string]bool{
	"assistant_text_delta": true, "thinking_text_delta": true, "create_message": true,
	"tool_call": true, "tool_result": true, "tool_progress_update": true,
	"tool_execution_phase_changed": true, "droid_working_state_changed": true,
	"agent_turn_completed": true, "error": true,
}

func (st *state) handle(typ string, v any, opts HandleOptions) {
	if retryClearing[typ] {
		st.setRetry(nil)
	}
	switch n := v.(type) {
	case *protocol.CreateMessageNotification:
		st.createMessage(n)
	case *protocol.ToolResultNotification:
		st.toolResult(n.MessageID, n)
	case *protocol.ToolCallNotification:
		st.toolCall(n.ToolUse)
	case *protocol.ToolProgressUpdateNotification:
		st.setPhase(n.ToolUseID, protocol.ToolExecutionLifecyclePhaseExecuting, false)
		st.addProgress(n.ToolUseID, n.Update)
	case *protocol.ToolExecutionPhaseChangedNotification:
		st.setPhase(n.ToolUseID, n.Phase, true)
	case *protocol.AssistantTextDeltaNotification:
		if st.working == protocol.DroidWorkingStateThinking {
			st.setWorking(protocol.DroidWorkingStateStreamingAssistantMessage)
			st.emit(EventWorkingStateChanged)
		}
		st.textDelta(n.MessageID, n.TextDelta)
	case *protocol.ThinkingTextDeltaNotification:
		if st.working == protocol.DroidWorkingStateStreamingAssistantMessage {
			st.setWorking(protocol.DroidWorkingStateThinking)
			st.emit(EventWorkingStateChanged)
		}
		st.thinkingDelta(n.MessageID, int(n.BlockIndex), n.TextDelta)
	case *protocol.AssistantTextCompleteNotification:
		st.completeText(n.MessageID, int(n.BlockIndex))
	case *protocol.ThinkingTextCompleteNotification:
		st.completeThinking(n.MessageID, int(n.BlockIndex), n.DurationMs)
	case *protocol.AssistantMessageRetractedNotification:
		st.retract(n.MessageID)
	case *protocol.DroidWorkingStateChangedNotification:
		st.workingRevision++
		st.transition(n.NewState)
	case *protocol.PermissionResolvedNotification:
		if st.working == protocol.DroidWorkingStateWaitingForToolConfirmation {
			st.transition(protocol.DroidWorkingStateStreamingAssistantMessage)
		}
	case *protocol.ErrorNotification:
		st.transition(protocol.DroidWorkingStateIdle)
		// A worker that exits after finishing its turn reports a process
		// exit error; that must not turn a completed turn into a failure.
		selfExit := n.ErrorType == protocol.DroidErrorTypeProcessExitError &&
			st.completion != "" && st.completion != protocol.AgentTurnCompletionReasonError
		if !selfExit {
			st.setCompletion(protocol.AgentTurnCompletionReasonError)
			st.s.updateSummaryFromState(st, protocol.AgentTurnCompletionReasonError)
		}
	case *protocol.AgentTurnCompletedNotification:
		st.setCompletion(n.Reason)
		st.s.updateSummaryFromState(st, n.Reason)
	case *protocol.SettingsUpdatedNotification:
		st.applySettingsUpdate(n.Settings)
	case *protocol.SessionTitleUpdatedNotification:
		st.setTitle(n.Title)
	case *protocol.SessionTokenUsageChangedNotification:
		if n.SessionID == st.id {
			st.setUsage(&n.TokenUsage, n.InclusiveTokenUsage)
			var lc *LastCallTokenUsage
			if n.LastCallTokenUsage != nil {
				c := LastCallTokenUsage(*n.LastCallTokenUsage)
				lc = &c
			}
			st.setLastCall(lc)
		}
	case *protocol.SessionWorkingDirectoryChangedNotification:
		st.setCwd(n.Cwd)
	case *protocol.LLMRetryNotification:
		st.setRetry(&LLMRetry{Attempt: int(n.Attempt), Reason: n.Reason})
	case *protocol.QueuedMessagesDiscardedNotification:
		if n.RequestID != "" {
			st.removeOptimistic(n.RequestID)
		}
		st.pauseDaemonQueued(n.RequestID)
	case *protocol.SessionInactivityNotification, *protocol.SessionProcessExitedNotification:
		st.s.inactive[st.id] = true
		st.setLoadState(NotLoaded, opts.PreserveWorkingStateOnInactive)
	case *protocol.SessionUnsubscribedNotification:
		st.setLoadState(NotLoaded, false)
	case *protocol.SessionClosedNotification:
		st.setLoadState(NotLoaded, false)
		st.s.removeSession(st.id)
	case *protocol.ChildSessionAvailableNotification:
		st.s.childSessionAvailable(st.id, n)
	}
}
