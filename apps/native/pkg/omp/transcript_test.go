package omp

import (
	"testing"

	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

func TestTranscriptEntriesMapsRoles(t *testing.T) {
	msgs := []Message{
		{ID: "u1", Role: "user", Text: "hi"},
		{ID: "a1", Role: "assistant", Thinking: "plan", Text: "hello **world**"},
		{ID: "t1", Role: "tool", ToolName: "bash", ToolID: "tc1", ToolArgs: `{"command":"ls"}`, ToolOut: "a\nb"},
	}
	got := TranscriptEntries(msgs)
	if len(got) != 3 {
		t.Fatalf("%d entries", len(got))
	}
	if !got[0].User || got[0].Blocks[0].Kind != transcript.Text || got[0].Blocks[0].Text != "hi" {
		t.Fatalf("user %+v", got[0])
	}
	if got[1].Blocks[0].Kind != transcript.Thinking || got[1].Blocks[1].Kind != transcript.Text {
		t.Fatalf("assistant %+v", got[1])
	}
	if got[2].Blocks[0].Kind != transcript.Tools || got[2].Blocks[0].Calls[0].Use.Name != "bash" {
		t.Fatalf("tool %+v", got[2])
	}
	if got[2].Blocks[0].Calls[0].Input["command"] != "ls" {
		t.Fatalf("args %+v", got[2].Blocks[0].Calls[0].Input)
	}
}
