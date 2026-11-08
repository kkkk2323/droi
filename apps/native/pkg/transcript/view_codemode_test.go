package transcript_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

// A Kernel codemode call is a script card: its program, and the calls its script made hanging under it, each with
// what it answered — from the result's details once it finished, from pi.live while it runs.

func codemodeCall(t *testing.T) kernel.Entry {
	return entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 2000, "toolUse",
		map[string]any{"type": "toolCall", "id": "cm", "name": "codemode", "arguments": map[string]any{
			"code": `const a = await tools.read({ path: "a.txt" }); await tools.bash({ command: "false" })`}},
	), nil)
}

func TestACodemodeResultHangsItsCallsUnderTheScript(t *testing.T) {
	result := []map[string]any{{
		"role": "toolResult", "toolCallId": "cm", "toolName": "codemode", "isError": false, "timestamp": 3000,
		"content": []map[string]any{text("Script completed\nOutput:\nhi")},
		"details": map[string]any{"calls": []map[string]any{
			{"id": 1, "name": "read", "args": `{"path":"a.txt"}`, "status": "ok", "output": "hello\n"},
			{"id": 2, "name": "bash", "args": `{"command":"false"}`, "status": "error", "error": "exit 1", "output": "exit 1", "truncated": true},
		}},
	}}
	v := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("go")), nil),
		codemodeCall(t),
		entry(t, 3, kernel.EntryToolResult, result, nil),
	)
	call := transcript.BuildView(v)[1].Blocks[0].Calls[0]
	if !call.Lifecycle || len(call.Nested) != 2 {
		t.Fatalf("the script's calls: lifecycle %v, %d nested", call.Lifecycle, len(call.Nested))
	}
	read, bash := call.Nested[0], call.Nested[1]
	if read.Use.ID != "cm/c1" || read.Use.Name != "read" || read.Input["path"] != "a.txt" {
		t.Errorf("the first call is %+v %v", read.Use, read.Input)
	}
	if r := transcript.ReadResult(read); r.Text != "hello" || r.IsError || r.Pending {
		t.Errorf("read answered %+v", r)
	}
	if r := transcript.ReadResult(bash); !r.IsError || r.Text != "exit 1" || r.Notes == "" {
		t.Errorf("bash answered %+v", r)
	}
	if label, pending := transcript.ClusterLabel([]*transcript.ToolCall{call}); pending || label != "Used 2 tools" {
		t.Errorf("the cluster says %q, pending %v", label, pending)
	}
	run := transcript.ReadScriptRun(call)
	if run.Status != "completed" || run.Output == "" {
		t.Errorf("the run is %+v", run)
	}
	if script, _, ok := transcript.ScriptSource(call); !ok || script == "" {
		t.Errorf("the source is %q %v", script, ok)
	}
	if got := transcript.ScriptSummary(call); got != "read · bash" {
		t.Errorf("the summary is %q", got)
	}
}

func TestARunningCodemodeShowsTheCallsItIsMaking(t *testing.T) {
	live, err := json.Marshal(map[string]any{
		"run": map[string]any{"taskId": 1, "inputs": []any{}, "turns": 1},
		"nestedTools": []map[string]any{{
			"parentCallId": "cm", "parentTaskId": 5, "taskId": 6, "callId": "cm/c1", "name": "read",
			"arguments": map[string]any{"path": "a.txt"}, "status": "running", "output": "partial",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	v := viewOf(entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("go")), nil), codemodeCall(t))
	v.Docs = map[string]json.RawMessage{"pi.live": live}
	call := transcript.BuildView(v)[1].Blocks[0].Calls[0]
	if len(call.Nested) != 1 || call.Nested[0].Use.ID != "cm/c1" {
		t.Fatalf("the running script's calls: %+v", call.Nested)
	}
	if r := transcript.ReadResult(call.Nested[0]); !r.Pending || r.Text != "partial" {
		t.Errorf("the running call reads %+v", r)
	}
	if !transcript.Pending(call) {
		t.Error("the script is not running")
	}
	if _, pending := transcript.ClusterLabel([]*transcript.ToolCall{call}); !pending {
		t.Error("the cluster is not running")
	}

	// Before its first call, the card is still a script's.
	v.Docs = nil
	if call := transcript.BuildView(v)[1].Blocks[0].Calls[0]; call.Nested == nil {
		t.Error("a script with no calls yet is not a script card")
	}
}

func TestAScriptCallShowsTheWholeArgumentsNotPisPreview(t *testing.T) {
	long := "echo " + strings.Repeat("z", 300)
	preview := `{"command":"echo ` + strings.Repeat("z", 180) + `...`
	result := []map[string]any{{
		"role": "toolResult", "toolCallId": "cm", "toolName": "codemode", "isError": false, "timestamp": 3000,
		"content": []map[string]any{text("Script completed")},
		"details": map[string]any{"calls": []map[string]any{
			{"id": 1, "name": "bash", "args": preview, "arguments": map[string]any{"command": long}, "status": "ok"},
			{"id": 2, "name": "bash", "args": preview, "status": "cancelled"},
		}},
	}}
	v := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("go")), nil),
		codemodeCall(t),
		entry(t, 3, kernel.EntryToolResult, result, nil),
	)
	call := transcript.BuildView(v)[1].Blocks[0].Calls[0]
	if len(call.Nested) != 2 {
		t.Fatalf("%d nested", len(call.Nested))
	}
	if got := call.Nested[0].Input["command"]; got != long {
		t.Errorf("the call's command is %q", got)
	}
	// A result without Kernel's arguments: Pi's preview is no JSON, and shows as the text it is.
	if raw := string(call.Nested[1].RawInput); !strings.Contains(raw, "zzz...") {
		t.Errorf("the preview shows as %q", raw)
	}
	if r := transcript.ReadResult(call.Nested[1]); !r.Interrupted {
		t.Errorf("the cancelled call reads %+v", r)
	}
}
