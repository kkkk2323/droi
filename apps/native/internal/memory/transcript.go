// What Memory reads from a Session's transcript (the Daemon's JSONL session
// file, `transcript_path` in hook input): the user's own prompts and the
// assistant's prose. Tool calls, tool results and injected reminders stay out;
// they are noise for extraction and would inflate the turn count.

package memory

import (
	"encoding/json"
	"os"
	"strings"
)

// Turn is one user prompt or one stretch of assistant prose.
type Turn struct {
	Role string // "user" or "assistant"
	Text string
}

var injected = jsRegexp(`^\s*<(system-reminder|memory-context)[\s>]`)

// ParseTranscript reads the turns of a JSONL transcript.
func ParseTranscript(jsonl string) []Turn {
	turns := []Turn{}
	for _, line := range strings.Split(jsonl, "\n") {
		if jsTrim(line) == "" {
			continue
		}
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil || entry["type"] != "message" {
			continue
		}
		message, _ := entry["message"].(map[string]any)
		role, _ := message["role"].(string)
		if role != "user" && role != "assistant" {
			continue
		}
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
		var texts []string
		toolResult := false
		for _, b := range blocks {
			if b["type"] == "tool_result" {
				toolResult = true
				break
			}
			if text, ok := b["text"].(string); ok && b["type"] == "text" && !injected.MatchString(text) {
				if t := jsTrim(text); t != "" {
					texts = append(texts, t)
				}
			}
		}
		if toolResult || len(texts) == 0 {
			continue
		}
		turns = append(turns, Turn{Role: role, Text: strings.Join(texts, "\n\n")})
	}
	return turns
}

// ReadTranscript reads the turns of a transcript file; none when it cannot be read.
func ReadTranscript(path string) []Turn {
	b, err := os.ReadFile(path)
	if err != nil {
		return []Turn{}
	}
	return ParseTranscript(string(b))
}

// UserPromptCount counts the user's prompts.
func UserPromptCount(turns []Turn) int {
	n := 0
	for _, t := range turns {
		if t.Role == "user" {
			n++
		}
	}
	return n
}
