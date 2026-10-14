package chatbridge_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/chatbridge/fakewecom"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

// Ids as WeCom gives them: opaque, base64-like, with / and +.
const (
	topic = "CiA8lewtac2Oci/6PxxRcz7alQLn4uO3tj4mwmvBwUf56xAB"
	other = "CiAotherChannel+rcz7alQLn4uO3tj4mwmvBwUf56xAB"
)

// atRobot is the robot's @ as WeCom writes it in a post.
func atRobot(ph string) fakewecom.Mention {
	return fakewecom.Mention{Placeholder: ph, BotID: robot.BotID}
}

// channel adds the Channel 定时任务 and a post in it by HuangJunXiang that
// @s the robot.
func (r *rig) channel(text string) string {
	r.t.Helper()
	r.w.AddChannel(topic, "定时任务")
	return r.w.AddPost(topic, fakewecom.Post{
		ID: "CiBRTTB2WklcpFULg7ryAHp6OQbp9mYPGg479/C2LXiaZxAB", From: "HuangJunXiang",
		Text: "${inline_mention:mention_ph_001}\u200b" + text + "\u200b", Mentions: []fakewecom.Mention{atRobot("mention_ph_001")},
	})
}

func TestAnAtInAChannelPostStartsAChatSession(t *testing.T) {
	r := start(t, nil)
	post := r.channel("你好, what failed last night?")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})

	p := r.initialized(1)
	if p.Cwd != workspace || p.AutonomyLevel != "low" {
		t.Errorf("cwd %q, autonomy %q", p.Cwd, p.AutonomyLevel)
	}
	if len(p.Tags) != 1 || p.Tags[0].Name != "droi.chat" {
		t.Fatalf("tags %+v", p.Tags)
	}
	meta := p.Tags[0].Metadata
	if meta["kind"] != "channel" || meta["topicId"] != topic || meta["postId"] != post || meta["channel"] != "定时任务" {
		t.Errorf("tag metadata %v", meta)
	}
	if sp := p.SystemPrompt.Append; !strings.Contains(sp, "定时任务") || !strings.Contains(sp, "reply") || strings.Contains(sp, "direct message") {
		t.Errorf("system prompt %q", sp)
	}
	r.droid(p)

	m := r.message(1)
	if m.SessionID != p.SessionID {
		t.Errorf("message to %s, the Chat Session is %s", m.SessionID, p.SessionID)
	}
	for _, want := range []string{"HuangJunXiang", "定时任务", topic, post, "@you", "你好, what failed last night?", "read_thread"} {
		if !strings.Contains(m.Text, want) {
			t.Errorf("trigger message without %q:\n%s", want, m.Text)
		}
	}
	if strings.Contains(m.Text, "${") || strings.Contains(m.Text, "\u200b") {
		t.Errorf("placeholders left in the trigger:\n%q", m.Text)
	}
}

// barrier sends a direct message and waits for it to reach the Daemon as
// the nth message: the Chat Bridge handles WeCom's events in order, so
// every event before it has been handled.
func (r *rig) barrier(nth int) {
	r.t.Helper()
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "barrier"})
	if m := r.message(nth); !strings.Contains(m.Text, "barrier") {
		r.t.Fatalf("message %d is %q, not the barrier", nth, m.Text)
	}
}

func TestALaterAtInTheSamePostContinuesItsChatSession(t *testing.T) {
	r := start(t, func(s *host.Settings) { s.ChatBridge.AllowedUsers = []string{"HuangJunXiang", "LiLei"} })
	post := r.channel("Look at the nightly job")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	p := r.initialized(1)
	r.message(1)

	reply := r.w.AddReply(topic, post, fakewecom.Post{
		From:     "LiLei",
		Text:     "${inline_mention:ph_bot}\u200b see ${inline_file:ph_f} from ${inline_mention:ph_u}\u200b, and tell ${inline_mention_all}",
		Mentions: []fakewecom.Mention{atRobot("ph_bot"), {Placeholder: "ph_u", UserID: "HuangJunXiang"}},
		Files:    []fakewecom.File{{Placeholder: "ph_f", Name: "nightly.log"}},
	})
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, ReplyID: reply, From: "LiLei"})

	m := r.message(2)
	if m.SessionID != p.SessionID {
		t.Errorf("the second @ went to %s, the post's Chat Session is %s", m.SessionID, p.SessionID)
	}
	for _, want := range []string{"LiLei", reply, post, "@you see [file: nightly.log] from @HuangJunXiang, and tell @all"} {
		if !strings.Contains(m.Text, want) {
			t.Errorf("trigger message without %q:\n%s", want, m.Text)
		}
	}
	if strings.Contains(m.Text, "read_thread") {
		t.Errorf("a later trigger has the read_thread hint:\n%s", m.Text)
	}
	if n := r.count("daemon.initialize_session"); n != 1 {
		t.Errorf("%d Chat Sessions for one post", n)
	}
}

