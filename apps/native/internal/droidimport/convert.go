// Package droidimport brings the droid CLI's sessions into Droi's Kernel (Q8): the user picks which, and each
// one becomes a conversation through conversation.import, once. droid's files are only read.
//
// A droid session is a JSONL transcript under ~/.factory/sessions/<cwd folder>/<id>.jsonl with a settings file
// beside it. Its messages carry a visibility: none for the conversation itself, user_only for what droid drew
// but the model never read (a Script's inner calls, hooks), llm_only for what the model read but the user never
// saw (reminders, completion notices). Only the conversation itself comes over; Droi's Kernel writes its own
// reminders on the next turn.
package droidimport

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/l10n"
)

// Conversation is one droid session as the entries conversation.import writes.
type Conversation struct {
	ID       string
	Title    string
	Cwd      string
	Archived bool
	// Model and Effort are what droid ran the session with, droid's names for them.
	Model  string
	Effort string
	// CreatedAt and LastAt are the first and last message's times, in epoch milliseconds; 0 without messages.
	CreatedAt int64
	LastAt    int64
	Entries   []kernel.ImportEntry
	// Heads are the compactions that keep part of what came before them: by the compaction's place in Entries,
	// the place of the first entry it keeps. The Kernel takes a head as an entry ID, so Write fills it in.
	Heads map[int]int
	// Hash is the transcript's SHA-256, which names the import: the same file imports into the same
	// conversation however often it is asked.
	Hash string
	// Messages counts droid's messages that came over, and Left the ones that did not: droid's own notices, a
	// Script's inner calls, thinking, and lines that are not JSON.
	Messages int
	Left     int
}

// compactionPrefix and compactionSuffix wrap a summary as Pi's own compaction does, so a model reads an imported
// summary as it reads one Kernel wrote.
const (
	compactionPrefix = "The conversation history before this point was compacted into the following summary:\n\n<summary>\n"
	compactionSuffix = "\n</summary>"
)

// noResult is the result a call gets when droid recorded none: a model's API refuses a call that nothing answers.
const noResult = "No result was recorded for this call."

// injected are the blocks droid put into a user's message for the model: reminders, notices and Memory. They are
// droid's context, not what the user said.
var injected = regexp.MustCompile(`(?s)<(system-reminder|system-notification|memory-context)(?:\s[^>]*)?>.*?</(?:system-reminder|system-notification|memory-context)>`)

// maxLine bounds one transcript line. droid writes a picture into the line of its message, and a long Session's
// largest lines are a few megabytes.
const maxLine = 256 << 20

// Convert reads a droid transcript, and the settings file beside it, as a conversation.
func Convert(path string) (Conversation, error) {
	f, err := os.Open(path)
	if err != nil {
		return Conversation{}, err
	}
	defer f.Close()
	sum := sha256.New()
	c := newConverter()
	sc := bufio.NewScanner(io.TeeReader(f, sum))
	sc.Buffer(make([]byte, 1<<20), maxLine)
	for sc.Scan() {
		c.line(sc.Bytes())
	}
	if err := sc.Err(); err != nil {
		return Conversation{}, fmt.Errorf("read %s: %w", path, err)
	}
	c.flush(c.at)
	out := c.out
	out.Entries = c.entries
	out.Hash = hex.EncodeToString(sum.Sum(nil))
	out.Archived, out.Model, out.Effort = settingsOf(path)
	return out, nil
}

// note is the line that ends an imported history: where it came from. It is written in the user's language, so
// Write sends it apart from the history, whose keys must stand for the same entries in any language.
func (conv Conversation) note() kernel.ImportEntry {
	data := map[string]any{"text": l10n.L("Imported from droid's session %s.", conv.ID), "source": "droid", "sourceSessionId": conv.ID}
	if conv.LastAt > 0 {
		data["at"] = conv.LastAt
	}
	raw, _ := json.Marshal(data)
	return kernel.ImportEntry{Kind: kernel.EntryNote, Data: raw}
}

