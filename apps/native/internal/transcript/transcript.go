// Package transcript turns a Session's flat message list into what the
// transcript renders: user turns, assistant turns whose tool calls carry
// their results, and nothing for bare tool-result messages, which only exist
// to complete a tool call.
package transcript

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/session"
)

// The tool that starts a subagent, the one that runs a program calling other
// tools, and the one that waits on such a run past its first minute.
const (
	TaskTool          = "Task"
	ScriptTool        = "Script"
	WaitForScriptTool = "WaitForScript"
)

// Image is a base64 picture as the Daemon sent it.
type Image struct {
	MediaType string
	Data      string
}

// ToolCall is a tool use and, once it came, its result.
type ToolCall struct {
	Use   protocol.ToolUse
	Input map[string]any
	// The input as the Daemon sent it, and its keys in that order: a row's
	// summary takes the first few.
	RawInput json.RawMessage
	Keys     []string
	Result   *protocol.ToolResult
	// Set on a Script or WaitForScript call (Lifecycle): the calls its run made
	// while this call was the latest one watching it.
	Nested    []*ToolCall
	Lifecycle bool
}

type BlockKind uint8

const (
	Text BlockKind = iota
	Picture
	Thinking
	// A run of tool calls with nothing said in between; rendered as one cluster.
	Tools
	// A Task call: work handed to a subagent, shown on its own.
	Subagent
)

type Block struct {
	Kind       BlockKind
	ID         string
	Text       string
	Image      Image
	DurationMs *float64
	Streaming  bool
	Calls      []*ToolCall
	Call       *ToolCall
}

type Entry struct {
	ID        string
	User      bool
	Blocks    []Block
	CreatedAt float64
	IsError   bool
}

// ScriptRunOf is the run a Script or WaitForScript call is about: the
// Script's own tool use id.
func ScriptRunOf(use protocol.ToolUse, input map[string]any) string {
	if use.ScriptExecution != nil {
		return ""
	}
	if use.Name == ScriptTool {
		return use.ID
	}
	if w, ok := input["toolCallId"].(string); ok && use.Name == WaitForScriptTool {
		return w
	}
	return ""
}

func decodeInput(raw map[string]json.RawMessage) map[string]any {
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		var x any
		if json.Unmarshal(v, &x) == nil {
			out[k] = x
		}
	}
	return out
}

func newCall(b protocol.ContentBlock, use protocol.ToolUse, result *protocol.ToolResult) *ToolCall {
	var raw struct {
		Input json.RawMessage `json:"input"`
	}
	_ = json.Unmarshal(b.Raw, &raw)
	if len(raw.Input) == 0 || raw.Input[0] != '{' {
		raw.Input = json.RawMessage("{}")
	}
	return &ToolCall{Use: use, Input: decodeInput(use.Input), RawInput: raw.Input, Keys: orderedKeys(raw.Input), Result: result}
}

func decode[T any](b protocol.ContentBlock) (T, bool) {
	var v T
	return v, json.Unmarshal(b.Raw, &v) == nil
}

