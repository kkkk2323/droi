package transcript_test

import (
	"encoding/json"
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/transcript"
)

// liveDoc is pi.live while the model writes message: the run, and its generation.
func liveDoc(t *testing.T, message map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"run":        map[string]any{"taskId": 11, "inputs": []int{10}, "turns": 0, "first": 9},
		"generation": map[string]any{"attempt": 1, "message": message},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func pending(responseID string, blocks ...map[string]any) map[string]any {
	return map[string]any{"role": "assistant", "content": blocks, "stopReason": "pending", "timestamp": 3000, "responseId": responseID}
}

// The answer the model is writing is drawn as it grows: pi.live's generation is the turn's answer until Pi writes
// it as an entry, its text marked as still streaming.
func TestBuildViewDrawsTheAnswerBeingWritten(t *testing.T) {
	v := viewOf(entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("tell me about bicycles")), nil))
	for _, so := range []string{"The first", "The first bicycle was the draisine"} {
		v.Docs = map[string]json.RawMessage{"pi.live": liveDoc(t, pending("r1",
			map[string]any{"type": "thinking", "thinking": "history"}, text(so)))}
		got := transcript.BuildView(v)
		if len(got) != 2 || got[1].User {
			t.Fatalf("%q: %d rows", so, len(got))
		}
		blocks := got[1].Blocks
		if len(blocks) != 2 || blocks[1].Kind != transcript.Text || blocks[1].Text != so || !blocks[1].Streaming || !blocks[0].Streaming {
			t.Fatalf("%q: the answer being written is %+v", so, blocks)
		}
	}
}

// After a tool round, the next generation joins the turn's answer as the written ones do.
func TestTheAnswerBeingWrittenJoinsItsTurn(t *testing.T) {
	v := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("read a.txt")), nil),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 2000, "toolUse",
			map[string]any{"type": "toolCall", "id": "call_1", "name": "read", "arguments": map[string]any{"path": "a.txt"}}), nil),
		entry(t, 3, kernel.EntryToolResult, toolResultMessage(t, 2500, "call_1", false, text("hello")), nil),
	)
	v.Docs = map[string]json.RawMessage{"pi.live": liveDoc(t, pending("r2", text("It says hel")))}
	got := transcript.BuildView(v)
	if len(got) != 2 {
		t.Fatalf("%d rows, want the user's and one answer", len(got))
	}
	blocks := got[1].Blocks
	if last := blocks[len(blocks)-1]; last.Kind != transcript.Text || last.Text != "It says hel" || !last.Streaming {
		t.Fatalf("the answer's blocks: %+v", blocks)
	}
}

// A generation Pi already wrote as an entry is not drawn a second time, and a run with no generation draws nothing
// more.
func TestAWrittenAnswerIsNotDrawnTwice(t *testing.T) {
	written := assistantMessage(t, 3000, "stop", text("done"))
	written[0]["responseId"] = "r1"
	v := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("go")), nil),
		entry(t, 2, kernel.EntryAssistant, written, nil),
	)
	v.Docs = map[string]json.RawMessage{"pi.live": liveDoc(t, pending("r1", text("done")))}
	if got := transcript.BuildView(v); len(got) != 2 || len(got[1].Blocks) != 1 || got[1].Blocks[0].Streaming {
		t.Fatalf("the written answer was drawn again: %+v", got[1].Blocks)
	}
	v.Docs = map[string]json.RawMessage{"pi.live": json.RawMessage(`{"run":{"taskId":11}}`)}
	if got := transcript.BuildView(v); len(got) != 2 || len(got[1].Blocks) != 1 {
		t.Fatalf("a run with no generation: %+v", got)
	}
}
