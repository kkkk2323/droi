package chatbridge_test

import (
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/chatbridge/fakewecom"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

func TestReplySaysWhenThePostHasUnreadReplies(t *testing.T) {
	r := start(t, func(s *host.Settings) { s.ChatBridge.AllowedUsers = []string{"HuangJunXiang", "LiLei"} })
	post := r.channel("The nightly job failed")
	at := r.w.AddReply(topic, post, fakewecom.Post{
		From: "HuangJunXiang", Text: "${inline_mention:b} can you look?", Mentions: []fakewecom.Mention{atRobot("b")},
	})
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, ReplyID: at, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))
	r.message(1)

	// The reply that @-ed Droid came in its message, and its own replies are
	// its own.
	for range 2 {
		text, isError := droid.call("reply", map[string]any{"text": "Looking"})
		if isError || strings.Contains(text, "not read") {
			t.Errorf("reply answered %q (error %v)", text, isError)
		}
	}

	r.w.AddReply(topic, post, fakewecom.Post{From: "LiLei", Text: "It was the disk"})
	r.w.AddReply(topic, post, fakewecom.Post{From: "LiLei", Text: "I cleaned it"})
	text, isError := droid.call("reply", map[string]any{"text": "Found it"})
	if isError || !strings.Contains(text, "2 replies") || !strings.Contains(text, "not read") || !strings.Contains(text, "read_thread") {
		t.Errorf("reply after two replies answered %q (error %v)", text, isError)
	}
	if replies := r.w.Replies(topic, post); replies[len(replies)-1].Text != "Found it" {
		t.Errorf("the reply was held back: %+v", replies[len(replies)-1])
	}

	if text, isError := droid.call("read_thread", nil); isError || !strings.Contains(text, "I cleaned it") {
		t.Fatalf("read_thread answered %q (error %v)", text, isError)
	}
	if text, isError := droid.call("reply", map[string]any{"text": "Thanks"}); isError || strings.Contains(text, "not read") {
		t.Errorf("reply after read_thread answered %q (error %v)", text, isError)
	}

	r.w.AddReply(topic, post, fakewecom.Post{From: "LiLei", Text: "One more thing"})
	if text, _ := droid.call("reply", map[string]any{"text": "Yes?"}); !strings.Contains(text, "1 reply ") {
		t.Errorf("reply after one reply answered %q", text)
	}
}

func TestAnUnreadCheckWeComRefusesStillSendsTheReply(t *testing.T) {
	r := start(t, nil)
	post := r.channel("Status?")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))
	r.message(1)
	r.w.FailAPI("/topic/get_reply_list", 45009, "api freq out of limit")

	if text, isError := droid.call("reply", map[string]any{"text": "Green"}); isError {
		t.Errorf("reply answered %q", text)
	}
	if n := len(r.w.Replies(topic, post)); n != 1 {
		t.Errorf("%d replies", n)
	}
}