func TestTwoPostsInAChannelAreTwoChatThreads(t *testing.T) {
	r := start(t, nil)
	first := r.channel("First subject")
	second := r.w.AddPost(topic, fakewecom.Post{From: "HuangJunXiang", Text: "${inline_mention:b} Second subject", Mentions: []fakewecom.Mention{atRobot("b")}})
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: first, From: "HuangJunXiang"})
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: second, From: "HuangJunXiang"})

	a, b := r.initialized(1), r.initialized(2)
	if a.SessionID == b.SessionID || a.Tags[0].Metadata["postId"] != first || b.Tags[0].Metadata["postId"] != second {
		t.Errorf("Sessions %s %v and %s %v", a.SessionID, a.Tags, b.SessionID, b.Tags)
	}
	if m := r.message(2); m.SessionID != b.SessionID || !strings.Contains(m.Text, "Second subject") || !strings.Contains(m.Text, "read_thread") {
		t.Errorf("second post's message %+v", m)
	}
	// Droid in the first post replies under the first post only.
	if text, isError := r.droid(a).call("reply", map[string]any{"text": "About the first"}); isError {
		t.Fatal(text)
	}
	if n, m := len(r.w.Replies(topic, first)), len(r.w.Replies(topic, second)); n != 1 || m != 0 {
		t.Errorf("%d replies under the first post, %d under the second", n, m)
	}
}

func TestTheGroupChatCopyOfAnAtIsIgnored(t *testing.T) {
	r := start(t, nil)
	post := r.channel("你好")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	r.w.GroupCopy("HuangJunXiang", "@小蓝 你好")
	r.message(1)
	r.barrier(2)

	if n := r.count("daemon.add_user_message"); n != 2 {
		t.Errorf("%d messages for one @ and the barrier", n)
	}
	if n := r.count("daemon.initialize_session"); n != 2 {
		t.Errorf("%d Chat Sessions; the post's and the barrier's are 2", n)
	}
	if n := len(r.w.Frames("aibot_respond_msg")) + len(r.w.Frames("aibot_send_msg")); n != 0 {
		t.Errorf("%d answers on the group chat copy", n)
	}
}

func TestChannelEventsWithoutAnAtStartNothing(t *testing.T) {
	r := start(t, nil)
	post := r.w.AddPost(topic, fakewecom.Post{From: "HuangJunXiang", Text: "Just talking"})
	r.w.AddChannel(topic, "定时任务")
	for _, ev := range []map[string]any{
		{"eventtype": "topic_event", "topic_event": map[string]any{"type": 1, "post_id": post}},
		{"eventtype": "topic_reply_event", "topic_reply_event": map[string]any{"type": 1, "post_id": post, "reply_id": "r1"}},
		{"eventtype": "topic_member_change"},
		{"eventtype": "topic_info_change", "topic_info_change": map[string]any{"info_type": 2}},
	} {
		r.w.Push("aibot_event_callback", "", map[string]any{
			"msgid": ev["eventtype"], "aibotid": robot.BotID, "from": map[string]string{"userid": "HuangJunXiang"},
			"msgtype": "event", "create_time": 1791963707, "event": ev, "topic_id": topic,
		})
	}
	// An @ by someone not on the allowlist starts nothing either.
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "wo-encrypted-9c1d"})
	r.barrier(1)

	if n := r.count("daemon.initialize_session"); n != 1 {
		t.Errorf("%d Chat Sessions; only the barrier's should start", n)
	}
	if got := host.OpenSettings(r.file, nil).Get().ChatBridge.RefusedUsers; len(got) != 1 || got[0] != "wo-encrypted-9c1d" {
		t.Errorf("refused %v", got)
	}
}

