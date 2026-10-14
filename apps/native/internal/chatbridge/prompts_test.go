package chatbridge_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/chatbridge/fakewecom"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

var permissionTurn = fakedaemon.Turn{Kind: "permission", Command: "git push origin main", Reply: "Pushed."}

// client is another Client of the Daemon, such as Droi's window: it sees
// the same Prompts and can answer them first.
type client struct {
	t   *testing.T
	ctl *controller.Controller

	mu      sync.Mutex
	events  []controller.Event
	changed chan struct{}
}

// window connects another Client to the Daemon, as Droi's window; connect it before the
// Prompt is asked, as the Daemon sends a Prompt to the Clients it has then.
func (r *rig) window() *client {
	r.t.Helper()
	c := &client{t: r.t, changed: make(chan struct{})}
	c.ctl = controller.New(controller.Config{
		URL:        r.d.URL,
		Credential: func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: "fk-test"}, nil },
	})
	r.t.Cleanup(func() { _ = c.ctl.Close() })
	c.ctl.Subscribe(func(e controller.Event) {
		c.mu.Lock()
		c.events = append(c.events, e)
		close(c.changed)
		c.changed = make(chan struct{})
		c.mu.Unlock()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.ctl.Connect(ctx); err != nil {
		r.t.Fatal(err)
	}
	return c
}

// waitFor waits for an event ok picks.
func (c *client) waitFor(what string, ok func(controller.Event) bool) controller.Event {
	c.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		c.mu.Lock()
		ch := c.changed
		for _, e := range c.events {
			if ok(e) {
				c.mu.Unlock()
				return e
			}
		}
		c.mu.Unlock()
		select {
		case <-ch:
		case <-deadline:
			c.t.Fatalf("the Client saw no %s", what)
		}
	}
}

// resolved waits for the Daemon to say how the permission was answered.
func (c *client) resolved() protocol.ToolConfirmationOutcome {
	c.t.Helper()
	e := c.waitFor("permission_resolved", func(e controller.Event) bool {
		n, ok := e.(controller.SessionNotification)
		_, resolved := n.Value.(*protocol.PermissionResolvedNotification)
		return ok && resolved
	})
	return e.(controller.SessionNotification).Value.(*protocol.PermissionResolvedNotification).SelectedOption
}

// card is a button_interaction card the robot sent or updated.
type card struct {
	CardType  string `json:"card_type"`
	MainTitle struct {
		Title string `json:"title"`
		Desc  string `json:"desc"`
	} `json:"main_title"`
	Buttons []struct {
		Text string `json:"text"`
		Key  string `json:"key"`
	} `json:"button_list"`
	Rows []struct {
		KeyName string `json:"keyname"`
		Value   string `json:"value"`
	} `json:"horizontal_content_list"`
	TaskID string `json:"task_id"`
	// text is everything the card shows, for checking what it says.
	text string
}

func cardIn(t *testing.T, body json.RawMessage) card {
	t.Helper()
	var b struct {
		MsgType string          `json:"msgtype"`
		Card    json.RawMessage `json:"template_card"`
	}
	if json.Unmarshal(body, &b) != nil || len(b.Card) == 0 {
		t.Fatalf("no template card in %s", body)
	}
	var c card
	_ = json.Unmarshal(b.Card, &c)
	var shown any
	_ = json.Unmarshal(b.Card, &shown)
	c.text = strings.Join(stringsIn(shown), "\n")
	return c
}

// stringsIn are the strings in a decoded JSON value but the card's keys and
// task id.
func stringsIn(v any) []string {
	var out []string
	switch v := v.(type) {
	case string:
		out = append(out, v)
	case []any:
		for _, x := range v {
			out = append(out, stringsIn(x)...)
		}
	case map[string]any:
		for k, x := range v {
			if k != "key" && k != "task_id" && k != "card_type" {
				out = append(out, stringsIn(x)...)
			}
		}
	}
	return out
}

