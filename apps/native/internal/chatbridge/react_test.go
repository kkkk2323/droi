package chatbridge_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/chatbridge/fakewecom"
)

func TestReactPutsAndRemovesAnEmojiOnThePostAndOnAReply(t *testing.T) {
	r := start(t, nil)
	post := r.newPost("Deploy the fix")
	reply := r.w.AddReply(topic, post, fakewecom.Post{From: "HuangJunXiang", Text: "Tests are green"})
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))

	if text, isError := droid.call("react", map[string]any{"emoji": 69}); isError {
		t.Fatalf("react on the post: %s", text)
	}
	if text, isError := droid.call("react", map[string]any{"emoji": 3, "reply_id": reply}); isError {
		t.Fatalf("react on the reply: %s", text)
	}
	if got := r.w.Emoji(topic, post, ""); !slices.Equal(got, []int{69}) {
		t.Errorf("emoji on the post %v", got)
	}
	if got := r.w.Emoji(topic, post, reply); !slices.Equal(got, []int{3}) {
		t.Errorf("emoji on the reply %v", got)
	}

	if text, isError := droid.call("react", map[string]any{"emoji": 69, "remove": true}); isError {
		t.Fatalf("remove from the post: %s", text)
	}
	if text, isError := droid.call("react", map[string]any{"emoji": 3, "reply_id": reply, "remove": true}); isError {
		t.Fatalf("remove from the reply: %s", text)
	}
	if a, b := r.w.Emoji(topic, post, ""), r.w.Emoji(topic, post, reply); len(a) != 0 || len(b) != 0 {
		t.Errorf("emoji left: %v on the post, %v on the reply", a, b)
	}

	for _, n := range []int{0, 116} {
		if text, isError := droid.call("react", map[string]any{"emoji": n}); !isError || !strings.Contains(text, "115") {
			t.Errorf("emoji %d answered %q (error %v)", n, text, isError)
		}
	}
	r.w.FailAPI("/topic/post_emoji", 45009, "api freq out of limit")
	if text, isError := droid.call("react", map[string]any{"emoji": 69}); !isError || !strings.Contains(text, "45009") {
		t.Errorf("a rate limit answered %q (error %v)", text, isError)
	}
	if n := len(r.w.APICalls("/topic/post_emoji")); n != 5 {
		t.Errorf("%d post_emoji calls; four went through and one was refused", n)
	}
}

func TestReactInADirectMessageIsAToolError(t *testing.T) {
	r := start(t, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Hi"})
	text, isError := r.droid(r.initialized(1)).call("react", map[string]any{"emoji": 69})
	if !isError || !strings.Contains(text, "Channel") || !strings.Contains(text, "direct message") {
		t.Errorf("react answered %q (error %v)", text, isError)
	}
	if n := len(r.w.APICalls("/topic/post_emoji")) + len(r.w.Frames("aibot_respond_msg")); n != 0 {
		t.Errorf("%d calls to WeCom", n)
	}
}
