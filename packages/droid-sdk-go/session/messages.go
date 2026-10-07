package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

type message = protocol.FactoryDroidMessage

const (
	roleUser      = protocol.MessageRoleUser
	roleAssistant = protocol.MessageRoleAssistant
	roleTool      = protocol.MessageRoleTool
	roleSystem    = protocol.MessageRoleSystem

	blockText       = protocol.ContentBlockTypeText
	blockThinking   = protocol.ContentBlockTypeThinking
	blockToolUse    = protocol.ContentBlockTypeToolUse
	blockToolResult = protocol.ContentBlockTypeToolResult
	blockImage      = protocol.ContentBlockTypeImage
	blockDocument   = protocol.ContentBlockTypeDocument
)

// blockOf encodes one of the protocol's block shapes (or a field map).
func blockOf(v any) protocol.ContentBlock {
	b, err := protocol.NewContentBlock(v)
	if err != nil {
		panic(fmt.Sprintf("session: encode block: %v", err))
	}
	return b
}

// withFields returns b with the given fields set; a nil value removes one.
// Fields the protocol types do not know (isStreaming, startedAtMs, ...)
// survive, which is why blocks are patched as JSON objects.
func withFields(b protocol.ContentBlock, kv map[string]any) protocol.ContentBlock {
	m := map[string]json.RawMessage{}
	_ = json.Unmarshal(b.Raw, &m)
	for k, v := range kv {
		if v == nil {
			delete(m, k)
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			panic(fmt.Sprintf("session: encode block field %s: %v", k, err))
		}
		m[k] = raw
	}
	return blockOf(m)
}

func decode[T any](b protocol.ContentBlock) T {
	var v T
	_ = json.Unmarshal(b.Raw, &v)
	return v
}

type textView struct {
	Text string `json:"text"`
}

type thinkingView struct {
	Thinking                 string `json:"thinking"`
	SupportsThinkingDuration *bool  `json:"supportsThinkingDuration"`
}

