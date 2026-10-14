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
func systemPromptAppend(addition string, th *thread) string {
	facts := fmt.Sprintf("You are working in a WeCom (企业微信) Chat Thread: a direct message with the WeCom user %s. "+
		"Nothing you write reaches them unless you call the droi-chat tool reply. "+
		"The droi-chat tools act on this Chat Thread only, and their messages arrive in this Session.", th.userID)
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