func TestARepeatedTopicAtBotStartsNothingTwice(t *testing.T) {
	r := start(t, nil)
	post := r.channel("Once")
	r.w.AtBot(fakewecom.AtBot{MsgID: "e-1", TopicID: topic, PostID: post, From: "HuangJunXiang"})
	r.w.AtBot(fakewecom.AtBot{MsgID: "e-1", TopicID: topic, PostID: post, From: "HuangJunXiang"})
	r.message(1)
	r.barrier(2)
	if n := r.count("daemon.add_user_message"); n != 2 {
		t.Errorf("%d messages", n)
	}
}

func TestAChannelTriggerIsCutAt2000Characters(t *testing.T) {
	r := start(t, nil)
	post := r.channel(strings.Repeat("字", 2500))
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})

	m := r.message(1)
	// "@you" and 1996 字 are the first 2000 characters.
	if !strings.Contains(m.Text, "@you"+strings.Repeat("字", 1996)) || strings.Contains(m.Text, strings.Repeat("字", 1997)) {
		t.Errorf("the text is not cut at 2000 characters")
	}
	if !strings.Contains(m.Text, "[cut: the message is 2504 characters") || !strings.Contains(m.Text, "read_thread") {
		t.Errorf("no cut marker or hint:\n%s", m.Text[len(m.Text)-300:])
	}
}

func TestReplyInAChannelPostsUnderThePost(t *testing.T) {
	r := start(t, nil)
	post := r.channel("Status?")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))

	text, isError := droid.call("reply", map[string]any{"text": "**Green** since 03:00"})
	if isError || !strings.Contains(text, "定时任务") {
		t.Fatalf("reply answered %q (error %v)", text, isError)
	}
	replies := r.w.Replies(topic, post)
	if len(replies) != 1 || replies[0].Text != "**Green** since 03:00" || replies[0].Robot != robot.BotID || replies[0].ParentReplyID != "" {
		t.Errorf("replies under the post: %+v", replies)
	}
	if n := len(r.w.Frames("aibot_respond_msg")) + len(r.w.Frames("aibot_send_msg")); n != 0 {
		t.Errorf("%d messages on the long connection", n)
	}

	r.w.FailAPI("/topic/send_reply", 45009, "api freq out of limit")
	if text, isError := droid.call("reply", map[string]any{"text": "Too fast"}); !isError || !strings.Contains(text, "45009") {
		t.Errorf("a rate limit answered %q (error %v)", text, isError)
	}
	if n := len(r.w.Replies(topic, post)); n != 1 {
		t.Errorf("%d replies after a refused one", n)
	}
}

func TestReadThreadShowsThePostAndItsRepliesPageByPage(t *testing.T) {
	r := start(t, func(s *host.Settings) { s.ChatBridge.AllowedUsers = []string{"HuangJunXiang", "LiLei"} })
	r.w.AddChannel(topic, "定时任务")
	at := time.Date(2026, 10, 14, 7, 40, 51, 0, time.UTC)
	post := r.w.AddPost(topic, fakewecom.Post{From: "HuangJunXiang", Time: at, Text: "The nightly job failed"})
	for i := range 25 {
		r.w.AddReply(topic, post, fakewecom.Post{ID: "reply-" + string(rune('A'+i)), From: "LiLei", Time: at.Add(time.Duration(i+1) * time.Minute), Text: "note " + string(rune('A'+i))})
	}
	last := r.w.AddReply(topic, post, fakewecom.Post{
		From: "HuangJunXiang", Time: at.Add(time.Hour), Text: "${inline_mention:b} what do you think?", Mentions: []fakewecom.Mention{atRobot("b")},
	})
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, ReplyID: last, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))

	page1, isError := droid.call("read_thread", nil)
	if isError {
		t.Fatal(page1)
	}
	for _, want := range []string{"The nightly job failed", "HuangJunXiang at 2026-10-14T07:40:51Z", "reply-A by LiLei at 2026-10-14T07:41:51Z", "note A", "note T"} {
		if !strings.Contains(page1, want) {
			t.Errorf("page 1 without %q:\n%s", want, page1)
		}
	}
	if strings.Contains(page1, "note U") {
		t.Errorf("page 1 has more than 20 replies")
	}
	cursor := between(page1, `cursor "`, `"`)
	if cursor == "" {
		t.Fatalf("page 1 gives no cursor:\n%s", page1)
	}

	page2, isError := droid.call("read_thread", map[string]any{"cursor": cursor})
	if isError {
		t.Fatal(page2)
	}
	for _, want := range []string{"note U", "note Y", "@you what do you think?", "HuangJunXiang at 2026-10-14T08:40:51Z", "No more replies"} {
		if !strings.Contains(page2, want) {
			t.Errorf("page 2 without %q:\n%s", want, page2)
		}
	}
	if strings.Contains(page2, "note T") || strings.Contains(page2, "The nightly job failed") {
		t.Errorf("page 2 repeats page 1:\n%s", page2)
	}

	// Droid's own reply shows as its own.
	droid.call("reply", map[string]any{"text": "Looking"})
	page3, _ := droid.call("read_thread", map[string]any{"cursor": "20", "limit": 50})
	if !strings.Contains(page3, "by you (the robot)") || !strings.Contains(page3, "Looking") {
		t.Errorf("Droid's reply is not marked as its own:\n%s", page3)
	}
}

