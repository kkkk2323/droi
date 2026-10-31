package transcript

import (
	"testing"

	"github.com/kkkk2323/droid-sdk-go/protocol"
)

// specCall is the ExitSpecMode call of a plan, answered with result unless
// it is empty.
func specCall(t *testing.T, result string) *ToolCall {
	t.Helper()
	ms := []protocol.FactoryDroidMessage{
		msg("assistant", `[{"type":"tool_use","id":"spec","name":"ExitSpecMode","input":{"plan":"1. Add hello.txt","title":"Hello"}}]`),
	}
	if result != "" {
		ms = append(ms, msg("tool", result))
	}
	es := Build(ms)
	eq(t, kinds(es[0]), []any{"spec"})
	return es[0].Blocks[0].Call
}

func TestReadSpec(t *testing.T) {
	eq(t, ReadSpec(specCall(t, "")), SpecPlan{Title: "Hello", Plan: "1. Add hello.txt"})
	eq(t, ReadSpec(specCall(t, `[{"type":"tool_result","toolUseId":"spec","isError":true,"content":"Error: Plan not approved - remaining in Spec Mode."}]`)).State, SpecRejected)

	got := ReadSpec(specCall(t, `[{"type":"tool_result","toolUseId":"spec","content":"{\"approved\":true,\"message\":\"…\",\"userComment\":\"Keep it short\"}"}]`))
	eq(t, []any{got.State, got.Comment, got.NewSession}, []any{SpecApproved, "Keep it short", false})

	got = ReadSpec(specCall(t, `[{"type":"tool_result","toolUseId":"spec","content":"{\"approved\":true,\"handoff\":{\"plan\":\"1.\",\"isNewSession\":true}}"}]`))
	eq(t, []any{got.State, got.NewSession}, []any{SpecApproved, true})

	// A result that is not the Daemon's JSON is still an approval: only a
	// declined plan comes back as an error.
	eq(t, ReadSpec(specCall(t, `[{"type":"tool_result","toolUseId":"spec","content":"Spec mode exited."}]`)).State, SpecApproved)
}

// The first message of a Session a spec handoff started, as droid 1.1's
// buildSpecHandoffPrompt writes it, with and without the user's note.
func TestReadSpecHandoff(t *testing.T) {
	const details = "If you need specific details from the planning session (like exact code snippets, error messages, or content generated), read the full transcript at: /Users/dev/.factory/sessions/a.jsonl"
	got, ok := ReadSpecHandoff("Implement the following plan:\n\nUser note: Keep it short\n\n## Steps\n\n1. Add hello.txt\n\n" + details)
	eq(t, []any{got, ok}, []any{SpecHandoff{Note: "Keep it short", Plan: "## Steps\n\n1. Add hello.txt", Source: "/Users/dev/.factory/sessions/a.jsonl"}, true})
	got, ok = ReadSpecHandoff("Implement the following plan:\n\n1. Add hello.txt\n\nIf you need specific details from the planning session, read the full spec at: /repo/specs/hello.md")
	eq(t, []any{got, ok}, []any{SpecHandoff{Plan: "1. Add hello.txt", Source: "/repo/specs/hello.md"}, true})
	for _, text := range []string{"Implement the following plan:\n\n1. Add hello.txt", "Please implement the following plan:\n\n1.\n\n" + details} {
		if _, ok := ReadSpecHandoff(text); ok {
			t.Errorf("%q read as a handoff", text)
		}
	}
	eq(t, []string{PlanTitle("## Steps\n\n1. Add"), PlanTitle("\n1. Add hello.txt\n2. Check"), PlanTitle("")}, []string{"Steps", "1. Add hello.txt", ""})
}
