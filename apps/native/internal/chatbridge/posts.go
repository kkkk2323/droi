package chatbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Chat Threads in WeCom Channel posts: an @ of the robot in a post or one of
// its replies arrives as the topic_at_bot event, which carries ids but no
// text; the Chat Bridge reads the text, and Droid reads and replies, through
// the Channel's HTTPS API.

// event is the body of aibot_event_callback.
type event struct {
	MsgID string `json:"msgid"`
	From  struct {
		UserID string `json:"userid"`
	} `json:"from"`
	TopicID string `json:"topic_id"`
	Event   struct {
		Type       string `json:"eventtype"`
		TopicAtBot struct {
			PostID  string `json:"post_id"`
			ReplyID string `json:"reply_id"`
		} `json:"topic_at_bot"`
	} `json:"event"`
}

const fetchTimeout = 30 * time.Second

// handleEvent starts or continues a post's Chat Session on an @ of the
// robot there. Every other event starts nothing.
func (b *Bridge) handleEvent(f frame) {
	var e event
	if json.Unmarshal(f.Body, &e) != nil {
		return
	}
	at, user := e.Event.TopicAtBot, e.From.UserID
	if e.Event.Type != "topic_at_bot" || e.TopicID == "" || at.PostID == "" || user == "" {
		return
	}
	if !b.admit(e.MsgID, user) {
		return
	}
	b.mu.Lock()
	th := b.threads[postThread(e.TopicID, at.PostID).key]
	if th == nil {
		th = postThread(e.TopicID, at.PostID)
		b.threads[th.key] = th
	}
	b.mu.Unlock()
	b.run(th, func() { b.atBot(th, e) })
}

// atBot sends a post's Chat Session the @ of the robot there, with its text.
func (b *Bridge) atBot(th *thread, e event) {
	at, user := e.Event.TopicAtBot, e.From.UserID
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	// After a restart the map knows the post's Chat Session only once
	// rebuilt, which the hint depends on.
	if err := b.ready(ctx); err != nil {
		b.o.Log(err.Error())
		return
	}
	b.mu.Lock()
	first := th.sessionID == ""
	api, name := b.api, th.channel
	b.mu.Unlock()
	if api == nil {
		return
	}
	if name == "" {
		var err error
		if name, err = api.channelName(ctx, e.TopicID); err != nil {
			b.o.Log("cannot read the WeCom Channel's name: " + err.Error())
		} else {
			b.mu.Lock()
			th.channel = name
			b.mu.Unlock()
		}
	}
	var c content
	var err error
	if at.ReplyID != "" {
		c, err = api.getReply(ctx, e.TopicID, at.PostID, at.ReplyID)
	} else {
		c, err = api.getPost(ctx, e.TopicID, at.PostID)
	}
	text := ""
	if err != nil {
		text = "[WeCom did not give the text: " + err.Error() + "]"
	} else {
		text = cut(render(c, b.robotID()))
	}
	if rest, ok := cutNew(strings.TrimPrefix(strings.TrimSpace(text), "@you")); ok {
		if rest == "" {
			rest = "[started a new Chat Session with !new, with no task yet]"
		}
		b.markSent(th, at.ReplyID, err)
		b.startOver(th, channelTrigger(user, name, e.TopicID, at.PostID, at.ReplyID, rest, true))
		return
	}
	// Droid reads the text with read_thread when it gets to it.
	if b.working(th) {
		b.deliver(th, busyNotice(user, at.ReplyID))
		return
	}
	b.markSent(th, at.ReplyID, err)
	b.deliver(th, channelTrigger(user, name, e.TopicID, at.PostID, at.ReplyID, text, first))
}

// admit reports whether an event is new and from an allowed sender; it
// records a sender who is not.
func (b *Bridge) admit(msgID, user string) bool {
	b.mu.Lock()
	dup := !b.seen.add(msgID)
	b.mu.Unlock()
	if dup {
		return false
	}
	if !slices.Contains(b.o.Settings.Get().ChatBridge.AllowedUsers, user) {
		b.refuse(user)
		return false
	}
	return true
}

