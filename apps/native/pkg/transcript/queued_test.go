package transcript

import (
	"encoding/json"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/protocol"
)

func userOnly(m *protocol.FactoryDroidMessage) { m.Visibility = protocol.MessageVisibilityUserOnly }

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestParseQueuedRecords(t *testing.T) {
	got := ParseQueuedRecords("Answered queued questions (AskUser call call_1):\n" +
		`Q: "Which environment?"` + "\n" + `A: "staging"` + "\n" +
		`Q: "Two\nlines?"` + "\n" + `A: "yes, \"quoted\""` + "\n" +
		"Q: not json\nA: \"lost\"\n" +
		"Dismissed queued questions (AskUser call call_2): the user closed them without answering; their answers will not arrive.\n" +
		`Q: "Ship on Friday?"`)
	eq(t, got, []QueuedRecord{
		{ToolCallID: "call_1", Answers: []QueuedAnswer{{"Which environment?", "staging"}, {"Two\nlines?", `yes, "quoted"`}}},
		{ToolCallID: "call_2", Dismissed: true, Answers: []QueuedAnswer{{Question: "Ship on Friday?"}}},
	})
	if got := ParseQueuedRecords("Q: \"orphan\"\nA: \"answer\""); got != nil {
		t.Fatalf("lines before a header: %#v", got)
	}
}

// The receipt is not shown; the records that follow each other are one block
// of a user entry; a record that does not parse reads as the text it is.
func TestQueuedAskUserRecordsInTheTranscript(t *testing.T) {
	answered := "Answered queued questions (AskUser call call_1):\nQ: \"Which environment?\"\nA: \"staging\""
	dismissed := "Dismissed queued questions (AskUser call call_2): the user closed them without answering; their answers will not arrive.\nQ: \"Ship on Friday?\""
	entries := Build([]protocol.FactoryDroidMessage{
		msg("user", `[{"type":"text","text":"Deploy it"}]`),
		msg("assistant", `[{"type":"tool_use","id":"call_1","name":"AskUser","input":{"questionnaire":"1. [question] Which environment?","blocking":false,"context":"The deploy needs a target.\nIt can wait."}},{"type":"tool_use","id":"call_2","name":"AskUser","input":{"blocking":false}},{"type":"tool_use","id":"call_3","name":"AskUser","input":{"blocking":false}},{"type":"tool_use","id":"call_4","name":"AskUser","input":{"questionnaire":"1. [question] Now?"}}]`),
		msg("tool", `[{"type":"tool_result","toolUseId":"call_1","content":"Queued."},{"type":"tool_result","toolUseId":"call_2","content":"Queued."},{"type":"tool_result","toolUseId":"call_3","content":"Queued."},{"type":"tool_result","toolUseId":"call_4","content":"1. Now? → yes"}]`),
		msg("system", `[{"type":"text","text":`+quote(answered)+`}]`, withID("ask-user-receipt:call_1"), userOnly),
		msg("user", `[{"type":"text","text":`+quote(answered)+`}]`, withID("ask-user-answer:call_1"), at(5)),
		msg("user", `[{"type":"text","text":`+quote(dismissed)+`}]`, withID("ask-user-answer:call_2"), at(6)),
		msg("assistant", `[{"type":"text","text":"Noted."}]`),
		msg("user", `[{"type":"text","text":"not a record"}]`, withID("ask-user-answer:call_9")),
	})
	eq(t, roles(entries), []string{"user", "assistant", "user", "assistant", "user"})
	rec := entries[2]
	if rec.ID != "ask-user-answer:call_1" || rec.CreatedAt != 6 || len(rec.Blocks) != 1 || rec.Blocks[0].Kind != QueuedAnswers {
		t.Fatalf("record entry %#v", rec)
	}
	eq(t, rec.Blocks[0].Records, []QueuedRecord{
		{ToolCallID: "call_1", Answers: []QueuedAnswer{{"Which environment?", "staging"}}},
		{ToolCallID: "call_2", Dismissed: true, Answers: []QueuedAnswer{{Question: "Ship on Friday?"}}},
	})
	if b := entries[4].Blocks; len(b) != 1 || b[0].Kind != Text || b[0].Text != "not a record" {
		t.Fatalf("an unreadable record: %#v", b)
	}
	calls := entries[1].Blocks[0].Calls
	states := []QueuedState{}
	for _, c := range calls {
		states = append(states, c.Queued)
	}
	eq(t, states, []QueuedState{QueuedAnswered, QueuedDismissed, QueuedWaiting, NotQueued})
	eq(t, Summary(calls[0]), "answered · The deploy needs a target.")
	eq(t, Summary(calls[2]), "waiting for your answer")
}

// A receipt marks its call answered before the record arrives.
func TestAReceiptResolvesItsCall(t *testing.T) {
	answered := "Answered queued questions (AskUser call call_1):\nQ: \"Which?\"\nA: \"this\""
	entries := Build([]protocol.FactoryDroidMessage{
		msg("assistant", `[{"type":"tool_use","id":"call_1","name":"AskUser","input":{"blocking":false}}]`),
		msg("tool", `[{"type":"tool_result","toolUseId":"call_1","content":"Queued."}]`),
		msg("system", `[{"type":"text","text":`+quote(answered)+`}]`, withID("ask-user-receipt:call_1"), userOnly),
	})
	eq(t, roles(entries), []string{"assistant"})
	if got := entries[0].Blocks[0].Calls[0].Queued; got != QueuedAnswered {
		t.Fatalf("state %v", got)
	}
}

func TestQueuedSummary(t *testing.T) {
	recs := []QueuedRecord{{Answers: []QueuedAnswer{{"Which?", "this"}}}, {Dismissed: true, Answers: []QueuedAnswer{{Question: "Now?"}}}}
	eq(t, QueuedSummary(recs), "Answered queued questions: Which? → this; Now?")
	eq(t, QueuedTitle(recs[1:]), "Dismissed queued questions")
}
