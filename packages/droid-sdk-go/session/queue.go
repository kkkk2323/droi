package session

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// QueuedMessageKind says who holds a queued user message and when it goes
// out.
type QueuedMessageKind string

const (
	// KindLocalDeferredAfterEsc is a follow-up held on this Client until an
	// Esc cancellation settles.
	KindLocalDeferredAfterEsc QueuedMessageKind = "local_deferred_after_esc"
	// KindLocalPausedAfterEsc is what stayed of the queue after an Esc; it
	// lives on this Client only and goes out when the user sends it again.
	KindLocalPausedAfterEsc QueuedMessageKind = "local_paused_after_esc"
	// KindDaemonQueuedDiscardable is held by the Daemon and steers the
	// running turn (queuePlacement end_of_turn).
	KindDaemonQueuedDiscardable QueuedMessageKind = "daemon_queued_discardable"
	// KindDaemonQueuedEndOfLoop is held by the Daemon until the agent loop
	// ends (queuePlacement end_of_loop).
	KindDaemonQueuedEndOfLoop QueuedMessageKind = "daemon_queued_end_of_loop"
	// KindLocalDeferredDuringManualCompaction waits on this Client for a
	// manual compaction to finish.
	KindLocalDeferredDuringManualCompaction QueuedMessageKind = "local_deferred_during_manual_compaction"
)

// DaemonBacked reports whether the Daemon holds messages of this kind.
func (k QueuedMessageKind) DaemonBacked() bool {
	return k == KindDaemonQueuedDiscardable || k == KindDaemonQueuedEndOfLoop
}

// KindForPlacement maps an add_user_message queuePlacement to the kind the
// Daemon then holds the message as.
func KindForPlacement(p protocol.QueuePlacement) QueuedMessageKind {
	if p == protocol.QueuePlacementEndOfLoop {
		return KindDaemonQueuedEndOfLoop
	}
	return KindDaemonQueuedDiscardable
}

// Placement is the queuePlacement that sends a message of this kind.
func (k QueuedMessageKind) Placement() protocol.QueuePlacement {
	if k == KindDaemonQueuedEndOfLoop {
		return protocol.QueuePlacementEndOfLoop
	}
	return protocol.QueuePlacementEndOfTurn
}

// QueuedMessage is a user message waiting to reach the agent.
type QueuedMessage struct {
	RequestID string
	Content   []protocol.ContentBlock
	Kind      QueuedMessageKind
	CreatedAt time.Time
}

const processedRequestLimit = 500

func (st *state) indexOfQueued(requestID string) int {
	return slices.IndexFunc(st.queued, func(q QueuedMessage) bool { return q.RequestID == requestID })
}

// queueMessages adds messages (or updates them in place), skipping request
// ids already confirmed by a create_message.
func (st *state) queueMessages(msgs []QueuedMessage) {
	queued, removedOptimistic := false, false
	for _, q := range msgs {
		if st.processed[q.RequestID] {
			continue
		}
		removedOptimistic = st.dropOptimistic(q.RequestID) || removedOptimistic
		if i := st.indexOfQueued(q.RequestID); i >= 0 {
			st.queued[i] = q
		} else {
			st.queued = append(st.queued, q)
		}
		queued = true
	}
	if !queued {
		return
	}
	if removedOptimistic {
		st.emitMessages(false)
	}
	st.emit(EventQueuedMessagesUpdated)
}

// replaceDaemonQueued swaps the Daemon-backed part of the queue for msgs,
// keeping the local entries.
func (st *state) replaceDaemonQueued(msgs []QueuedMessage) {
	next := make([]QueuedMessage, 0, len(st.queued)+len(msgs))
	changed, removedOptimistic := false, false
	for _, q := range st.queued {
		if q.Kind.DaemonBacked() {
			changed = true
			continue
		}
		next = append(next, q)
	}
	for _, q := range msgs {
		changed = true
		if st.processed[q.RequestID] {
			continue
		}
		removedOptimistic = st.dropOptimistic(q.RequestID) || removedOptimistic
		if i := slices.IndexFunc(next, func(n QueuedMessage) bool { return n.RequestID == q.RequestID }); i >= 0 {
			next[i] = q
		} else {
			next = append(next, q)
		}
	}
	if !changed {
		return
	}
	st.queued = next
	if removedOptimistic {
		st.emitMessages(false)
	}
	st.emit(EventQueuedMessagesUpdated)
}

