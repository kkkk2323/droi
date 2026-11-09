package transcript_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/transcript"
)

// A tool result is what Pi's appendToolResult writes: the output, then — when there are diagnostics — one last text
// item rendering them for the model, and the same diagnostics structured in the entry's data. These tests build
// results that way and check what the row reads: the output and each diagnostic's sentence, never the wrapper,
// and a state that says what the harness did with the call.

type diagnostic struct{ severity, code, message string }

// piResult is a tool-result entry as Pi writes one.
func piResult(t *testing.T, callID string, isError bool, output []string, ds ...diagnostic) kernel.Entry {
	t.Helper()
	content := []map[string]any{}
	for _, o := range output {
		content = append(content, map[string]any{"type": "text", "text": o})
	}
	structured := []map[string]any{}
	if len(ds) > 0 {
		lines := make([]string, len(ds))
		for i, d := range ds {
			lines[i] = fmt.Sprintf("[%s] %s", d.severity, d.message)
			structured = append(structured, map[string]any{"severity": d.severity, "code": d.code, "message": d.message})
		}
		content = append(content, map[string]any{"type": "text", "text": "<harness>\n" + strings.Join(lines, "\n") + "\n</harness>"})
	}
	return entry(t, 3, kernel.EntryToolResult, []map[string]any{{
		"role": "toolResult", "toolCallId": callID, "toolName": "bash", "content": content, "isError": isError, "timestamp": 2000,
	}}, map[string]any{"diagnostics": structured})
}

// shownResult is what the row reads of the one call in a turn whose result is result.
func shownResult(t *testing.T, result kernel.Entry) (transcript.ToolResultView, *transcript.ToolCall) {
	t.Helper()
	entries := transcript.BuildView(viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 500, text("run it")), nil),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 1000, "toolUse",
			map[string]any{"type": "toolCall", "id": "c1", "name": "bash", "arguments": map[string]any{"command": "make"}}), nil),
		result,
	))
	if len(entries) != 2 || len(entries[1].Blocks) == 0 || len(entries[1].Blocks[0].Calls) != 1 {
		t.Fatalf("the turn built %+v", entries)
	}
	call := entries[1].Blocks[0].Calls[0]
	return transcript.ReadResult(call), call
}

// TestAResultsDiagnosticsReadAsSentencesAfterItsOutput: a long command keeps the end of its output and says so in
// a warning, with the path of the full output. Both are the result's notes, read without the harness's wrapper —
// tags and severities — and kept apart from the output, which the row cuts at its first lines.
func TestAResultsDiagnosticsReadAsSentencesAfterItsOutput(t *testing.T) {
	shown, _ := shownResult(t, piResult(t, "c1", false, []string{"ok 1\nok 2\n"},
		diagnostic{"info", "full_output", "Full output: /tmp/pi/out-1.txt"},
		diagnostic{"warn", "truncated", "Output truncated to its end: 812 lines, 40210 bytes dropped"},
	))
	if shown.Text != "ok 1\nok 2" {
		t.Errorf("the output reads %q, want the command's own lines", shown.Text)
	}
	if want := "Full output: /tmp/pi/out-1.txt\nOutput truncated to its end: 812 lines, 40210 bytes dropped"; shown.Notes != want {
		t.Errorf("the notes read\n%q\nwant\n%q", shown.Notes, want)
	}
	for _, wrapper := range []string{"<harness>", "</harness>", "[warn]", "[info]"} {
		if strings.Contains(shown.Text+shown.Notes, wrapper) {
			t.Errorf("the row still shows %q", wrapper)
		}
	}
	if shown.IsError || shown.Interrupted || shown.Blocked || shown.Pending {
		t.Errorf("a truncated result that succeeded reads as %+v", shown)
	}
}

