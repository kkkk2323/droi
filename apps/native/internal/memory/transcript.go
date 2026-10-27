// What Memory reads from a Session's transcript (the Daemon's JSONL session
// file) to summarize it: the user's prompts, Droid's replies, compaction
// summaries and one line per tool call, tool result and notice from the
// Daemon. Injected reminders, skill bodies and thinking stay out.

package memory

import (
	"bufio"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Transcript is a Session as a Session Summary is written from it.
type Transcript struct {
	Cwd   string
	Title string
	// Subagent is set for a Session another Session started.
	Subagent bool
	// UserPrompts counts the user's own prompts.
	UserPrompts int
	// Text is the rendered conversation within the budget it was read with.
	Text string
	// Omitted says some of it did not fit.
	Omitted bool
}

// A row is one line of the rendered conversation; tier ranks what is kept
// when the budget is short, the user's words first.
type row struct {
	tier int
	text string
}

const (
	tierUser = iota
	tierReply
	tierCompaction
	tierTool
)

var rowCaps = map[int]int{tierUser: 6_000, tierReply: 4_000, tierCompaction: 8_000, tierTool: 300}

var injectedBlocks = regexp.MustCompile(`(?s)<(system-reminder|system-notification|memory-context)[\s>].*?</(system-reminder|system-notification|memory-context)>`)

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// toolResultText is a tool result's text, from a string or text blocks.
func toolResultText(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, b := range c {
			if m, ok := b.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// ReadTranscript renders a transcript within budget characters, keeping the
// newest rows of each tier first, the user's prompts before Droid's replies,
// compaction summaries and tool lines, and putting them back in order.
func ReadTranscript(file string, budget int) (Transcript, error) {
	f, err := os.Open(file)
	if err != nil {
		return Transcript{}, err
	}
	defer f.Close()
	var t Transcript
	var rows []row
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	for scanner.Scan() {
		var entry map[string]any
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		switch entry["type"] {
		case "session_start":
			t.Cwd, _ = entry["cwd"].(string)
			t.Title, _ = entry["title"].(string)
			calling, _ := entry["callingSessionId"].(string)
			t.Subagent = calling != ""
		case "compaction_state":
			if s, _ := entry["summaryText"].(string); strings.TrimSpace(s) != "" {
				rows = append(rows, row{tierCompaction, "[Earlier part of the Session, as compacted] " + clip(s, rowCaps[tierCompaction])})
			}
		case "message":
			message, _ := entry["message"].(map[string]any)
			role, _ := message["role"].(string)
			// The Daemon's own messages in the user's turn (a finished background
			// task or Script, an interruption, an error) carry a visibility; the
			// user's prompts have none.
			_, notice := message["visibility"]
			var blocks []map[string]any
			switch content := message["content"].(type) {
			case string:
				blocks = []map[string]any{{"type": "text", "text": content}}
			case []any:
				for _, b := range content {
					if block, ok := b.(map[string]any); ok {
						blocks = append(blocks, block)
					}
				}
			}
			for _, b := range blocks {
				switch b["type"] {
				case "text":
					text, _ := b["text"].(string)
					text = strings.TrimSpace(injectedBlocks.ReplaceAllString(text, ""))
					if text == "" {
						continue
					}
					if role == "user" && notice {
						rows = append(rows, row{tierTool, "[notice] " + clip(oneLine(text), rowCaps[tierTool])})
					} else if role == "user" {
						t.UserPrompts++
						rows = append(rows, row{tierUser, "User: " + clip(text, rowCaps[tierUser])})
					} else if role == "assistant" {
						rows = append(rows, row{tierReply, "Droid: " + clip(text, rowCaps[tierReply])})
					}
				case "tool_use":
					name, _ := b["name"].(string)
					input, _ := json.Marshal(b["input"])
					rows = append(rows, row{tierTool, "[tool call] " + name + " " + clip(oneLine(string(input)), rowCaps[tierTool])})
				case "tool_result":
					label := "[tool result] "
					if isErr, _ := b["is_error"].(bool); isErr {
						label = "[tool error] "
					}
					rows = append(rows, row{tierTool, label + clip(oneLine(toolResultText(b["content"])), rowCaps[tierTool])})
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return t, err
	}
	keep := make([]bool, len(rows))
	left := budget
	for tier := tierUser; tier <= tierTool; tier++ {
		for i := len(rows) - 1; i >= 0; i-- {
			if rows[i].tier != tier {
				continue
			}
			if n := utf8.RuneCountInString(rows[i].text) + 1; n <= left {
				keep[i] = true
				left -= n
			}
		}
	}
	var b strings.Builder
	gap := false
	for i, r := range rows {
		if !keep[i] {
			gap = true
			continue
		}
		if gap {
			b.WriteString("[... omitted ...]\n")
			t.Omitted = true
			gap = false
		}
		b.WriteString(r.text)
		b.WriteByte('\n')
	}
	if gap {
		b.WriteString("[... omitted ...]\n")
		t.Omitted = true
	}
	t.Text = b.String()
	return t, nil
}