func (st *state) clearQueued(requestID string) {
	if i := st.indexOfQueued(requestID); i >= 0 {
		st.queued = slices.Delete(st.queued, i, i+1)
	}
	st.emit(EventQueuedMessagesUpdated)
}

// clearQueuedKinds removes the messages of the given kinds, or all of them.
func (st *state) clearQueuedKinds(kinds []QueuedMessageKind) {
	n := len(st.queued)
	st.queued = slices.DeleteFunc(st.queued, func(q QueuedMessage) bool {
		return len(kinds) == 0 || slices.Contains(kinds, q.Kind)
	})
	if len(st.queued) != n {
		st.emit(EventQueuedMessagesUpdated)
	}
}

// dequeue removes and returns the messages of a kind (any kind when
// empty), oldest first; at most limit when limit > 0.
func (st *state) dequeue(kind QueuedMessageKind, limit int) []QueuedMessage {
	var out []QueuedMessage
	st.queued = slices.DeleteFunc(st.queued, func(q QueuedMessage) bool {
		if (kind != "" && q.Kind != kind) || (limit > 0 && len(out) == limit) {
			return false
		}
		out = append(out, q)
		return true
	})
	if len(out) > 0 {
		st.emit(EventQueuedMessagesUpdated)
	}
	return out
}

func (st *state) restoreToFront(msgs []QueuedMessage) {
	if len(msgs) == 0 {
		return
	}
	front := make([]QueuedMessage, 0, len(msgs))
	for _, q := range msgs {
		if i := slices.IndexFunc(front, func(f QueuedMessage) bool { return f.RequestID == q.RequestID }); i >= 0 {
			front[i] = q
		} else {
			front = append(front, q)
		}
	}
	rest := slices.DeleteFunc(slices.Clone(st.queued), func(q QueuedMessage) bool {
		return slices.ContainsFunc(front, func(f QueuedMessage) bool { return f.RequestID == q.RequestID })
	})
	st.queued = append(front, rest...)
	st.emit(EventQueuedMessagesUpdated)
}

// pauseDaemonQueued keeps what an interrupt dropped on the Daemon: end of
// loop messages, and steering messages with attachments (text the user
// can retype is let go), become local paused entries.
func (st *state) pauseDaemonQueued(restoredRequestID string) {
	changed := false
	var paused []QueuedMessage
	for _, q := range st.queued {
		attachment := slices.ContainsFunc(q.Content, func(b protocol.ContentBlock) bool {
			return b.Type == blockDocument || b.Type == blockImage
		})
		pause := func() {
			q.Kind = KindLocalPausedAfterEsc
			paused = append(paused, q)
		}
		switch {
		case restoredRequestID != "" && q.RequestID == restoredRequestID:
			changed = true
			if attachment {
				pause()
			}
		case !q.Kind.DaemonBacked():
			paused = append(paused, q)
		case q.Kind == KindDaemonQueuedDiscardable:
			changed = true
			if attachment {
				pause()
			}
		default:
			changed = true
			pause()
		}
	}
	if !changed {
		return
	}
	st.queued = paused
	st.emit(EventQueuedMessagesUpdated)
}

func (st *state) rememberProcessed(requestID string) {
	if st.processed[requestID] {
		st.processedOrder = slices.DeleteFunc(st.processedOrder, func(id string) bool { return id == requestID })
	}
	st.processed[requestID] = true
	st.processedOrder = append(st.processedOrder, requestID)
	for len(st.processedOrder) > processedRequestLimit {
		delete(st.processed, st.processedOrder[0])
		st.processedOrder = st.processedOrder[1:]
	}
}

// ---- optimistic messages ----

// optimisticEntry is a message (or a staged exchange) shown before the
// Daemon records it. leading entries render ahead of the transcript, as
// session-launch steps do.
type optimisticEntry struct {
	messages []message
	leading  *int
	seq      int
}

func (st *state) addOptimistic(requestID string, msgs []message, leading *int) {
	if len(msgs) == 0 {
		return
	}
	st.optimisticSeq++
	st.optimistic[requestID] = optimisticEntry{messages: slices.Clone(msgs), leading: leading, seq: st.optimisticSeq}
	st.emitMessages(false)
}

func (st *state) dropOptimistic(requestID string) bool {
	_, ok := st.optimistic[requestID]
	delete(st.optimistic, requestID)
	return ok
}

