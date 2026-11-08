package droidimport

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// resultCap bounds one tool result in the Markdown: the conversation is what the user reads there.
const resultCap = 2000

// Markdown is conv as a document the user can keep when its import failed: the prompts, the answers, each tool
// call and the start of its result, and the compaction summaries. Pictures are named, not embedded.
func Markdown(conv Conversation) string {
	var b strings.Builder
	title := conv.Title
	if title == "" {
		title = conv.ID
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	if conv.Cwd != "" {
		fmt.Fprintf(&b, "- Folder: `%s`\n", conv.Cwd)
	}
	fmt.Fprintf(&b, "- droid session: `%s`\n", conv.ID)
	if conv.CreatedAt > 0 {
		fmt.Fprintf(&b, "- Started: %s\n", time.UnixMilli(conv.CreatedAt).Format(time.RFC3339))
	}
	b.WriteString("\n")
	for _, e := range conv.Entries {
		var msgs []struct {
			Content    json.RawMessage `json:"content"`
			ToolName   string          `json:"toolName"`
			IsError    bool            `json:"isError"`
			ToolCallID string          `json:"toolCallId"`
		}
		_ = json.Unmarshal(e.Model, &msgs)
		for _, m := range msgs {
			switch e.Kind {
			case kernel.EntryUser:
				fmt.Fprintf(&b, "## User\n\n%s\n\n", strings.TrimSpace(markdownText(m.Content, 0)))
			case kernel.EntryAssistant:
				b.WriteString("## Droid\n\n")
				b.WriteString(answerMarkdown(m.Content))
			case kernel.EntryToolResult:
				label := "Result"
				if m.IsError {
					label = "Error"
				}
				fmt.Fprintf(&b, "%s of `%s`:\n\n```text\n%s\n```\n\n", label, m.ToolName, fence(markdownText(m.Content, resultCap)))
			case kernel.EntryCompaction:
				fmt.Fprintf(&b, "## Earlier part, as compacted\n\n%s\n\n", strings.TrimSpace(markdownText(m.Content, 0)))
			}
		}
	}
	return b.String()
}

func answerMarkdown(raw json.RawMessage) string {
	var blocks []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	_ = json.Unmarshal(raw, &blocks)
	var b strings.Builder
	for _, bl := range blocks {
		switch bl.Type {
		case "text":
			fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(bl.Text))
		case "toolCall":
			args, _ := json.MarshalIndent(json.RawMessage(bl.Arguments), "", "  ")
			fmt.Fprintf(&b, "Tool `%s`:\n\n```json\n%s\n```\n\n", bl.Name, fence(string(args)))
		}
	}
	return b.String()
}

// markdownText is content's text, a string or blocks, cut at limit runes when limit is set.
func markdownText(raw json.RawMessage, limit int) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		_ = json.Unmarshal(raw, &blocks)
		var parts []string
		for _, bl := range blocks {
			switch bl.Type {
			case "text":
				parts = append(parts, bl.Text)
			case "image":
				parts = append(parts, "[picture]")
			}
		}
		s = strings.Join(parts, "\n")
	}
	if r := []rune(s); limit > 0 && len(r) > limit {
		s = string(r[:limit]) + "\n…"
	}
	return s
}

// fence keeps a block's own fences from closing the one around it.
func fence(s string) string { return strings.ReplaceAll(s, "```", "` ` `") }