func (b *Bridge) robotID() string { return b.o.Settings.Get().ChatBridge.BotID }

// placeholder is WeCom's placeholder in a post's Markdown.
var placeholder = regexp.MustCompile(`\$\{(?:(inline_mention|inline_file):([A-Za-z0-9_-]{1,32})|inline_mention_all)\}`)

// render is a post or reply's text with its placeholders as @name and
// [file: name]. WeCom gives no member's name, so a member's is the user id;
// the robot's own @ is @you.
func render(c content, robot string) string {
	if c.IsRecalled {
		return "[recalled]"
	}
	mentions, files := map[string]member{}, map[string]string{}
	for _, m := range c.Mentions {
		mentions[m.Placeholder] = m.Member
	}
	for _, f := range c.Files {
		files[f.Placeholder] = f.FileName
	}
	text := placeholder.ReplaceAllStringFunc(c.Content, func(s string) string {
		m := placeholder.FindStringSubmatch(s)
		switch m[1] {
		case "inline_mention":
			if who, ok := mentions[m[2]]; ok {
				return "@" + mentionName(who, robot)
			}
		case "inline_file":
			if name, ok := files[m[2]]; ok {
				return "[file: " + name + "]"
			}
		default:
			return "@all"
		}
		return s
	})
	// WeCom puts zero-width spaces around a mention.
	return strings.TrimSpace(strings.ReplaceAll(text, "\u200b", ""))
}

func mentionName(m member, robot string) string {
	switch {
	case m.Type == 1 && m.BotID == robot:
		return "you"
	case m.Type == 1:
		return "robot " + m.BotID
	}
	return m.UserID
}

// authorName is who wrote a post or reply, as read_thread shows it.
func authorName(m member, robot string) string {
	if m.Type == 1 && m.BotID == robot {
		return "you (the robot)"
	}
	return mentionName(m, robot)
}

func stamp(unix int64) string { return time.Unix(unix, 0).UTC().Format(time.RFC3339) }

const (
	defaultReplyPage = 20
	maxReplyPage     = 50
)

// readThread is a Channel post and a page of its replies, the post on the
// first page only. It marks the replies shown as read.
func (b *Bridge) readThread(ctx context.Context, th *thread, cursor string, limit int) (string, error) {
	if th.kind != threadKindChannel {
		return "This Chat Thread is a direct message with WeCom user " + th.userID + ": WeCom keeps no history the Chat Bridge can read. " +
			"Every new message from them arrives in this Session as a message.", nil
	}
	if limit <= 0 {
		limit = defaultReplyPage
	}
	limit = min(limit, maxReplyPage)
	b.mu.Lock()
	api, name := b.api, th.channel
	b.mu.Unlock()
	if api == nil {
		return "", errNotConnected
	}
	robot := b.robotID()
	var out strings.Builder
	if cursor == "" {
		p, err := api.getPost(ctx, th.topicID, th.postID)
		if err != nil {
			return "", fmt.Errorf("cannot read the post: %w", err)
		}
		fmt.Fprintf(&out, "Post %s in the WeCom Channel %s, by %s at %s%s:\n%s\n\n",
			th.postID, channelLabel(name, th.topicID), authorName(p.author(), robot), stamp(p.CreateTime), edited(p), render(p, robot))
	}
	replies, next, err := api.replyPage(ctx, th.topicID, th.postID, cursor, limit)
	if err != nil {
		return "", fmt.Errorf("cannot read the replies: %w", err)
	}
	if len(replies) == 0 {
		out.WriteString("No replies.\n")
	} else {
		out.WriteString("Replies, oldest first:\n\n")
	}
	for _, r := range replies {
		answering := ""
		if r.ParentReplyID != "" {
			answering = ", answering reply " + r.ParentReplyID
		}
		fmt.Fprintf(&out, "Reply %s by %s at %s%s%s:\n%s\n\n",
			r.ReplyID, authorName(r.author(), robot), stamp(r.CreateTime), edited(r), answering, render(r, robot))
	}
	if next != "" {
		fmt.Fprintf(&out, "More replies: call read_thread with cursor %q.", next)
	} else if len(replies) > 0 {
		out.WriteString("No more replies.")
	}
	for _, r := range replies {
		b.markRead(th, r.ReplyID)
	}
	return strings.TrimSpace(out.String()), nil
}

