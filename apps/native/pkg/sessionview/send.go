package sessionview

import (
	"strings"
	"time"

	"github.com/google/uuid"
	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/attachments"
)

// Send sends a message with its images, or queues it with placement while a
// turn runs; an idle send (placement "") shows at once as an optimistic
// message. It scrolls the transcript to its end, and a failure shows as
// the composer's error.
func (v *View) Send(text string, images []attachments.Image, placement protocol.QueuePlacement) {
	ctl := v.env.Controller
	trimmed := strings.TrimSpace(text)
	if ctl == nil || trimmed == "" && len(images) == 0 {
		return
	}
	v.err = ""
	requestID, messageID := uuid.NewString(), uuid.NewString()
	var content []protocol.ContentBlock
	if trimmed != "" {
		b, _ := protocol.NewContentBlock(map[string]any{"type": "text", "text": trimmed})
		content = append(content, b)
	}
	sources := make([]protocol.Base64ImageSource, len(images))
	for i, img := range images {
		sources[i] = protocol.Base64ImageSource{Type: "base64", Data: img.Data, MediaType: protocol.Base64ImageSourceMediaType(img.MediaType)}
		b, _ := protocol.NewContentBlock(map[string]any{"type": "image", "source": sources[i]})
		content = append(content, b)
	}
	if placement == "" {
		now := float64(time.Now().UnixMilli())
		ctl.Store().RegisterOptimisticSubmit(session.OptimisticSubmit{
			SessionID:         v.id,
			ExternalKey:       requestID,
			UserMessage:       protocol.FactoryDroidMessage{ID: messageID, Role: protocol.MessageRoleUser, Content: content, CreatedAt: now, UpdatedAt: now},
			AssistantBubbleID: uuid.NewString(),
			OnError:           func(err error) { v.Fail(err) },
		})
	}
	v.ScrollToLatest()
	p := protocol.AddUserMessageParams{SessionID: v.id, Text: trimmed, MessageID: messageID, QueuePlacement: placement}
	if len(sources) > 0 {
		p.Images = sources
	}
	go func() {
		if _, err := ctl.AddUserMessage(droid.WithRequestID(v.env.Ctx, requestID), p); err != nil {
			if placement == "" {
				ctl.Store().CancelOptimisticSubmit(requestID)
			}
			v.Fail(err)
		}
	}()
}

// Fail shows err as the composer's error. Any goroutine.
func (v *View) Fail(err error) {
	v.env.Update(func() { v.err = err.Error() })
}

// Err is the composer's error line, "" for none.
func (v *View) Err() string { return v.err }

// SetErr sets the composer's error line; "" clears it. UI thread only.
func (v *View) SetErr(msg string) { v.err = msg }

// Interrupt stops the Session's running turn.
func (v *View) Interrupt() {
	ctl := v.env.Controller
	if ctl == nil {
		return
	}
	go func() { _ = ctl.InterruptSession(v.env.Ctx, v.id) }()
}
