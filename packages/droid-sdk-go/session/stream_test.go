package session

import (
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// The notification sequence of droi's Fake Daemon streamedReply turn, which
// mirrors a real Daemon.
func TestStreamedReplyBecomesOneAssistantMessage(t *testing.T) {
	s, _ := newTestStore()
	const id = "s1"
	loadWith(t, s, id, protocol.LoadSessionResult{Session: protocol.Session{Title: "Chat", Messages: []protocol.FactoryDroidMessage{
		msg("u0", roleUser, "Why does login fail?", 1, ""),
		msg("a0", roleAssistant, "The refresh is not awaited.", 2, "u0"),
	}}})
	rec := record(s)

	send(t, s, id, working("streaming_assistant_message"))
	send(t, s, id, created(msg("u1", roleUser, "Say hello", 10, ""), "req-1"))
	send(t, s, id, working("thinking"))
	for _, d := range []string{"Hel", "lo ", "there"} {
		send(t, s, id, textDelta("a1", 0, d))
	}

	sess := s.Session(id)
	msgs := sess.Messages()
	if got := idsOf(msgs); got != "u0,a0,u1,a1" {
		t.Fatalf("order %s", got)
	}
	if msgs[3].ParentID != "u1" || textOf(msgs[3]) != "Hello there" || !IsStreaming(msgs[3].Content[0]) {
		t.Fatalf("streamed message %+v", msgs[3])
	}
	if ws := sess.WorkingState(); ws != protocol.DroidWorkingStateStreamingAssistantMessage {
		t.Fatalf("a text delta while thinking should switch to streaming, got %s", ws)
	}
	if !rec.has(Event{Kind: EventMessagesUpdated, SessionID: id, Streaming: true}) {
		t.Fatal("deltas should emit streaming message events")
	}

	send(t, s, id, map[string]any{"type": "assistant_text_complete", "messageId": "a1", "blockIndex": 0})
	if m, _ := sess.Message("a1"); IsStreaming(m.Content[0]) {
		t.Fatal("text complete should end streaming")
	}
	final := msg("a1", roleAssistant, "Hello there", 20, "")
	send(t, s, id, map[string]any{"type": "create_message", "message": final, "parentId": "u1"})
	send(t, s, id, map[string]any{"type": "agent_turn_completed", "reason": "completed", "turnId": "u1", "tokenUsage": zeroUsage})
	send(t, s, id, map[string]any{
		"type": "session_token_usage_changed", "sessionId": id, "tokenUsage": zeroUsage,
		"lastCallTokenUsage": map[string]any{"inputTokens": 8000, "cacheReadTokens": 2000, "outputTokens": 300},
	})
	send(t, s, id, working("idle"))

	msgs = sess.Messages()
	if got := idsOf(msgs); got != "u0,a0,u1,a1" {
		t.Fatalf("final order %s", got)
	}
	if msgs[3].ParentID != "u1" || textOf(msgs[3]) != "Hello there" || IsStreaming(msgs[3].Content[0]) {
		t.Fatalf("final message %+v", msgs[3])
	}
	if sess.WorkingState() != protocol.DroidWorkingStateIdle || sess.AgentTurnCompletionReason() != protocol.AgentTurnCompletionReasonCompleted {
		t.Fatalf("state %s / %s", sess.WorkingState(), sess.AgentTurnCompletionReason())
	}
	if u := sess.Usage(); u.LastCall == nil || u.LastCall.InputTokens != 8000 || u.Session == nil {
		t.Fatalf("usage %+v", u)
	}
	if sess.Title() != "Chat" {
		t.Fatalf("title %q", sess.Title())
	}
}

func TestDuplicateCreateMessageAndDeltasForExistingMessage(t *testing.T) {
	s, _ := newTestStore()
	const id = "s1"
	loadWith(t, s, id, protocol.LoadSessionResult{})
	u := msg("u1", roleUser, "hi", 1, "")
	send(t, s, id, created(u, ""))
	send(t, s, id, created(u, ""))
	a := msg("a1", roleAssistant, "Hel", 2, "")
	send(t, s, id, created(a, ""))
	send(t, s, id, textDelta("a1", 0, "lo"))
	msgs := s.Session(id).Messages()
	if idsOf(msgs) != "u1,a1" || textOf(msgs[1]) != "Hello" || msgs[1].ParentID != "u1" {
		t.Fatalf("got %s %q parent %q", idsOf(msgs), textOf(msgs[1]), msgs[1].ParentID)
	}
}

func TestThinkingStreamsIntoItsOwnBlock(t *testing.T) {
	s, clk := newTestStore()
	const id = "s1"
	loadWith(t, s, id, protocol.LoadSessionResult{Session: protocol.Session{Messages: []protocol.FactoryDroidMessage{msg("u1", roleUser, "q", 1, "")}}})
	sess := s.Session(id)
	thinking := func(d string) map[string]any {
		return map[string]any{"type": "thinking_text_delta", "messageId": "m1", "blockIndex": 0, "textDelta": d}
	}
	send(t, s, id, working("thinking"))
	send(t, s, id, thinking("Let me "))
	clk.add(1500 * time.Millisecond)
	send(t, s, id, thinking("think"))

	m, _ := sess.Message("m1")
	v := decode[struct {
		Thinking    string  `json:"thinking"`
		StartedAtMs float64 `json:"startedAtMs"`
	}](m.Content[0])
	if m.Content[0].Type != blockThinking || v.Thinking != "Let me think" || !IsStreaming(m.Content[0]) || v.StartedAtMs == 0 {
		t.Fatalf("thinking block %s", m.Content[0].Raw)
	}

	send(t, s, id, textDelta("m1", 1, "Answer"))
	if ws := sess.WorkingState(); ws != protocol.DroidWorkingStateStreamingAssistantMessage {
		t.Fatalf("working %s", ws)
	}
	send(t, s, id, map[string]any{"type": "thinking_text_complete", "messageId": "m1", "blockIndex": 0})
	m, _ = sess.Message("m1")
	if len(m.Content) != 2 || m.Content[1].Type != blockText || textOf(m) != "Answer" {
		t.Fatalf("content %+v", m.Content)
	}
	dur := decode[struct {
		DurationMs float64 `json:"durationMs"`
	}](m.Content[0]).DurationMs
	if IsStreaming(m.Content[0]) || dur != 1500 {
		t.Fatalf("completed thinking %s", m.Content[0].Raw)
	}
	if !IsStreaming(m.Content[1]) {
		t.Fatal("text still streaming until the turn stops")
	}
	send(t, s, id, working("idle"))
	m, _ = sess.Message("m1")
	if IsStreaming(m.Content[1]) {
		t.Fatal("idle should end every streaming block")
	}
}

func TestToolCallThenAssistantMessageThenResult(t *testing.T) {
	s, _ := newTestStore()
	s.progressTTL = 20 * time.Millisecond
	const id = "s1"
	loadWith(t, s, id, protocol.LoadSessionResult{Session: protocol.Session{Messages: []protocol.FactoryDroidMessage{msg("u1", roleUser, "list files", 1, "")}}})
	sess := s.Session(id)

	input := map[string]any{"command": "ls"}
	send(t, s, id, map[string]any{"type": "tool_call", "toolUse": map[string]any{"type": "tool_use", "id": "t1", "name": "Execute", "input": input}})
	msgs := sess.Messages()
	if len(msgs) != 2 || !hasToolUse(msgs[1], "t1") || msgs[1].ParentID != "u1" {
		t.Fatalf("pending assistant %+v", msgs)
	}
	if sess.ToolPhase("t1") != protocol.ToolExecutionLifecyclePhaseStreamingInput {
		t.Fatalf("phase %s", sess.ToolPhase("t1"))
	}

	send(t, s, id, created(protocol.FactoryDroidMessage{
		ID: "a1", Role: roleAssistant, Content: []protocol.ContentBlock{toolUseBlock("t1", "Execute", input)}, CreatedAt: 2, UpdatedAt: 2,
	}, ""))
	msgs = sess.Messages()
	if idsOf(msgs) != "u1,a1" || msgs[1].ParentID != "u1" {
		t.Fatalf("create_message should replace the pending assistant: %s parent %q", idsOf(msgs), msgs[1].ParentID)
	}

	send(t, s, id, working("executing_tool"))
	progress := map[string]any{"type": "tool_progress_update", "toolUseId": "t1", "toolName": "Execute", "update": map[string]any{"type": "output", "text": "a.go"}}
	send(t, s, id, progress)
	send(t, s, id, progress)
	if n := len(sess.ToolProgress("t1")); n != 1 || sess.ToolPhase("t1") != protocol.ToolExecutionLifecyclePhaseExecuting {
		t.Fatalf("progress %d phase %s", n, sess.ToolPhase("t1"))
	}

	send(t, s, id, map[string]any{"type": "tool_result", "toolUseId": "t1", "content": "a.go", "isError": false, "messageId": "r1"})
	msgs = sess.Messages()
	if idsOf(msgs) != "u1,a1,r1" || msgs[2].Role != roleTool || msgs[2].ParentID != "a1" {
		t.Fatalf("tool message: %s %+v", idsOf(msgs), msgs[2])
	}
	if sess.ToolPhase("t1") != protocol.ToolExecutionLifecyclePhaseSettledAfterExecution {
		t.Fatalf("phase %s", sess.ToolPhase("t1"))
	}
	waitFor(t, "progress cleanup", func() bool { return len(sess.ToolProgress("t1")) == 0 })
}

func TestToolResultBeforeItsAssistantMessageIsAdopted(t *testing.T) {
	s, _ := newTestStore()
	const id = "s1"
	loadWith(t, s, id, protocol.LoadSessionResult{Session: protocol.Session{Messages: []protocol.FactoryDroidMessage{msg("u1", roleUser, "go", 1, "")}}})
	sess := s.Session(id)
	send(t, s, id, map[string]any{"type": "tool_result", "toolUseId": "t2", "content": "done", "messageId": "r2"})
	if sess.MessageCount() != 1 {
		t.Fatalf("an orphaned result must wait, got %s", idsOf(sess.Messages()))
	}
	send(t, s, id, created(protocol.FactoryDroidMessage{
		ID: "a2", Role: roleAssistant, Content: []protocol.ContentBlock{toolUseBlock("t2", "Read", map[string]any{})}, CreatedAt: 2, UpdatedAt: 2,
	}, ""))
	msgs := sess.Messages()
	if idsOf(msgs) != "u1,a2,r2" {
		t.Fatalf("order %s", idsOf(msgs))
	}
	if r, _ := sess.Message("r2"); r.ParentID != "a2" {
		t.Fatalf("orphan parent %q", r.ParentID)
	}
}

func TestRetractionRemovesStreamedAndPendingMessages(t *testing.T) {
	s, _ := newTestStore()
	const id = "s1"
	loadWith(t, s, id, protocol.LoadSessionResult{Session: protocol.Session{Messages: []protocol.FactoryDroidMessage{msg("u1", roleUser, "q", 1, "")}}})
	sess := s.Session(id)
	send(t, s, id, textDelta("a1", 0, "partial"))
	send(t, s, id, map[string]any{"type": "assistant_message_retracted", "messageId": "a1"})
	if got := idsOf(sess.Messages()); got != "u1" {
		t.Fatalf("after retraction %s", got)
	}
	send(t, s, id, map[string]any{"type": "tool_call", "toolUse": map[string]any{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{}}})
	if sess.MessageCount() != 2 {
		t.Fatal("tool_call should add a pending assistant")
	}
	send(t, s, id, map[string]any{"type": "assistant_message_retracted", "messageId": "unknown"})
	if got := idsOf(sess.Messages()); got != "u1" {
		t.Fatalf("retraction should drop the pending assistant too, got %s", got)
	}
}

func TestSettingsTitleAndTokenUsage(t *testing.T) {
	s, _ := newTestStore()
	const id = "s1"
	loadWith(t, s, id, protocol.LoadSessionResult{
		Settings:        protocol.SessionSettings{ModelID: "claude", ReasoningEffort: "low", InteractionMode: "spec", AutonomyLevel: "low"},
		AvailableModels: []protocol.ModelMetadata{{ID: "claude"}, {ID: "gpt"}},
		TokenUsage:      &protocol.TokenUsage{InputTokens: 5},
	})
	sess := s.Session(id)
	if set := sess.Settings(); set.ModelID != "claude" || set.InteractionMode != "spec" || len(sess.AvailableModels()) != 2 {
		t.Fatalf("load settings %+v", set)
	}
	rec := record(s)
	send(t, s, id, map[string]any{"type": "settings_updated", "settings": map[string]any{"modelId": "gpt", "reasoningEffort": "high", "autonomyLevel": "medium"}})
	set := sess.Settings()
	if set.ModelID != "gpt" || set.ReasoningEffort != "high" || set.AutonomyLevel != "medium" || set.InteractionMode != "spec" {
		t.Fatalf("updated settings %+v", set)
	}
	if !rec.has(Event{Kind: EventSettingsUpdated, SessionID: id}) {
		t.Fatal("no settings event")
	}
	send(t, s, id, map[string]any{"type": "session_title_updated", "title": "Fix login"})
	send(t, s, id, map[string]any{"type": "session_token_usage_changed", "sessionId": "other", "tokenUsage": map[string]any{"inputTokens": 99}})
	if u := sess.Usage(); u.Session.InputTokens != 5 {
		t.Fatalf("usage of another Session applied: %+v", u.Session)
	}
	send(t, s, id, map[string]any{
		"type": "session_token_usage_changed", "sessionId": id,
		"tokenUsage": map[string]any{"inputTokens": 7}, "inclusiveTokenUsage": map[string]any{"inputTokens": 9},
	})
	if u := sess.Usage(); u.Session.InputTokens != 7 || u.InclusiveOrOwn().InputTokens != 9 || sess.Title() != "Fix login" {
		t.Fatalf("usage %+v title %q", u, sess.Title())
	}
}

func TestConcurrentNotificationsAndReads(t *testing.T) {
	s, _ := newTestStore()
	const id = "s1"
	loadWith(t, s, id, protocol.LoadSessionResult{})
	// A subscriber that reads back into the Store must not deadlock.
	s.Subscribe(func(e Event) {
		if sess := s.Session(e.SessionID); sess != nil {
			_ = sess.DisplayMessages()
		}
	})
	done := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				sess := s.Session(id)
				_ = sess.Messages()
				_ = sess.WorkingState()
				_ = sess.Settings()
			}
		})
	}
	for range 200 {
		send(t, s, id, textDelta("a1", 0, "x"))
	}
	close(done)
	wg.Wait()
	if m, _ := s.Session(id).Message("a1"); len(textOf(m)) != 200 {
		t.Fatalf("text length %d", len(textOf(m)))
	}
}
