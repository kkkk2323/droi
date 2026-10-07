// Package memorywork is the Host's side of Memory (ADR 0011): the Memory
// Sessions that consolidate a Memory or extract entries from a finished
// Session, run on the Host's own Daemon in hidden Sessions, and what
// Settings → Memory shows.
package memorywork

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/sessions"
)

// Request is one Memory Session.
type Request struct {
	Title string
	// Cwd is where the Session runs; the Daemon needs an existing directory.
	Cwd     string
	ModelID string
	// Prompt is the task's instructions, appended to Droid's system prompt.
	Prompt string
	// Input is the one user message: the material to work on.
	Input string
	// Schema is the JSON Schema of the reply.
	Schema map[string]any
}

// Run runs one Memory Session and answers its structured reply, still
// unvalidated.
type Run func(ctx context.Context, r Request) (json.RawMessage, error)

// Dial connects to the Daemon.
type Dial func(ctx context.Context) (*droid.Client, error)

// NewRunner runs Memory Sessions on the Daemon that dial reaches: tagged so
// no Client lists them, never waiting on a permission prompt, answering in
// JSON, and archived once their one turn has ended.
func NewRunner(dial Dial) Run {
	return func(ctx context.Context, r Request) (json.RawMessage, error) {
		c, err := dial(ctx)
		if err != nil {
			return nil, err
		}
		defer c.Close()
		schema := map[string]json.RawMessage{}
		for k, v := range r.Schema {
			b, _ := json.Marshal(v)
			schema[k] = b
		}
		system, _ := json.Marshal(map[string]string{"type": "preset", "preset": "droid", "append": r.Prompt})
		yes := true
		init, err := c.InitializeSession(ctx, protocol.InitializeSessionParams{
			MachineID:                    "local",
			Cwd:                          r.Cwd,
			Title:                        r.Title,
			Tags:                         []protocol.SessionTag{{Name: sessions.MemoryTag}},
			PrivacyLevel:                 protocol.InitializeSessionParamsPrivacyLevelPrivate,
			ModelID:                      r.ModelID,
			SystemPrompt:                 system,
			AutoRejectPermissionRequests: &yes,
			StructuredOutputFormat:       &protocol.OutputFormat{Type: "json_schema", Schema: schema},
		})
		if err != nil {
			return nil, err
		}
		id := init.SessionID
		defer func() {
			_, _ = c.ArchiveSession(context.WithoutCancel(ctx), protocol.ArchiveSessionParams{SessionID: id})
		}()

		done := make(chan turnEnd, 1)
		var t turn
		unsub := c.Subscribe(func(n droid.Notification) {
			if n.Method != protocol.NotificationSessionNotification {
				return
			}
			var p protocol.SessionNotificationParams
			if json.Unmarshal(n.Params, &p) != nil || p.SessionID != id {
				return
			}
			if end, ok := t.see(p.Notification); ok {
				select {
				case done <- end:
				default:
				}
			}
		})
		defer unsub()
		if _, err := c.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: id, Text: r.Input}); err != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.Done():
			return nil, errors.New("Memory Session failed: the Daemon went away")
		case end := <-done:
			if end.failure != "" {
				return nil, fmt.Errorf("Memory Session failed: %s", end.failure)
			}
			return end.reply, nil
		}
	}
}

type turnEnd struct {
	reply   json.RawMessage
	failure string
}

// turn follows one turn's notifications as the TypeScript SDK's Result
// does: the structured output, or the final assistant text parsed as JSON
// when none came, and the first error for a failure.
type turn struct {
	structured json.RawMessage
	finalText  string
	firstError string
}

func (t *turn) see(n protocol.SessionNotificationParamsNotification) (turnEnd, bool) {
	v, err := n.Value()
	if err != nil {
		return turnEnd{}, false
	}
	switch v := v.(type) {
	case *protocol.StructuredOutputNotification:
		t.structured, _ = json.Marshal(v.StructuredOutput)
	case *protocol.CreateMessageNotification:
		if v.Message.Role == protocol.MessageRoleAssistant {
			t.finalText = textOf(v.Message.Content)
		}
	case *protocol.ErrorNotification:
		if t.firstError == "" {
			t.firstError = v.Message
		}
	case *protocol.AgentTurnCompletedNotification:
		reply := t.structured
		if reply == nil {
			var obj map[string]json.RawMessage
			if json.Unmarshal([]byte(t.finalText), &obj) == nil {
				reply = json.RawMessage(t.finalText)
			}
		}
		switch v.Reason {
		case protocol.AgentTurnCompletionReasonCompleted, protocol.AgentTurnCompletionReasonSpecHandoff:
			return turnEnd{reply: reply}, true
		case protocol.AgentTurnCompletionReasonCancelled, protocol.AgentTurnCompletionReasonPermissionRejected:
			return turnEnd{failure: "the turn ended as interrupted"}, true
		}
		msg := t.firstError
		if msg == "" {
			msg = "Agent turn ended: " + string(v.Reason)
		}
		return turnEnd{failure: msg}, true
	}
	return turnEnd{}, false
}

func textOf(blocks []protocol.ContentBlock) string {
	out := ""
	for _, b := range blocks {
		if v, err := b.Value(); err == nil {
			if tb, ok := v.(*protocol.TextBlock); ok {
				out += tb.Text
			}
		}
	}
	return out
}
