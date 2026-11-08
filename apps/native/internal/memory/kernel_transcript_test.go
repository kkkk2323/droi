package memory

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

func kernelEntry(id, conversation kernel.ID, kind string, model any, data any) kernel.Entry {
	e := kernel.Entry{ID: id, ConversationID: conversation, Kind: kind}
	if model != nil {
		e.Model, _ = json.Marshal(model)
	}
	if data != nil {
		e.Data, _ = json.Marshal(data)
	}
	return e
}

func TestAKernelTranscriptReadsAsADroidOne(t *testing.T) {
	user := func(id kernel.ID, content any) kernel.Entry {
		return kernelEntry(id, 7, kernel.EntryUser, []any{map[string]any{"role": "user", "content": content}}, nil)
	}
	entries := []kernel.Entry{
		// A fork's inherited entry is its source's.
		kernelEntry(1, 3, kernel.EntryUser, []any{map[string]any{"role": "user", "content": "the source's question"}}, nil),
		user(2, "<skill name=\"review\" location=\"/x/SKILL.md\">\nReview carefully.\n</skill>\n\nthe login page"),
		kernelEntry(3, 7, kernel.EntryAssistant, []any{map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "thinking", "thinking": "secret thoughts"},
			map[string]any{"type": "text", "text": "Looking."},
			map[string]any{"type": "toolCall", "id": "c1", "name": "Read", "arguments": map[string]any{"path": "a.go"}},
		}}}, nil),
		kernelEntry(4, 7, kernel.EntryToolResult, []any{map[string]any{"role": "toolResult", "toolCallId": "c1", "isError": true, "content": []any{map[string]any{"type": "text", "text": "no such\nfile"}}}}, nil),
		kernelEntry(5, 7, kernel.EntryCompaction, []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "The conversation history before this point was compacted into the following summary:\n\n<summary>\nThey fixed login.\n</summary>"}}}}, nil),
		kernelEntry(6, 7, kernel.EntryNote, nil, map[string]any{"text": "Background task finished"}),
		// A write submission: nothing the user said.
		user(7, []any{}),
		kernelEntry(8, 7, kernel.EntryProcess, nil, map[string]any{"command": "ls"}),
		user(9, []any{map[string]any{"type": "text", "text": "thanks <system-reminder>x</system-reminder>"}}),
	}
	got := KernelTranscript(7, entries, 80_000)
	want := "User: /review the login page\n" +
		"Droid: Looking.\n" +
		"[tool call] Read {\"path\":\"a.go\"}\n" +
		"[tool error] no such file\n" +
		"[Earlier part of the Session, as compacted] They fixed login.\n" +
		"[notice] Background task finished\n" +
		"User: thanks\n"
	if got.Text != want || got.UserPrompts != 2 || got.Omitted {
		t.Fatalf("got %q (%d prompts, omitted %v)", got.Text, got.UserPrompts, got.Omitted)
	}

	// Short of room, the user's words stay and the rest is marked as left out.
	short := KernelTranscript(7, entries, len([]rune("User: /review the login page\nUser: thanks\n")))
	if short.Text != "User: /review the login page\n[... omitted ...]\nUser: thanks\n" || !short.Omitted {
		t.Fatalf("short: %q", short.Text)
	}
	if strings.Contains(got.Text, "secret") || strings.Contains(got.Text, "source") {
		t.Fatal(got.Text)
	}
}