// TestACallTheHarnessEndedReadsAsWhatHappened: the harness writes an error result for a call it did not let finish,
// and its diagnostic code is why. Stopped, cut off by a restart, or never started because its caller ended: the
// call did not fail. Refused by a rule or a person: it did not run, and did not fail either. A call that kept
// crashing the process, or a command that exited non-zero, did fail.
func TestACallTheHarnessEndedReadsAsWhatHappened(t *testing.T) {
	cases := []struct {
		code, message              string
		output                     []string
		interrupted, blocked, fail bool
	}{
		{code: "aborted", message: "Tool bash was aborted", output: []string{"partial"}, interrupted: true},
		{code: "interrupted", message: "Tool bash was interrupted by a restart and may have partially run", interrupted: true},
		{code: "abandoned", message: "Tool bash was not started: its caller ended with a restart", interrupted: true},
		{code: "blocked", message: "Tool call blocked: bash is disabled here", blocked: true},
		{code: "restart_limit", message: "Tool bash was interrupted 5 times; it is not run again", fail: true},
		{code: "exit_code", message: "Command exited with code 2", output: []string{"make: *** [all] Error 2\n"}, fail: true},
		{code: "tool_unavailable", message: "Tool bash is not available", fail: true},
		{code: "invalid_arguments", message: "command: required", fail: true},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			shown, call := shownResult(t, piResult(t, "c1", true, tc.output, diagnostic{"error", tc.code, tc.message}))
			if shown.Interrupted != tc.interrupted || shown.Blocked != tc.blocked || shown.IsError != tc.fail {
				t.Errorf("reads interrupted=%v blocked=%v failed=%v, want %v %v %v",
					shown.Interrupted, shown.Blocked, shown.IsError, tc.interrupted, tc.blocked, tc.fail)
			}
			if shown.Pending {
				t.Error("a call the harness ended reads as still running")
			}
			if shown.Notes != tc.message {
				t.Errorf("the notes read %q, want %q", shown.Notes, tc.message)
			}
			if want := strings.TrimRight(strings.Join(tc.output, "\n"), "\n"); shown.Text != want {
				t.Errorf("the output reads %q, want %q", shown.Text, want)
			}
			if label, pending := transcript.ClusterLabel([]*transcript.ToolCall{call}); pending {
				t.Errorf("the cluster says %q for a call that ended", label)
			}
		})
	}
}

// TestTheHarnessEndingIsItsOwnLastWord: a tool's own diagnostics come first and the harness appends its ending
// last, so only the last one says how the call ended. A tool that reported something coded "aborted" and then
// failed still failed.
func TestTheHarnessEndingIsItsOwnLastWord(t *testing.T) {
	shown, _ := shownResult(t, piResult(t, "c1", true, []string{"out"},
		diagnostic{"warn", "aborted", "a sub-step was aborted"},
		diagnostic{"error", "exit_code", "Command exited with code 1"},
	))
	if !shown.IsError || shown.Interrupted {
		t.Errorf("a failed command reads as %+v", shown)
	}
}

// TestOnlyPisRenderingOfTheDiagnosticsIsDropped: several output items all stay, a picture stays beside the notes,
// and an item after the output that is not Pi's rendering of the diagnostics — an imported history may hold one —
// is output too.
func TestOnlyPisRenderingOfTheDiagnosticsIsDropped(t *testing.T) {
	shown, _ := shownResult(t, piResult(t, "c1", false, []string{"first", "second"}, diagnostic{"info", "full_output", "Full output: /tmp/x"}))
	if shown.Text != "first\nsecond" || shown.Notes != "Full output: /tmp/x" {
		t.Errorf("two output items read %q with notes %q", shown.Text, shown.Notes)
	}

	withPicture := piResult(t, "c1", false, nil, diagnostic{"info", "image", "Read image file [image/png]"})
	var model []map[string]any
	if err := json.Unmarshal(withPicture.Model, &model); err != nil {
		t.Fatal(err)
	}
	content := model[0]["content"].([]any)
	model[0]["content"] = append([]any{map[string]any{"type": "image", "data": "AAAA", "mimeType": "image/png"}}, content...)
	withPicture.Model, _ = json.Marshal(model)
	shown, _ = shownResult(t, withPicture)
	if len(shown.Images) != 1 || shown.Text != "" || shown.Notes != "Read image file [image/png]" {
		t.Errorf("a picture with a note reads %+v", shown)
	}

	imported := piResult(t, "c1", false, []string{"real output", "<harness>\n[info] not this result's\n</harness>"}, diagnostic{"info", "full_output", "Full output: /tmp/y"})
	var m2 []map[string]any
	_ = json.Unmarshal(imported.Model, &m2)
	c2 := m2[0]["content"].([]any)
	m2[0]["content"] = c2[:len(c2)-1] // no rendering of its own diagnostics at the end
	imported.Model, _ = json.Marshal(m2)
	shown, _ = shownResult(t, imported)
	if shown.Text != "real output\n<harness>\n[info] not this result's\n</harness>" {
		t.Errorf("an item that is not the rendering was dropped: %q", shown.Text)
	}
}

// TestAResultWithoutDiagnosticsKeepsAllItsOutput: only Pi's own rendering is dropped, and Pi writes it only when
// there are diagnostics. A tool whose output merely mentions the harness keeps every word.
func TestAResultWithoutDiagnosticsKeepsAllItsOutput(t *testing.T) {
	shown, _ := shownResult(t, piResult(t, "c1", false, []string{"first", "<harness>\n[info] printed by the tool\n</harness>"}))
	if shown.Text != "first\n<harness>\n[info] printed by the tool\n</harness>" {
		t.Errorf("the row reads %q, want the tool's whole output", shown.Text)
	}
}
