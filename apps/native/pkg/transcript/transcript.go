// Package transcript turns a Session's flat message list into what the
// transcript renders: user turns, assistant turns whose tool calls carry
// their results, and nothing for bare tool-result messages, which only exist
// to complete a tool call.
package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"
)

// The tool that starts a subagent, the one that runs a program calling other
// tools, the one that waits on such a run past its first minute, and the one
// that ends Spec mode with a plan for the user to approve.
const (
	TaskTool          = "Task"
	ScriptTool        = "Script"
	WaitForScriptTool = "WaitForScript"
	SpecTool          = "ExitSpecMode"
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
	// An ExitSpecMode call: the plan Spec mode made, shown on its own.
	Spec
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
	input, rawInput, keys := callInput(b, use)
	return &ToolCall{Use: use, Input: input, RawInput: rawInput, Keys: keys, Result: result}
}

// callInput is a tool use's input decoded, as the Daemon sent it, and its
// keys in that order.
func callInput(b protocol.ContentBlock, use protocol.ToolUse) (map[string]any, json.RawMessage, []string) {
	var raw struct {
		Input json.RawMessage `json:"input"`
	}
	_ = json.Unmarshal(b.Raw, &raw)
	if len(raw.Input) == 0 || raw.Input[0] != '{' {
		raw.Input = json.RawMessage("{}")
	}
	return decodeInput(use.Input), raw.Input, orderedKeys(raw.Input)
}

func decode[T any](b protocol.ContentBlock) (T, bool) {
	var v T
	return v, json.Unmarshal(b.Raw, &v) == nil
}

// Build makes the transcript of the messages a Session shows.
func Build(messages []protocol.FactoryDroidMessage) []*Entry {
	var b Builder
	return b.Build(messages)
}

// LatestTool is the last tool call among messages and how many tool calls
// the assistant made, as a subagent's progress reads them. Only the last
// call is decoded.
func LatestTool(messages []protocol.FactoryDroidMessage) (*ToolCall, int) {
	var last protocol.ContentBlock
	n := 0
	for _, m := range messages {
		if m.Role != protocol.MessageRoleAssistant {
			continue
		}
		for _, b := range m.Content {
			if b.Type == protocol.ContentBlockTypeToolUse {
				n++
				last = b
			}
		}
	}
	if n == 0 {
		return nil, 0
	}
	use, ok := decode[protocol.ToolUse](last)
	if !ok {
		return nil, n
	}
	return newCall(last, use, nil), n
}

// Builder makes a Session's transcript again each time its messages change,
// decoding each content block once: while a turn streams only its last
// messages change, and a page of older messages leaves the others as they
// were. Decoding every block of a long Session again on each change was
// most of what a working Session's window allocated.
type Builder struct {
	messages map[string]*seenMessage
	gen      uint64
}

// seenMessage is a message's blocks as the last Build decoded them.
type seenMessage struct {
	gen    uint64
	blocks []*decodedBlock
}

// decodedBlock is what Build reads from one content block, kept while the
// block's JSON stays the same. Builds share its values, which no one changes.
type decodedBlock struct {
	typ, id    string
	raw        json.RawMessage
	ok         bool
	text       string
	streaming  bool
	durationMs *float64
	image      Image
	use        protocol.ToolUse
	input      map[string]any
	rawInput   json.RawMessage
	keys       []string
	result     *protocol.ToolResult
}

func decodeBlock(b protocol.ContentBlock, id string) *decodedBlock {
	d := &decodedBlock{typ: b.Type, id: id, raw: b.Raw}
	switch b.Type {
	case protocol.ContentBlockTypeText:
		t, _ := decode[protocol.TextBlock](b)
		d.text, d.ok = t.Text, t.Text != ""
		d.streaming = d.ok && session.IsStreaming(b)
	case protocol.ContentBlockTypeImage:
		im, _ := decode[protocol.ImageBlock](b)
		d.image, d.ok = Image{string(im.Source.MediaType), im.Source.Data}, im.Source.Type == "base64"
	case protocol.ContentBlockTypeThinking:
		t, _ := decode[protocol.ThinkingBlock](b)
		d.text, d.durationMs, d.ok = t.Thinking, t.DurationMs, t.Thinking != ""
		d.streaming = d.ok && session.IsStreaming(b)
	case protocol.ContentBlockTypeToolUse:
		d.use, d.ok = decode[protocol.ToolUse](b)
		if d.ok {
			d.input, d.rawInput, d.keys = callInput(b, d.use)
		}
	case protocol.ContentBlockTypeToolResult:
		var r protocol.ToolResult
		r, d.ok = decode[protocol.ToolResult](b)
		d.result = &r
	}
	return d
}

// blocks are message m's blocks decoded, those unchanged since the last
// Build as it left them.
func (bl *Builder) blocks(m protocol.FactoryDroidMessage) []*decodedBlock {
	seen := bl.messages[m.ID]
	// A message without an id, or with one twice in a list, is decoded anew.
	if m.ID == "" || seen != nil && seen.gen == bl.gen {
		out := make([]*decodedBlock, len(m.Content))
		for i, b := range m.Content {
			out[i] = decodeBlock(b, fmt.Sprintf("%s:%d", m.ID, i))
		}
		return out
	}
	if seen == nil {
		seen = &seenMessage{}
		bl.messages[m.ID] = seen
	}
	seen.gen = bl.gen
	if len(seen.blocks) > len(m.Content) {
		seen.blocks = seen.blocks[:len(m.Content)]
	}
	for len(seen.blocks) < len(m.Content) {
		seen.blocks = append(seen.blocks, nil)
	}
	for i, b := range m.Content {
		if d := seen.blocks[i]; d == nil || d.typ != b.Type || !bytes.Equal(d.raw, b.Raw) {
			seen.blocks[i] = decodeBlock(b, fmt.Sprintf("%s:%d", m.ID, i))
		}
	}
	return seen.blocks
}

