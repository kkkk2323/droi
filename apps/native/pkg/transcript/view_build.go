package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// BuildView makes the transcript of a Kernel view: Pi's entries in, the blocks the window draws out.
//
// It is the Kernel's counterpart of Build, which reads the Daemon's flat messages; the two live side by side
// while the window still draws from the Daemon, and shared/ci-debt.md records the work that removes the old one.
//
// One Pi entry is one model message, where the Daemon handed a turn's messages over as one array. So the folding
// the old path did inside a message happens here across entries: a user entry opens a turn and the answers after
// it join it, because one answer can span several entries — a tool round hands the run to a new generation.
func BuildView(view kernel.View) []*Entry {
	var b Builder
	return b.BuildView(view)
}

// BuildView is BuildView with the Builder's cache: an entry whose JSON did not change is not decoded again.
func (bl *Builder) BuildView(view kernel.View) []*Entry {
	if bl.view == nil {
		bl.view = map[string]*seenViewEntry{}
	}
	bl.viewGen++
	defer func() {
		for id, seen := range bl.view {
			if seen.gen != bl.viewGen {
				delete(bl.view, id)
			}
		}
	}()

	results := bl.toolResults(view)
	var entries []*Entry
	// boundary is set by the two entries that move the context on — a reset and a compaction — so the answer
	// after one opens its own row. Folding across them joined two answers from two contexts into one.
	boundary := false
	for _, entry := range view.Entries {
		switch entry.Kind {
		case kernel.EntryUser:
			// A user entry opens a turn: nothing folds it into what came before. A user message with nothing
			// to show is not a turn — a handoff or a write submission is written as one, and the model reads
			// it while the window shows nothing.
			blocks := onlyTextAndPictures(bl.blocksOf(entry, results, false))
			if len(blocks) == 0 {
				// A handoff or a write submission: the model reads it and the window shows nothing. It is still a
				// turn's start, so the answer after it must not fold into the turn before.
				boundary = true
				continue
			}
			entries = append(entries, &Entry{ID: entryID(entry), User: true, Blocks: blocks, CreatedAt: createdAt(entry)})
			boundary = false
		case kernel.EntryAssistant:
			blocks := bl.blocksOf(entry, results, true)
			failed, stopped := answerEnd(entry)
			if len(blocks) == 0 {
				// A refused request and a cancelled one can both end with no content at all. The turn did
				// happen, and it is the row that says so; skipping it left the question standing alone.
				text, ok := emptyAnswerSentence(entry, failed, stopped)
				if !ok {
					continue
				}
				blocks = []Block{{Kind: Text, ID: entryID(entry) + ":0:0", Text: text}}
			}
			if n := len(entries); n > 0 && !entries[n-1].User && !boundary {
				joinAnswer(entries[n-1], blocks, createdAt(entry), failed)
				boundary = false
				continue
			}
			entries = append(entries, &Entry{ID: entryID(entry), Blocks: blocks, CreatedAt: createdAt(entry), IsError: failed})
			boundary = false
		case kernel.EntryReset, kernel.EntryCompaction:
			// The summary a compaction writes is a message, and a reset's handoff may carry one; neither is
			// something the user said. They mark where the context moved and draw no row of this PR: their own
			// cards (Block kinds Compaction and the reset marker) come with the next one, which the plan names.
			boundary = true
		case kernel.EntryNote:
			block, ok := noteBlock(entry)
			if !ok {
				continue
			}
			// A note is its own row and nothing answers it: an answer after it opens a new row rather than joining
			// the note's.
			entries = append(entries, &Entry{ID: entryID(entry), Note: true, Blocks: []Block{block}, CreatedAt: createdAt(entry)})
			boundary = true
		}
		// Every other kind shows nothing here: a tool result completes a call, a positional system entry is
		// the prompt rather than the conversation, and Droi's bookkeeping entries are not lines of a transcript.
	}
	return entries
}

// answerEnd reads how an answer ended: a failure, and a turn the user stopped.
//
// Both are read from the last message that says anything, because Pi writes one message per entry today but the
// contract allows several: the last stop reason is the one that ended the answer.
func answerEnd(e kernel.Entry) (failed, stopped bool) {
	msgs, _ := decodeMessages(e.Model)
	for i := len(msgs) - 1; i >= 0; i-- {
		switch msgs[i].StopReason {
		case "error":
			return true, false
		case "aborted":
			return false, true
		case "stop", "toolUse", "length":
			return false, false
		}
		if msgs[i].ErrorMessage != "" {
			return true, false
		}
	}
	return false, false
}

