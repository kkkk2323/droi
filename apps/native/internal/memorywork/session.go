// Package memorywork is the Host's side of Memory (ADR 0031): the Memory
// Sessions that summarize a finished Session and keep the Memory Summary,
// run on the Host's own Daemon in Sessions no Client lists (ADR 0011), and
// what Settings → Memory shows.
package memorywork

import (
	"context"
	"encoding/json"
	"errors"

	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
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
	// ReasoningEffort is what the Session asks of the model; "" is low. The
	// Daemon uses the model's default where the model has no such effort.
	ReasoningEffort protocol.ReasoningEffort
}

// Reply is a Memory Session's structured reply, still unvalidated, the
// Session, which the user can open, and the model it ran on: the Daemon runs
// a model it does not know as its default instead.
type Reply struct {
	JSON      json.RawMessage
	SessionID string
	Model     string
}

// Run runs one Memory Session. A Session that started and failed still
// answers its id.
type Run func(ctx context.Context, r Request) (Reply, error)

// Dial connects to the Daemon.
type Dial func(ctx context.Context) (*droid.Client, error)

// NewRunner runs Memory Sessions on the Daemon that dial reaches: tagged so
// no Client lists them, with no tool but the structured answer, answering in
// JSON, and archived and closed once their one turn has ended.
func NewRunner(dial Dial) Run {
	return func(ctx context.Context, r Request) (Reply, error) {
		c, err := dial(ctx)
		if err != nil {
			return Reply{}, err
		}
		defer c.Close()
		schema := map[string]json.RawMessage{}
		for k, v := range r.Schema {
			b, _ := json.Marshal(v)
			schema[k] = b
		}
		system, _ := json.Marshal(map[string]string{"type": "preset", "preset": "droid", "append": r.Prompt})
		yes := true
		effort := r.ReasoningEffort
		if effort == "" {
			// A model's default effort may be high: gemini-3.8-flash thought
			// for five minutes over 33 entries to sort.
			effort = protocol.ReasoningEffortLow
		}
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
			ReasoningEffort:              effort,
			// Asked not to, gemini-3.8-flash still tried to count bytes with
			// Script and Execute, and the refused call ended its turn.
			RestrictToolIDs: []string{"StructuredOutput"},
		})
		if err != nil {
			return Reply{}, err
		}
		id := init.SessionID
		defer func() {
			bg := context.WithoutCancel(ctx)
			_, _ = c.ArchiveSession(bg, protocol.ArchiveSessionParams{SessionID: id})
			// An open Session keeps its place in the Daemon, which runs only so
			// many at once: a pass of Memory Sessions left open would keep the
			// user's own from starting.
			_, _ = c.CloseSession(bg, protocol.CloseSessionParams{SessionID: id})
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
		started := Reply{SessionID: id, Model: init.Settings.ModelID}
		if _, err := c.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: id, Text: r.Input}); err != nil {
			return started, err
		}
		select {
		case <-ctx.Done():
			return started, ctx.Err()
		case <-c.Done():
			return started, errors.New(l10n.L("Memory Session failed: the Daemon went away"))
		case end := <-done:
			if end.failure != "" {
				return started, errors.New(l10n.L("Memory Session failed: %s", end.failure))
			}
			started.JSON = end.reply
			return started, nil
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
			return turnEnd{failure: l10n.L("the turn ended as interrupted")}, true
		}
		msg := t.firstError
		if msg == "" {
			msg = l10n.L("Agent turn ended: %s", string(v.Reason))
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