func between(s, from, to string) string {
	_, after, ok := strings.Cut(s, from)
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(after, to)
	return v
}

func TestReadThreadInADirectMessageSaysThereIsNoHistory(t *testing.T) {
	r := start(t, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Hi"})
	text, isError := r.droid(r.initialized(1)).call("read_thread", nil)
	if isError || !strings.Contains(text, "direct message") || !strings.Contains(text, "no history") {
		t.Errorf("read_thread answered %q (error %v)", text, isError)
	}
	if n := len(r.w.APICalls("/topic/get_reply_list")); n != 0 {
		t.Errorf("%d Channel API calls for a direct message", n)
	}
}

func TestTheTokenIsFetchedOnceAndReusedUntilItExpires(t *testing.T) {
	r := start(t, nil)
	post := r.channel("Status?")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))
	r.message(1)
	for _, tool := range []string{"read_thread", "reply", "read_thread"} {
		if text, isError := droid.call(tool, map[string]any{"text": "x"}); isError && tool == "reply" {
			t.Fatal(text)
		}
	}
	if n := len(r.w.APICalls("/get_token")); n != 1 {
		t.Errorf("%d get_token calls for one day", n)
	}
	var sent struct {
		BotID string `json:"bot_id"`
		Time  int64  `json:"time"`
	}
	_ = json.Unmarshal(r.w.APICalls("/get_token")[0].Body, &sent)
	if sent.BotID != robot.BotID || sent.Time != r.clock.Now().Unix() {
		t.Errorf("get_token for %q at %d", sent.BotID, sent.Time)
	}

	r.clock.Add(23 * time.Hour)
	if text, isError := droid.call("read_thread", nil); isError {
		t.Fatal(text)
	}
	if n := len(r.w.APICalls("/get_token")); n != 1 {
		t.Errorf("%d get_token calls within the day", n)
	}
	r.clock.Add(2 * time.Hour)
	if text, isError := droid.call("read_thread", nil); isError {
		t.Fatal(text)
	}
	if n := len(r.w.APICalls("/get_token")); n != 2 {
		t.Errorf("%d get_token calls after the day", n)
	}
}

func TestATokenWeComNoLongerTakesIsFetchedAgain(t *testing.T) {
	r := start(t, nil)
	post := r.channel("Status?")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))
	r.message(1)
	r.w.ExpireTokens()

	if text, isError := droid.call("reply", map[string]any{"text": "Still here"}); isError {
		t.Fatalf("reply after the token expired: %s", text)
	}
	if n := len(r.w.APICalls("/get_token")); n != 2 {
		t.Errorf("%d get_token calls", n)
	}
	if n := len(r.w.Replies(topic, post)); n != 1 {
		t.Errorf("%d replies", n)
	}
}

func TestARefusedTokenIsAToolErrorThatNamesWeComsError(t *testing.T) {
	r := start(t, nil)
	post := r.channel("Status?")
	r.w.AtBot(fakewecom.AtBot{TopicID: topic, PostID: post, From: "HuangJunXiang"})
	droid := r.droid(r.initialized(1))
	r.message(1)
	r.w.ExpireTokens()
	r.w.FailAPI("/get_token", 45009, "api freq out of limit")

	text, isError := droid.call("reply", map[string]any{"text": "x"})
	if !isError || !strings.Contains(text, "45009") || strings.Contains(text, "tok-") || strings.Contains(text, robot.Secret) {
		t.Errorf("reply answered %q (error %v)", text, isError)
	}
}