// emptyAnswerSentence is what a row says when an answer ended with nothing in it: the provider's own sentence, a
// sentence for a refused request that gave none, or the word for a turn the user stopped. ok is false for an
// answer that simply had nothing to say.
func emptyAnswerSentence(e kernel.Entry, failed, stopped bool) (string, bool) {
	msgs, _ := decodeMessages(e.Model)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].ErrorMessage != "" {
			return msgs[i].ErrorMessage, true
		}
	}
	switch {
	case failed:
		return l10n.L("The model could not answer this turn."), true
	case stopped:
		return l10n.L("Stopped"), true
	}
	return "", false
}

// noteBlock is the row a droi.note draws: a line of text the Host or the Kernel left in the transcript.
func noteBlock(e kernel.Entry) (Block, bool) {
	var data struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(e.Data, &data) != nil {
		return Block{}, false
	}
	if data.Text == "" {
		return Block{}, false
	}
	return Block{Kind: Text, ID: entryID(e) + ":0:0", Text: data.Text}, true
}

// onlyTextAndPictures is the blocks a user entry can carry: the message the user sent, not the answer side.
func onlyTextAndPictures(blocks []Block) []Block {
	out := blocks[:0]
	for _, b := range blocks {
		if b.Kind == Text || b.Kind == Picture {
			out = append(out, b)
		}
	}
	return out
}

// joinAnswer folds an answer entry into the turn it belongs to: adjacent clusters join into one, as the window
// draws a run of calls as one cluster, and the entry's time becomes the last message's.
func joinAnswer(target *Entry, blocks []Block, at float64, failed bool) {
	for _, b := range blocks {
		if k := len(target.Blocks); b.Kind == Tools && k > 0 && target.Blocks[k-1].Kind == Tools {
			target.Blocks[k-1].Calls = append(target.Blocks[k-1].Calls, b.Calls...)
			continue
		}
		target.Blocks = append(target.Blocks, b)
	}
	target.CreatedAt = at
	target.IsError = target.IsError || failed
}

func entryID(e kernel.Entry) string { return fmt.Sprintf("%d", e.ID) }

// createdAt is when the entry happened: its last message's own timestamp, which is what Pi stores.
func createdAt(e kernel.Entry) float64 {
	msgs, _ := decodeMessages(e.Model)
	if n := len(msgs); n > 0 {
		return msgs[n-1].Timestamp
	}
	// A droi entry carries no messages, so its own data says when it happened: `at`, which the Host and the Kernel
	// write for the rows that need a place in time.
	var data struct {
		At float64 `json:"at"`
	}
	_ = json.Unmarshal(e.Data, &data)
	return data.At
}

// toolResults reads every tool-result entry's result, by the call it completes: Pi writes the result as its own
// entry, after the answer that asked for the call.
func (bl *Builder) toolResults(view kernel.View) map[string]*ToolResultView {
	out := map[string]*ToolResultView{}
	for _, entry := range view.Entries {
		if entry.Kind != kernel.EntryToolResult {
			continue
		}
		diagnostics := diagnosticsOf(entry)
		for _, m := range bl.messagesOf(entry) {
			if m.Role == "toolResult" && m.ToolCallID != "" {
				m.Diagnostics = diagnostics
				out[m.ToolCallID] = m.resultView()
			}
		}
	}
	return out
}

// diagnosticsOf reads a tool-result entry's diagnostics: `data.diagnostics`, the structured list whose rendering
// closes the message the model reads.
func diagnosticsOf(entry kernel.Entry) []piDiagnostic {
	var data struct {
		Diagnostics []piDiagnostic `json:"diagnostics"`
	}
	if len(entry.Data) == 0 || json.Unmarshal(entry.Data, &data) != nil {
		return nil
	}
	return data.Diagnostics
}