// settingsOf reads, from the settings file beside the transcript, whether droid archived the session and the
// model and reasoning effort it ran with.
func settingsOf(transcript string) (archived bool, model, effort string) {
	raw, err := os.ReadFile(strings.TrimSuffix(transcript, ".jsonl") + ".settings.json")
	if err != nil {
		return false, "", ""
	}
	var s struct {
		ArchivedAt any    `json:"archivedAt"`
		Model      string `json:"model"`
		Effort     string `json:"reasoningEffort"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return false, "", ""
	}
	return s.ArchivedAt != nil, s.Model, s.Effort
}

type converter struct {
	out     Conversation
	entries []kernel.ImportEntry
	// at is the last time read, for a line that has none.
	at int64
	// calls are the last answer's calls that no result has answered yet, in order, by id, with their names.
	calls []call
	// ends maps a droid message's id to how many entries there were once it was read, for a compaction's anchor.
	ends map[string]int
}

func newConverter() *converter { return &converter{ends: map[string]int{}} }

type call struct{ id, name string }

type record struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Cwd       string          `json:"cwd"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
	Summary   string          `json:"summaryText"`
	Anchor    *struct {
		ID string `json:"id"`
	} `json:"anchorMessage"`
}

type message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Visibility string          `json:"visibility"`
	Provider   string          `json:"apiProvider"`
	Model      string          `json:"modelId"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Source    *struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source"`
}

func (c *converter) line(raw []byte) {
	var r record
	if json.Unmarshal(raw, &r) != nil {
		c.out.Left++
		return
	}
	switch r.Type {
	case "session_start":
		c.out.ID, c.out.Title, c.out.Cwd = r.ID, r.Title, r.Cwd
	case "compaction_state":
		c.compaction(r)
	case "message":
		var m message
		if json.Unmarshal(r.Message, &m) != nil {
			c.out.Left++
			return
		}
		c.message(m, c.time(r.Timestamp))
		if r.ID != "" {
			c.ends[r.ID] = len(c.entries)
		}
	}
}

// compaction writes droid's summary as Pi's. droid's drops everything up to its anchor message and keeps what came
// after it, as Pi's keeps everything from its head: the head is the first entry after the anchor's, or the summary
// itself when nothing after the anchor came over.
func (c *converter) compaction(r record) {
	at := c.time(r.Timestamp)
	kept := len(c.entries)
	if r.Anchor != nil {
		if end, ok := c.ends[r.Anchor.ID]; ok {
			kept = end
		}
	}
	// A result whose call went into the summary would answer nothing in the context that remains, and an earlier
	// summary is what this one replaces.
	for kept < len(c.entries) && (c.entries[kept].Kind == kernel.EntryToolResult || c.entries[kept].Kind == kernel.EntryCompaction) {
		kept++
	}
	if kept == len(c.entries) {
		c.flush(at)
		kept = len(c.entries)
	}
	if strings.TrimSpace(r.Summary) == "" {
		return
	}
	place := len(c.entries)
	c.add(kernel.EntryCompaction, map[string]any{"role": "user", "content": compactionPrefix + r.Summary + compactionSuffix, "timestamp": at}, kept == place)
	if kept < place {
		if c.out.Heads == nil {
			c.out.Heads = map[int]int{}
		}
		c.out.Heads[place] = kept
	}
}

// time is a line's time in epoch milliseconds, or the last one read.
func (c *converter) time(s string) int64 {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		c.at = t.UnixMilli()
	}
	return c.at
}

func (c *converter) message(m message, at int64) {
	blocks := blocksOf(m.Content)
	if m.Role == "assistant" {
		if m.Visibility != "" && m.Visibility != "both" {
			c.out.Left++
			return
		}
		c.answer(m, blocks, at)
		return
	}
	if m.Role != "user" {
		c.out.Left++
		return
	}
	// A result answers a call whatever its visibility: droid shows a few results to the user only, and a call
	// left unanswered would get a made-up one instead.
	answered := false
	var said []map[string]any
	for _, b := range blocks {
		switch b.Type {
		case "tool_result":
			if c.result(b, at) {
				answered = true
			}
		case "text":
			if text := strings.TrimSpace(injected.ReplaceAllString(b.Text, "")); text != "" {
				said = append(said, map[string]any{"type": "text", "text": text})
			}
		case "image":
			if img, ok := imageOf(b); ok {
				said = append(said, img)
			}
		}
	}
	if (m.Visibility != "" && m.Visibility != "both") || len(said) == 0 {
		if !answered {
			c.out.Left++
		}
		return
	}
	c.flush(at)
	var content any = said
	if len(said) == 1 && said[0]["type"] == "text" {
		content = said[0]["text"]
	}
	c.add(kernel.EntryUser, map[string]any{"role": "user", "content": content, "timestamp": at}, false)
}

