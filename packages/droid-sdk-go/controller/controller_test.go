package controller_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// recorder keeps a Controller's events for a test to wait on.
type recorder struct {
	mu     sync.Mutex
	events []controller.Event
	ch     chan struct{}
}

func record(c *controller.Controller) *recorder {
	r := &recorder{ch: make(chan struct{}, 1)}
	c.Subscribe(func(e controller.Event) {
		r.mu.Lock()
		r.events = append(r.events, e)
		r.mu.Unlock()
		select {
		case r.ch <- struct{}{}:
		default:
		}
	})
	return r
}

// wait returns the first event since index from on that ok accepts, and the
// index after it.
func (r *recorder) wait(t *testing.T, from int, what string, ok func(controller.Event) bool) (controller.Event, int) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		r.mu.Lock()
		for i := from; i < len(r.events); i++ {
			if ok(r.events[i]) {
				e := r.events[i]
				r.mu.Unlock()
				return e, i + 1
			}
		}
		r.mu.Unlock()
		select {
		case <-r.ch:
		case <-deadline:
			t.Fatalf("no event: %s", what)
		}
	}
}

func start(t *testing.T, d *fakedaemon.Daemon, cfg controller.Config) (*controller.Controller, *recorder) {
	t.Helper()
	cfg.URL = d.URL
	if cfg.Credential == nil {
		cfg.Credential = func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: "fk-test"}, nil }
	}
	c := controller.New(cfg)
	t.Cleanup(func() { c.Close() })
	r := record(c)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	return c, r
}

func turnEnded(e controller.Event) bool {
	n, ok := e.(controller.SessionNotification)
	if !ok {
		return false
	}
	_, ok = n.Value.(*protocol.AgentTurnCompletedNotification)
	return ok
}

func streamedText(r *recorder) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	for _, e := range r.events {
		if n, ok := e.(controller.SessionNotification); ok {
			if d, ok := n.Value.(*protocol.AssistantTextDeltaNotification); ok {
				b.WriteString(d.TextDelta)
			}
		}
	}
	return b.String()
}

func TestPermissionTurn(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "Chat", Cwd: "/Users/dev/acme-web"}},
		Turn:     &fakedaemon.Turn{Kind: "permission", Command: "npm test", Reply: "All tests pass."},
	})
	c, r := start(t, d, controller.Config{})
	ctx := context.Background()
	id := d.Sessions[0].SessionID
	if _, err := c.LoadSession(ctx, protocol.LoadSessionParams{SessionID: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: id, Text: "Run the tests"}); err != nil {
		t.Fatal(err)
	}
	e, next := r.wait(t, 0, "permission requested", func(e controller.Event) bool { _, ok := e.(controller.PermissionRequested); return ok })
	p := e.(controller.PermissionRequested).Permission
	if ws := c.Store().Session(id).WorkingState(); ws != protocol.DroidWorkingStateWaitingForToolConfirmation {
		t.Fatalf("working state while asked: %s", ws)
	}
	if p.SessionID != id || len(p.ToolUses) != 1 || p.ToolUses[0].ToolUse.Name != "Execute" {
		t.Fatalf("permission: %+v", p)
	}
	if got := c.PendingPermissions(id); len(got) != 1 {
		t.Fatalf("pending: %d", len(got))
	}
	if err := c.RespondToPermission(ctx, p.RequestID, controller.PermissionAnswer{SelectedOption: "not-an-option"}); err == nil {
		t.Fatal("an option the permission does not offer was taken")
	}
	if err := c.RespondToPermission(ctx, p.RequestID, controller.PermissionAnswer{SelectedOption: protocol.ToolConfirmationOutcomeProceedOnce}); err != nil {
		t.Fatal(err)
	}
	r.wait(t, next, "turn end", turnEnded)
	if got := c.PendingPermissions(""); len(got) != 0 {
		t.Fatalf("still pending: %+v", got)
	}
	if got := streamedText(r); !strings.Contains(got, "All tests pass.") {
		t.Fatalf("streamed %q", got)
	}
	if err := c.RespondToPermission(ctx, p.RequestID, controller.PermissionAnswer{SelectedOption: protocol.ToolConfirmationOutcomeProceedOnce}); err != controller.ErrPromptNotFound {
		t.Fatalf("second answer: %v", err)
	}
}

func TestAskUserTurn(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "Chat", Cwd: "/Users/dev/acme-web"}},
		Turn:     &fakedaemon.Turn{Kind: "askUser", Question: "Which database?", Options: []string{"Postgres", "SQLite"}},
	})
	c, r := start(t, d, controller.Config{})
	ctx := context.Background()
	id := d.Sessions[0].SessionID
	if _, err := c.LoadSession(ctx, protocol.LoadSessionParams{SessionID: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: id, Text: "Set up storage"}); err != nil {
		t.Fatal(err)
	}
	e, next := r.wait(t, 0, "AskUser requested", func(e controller.Event) bool { _, ok := e.(controller.AskUserRequested); return ok })
	a := e.(controller.AskUserRequested).AskUser
	if len(a.Questions) != 1 || a.Questions[0].Question != "Which database?" {
		t.Fatalf("questions: %+v", a.Questions)
	}
	if err := c.RespondToAskUser(ctx, a.RequestID, protocol.AskUserResult{}); err == nil {
		t.Fatal("an answer missing a question was taken")
	}
	q := a.Questions[0]
	if err := c.RespondToAskUser(ctx, a.RequestID, protocol.AskUserResult{Answers: []protocol.AskUserCollectedAnswer{{Index: q.Index, Question: q.Question, Answer: "SQLite"}}}); err != nil {
		t.Fatal(err)
	}
	r.wait(t, next, "turn end", turnEnded)
	if got := streamedText(r); !strings.Contains(got, "You chose SQLite.") {
		t.Fatalf("streamed %q", got)
	}
}

