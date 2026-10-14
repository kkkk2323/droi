package chatbridge_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/internal/chatbridge/fakewecom"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

// working is a turn that lasts longer than any test: the Chat Session works
// from its first message on.
var working = &fakedaemon.Turn{Kind: "reply", Deltas: []string{"working"}, DelayMs: 60_000}

// turns are the Chat Sessions' turns as another Client of the Daemon sees
// them end: the Daemon tells every connection.
type turns struct {
	t       *testing.T
	mu      sync.Mutex
	ended   map[string]int
	changed chan struct{}
}

// watchTurns starts watching; call it before the turns to wait for start.
func (r *rig) watchTurns() *turns {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := droid.Dial(ctx, droid.Options{URL: r.d.URL, Credential: &droid.Credential{APIKey: "fk-test"}})
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = c.Close() })
	w := &turns{t: r.t, ended: map[string]int{}, changed: make(chan struct{})}
	c.Subscribe(func(n droid.Notification) {
		var p struct {
			SessionID    string `json:"sessionId"`
			Notification struct {
				Type     string `json:"type"`
				NewState string `json:"newState"`
			} `json:"notification"`
		}
		if json.Unmarshal(n.Params, &p) != nil || p.Notification.Type != "droid_working_state_changed" || p.Notification.NewState != "idle" {
			return
		}
		w.mu.Lock()
		w.ended[p.SessionID]++
		close(w.changed)
		w.changed = make(chan struct{})
		w.mu.Unlock()
	})
	return w
}

// wait waits until the Session's nth turn is over. The Chat Bridge may not
// have seen it yet: a barrier after it makes sure, since the Daemon's
// answer to the barrier comes after the turn's end on the Chat Bridge's
// connection.
func (w *turns) wait(sessionID string, nth int) {
	w.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		w.mu.Lock()
		n, ch := w.ended[sessionID], w.changed
		w.mu.Unlock()
		if n >= nth {
			return
		}
		select {
		case <-ch:
		case <-deadline:
			w.t.Fatalf("turn %d of %s did not end", nth, sessionID)
		}
	}
}

func TestAnAtWhileItsChatSessionWorksSendsANoticeWithoutTheText(t *testing.T) {
	r := startOn(t, fakedaemon.Scenario{Turn: working}, func(s *host.Settings) { s.ChatBridge.AllowedUsers = []string{"HuangJunXiang", "LiLei"} }, nil)
	post := r.newPost("Fix the nightly job")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	p := r.initialized(1)
	r.message(1)

	reply := r.w.AddReply(topic, post, fakewecom.Post{
		From: "LiLei", Text: "${inline_mention:b}\u200b the disk on build-7 is full", Mentions: []fakewecom.Mention{atRobot("b")},
	})
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, ReplyID: reply, From: "LiLei"})

	m := r.message(2)
	if m.SessionID != p.SessionID {
		t.Errorf("the notice went to %s, the post's Chat Session is %s", m.SessionID, p.SessionID)
	}
	if strings.Contains(m.Text, "disk") || strings.Contains(m.Text, "build-7") {
		t.Errorf("the notice carries the reply's text:\n%s", m.Text)
	}
	for _, want := range []string{"LiLei", reply, "read_thread"} {
		if !strings.Contains(m.Text, want) {
			t.Errorf("notice without %q:\n%s", want, m.Text)
		}
	}
	if m.QueuePlacement != "end_of_turn" {
		t.Errorf("the notice is queued %q, not into the running turn", m.QueuePlacement)
	}
	if n := r.count("daemon.initialize_session"); n != 1 {
		t.Errorf("%d Chat Sessions for one post", n)
	}

	thread, isError := r.droid(p).call("read_thread", nil)
	if isError || !strings.Contains(thread, "the disk on build-7 is full") {
		t.Errorf("read_thread answered %q (error %v)", thread, isError)
	}
}

func TestADirectMessageWhileItsChatSessionWorksGoesIntoTheTurnInFull(t *testing.T) {
	r := startOn(t, fakedaemon.Scenario{Turn: working}, nil, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Fix the nightly job"})
	p := r.initialized(1)
	if m := r.message(1); m.QueuePlacement != "" {
		t.Errorf("the first message is queued %q, the Session was idle", m.QueuePlacement)
	}

	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Also: the disk on build-7 is full"})
	m := r.message(2)
	if m.SessionID != p.SessionID || !strings.Contains(m.Text, "Also: the disk on build-7 is full") {
		t.Errorf("second message %+v; the Chat Session is %s", m, p.SessionID)
	}
	if m.QueuePlacement != "end_of_turn" {
		t.Errorf("the message is queued %q, not into the running turn", m.QueuePlacement)
	}
}

func TestNewWhileItsChatSessionWorksStillStartsOver(t *testing.T) {
	r := startOn(t, fakedaemon.Scenario{Turn: working}, nil, nil)
	post := r.newPost("Fix the nightly job")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	first := r.initialized(1)
	r.message(1)

	reply := r.w.AddReply(topic, post, fakewecom.Post{
		From: "HuangJunXiang", Text: "${inline_mention:b}\u200b !new look at build-7 instead", Mentions: []fakewecom.Mention{atRobot("b")},
	})
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, ReplyID: reply, From: "HuangJunXiang"})

	second := r.initialized(2)
	m := r.message(2)
	if second.SessionID == first.SessionID || m.SessionID != second.SessionID || !strings.Contains(m.Text, "look at build-7 instead") {
		t.Errorf("!new while working sent %+v; the new Chat Session is %s", m, second.SessionID)
	}
}
