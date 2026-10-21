package omp

import (
	"encoding/json"
	"strings"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

// TranscriptEntries turns live OMP messages into sessionview-compatible
// transcript entries: user/assistant text, thinking, and tool cards.
func TranscriptEntries(msgs []Message) []transcript.Entry {
	out := make([]transcript.Entry, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "user":
			e := transcript.Entry{ID: m.ID, User: true, CreatedAt: float64(m.Timestamp)}
			if m.Text != "" {
				e.Blocks = append(e.Blocks, transcript.Block{
					Kind: transcript.Text, ID: m.ID + ":t", Text: m.Text, Streaming: m.Streaming,
				})
			}
			if len(e.Blocks) > 0 {
				out = append(out, e)
			}
		case "assistant":
			e := transcript.Entry{ID: m.ID, CreatedAt: float64(m.Timestamp), IsError: m.IsError}
			if m.Thinking != "" {
				e.Blocks = append(e.Blocks, transcript.Block{
					Kind: transcript.Thinking, ID: m.ID + ":th", Text: m.Thinking, Streaming: m.Streaming,
				})
			}
			if m.Text != "" {
				e.Blocks = append(e.Blocks, transcript.Block{
					Kind: transcript.Text, ID: m.ID + ":t", Text: m.Text, Streaming: m.Streaming,
				})
			}
			if len(e.Blocks) > 0 {
				out = append(out, e)
			}
		case "tool":
			call := toolCallOf(m)
			e := transcript.Entry{ID: m.ID, CreatedAt: float64(m.Timestamp), IsError: m.IsError}
			e.Blocks = append(e.Blocks, transcript.Block{
				Kind: transcript.Tools, ID: m.ID + ":tools", Calls: []*transcript.ToolCall{call}, Streaming: m.Streaming,
			})
			out = append(out, e)
		}
	}
	return out
}

func toolCallOf(m Message) *transcript.ToolCall {
	name := m.ToolName
	if name == "" {
		name = "tool"
	}
	id := m.ToolID
	if id == "" {
		id = m.ID
	}
	input := map[string]any{}
	var keys []string
	var raw json.RawMessage
	if strings.TrimSpace(m.ToolArgs) != "" {
		raw = json.RawMessage(m.ToolArgs)
		if err := json.Unmarshal(raw, &input); err != nil {
			input = map[string]any{"args": m.ToolArgs}
			keys = []string{"args"}
			raw, _ = json.Marshal(input)
		} else {
			for k := range input {
				keys = append(keys, k)
			}
		}
	}
	useInput := map[string]json.RawMessage{}
	for k, v := range input {
		b, err := json.Marshal(v)
		if err != nil {
			continue
		}
		useInput[k] = b
	}
	call := &transcript.ToolCall{
		Use: protocol.ToolUse{
			Type:  "tool_use",
			ID:    id,
			Name:  name,
			Input: useInput,
		},
		Input:    input,
		RawInput: raw,
		Keys:     keys,
	}
	if m.ToolOut != "" || m.IsError {
		content, _ := json.Marshal(m.ToolOut)
		errFlag := m.IsError
		call.Result = &protocol.ToolResult{
			Type:      "tool_result",
			ToolUseID: id,
			Content:   content,
			IsError:   &errFlag,
		}
	}
	return call
}
