package memory

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// What Memory reads from a Kernel conversation to summarize it (ADR 0042): the
// same rows as a droid transcript — the user's prompts, the replies, the
// compaction summaries and one line per tool call, tool result and note — read
// from conversation.entries instead of droid's JSONL.

// kernelMessage is the part of a Pi message a transcript reads.
type kernelMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	IsError bool            `json:"isError"`
}

type kernelBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// blocks reads a message's content, which is a string whenever it held only text.
func (m kernelMessage) blocks() []kernelBlock {
	var text string
	if json.Unmarshal(m.Content, &text) == nil {
		return []kernelBlock{{Type: "text", Text: text}}
	}
	var out []kernelBlock
	_ = json.Unmarshal(m.Content, &out)
	return out
}

func kernelMessages(raw json.RawMessage) []kernelMessage {
	var out []kernelMessage
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// skillBlock is how the Kernel sends a skill command to the model (resources/expand.ts); the user typed /name args.
var skillBlock = regexp.MustCompile(`^<skill name="([^"]+)" location="[^"]*">\n[\s\S]*\n</skill>(?:\n\n([\s\S]*))?$`)

// asTyped is a user message as the user typed it.
func asTyped(text string) string {
	if m := skillBlock.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace("/" + m[1] + " " + m[2])
	}
	return text
}

// compactionSummary is the summary Pi wraps in the message a compaction writes.
func compactionSummary(text string) string {
	if open := strings.Index(text, "<summary>"); open >= 0 {
		text = text[open+len("<summary>"):]
		if end := strings.LastIndex(text, "</summary>"); end >= 0 {
			text = text[:end]
		}
	}
	return strings.TrimSpace(text)
}

// KernelTranscript renders a conversation's entries within budget characters, as ReadTranscript renders a droid
// transcript. Entries a fork inherited are its source's words, summarized with the source, and are left out.
func KernelTranscript(id kernel.ID, entries []kernel.Entry, budget int) Transcript {
	var t Transcript
	var rows []row
	for _, e := range entries {
		if e.ConversationID != id {
			continue
		}
		switch e.Kind {
		case kernel.EntryUser:
			for _, m := range kernelMessages(e.Model) {
				for _, b := range m.blocks() {
					text := strings.TrimSpace(injectedBlocks.ReplaceAllString(asTyped(b.Text), ""))
					if b.Type != "text" || text == "" {
						continue
					}
					t.UserPrompts++
					rows = append(rows, row{tierUser, "User: " + clip(text, rowCaps[tierUser])})
				}
			}
		case kernel.EntryAssistant:
			for _, m := range kernelMessages(e.Model) {
				for _, b := range m.blocks() {
					switch b.Type {
					case "text":
						if text := strings.TrimSpace(b.Text); text != "" {
							rows = append(rows, row{tierReply, "Droid: " + clip(text, rowCaps[tierReply])})
						}
					case "toolCall":
						rows = append(rows, row{tierTool, "[tool call] " + b.Name + " " + clip(oneLine(string(b.Arguments)), rowCaps[tierTool])})
					}
				}
			}
		case kernel.EntryToolResult:
			for _, m := range kernelMessages(e.Model) {
				var parts []string
				for _, b := range m.blocks() {
					if b.Type == "text" && b.Text != "" {
						parts = append(parts, b.Text)
					}
				}
				label := "[tool result] "
				if m.IsError {
					label = "[tool error] "
				}
				rows = append(rows, row{tierTool, label + clip(oneLine(strings.Join(parts, " ")), rowCaps[tierTool])})
			}
		case kernel.EntryCompaction:
			for _, m := range kernelMessages(e.Model) {
				for _, b := range m.blocks() {
					if s := compactionSummary(b.Text); b.Type == "text" && s != "" {
						rows = append(rows, row{tierCompaction, "[Earlier part of the Session, as compacted] " + clip(s, rowCaps[tierCompaction])})
					}
				}
			}
		case kernel.EntryNote:
			var data struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(e.Data, &data) == nil && strings.TrimSpace(data.Text) != "" {
				rows = append(rows, row{tierTool, "[notice] " + clip(oneLine(data.Text), rowCaps[tierTool])})
			}
		}
	}
	t.Text, t.Omitted = fit(rows, budget)
	return t
}