// Build makes the transcript of the messages a Session shows.
func Build(messages []protocol.FactoryDroidMessage) []*Entry {
	results := map[string]*protocol.ToolResult{}
	for _, m := range messages {
		if m.Role != protocol.MessageRoleTool {
			continue
		}
		for _, b := range m.Content {
			if b.Type != protocol.ContentBlockTypeToolResult {
				continue
			}
			if r, ok := decode[protocol.ToolResult](b); ok {
				results[r.ToolUseID] = &r
			}
		}
	}

	// A call a Script made goes under the latest call watching its run, so the
	// calls a WaitForScript saw show where that wait sits in the turn.
	watching := map[string]*ToolCall{}
	var entries []*Entry
	for _, m := range messages {
		if m.Role != protocol.MessageRoleUser && m.Role != protocol.MessageRoleAssistant {
			continue
		}
		if m.IsUserVisible != nil && !*m.IsUserVisible {
			continue
		}
		var blocks []Block
		for i, b := range m.Content {
			id := fmt.Sprintf("%s:%d", m.ID, i)
			switch b.Type {
			case protocol.ContentBlockTypeText:
				if t, _ := decode[protocol.TextBlock](b); t.Text != "" {
					blocks = append(blocks, Block{Kind: Text, ID: id, Text: t.Text, Streaming: session.IsStreaming(b)})
				}
			case protocol.ContentBlockTypeImage:
				if im, _ := decode[protocol.ImageBlock](b); im.Source.Type == "base64" {
					blocks = append(blocks, Block{Kind: Picture, ID: id, Image: Image{string(im.Source.MediaType), im.Source.Data}})
				}
			case protocol.ContentBlockTypeThinking:
				if t, _ := decode[protocol.ThinkingBlock](b); t.Thinking != "" {
					blocks = append(blocks, Block{Kind: Thinking, ID: id, Text: t.Thinking, DurationMs: t.DurationMs, Streaming: session.IsStreaming(b)})
				}
			case protocol.ContentBlockTypeToolUse:
				use, ok := decode[protocol.ToolUse](b)
				if !ok {
					continue
				}
				call := newCall(b, use, results[use.ID])
				if use.ScriptExecution != nil {
					if owner := watching[use.ScriptExecution.OuterToolUseID]; owner != nil {
						owner.Nested = append(owner.Nested, call)
						continue
					}
				}
				if run := ScriptRunOf(use, call.Input); run != "" {
					call.Lifecycle = true
					watching[run] = call
				}
				if use.Name == TaskTool {
					blocks = append(blocks, Block{Kind: Subagent, ID: id, Call: call})
					continue
				}
				if n := len(blocks); n > 0 && blocks[n-1].Kind == Tools {
					blocks[n-1].Calls = append(blocks[n-1].Calls, call)
				} else {
					blocks = append(blocks, Block{Kind: Tools, ID: id, Calls: []*ToolCall{call}})
				}
			}
		}
		// A user message with nothing to show is a record, not a turn: the
		// Daemon persists each hook run as one, and a message that was only a
		// system reminder loses its text on the way.
		if len(blocks) == 0 {
			continue
		}
		isError := m.IsError != nil && *m.IsError
		user := m.Role == protocol.MessageRoleUser
		// One turn arrives as several assistant messages; shown as one entry
		// so nothing splits it. Tool runs join up too.
		if n := len(entries); !user && n > 0 && !entries[n-1].User {
			prev := entries[n-1]
			for _, b := range blocks {
				if k := len(prev.Blocks); b.Kind == Tools && k > 0 && prev.Blocks[k-1].Kind == Tools {
					prev.Blocks[k-1].Calls = append(prev.Blocks[k-1].Calls, b.Calls...)
				} else {
					prev.Blocks = append(prev.Blocks, b)
				}
			}
			prev.CreatedAt = m.CreatedAt
			prev.IsError = prev.IsError || isError
			continue
		}
		entries = append(entries, &Entry{ID: m.ID, User: user, Blocks: blocks, CreatedAt: m.CreatedAt, IsError: isError})
	}
	return entries
}

// ReuseUnchanged keeps the previous build's entries wherever nothing
// changed, so rows can skip rendering; while a turn streams only its last
// entry is new. It reports whether anything changed.
func ReuseUnchanged(previous, next []*Entry) ([]*Entry, bool) {
	if len(previous) == 0 {
		return next, len(next) > 0
	}
	byID := make(map[string]*Entry, len(previous))
	for _, e := range previous {
		byID[e.ID] = e
	}
	changed := len(previous) != len(next)
	out := make([]*Entry, len(next))
	for i, e := range next {
		if old := byID[e.ID]; old != nil && reflect.DeepEqual(old, e) {
			if i >= len(previous) || previous[i] != old {
				changed = true
			}
			out[i] = old
			continue
		}
		changed = true
		out[i] = e
	}
	if !changed {
		return previous, false
	}
	return out, true
}

type resultPart struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Source *struct {
		Type      string `json:"type"`
		MediaType string `json:"mediaType"`
		Data      string `json:"data"`
	} `json:"source"`
}

