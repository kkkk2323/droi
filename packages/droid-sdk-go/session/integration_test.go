package session_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/session"
)

func TestStoreFollowsAFakeDaemonTurn(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{
			Title: "Chat", Cwd: "/Users/dev/acme-web",
			Messages: []fakedaemon.Message{{Role: "user", Text: "Hi"}, {Role: "assistant", Text: "Hello."}},
		}},
		Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"Hel", "lo ", "there"}, DelayMs: 20},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := droid.Dial(ctx, droid.Options{URL: d.URL, Credential: &droid.Credential{APIKey: "fk-test"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	id := d.Sessions[0].SessionID

	store := session.NewStore()
	idle := make(chan struct{}, 1)
	var worked, streamed atomic.Bool
	store.Subscribe(func(e session.Event) {
		if e.SessionID != id {
			return
		}
		if e.Streaming {
			streamed.Store(true)
		}
		if e.Kind != session.EventWorkingStateChanged {
			return
		}
		if store.Session(id).WorkingState() != protocol.DroidWorkingStateIdle {
			worked.Store(true)
		} else if worked.Load() {
			select {
			case idle <- struct{}{}:
			default:
			}
		}
	})
	c.Subscribe(func(n droid.Notification) {
		if n.Method != protocol.NotificationSessionNotification {
			return
		}
		var p protocol.SessionNotificationParams
		if err := json.Unmarshal(n.Params, &p); err != nil {
			t.Error(err)
			return
		}
		if err := store.HandleNotification(p, session.HandleOptions{}); err != nil {
			t.Error(err)
		}
	})

	tok := store.BeginLoad(id)
	res, err := c.LoadSession(ctx, protocol.LoadSessionParams{SessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	out := store.ApplyLoadResult(tok, res, 0)
	store.ApplyLoadedWorkingState(tok, out.ReportedWorkingState)
	sess := store.Session(id)
	if sess.LoadState() != session.Loaded || sess.MessageCount() != 2 || sess.Cwd() != "/Users/dev/acme-web" {
		t.Fatalf("loaded: %s, %d messages, cwd %q", sess.LoadState(), sess.MessageCount(), sess.Cwd())
	}

	if _, err := c.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: id, Text: "Say hello", MessageID: "user-1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-idle:
	case <-ctx.Done():
		t.Fatal("turn did not end")
	}

	msgs := sess.DisplayMessages()
	if len(msgs) != 4 {
		t.Fatalf("%d messages", len(msgs))
	}
	if msgs[2].ID != "user-1" || msgs[2].Role != protocol.MessageRoleUser {
		t.Fatalf("user message %+v", msgs[2])
	}
	reply := msgs[3]
	if reply.Role != protocol.MessageRoleAssistant || reply.ParentID != "user-1" || len(reply.Content) != 1 {
		t.Fatalf("reply %+v", reply)
	}
	var text struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(reply.Content[0].Raw, &text); err != nil || text.Text != "Hello there" || session.IsStreaming(reply.Content[0]) {
		t.Fatalf("reply block %s", reply.Content[0].Raw)
	}
	if sess.WorkingState() != protocol.DroidWorkingStateIdle || sess.AgentTurnCompletionReason() != protocol.AgentTurnCompletionReasonCompleted {
		t.Fatalf("working %s, completion %q", sess.WorkingState(), sess.AgentTurnCompletionReason())
	}
	if u := sess.Usage(); u.LastCall == nil || u.LastCall.InputTokens == 0 {
		t.Fatalf("usage %+v", u)
	}
	if !streamed.Load() {
		t.Fatal("no streaming events")
	}
	if strings.TrimSpace(sess.Title()) == "" {
		t.Fatal("no title")
	}
}
