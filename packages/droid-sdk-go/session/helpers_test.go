package session

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTestStore() (*Store, *clock) {
	c := &clock{t: time.UnixMilli(1_700_000_000_000)}
	s := NewStore()
	s.now = c.now
	return s, c
}

// send applies a notification given in its wire shape.
func send(t *testing.T, s *Store, sessionID string, n map[string]any) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"sessionId": sessionID, "notification": n})
	if err != nil {
		t.Fatal(err)
	}
	var p protocol.SessionNotificationParams
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleNotification(p, HandleOptions{}); err != nil {
		t.Fatal(err)
	}
}

func working(state string) map[string]any {
	return map[string]any{"type": "droid_working_state_changed", "newState": state}
}

func textDelta(messageID string, block int, delta string) map[string]any {
	return map[string]any{"type": "assistant_text_delta", "messageId": messageID, "blockIndex": block, "textDelta": delta}
}

func created(m protocol.FactoryDroidMessage, requestID string) map[string]any {
	n := map[string]any{"type": "create_message", "message": m}
	if requestID != "" {
		n["requestId"] = requestID
	}
	return n
}

func textBlock(text string) protocol.ContentBlock {
	return blockOf(protocol.TextBlock{Type: blockText, Text: text})
}

func toolUseBlock(id, name string, input map[string]any) protocol.ContentBlock {
	return blockOf(map[string]any{"type": blockToolUse, "id": id, "name": name, "input": input})
}

func msg(id string, role protocol.MessageRole, text string, createdAt float64, parent string) protocol.FactoryDroidMessage {
	return protocol.FactoryDroidMessage{
		ID: id, Role: role, Content: []protocol.ContentBlock{textBlock(text)},
		CreatedAt: createdAt, UpdatedAt: createdAt, ParentID: parent,
	}
}

func textOf(m protocol.FactoryDroidMessage) string {
	var parts []string
	for _, b := range m.Content {
		if b.Type == blockText {
			parts = append(parts, decode[textView](b).Text)
		}
	}
	return strings.Join(parts, "|")
}

func idsOf(msgs []protocol.FactoryDroidMessage) string {
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	return strings.Join(ids, ",")
}

func loadWith(t *testing.T, s *Store, id string, res protocol.LoadSessionResult) LoadOutcome {
	t.Helper()
	tok := s.BeginLoad(id)
	return s.ApplyLoadResult(tok, &res, 0)
}

type recorder struct {
	mu     sync.Mutex
	events []Event
}

func record(s *Store) *recorder {
	r := &recorder{}
	s.Subscribe(func(e Event) {
		r.mu.Lock()
		r.events = append(r.events, e)
		r.mu.Unlock()
	})
	return r
}

func (r *recorder) has(want Event) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e == want {
			return true
		}
	}
	return false
}

func (r *recorder) reset() {
	r.mu.Lock()
	r.events = nil
	r.mu.Unlock()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

var zeroUsage = map[string]any{"inputTokens": 0, "outputTokens": 0, "cacheCreationTokens": 0, "cacheReadTokens": 0, "thinkingTokens": 0}
