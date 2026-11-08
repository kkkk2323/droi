package testsupport

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// History is a Fake Daemon Session's messages as the entries conversation.import writes, so a view test that
// describes a Session's history draws the same transcript from the Kernel.
//
// Each message becomes Pi's entry for it: a user message a pi.user entry, an answer a pi.assistant entry whose
// tool_use blocks are tool calls, and each tool_result block of a "tool" message a pi.tool-result entry, named
// after the call it answers. Messages are a second apart after start, as the Fake Daemon's fixtures are. A block
// the Kernel has no counterpart for is refused by name rather than dropped: a history that quietly lost a block
// draws a transcript the test did not describe.
func History(messages []fakedaemon.Message, start time.Time) ([]kernel.ImportEntry, error) {
	at := start.UnixMilli()
	names := map[string]string{}
	var out []kernel.ImportEntry
	for i, m := range messages {
		at += 1000
		switch m.Role {
		case "user":
			content, err := userContent(m)
			if err != nil {
				return nil, fmt.Errorf("message %d: %w", i, err)
			}
			out = append(out, entryOf(kernel.EntryUser, map[string]any{"role": "user", "content": content, "timestamp": at}))
		case "assistant":
			blocks, calls, err := answerContent(m, names)
			if err != nil {
				return nil, fmt.Errorf("message %d: %w", i, err)
			}
			stop := "stop"
			if calls {
				stop = "toolUse"
			}
			out = append(out, entryOf(kernel.EntryAssistant, map[string]any{
				"role": "assistant", "content": blocks, "stopReason": stop, "timestamp": at,
			}))
		case "tool":
			results, err := toolResults(m, names, at)
			if err != nil {
				return nil, fmt.Errorf("message %d: %w", i, err)
			}
			out = append(out, results...)
		default:
			return nil, &UnsupportedScenarioError{Feature: "a message of role " + m.Role, Needs: "a Pi role for it"}
		}
	}
	return out, nil
}

func entryOf(kind string, message map[string]any) kernel.ImportEntry {
	raw, _ := json.Marshal([]map[string]any{message})
	return kernel.ImportEntry{Kind: kind, Model: raw}
}

// userContent is a user message's content: its text, or its text and pictures as blocks.
func userContent(m fakedaemon.Message) (any, error) {
	if len(m.Content) == 0 {
		return m.Text, nil
	}
	var blocks []map[string]any
	for _, b := range m.Content {
		switch b["type"] {
		case "text":
			blocks = append(blocks, map[string]any{"type": "text", "text": b["text"]})
		case "image":
			img, err := imageOf(b)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, img)
		default:
			return nil, &UnsupportedScenarioError{Feature: fmt.Sprintf("a user block of type %v", b["type"]), Needs: "a Pi block for it"}
		}
	}
	return blocks, nil
}

// answerContent is an answer's blocks, and whether it calls a tool. It records each call's name, which the
// tool result that answers it carries in Pi.
func answerContent(m fakedaemon.Message, names map[string]string) ([]map[string]any, bool, error) {
	if len(m.Content) == 0 {
		return []map[string]any{{"type": "text", "text": m.Text}}, false, nil
	}
	blocks := []map[string]any{}
	calls := false
	for _, b := range m.Content {
		switch b["type"] {
		case "text":
			blocks = append(blocks, map[string]any{"type": "text", "text": b["text"]})
		case "thinking":
			thinking := map[string]any{"type": "thinking", "thinking": b["thinking"]}
			if ms, ok := b["durationMs"]; ok {
				thinking["durationMs"] = ms
			}
			blocks = append(blocks, thinking)
		case "tool_use":
			id, _ := b["id"].(string)
			name, _ := b["name"].(string)
			names[id] = name
			args := b["input"]
			if args == nil {
				args = map[string]any{}
			}
			blocks = append(blocks, map[string]any{"type": "toolCall", "id": id, "name": name, "arguments": args})
			calls = true
		default:
			return nil, false, &UnsupportedScenarioError{Feature: fmt.Sprintf("an answer block of type %v", b["type"]), Needs: "a Pi block for it"}
		}
	}
	return blocks, calls, nil
}

// toolResults are a "tool" message's results, one pi.tool-result entry each.
func toolResults(m fakedaemon.Message, names map[string]string, at int64) ([]kernel.ImportEntry, error) {
	var out []kernel.ImportEntry
	for _, b := range m.Content {
		if b["type"] != "tool_result" {
			return nil, &UnsupportedScenarioError{Feature: fmt.Sprintf("a tool block of type %v", b["type"]), Needs: "a Pi block for it"}
		}
		id, _ := b["toolUseId"].(string)
		isError, _ := b["isError"].(bool)
		content, err := resultContent(b["content"])
		if err != nil {
			return nil, err
		}
		out = append(out, entryOf(kernel.EntryToolResult, map[string]any{
			"role": "toolResult", "toolCallId": id, "toolName": names[id], "content": content, "isError": isError, "timestamp": at,
		}))
	}
	return out, nil
}