// blocksOf decodes one entry's messages into blocks. answer says the entry is the assistant's, the only side whose
// tool calls, thinking and failed stop are drawn.
func (bl *Builder) blocksOf(entry kernel.Entry, results map[string]*ToolResultView, answer bool) []Block {
	var blocks []Block
	id := entryID(entry)
	for mi, m := range bl.messagesOf(entry) {
		for i, c := range m.Blocks {
			// The identity carries the message as well as the block: one entry may hold several messages, and
			// the rows key a picture's decoded bitmap by it, so two pictures with one ID drew one another.
			blockID := fmt.Sprintf("%s:%d:%d", id, mi, i)
			switch {
			case m.Role == "user" && c.Type == "text" && c.Text != "":
				blocks = append(blocks, Block{Kind: Text, ID: blockID, Text: c.Text})
			case m.Role == "user" && c.Type == "image":
				blocks = append(blocks, Block{Kind: Picture, ID: blockID, Image: Image{MediaType: c.MimeType, Data: c.Data}})
			case !answer:
				continue
			case c.Type == "text" && c.Text != "":
				blocks = append(blocks, Block{Kind: Text, ID: blockID, Text: c.Text})
			case c.Type == "thinking" && c.Thinking != "":
				blocks = append(blocks, Block{Kind: Thinking, ID: blockID, Text: c.Thinking})
			case c.Type == "toolCall":
				// The call's identity is the same shape the Daemon's calls take, so the rows that draw them
				// need no branch; the field is the SDK's type until the Daemon's path goes (M5).
				call := &ToolCall{Use: protocol.ToolUse{Type: "tool_use", ID: c.ID, Name: c.Name},
					Input: decodeInputObject(c.Arguments), RawInput: c.Arguments, Keys: orderedKeys(c.Arguments),
					ResultView: results[c.ID]}
				if call.ResultView == nil {
					if result := interruptedResult(entry); result != nil {
						call.ResultView = result
					}
				}
				if n := len(blocks); n > 0 && blocks[n-1].Kind == Tools {
					blocks[n-1].Calls = append(blocks[n-1].Calls, call)
					continue
				}
				blocks = append(blocks, Block{Kind: Tools, ID: blockID, Calls: []*ToolCall{call}})
			}
		}
	}
	return blocks
}

// messagesOf is the entry's messages decoded, those unchanged since the last build as it left them.
func (bl *Builder) messagesOf(entry kernel.Entry) []piMessage {
	id := entryID(entry)
	seen := bl.view[id]
	if seen == nil {
		seen = &seenViewEntry{}
		bl.view[id] = seen
	} else if seen.gen == bl.viewGen {
		return seen.messages
	}
	seen.gen = bl.viewGen
	if !bytes.Equal(seen.raw, entry.Model) {
		seen.raw = append(seen.raw[:0], entry.Model...)
		seen.messages, _ = decodeMessages(entry.Model)
	}
	return seen.messages
}

// seenViewEntry is one entry's messages as the last build decoded them.
type seenViewEntry struct {
	gen      uint64
	raw      json.RawMessage
	messages []piMessage
}

// piMessage is the part of Pi's Message this package reads; Content is either a string or blocks.
type piMessage struct {
	Role         string
	Timestamp    float64
	StopReason   string
	ErrorMessage string
	ToolCallID   string
	IsError      bool
	Text         string
	Blocks       []piContent
	// Diagnostics are a tool result's structured findings, from the entry's data: the harness reports a stopped
	// run there rather than in the result's shape.
	Diagnostics []piDiagnostic
}

// piDiagnostic is one finding of a tool result: {severity, code, message}.
type piDiagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// piContent is one block of a Pi message.
type piContent struct {
	Type      string
	Text      string
	Thinking  string
	ID        string
	Name      string
	Arguments json.RawMessage
	Data      string
	MimeType  string
}

