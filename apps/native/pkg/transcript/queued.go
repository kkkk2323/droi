package transcript

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droid-sdk-go/protocol"
)

// AskUserTool asks the user questions. With blocking false (droid 1.1) it
// queues them: the call returns at once, the agent keeps working, and the
// user's answers, or their dismissal, come back as two messages named after
// the call. The receipt is user-only and saved as they arrive; no transcript
// shows it. The record is the user message the agent reads.
const (
	AskUserTool          = "AskUser"
	askUserAnswerPrefix  = "ask-user-answer:"
	askUserReceiptPrefix = "ask-user-receipt:"
)

// QueuedState is where a queued AskUser call stands.
type QueuedState uint8

const (
	NotQueued QueuedState = iota
	QueuedWaiting
	QueuedAnswered
	QueuedDismissed
)

// QueuedAnswer is a question of a queued set and the user's answer to it,
// "" when the set was dismissed.
type QueuedAnswer struct{ Question, Answer string }

// QueuedRecord is the record of a queued set the user answered or dismissed.
type QueuedRecord struct {
	ToolCallID string
	Dismissed  bool
	Answers    []QueuedAnswer
}

var recordHeader = regexp.MustCompile(`^(Answered|Dismissed) queued questions \(AskUser call ([^)]+)\):`)

// ParseQueuedRecords reads the records in a record message's text as droid
// writes them: a header naming the call, then a `Q: ` line per question,
// each followed by an `A: ` line when answered, both JSON strings. A line
// that does not parse is skipped, as the text can come from other writers.
func ParseQueuedRecords(text string) []QueuedRecord {
	var out []QueuedRecord
	var question *string
	for _, line := range strings.Split(text, "\n") {
		if m := recordHeader.FindStringSubmatch(line); m != nil {
			out = append(out, QueuedRecord{ToolCallID: m[2], Dismissed: m[1] == "Dismissed"})
			question = nil
			continue
		}
		if len(out) == 0 {
			continue
		}
		r := &out[len(out)-1]
		switch {
		case strings.HasPrefix(line, "Q: "):
			question = nil
			if q, ok := quotedLine(line[3:]); ok {
				question = &q
				if r.Dismissed {
					r.Answers = append(r.Answers, QueuedAnswer{Question: q})
				}
			}
		case strings.HasPrefix(line, "A: ") && !r.Dismissed && question != nil:
			if a, ok := quotedLine(line[3:]); ok {
				r.Answers = append(r.Answers, QueuedAnswer{Question: *question, Answer: a})
			}
		}
	}
	return out
}

func quotedLine(s string) (string, bool) {
	var v string
	return v, json.Unmarshal([]byte(s), &v) == nil
}

// QueuedSummary is a record block on one line, for a list of messages.
func QueuedSummary(records []QueuedRecord) string {
	var parts []string
	for _, r := range records {
		for _, a := range r.Answers {
			if r.Dismissed {
				parts = append(parts, a.Question)
			} else {
				parts = append(parts, a.Question+" → "+a.Answer)
			}
		}
	}
	return QueuedTitle(records) + ": " + strings.Join(parts, "; ")
}

// QueuedTitle is a record block's title: answered when any set in it was.
func QueuedTitle(records []QueuedRecord) string {
	for _, r := range records {
		if !r.Dismissed {
			return l10n.L("Answered queued questions")
		}
	}
	return l10n.L("Dismissed queued questions")
}

// queuedRecords are the record messages' records by message id, and how each
// queued call they or a receipt name was resolved, by tool use id. A receipt
// resolves its call before the record follows.
func queuedRecords(messages []protocol.FactoryDroidMessage) (map[string][]QueuedRecord, map[string]QueuedState) {
	var records map[string][]QueuedRecord
	var states map[string]QueuedState
	for _, m := range messages {
		call, isRecord := strings.CutPrefix(m.ID, askUserAnswerPrefix)
		if !isRecord {
			var isReceipt bool
			if call, isReceipt = strings.CutPrefix(m.ID, askUserReceiptPrefix); !isReceipt {
				continue
			}
		}
		recs := ParseQueuedRecords(plainText(m))
		if states == nil {
			records, states = map[string][]QueuedRecord{}, map[string]QueuedState{}
		}
		state := QueuedAnswered
		if len(recs) > 0 && recs[0].Dismissed {
			state = QueuedDismissed
		}
		states[call] = state
		if isRecord && len(recs) > 0 {
			records[m.ID] = recs
		}
	}
	return records, states
}

func isReceipt(m protocol.FactoryDroidMessage) bool {
	return strings.HasPrefix(m.ID, askUserReceiptPrefix)
}

func plainText(m protocol.FactoryDroidMessage) string {
	var parts []string
	for _, b := range m.Content {
		if b.Type != protocol.ContentBlockTypeText {
			continue
		}
		if t, ok := decode[protocol.TextBlock](b); ok {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// queuedStateOf is where call stands when it queued questions; NotQueued
// for any other call, and while a queued one has no result yet.
func queuedStateOf(call *ToolCall, resolved map[string]QueuedState) QueuedState {
	if call.Use.Name != AskUserTool {
		return NotQueued
	}
	if blocking, ok := call.Input["blocking"].(bool); !ok || blocking {
		return NotQueued
	}
	if s := resolved[call.Use.ID]; s != NotQueued {
		return s
	}
	if call.Result == nil || call.Result.IsError != nil && *call.Result.IsError {
		return NotQueued
	}
	return QueuedWaiting
}

var queuedLabels = map[QueuedState]string{
	QueuedWaiting:   l10n.N("waiting for your answer"),
	QueuedAnswered:  l10n.N("answered"),
	QueuedDismissed: l10n.N("dismissed"),
}

// queuedParts is what a queued AskUser call's row says after its name:
// where it stands, then its context.
func queuedParts(c *ToolCall) []SummaryPart {
	parts := []SummaryPart{{Value: l10n.T(queuedLabels[c.Queued])}}
	if ctx, _ := c.Input["context"].(string); strings.TrimSpace(ctx) != "" {
		parts = append(parts, SummaryPart{Value: firstLine(strings.TrimSpace(ctx), 120)})
	}
	return parts
}
