package chatbridge

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxTriggerRunes is how much of a message a trigger carries.
const maxTriggerRunes = 2000

// systemPromptAppend is the Host's System Prompt Addition, then the facts
// of the Chat Thread: which one it is, and that only reply reaches it.
func systemPromptAppend(addition string, th *chatThread) string {
	facts := fmt.Sprintf("You are working in a WeCom (企业微信) Chat Thread: a direct message with the WeCom user %s. "+
		"Nothing you write reaches them unless you call the droi-chat tool reply. "+
		"The droi-chat tools act on this Chat Thread only, and their messages arrive in this Session.", th.userID)
	if th.kind == kindChannelPost {
		facts = fmt.Sprintf("You are working in a WeCom (企业微信) Chat Thread: the post %s in the WeCom Channel %s, with its replies. "+
			"Each time someone @s you there, the @ arrives in this Session as a message, and so does each new reply under the post once this Chat Session has started, even one without an @ of you. "+
			"A reply without an @ may be for someone else: when a message is not meant for you, do not reply to it. "+
			"Nothing you write reaches anyone unless you call the droi-chat tool reply, which posts a reply under the post; "+
			"read_thread shows the post and its replies. The droi-chat tools act on this Chat Thread only.",
			th.postID, channelLabel(th.channelName, th.topicID))
	}
	if addition == "" {
		return facts
	}
	return addition + "\n\n" + facts
}

// archivedNote opens the first message of a Chat Session whose Chat
// Thread's Chat Session before was archived.
const archivedNote = "The Chat Thread's previous Chat Session was archived; this is a new Chat Session, without that conversation.\n\n"

// directTrigger is the message a direct message sends its Chat Session.
func directTrigger(user, text string) string {
	return fmt.Sprintf("WeCom user %s wrote in a direct message:\n\n%s", user, cut(text))
}

// channelTrigger is the message an @ in a Channel post, or a reply there
// without one, sends its Chat Session; the first one in a post says where
// to read the rest.
func channelTrigger(m channelMessage, channel, text string, first bool) string {
	where := "a post"
	if m.replyID != "" {
		where = "a reply to a post"
	}
	var b strings.Builder
	if m.at {
		fmt.Fprintf(&b, "WeCom user %s @-ed you in %s in the WeCom Channel %s.\n", m.user, where, channelLabel(channel, m.topicID))
	} else {
		fmt.Fprintf(&b, "WeCom user %s replied to the post in the WeCom Channel %s, without an @ of you; it may be for someone else.\n", m.user, channelLabel(channel, m.topicID))
	}
	fmt.Fprintf(&b, "topic_id: %s\npost_id: %s\n", m.topicID, m.postID)
	if m.replyID != "" {
		fmt.Fprintf(&b, "reply_id: %s\n", m.replyID)
	}
	b.WriteString("\n" + text)
	if first {
		b.WriteString("\n\nread_thread shows the whole post and its replies.")
	}
	return b.String()
}

// busyNotice is what an @ in a post, or a reply without one, sends its
// Chat Session while it works: who and where, not the text.
func busyNotice(m channelMessage) string {
	where := "this post"
	if m.replyID != "" {
		where = "reply " + m.replyID + " under this post"
	}
	did := "@-ed you in " + where
	if !m.at {
		did = "wrote " + where + ", without an @ of you,"
	}
	return fmt.Sprintf("WeCom user %s %s while you were working. "+
		"The Chat Thread has messages you have not read: read_thread shows them.", m.user, did)
}

// channelLabel names a WeCom Channel by its name, or its id when WeCom did
// not give the name.
func channelLabel(name, topicID string) string {
	if name == "" {
		return "with topic_id " + topicID
	}
	return fmt.Sprintf("%q", name)
}

func cut(text string) string {
	n := utf8.RuneCountInString(text)
	if n <= maxTriggerRunes {
		return text
	}
	return cutRunes(text, maxTriggerRunes, "") + fmt.Sprintf("\n\n[cut: the message is %d characters; this is the first %d]", n, maxTriggerRunes)
}

// newDirectTrigger is the first message of the Chat Session a direct
// message's !new starts: the rest of the text, or a note that there is none.
func newDirectTrigger(user, rest string) string {
	if rest == "" {
		return fmt.Sprintf("WeCom user %s started a new Chat Session with !new in a direct message, with no task yet.", user)
	}
	return directTrigger(user, rest)
}

// cutNew reports whether text is !new, alone or at its start, and the text
// after it.
func cutNew(text string) (string, bool) {
	t := strings.TrimSpace(text)
	if len(t) < len("!new") || !strings.EqualFold(t[:len("!new")], "!new") {
		return "", false
	}
	rest := t[len("!new"):]
	if r, _ := utf8.DecodeRuneInString(rest); rest != "" && !unicode.IsSpace(r) {
		return "", false
	}
	return strings.TrimSpace(rest), true
}