// decodeMessages reads a Pi message list. A malformed list is no messages: the window draws nothing rather than
// failing to draw at all.
func decodeMessages(raw json.RawMessage) ([]piMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var wire []struct {
		Role         string          `json:"role"`
		Content      json.RawMessage `json:"content"`
		Timestamp    float64         `json:"timestamp"`
		StopReason   string          `json:"stopReason"`
		ErrorMessage string          `json:"errorMessage"`
		ToolCallID   string          `json:"toolCallId"`
		IsError      bool            `json:"isError"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, err
	}
	out := make([]piMessage, 0, len(wire))
	for _, w := range wire {
		m := piMessage{Role: w.Role, Timestamp: w.Timestamp, StopReason: w.StopReason,
			ErrorMessage: w.ErrorMessage, ToolCallID: w.ToolCallID, IsError: w.IsError}
		if len(w.Content) > 0 && w.Content[0] == '"' {
			// A message's content is a bare string whenever the sender had only text: the Host submits
			// `input:"hello"`, Pi stores it as `content:"hello"`, and it is the ordinary shape of a user message,
			// not an edge case. It becomes the one text block the list form would have carried.
			_ = json.Unmarshal(w.Content, &m.Text)
			if m.Text != "" {
				m.Blocks = []piContent{{Type: "text", Text: m.Text}}
			}
			out = append(out, m)
			continue
		}
		m.Blocks = decodeContent(w.Content)
		out = append(out, m)
	}
	return out, nil
}

// decodeContent reads a message's blocks.
func decodeContent(raw json.RawMessage) []piContent {
	if len(raw) == 0 {
		return nil
	}
	var wire []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Thinking  string          `json:"thinking"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Data      string          `json:"data"`
		MimeType  string          `json:"mimeType"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return nil
	}
	out := make([]piContent, len(wire))
	for i, w := range wire {
		out[i] = piContent{Type: w.Type, Text: w.Text, Thinking: w.Thinking, ID: w.ID, Name: w.Name,
			Arguments: w.Arguments, Data: w.Data, MimeType: w.MimeType}
	}
	return out
}

// interruptedResult is what a call on a stopped answer reads: the turn was cancelled before its result arrived,
// so the call did not run to an answer. Without this the row says "Running" for ever — a window that never rests.
// A call whose answer merely stopped on tool calls has not finished either, and keeps waiting: that run is live.
func interruptedResult(answer kernel.Entry) *ToolResultView {
	if _, stopped := answerEnd(answer); !stopped {
		return nil
	}
	return &ToolResultView{Text: l10n.L("Interrupted"), Interrupted: true}
}

// resultView is a tool-result message as the row reads it.
//
// Pi closes a result's content with its diagnostics rendered for the model — one last text item,
// `<harness>\n[severity] message\n…\n</harness>` — and keeps the same list structured in the entry's data. The row
// shows the output, then each diagnostic's message from the structured list: a truncation, the path of the full
// output, an exit code or why the call did not run are all worth reading, the wrapper is not.
//
// A call the harness ended itself comes back as an error result whose diagnostic code says why, and that code is
// the whole row: a call that was stopped, or refused, did not fail, and a red row tells the user something broke.
func (m piMessage) resultView() *ToolResultView {
	v := &ToolResultView{}
	var output []string
	for _, c := range m.Blocks {
		switch c.Type {
		case "text":
			if c.Text != "" {
				output = append(output, c.Text)
			}
		case "image":
			v.Images = append(v.Images, Image{MediaType: c.MimeType, Data: c.Data})
		}
	}
	if n := len(output); n > 0 && output[n-1] == renderedDiagnostics(m.Diagnostics) {
		// Pi renders the diagnostics as the content's last item exactly when there are some (appendToolResult).
		// Only that rendering goes: an item that merely follows them, as an imported history may have, is output.
		output = output[:n-1]
	}
	for _, line := range output {
		v.Text = joinLine(v.Text, "\n", line)
	}
	v.Text = withoutTrailingNewlines(v.Text)
	for _, d := range m.Diagnostics {
		if d.Message != "" {
			v.Notes = joinLine(v.Notes, "\n", d.Message)
		}
	}
	switch m.harnessEnding() {
	case endingStopped:
		v.Interrupted = true
	case endingRefused:
		v.Blocked = true
	default:
		v.IsError = m.IsError
	}
	return v
}

// renderedDiagnostics is Pi's rendering of diagnostics for the model (renderDiagnostics in harness/tool.js), ""
// for none.
func renderedDiagnostics(ds []piDiagnostic) string {
	if len(ds) == 0 {
		return ""
	}
	lines := ""
	for i, d := range ds {
		line := fmt.Sprintf("[%s] %s", d.Severity, d.Message)
		if i == 0 {
			lines = line
			continue
		}
		lines += "\n" + line
	}
	return "<harness>\n" + lines + "\n</harness>"
}

// withoutTrailingNewlines is text without the line ends a command's output closes with.
func withoutTrailingNewlines(text string) string {
	for len(text) > 0 && (text[len(text)-1] == '\n' || text[len(text)-1] == '\r') {
		text = text[:len(text)-1]
	}
	return text
}

// joinLine is text with more after it, sep between the two when both have something.
func joinLine(text, sep, more string) string {
	switch {
	case more == "":
		return text
	case text == "":
		return more
	}
	return text + sep + more
}

// harnessEnding is how the harness ended a call it did not let finish, read from its diagnostic codes (Pi's
// harness/tool.js writes them).
type harnessEnding int

const (
	endingNone harnessEnding = iota
	// endingStopped: the run was stopped (aborted), a restart cut the call off (interrupted), or its caller ended
	// before it started (abandoned). None of these is the tool failing.
	endingStopped
	// endingRefused: a rule or a person refused the call before it ran (blocked).
	endingRefused
)

// harnessEnding reads the result's last diagnostic: the harness appends its own ending after whatever the tool
// reported, so a tool's diagnostic that happens to share a code cannot turn a failure into a stop. A call that kept
// crashing its process (restart_limit) is not among the endings: that one failed, and the row says so.
func (m piMessage) harnessEnding() harnessEnding {
	if len(m.Diagnostics) == 0 {
		return endingNone
	}
	switch m.Diagnostics[len(m.Diagnostics)-1].Code {
	case "aborted", "interrupted", "abandoned":
		return endingStopped
	case "blocked":
		return endingRefused
	}
	return endingNone
}

// decodeInputObject reads a tool call's arguments, for the row's summary.
func decodeInputObject(raw json.RawMessage) map[string]any {
	var fields map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &fields) != nil {
		return map[string]any{}
	}
	return decodeInput(fields)
}