// resultContent is a tool result's content as Pi blocks: a string is one text block.
func resultContent(v any) ([]map[string]any, error) {
	switch c := v.(type) {
	case nil:
		return []map[string]any{}, nil
	case string:
		return []map[string]any{{"type": "text", "text": c}}, nil
	case []map[string]any:
		return resultBlocks(c)
	case []any:
		blocks := make([]map[string]any, 0, len(c))
		for _, x := range c {
			b, ok := x.(map[string]any)
			if !ok {
				return nil, &UnsupportedScenarioError{Feature: fmt.Sprintf("a tool result block %T", x), Needs: "a block object"}
			}
			blocks = append(blocks, b)
		}
		return resultBlocks(blocks)
	}
	return nil, &UnsupportedScenarioError{Feature: fmt.Sprintf("tool result content %T", v), Needs: "a string or blocks"}
}

func resultBlocks(in []map[string]any) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(in))
	for _, b := range in {
		switch b["type"] {
		case "text":
			out = append(out, map[string]any{"type": "text", "text": b["text"]})
		case "image":
			img, err := imageOf(b)
			if err != nil {
				return nil, err
			}
			out = append(out, img)
		default:
			return nil, &UnsupportedScenarioError{Feature: fmt.Sprintf("a tool result block of type %v", b["type"]), Needs: "a Pi block for it"}
		}
	}
	return out, nil
}

// imageOf is a Daemon image block, {source: {data, mediaType}}, as Pi's {data, mimeType}.
func imageOf(b map[string]any) (map[string]any, error) {
	src, _ := b["source"].(map[string]any)
	data, _ := src["data"].(string)
	mime, _ := src["mediaType"].(string)
	if data == "" {
		return nil, &UnsupportedScenarioError{Feature: "an image without base64 data", Needs: "the picture's bytes"}
	}
	if mime == "" {
		mime = "image/png"
	}
	return map[string]any{"type": "image", "data": data, "mimeType": mime}, nil
}

// Importer is the call ImportSessions makes: a Kernel client, or an Observer of one.
type Importer interface {
	Import(ctx context.Context, p kernel.ImportParams) (kernel.ImportResult, error)
}

// importBatch is the Kernel's limits.maxImportEntriesPerCall.
const importBatch = 1000

// ImportSessions writes each Session of a Fake Daemon Scenario into a Kernel as a conversation with its title,
// working directory and history, and answers each Session id's conversation. ids are the Fake Daemon's ids of
// sc.Sessions, in order (fakedaemon.Daemon.Sessions), so the window's pages, which still name a Session by the
// Daemon's id while its list comes from the Daemon, find the conversation that holds it.
//
// The times follow the Fake Daemon's one clock from start: a second for each message, then a minute for the
// Session, so the times a page draws are the ones the Daemon would have given it. agent, when set, is what each
// conversation runs with: the model a test's messages are answered by.
func ImportSessions(ctx context.Context, k Importer, sc fakedaemon.Scenario, ids []string, start time.Time, agent *kernel.AgentChange) (map[string]kernel.ID, error) {
	if len(ids) != len(sc.Sessions) {
		return nil, fmt.Errorf("%d session ids for %d sessions", len(ids), len(sc.Sessions))
	}
	out := make(map[string]kernel.ID, len(ids))
	clock := start
	for i, s := range sc.Sessions {
		if err := sessionNeeds(s); err != nil {
			return nil, err
		}
		entries, err := History(s.Messages, clock)
		if err != nil {
			return nil, fmt.Errorf("session %q: %w", s.Title, err)
		}
		clock = clock.Add(time.Duration(len(s.Messages))*time.Second + time.Minute)
		created := clock.UnixMilli()
		p := kernel.ImportParams{Title: s.Title, Cwd: s.Cwd, CreatedAt: &created, Agent: agent}
		var conv kernel.ID
		for first := true; first || len(entries) > 0; first = false {
			batch := entries[:min(importBatch, len(entries))]
			entries = entries[len(batch):]
			p.Entries, p.ID = batch, conv
			if conv != 0 {
				p.Title, p.Cwd, p.CreatedAt, p.Agent = "", "", nil, nil
			}
			if p.Entries == nil {
				// A Session with no messages yet: Kernel takes an empty batch, not a missing one.
				p.Entries = []kernel.ImportEntry{}
			}
			res, err := k.Import(ctx, p)
			if err != nil {
				return nil, fmt.Errorf("session %q: conversation.import: %w", s.Title, err)
			}
			conv = res.ConversationID
		}
		out[ids[i]] = conv
	}
	return out, nil
}