func (c *converter) answer(m message, blocks []block, at int64) {
	c.flush(at)
	content := []map[string]any{}
	var calls []call
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				content = append(content, map[string]any{"type": "text", "text": b.Text})
			}
		case "tool_use":
			if b.ID == "" {
				continue
			}
			var args any = map[string]any{}
			if len(b.Input) > 0 && string(b.Input) != "null" {
				args = b.Input
			}
			content = append(content, map[string]any{"type": "toolCall", "id": b.ID, "name": b.Name, "arguments": args})
			calls = append(calls, call{b.ID, b.Name})
		}
		// thinking stays out: its signature is the provider's, for the model that wrote it.
	}
	if len(content) == 0 {
		c.out.Left++
		return
	}
	stop := "stop"
	if len(calls) > 0 {
		stop = "toolUse"
	}
	msg := map[string]any{"role": "assistant", "content": content, "stopReason": stop, "timestamp": at}
	if m.Provider != "" {
		msg["provider"] = m.Provider
	}
	if m.Model != "" {
		msg["model"] = m.Model
	}
	c.add(kernel.EntryAssistant, msg, false)
	c.calls = calls
}

// result writes the result of a call the last answer made; a result for any other call is left out, because a
// model's API refuses it.
func (c *converter) result(b block, at int64) bool {
	for i, pending := range c.calls {
		if pending.id != b.ToolUseID {
			continue
		}
		c.calls = append(c.calls[:i], c.calls[i+1:]...)
		c.add(kernel.EntryToolResult, map[string]any{
			"role": "toolResult", "toolCallId": pending.id, "toolName": pending.name,
			"content": resultContent(b.Content), "isError": b.IsError, "timestamp": at,
		}, false)
		return true
	}
	return false
}

// flush answers the calls nothing answered before the conversation moved on.
func (c *converter) flush(at int64) {
	for _, pending := range c.calls {
		c.out.LastAt = max(c.out.LastAt, at)
		c.entries = append(c.entries, entryOf(kernel.EntryToolResult, map[string]any{
			"role": "toolResult", "toolCallId": pending.id, "toolName": pending.name,
			"content": []map[string]any{{"type": "text", "text": noResult}}, "isError": true, "timestamp": at,
		}, false))
	}
	c.calls = nil
}

func (c *converter) add(kind string, msg map[string]any, head bool) {
	at, _ := msg["timestamp"].(int64)
	if c.out.CreatedAt == 0 || (at > 0 && at < c.out.CreatedAt) {
		c.out.CreatedAt = at
	}
	c.out.LastAt = max(c.out.LastAt, at)
	c.out.Messages++
	c.entries = append(c.entries, entryOf(kind, msg, head))
}

func entryOf(kind string, msg map[string]any, head bool) kernel.ImportEntry {
	raw, _ := json.Marshal([]map[string]any{msg})
	e := kernel.ImportEntry{Kind: kind, Model: raw}
	if head {
		e.Head = json.RawMessage(`"self"`)
	}
	return e
}

// blocksOf is a message's content as blocks: droid writes a plain prompt as a string.
func blocksOf(raw json.RawMessage) []block {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []block{{Type: "text", Text: s}}
	}
	var blocks []block
	_ = json.Unmarshal(raw, &blocks)
	return blocks
}

// resultContent is a tool result's content as Pi's blocks: text and pictures.
func resultContent(raw json.RawMessage) []map[string]any {
	out := []map[string]any{}
	for _, b := range blocksOf(raw) {
		switch b.Type {
		case "text":
			out = append(out, map[string]any{"type": "text", "text": b.Text})
		case "image":
			if img, ok := imageOf(b); ok {
				out = append(out, img)
			}
		}
	}
	return out
}

// imageOf is droid's picture, {source: {type: base64, media_type, data}}, as Pi's {data, mimeType}.
func imageOf(b block) (map[string]any, bool) {
	if b.Source == nil || b.Source.Type != "base64" || b.Source.Data == "" {
		return nil, false
	}
	mime := b.Source.MediaType
	if mime == "" {
		mime = "image/png"
	}
	return map[string]any{"type": "image", "data": b.Source.Data, "mimeType": mime}, true
}