// Build makes the transcript of the messages a Session shows, as the
// package's Build does.
func (bl *Builder) Build(messages []protocol.FactoryDroidMessage) []*Entry {
	if bl.messages == nil {
		bl.messages = map[string]*seenMessage{}
	}
	bl.gen++
	defer func() {
		for id, s := range bl.messages {
			if s.gen != bl.gen {
				delete(bl.messages, id)
			}
		}
	}()

	results := map[string]*protocol.ToolResult{}
	for _, m := range messages {
		if m.Role != protocol.MessageRoleTool {
			continue
		}
		for _, d := range bl.blocks(m) {
			if d.typ == protocol.ContentBlockTypeToolResult && d.ok {
				results[d.result.ToolUseID] = d.result
			}
		}
	}

	// A call a Script made goes under the latest call watching its run, so the
	// calls a WaitForScript saw show where that wait sits in the turn.
	watching := map[string]*ToolCall{}
	var entries []*Entry
	for _, m := range messages {
		role := m.Role
		// A user-only message is the Daemon telling the user something (a
		// usage limit, a model swap): a system message live, a user message
		// once loaded. It reads as the Droid's, as in Factory's own app.
		if m.Visibility == protocol.MessageVisibilityUserOnly && (role == protocol.MessageRoleSystem || role == protocol.MessageRoleUser) {
			role = protocol.MessageRoleAssistant
		}
		if role != protocol.MessageRoleUser && role != protocol.MessageRoleAssistant {
			continue
		}
		if m.IsUserVisible != nil && !*m.IsUserVisible {
			continue
		}
		var blocks []Block
		for _, d := range bl.blocks(m) {
			if !d.ok {
				continue
			}
			switch d.typ {
			case protocol.ContentBlockTypeText:
				blocks = append(blocks, Block{Kind: Text, ID: d.id, Text: d.text, Streaming: d.streaming})
			case protocol.ContentBlockTypeImage:
				blocks = append(blocks, Block{Kind: Picture, ID: d.id, Image: d.image})
			case protocol.ContentBlockTypeThinking:
				blocks = append(blocks, Block{Kind: Thinking, ID: d.id, Text: d.text, DurationMs: d.durationMs, Streaming: d.streaming})
			case protocol.ContentBlockTypeToolUse:
				use := d.use
				call := &ToolCall{Use: use, Input: d.input, RawInput: d.rawInput, Keys: d.keys, Result: results[use.ID]}
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
					blocks = append(blocks, Block{Kind: Subagent, ID: d.id, Call: call})
					continue
				}
				if use.Name == SpecTool {
					blocks = append(blocks, Block{Kind: Spec, ID: d.id, Call: call})
					continue
				}
				if n := len(blocks); n > 0 && blocks[n-1].Kind == Tools {
					blocks[n-1].Calls = append(blocks[n-1].Calls, call)
				} else {
					blocks = append(blocks, Block{Kind: Tools, ID: d.id, Calls: []*ToolCall{call}})
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
		user := role == protocol.MessageRoleUser
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
// today, as Intl formats it for en-US (hour and minute two digits). The two
// layouts are catalog keys, so a language can give its own order and clock.
func FormatTimestamp(ms float64, now time.Time) string {
	t := time.UnixMilli(int64(ms)).In(now.Location())
	clock := t.Format(l10n.L("03:04 PM"))
	if y, m, d := t.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return clock
	}
	return t.Format(l10n.L("Jan 2")) + " " + clock
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

// FormatTurnEnd is `11:13 PM`, or `11:13 PM · 4m 12s` when the turn's start is known.
func FormatTurnEnd(end TurnEnd, now time.Time) string {
	t := FormatTimestamp(end.EndedAt, now)
	if !end.HasStart {
		return t
	}
	if took := end.EndedAt - end.StartedAt; took >= 1000 {
		return t + " · " + FormatDuration(took)
	}
	return t
}

// ReplyText is what the assistant wrote in a turn's entries, for Copy: the
// text blocks a blank line apart, without thinking, tool calls or the
// user's messages.
func ReplyText(entries []*Entry) string {
	var parts []string
	for _, e := range entries {
		if e.User {
			continue
		}
		for _, b := range e.Blocks {
			if s := strings.TrimSpace(b.Text); b.Kind == Text && s != "" {
				parts = append(parts, s)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// FormatDuration is `640 ms`, `12s`, `4m 12s`, `1h 03m`.
func FormatDuration(ms float64) string {
	if ms < 1000 {
		return l10n.L("%d ms", int(ms+0.5))
	}
	s := int(ms/1000 + 0.5)
	if s < 60 {
		return l10n.L("%ds", s)
	}
	m := s / 60
	if m < 60 {
		return l10n.L("%dm %ds", m, s%60)
	}
	return l10n.L("%dh %02dm", m/60, m%60)
}

var workingLabels = map[string]string{
	"idle":                          "",
	"thinking":                      l10n.N("Thinking"),
	"streaming_assistant_message":   l10n.N("Responding"),
	"waiting_for_tool_confirmation": l10n.N("Waiting for your approval"),
	"executing_tool":                l10n.N("Running a tool"),
	"compacting_conversation":       l10n.N("Compacting"),
}

// WorkingLabel is what the activity row under the transcript says for a working state.
func WorkingLabel(state string) string {
	if l, ok := workingLabels[state]; ok {
		return l10n.T(l)
	}
	return state
}
