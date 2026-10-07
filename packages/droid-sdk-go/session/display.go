package session

import (
	"strings"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

const (
	systemReminderStart     = "<system-reminder>"
	systemReminderEnd       = "</system-reminder>"
	systemNotificationStart = "<system-notification>"
	systemNotificationEnd   = "</system-notification>"
)

// FilterMessagesForUI returns the messages a transcript shows. It drops
// messages hidden from user views or meant for the model only, strips
// <system-reminder> and <system-notification> blocks from the text of
// non-assistant messages, and drops messages left without content
// (persisted hook rows stay). The input is not modified.
func FilterMessagesForUI(msgs []protocol.FactoryDroidMessage) []protocol.FactoryDroidMessage {
	out := make([]protocol.FactoryDroidMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.HiddenFromUserViews != nil && *m.HiddenFromUserViews || m.Visibility == protocol.MessageVisibilityLLMOnly {
			continue
		}
		if m.Role != roleAssistant {
			content := make([]protocol.ContentBlock, 0, len(m.Content))
			for _, b := range m.Content {
				if b.Type == blockText {
					if t := decode[textView](b).Text; strings.Contains(t, systemReminderStart) || strings.Contains(t, systemNotificationStart) {
						t = strings.TrimSpace(stripSystemTags(t))
						if t == "" {
							continue
						}
						b = withFields(b, map[string]any{"text": t})
					}
				}
				content = append(content, b)
			}
			m.Content = content
		}
		if len(m.Content) == 0 && !isPersistedHook(m) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func stripSystemTags(text string) string {
	return stripTagged(stripTagged(text, systemReminderStart, systemReminderEnd), systemNotificationStart, systemNotificationEnd)
}

// stripTagged removes each complete start...end span; an unclosed start
// tag and what follows it stay.
func stripTagged(text, start, end string) string {
	var out strings.Builder
	for {
		i := strings.Index(text, start)
		if i < 0 {
			break
		}
		j := strings.Index(text[i+len(start):], end)
		if j < 0 {
			break
		}
		out.WriteString(text[:i])
		text = text[i+len(start)+j+len(end):]
	}
	out.WriteString(text)
	return out.String()
}
