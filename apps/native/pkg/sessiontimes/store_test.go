package sessiontimes

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

type clock struct{ ms int64 }

func (c *clock) now() time.Time { return time.UnixMilli(c.ms) }

func TestATurnEndsOnceAndReadsWhenShown(t *testing.T) {
	c := &clock{ms: 1000}
	s := Open("", c.now)
	s.Ended("loaded")
	if _, ok := s.Snapshot().Sessions["loaded"]; ok {
		t.Fatal("an idle report of a Session never seen working ended a turn")
	}
	s.Sent("a")
	c.ms = 2000
	s.Working("a")
	c.ms = 3000
	s.Ended("a")
	c.ms = 3001
	// The same report on another Client's socket.
	s.Ended("a")
	got := s.Snapshot().Sessions["a"]
	if got != (sessions.Times{SentAt: 1000, EndedAt: 3000}) || !got.Unread() {
		t.Fatalf("times %+v", got)
	}
	c.ms = 4000
	s.Viewed("a")
	if s.Snapshot().Sessions["a"].Unread() {
		t.Fatal("still unread once shown")
	}
	// A Loop's turn, which no message of the user's starts.
	s.Working("a")
	c.ms = 5000
	s.Ended("a")
	if got := s.Snapshot().Sessions["a"]; got.SentAt != 1000 || !got.Unread() {
		t.Fatalf("times %+v", got)
	}
}

func TestWaitAnswersAChange(t *testing.T) {
	s := Open("", nil)
	first := s.Snapshot()
	if got := s.Wait(context.Background(), 0); got.Rev != first.Rev {
		t.Fatalf("an old revision waited: %d", got.Rev)
	}
	done := make(chan sessions.TimesSnapshot)
	go func() { done <- s.Wait(context.Background(), first.Rev) }()
	s.Sent("a")
	got := <-done
	if got.Rev == first.Rev || got.Sessions["a"].SentAt == 0 {
		t.Fatalf("the wait missed the change: %+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if again := s.Wait(ctx, got.Rev); again.Rev != got.Rev || len(again.Sessions) != 0 || again.Full {
		t.Fatalf("an ended wait answered %+v", again)
	}
}

// A Client that has a revision of this launch gets only what changed
// since; one with another launch's, or none, gets every Session's Times.
func TestSinceAnswersTheChanges(t *testing.T) {
	s := Open("", nil)
	s.Sent("a")
	s.Sent("b")
	mid := s.Snapshot().Rev
	s.Viewed("b")
	got := s.Since(mid)
	if got.Full || len(got.Sessions) != 1 || got.Sessions["b"].ViewedAt == 0 {
		t.Fatalf("since %d: %+v", mid, got)
	}
	if all := s.Since(0); !all.Full || len(all.Sessions) != 2 {
		t.Fatalf("since 0: %+v", all)
	}
	merged := got.Apply(map[string]sessions.Times{"a": {SentAt: 1}})
	if merged["a"].SentAt != 1 || merged["b"].ViewedAt == 0 {
		t.Fatalf("applied %+v", merged)
	}
}

func TestTheStoreSurvivesARelaunch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-times.json")
	c := &clock{ms: 7000}
	s := Open(path, c.now)
	s.Sent("a")
	s.Viewed("b")
	s.Flush()
	again := Open(path, func() time.Time { return time.UnixMilli(9000) })
	if got := again.Snapshot(); got.Sessions["a"].SentAt != 7000 || got.Sessions["b"].ViewedAt != 7000 || got.Rev <= s.Snapshot().Rev {
		t.Fatalf("after a relaunch %+v", got)
	}
}

func line(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

type msg struct {
	Role          string           `json:"role"`
	Content       []map[string]any `json:"content"`
	Visibility    string           `json:"visibility,omitempty"`
	HookEventName string           `json:"hookEventName,omitempty"`
}

// message is a transcript line in droid's order: the type and the role
// ahead of the content.
func message(ts, role string, content []map[string]any, extra msg) any {
	extra.Role, extra.Content = role, content
	return struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		Timestamp string `json:"timestamp"`
		Message   msg    `json:"message"`
	}{"message", ts, ts, extra}
}

func text(s string) []map[string]any { return []map[string]any{{"type": "text", "text": s}} }

// The messages sent before the Store kept them date the Sessions: the last
// one the user typed, not a tool result, a hook's record or what droid
// adds for the model, however long the lines.
func TestBackfillFindsTheLastTypedMessage(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "-Users-me-dev-alpha")
	if err := os.MkdirAll(ws, 0o700); err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("x", 3*readChunk)
	transcript := line(t, map[string]any{"type": "session_start", "id": "s1", "title": "Alpha", "cwd": "/Users/me/dev/alpha"}) +
		line(t, message("2026-10-01T10:00:00.000Z", "user", text("first"), msg{})) +
		line(t, message("2026-10-02T10:00:00.000Z", "user", text(huge), msg{})) +
		line(t, message("2026-10-02T10:01:00.000Z", "assistant", text("ok"), msg{})) +
		line(t, message("2026-10-03T10:00:00.000Z", "user", []map[string]any{{"type": "tool_result", "content": huge}}, msg{})) +
		line(t, message("2026-10-04T10:00:00.000Z", "user", text("reminder"), msg{Visibility: "llm_only"})) +
		line(t, message("2026-10-05T10:00:00.000Z", "user", nil, msg{HookEventName: "SessionStart", Visibility: "user_only"})) +
		line(t, map[string]any{"type": "agent_turn_outcome", "turnId": "t"})
	if err := os.WriteFile(filepath.Join(ws, "s1.jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "s2.jsonl"), []byte(line(t, message("2026-10-06T10:00:00.000Z", "assistant", text("only me"), msg{}))), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Open("", func() time.Time { return time.UnixMilli(1) })
	s.Sent("s1")
	if err := s.Backfill(dir); err != nil {
		t.Fatal(err)
	}
	want, _ := time.Parse(time.RFC3339, "2026-10-02T10:00:00Z")
	got := s.Snapshot().Sessions
	if got["s1"].SentAt != want.UnixMilli() {
		t.Fatalf("s1 dated %d, want %d", got["s1"].SentAt, want.UnixMilli())
	}
	if _, ok := got["s2"]; ok {
		t.Fatal("a Session the user never typed in was dated")
	}
	s.Sent("s1")
	if err := s.Backfill(dir); err != nil || s.Snapshot().Sessions["s1"].SentAt != want.UnixMilli() {
		t.Fatal("a second backfill ran")
	}
}