// markRead records replies Droid has seen, in read_thread or in the message
// an @ sent it.
func (b *Bridge) markRead(th *thread, replyIDs ...string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if th.read == nil {
		th.read = map[string]bool{}
	}
	for _, id := range replyIDs {
		th.read[id] = true
	}
}

// markSent marks the reply an @ was in as read when its message carries
// the reply's text, which it does unless WeCom did not give it (err).
func (b *Bridge) markSent(th *thread, replyID string, err error) {
	if replyID != "" && err == nil {
		b.markRead(th, replyID)
	}
}

// unread counts the replies under the post, not the robot's, that Droid
// has not seen, among the newest page of them; more says the page was all
// unread and older ones were left out.
func (b *Bridge) unread(ctx context.Context, th *thread, api *channelAPI) (n int, more bool, err error) {
	replies, older, err := api.latestReplies(ctx, th.topicID, th.postID, maxReplyPage)
	if err != nil {
		return 0, false, err
	}
	robot := b.robotID()
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range replies {
		if a := r.author(); !th.read[r.ReplyID] && (a.Type != 1 || a.BotID != robot) {
			n++
		}
	}
	return n, older && n == len(replies), nil
}

func edited(c content) string {
	if c.IsModified {
		return " (edited)"
	}
	return ""
}

const maxEmoji = 115

// react puts one of WeCom's emoji on the post or one of its replies, or
// removes it.
func (b *Bridge) react(ctx context.Context, th *thread, emoji int, replyID string, remove bool) (string, error) {
	if th.kind != threadKindChannel {
		return "", errors.New("react works only in a WeCom Channel; this Chat Thread is a direct message")
	}
	if emoji < 1 || emoji > maxEmoji {
		return "", fmt.Errorf("emoji is %d; WeCom's emoji are 1 to %d", emoji, maxEmoji)
	}
	b.mu.Lock()
	api := b.api
	b.mu.Unlock()
	if api == nil {
		return "", errNotConnected
	}
	if err := api.postEmoji(ctx, th.topicID, th.postID, replyID, emoji, !remove); err != nil {
		return "", fmt.Errorf("not done: %w", err)
	}
	on := "the post"
	if replyID != "" {
		on = "reply " + replyID
	}
	if remove {
		return fmt.Sprintf("Removed emoji %d from %s.", emoji, on), nil
	}
	return fmt.Sprintf("Put emoji %d on %s.", emoji, on), nil
}

// replyInChannel posts Markdown as a reply under the Chat Thread's post.
func (b *Bridge) replyInChannel(ctx context.Context, th *thread, text string) (string, error) {
	b.mu.Lock()
	api, name := b.api, th.channel
	b.mu.Unlock()
	if api == nil {
		return "", errNotConnected
	}
	id, err := api.sendReply(ctx, th.topicID, th.postID, text)
	if err != nil {
		return "", fmt.Errorf("not sent: %w", err)
	}
	out := fmt.Sprintf("Replied under the post in the WeCom Channel %s (reply %s).", channelLabel(name, th.topicID), id)
	// The reply is sent already; a failed count only leaves the hint out.
	n, more, err := b.unread(ctx, th, api)
	if err != nil {
		b.o.Log("cannot count the unread replies: " + err.Error())
		return out, nil
	}
	switch {
	case more:
		out += fmt.Sprintf(" The post has %d or more replies you have not read; read_thread shows them.", n)
	case n == 1:
		out += " The post has 1 reply you have not read; read_thread shows it."
	case n > 1:
		out += fmt.Sprintf(" The post has %d replies you have not read; read_thread shows them.", n)
	}
	return out, nil
}
