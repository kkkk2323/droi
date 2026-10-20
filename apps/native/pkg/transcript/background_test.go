package transcript

import (
	"encoding/json"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/protocol"
)

func withID(id string) func(*protocol.FactoryDroidMessage) {
	return func(m *protocol.FactoryDroidMessage) { m.ID = id }
}

func resultOf(id string, content any) string {
	b, _ := json.Marshal([]map[string]any{{"type": "tool_result", "toolUseId": id, "content": content}})
	return string(b)
}

const started = "Background process started (PID: 97178)\nCommand: cd /repo && scripts/release.sh 1.57.1\nOutput: /tmp/droid-bg-1.out\nStatus: Running in background\n\nThe command's result will be delivered automatically when it finishes."

func TestJobsReadABackgroundCommand(t *testing.T) {
	msgs := []protocol.FactoryDroidMessage{
		msg("assistant", `[{"type":"tool_use","id":"s1","name":"Script","input":{"script":"await tools.Execute({})"}}]`),
		msg("assistant", `[{"type":"tool_use","id":"s1-1","name":"Execute","input":{"summary":"Release Droi 1.57.1","command":"cd /repo && scripts/release.sh 1.57.1","fireAndForget":true},"scriptExecution":{"runId":"s1","outerToolUseId":"s1"}}]`),
		msg("user", resultOf("s1-1", started), at(1000)),
		// The Script's own answer quotes the start; it is no job of its own.
		msg("tool", resultOf("s1", []map[string]any{{"type": "text", "text": "exit null · pid 97178\n" + started}, {"type": "text", "text": "[Script completed · 0.2 s elapsed · 1 call · 1 background execute running]"}}), at(1001)),
	}
	jobs := Jobs(msgs)
	eq(t, len(jobs), 1)
	j := jobs[0]
	eq(t, []any{j.ID, j.Script, j.Summary, j.Command, j.PID, j.OutputFile, j.StartedAt, j.Done}, []any{"s1-1", false, "Release Droi 1.57.1", "cd /repo && scripts/release.sh 1.57.1", 97178, "/tmp/droid-bg-1.out", 1000.0, false})
	eq(t, RunningJobs(jobs), 1)

	// Live, the end comes without its text.
	live := append(msgs, msg("user", `[]`, withID("execute-completion:s1-1"), at(5000)))
	j = Jobs(live)[0]
	eq(t, []any{j.Done, j.EndedAt, j.ExitCode == nil, j.Failed()}, []any{true, 5000.0, true, false})

	// Loaded, it says how the command exited.
	ended := "<system-reminder>Background Execute command finished.\ntool_use_id: s1-1\nsummary: Release Droi 1.57.1\nexit_status: code 1\nstopped_by: process\noutput_file: /tmp/droid-bg-1.out\noutput: release: main is behind origin/main\n</system-reminder>"
	b, _ := json.Marshal([]map[string]any{{"type": "text", "text": ended}})
	loaded := append(msgs, msg("user", string(b), withID("execute-completion:s1-1"), at(5000), func(m *protocol.FactoryDroidMessage) {
		m.Visibility = protocol.MessageVisibilityLLMOnly
	}))
	j = Jobs(loaded)[0]
	eq(t, []any{j.Done, *j.ExitCode, j.Failed(), RunningJobs(Jobs(loaded))}, []any{true, 1, true, 0})
}

func TestJobsReadAScriptLeftRunning(t *testing.T) {
	running := []map[string]any{{"type": "text", "text": "[Script running · 30.0 s elapsed · 1 pending call]"}, {"type": "text", "text": `{"toolCallId":"s2","status":"running"}`}}
	msgs := []protocol.FactoryDroidMessage{
		msg("assistant", `[{"type":"tool_use","id":"s2","name":"Script","input":{"script":"await tools.Execute({}); await tools.Read({})"}}]`),
		msg("tool", resultOf("s2", running), at(2000)),
	}
	jobs := Jobs(msgs)
	eq(t, len(jobs), 1)
	eq(t, []any{jobs[0].ID, jobs[0].Script, jobs[0].Summary, jobs[0].Done}, []any{"s2", true, "Execute · Read", false})

	// A wait that saw it end ends it.
	waited := append(msgs,
		msg("assistant", `[{"type":"tool_use","id":"w1","name":"WaitForScript","input":{"toolCallId":"s2"}}]`),
		msg("tool", resultOf("w1", []map[string]any{{"type": "text", "text": `{"toolCallId":"s2","status":"failed","error":"boom"}`}}), at(3000)))
	j := Jobs(waited)[0]
	eq(t, []any{j.Done, j.Status, j.EndedAt, j.Failed()}, []any{true, "failed", 3000.0, true})

	// So does the Daemon's record of its end.
	b, _ := json.Marshal([]map[string]any{{"type": "text", "text": `Script s2 reached its terminal state with result {"toolCallId":"s2","status":"completed","result":null}. This is a new asynchronous lifecycle event.`}})
	recorded := append(msgs, msg("user", string(b), withID("script-completion:sess:s2:s2"), at(4000)))
	j = Jobs(recorded)[0]
	eq(t, []any{j.Done, j.Status, j.EndedAt, j.Failed()}, []any{true, "completed", 4000.0, false})
	j = Jobs(append(msgs, msg("user", `[]`, withID("script-completion:sess:s2:s2"), at(4000))))[0]
	eq(t, []any{j.Done, j.Status}, []any{true, ""})
}

func TestJobsIgnoreOtherAnswers(t *testing.T) {
	msgs := []protocol.FactoryDroidMessage{
		msg("assistant", `[{"type":"tool_use","id":"s3","name":"Script","input":{"script":"x"}}]`),
		msg("tool", resultOf("s3", []map[string]any{{"type": "text", "text": `{"toolCallId":"s3","status":"completed"}`}})),
		msg("assistant", `[{"type":"tool_use","id":"e1","name":"Execute","input":{"command":"echo Background process started"}}]`),
		msg("tool", resultOf("e1", "Background process started is what it printed")),
	}
	eq(t, len(Jobs(msgs)), 0)
}
