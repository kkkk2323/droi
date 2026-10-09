package session

import (
	"testing"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

func usageWithOutput(sessionID string, output float64) map[string]any {
	u := map[string]any{"inputTokens": 100, "outputTokens": output, "cacheCreationTokens": 0, "cacheReadTokens": 0, "thinkingTokens": 0}
	return map[string]any{"type": "session_token_usage_changed", "sessionId": sessionID, "tokenUsage": u}
}

func thinkingDelta(messageID string, block int, delta string) map[string]any {
	return map[string]any{"type": "thinking_text_delta", "messageId": messageID, "blockIndex": block, "textDelta": delta}
}

func TestStatsTimeACallAndATool(t *testing.T) {
	s, clk := newTestStore()
	const id = "s1"
	loadWith(t, s, id, protocol.LoadSessionResult{Session: protocol.Session{Messages: []protocol.FactoryDroidMessage{msg("u1", roleUser, "hi", 1, "")}}})
	send(t, s, id, usageWithOutput(id, 0))
	rec := record(s)

	// Call 1: 1.5 s to the first token, then 2 s of streaming, 100 tokens.
	send(t, s, id, working("thinking"))
	clk.add(1500 * time.Millisecond)
	send(t, s, id, thinkingDelta("a1", 0, "hm"))
	clk.add(2 * time.Second)
	send(t, s, id, created(msg("a1", roleAssistant, "ok", 2, "u1"), ""))
	send(t, s, id, usageWithOutput(id, 100))

	// A tool runs for 3 s.
	send(t, s, id, working("executing_tool"))
	clk.add(3 * time.Second)

	// Call 2: 0.5 s to the first token, 1 s of streaming, 300 tokens; the
	// usage arrives before the message this time.
	send(t, s, id, working("streaming_assistant_message"))
	clk.add(500 * time.Millisecond)
	send(t, s, id, textDelta("a2", 0, "done"))
	clk.add(time.Second)
	send(t, s, id, usageWithOutput(id, 400))
	send(t, s, id, created(msg("a2", roleAssistant, "done", 3, "a1"), ""))
	send(t, s, id, working("idle"))

	st := s.Session(id).Stats()
	if st.Calls != 2 || st.ModelMs != 5000 || st.ToolMs != 3000 {
		t.Fatalf("calls/model/tool = %d/%v/%v, want 2/5000/3000", st.Calls, st.ModelMs, st.ToolMs)
	}
	if st.MeanTTFTMs() != 1000 {
		t.Fatalf("mean TTFT = %v, want 1000", st.MeanTTFTMs())
	}
	if got := st.TokensPerSecond(); got != 400.0/3 {
		t.Fatalf("tok/s = %v, want %v", got, 400.0/3)
	}
	if st.Last == nil || st.Last.TTFTMs != 500 || st.Last.OutputTokens != 300 || st.Last.TokensPerSecond() != 300 {
		t.Fatalf("last call = %+v, want 500 ms TTFT, 300 tokens at 300 tok/s", st.Last)
	}
	if !rec.has(Event{Kind: EventStatsUpdated, SessionID: id}) {
		t.Fatal("expected a stats event")
	}
}

func TestStatsSkipCallsWithoutAMessage(t *testing.T) {
	s, clk := newTestStore()
	const id = "s1"
	loadWith(t, s, id, protocol.LoadSessionResult{Session: protocol.Session{Messages: []protocol.FactoryDroidMessage{msg("u1", roleUser, "hi", 1, "")}}})

	// Approving a tool streams before the tool runs; that is no model call.
	send(t, s, id, working("streaming_assistant_message"))
	clk.add(time.Second)
	send(t, s, id, working("executing_tool"))
	clk.add(time.Second)
	send(t, s, id, working("thinking"))
	clk.add(time.Second)
	send(t, s, id, working("idle"))

	st := s.Session(id).Stats()
	if st.Calls != 0 || st.ModelMs != 0 || st.ToolMs != 1000 {
		t.Fatalf("stats = %+v, want only 1000 ms of tool time", st)
	}
}

func TestStatsBurstHasNoSpeed(t *testing.T) {
	s, clk := newTestStore()
	const id = "s1"
	loadWith(t, s, id, protocol.LoadSessionResult{Session: protocol.Session{Messages: []protocol.FactoryDroidMessage{msg("u1", roleUser, "hi", 1, "")}}})
	send(t, s, id, usageWithOutput(id, 0))
	send(t, s, id, working("thinking"))
	clk.add(time.Second)
	send(t, s, id, textDelta("a1", 0, "all at once"))
	clk.add(5 * time.Millisecond)
	send(t, s, id, created(msg("a1", roleAssistant, "all at once", 2, "u1"), ""))
	send(t, s, id, usageWithOutput(id, 50))

	st := s.Session(id).Stats()
	if st.TokensPerSecond() != 0 || st.Last.TokensPerSecond() != 0 || st.Last.OutputTokens != 50 {
		t.Fatalf("a 5 ms burst must not give a speed: %+v, last %+v", st, st.Last)
	}
}

func TestStatsSeedAddsUp(t *testing.T) {
	s, clk := newTestStore()
	const id = "s1"
	s.SetStatsSeed(func(sid string) (Stats, bool) {
		return Stats{Calls: 3, ModelMs: 9000}, sid == id
	})
	loadWith(t, s, id, protocol.LoadSessionResult{Session: protocol.Session{Messages: []protocol.FactoryDroidMessage{msg("u1", roleUser, "hi", 1, "")}}})
	send(t, s, id, working("thinking"))
	clk.add(time.Second)
	send(t, s, id, created(msg("a1", roleAssistant, "ok", 2, "u1"), ""))

	if st := s.Session(id).Stats(); st.Calls != 4 || st.ModelMs != 10000 {
		t.Fatalf("stats = %+v, want the seed plus one call", st)
	}
}