func resultParts(r *protocol.ToolResult) (string, []resultPart, bool) {
	if r == nil || len(r.Content) == 0 {
		return "", nil, false
	}
	var s string
	if json.Unmarshal(r.Content, &s) == nil {
		return s, nil, true
	}
	var parts []resultPart
	if json.Unmarshal(r.Content, &parts) == nil {
		return "", parts, false
	}
	return "", nil, false
}

// ResultText is a tool result's text, its text parts joined by newlines.
func ResultText(r *protocol.ToolResult) string {
	s, parts, isString := resultParts(r)
	if isString {
		return s
	}
	var texts []string
	for _, p := range parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// ResultImages are the pictures a tool handed back (a Read of an image file,
// a screenshot).
func ResultImages(r *protocol.ToolResult) []Image {
	_, parts, _ := resultParts(r)
	var out []Image
	for _, p := range parts {
		if p.Type == "image" && p.Source != nil && p.Source.Type == "base64" {
			out = append(out, Image{p.Source.MediaType, p.Source.Data})
		}
	}
	return out
}

// FormatTimestamp is a turn's time, with the day in front when it was not
// today, as Intl formats it for en-US (hour and minute two digits).
func FormatTimestamp(ms float64, now time.Time) string {
	t := time.UnixMilli(int64(ms)).In(now.Location())
	clock := t.Format("03:04 PM")
	if y, m, d := t.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return clock
	}
	return t.Format("Jan 2") + " " + clock
}

// TurnEnd is when a turn ended, and when the user's message that started it
// was sent (HasStart false when unknown).
type TurnEnd struct {
	EndedAt   float64
	StartedAt float64
	HasStart  bool
}

// TurnEnds are the assistant entries that close a turn, by id. A turn closes
// with a reply: an assistant entry whose last block is text, followed by a
// user message. One that stops on tool calls and is followed by a user
// message was steered mid-turn, not closed. The last entry closes once the
// Daemon rests, however it ends.
func TurnEnds(entries []*Entry, running bool) map[string]TurnEnd {
	ends := map[string]TurnEnd{}
	var start float64
	hasStart := false
	for i, e := range entries {
		if e.User {
			if !hasStart {
				start, hasStart = e.CreatedAt, true
			}
			continue
		}
		replied := len(e.Blocks) > 0 && e.Blocks[len(e.Blocks)-1].Kind == Text
		closes := !running
		if i+1 < len(entries) {
			closes = entries[i+1].User && replied
		}
		if !closes {
			continue
		}
		ends[e.ID] = TurnEnd{EndedAt: e.CreatedAt, StartedAt: start, HasStart: hasStart}
		hasStart = false
	}
	return ends
}

// FormatTurnEnd is `11:13 PM`, or `11:13 PM · took 4m 12s` when the turn's start is known.
func FormatTurnEnd(end TurnEnd, now time.Time) string {
	t := FormatTimestamp(end.EndedAt, now)
	if !end.HasStart {
		return t
	}
	if took := end.EndedAt - end.StartedAt; took >= 1000 {
		return t + " · took " + FormatDuration(took)
	}
	return t
}

// FormatDuration is `640 ms`, `12s`, `4m 12s`, `1h 03m`.
func FormatDuration(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%d ms", int(ms+0.5))
	}
	s := int(ms/1000 + 0.5)
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	m := s / 60
	if m < 60 {
		return fmt.Sprintf("%dm %ds", m, s%60)
	}
	return fmt.Sprintf("%dh %02dm", m/60, m%60)
}

var workingLabels = map[string]string{
	"idle":                          "",
	"thinking":                      "Thinking",
	"streaming_assistant_message":   "Responding",
	"waiting_for_tool_confirmation": "Waiting for your approval",
	"executing_tool":                "Running a tool",
	"compacting_conversation":       "Compacting",
}

// WorkingLabel is what the activity row under the transcript says for a working state.
func WorkingLabel(state string) string {
	if l, ok := workingLabels[state]; ok {
		return l
	}
	return state
}