// sentCard is the nth card the robot sent in a direct message.
func (r *rig) sentCard(nth int) card {
	r.t.Helper()
	n := 0
	var found *card
	deadline := time.Now().Add(10 * time.Second)
	for found == nil {
		n = 0
		for _, f := range append(r.w.Frames("aibot_respond_msg"), r.w.Frames("aibot_send_msg")...) {
			if strings.Contains(string(f.Body), `"template_card"`) {
				if n++; n == nth {
					c := cardIn(r.t, f.Body)
					found = &c
					break
				}
			}
		}
		if found == nil {
			if time.Now().After(deadline) {
				r.t.Fatalf("the robot sent no card %d", nth)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	return *found
}

// updatedCard is the card update the robot sent on a click's req_id, and
// whether WeCom took it.
func (r *rig) updatedCard(clickReqID string) (card, bool) {
	r.t.Helper()
	deadline := time.Now().Add(fakewecom.CardUpdateWindow)
	for {
		for _, f := range r.w.Frames("aibot_respond_update_msg") {
			if f.ReqID == clickReqID {
				return cardIn(r.t, f.Body), f.Errcode == 0
			}
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("the card was not updated within %v of the click", fakewecom.CardUpdateWindow)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// buttonFor is the key of the card's button that answers with label: the
// button shows the label, or the number of the row that does.
func (c card) buttonFor(t *testing.T, label string) string {
	t.Helper()
	text := label
	for _, row := range c.Rows {
		if row.Value == label {
			text = row.KeyName
		}
	}
	for _, b := range c.Buttons {
		if b.Text == text {
			return b.Key
		}
	}
	t.Fatalf("no button answers %q on the card:\n%s", label, c.text)
	return ""
}

func TestAPermissionRequestInADirectMessageIsACardAClickAnswers(t *testing.T) {
	r := startTurn(t, permissionTurn, nil, nil)
	other := r.window()
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Push it"})

	c := r.sentCard(1)
	if c.CardType != "button_interaction" || c.TaskID == "" {
		t.Fatalf("card %+v", c)
	}
	for _, want := range []string{"git push origin main", "Yes, allow", "Yes, and always allow low impact commands", "No, cancel"} {
		if !strings.Contains(c.text, want) {
			t.Errorf("the card does not show %q:\n%s", want, c.text)
		}
	}
	if len(c.Buttons) != 3 {
		t.Errorf("%d buttons for 3 options", len(c.Buttons))
	}

	click := r.w.Click(fakewecom.Click{From: "HuangJunXiang", TaskID: c.TaskID, Key: c.buttonFor(t, "Yes, allow")})
	if got := other.resolved(); got != protocol.ToolConfirmationOutcomeProceedOnce {
		t.Errorf("the Daemon got %q", got)
	}
	updated, ok := r.updatedCard(click)
	if !ok {
		t.Errorf("WeCom refused the card's update")
	}
	for _, want := range []string{"Yes, allow", "HuangJunXiang"} {
		if !strings.Contains(updated.text, want) {
			t.Errorf("the updated card does not show %q:\n%s", want, updated.text)
		}
	}
	if strings.Contains(updated.text, "No, cancel") {
		t.Errorf("the updated card still offers the options:\n%s", updated.text)
	}
}

// robotReply waits for the nth reply the robot posted under the post.
func (r *rig) robotReply(post string, nth int) string {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		n := 0
		for _, p := range r.w.Replies(topic, post) {
			if p.Robot != "" {
				if n++; n == nth {
					return p.Text
				}
			}
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("the robot posted no reply %d under the post", nth)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// robotReplies is how many replies the robot posted under the post.
func (r *rig) robotReplies(post string) int {
	n := 0
	for _, p := range r.w.Replies(topic, post) {
		if p.Robot != "" {
			n++
		}
	}
	return n
}

// atIn has a member reply under the post with an @ of the robot and text,
// as WeCom writes it.
func (r *rig) atIn(post, from, text string) {
	r.t.Helper()
	reply := r.w.AddReply(topic, post, fakewecom.Post{
		From: from, Text: "${inline_mention:ph_bot}\u200b " + text, Mentions: []fakewecom.Mention{atRobot("ph_bot")},
	})
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, ReplyID: reply, From: from})
}

// directText waits for the nth Markdown message the robot sent in a
// direct message.
func (r *rig) directText(nth int) string {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		n := 0
		for _, f := range append(r.w.Frames("aibot_respond_msg"), r.w.Frames("aibot_send_msg")...) {
			var b struct {
				Markdown struct {
					Content string `json:"content"`
				} `json:"markdown"`
			}
			if json.Unmarshal(f.Body, &b) == nil && b.Markdown.Content != "" {
				if n++; n == nth {
					return b.Markdown.Content
				}
			}
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("the robot sent no message %d in the direct message", nth)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// answered waits for the Daemon to show the AskUser's answer in the turn.
func (c *client) answered(answer string) {
	c.t.Helper()
	c.waitFor("answer "+answer, func(e controller.Event) bool {
		n, ok := e.(controller.SessionNotification)
		return ok && n.Raw.Type == "tool_result" && strings.Contains(string(n.Raw.Raw), "[answer] "+answer)
	})
}

// resolutions is how many permission_resolved the Daemon sent.
func (c *client) resolutions() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.events {
		if s, ok := e.(controller.SessionNotification); ok {
			if _, ok := s.Value.(*protocol.PermissionResolvedNotification); ok {
				n++
			}
		}
	}
	return n
}

func (c *client) permission() controller.Permission {
	c.t.Helper()
	return c.waitFor("permission request", func(e controller.Event) bool {
		_, ok := e.(controller.PermissionRequested)
		return ok
	}).(controller.PermissionRequested).Permission
}

func TestAPermissionRequestInAChannelIsNumberedTextAnAtWithTheNumberAnswers(t *testing.T) {
	r := startTurn(t, permissionTurn, nil, nil)
	other := r.window()
	post := r.channel("Push the release")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})

	asked := r.robotReply(post, 1)
	for _, want := range []string{"git push origin main", "1. Yes, allow", "2. Yes, and always allow low impact commands", "3. No, cancel", "@"} {
		if !strings.Contains(asked, want) {
			t.Errorf("the reply does not show %q:\n%s", want, asked)
		}
	}
	r.atIn(post, "HuangJunXiang", "1")
	if got := other.resolved(); got != protocol.ToolConfirmationOutcomeProceedOnce {
		t.Errorf("the Daemon got %q", got)
	}
	if note := r.robotReply(post, 2); !strings.Contains(note, "HuangJunXiang") || !strings.Contains(note, "Yes, allow") {
		t.Errorf("the thread does not show who answered what:\n%s", note)
	}
	if n := r.count("daemon.add_user_message"); n != 1 {
		t.Errorf("the answer also reached Droid as a message (%d messages)", n)
	}
	for _, f := range append(r.w.Frames("aibot_respond_msg"), r.w.Frames("aibot_send_msg")...) {
		t.Errorf("something went over the long connection for a Channel: %s", f.Body)
	}
}

func TestOnlyAnAllowedUserAnswersAPrompt(t *testing.T) {
	r := startTurn(t, permissionTurn, nil, nil)
	other := r.window()
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Push it"})
	c := r.sentCard(1)
	click := r.w.Click(fakewecom.Click{From: "wo-stranger", TaskID: c.TaskID, Key: c.buttonFor(t, "Yes, allow")})
	post := r.channel("Push the release")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	r.robotReply(post, 1)
	r.atIn(post, "wo-stranger", "1")
	r.barrier(3)
	time.Sleep(300 * time.Millisecond)

	if n := len(other.ctl.PendingPermissions("")); n != 2 {
		t.Errorf("%d of 2 permission requests still wait", n)
	}
	if n := other.resolutions(); n != 0 {
		t.Errorf("%d permission requests answered", n)
	}
	for _, f := range r.w.Frames("aibot_respond_update_msg") {
		if f.ReqID == click {
			t.Errorf("the card changed for a stranger's click: %s", f.Body)
		}
	}
	if n := r.robotReplies(post); n != 1 {
		t.Errorf("the robot posted %d replies", n)
	}
	if got := host.OpenSettings(r.file, nil).Get().ChatBridge.RefusedUsers; len(got) != 1 || got[0] != "wo-stranger" {
		t.Errorf("refused %v", got)
	}
}

func TestTextThatIsNotANumberReachesDroidAndThePromptWaits(t *testing.T) {
	r := startTurn(t, permissionTurn, nil, nil)
	other := r.window()
	post := r.channel("Push the release")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	r.robotReply(post, 1)
	r.atIn(post, "HuangJunXiang", "1 is fine, but check the tag first")
	if m := r.message(2); !strings.Contains(m.Text, "1 is fine, but check the tag first") {
		t.Errorf("message %q", m.Text)
	}
	r.atIn(post, "HuangJunXiang", "4")
	if m := r.message(3); !strings.Contains(m.Text, "@you 4") {
		t.Errorf("a number with no option reached Droid as %q", m.Text)
	}

	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Push it"})
	r.sentCard(1)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "1"})
	if m := r.message(5); !strings.Contains(m.Text, "1") {
		t.Errorf("message %q", m.Text)
	}
	if n := len(other.ctl.PendingPermissions("")); n != 2 {
		t.Errorf("%d of 2 permission requests still wait", n)
	}
	if n := other.resolutions(); n != 0 {
		t.Errorf("%d permission requests answered", n)
	}
}

var askTurn = fakedaemon.Turn{Kind: "askUser", Question: "Which branch?", Options: []string{"main", "release"}}

func TestAnAskUserIsNumberedTextANumberPicksAnOption(t *testing.T) {
	r := startTurn(t, askTurn, nil, nil)
	other := r.window()
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Deploy"})

	asked := r.directText(1)
	for _, want := range []string{"Which branch?", "1. main", "2. release"} {
		if !strings.Contains(asked, want) {
			t.Errorf("the question does not show %q:\n%s", want, asked)
		}
	}
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "2"})
	other.answered("release")
	if n := r.count("daemon.add_user_message"); n != 1 {
		t.Errorf("the answer also reached Droid as a message (%d messages)", n)
	}
}

func TestAnAskUserTakesOtherTextAsTheAnswer(t *testing.T) {
	r := startTurn(t, askTurn, nil, nil)
	other := r.window()
	post := r.channel("Deploy")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	if asked := r.robotReply(post, 1); !strings.Contains(asked, "Which branch?") || !strings.Contains(asked, "2. release") {
		t.Errorf("the question:\n%s", asked)
	}
	r.atIn(post, "HuangJunXiang", "the hotfix branch")
	other.answered("the hotfix branch")
	if n := r.count("daemon.add_user_message"); n != 1 {
		t.Errorf("the answer also reached Droid as a message (%d messages)", n)
	}
}

func TestAPermissionAnsweredInAClientFirstIsSettledInTheDirectMessage(t *testing.T) {
	r := startTurn(t, permissionTurn, nil, nil)
	other := r.window()
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Push it"})
	c := r.sentCard(1)
	if err := other.ctl.RespondToPermission(context.Background(), other.permission().RequestID,
		controller.PermissionAnswer{SelectedOption: protocol.ToolConfirmationOutcomeProceedAlways}); err != nil {
		t.Fatal(err)
	}
	if note := r.directText(1); !strings.Contains(note, "Droi") || !strings.Contains(note, "Yes, and always allow low impact commands") {
		t.Errorf("the direct message does not say where it was answered:\n%s", note)
	}

	click := r.w.Click(fakewecom.Click{From: "HuangJunXiang", TaskID: c.TaskID, Key: c.buttonFor(t, "No, cancel")})
	updated, ok := r.updatedCard(click)
	if !ok || !strings.Contains(updated.text, "Droi") || !strings.Contains(updated.text, "Yes, and always allow low impact commands") {
		t.Errorf("the clicked card (taken %v) does not say how it was answered:\n%s", ok, updated.text)
	}
	time.Sleep(300 * time.Millisecond)
	if n, got := other.resolutions(), other.resolved(); n != 1 || got != protocol.ToolConfirmationOutcomeProceedAlways {
		t.Errorf("%d answers, the Daemon took %q", n, got)
	}
}

func TestAPermissionAnsweredInAClientFirstIsSettledInTheChannel(t *testing.T) {
	r := startTurn(t, permissionTurn, nil, nil)
	other := r.window()
	post := r.channel("Push the release")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	r.robotReply(post, 1)
	if err := other.ctl.RespondToPermission(context.Background(), other.permission().RequestID,
		controller.PermissionAnswer{SelectedOption: protocol.ToolConfirmationOutcomeCancel}); err != nil {
		t.Fatal(err)
	}
	note := r.robotReply(post, 2)
	if !strings.Contains(note, "Droi") || !strings.Contains(note, "No, cancel") || strings.Count(note, "\n") > 0 {
		t.Errorf("not a one-line note of where it was answered:\n%s", note)
	}

	r.atIn(post, "HuangJunXiang", "1")
	r.message(2)
	if n, got := other.resolutions(), other.resolved(); n != 1 || got != protocol.ToolConfirmationOutcomeCancel {
		t.Errorf("%d answers, the Daemon took %q", n, got)
	}
}

func TestAnAskUserAnsweredInAClientFirstIsSettledInWeCom(t *testing.T) {
	r := startTurn(t, askTurn, nil, nil)
	other := r.window()
	post := r.channel("Deploy")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	r.robotReply(post, 1)
	ask := other.waitFor("AskUser", func(e controller.Event) bool {
		_, ok := e.(controller.AskUserRequested)
		return ok
	}).(controller.AskUserRequested).AskUser
	if err := other.ctl.RespondToAskUser(context.Background(), ask.RequestID, protocol.AskUserResult{Answers: []protocol.AskUserCollectedAnswer{
		{Index: ask.Questions[0].Index, Question: ask.Questions[0].Question, Answer: "main"},
	}}); err != nil {
		t.Fatal(err)
	}
	if note := r.robotReply(post, 2); !strings.Contains(note, "Droi") || !strings.Contains(note, "Which branch?") {
		t.Errorf("the thread does not say where it was answered:\n%s", note)
	}
	r.atIn(post, "HuangJunXiang", "2")
	r.message(2)
}

func TestAnUnansweredPromptWaits(t *testing.T) {
	r := startTurn(t, permissionTurn, nil, nil)
	other := r.window()
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Push it"})
	r.sentCard(1)
	r.clock.Add(48 * time.Hour)
	time.Sleep(500 * time.Millisecond)

	if n := len(other.ctl.PendingPermissions("")); n != 1 || other.resolutions() != 0 {
		t.Errorf("%d waiting, %d answered", n, other.resolutions())
	}
	if n := len(r.w.Frames("aibot_respond_update_msg")); n != 0 {
		t.Errorf("%d card updates", n)
	}
}

func TestNewStartsOverWhileAnAskUserWaits(t *testing.T) {
	r := startTurn(t, askTurn, nil, nil)
	post := r.channel("Deploy")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	first := r.initialized(1)
	r.robotReply(post, 1)
	r.atIn(post, "HuangJunXiang", "!new deploy the hotfix")
	if second := r.initialized(2); second.SessionID == first.SessionID {
		t.Error("!new answered the question instead of starting over")
	}
}
