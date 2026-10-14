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
			"Each time someone @s you there, the @ arrives in this Session as a message. "+
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

// channelTrigger is the message an @ in a Channel post sends its Chat
// Session; the first one in a post says where to read the rest.
func channelTrigger(user, channel, topicID, postID, replyID, text string, first bool) string {
	where := "a post"
	if replyID != "" {
		where = "a reply to a post"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "WeCom user %s @-ed you in %s in the WeCom Channel %s.\ntopic_id: %s\npost_id: %s\n", user, where, channelLabel(channel, topicID), topicID, postID)
	if replyID != "" {
		fmt.Fprintf(&b, "reply_id: %s\n", replyID)
	}
	b.WriteString("\n" + text)
	if first {
		b.WriteString("\n\nread_thread shows the whole post and its replies.")
	}
	return b.String()
}

// busyNotice is what an @ in a post sends its Chat Session while it works:
// who and where, not the text.
func busyNotice(user, replyID string) string {
	where := "this post"
	if replyID != "" {
		where = "reply " + replyID + " under this post"
	}
	return fmt.Sprintf("WeCom user %s @-ed you in %s while you were working. "+
		"The Chat Thread has messages you have not read: read_thread shows them.", user, where)
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
	r := []rune(text)
	if len(r) <= maxTriggerRunes {
		return text
	}
	return string(r[:maxTriggerRunes]) + fmt.Sprintf("\n\n[cut: the message is %d characters; this is the first %d]", len(r), maxTriggerRunes)
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
