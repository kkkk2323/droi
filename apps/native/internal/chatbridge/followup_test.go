package chatbridge_test

import (
	"strings"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/internal/chatbridge/fakewecom"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

// Replies under a post that has a Chat Session reach it without an @ of
// the robot, as WeCom's topic_reply_event.

// startedPost starts a post's Chat Session with an @ and waits for its
// first turn to end, so the next message finds it idle.
func (r *rig) startedPost(text string) (string, initialized) {
	r.t.Helper()
	turns := r.watchTurns()
	post := r.newPost(text)
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	p := r.initialized(1)
	r.message(1)
	turns.wait(p.SessionID, 1)
	return post, p
}

func TestAReplyWithoutAnAtContinuesThePostsChatSession(t *testing.T) {
	r := start(t, func(s *host.Settings) { s.ChatBridge.AllowedUsers = []string{"HuangJunXiang", "LiLei"} })
	post, p := r.startedPost("你好小蓝")
	if sp := p.SystemPrompt.Append; !strings.Contains(sp, "without an @") || !strings.Contains(sp, "not meant for you") {
		t.Errorf("system prompt does not tell of replies without an @:\n%s", sp)
	}
	r.barrier(2)

	reply := r.w.AddReply(topic, post, fakewecom.Post{From: "LiLei", Text: "你能做什么？"})
	r.w.Replied(fakewecom.ReplyEvent{TopicID: topic, PostID: post, ReplyID: reply, From: "LiLei"})

	m := r.message(3)
	if m.SessionID != p.SessionID {
		t.Errorf("the reply went to %s, the post's Chat Session is %s", m.SessionID, p.SessionID)
	}
	for _, want := range []string{"LiLei", reply, post, "你能做什么？", "without an @"} {
		if !strings.Contains(m.Text, want) {
			t.Errorf("message without %q:\n%s", want, m.Text)
		}
	}
	if strings.Contains(m.Text, "@-ed you") {
		t.Errorf("the message says the reply @-ed the robot:\n%s", m.Text)
	}
	if n := r.count("daemon.initialize_session"); n != 2 {
		t.Errorf("%d Chat Sessions; the post's and the barrier's are 2", n)
	}
}

func TestRepliesWithoutAnAtThatAreNotForTheChatSessionSendNothing(t *testing.T) {
	r := start(t, nil)
	post, _ := r.startedPost("Look at the nightly job")
	// A post with no Chat Session.
	quiet := r.w.AddPost(topic, fakewecom.Post{From: "HuangJunXiang", Text: "Lunch?"})
	a := r.w.AddReply(topic, quiet, fakewecom.Post{From: "HuangJunXiang", Text: "Yes"})
	r.w.Replied(fakewecom.ReplyEvent{TopicID: topic, PostID: quiet, ReplyID: a, From: "HuangJunXiang"})
	// Someone not on the allowlist, and the robot's own reply.
	b := r.w.AddReply(topic, post, fakewecom.Post{From: "WangWu", Text: "I can look too"})
	r.w.Replied(fakewecom.ReplyEvent{TopicID: topic, PostID: post, ReplyID: b, From: "WangWu"})
	r.w.Replied(fakewecom.ReplyEvent{TopicID: topic, PostID: post, ReplyID: "robot-reply", From: robot.BotID})
	// An edit and a deletion.
	c := r.w.AddReply(topic, post, fakewecom.Post{From: "HuangJunXiang", Text: "Edited"})
	r.w.Replied(fakewecom.ReplyEvent{TopicID: topic, PostID: post, ReplyID: c, From: "HuangJunXiang", Type: 2})
	r.w.Replied(fakewecom.ReplyEvent{TopicID: topic, PostID: post, ReplyID: c, From: "HuangJunXiang", Type: 3})
	// The same event twice reaches the Chat Session once.
	d := r.w.AddReply(topic, post, fakewecom.Post{From: "HuangJunXiang", Text: "Once"})
	r.w.Replied(fakewecom.ReplyEvent{MsgID: "e-1", TopicID: topic, PostID: post, ReplyID: d, From: "HuangJunXiang"})
	r.w.Replied(fakewecom.ReplyEvent{MsgID: "e-1", TopicID: topic, PostID: post, ReplyID: d, From: "HuangJunXiang"})
	r.barrier(3)

	if m := r.message(2); !strings.Contains(m.Text, "Once") {
		t.Errorf("message 2 is %q, not the reply", m.Text)
	}
	if n := r.count("daemon.add_user_message"); n != 3 {
		t.Errorf("%d messages; the @, one reply and the barrier are 3", n)
	}
	if n := r.count("daemon.initialize_session"); n != 2 {
		t.Errorf("%d Chat Sessions; the post's and the barrier's are 2", n)
	}
	if got := host.OpenSettings(r.file, nil).Get().ChatBridge.RefusedUsers; len(got) != 0 {
		t.Errorf("replies without an @ recorded refused senders %v", got)
	}
}

func TestAReplyWithoutAnAtWhileItsChatSessionWorksSendsANotice(t *testing.T) {
	r := startOn(t, fakedaemon.Scenario{Turn: working}, nil, nil)
	post := r.newPost("Fix the nightly job")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	p := r.initialized(1)
	r.message(1)

	reply := r.w.AddReply(topic, post, fakewecom.Post{From: "HuangJunXiang", Text: "the disk on build-7 is full"})
	r.w.Replied(fakewecom.ReplyEvent{TopicID: topic, PostID: post, ReplyID: reply, From: "HuangJunXiang"})

	m := r.message(2)
	if m.SessionID != p.SessionID || m.QueuePlacement != "end_of_turn" {
		t.Errorf("notice %+v; the post's Chat Session is %s", m, p.SessionID)
	}
	if strings.Contains(m.Text, "disk") || !strings.Contains(m.Text, reply) || !strings.Contains(m.Text, "read_thread") || strings.Contains(m.Text, "@-ed you") {
		t.Errorf("notice:\n%s", m.Text)
	}
}

func TestNewInAReplyWithoutAnAtStartsAnotherChatSession(t *testing.T) {
	r := start(t, nil)
	post, first := r.startedPost("Look at the nightly job")
	reply := r.w.AddReply(topic, post, fakewecom.Post{From: "HuangJunXiang", Text: "!new Try the weekly job"})
	r.w.Replied(fakewecom.ReplyEvent{TopicID: topic, PostID: post, ReplyID: reply, From: "HuangJunXiang"})

	if id := r.archivedSession(1); id != first.SessionID {
		t.Errorf("archived %s, the post's Chat Session is %s", id, first.SessionID)
	}
	second := r.initialized(2)
	m := r.message(2)
	if m.SessionID != second.SessionID || !strings.Contains(m.Text, "Try the weekly job") || strings.Contains(m.Text, "!new") {
		t.Errorf("first message of the new Chat Session %+v", m)
	}
}

func TestAfterTheHostRestartsAReplyWithoutAnAtContinuesThePostsChatSession(t *testing.T) {
	r := startOn(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{postSession(postID)}}, nil, nil)
	id := r.d.Sessions[0].SessionID
	r.newPost("Earlier post")
	reply := r.w.AddReply(topic, postID, fakewecom.Post{From: "HuangJunXiang", Text: "Any news?"})
	r.w.Replied(fakewecom.ReplyEvent{TopicID: topic, PostID: postID, ReplyID: reply, From: "HuangJunXiang"})

	m := r.message(1)
	if m.SessionID != id || !strings.Contains(m.Text, "Any news?") {
		t.Errorf("message %+v; the post's Chat Session is %s", m, id)
	}
	if n := r.count("daemon.initialize_session"); n != 0 {
		t.Errorf("%d new Chat Sessions", n)
	}
}
