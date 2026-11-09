package transcript_test

import (
	"encoding/json"
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/transcript"
)

// A Kernel task call is a subagent card of its own: its droid, task and prompt from the call, the child the Kernel
// made from the call's details (pi.live's while it runs), and a background one's end from the entry it writes.

func taskCall(t *testing.T, background bool) kernel.Entry {
	args := map[string]any{"subagent_type": "explorer", "description": "Find the config", "prompt": "Where is it read?"}
	if background {
		args["run_in_background"] = true
	}
	return entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 2000, "toolUse",
		map[string]any{"type": "toolCall", "id": "tk", "name": "task", "arguments": args}), nil)
}

func taskResult(t *testing.T, textOut string, details map[string]any) kernel.Entry {
	return entry(t, 3, kernel.EntryToolResult, []map[string]any{{
		"role": "toolResult", "toolCallId": "tk", "toolName": "task", "isError": false, "timestamp": 3000,
		"content": []map[string]any{text(textOut)}, "details": details,
	}}, nil)
}

func subagentOf(t *testing.T, v kernel.View) *transcript.ToolCall {
	t.Helper()
	for _, e := range transcript.BuildView(v) {
		for _, b := range e.Blocks {
			if b.Kind == transcript.Subagent {
				return b.Call
			}
		}
	}
	t.Fatal("no subagent card")
	return nil
}

func TestATaskCallIsASubagentCardWithItsChild(t *testing.T) {
	user := entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("go")), nil)

	// Before the child exists: the call's arguments only.
	call := subagentOf(t, viewOf(user, taskCall(t, false)))
	if s := call.Subagent; s == nil || s.Type != "explorer" || s.Description != "Find the config" || s.Prompt != "Where is it read?" || s.ConversationID != 0 {
		t.Fatalf("before the child: %+v", call.Subagent)
	}

	// Running: pi.live's slot carries the child.
	live, _ := json.Marshal(map[string]any{
		"run":   map[string]any{"taskId": 1, "inputs": []any{}, "turns": 1},
		"tools": []map[string]any{{"callId": "tk", "name": "task", "status": "running", "details": map[string]any{"conversationId": 7, "subagentType": "explorer", "description": "Find the config", "status": "running"}}},
	})
	v := viewOf(user, taskCall(t, false))
	v.Docs = map[string]json.RawMessage{"pi.live": live}
	if s := subagentOf(t, v).Subagent; s.ConversationID != 7 || s.Status != "running" {
		t.Errorf("running: %+v", s)
	}

	// Answered: the result's details.
	call = subagentOf(t, viewOf(user, taskCall(t, false), taskResult(t, "It is in config.ts.\n\n(task_id: 7; pass it as resume to go on in this subagent.)",
		map[string]any{"conversationId": 7, "subagentType": "explorer", "description": "Find the config", "status": "completed"})))
	if s := call.Subagent; s.ConversationID != 7 || s.Status != "completed" {
		t.Errorf("answered: %+v", s)
	}
}

func TestABackgroundSubagentsEndIsANoteAndUpdatesItsCard(t *testing.T) {
	user := entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("go")), nil)
	started := taskResult(t, "Started in the background as task_id 7.", map[string]any{
		"conversationId": 7, "subagentType": "explorer", "description": "Find the config", "status": "running", "background": true})
	data := map[string]any{"conversationId": 7, "subagentType": "explorer", "description": "Find the config",
		"status": "completed", "background": true, "callId": "tk", "output": "It is in config.ts."}
	done := entry(t, 4, transcript.EntrySubagentDone, []map[string]any{{"role": "user", "content": []map[string]any{text("Background subagent 7 ended.")}, "timestamp": 4000}}, data)

	v := viewOf(user, taskCall(t, true), started)
	if s := subagentOf(t, v).Subagent; !s.Background || s.Status != "running" || s.Output != "" {
		t.Errorf("started: %+v", s)
	}
	v = viewOf(user, taskCall(t, true), started, done)
	if s := subagentOf(t, v).Subagent; s.Status != "completed" || s.Output != "It is in config.ts." {
		t.Errorf("ended: %+v", s)
	}
	entries := transcript.BuildView(v)
	last := entries[len(entries)-1]
	if !last.Note || len(last.Blocks) != 1 || last.Blocks[0].Text != "Subagent “Find the config” finished." {
		t.Errorf("the end's note: %+v", last)
	}
}