func (st *state) removeOptimistic(requestID string) {
	if st.dropOptimistic(requestID) {
		st.emitMessages(false)
	}
}

// confirmOptimistic drops the part of a staged exchange that messageID
// recorded, keeping the rest until it arrives too.
func (st *state) confirmOptimistic(requestID, messageID string) {
	e, ok := st.optimistic[requestID]
	if !ok {
		return
	}
	rest := slices.DeleteFunc(slices.Clone(e.messages), func(m message) bool { return m.ID == messageID })
	if len(rest) < len(e.messages) && len(rest) > 0 {
		e.messages = rest
		st.optimistic[requestID] = e
		return
	}
	delete(st.optimistic, requestID)
}

func (st *state) rememberConfirmedLeading(requestID, messageID string) {
	if e, ok := st.optimistic[requestID]; ok && e.leading != nil {
		st.confirmedLeading[messageID] = true
	}
}

func (st *state) setBubble(id string) {
	if st.bubble == id {
		return
	}
	st.bubble = id
	st.emit(EventStreamingPlaceholderUpdated)
}

// mergeOptimistic places optimistic messages around the real ones:
// historical system messages first, leading ones after the last recorded
// leading sibling, pending turns last.
func (st *state) mergeOptimistic(real []message) []message {
	if len(st.optimistic) == 0 {
		return real
	}
	entries := make([]optimisticEntry, 0, len(st.optimistic))
	for _, e := range st.optimistic {
		entries = append(entries, e)
	}
	slices.SortStableFunc(entries, func(a, b optimisticEntry) int {
		if a.messages[0].CreatedAt != b.messages[0].CreatedAt {
			if a.messages[0].CreatedAt < b.messages[0].CreatedAt {
				return -1
			}
			return 1
		}
		return a.seq - b.seq
	})
	var system, leading, pending []message
	var leadingEntries []optimisticEntry
	for _, e := range entries {
		switch {
		case e.leading != nil:
			leadingEntries = append(leadingEntries, e)
		case e.messages[0].Role == roleSystem:
			system = append(system, cloneMessages(e.messages)...)
		default:
			pending = append(pending, cloneMessages(e.messages)...)
		}
	}
	slices.SortStableFunc(leadingEntries, func(a, b optimisticEntry) int { return *a.leading - *b.leading })
	for _, e := range leadingEntries {
		leading = append(leading, cloneMessages(e.messages)...)
	}
	at := 0
	if len(leading) > 0 {
		for i, m := range real {
			if st.confirmedLeading[m.ID] {
				at = i + 1
			}
		}
	}
	out := make([]message, 0, len(system)+len(real)+len(leading)+len(pending))
	out = append(out, system...)
	out = append(out, real[:at]...)
	out = append(out, leading...)
	out = append(out, real[at:]...)
	return append(out, pending...)
}

func cloneMessages(msgs []message) []message {
	out := slices.Clone(msgs)
	for i := range out {
		out[i].Content = slices.Clone(out[i].Content)
	}
	return out
}

// ---- queue reconciliation after a load ----

// UserContent builds the content blocks of a user message the way the
// Daemon records add_user_message parameters: explicit content when given,
// otherwise images then text; documents always follow.
func UserContent(text string, content []protocol.AddUserMessageParamsContentItem, images []protocol.Base64ImageSource, files []protocol.DocumentSource) []protocol.ContentBlock {
	var blocks []protocol.ContentBlock
	for _, c := range content {
		blocks = append(blocks, protocol.ContentBlock{Type: c.Type, Raw: c.Raw})
	}
	explicit := len(blocks) > 0
	if !explicit {
		for _, img := range images {
			blocks = append(blocks, blockOf(protocol.ImageBlock{Type: blockImage, Source: img}))
		}
	}
	for _, f := range files {
		blocks = append(blocks, blockOf(protocol.DocumentBlock{Type: blockDocument, Source: f}))
	}
	if !explicit {
		blocks = append(blocks, blockOf(protocol.TextBlock{Type: blockText, Text: text}))
	}
	return blocks
}

// sendable is a message's content as add_user_message fields.
type sendable struct {
	text    string
	images  []json.RawMessage
	files   []json.RawMessage
	ordered []protocol.ContentBlock // image and text blocks in order
}