type resultView struct {
	ToolUseID string          `json:"toolUseId"`
	LegacyID  string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"isError"`
}

func toolResultOf(b protocol.ContentBlock) (resultView, bool) {
	if b.Type != blockToolResult {
		return resultView{}, false
	}
	v := decode[resultView](b)
	if v.ToolUseID == "" {
		v.ToolUseID = v.LegacyID
	}
	return v, true
}

func toolUseID(b protocol.ContentBlock) string {
	if b.Type != blockToolUse {
		return ""
	}
	return decode[struct {
		ID string `json:"id"`
	}](b).ID
}

func hasToolUse(m message, id string) bool {
	return slices.ContainsFunc(m.Content, func(b protocol.ContentBlock) bool { return toolUseID(b) == id })
}

func indexOfToolResult(content []protocol.ContentBlock, id string) int {
	return slices.IndexFunc(content, func(b protocol.ContentBlock) bool {
		r, ok := toolResultOf(b)
		return ok && r.ToolUseID == id
	})
}

var streamingKey = []byte(`"isStreaming"`)

// IsStreaming reports whether a text or thinking block is still being
// streamed. Blocks built from deltas carry an "isStreaming" field, as in the
// TypeScript SDK; it turns false when the block completes or the agent
// stops streaming.
func IsStreaming(b protocol.ContentBlock) bool {
	if !bytes.Contains(b.Raw, streamingKey) {
		return false
	}
	return decode[struct {
		IsStreaming bool `json:"isStreaming"`
	}](b).IsStreaming
}

func streamingText(text string) protocol.ContentBlock {
	return blockOf(map[string]any{"type": blockText, "text": text, "isStreaming": true})
}

func streamingThinking(text string, startedAtMs float64) protocol.ContentBlock {
	return blockOf(map[string]any{
		"type": blockThinking, "thinking": text, "signature": "",
		"isStreaming": true, "supportsThinkingDuration": true, "startedAtMs": startedAtMs,
	})
}

func isPersistedHook(m message) bool {
	return m.HookEventName != "" && m.HookCommands != nil && m.HookStatus != ""
}

// isSideband tells transcript-only rows (hook rows, user-only system
// receipts) that never become the parent of a later turn.
func isSideband(m message) bool {
	return isPersistedHook(m) || m.Role == roleSystem && m.Visibility == protocol.MessageVisibilityUserOnly
}

// thread is a Session's transcript in display order.
type thread struct {
	byID     map[string]message
	order    []string
	lastConv string
}

func newThread() thread { return thread{byID: map[string]message{}} }

func (t *thread) reset(msgs []message) {
	t.byID = make(map[string]message, len(msgs))
	t.order = make([]string, 0, len(msgs))
	for _, m := range msgs {
		if _, dup := t.byID[m.ID]; !dup {
			t.order = append(t.order, m.ID)
		}
		t.byID[m.ID] = m
	}
	t.recomputeLastConv()
}

func (t *thread) len() int { return len(t.order) }

func (t *thread) get(id string) (message, bool) {
	m, ok := t.byID[id]
	return m, ok
}

// all returns the stored messages, sharing their content; callers must
// not modify it.
func (t *thread) all() []message {
	out := make([]message, 0, len(t.order))
	for _, id := range t.order {
		out = append(out, t.byID[id])
	}
	return out
}

// list returns a copy safe to hand out.
func (t *thread) list() []message {
	out := t.all()
	for i := range out {
		out[i].Content = slices.Clone(out[i].Content)
	}
	return out
}

func (t *thread) lastConversation() (message, bool) {
	return t.get(t.lastConv)
}

func (t *thread) recomputeLastConv() {
	t.lastConv = ""
	for i := len(t.order) - 1; i >= 0; i-- {
		if !isSideband(t.byID[t.order[i]]) {
			t.lastConv = t.order[i]
			return
		}
	}
}

// add stores a new message where its parent chain puts it, falling back to
// timestamp order; a message already present is replaced in place.
func (t *thread) add(m message) {
	m = t.normalizeParent(m)
	if _, exists := t.byID[m.ID]; exists {
		t.set(m)
		return
	}
	t.byID[m.ID] = m
	side := isSideband(m)
	switch {
	case side || t.lastConv == "" || m.ParentID == t.lastConv:
		t.order = append(t.order, m.ID)
	case m.ParentID != "" && slices.Contains(t.order, m.ParentID):
		// Insert after the parent's existing descendants so a late child
		// does not split a subtree.
		parent := slices.Index(t.order, m.ParentID)
		descendants := map[string]bool{m.ParentID: true}
		at := parent + 1
		for ; at < len(t.order); at++ {
			c := t.byID[t.order[at]]
			if c.ParentID == "" || !descendants[c.ParentID] {
				break
			}
			descendants[c.ID] = true
		}
		t.order = slices.Insert(t.order, at, m.ID)
	default:
		t.insertByTimestamp(m)
	}
	if !side {
		t.lastConv = m.ID
	}
}

func (t *thread) insertByTimestamp(m message) {
	at := slices.IndexFunc(t.order, func(id string) bool {
		e := t.byID[id]
		return e.CreatedAt > m.CreatedAt || e.CreatedAt == m.CreatedAt && e.ID > m.ID
	})
	if at < 0 {
		t.order = append(t.order, m.ID)
		return
	}
	t.order = slices.Insert(t.order, at, m.ID)
}

// normalizeParent points a conversation message past sideband parents to
// the nearest conversation ancestor.
func (t *thread) normalizeParent(m message) message {
	if isSideband(m) || m.ParentID == "" {
		return m
	}
	visited := map[string]bool{}
	for id := m.ParentID; id != "" && !visited[id]; {
		visited[id] = true
		p, ok := t.byID[id]
		if !ok {
			break
		}
		if !isSideband(p) {
			m.ParentID = p.ID
			return m
		}
		id = p.ParentID
	}
	if _, ok := t.byID[m.ParentID]; ok {
		m.ParentID = t.lastConv
	}
	return m
}

// set replaces a stored message; it reports whether one was there.
func (t *thread) set(m message) bool {
	old, ok := t.byID[m.ID]
	if !ok {
		return false
	}
	t.byID[m.ID] = m
	if isSideband(old) != isSideband(m) {
		t.recomputeLastConv()
	}
	return true
}

func (t *thread) remove(id string) bool {
	if _, ok := t.byID[id]; !ok {
		return false
	}
	delete(t.byID, id)
	t.order = slices.DeleteFunc(t.order, func(o string) bool { return o == id })
	if id == t.lastConv {
		t.recomputeLastConv()
	}
	return true
}

// ---- message mutations of one Session ----

func (st *state) addMessage(m message, streaming bool) {
	if st.todos.applyMessage(m) {
		st.emit(EventTodosUpdated)
	}
	st.thread.add(m)
	st.emitMessages(streaming)
}

func (st *state) replaceMessage(m message, streaming bool) {
	if st.thread.set(m) {
		st.emitMessages(streaming)
	}
}

func (st *state) newAssistant(id, parentID string, content ...protocol.ContentBlock) message {
	now := st.nowMs()
	return message{ID: id, Role: roleAssistant, Content: content, ParentID: parentID, CreatedAt: now, UpdatedAt: now}
}

func (st *state) textDelta(id, delta string) {
	st.streamingIDs[id] = true
	m, ok := st.thread.get(id)
	if !ok {
		st.addMessage(st.newAssistant(id, st.thread.lastConv, streamingText(delta)), true)
		return
	}
	content := slices.Clone(m.Content)
	if i := lastIndexOfType(content, blockText); i >= 0 {
		content[i] = withFields(content[i], map[string]any{"text": decode[textView](content[i]).Text + delta})
	} else {
		content = append(content, streamingText(delta))
	}
	m.Content, m.UpdatedAt = content, st.nowMs()
	st.replaceMessage(m, true)
}

func (st *state) thinkingDelta(id string, blockIndex int, delta string) {
	st.streamingIDs[id] = true
	key := blockKey{id, blockIndex}
	if _, ok := st.thinkStart[key]; !ok {
		st.thinkStart[key] = st.s.now()
	}
	startedAt := float64(st.thinkStart[key].UnixMilli())
	m, ok := st.thread.get(id)
	if !ok {
		st.thinkIndex[key] = 0
		st.addMessage(st.newAssistant(id, st.thread.lastConv, streamingThinking(delta, startedAt)), true)
		return
	}
	content := slices.Clone(m.Content)
	if i := st.mappedThinkingIndex(key, content); i >= 0 {
		v := decode[thinkingView](content[i])
		supports := v.SupportsThinkingDuration == nil || *v.SupportsThinkingDuration
		content[i] = withFields(content[i], map[string]any{
			"thinking": v.Thinking + delta, "supportsThinkingDuration": supports, "startedAtMs": startedAt,
		})
	} else {
		st.thinkIndex[key] = len(content)
		content = append(content, streamingThinking(delta, startedAt))
	}
	m.Content, m.UpdatedAt = content, st.nowMs()
	st.replaceMessage(m, true)
}

func lastIndexOfType(content []protocol.ContentBlock, typ string) int {
	for i := len(content) - 1; i >= 0; i-- {
		if content[i].Type == typ {
			return i
		}
	}
	return -1
}

// nthOfType returns the content index of the n-th block of a type.
func nthOfType(content []protocol.ContentBlock, typ string, n int) int {
	seen := 0
	for i, b := range content {
		if b.Type == typ {
			if seen == n {
				return i
			}
			seen++
		}
	}
	return -1
}

type blockKey struct {
	messageID string
	block     int
}

// mappedThinkingIndex finds where the Daemon's thinking block index landed
// in the message content; Daemon block indexes count every block kind, so
// they cannot index content directly.
func (st *state) mappedThinkingIndex(key blockKey, content []protocol.ContentBlock) int {
	i, ok := st.thinkIndex[key]
	if ok && i < len(content) && content[i].Type == blockThinking {
		return i
	}
	if ok {
		delete(st.thinkIndex, key)
	}
	return -1
}

func (st *state) trackedThinkingBlock(messageID string, contentIndex int) (int, bool) {
	for k, i := range st.thinkIndex {
		if k.messageID == messageID && i == contentIndex {
			return k.block, true
		}
	}
	return 0, false
}

func (st *state) thinkingDuration(key blockKey) *float64 {
	start, ok := st.thinkStart[key]
	if !ok {
		return nil
	}
	delete(st.thinkStart, key)
	d := float64(max(0, st.s.now().Sub(start).Milliseconds()))
	return &d
}

func (st *state) clearThinkingTracking() {
	clear(st.thinkStart)
	clear(st.thinkIndex)
}

func (st *state) completeText(id string, blockIndex int) {
	m, ok := st.thread.get(id)
	if !ok {
		return
	}
	content := slices.Clone(m.Content)
	i := blockIndex
	if i >= len(content) || content[i].Type != blockText {
		i = nthOfType(content, blockText, blockIndex)
	}
	if i >= 0 {
		content[i] = withFields(content[i], map[string]any{"isStreaming": false})
	}
	m.Content, m.UpdatedAt = content, st.nowMs()
	st.replaceMessage(m, false)
}

func (st *state) completeThinking(id string, blockIndex int, durationMs *float64) {
	m, ok := st.thread.get(id)
	if !ok {
		return
	}
	content := slices.Clone(m.Content)
	key := blockKey{id, blockIndex}
	i := st.mappedThinkingIndex(key, content)
	if i < 0 {
		if i = nthOfType(content, blockThinking, blockIndex); i >= 0 {
			st.thinkIndex[key] = i
		}
	}
	if i >= 0 {
		d := st.thinkingDuration(key)
		if durationMs != nil {
			d = durationMs
		}
		kv := map[string]any{"isStreaming": false}
		if d != nil {
			kv["durationMs"] = *d
		}
		content[i] = withFields(content[i], kv)
	}
	m.Content, m.UpdatedAt = content, st.nowMs()
	st.replaceMessage(m, false)
}

// completeStreamingBlocks ends every block still marked streaming, as when
// the agent stops or turns to a tool.
func (st *state) completeStreamingBlocks() {
	changed := false
	for id := range st.streamingIDs {
		m, ok := st.thread.get(id)
		if !ok {
			continue
		}
		var content []protocol.ContentBlock
		patch := func(i int, kv map[string]any) {
			if content == nil {
				content = slices.Clone(m.Content)
			}
			content[i] = withFields(content[i], kv)
		}
		ordinal := 0
		for i, b := range m.Content {
			switch b.Type {
			case blockText:
				if IsStreaming(b) {
					patch(i, map[string]any{"isStreaming": false})
				}
			case blockThinking:
				bi, ok := st.trackedThinkingBlock(m.ID, i)
				if !ok {
					bi = ordinal
				}
				ordinal++
				if IsStreaming(b) {
					kv := map[string]any{"isStreaming": false}
					if d := st.thinkingDuration(blockKey{m.ID, bi}); d != nil {
						kv["durationMs"] = *d
					}
					patch(i, kv)
				}
			}
		}
		if content != nil {
			m.Content, m.UpdatedAt = content, st.nowMs()
			st.thread.set(m)
			changed = true
		}
	}
	clear(st.streamingIDs)
	st.clearThinkingTracking()
	if changed {
		st.emitMessages(false)
	}
}

func (st *state) retract(id string) {
	ids := []string{id}
	if st.pendingAssistant != "" {
		ids = append(ids, st.pendingAssistant)
		st.pendingAssistant = ""
	}
	removed := false
	for _, id := range ids {
		removed = st.thread.remove(id) || removed
	}
	if removed {
		st.emitMessages(false)
	}
}

// toolCall files a tool use announced before its assistant message: into
// the pending assistant message, the last assistant message, or a new
// pending one that create_message later replaces.
func (st *state) toolCall(tu protocol.ToolUse) {
	st.setPhase(tu.ID, protocol.ToolExecutionLifecyclePhaseStreamingInput, false)
	tu = protocol.ToolUse{Type: blockToolUse, ID: tu.ID, Name: tu.Name, Input: tu.Input}
	block := blockOf(tu)
	if st.pendingAssistant != "" {
		if _, ok := st.thread.get(st.pendingAssistant); !ok {
			st.pendingAssistant = ""
		}
	}
	if st.pendingAssistant != "" {
		st.toolUse(st.pendingAssistant, tu, block)
		return
	}
	last, ok := st.thread.lastConversation()
	if ok && last.Role == roleAssistant {
		st.toolUse(last.ID, tu, block)
		return
	}
	for _, m := range st.thread.all() {
		if m.Role == roleAssistant && hasToolUse(m, tu.ID) {
			return
		}
	}
	st.pendingAssistant = fmt.Sprintf("pending-assistant-%d", st.s.now().UnixMilli())
	st.addMessage(st.newAssistant(st.pendingAssistant, last.ID, block), false)
}

func (st *state) toolUse(messageID string, tu protocol.ToolUse, block protocol.ContentBlock) {
	st.todos.track(tu)
	m, ok := st.thread.get(messageID)
	if !ok {
		st.addMessage(st.newAssistant(messageID, "", block), false)
		return
	}
	content := slices.Clone(m.Content)
	i := slices.IndexFunc(content, func(b protocol.ContentBlock) bool { return toolUseID(b) == tu.ID })
	if i >= 0 {
		content[i] = block
	} else {
		content = append(content, block)
	}
	m.Content, m.UpdatedAt = content, st.nowMs()
	st.replaceMessage(m, i >= 0)
}

func (st *state) toolResult(messageID string, n *protocol.ToolResultNotification) {
	st.setPhase(n.ToolUseID, settlePhase(st.phases[n.ToolUseID]), false)
	block := blockOf(protocol.ToolResult{Type: blockToolResult, ToolUseID: n.ToolUseID, Content: n.Content, IsError: n.IsError})
	if st.todos.onResult(n.ToolUseID, n.IsError != nil && *n.IsError) {
		st.emit(EventTodosUpdated)
	}
	st.setWorking(protocol.DroidWorkingStateExecutingTool)
	st.emit(EventWorkingStateChanged)
	defer st.scheduleProgressCleanup(n.ToolUseID)

	if m, ok := st.thread.get(messageID); ok {
		if m.Role != roleTool {
			return
		}
		content := slices.Clone(m.Content)
		if i := indexOfToolResult(content, n.ToolUseID); i >= 0 {
			content[i] = block
		} else {
			content = append(content, block)
		}
		m.Content, m.UpdatedAt = content, st.nowMs()
		st.replaceMessage(m, false)
		return
	}
	now := st.nowMs()
	tm := message{ID: messageID, Role: roleTool, Content: []protocol.ContentBlock{block}, CreatedAt: now, UpdatedAt: now}
	msgs := st.thread.all()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == roleAssistant && hasToolUse(msgs[i], n.ToolUseID) {
			tm.ParentID = msgs[i].ID
			break
		}
	}
	if tm.ParentID == "" {
		// The result beat its tool use; the assistant message adopts it.
		st.orphans[n.ToolUseID] = tm
		return
	}
	st.addMessage(tm, false)
}

func (st *state) adoptOrphans(assistant message) {
	for _, b := range assistant.Content {
		id := toolUseID(b)
		if id == "" {
			continue
		}
		if tm, ok := st.orphans[id]; ok {
			delete(st.orphans, id)
			tm.ParentID = assistant.ID
			st.addMessage(tm, false)
		}
	}
}

func (st *state) createMessage(n *protocol.CreateMessageNotification) {
	m := n.Message
	if st.pendingAssistant != "" && m.Role == roleAssistant {
		pending := st.pendingAssistant
		st.pendingAssistant = ""
		p, _ := st.thread.get(pending)
		for _, c := range st.thread.all() {
			if c.ParentID == pending {
				c.ParentID = m.ID
				st.replaceMessage(c, false)
			}
		}
		if st.thread.remove(pending) {
			st.emitMessages(false)
		}
		if m.ParentID == "" && p.ParentID != "" {
			m.ParentID = p.ParentID
		}
	}
	if n.RequestID != "" {
		st.rememberProcessed(n.RequestID)
		st.clearQueued(n.RequestID)
		st.rememberConfirmedLeading(n.RequestID, m.ID)
		st.confirmOptimistic(n.RequestID, m.ID)
	}
	st.upsert(m)
}

func (st *state) upsert(m message) {
	parentID := m.ParentID
	if parentID == m.ID {
		parentID = ""
	}
	existing, ok := st.thread.get(m.ID)
	if !ok {
		if parentID == "" && !isSideband(m) {
			parentID = st.thread.lastConv
		}
		m.ParentID = parentID
		if m.Role == roleAssistant {
			st.todos.trackMessage(m)
			st.adoptOrphans(m)
		}
		st.addMessage(m, false)
		return
	}
	if parentID == "" {
		parentID = existing.ParentID
	}
	m.ParentID = parentID
	if existing.Role == roleAssistant && m.Role == roleAssistant {
		// Keep tool results merged into the streamed message that the
		// authoritative copy does not carry.
		var kept []protocol.ContentBlock
		missing := false
		for _, b := range existing.Content {
			if r, ok := toolResultOf(b); ok {
				kept = append(kept, b)
				if indexOfToolResult(m.Content, r.ToolUseID) < 0 {
					missing = true
				}
			}
		}
		if missing {
			m.Content = append(slices.Clone(m.Content), kept...)
		}
	}
	if st.todos.applyMessage(m) {
		st.emit(EventTodosUpdated)
	}
	if m.Role == roleAssistant {
		st.adoptOrphans(m)
	}
	st.replaceMessage(m, false)
}

// ---- ordering a merged transcript ----

func lessByTime(a, b message) bool {
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt < b.CreatedAt
	}
	return a.ID < b.ID
}

func compareByTime(a, b message) int {
	switch {
	case lessByTime(a, b):
		return -1
	case lessByTime(b, a):
		return 1
	}
	return 0
}

// repairParentChain drops self-parents, keeps the first parent of a
// duplicated id, and breaks parent cycles at their oldest message.
func repairParentChain(msgs []message) []message {
	firstParent := map[string]string{}
	repaired := make([]message, len(msgs))
	for i, m := range msgs {
		if p, ok := firstParent[m.ID]; ok {
			m.ParentID = p
		} else {
			if m.ParentID == m.ID {
				m.ParentID = ""
			}
			firstParent[m.ID] = m.ParentID
		}
		repaired[i] = m
	}
	byID := make(map[string]message, len(repaired))
	for _, m := range repaired {
		byID[m.ID] = m
	}
	cycleRoots := map[string]bool{}
	safe := map[string]bool{}
	for _, start := range repaired {
		pathIndex := map[string]int{}
		var path []message
		cur, ok := byID[start.ID], true
		for ok && !safe[cur.ID] {
			if at, seen := pathIndex[cur.ID]; seen {
				root := path[at]
				for _, c := range path[at+1:] {
					if lessByTime(c, root) {
						root = c
					}
				}
				cycleRoots[root.ID] = true
				break
			}
			pathIndex[cur.ID] = len(path)
			path = append(path, cur)
			if cur.ParentID == "" {
				break
			}
			cur, ok = byID[cur.ParentID]
		}
		for id := range pathIndex {
			safe[id] = true
		}
	}
	for i := range repaired {
		if cycleRoots[repaired[i].ID] {
			repaired[i].ParentID = ""
		}
	}
	return repaired
}

func isSingleRooted(msgs []message) bool {
	if len(msgs) == 0 {
		return false
	}
	ids := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		ids[m.ID] = true
	}
	linked := func(m message) bool { return m.ParentID != "" && m.ParentID != m.ID && ids[m.ParentID] }
	var roots []message
	children := map[string][]string{}
	for _, m := range msgs {
		if linked(m) {
			children[m.ParentID] = append(children[m.ParentID], m.ID)
		} else {
			roots = append(roots, m)
		}
	}
	if len(roots) != 1 {
		return false
	}
	visited := map[string]bool{}
	stack := []string{roots[0].ID}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[id] {
			continue
		}
		visited[id] = true
		stack = append(stack, children[id]...)
	}
	return len(visited) == len(msgs)
}

// orderByParentChain walks back from the newest leaf; messages off that
// chain follow in time order.
func orderByParentChain(msgs []message) []message {
	if len(msgs) == 0 {
		return nil
	}
	byID := make(map[string]message, len(msgs))
	for _, m := range msgs {
		byID[m.ID] = m
	}
	linked := false
	referenced := map[string]bool{}
	for _, m := range msgs {
		if m.ParentID != "" && m.ParentID != m.ID {
			referenced[m.ParentID] = true
			if _, ok := byID[m.ParentID]; ok {
				linked = true
			}
		}
	}
	if !linked {
		out := slices.Clone(msgs)
		slices.SortStableFunc(out, compareByTime)
		return out
	}
	var tips []message
	for _, m := range msgs {
		if !referenced[m.ID] {
			tips = append(tips, m)
		}
	}
	if len(tips) == 0 {
		tips = msgs
	}
	tip := slices.MinFunc(tips, func(a, b message) int {
		if a.CreatedAt != b.CreatedAt {
			if a.CreatedAt > b.CreatedAt {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	var chain []message
	visited := map[string]bool{}
	for cur, ok := tip, true; ok && !visited[cur.ID]; {
		visited[cur.ID] = true
		chain = append(chain, cur)
		if cur.ParentID == "" || cur.ParentID == cur.ID {
			break
		}
		cur, ok = byID[cur.ParentID]
	}
	slices.Reverse(chain)
	if len(chain) < len(msgs) {
		var rest []message
		for _, m := range msgs {
			if !visited[m.ID] {
				rest = append(rest, m)
			}
		}
		slices.SortStableFunc(rest, compareByTime)
		chain = append(chain, rest...)
	}
	return chain
}

func orderMerged(msgs []message) []message {
	if isSingleRooted(msgs) {
		return orderByParentChain(msgs)
	}
	out := slices.Clone(msgs)
	slices.SortStableFunc(out, func(a, b message) int {
		switch {
		case a.CreatedAt < b.CreatedAt:
			return -1
		case a.CreatedAt > b.CreatedAt:
			return 1
		}
		return 0
	})
	return out
}
