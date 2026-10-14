package chatbridge

import (
	"fmt"
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