func sendableOf(content []protocol.ContentBlock) sendable {
	var s sendable
	var texts []string
	for _, b := range content {
		switch b.Type {
		case blockImage:
			s.images = append(s.images, decode[struct {
				Source json.RawMessage `json:"source"`
			}](b).Source)
			s.ordered = append(s.ordered, b)
		case blockDocument:
			s.files = append(s.files, decode[struct {
				Source json.RawMessage `json:"source"`
			}](b).Source)
		case blockText:
			texts = append(texts, decode[textView](b).Text)
			s.ordered = append(s.ordered, b)
		}
	}
	s.text = strings.TrimSpace(strings.Join(texts, "\n"))
	return s
}

// canonical reports whether the ordered content is what the Daemon would
// rebuild from text and images alone (images, then the text).
func (s sendable) canonical() bool {
	seenText := false
	for _, b := range s.ordered {
		if b.Type == blockImage && seenText {
			return false
		}
		if b.Type == blockText {
			if seenText || decode[textView](b).Text != s.text || s.text == "" {
				return false
			}
			seenText = true
		}
	}
	return true
}

func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

func sameJSONs(a, b []json.RawMessage) bool {
	return slices.EqualFunc(a, b, sameJSON)
}

func sendablesMatch(a, b sendable) bool {
	if a.text != b.text || !sameJSONs(a.images, b.images) || !sameJSONs(a.files, b.files) {
		return false
	}
	if len(a.files) > 0 || len(b.files) > 0 || a.canonical() && b.canonical() {
		return true
	}
	return slices.EqualFunc(a.ordered, b.ordered, func(x, y protocol.ContentBlock) bool { return sameJSON(x.Raw, y.Raw) })
}

// delivered reports whether a loaded transcript already holds the queued
// message, by id or by content sent no earlier than it was queued.
func delivered(loaded []message, q QueuedMessage) bool {
	qs := sendableOf(q.Content)
	if qs.text == "" && len(qs.images) == 0 && len(qs.files) == 0 {
		return false
	}
	queuedAt := float64(q.CreatedAt.UnixMilli())
	for _, m := range loaded {
		if m.Role != roleUser {
			continue
		}
		if m.ID == q.RequestID {
			return true
		}
		if m.CreatedAt >= queuedAt && sendablesMatch(sendableOf(m.Content), qs) {
			return true
		}
	}
	return false
}

// reconcileQueue replaces the Daemon-backed queue with the one a load
// reported and returns the messages this Client queued that the Daemon
// neither holds nor delivered: they must be sent again (ResubmitParams).
// While the agent loop runs such messages may still be draining, so they
// stay queued instead.
func (st *state) reconcileQueue(res *protocol.LoadSessionResult) []QueuedMessage {
	now := st.s.now()
	var restored []QueuedMessage
	restoredIDs := map[string]bool{}
	for _, q := range res.QueuedMessages {
		restored = append(restored, QueuedMessage{
			RequestID: q.RequestID,
			Content:   UserContent(q.Text, q.Content, q.Images, q.Files),
			Kind:      KindForPlacement(q.QueuePlacement),
			CreatedAt: now,
		})
		restoredIDs[q.RequestID] = true
	}
	loopRunning := res.IsAgentLoopInProgress != nil && *res.IsAgentLoopInProgress
	var retained, resubmit []QueuedMessage
	for _, q := range st.queued {
		if !q.Kind.DaemonBacked() || restoredIDs[q.RequestID] || delivered(res.Session.Messages, q) {
			continue
		}
		if loopRunning {
			retained = append(retained, q)
		} else {
			resubmit = append(resubmit, q)
		}
	}
	st.replaceDaemonQueued(append(restored, retained...))
	return resubmit
}

// ResubmitParams builds the add_user_message parameters that send a stale
// queued message again (SessionID still to be set).
func ResubmitParams(q QueuedMessage) protocol.AddUserMessageParams {
	s := sendableOf(q.Content)
	p := protocol.AddUserMessageParams{Text: s.text, QueuePlacement: q.Kind.Placement()}
	if len(s.files) == 0 && !s.canonical() {
		for _, b := range s.ordered {
			p.Content = append(p.Content, protocol.AddUserMessageParamsContentItem{Type: b.Type, Raw: b.Raw})
		}
	}
	for _, raw := range s.images {
		var img protocol.Base64ImageSource
		if json.Unmarshal(raw, &img) == nil {
			p.Images = append(p.Images, img)
		}
	}
	for _, raw := range s.files {
		var f protocol.DocumentSource
		if json.Unmarshal(raw, &f) == nil {
			p.Files = append(p.Files, f)
		}
	}
	return p
}