func TestReconnectLoadsSessionsAgain(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "Chat", Cwd: "/Users/dev/acme-web"}},
		Turn:     &fakedaemon.Turn{Kind: "reply", Deltas: []string{"back ", "again"}},
	})
	c, r := start(t, d, controller.Config{ReconnectDelay: 200 * time.Millisecond, MaxReconnectAttempts: 10})
	ctx := context.Background()
	id := d.Sessions[0].SessionID
	if _, err := c.LoadSession(ctx, protocol.LoadSessionParams{SessionID: id}); err != nil {
		t.Fatal(err)
	}
	if err := d.GoDown(); err != nil {
		t.Fatal(err)
	}
	_, next := r.wait(t, 0, "reconnecting", func(e controller.Event) bool {
		s, ok := e.(controller.StatusChanged)
		return ok && s.Status.Reconnecting && !s.Status.Connected
	})
	if _, err := c.Client(); err != controller.ErrNotConnected {
		t.Fatalf("client while down: %v", err)
	}
	if err := d.ComeBack(); err != nil {
		t.Fatal(err)
	}
	_, next = r.wait(t, next, "session loaded again", func(e controller.Event) bool {
		l, ok := e.(controller.SessionLoaded)
		return ok && l.SessionID == id
	})
	if !c.Status().Connected {
		t.Fatal("not connected after reload")
	}
	if _, err := c.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: id, Text: "Still there?"}); err != nil {
		t.Fatal(err)
	}
	r.wait(t, next, "idle after the turn", func(e controller.Event) bool {
		n, ok := e.(controller.SessionNotification)
		if !ok {
			return false
		}
		s, ok := n.Value.(*protocol.DroidWorkingStateChangedNotification)
		return ok && s.NewState == protocol.DroidWorkingStateIdle
	})
	if got := streamedText(r); got != "back again" {
		t.Fatalf("streamed %q", got)
	}
	s := c.Store().Session(id)
	if s == nil {
		t.Fatal("session not in the store")
	}
	msgs := s.Messages()
	last := msgs[len(msgs)-1]
	if last.Role != "assistant" || len(last.Content) == 0 {
		t.Fatalf("last message %+v", last)
	}
	if ws := s.WorkingState(); ws != protocol.DroidWorkingStateIdle {
		t.Fatalf("working state %s", ws)
	}
	if q := s.QueuedMessages(); len(q) != 0 {
		t.Fatalf("queued %+v", q)
	}
}

func TestConnectWaitsForTheDaemon(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{})
	if err := d.GoDown(); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(1500 * time.Millisecond)
		d.ComeBack()
	}()
	// Attempts against a Daemon that is not up do not count, so one is enough.
	start(t, d, controller.Config{MaxConnectAttempts: 1})
}

func TestRefusedCredentialStopsConnect(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{})
	c := controller.New(controller.Config{URL: strings.Replace(d.URL, "token=", "token=wrong", 1)})
	defer c.Close()
	begin := time.Now()
	err := c.Connect(context.Background())
	ce, ok := err.(*controller.ConnectionError)
	if !ok || ce.Reason != controller.ReasonAuthRejected || ce.Retryable {
		t.Fatalf("want a refused credential, got %v", err)
	}
	if time.Since(begin) > 3*time.Second {
		t.Fatal("kept trying a refused credential")
	}
}

func TestResolveURLIsAskedOnEveryConnect(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{})
	var mu sync.Mutex
	asked := 0
	c := controller.New(controller.Config{
		URL: "ws://127.0.0.1:1/unused",
		ResolveURL: func(context.Context) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			asked++
			if asked == 1 {
				return "", errors.New("not started yet")
			}
			return d.URL, nil
		},
		Credential:         func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: "fk-test"}, nil },
		MaxConnectAttempts: 1,
	})
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// A URL not known yet counts as a Daemon not spawned, so one attempt waits for it.
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if asked != 2 {
		t.Fatalf("asked %d times", asked)
	}
}

func TestAddUserMessageKeepsTheCallersRequestID(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "Chat", Cwd: "/Users/dev/acme-web"}},
		Turn:     &fakedaemon.Turn{Kind: "reply", Deltas: []string{"ok"}},
	})
	c, r := start(t, d, controller.Config{})
	ctx := context.Background()
	id := d.Sessions[0].SessionID
	if _, err := c.LoadSession(ctx, protocol.LoadSessionParams{SessionID: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddUserMessage(droid.WithRequestID(ctx, "req-1"), protocol.AddUserMessageParams{SessionID: id, Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	r.wait(t, 0, "create_message echoing req-1", func(e controller.Event) bool {
		n, ok := e.(controller.SessionNotification)
		if !ok {
			return false
		}
		m, ok := n.Value.(*protocol.CreateMessageNotification)
		return ok && m.RequestID == "req-1"
	})
}
