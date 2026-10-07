package droid_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

func dial(t *testing.T, d *fakedaemon.Daemon, record func(droid.Direction, []byte)) *droid.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := droid.Dial(ctx, droid.Options{URL: d.URL, Credential: &droid.Credential{APIKey: "fk-test"}, Record: record})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestListAndLoadSession(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{
		Title: "Fix the login race", Cwd: "/Users/dev/acme-web",
		Messages: []fakedaemon.Message{{Role: "user", Text: "Why does login fail?"}, {Role: "assistant", Text: "The refresh is not awaited."}},
	}}})
	c := dial(t, d, nil)
	ctx := context.Background()

	list, err := c.ListAvailableSessions(ctx, protocol.ListAvailableSessionsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Sessions) != 1 || list.Sessions[0].Title != "Fix the login race" {
		t.Fatalf("sessions: %+v", list.Sessions)
	}
	loaded, err := c.LoadSession(ctx, protocol.LoadSessionParams{SessionID: list.Sessions[0].SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Session.Messages) != 2 {
		t.Fatalf("messages: %d", len(loaded.Session.Messages))
	}
	if loaded.Session.Messages[1].Role != "assistant" {
		t.Fatalf("second message role %q", loaded.Session.Messages[1].Role)
	}
}

func TestStreamedReply(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "Chat", Cwd: "/Users/dev/acme-web"}},
		Turn:     &fakedaemon.Turn{Kind: "reply", Deltas: []string{"Hel", "lo ", "there"}},
	})
	c := dial(t, d, nil)
	ctx := context.Background()
	id := d.Sessions[0].SessionID
	if _, err := c.LoadSession(ctx, protocol.LoadSessionParams{SessionID: id}); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var text strings.Builder
	idle := make(chan struct{}, 1)
	sawWorking := false
	c.Subscribe(func(n droid.Notification) {
		if n.Method != protocol.NotificationSessionNotification {
			return
		}
		var p protocol.SessionNotificationParams
		if err := json.Unmarshal(n.Params, &p); err != nil {
			t.Error(err)
			return
		}
		v, err := p.Notification.Value()
		if err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch v := v.(type) {
		case *protocol.AssistantTextDeltaNotification:
			text.WriteString(v.TextDelta)
		case *protocol.DroidWorkingStateChangedNotification:
			if v.NewState != protocol.DroidWorkingStateIdle {
				sawWorking = true
			} else if sawWorking {
				select {
				case idle <- struct{}{}:
				default:
				}
			}
		}
	})
	if _, err := c.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: id, Text: "Say hello"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-idle:
	case <-time.After(10 * time.Second):
		t.Fatal("turn did not end")
	}
	mu.Lock()
	defer mu.Unlock()
	if got := text.String(); got != "Hello there" {
		t.Fatalf("streamed %q", got)
	}
}

func TestAuthRejectedAndBadToken(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := droid.Dial(ctx, droid.Options{URL: strings.Replace(d.URL, "token=", "token=wrong", 1)})
	var de *droid.DialError
	if err == nil || !asDial(err, &de) || de.StatusCode != 401 {
		t.Fatalf("want a 401 dial error, got %v", err)
	}
}

func asDial(err error, target **droid.DialError) bool {
	de, ok := err.(*droid.DialError)
	if ok {
		*target = de
	}
	return ok
}
